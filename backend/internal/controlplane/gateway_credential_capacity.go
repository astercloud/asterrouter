package controlplane

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

const (
	gatewayCredentialRateWindow = time.Minute
	gatewayCredentialQPSWindow  = time.Second
	gatewayCredentialLeaseTTL   = 10 * time.Minute
)

type CredentialCapacityRequest struct {
	LeaseID          string
	ProfileScope     string
	TenantID         string
	CredentialID     string
	QPSLimit         int
	RPMLimit         int
	TPMLimit         int
	ConcurrencyLimit int
	EstimatedTokens  int
	Now              time.Time
	LeaseUntil       time.Time
}

type CredentialCapacityLease struct {
	ID           string
	ProfileScope string
	TenantID     string
	CredentialID string
	ExpiresAt    time.Time
}

type credentialRateSample struct {
	At              time.Time
	EstimatedTokens int
}

type CredentialCapacityStore interface {
	AcquireCredentialCapacity(context.Context, CredentialCapacityRequest) (CredentialCapacityLease, string, bool, error)
	ExtendCredentialCapacity(context.Context, CredentialCapacityLease, time.Time, time.Time) (CredentialCapacityLease, bool, error)
	ReleaseCredentialCapacity(context.Context, CredentialCapacityLease) error
}

type GatewayCredentialPermit struct {
	state *gatewayCredentialPermitState
	once  sync.Once
}

type gatewayCredentialPermitState struct {
	mu              sync.Mutex
	closed          bool
	store           CredentialCapacityStore
	lease           CredentialCapacityLease
	heartbeatCancel context.CancelFunc
	lost            chan error
	lostOnce        sync.Once
}

func (s *Service) SetCredentialCapacityStore(store CredentialCapacityStore) {
	if store != nil {
		s.credentialCapacityStore = store
	}
}

func (p *GatewayCredentialPermit) Release() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		if p.state != nil {
			p.state.close()
		}
	})
}

// Lost reports an authoritative capacity lease failure. Callers with a
// long-lived session should terminate the session when this channel receives.
func (p *GatewayCredentialPermit) Lost() <-chan error {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.lost
}

func (s *Service) TryAcquireGatewayCredentialPermit(ctx context.Context, auth gatewaycore.CanonicalAuthContext, estimatedTokens int) (*GatewayCredentialPermit, string, bool, error) {
	if strings.TrimSpace(auth.CredentialID) == "" {
		return nil, "credential_missing", false, ErrGatewayUnauthorized
	}
	if auth.Limits.QPSLimit <= 0 && auth.Limits.RPMLimit <= 0 && auth.Limits.TPMLimit <= 0 && auth.Limits.ConcurrencyLimit <= 0 {
		return &GatewayCredentialPermit{}, "", true, nil
	}
	if s.credentialCapacityStore == nil {
		if auth.Limits.QPSLimit > 0 || auth.Limits.RPMLimit > 0 || auth.Limits.TPMLimit > 0 || auth.Limits.ConcurrencyLimit > 0 {
			return nil, "capacity_store_unavailable", false, errors.New("gateway credential capacity store is not available")
		}
		return &GatewayCredentialPermit{}, "", true, nil
	}
	now := s.nowUTC()
	request := CredentialCapacityRequest{
		LeaseID: "credential_lease_" + randomID(12), ProfileScope: auth.ProfileScope, TenantID: auth.TenantID,
		CredentialID: auth.CredentialID, QPSLimit: auth.Limits.QPSLimit, RPMLimit: auth.Limits.RPMLimit, TPMLimit: auth.Limits.TPMLimit,
		ConcurrencyLimit: auth.Limits.ConcurrencyLimit, EstimatedTokens: nonNegative(estimatedTokens), Now: now, LeaseUntil: now.Add(gatewayCredentialLeaseTTL),
	}
	lease, reason, acquired, err := s.credentialCapacityStore.AcquireCredentialCapacity(ctx, request)
	if err != nil || !acquired {
		return nil, reason, acquired, err
	}
	state := &gatewayCredentialPermitState{store: s.credentialCapacityStore, lease: lease, lost: make(chan error, 1)}
	state.startHeartbeat(s.nowUTC, gatewayCredentialLeaseTTL)
	permit := &GatewayCredentialPermit{state: state}
	return permit, "", true, nil
}

func (state *gatewayCredentialPermitState) startHeartbeat(now func() time.Time, duration time.Duration) {
	if state == nil || state.store == nil || duration <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	state.heartbeatCancel = cancel
	interval := duration / 3
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := state.extend(ctx, now, duration); err != nil {
					state.signalLost(err)
				}
			}
		}
	}()
}

func (state *gatewayCredentialPermitState) extend(ctx context.Context, now func() time.Time, duration time.Duration) error {
	state.mu.Lock()
	if state.closed {
		state.mu.Unlock()
		return nil
	}
	lease := state.lease
	store := state.store
	state.mu.Unlock()
	if store == nil {
		return errors.New("gateway credential capacity store is not available")
	}
	extendedAt := now().UTC()
	extended, found, err := store.ExtendCredentialCapacity(ctx, lease, extendedAt, extendedAt.Add(duration))
	if err != nil {
		return err
	}
	if !found {
		return errors.New("gateway credential capacity lease was lost")
	}
	state.mu.Lock()
	if !state.closed {
		state.lease = extended
	}
	state.mu.Unlock()
	return nil
}

func (state *gatewayCredentialPermitState) signalLost(err error) {
	if state == nil || err == nil {
		return
	}
	state.lostOnce.Do(func() { state.lost <- err })
}

func (state *gatewayCredentialPermitState) close() {
	if state == nil {
		return
	}
	state.mu.Lock()
	if state.closed {
		state.mu.Unlock()
		return
	}
	state.closed = true
	if state.heartbeatCancel != nil {
		state.heartbeatCancel()
	}
	lease := state.lease
	store := state.store
	state.mu.Unlock()
	if store != nil {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = store.ReleaseCredentialCapacity(releaseCtx, lease)
		cancel()
	}
}

func credentialCapacityKey(profileScope, tenantID, credentialID string) string {
	profileScope = strings.TrimSpace(profileScope)
	tenantID = strings.TrimSpace(tenantID)
	credentialID = strings.TrimSpace(credentialID)
	return strconv.Itoa(len(profileScope)) + ":" + profileScope + strconv.Itoa(len(tenantID)) + ":" + tenantID + strconv.Itoa(len(credentialID)) + ":" + credentialID
}

func validateCredentialCapacityRequest(request CredentialCapacityRequest) error {
	if strings.TrimSpace(request.LeaseID) == "" || strings.TrimSpace(request.CredentialID) == "" || request.Now.IsZero() || !request.LeaseUntil.After(request.Now) {
		return errors.New("invalid credential capacity request")
	}
	if request.QPSLimit < 0 || request.RPMLimit < 0 || request.TPMLimit < 0 || request.ConcurrencyLimit < 0 || request.EstimatedTokens < 0 {
		return errors.New("credential capacity limits must be non-negative")
	}
	return nil
}

func (r *MemoryRepository) AcquireCredentialCapacity(_ context.Context, request CredentialCapacityRequest) (CredentialCapacityLease, string, bool, error) {
	if err := validateCredentialCapacityRequest(request); err != nil {
		return CredentialCapacityLease{}, "", false, err
	}
	key := credentialCapacityKey(request.ProfileScope, request.TenantID, request.CredentialID)
	r.mu.Lock()
	defer r.mu.Unlock()
	samples := r.credentialRateSamples[key]
	minuteCutoff := request.Now.Add(-gatewayCredentialRateWindow)
	keptSamples := samples[:0]
	qpsCount := 0
	tokens := 0
	for _, sample := range samples {
		if sample.At.After(minuteCutoff) {
			keptSamples = append(keptSamples, sample)
			tokens += sample.EstimatedTokens
			if sample.At.After(request.Now.Add(-gatewayCredentialQPSWindow)) {
				qpsCount++
			}
		}
	}
	r.credentialRateSamples[key] = keptSamples
	concurrency := 0
	for id, lease := range r.credentialCapacityLeases {
		if !lease.ExpiresAt.After(request.Now) {
			delete(r.credentialCapacityLeases, id)
			continue
		}
		if credentialCapacityKey(lease.ProfileScope, lease.TenantID, lease.CredentialID) == key {
			concurrency++
		}
	}
	if request.ConcurrencyLimit > 0 && concurrency >= request.ConcurrencyLimit {
		return CredentialCapacityLease{}, "concurrency_exhausted", false, nil
	}
	if request.QPSLimit > 0 && qpsCount >= request.QPSLimit {
		return CredentialCapacityLease{}, "qps_exhausted", false, nil
	}
	if request.RPMLimit > 0 && len(keptSamples) >= request.RPMLimit {
		return CredentialCapacityLease{}, "rpm_exhausted", false, nil
	}
	if request.TPMLimit > 0 && tokens+request.EstimatedTokens > request.TPMLimit {
		return CredentialCapacityLease{}, "tpm_exhausted", false, nil
	}
	lease := CredentialCapacityLease{ID: request.LeaseID, ProfileScope: request.ProfileScope, TenantID: request.TenantID, CredentialID: request.CredentialID, ExpiresAt: request.LeaseUntil}
	r.credentialRateSamples[key] = append(keptSamples, credentialRateSample{At: request.Now, EstimatedTokens: request.EstimatedTokens})
	r.credentialCapacityLeases[lease.ID] = lease
	return lease, "", true, nil
}

func (r *MemoryRepository) ReleaseCredentialCapacity(_ context.Context, lease CredentialCapacityLease) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.credentialCapacityLeases, lease.ID)
	return nil
}

func (r *MemoryRepository) ExtendCredentialCapacity(_ context.Context, lease CredentialCapacityLease, now, leaseUntil time.Time) (CredentialCapacityLease, bool, error) {
	if strings.TrimSpace(lease.ID) == "" || now.IsZero() || !leaseUntil.After(now) {
		return CredentialCapacityLease{}, false, errors.New("invalid credential capacity lease extension")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, found := r.credentialCapacityLeases[lease.ID]
	if !found || !current.ExpiresAt.After(now) || credentialCapacityKey(current.ProfileScope, current.TenantID, current.CredentialID) != credentialCapacityKey(lease.ProfileScope, lease.TenantID, lease.CredentialID) {
		return CredentialCapacityLease{}, false, nil
	}
	if leaseUntil.After(current.ExpiresAt) {
		current.ExpiresAt = leaseUntil
		r.credentialCapacityLeases[current.ID] = current
	}
	return current, true, nil
}

func (r *PostgresRepository) AcquireCredentialCapacity(ctx context.Context, request CredentialCapacityRequest) (CredentialCapacityLease, string, bool, error) {
	if err := validateCredentialCapacityRequest(request); err != nil {
		return CredentialCapacityLease{}, "", false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return CredentialCapacityLease{}, "", false, err
	}
	defer func() { _ = tx.Rollback() }()
	key := credentialCapacityKey(request.ProfileScope, request.TenantID, request.CredentialID)
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
		return CredentialCapacityLease{}, "", false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM gateway_credential_capacity_leases WHERE profile_scope=$1 AND tenant_id=$2 AND credential_id=$3 AND expires_at<=$4`, request.ProfileScope, request.TenantID, request.CredentialID, request.Now); err != nil {
		return CredentialCapacityLease{}, "", false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM gateway_credential_rate_samples WHERE profile_scope=$1 AND tenant_id=$2 AND credential_id=$3 AND occurred_at<=$4`, request.ProfileScope, request.TenantID, request.CredentialID, request.Now.Add(-gatewayCredentialRateWindow)); err != nil {
		return CredentialCapacityLease{}, "", false, err
	}
	var concurrency, qps, rpm, tokens int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM gateway_credential_capacity_leases WHERE profile_scope=$1 AND tenant_id=$2 AND credential_id=$3 AND expires_at>$4`, request.ProfileScope, request.TenantID, request.CredentialID, request.Now).Scan(&concurrency); err != nil {
		return CredentialCapacityLease{}, "", false, err
	}
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE occurred_at>$4), COUNT(*), COALESCE(SUM(estimated_tokens), 0)
FROM gateway_credential_rate_samples
WHERE profile_scope=$1 AND tenant_id=$2 AND credential_id=$3 AND occurred_at>$5
`, request.ProfileScope, request.TenantID, request.CredentialID, request.Now.Add(-gatewayCredentialQPSWindow), request.Now.Add(-gatewayCredentialRateWindow)).Scan(&qps, &rpm, &tokens); err != nil {
		return CredentialCapacityLease{}, "", false, err
	}
	reason := ""
	switch {
	case request.ConcurrencyLimit > 0 && concurrency >= request.ConcurrencyLimit:
		reason = "concurrency_exhausted"
	case request.QPSLimit > 0 && qps >= request.QPSLimit:
		reason = "qps_exhausted"
	case request.RPMLimit > 0 && rpm >= request.RPMLimit:
		reason = "rpm_exhausted"
	case request.TPMLimit > 0 && tokens+request.EstimatedTokens > request.TPMLimit:
		reason = "tpm_exhausted"
	}
	if reason != "" {
		if err := tx.Commit(); err != nil {
			return CredentialCapacityLease{}, "", false, err
		}
		return CredentialCapacityLease{}, reason, false, nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO gateway_credential_rate_samples(id, profile_scope, tenant_id, credential_id, estimated_tokens, occurred_at) VALUES($1,$2,$3,$4,$5,$6)`, "credential_sample_"+randomID(12), request.ProfileScope, request.TenantID, request.CredentialID, request.EstimatedTokens, request.Now); err != nil {
		return CredentialCapacityLease{}, "", false, err
	}
	lease := CredentialCapacityLease{ID: request.LeaseID, ProfileScope: request.ProfileScope, TenantID: request.TenantID, CredentialID: request.CredentialID, ExpiresAt: request.LeaseUntil}
	if _, err := tx.ExecContext(ctx, `INSERT INTO gateway_credential_capacity_leases(id, profile_scope, tenant_id, credential_id, expires_at, created_at) VALUES($1,$2,$3,$4,$5,$6)`, lease.ID, lease.ProfileScope, lease.TenantID, lease.CredentialID, lease.ExpiresAt, request.Now); err != nil {
		return CredentialCapacityLease{}, "", false, err
	}
	if err := tx.Commit(); err != nil {
		return CredentialCapacityLease{}, "", false, err
	}
	return lease, "", true, nil
}

func (r *PostgresRepository) ReleaseCredentialCapacity(ctx context.Context, lease CredentialCapacityLease) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM gateway_credential_capacity_leases WHERE id=$1`, lease.ID)
	return err
}

func (r *PostgresRepository) ExtendCredentialCapacity(ctx context.Context, lease CredentialCapacityLease, now, leaseUntil time.Time) (CredentialCapacityLease, bool, error) {
	if strings.TrimSpace(lease.ID) == "" || now.IsZero() || !leaseUntil.After(now) {
		return CredentialCapacityLease{}, false, errors.New("invalid credential capacity lease extension")
	}
	result, err := r.db.ExecContext(ctx, `
UPDATE gateway_credential_capacity_leases
SET expires_at=GREATEST(expires_at, $5)
WHERE id=$1 AND profile_scope=$2 AND tenant_id=$3 AND credential_id=$4 AND expires_at>$6
`, lease.ID, lease.ProfileScope, lease.TenantID, lease.CredentialID, leaseUntil, now)
	if err != nil {
		return CredentialCapacityLease{}, false, err
	}
	updated, err := result.RowsAffected()
	if err != nil || updated == 0 {
		return CredentialCapacityLease{}, false, err
	}
	lease.ExpiresAt = leaseUntil
	return lease, true, nil
}

var _ CredentialCapacityStore = (*MemoryRepository)(nil)
var _ CredentialCapacityStore = (*PostgresRepository)(nil)
