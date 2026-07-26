package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (r *PostgresRepository) Acquire(ctx context.Context, request ProviderCapacityRequest) (ProviderCapacityLease, string, bool, error) {
	if err := validateProviderCapacityRequest(request); err != nil || request.LeaseDuration > maxProviderCapacityLeaseDuration {
		return ProviderCapacityLease{}, "", false, ErrProviderCapacityConfig
	}
	request.LeaseID = strings.TrimSpace(request.LeaseID)
	request.ProviderAccountID = strings.TrimSpace(request.ProviderAccountID)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ProviderCapacityLease{}, "", false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockPostgresProviderCapacity(ctx, tx, request.LeaseID, request.ProviderAccountID); err != nil {
		return ProviderCapacityLease{}, "", false, err
	}
	now, err := postgresProviderCapacityNow(ctx, tx)
	if err != nil {
		return ProviderCapacityLease{}, "", false, err
	}
	current, found, err := postgresProviderCapacityLease(ctx, tx, request.LeaseID)
	if err != nil {
		return ProviderCapacityLease{}, "", false, err
	}
	if found {
		if current.ProviderAccountID != request.ProviderAccountID || current.CapacityUnits != request.CapacityUnits {
			return ProviderCapacityLease{}, "", false, ErrProviderCapacityConflict
		}
		if current.ExpiresAt.After(now) {
			requestedExpiry := now.Add(request.LeaseDuration)
			if requestedExpiry.After(current.ExpiresAt) {
				current.ExpiresAt = requestedExpiry
				if _, err := tx.ExecContext(ctx, `UPDATE gateway_provider_capacity_leases SET expires_at=$2 WHERE id=$1`, current.ID, current.ExpiresAt); err != nil {
					return ProviderCapacityLease{}, "", false, err
				}
			}
			if err := tx.Commit(); err != nil {
				return ProviderCapacityLease{}, "", false, err
			}
			return current, "", true, nil
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM gateway_provider_capacity_leases WHERE id=$1`, current.ID); err != nil {
			return ProviderCapacityLease{}, "", false, err
		}
	}
	if err := prunePostgresProviderCapacity(ctx, tx, request.ProviderAccountID, now); err != nil {
		return ProviderCapacityLease{}, "", false, err
	}
	snapshot, err := postgresProviderCapacitySnapshot(ctx, tx, request.ProviderAccountID)
	if err != nil {
		return ProviderCapacityLease{}, "", false, err
	}
	reason := ""
	switch {
	case request.ConcurrencyLimit > 0 && snapshot.CapacityUnits+request.CapacityUnits > request.ConcurrencyLimit:
		reason = "concurrency_exhausted"
	case request.RPMLimit > 0 && snapshot.Requests >= request.RPMLimit:
		reason = "rpm_exhausted"
	case request.TPMLimit > 0 && snapshot.Tokens+request.EstimatedTokens > request.TPMLimit:
		reason = "tpm_exhausted"
	}
	if reason != "" {
		if err := tx.Commit(); err != nil {
			return ProviderCapacityLease{}, "", false, err
		}
		return ProviderCapacityLease{}, reason, false, nil
	}
	if request.RPMLimit > 0 || request.TPMLimit > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO gateway_provider_rate_samples(id, provider_account_id, estimated_tokens, occurred_at) VALUES($1,$2,$3,$4)`, "provider_sample_"+randomID(12), request.ProviderAccountID, request.EstimatedTokens, now); err != nil {
			return ProviderCapacityLease{}, "", false, err
		}
	}
	lease := ProviderCapacityLease{
		ID: request.LeaseID, ProviderAccountID: request.ProviderAccountID, CapacityUnits: request.CapacityUnits,
		ExpiresAt: now.Add(request.LeaseDuration),
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO gateway_provider_capacity_leases(id, provider_account_id, capacity_units, expires_at, created_at) VALUES($1,$2,$3,$4,$5)`, lease.ID, lease.ProviderAccountID, lease.CapacityUnits, lease.ExpiresAt, now); err != nil {
		return ProviderCapacityLease{}, "", false, err
	}
	if err := tx.Commit(); err != nil {
		return ProviderCapacityLease{}, "", false, err
	}
	return lease, "", true, nil
}

func (r *PostgresRepository) Extend(ctx context.Context, lease ProviderCapacityLease, duration time.Duration) (ProviderCapacityLease, bool, error) {
	if err := validateProviderCapacityLease(lease); err != nil || duration <= 0 || duration > maxProviderCapacityLeaseDuration {
		return ProviderCapacityLease{}, false, ErrProviderCapacityConfig
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ProviderCapacityLease{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockPostgresProviderCapacity(ctx, tx, lease.ID, lease.ProviderAccountID); err != nil {
		return ProviderCapacityLease{}, false, err
	}
	now, err := postgresProviderCapacityNow(ctx, tx)
	if err != nil {
		return ProviderCapacityLease{}, false, err
	}
	current, found, err := postgresProviderCapacityLease(ctx, tx, lease.ID)
	if err != nil || !found {
		return ProviderCapacityLease{}, false, err
	}
	if current.ProviderAccountID != lease.ProviderAccountID || current.CapacityUnits != lease.CapacityUnits {
		return ProviderCapacityLease{}, false, ErrProviderCapacityConflict
	}
	if !current.ExpiresAt.After(now) {
		if _, err := tx.ExecContext(ctx, `DELETE FROM gateway_provider_capacity_leases WHERE id=$1`, current.ID); err != nil {
			return ProviderCapacityLease{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return ProviderCapacityLease{}, false, err
		}
		return ProviderCapacityLease{}, false, nil
	}
	requestedExpiry := now.Add(duration)
	if requestedExpiry.After(current.ExpiresAt) {
		current.ExpiresAt = requestedExpiry
		if _, err := tx.ExecContext(ctx, `UPDATE gateway_provider_capacity_leases SET expires_at=$2 WHERE id=$1`, current.ID, current.ExpiresAt); err != nil {
			return ProviderCapacityLease{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ProviderCapacityLease{}, false, err
	}
	return current, true, nil
}

func (r *PostgresRepository) Restore(ctx context.Context, lease ProviderCapacityLease, duration time.Duration) (ProviderCapacityLease, error) {
	if err := validateProviderCapacityLease(lease); err != nil || duration <= 0 || duration > maxProviderCapacityLeaseDuration {
		return ProviderCapacityLease{}, ErrProviderCapacityConfig
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ProviderCapacityLease{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockPostgresProviderCapacity(ctx, tx, lease.ID, lease.ProviderAccountID); err != nil {
		return ProviderCapacityLease{}, err
	}
	now, err := postgresProviderCapacityNow(ctx, tx)
	if err != nil {
		return ProviderCapacityLease{}, err
	}
	current, found, err := postgresProviderCapacityLease(ctx, tx, lease.ID)
	if err != nil {
		return ProviderCapacityLease{}, err
	}
	if found && (current.ProviderAccountID != lease.ProviderAccountID || current.CapacityUnits != lease.CapacityUnits) {
		return ProviderCapacityLease{}, ErrProviderCapacityConflict
	}
	lease.ExpiresAt = now.Add(duration)
	if found {
		_, err = tx.ExecContext(ctx, `UPDATE gateway_provider_capacity_leases SET expires_at=$2 WHERE id=$1`, lease.ID, lease.ExpiresAt)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO gateway_provider_capacity_leases(id, provider_account_id, capacity_units, expires_at, created_at) VALUES($1,$2,$3,$4,$5)`, lease.ID, lease.ProviderAccountID, lease.CapacityUnits, lease.ExpiresAt, now)
	}
	if err != nil {
		return ProviderCapacityLease{}, err
	}
	if err := tx.Commit(); err != nil {
		return ProviderCapacityLease{}, err
	}
	return lease, nil
}

func (r *PostgresRepository) Release(ctx context.Context, lease ProviderCapacityLease) error {
	if err := validateProviderCapacityLease(lease); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockPostgresProviderCapacity(ctx, tx, lease.ID, lease.ProviderAccountID); err != nil {
		return err
	}
	current, found, err := postgresProviderCapacityLease(ctx, tx, lease.ID)
	if err != nil {
		return err
	}
	if found {
		if current.ProviderAccountID != lease.ProviderAccountID || current.CapacityUnits != lease.CapacityUnits {
			return ErrProviderCapacityConflict
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM gateway_provider_capacity_leases WHERE id=$1`, lease.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *PostgresRepository) Snapshot(ctx context.Context, providerAccountID string) (ProviderCapacitySnapshot, error) {
	providerAccountID = strings.TrimSpace(providerAccountID)
	if providerAccountID == "" {
		return ProviderCapacitySnapshot{}, ErrProviderCapacityConfig
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ProviderCapacitySnapshot{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockPostgresProviderCapacityAccount(ctx, tx, providerAccountID); err != nil {
		return ProviderCapacitySnapshot{}, err
	}
	now, err := postgresProviderCapacityNow(ctx, tx)
	if err != nil {
		return ProviderCapacitySnapshot{}, err
	}
	if err := prunePostgresProviderCapacity(ctx, tx, providerAccountID, now); err != nil {
		return ProviderCapacitySnapshot{}, err
	}
	snapshot, err := postgresProviderCapacitySnapshot(ctx, tx, providerAccountID)
	if err != nil {
		return ProviderCapacitySnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return ProviderCapacitySnapshot{}, err
	}
	return snapshot, nil
}

func lockPostgresProviderCapacity(ctx context.Context, tx *sql.Tx, leaseID, providerAccountID string) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "provider-capacity:lease:"+strings.TrimSpace(leaseID)); err != nil {
		return err
	}
	return lockPostgresProviderCapacityAccount(ctx, tx, providerAccountID)
}

func lockPostgresProviderCapacityAccount(ctx context.Context, tx *sql.Tx, providerAccountID string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "provider-capacity:account:"+strings.TrimSpace(providerAccountID))
	return err
}

func postgresProviderCapacityNow(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	var now time.Time
	err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return now.UTC(), err
}

func postgresProviderCapacityLease(ctx context.Context, tx *sql.Tx, leaseID string) (ProviderCapacityLease, bool, error) {
	var lease ProviderCapacityLease
	err := tx.QueryRowContext(ctx, `SELECT id, provider_account_id, capacity_units, expires_at FROM gateway_provider_capacity_leases WHERE id=$1`, leaseID).
		Scan(&lease.ID, &lease.ProviderAccountID, &lease.CapacityUnits, &lease.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderCapacityLease{}, false, nil
	}
	return lease, err == nil, err
}

func prunePostgresProviderCapacity(ctx context.Context, tx *sql.Tx, providerAccountID string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM gateway_provider_capacity_leases WHERE provider_account_id=$1 AND expires_at<=$2`, providerAccountID, now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM gateway_provider_rate_samples WHERE provider_account_id=$1 AND occurred_at<=$2`, providerAccountID, now.Add(-providerCapacityRateWindow))
	return err
}

func postgresProviderCapacitySnapshot(ctx context.Context, tx *sql.Tx, providerAccountID string) (ProviderCapacitySnapshot, error) {
	var snapshot ProviderCapacitySnapshot
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(capacity_units), 0) FROM gateway_provider_capacity_leases WHERE provider_account_id=$1`, providerAccountID).Scan(&snapshot.CapacityUnits); err != nil {
		return ProviderCapacitySnapshot{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(estimated_tokens), 0) FROM gateway_provider_rate_samples WHERE provider_account_id=$1`, providerAccountID).Scan(&snapshot.Requests, &snapshot.Tokens); err != nil {
		return ProviderCapacitySnapshot{}, err
	}
	return snapshot, nil
}

var _ ProviderCapacityStore = (*PostgresRepository)(nil)
