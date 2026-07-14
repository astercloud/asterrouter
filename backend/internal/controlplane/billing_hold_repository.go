package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const billingHoldSelectColumns = `id, operation_id, profile_scope, tenant_id, credential_id, credential_source,
integration_id, principal_type, principal_id, external_subject_reference, request_fingerprint, status, version,
reserved_amount_cents, settled_amount_cents, currency, price_snapshot_id, estimate_source, reason,
budget_period_start, expires_at, created_at, updated_at, settled_at, released_at`

type billingHoldScanner interface {
	Scan(dest ...any) error
}

func scanBillingHold(scanner billingHoldScanner) (BillingHold, error) {
	var hold BillingHold
	var settledAt sql.NullTime
	var releasedAt sql.NullTime
	err := scanner.Scan(
		&hold.ID, &hold.OperationID, &hold.ProfileScope, &hold.TenantID, &hold.CredentialID, &hold.CredentialSource,
		&hold.IntegrationID, &hold.PrincipalType, &hold.PrincipalID, &hold.ExternalSubjectReference,
		&hold.RequestFingerprint, &hold.Status, &hold.Version, &hold.ReservedAmountCents, &hold.SettledAmountCents,
		&hold.Currency, &hold.PriceSnapshotID, &hold.EstimateSource, &hold.Reason, &hold.BudgetPeriodStart,
		&hold.ExpiresAt, &hold.CreatedAt, &hold.UpdatedAt, &settledAt, &releasedAt,
	)
	if err != nil {
		return BillingHold{}, err
	}
	if settledAt.Valid {
		hold.SettledAt = &settledAt.Time
	}
	if releasedAt.Valid {
		hold.ReleasedAt = &releasedAt.Time
	}
	return hold, nil
}

func (r *MemoryRepository) CreateAIOperationWithBillingHold(_ context.Context, operation AIOperation, admission BillingHoldAdmission) (AIOperation, bool, error) {
	if err := validateBillingHoldAdmission(operation, admission); err != nil {
		return AIOperation{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if operation.IdempotencyKey != "" {
		for _, current := range r.aiOperations {
			if sameAIOperationIdempotencyScope(current, operation) {
				return current, false, nil
			}
		}
	}
	if _, exists := r.aiOperations[operation.ID]; exists {
		return AIOperation{}, false, fmt.Errorf("ai operation %q already exists", operation.ID)
	}
	if _, exists := r.billingHolds[admission.Hold.ID]; exists {
		return AIOperation{}, false, fmt.Errorf("billing hold %q already exists", admission.Hold.ID)
	}
	if _, found := memoryBillingHoldForOperation(r.billingHolds, operation.ID); found {
		return AIOperation{}, false, fmt.Errorf("billing hold for operation %q already exists", operation.ID)
	}
	if err := enforceMemoryBillingHoldBudget(r, admission); err != nil {
		return AIOperation{}, false, err
	}
	r.aiOperations[operation.ID] = operation
	r.billingHolds[admission.Hold.ID] = admission.Hold
	return operation, true, nil
}

func (r *PostgresRepository) CreateAIOperationWithBillingHold(ctx context.Context, operation AIOperation, admission BillingHoldAdmission) (AIOperation, bool, error) {
	if err := validateBillingHoldAdmission(operation, admission); err != nil {
		return AIOperation{}, false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return AIOperation{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := insertAIOperation(ctx, tx, operation)
	if err != nil {
		return AIOperation{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return AIOperation{}, false, err
	}
	if rows == 0 {
		existing, findErr := findPostgresAIOperationByIdempotencyScope(ctx, tx, operation)
		if findErr != nil {
			return AIOperation{}, false, findErr
		}
		if err := tx.Commit(); err != nil {
			return AIOperation{}, false, err
		}
		return existing, false, nil
	}
	if err := enforcePostgresBillingHoldBudget(ctx, tx, admission); err != nil {
		return AIOperation{}, false, err
	}
	if err := insertBillingHold(ctx, tx, admission.Hold); err != nil {
		return AIOperation{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return AIOperation{}, false, err
	}
	return operation, true, nil
}

func findPostgresAIOperationByIdempotencyScope(ctx context.Context, executor aiJobExecutor, operation AIOperation) (AIOperation, error) {
	return scanAIOperation(executor.QueryRowContext(ctx, `SELECT `+aiOperationSelectColumns+` FROM ai_operations WHERE
profile_scope=$1 AND tenant_id=$2 AND credential_source=$3 AND credential_id=$4 AND integration_id=$5 AND principal_type=$6 AND principal_id=$7 AND external_subject_reference=$8 AND operation=$9 AND idempotency_key=$10`,
		operation.ProfileScope, operation.TenantID, operation.CredentialSource, operation.CredentialID, operation.IntegrationID,
		operation.PrincipalType, operation.PrincipalID, operation.ExternalSubjectReference, operation.Operation, operation.IdempotencyKey))
}

func insertBillingHold(ctx context.Context, executor usageRecordExecutor, hold BillingHold) error {
	_, err := executor.ExecContext(ctx, `
INSERT INTO billing_holds(
  id, operation_id, profile_scope, tenant_id, credential_id, credential_source, integration_id, principal_type,
  principal_id, external_subject_reference, request_fingerprint, status, version, reserved_amount_cents,
  settled_amount_cents, currency, price_snapshot_id, estimate_source, reason, budget_period_start, expires_at,
  created_at, updated_at, settled_at, released_at
)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,NULL,NULL)
`, hold.ID, hold.OperationID, hold.ProfileScope, hold.TenantID, hold.CredentialID, hold.CredentialSource,
		hold.IntegrationID, hold.PrincipalType, hold.PrincipalID, hold.ExternalSubjectReference, hold.RequestFingerprint,
		hold.Status, hold.Version, hold.ReservedAmountCents, hold.SettledAmountCents, hold.Currency, hold.PriceSnapshotID,
		hold.EstimateSource, hold.Reason, hold.BudgetPeriodStart, hold.ExpiresAt, hold.CreatedAt, hold.UpdatedAt)
	return err
}

func enforceMemoryBillingHoldBudget(r *MemoryRepository, admission BillingHoldAdmission) error {
	if admission.MonthlyBudgetCents <= 0 {
		return nil
	}
	hold := admission.Hold
	exposure := 0
	periodEnd := hold.BudgetPeriodStart.AddDate(0, 1, 0)
	for _, entry := range r.billingLedgerEntries {
		if entry.EntryType != BillingLedgerEntryTypeUsage || entry.Status != BillingLedgerStatusApplied || entry.CreatedAt.Before(hold.BudgetPeriodStart) || !entry.CreatedAt.Before(periodEnd) {
			continue
		}
		operation, found := r.aiOperations[entry.OperationID]
		if found && sameBillingCredentialScope(operation.ProfileScope, operation.TenantID, operation.CredentialID, hold) {
			exposure += nonNegative(entry.AmountCents)
		}
	}
	for _, current := range r.billingHolds {
		if billingHoldCountsAgainstBudget(current.Status) && current.BudgetPeriodStart.Equal(hold.BudgetPeriodStart) && sameBillingCredentialScope(current.ProfileScope, current.TenantID, current.CredentialID, hold) {
			exposure += nonNegative(current.ReservedAmountCents)
		}
	}
	if exposure+hold.ReservedAmountCents > admission.MonthlyBudgetCents {
		return ErrBillingHoldBudgetExceeded
	}
	return nil
}

func sameBillingCredentialScope(profileScope, tenantID, credentialID string, hold BillingHold) bool {
	return profileScope == hold.ProfileScope && tenantID == hold.TenantID && credentialID == hold.CredentialID
}

func enforcePostgresBillingHoldBudget(ctx context.Context, tx *sql.Tx, admission BillingHoldAdmission) error {
	if admission.MonthlyBudgetCents <= 0 {
		return nil
	}
	hold := admission.Hold
	lockKey := strings.Join([]string{"billing_hold", hold.ProfileScope, hold.TenantID, hold.CredentialID, hold.BudgetPeriodStart.Format("2006-01")}, "\x00")
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return err
	}
	var settled int
	var active int
	periodEnd := hold.BudgetPeriodStart.AddDate(0, 1, 0)
	if err := tx.QueryRowContext(ctx, `
SELECT
  COALESCE((
    SELECT SUM(entry.amount_cents)
    FROM billing_ledger_entries entry
    JOIN ai_operations operation ON operation.id = entry.operation_id
    WHERE operation.profile_scope=$1 AND operation.tenant_id=$2 AND operation.credential_id=$3
      AND entry.entry_type=$4 AND entry.status=$5 AND entry.created_at >= $6 AND entry.created_at < $7
  ), 0),
  COALESCE((
    SELECT SUM(reserved_amount_cents)
    FROM billing_holds
    WHERE profile_scope=$1 AND tenant_id=$2 AND credential_id=$3 AND budget_period_start=$6
      AND status IN ($8,$9,$10)
  ), 0)
`, hold.ProfileScope, hold.TenantID, hold.CredentialID, BillingLedgerEntryTypeUsage, BillingLedgerStatusApplied,
		hold.BudgetPeriodStart, periodEnd, BillingHoldStatusReserved, BillingHoldStatusCommitted, BillingHoldStatusDisputed).Scan(&settled, &active); err != nil {
		return err
	}
	if settled+active+hold.ReservedAmountCents > admission.MonthlyBudgetCents {
		return ErrBillingHoldBudgetExceeded
	}
	return nil
}

func memoryBillingHoldForOperation(holds map[string]BillingHold, operationID string) (BillingHold, bool) {
	for _, hold := range holds {
		if hold.OperationID == operationID {
			return hold, true
		}
	}
	return BillingHold{}, false
}

func (r *MemoryRepository) FindBillingHoldByOperationID(_ context.Context, operationID string) (BillingHold, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hold, found := memoryBillingHoldForOperation(r.billingHolds, strings.TrimSpace(operationID))
	return hold, found, nil
}

func (r *PostgresRepository) FindBillingHoldByOperationID(ctx context.Context, operationID string) (BillingHold, bool, error) {
	hold, err := scanBillingHold(r.db.QueryRowContext(ctx, `SELECT `+billingHoldSelectColumns+` FROM billing_holds WHERE operation_id=$1`, strings.TrimSpace(operationID)))
	if errors.Is(err, sql.ErrNoRows) {
		return BillingHold{}, false, nil
	}
	return hold, err == nil, err
}

func (r *MemoryRepository) TransitionBillingHold(_ context.Context, operationID string, expectedVersion int, toStatus string, settledAmount int, reason string, transitionedAt time.Time) (BillingHold, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hold, found := memoryBillingHoldForOperation(r.billingHolds, strings.TrimSpace(operationID))
	if !found {
		return BillingHold{}, false, nil
	}
	if hold.Version != expectedVersion {
		return hold, false, nil
	}
	updated, err := prepareBillingHoldTransition(hold, toStatus, settledAmount, reason, transitionedAt)
	if err != nil {
		return BillingHold{}, false, err
	}
	if updated.Version == hold.Version {
		return hold, false, nil
	}
	r.billingHolds[hold.ID] = updated
	return updated, true, nil
}

func (r *PostgresRepository) TransitionBillingHold(ctx context.Context, operationID string, expectedVersion int, toStatus string, settledAmount int, reason string, transitionedAt time.Time) (BillingHold, bool, error) {
	hold, found, err := r.FindBillingHoldByOperationID(ctx, operationID)
	if err != nil || !found {
		return hold, false, err
	}
	if hold.Version != expectedVersion {
		return hold, false, nil
	}
	updated, err := prepareBillingHoldTransition(hold, toStatus, settledAmount, reason, transitionedAt)
	if err != nil {
		return BillingHold{}, false, err
	}
	if updated.Version == hold.Version {
		return hold, false, nil
	}
	result, err := r.db.ExecContext(ctx, `
UPDATE billing_holds
SET status=$1, version=$2, settled_amount_cents=$3, reason=$4, updated_at=$5, settled_at=$6, released_at=$7
WHERE operation_id=$8 AND version=$9 AND status=$10
`, updated.Status, updated.Version, updated.SettledAmountCents, updated.Reason, updated.UpdatedAt, updated.SettledAt,
		updated.ReleasedAt, updated.OperationID, expectedVersion, hold.Status)
	if err != nil {
		return BillingHold{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return BillingHold{}, false, err
	}
	if rows == 0 {
		current, _, findErr := r.FindBillingHoldByOperationID(ctx, operationID)
		return current, false, findErr
	}
	return updated, true, nil
}

func (r *MemoryRepository) ListBillingHolds(context.Context) ([]BillingHold, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]BillingHold, 0, len(r.billingHolds))
	for _, hold := range r.billingHolds {
		out = append(out, hold)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *PostgresRepository) ListBillingHolds(ctx context.Context) ([]BillingHold, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+billingHoldSelectColumns+` FROM billing_holds ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]BillingHold, 0)
	for rows.Next() {
		hold, scanErr := scanBillingHold(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, hold)
	}
	return out, rows.Err()
}

func releaseMemoryBillingHoldForOperation(r *MemoryRepository, operationID, reason string, transitionedAt time.Time) error {
	hold, found := memoryBillingHoldForOperation(r.billingHolds, operationID)
	if !found || oneOf(hold.Status, BillingHoldStatusSettled, BillingHoldStatusReleased, BillingHoldStatusCommitted) {
		return nil
	}
	updated, err := prepareBillingHoldTransition(hold, BillingHoldStatusReleased, 0, reason, transitionedAt)
	if err != nil {
		return err
	}
	r.billingHolds[hold.ID] = updated
	return nil
}

func settleMemoryBillingHoldForUsage(r *MemoryRepository, record UsageRecord, billing BillingLedgerEntry) error {
	if !billingHoldUsageIsFinal(record) {
		return nil
	}
	hold, found := memoryBillingHoldForOperation(r.billingHolds, record.OperationID)
	if !found || oneOf(hold.Status, BillingHoldStatusSettled, BillingHoldStatusReleased) {
		return nil
	}
	updated, err := prepareBillingHoldTransition(hold, BillingHoldStatusSettled, billing.AmountCents, "usage_ledger_applied", record.CreatedAt)
	if err != nil {
		return err
	}
	r.billingHolds[hold.ID] = updated
	return nil
}

func billingHoldUsageIsFinal(record UsageRecord) bool {
	switch strings.ToLower(strings.TrimSpace(record.UsageSource)) {
	case "gateway_final", "upstream_final", "provider_final", "provider_billing":
		return true
	default:
		return false
	}
}

func findPostgresBillingHoldForUpdate(ctx context.Context, tx *sql.Tx, operationID string) (BillingHold, bool, error) {
	hold, err := scanBillingHold(tx.QueryRowContext(ctx, `SELECT `+billingHoldSelectColumns+` FROM billing_holds WHERE operation_id=$1 FOR UPDATE`, strings.TrimSpace(operationID)))
	if errors.Is(err, sql.ErrNoRows) {
		return BillingHold{}, false, nil
	}
	return hold, err == nil, err
}

func persistPostgresBillingHoldTransition(ctx context.Context, tx *sql.Tx, hold BillingHold, expectedVersion int, expectedStatus string) error {
	result, err := tx.ExecContext(ctx, `
UPDATE billing_holds
SET status=$1, version=$2, settled_amount_cents=$3, reason=$4, updated_at=$5, settled_at=$6, released_at=$7
WHERE operation_id=$8 AND version=$9 AND status=$10
`, hold.Status, hold.Version, hold.SettledAmountCents, hold.Reason, hold.UpdatedAt, hold.SettledAt, hold.ReleasedAt,
		hold.OperationID, expectedVersion, expectedStatus)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrBillingHoldStateConflict
	}
	return nil
}

func releasePostgresBillingHoldForOperation(ctx context.Context, tx *sql.Tx, operationID, reason string, transitionedAt time.Time) error {
	hold, found, err := findPostgresBillingHoldForUpdate(ctx, tx, operationID)
	if err != nil || !found || oneOf(hold.Status, BillingHoldStatusSettled, BillingHoldStatusReleased, BillingHoldStatusCommitted) {
		return err
	}
	updated, err := prepareBillingHoldTransition(hold, BillingHoldStatusReleased, 0, reason, transitionedAt)
	if err != nil {
		return err
	}
	return persistPostgresBillingHoldTransition(ctx, tx, updated, hold.Version, hold.Status)
}

func settlePostgresBillingHoldForUsage(ctx context.Context, tx *sql.Tx, record UsageRecord, billing BillingLedgerEntry) error {
	if !billingHoldUsageIsFinal(record) {
		return nil
	}
	hold, found, err := findPostgresBillingHoldForUpdate(ctx, tx, record.OperationID)
	if err != nil || !found || oneOf(hold.Status, BillingHoldStatusSettled, BillingHoldStatusReleased) {
		return err
	}
	updated, err := prepareBillingHoldTransition(hold, BillingHoldStatusSettled, billing.AmountCents, "usage_ledger_applied", record.CreatedAt)
	if err != nil {
		return err
	}
	return persistPostgresBillingHoldTransition(ctx, tx, updated, hold.Version, hold.Status)
}
