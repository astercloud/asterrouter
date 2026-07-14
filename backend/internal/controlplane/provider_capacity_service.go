package controlplane

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ProviderAccountPermit struct {
	state *providerAccountPermitState
}

type providerAccountPermitState struct {
	mu              sync.Mutex
	closed          bool
	store           ProviderCapacityStore
	lease           ProviderCapacityLease
	heartbeatCancel context.CancelFunc
	localRelease    func()
	circuitRelease  func()
}

func (s *Service) SetProviderCapacityStore(store ProviderCapacityStore) {
	if store == nil {
		return
	}
	s.providerCapacityMu.Lock()
	defer s.providerCapacityMu.Unlock()
	s.providerCapacityStore = store
}

func (s *Service) currentProviderCapacityStore() ProviderCapacityStore {
	s.providerCapacityMu.RLock()
	defer s.providerCapacityMu.RUnlock()
	return s.providerCapacityStore
}

func (s *Service) TryAcquireProviderAccountPermitContext(ctx context.Context, provider GatewayProvider, estimatedTokens int, leaseID string) (ProviderAccountPermit, string, bool, error) {
	provider.AccountID = strings.TrimSpace(provider.AccountID)
	if provider.AccountID == "" {
		return ProviderAccountPermit{}, "provider_account_missing", false, ErrProviderCapacityConfig
	}
	circuitRelease, reason, acquired := s.tryAcquireProviderCircuitPermit(provider)
	if !acquired {
		return ProviderAccountPermit{}, reason, false, nil
	}
	if provider.Concurrency <= 0 && provider.RPMLimit <= 0 && provider.TPMLimit <= 0 {
		return ProviderAccountPermit{state: &providerAccountPermitState{circuitRelease: circuitRelease}}, "", true, nil
	}
	store := s.currentProviderCapacityStore()
	if store == nil {
		circuitRelease()
		return ProviderAccountPermit{}, "capacity_store_unavailable", false, errors.New("provider capacity store is not available")
	}
	if strings.TrimSpace(leaseID) == "" {
		leaseID = "provider_lease_" + randomID(12)
	}
	request := ProviderCapacityRequest{
		LeaseID: strings.TrimSpace(leaseID), ProviderAccountID: provider.AccountID, CapacityUnits: 1,
		ConcurrencyLimit: provider.Concurrency, RPMLimit: provider.RPMLimit, TPMLimit: provider.TPMLimit,
		EstimatedTokens: nonNegative(estimatedTokens), LeaseDuration: providerCapacityLeaseTTL,
	}
	lease, reason, acquired, err := store.Acquire(ctx, request)
	if err != nil || !acquired {
		circuitRelease()
		if reason == "concurrency_exhausted" {
			reason = "at_capacity"
		}
		return ProviderAccountPermit{}, reason, acquired, err
	}
	localRelease := s.trackProviderAccountSlot(provider.AccountID)
	if provider.RPMLimit > 0 || provider.TPMLimit > 0 {
		s.recordProviderCapacitySample(provider.AccountID, request.EstimatedTokens)
	}
	state := &providerAccountPermitState{
		store: store, lease: lease, localRelease: localRelease, circuitRelease: circuitRelease,
	}
	state.startHeartbeat(providerCapacityLeaseTTL)
	return ProviderAccountPermit{state: state}, "", true, nil
}

func (s *Service) tryAcquireProviderCircuitPermit(provider GatewayProvider) (func(), string, bool) {
	if provider.CircuitState == CircuitStateOpen && !provider.CircuitProbe {
		return func() {}, "circuit_open", false
	}
	if s.scheduler == nil || !provider.CircuitProbe {
		return func() {}, "", true
	}
	s.scheduler.mu.Lock()
	defer s.scheduler.mu.Unlock()
	if s.scheduler.halfOpenProbes[provider.AccountID] {
		return func() {}, "circuit_half_open_busy", false
	}
	s.scheduler.halfOpenProbes[provider.AccountID] = true
	var once sync.Once
	return func() {
		once.Do(func() {
			s.scheduler.mu.Lock()
			defer s.scheduler.mu.Unlock()
			delete(s.scheduler.halfOpenProbes, provider.AccountID)
		})
	}, "", true
}

func (s *Service) recordProviderCapacitySample(accountID string, estimatedTokens int) {
	if s.scheduler == nil {
		return
	}
	now := s.nowUTC()
	s.scheduler.mu.Lock()
	defer s.scheduler.mu.Unlock()
	samples := s.scheduler.pruneSamples(accountID, now)
	s.scheduler.rateSamples[accountID] = append(samples, gatewayRateSample{at: now, tokens: estimatedTokens})
}

func (permit ProviderAccountPermit) Release() {
	if permit.state == nil {
		return
	}
	permit.state.close(true)
}

// Retain keeps the distributed lease for an accepted or ambiguous durable
// provider task while stopping the dispatch heartbeat owned by this worker.
func (permit ProviderAccountPermit) Retain(ctx context.Context, duration time.Duration) error {
	if permit.state == nil {
		return nil
	}
	state := permit.state
	state.mu.Lock()
	if state.closed {
		state.mu.Unlock()
		return nil
	}
	state.closed = true
	if state.heartbeatCancel != nil {
		state.heartbeatCancel()
	}
	lease := state.lease
	store := state.store
	state.mu.Unlock()
	defer state.releaseLocal()
	if store == nil {
		return nil
	}
	extended, found, err := store.Extend(ctx, lease, duration)
	if err != nil {
		return err
	}
	if !found {
		_, err = store.Restore(ctx, lease, duration)
		return err
	}
	state.mu.Lock()
	state.lease = extended
	state.mu.Unlock()
	return nil
}

func (state *providerAccountPermitState) startHeartbeat(duration time.Duration) {
	if state == nil || state.store == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	state.heartbeatCancel = cancel
	interval := duration / 3
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				state.extend(ctx, duration)
			}
		}
	}()
}

func (state *providerAccountPermitState) extend(ctx context.Context, duration time.Duration) {
	state.mu.Lock()
	if state.closed {
		state.mu.Unlock()
		return
	}
	lease := state.lease
	store := state.store
	state.mu.Unlock()
	if store == nil {
		return
	}
	extended, found, err := store.Extend(ctx, lease, duration)
	if err != nil || !found {
		return
	}
	state.mu.Lock()
	if !state.closed {
		state.lease = extended
	}
	state.mu.Unlock()
}

func (state *providerAccountPermitState) close(releaseStore bool) {
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
	if releaseStore && store != nil {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = store.Release(releaseCtx, lease)
		cancel()
	}
	state.releaseLocal()
}

func (state *providerAccountPermitState) releaseLocal() {
	if state.localRelease != nil {
		state.localRelease()
	}
	if state.circuitRelease != nil {
		state.circuitRelease()
	}
}

func providerCapacityRetentionDuration(now time.Time, reconcileAfter *time.Time) time.Duration {
	duration := providerCapacityLeaseTTL
	if reconcileAfter != nil && reconcileAfter.After(now) {
		duration = reconcileAfter.Sub(now) + providerCapacityLeaseGrace
	}
	if duration > maxProviderCapacityLeaseDuration {
		return maxProviderCapacityLeaseDuration
	}
	return duration
}

func providerCapacityLeaseForAttempt(attempt AIAttempt) ProviderCapacityLease {
	return ProviderCapacityLease{
		ID: providerCapacityLeaseID(attempt.OperationID, attempt.AttemptNumber), ProviderAccountID: strings.TrimSpace(attempt.ProviderAccountID), CapacityUnits: 1,
	}
}

func providerCapacityLeaseID(operationID string, attemptNumber int) string {
	return "provider_lease_" + strings.TrimSpace(operationID) + "_" + strconv.Itoa(attemptNumber)
}
