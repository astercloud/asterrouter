package controlplane

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrProviderCapacityConfig   = errors.New("invalid provider capacity configuration")
	ErrProviderCapacityConflict = errors.New("provider capacity lease conflicts with existing state")
)

const (
	providerCapacityRateWindow = time.Minute
	providerCapacityLeaseTTL   = 2 * time.Minute
	providerCapacityLeaseGrace = 2 * time.Minute
)

type ProviderCapacityRequest struct {
	LeaseID           string
	ProviderAccountID string
	CapacityUnits     int
	ConcurrencyLimit  int
	RPMLimit          int
	TPMLimit          int
	EstimatedTokens   int
	LeaseDuration     time.Duration
}

type ProviderCapacityLease struct {
	ID                string
	ProviderAccountID string
	CapacityUnits     int
	ExpiresAt         time.Time
}

type ProviderCapacitySnapshot struct {
	CapacityUnits int
	Requests      int
	Tokens        int
}

// ProviderCapacityStore owns ephemeral provider concurrency and rate state.
// Durable Attempt state remains authoritative and can restore a lost lease.
type ProviderCapacityStore interface {
	Acquire(context.Context, ProviderCapacityRequest) (ProviderCapacityLease, string, bool, error)
	Extend(context.Context, ProviderCapacityLease, time.Duration) (ProviderCapacityLease, bool, error)
	Restore(context.Context, ProviderCapacityLease, time.Duration) (ProviderCapacityLease, error)
	Release(context.Context, ProviderCapacityLease) error
	Snapshot(context.Context, string) (ProviderCapacitySnapshot, error)
}

type providerCapacityRateSample struct {
	LeaseID         string
	At              time.Time
	EstimatedTokens int
}

type MemoryProviderCapacityStore struct {
	mu      sync.Mutex
	now     func() time.Time
	leases  map[string]ProviderCapacityLease
	samples map[string][]providerCapacityRateSample
}

func NewMemoryProviderCapacityStore() *MemoryProviderCapacityStore {
	return &MemoryProviderCapacityStore{
		now: time.Now, leases: map[string]ProviderCapacityLease{}, samples: map[string][]providerCapacityRateSample{},
	}
}

var _ ProviderCapacityStore = (*MemoryProviderCapacityStore)(nil)

func validateProviderCapacityRequest(request ProviderCapacityRequest) error {
	if strings.TrimSpace(request.LeaseID) == "" || strings.TrimSpace(request.ProviderAccountID) == "" || request.CapacityUnits <= 0 || request.LeaseDuration <= 0 ||
		request.ConcurrencyLimit < 0 || request.RPMLimit < 0 || request.TPMLimit < 0 || request.EstimatedTokens < 0 {
		return ErrProviderCapacityConfig
	}
	return nil
}

func validateProviderCapacityLease(lease ProviderCapacityLease) error {
	if strings.TrimSpace(lease.ID) == "" || strings.TrimSpace(lease.ProviderAccountID) == "" || lease.CapacityUnits <= 0 {
		return ErrProviderCapacityConfig
	}
	return nil
}

func (store *MemoryProviderCapacityStore) Acquire(_ context.Context, request ProviderCapacityRequest) (ProviderCapacityLease, string, bool, error) {
	if err := validateProviderCapacityRequest(request); err != nil {
		return ProviderCapacityLease{}, "", false, err
	}
	request.LeaseID = strings.TrimSpace(request.LeaseID)
	request.ProviderAccountID = strings.TrimSpace(request.ProviderAccountID)
	now := store.nowUTC()
	store.mu.Lock()
	defer store.mu.Unlock()
	store.pruneLocked(request.ProviderAccountID, now)
	if current, found := store.leases[request.LeaseID]; found {
		if current.ProviderAccountID != request.ProviderAccountID || current.CapacityUnits != request.CapacityUnits {
			return ProviderCapacityLease{}, "", false, ErrProviderCapacityConflict
		}
		requestedExpiry := now.Add(request.LeaseDuration)
		if requestedExpiry.After(current.ExpiresAt) {
			current.ExpiresAt = requestedExpiry
			store.leases[current.ID] = current
		}
		return current, "", true, nil
	}
	snapshot := store.snapshotLocked(request.ProviderAccountID)
	switch {
	case request.ConcurrencyLimit > 0 && snapshot.CapacityUnits+request.CapacityUnits > request.ConcurrencyLimit:
		return ProviderCapacityLease{}, "concurrency_exhausted", false, nil
	case request.RPMLimit > 0 && snapshot.Requests >= request.RPMLimit:
		return ProviderCapacityLease{}, "rpm_exhausted", false, nil
	case request.TPMLimit > 0 && snapshot.Tokens+request.EstimatedTokens > request.TPMLimit:
		return ProviderCapacityLease{}, "tpm_exhausted", false, nil
	}
	lease := ProviderCapacityLease{
		ID: request.LeaseID, ProviderAccountID: request.ProviderAccountID, CapacityUnits: request.CapacityUnits,
		ExpiresAt: now.Add(request.LeaseDuration),
	}
	store.leases[lease.ID] = lease
	if request.RPMLimit > 0 || request.TPMLimit > 0 {
		store.samples[request.ProviderAccountID] = append(store.samples[request.ProviderAccountID], providerCapacityRateSample{
			LeaseID: lease.ID, At: now, EstimatedTokens: request.EstimatedTokens,
		})
	}
	return lease, "", true, nil
}

func (store *MemoryProviderCapacityStore) Extend(_ context.Context, lease ProviderCapacityLease, duration time.Duration) (ProviderCapacityLease, bool, error) {
	if err := validateProviderCapacityLease(lease); err != nil || duration <= 0 {
		return ProviderCapacityLease{}, false, ErrProviderCapacityConfig
	}
	now := store.nowUTC()
	store.mu.Lock()
	defer store.mu.Unlock()
	store.pruneLocked(lease.ProviderAccountID, now)
	current, found := store.leases[lease.ID]
	if !found || current.ProviderAccountID != lease.ProviderAccountID || current.CapacityUnits != lease.CapacityUnits {
		return current, false, nil
	}
	expiresAt := now.Add(duration)
	if expiresAt.After(current.ExpiresAt) {
		current.ExpiresAt = expiresAt
		store.leases[current.ID] = current
	}
	return current, true, nil
}

func (store *MemoryProviderCapacityStore) Restore(_ context.Context, lease ProviderCapacityLease, duration time.Duration) (ProviderCapacityLease, error) {
	if err := validateProviderCapacityLease(lease); err != nil || duration <= 0 {
		return ProviderCapacityLease{}, ErrProviderCapacityConfig
	}
	now := store.nowUTC()
	store.mu.Lock()
	defer store.mu.Unlock()
	store.pruneLocked(lease.ProviderAccountID, now)
	if current, found := store.leases[lease.ID]; found && (current.ProviderAccountID != lease.ProviderAccountID || current.CapacityUnits != lease.CapacityUnits) {
		return ProviderCapacityLease{}, ErrProviderCapacityConflict
	}
	lease.ExpiresAt = now.Add(duration)
	store.leases[lease.ID] = lease
	return lease, nil
}

func (store *MemoryProviderCapacityStore) Release(_ context.Context, lease ProviderCapacityLease) error {
	if err := validateProviderCapacityLease(lease); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if current, found := store.leases[lease.ID]; found {
		if current.ProviderAccountID != lease.ProviderAccountID || current.CapacityUnits != lease.CapacityUnits {
			return ErrProviderCapacityConflict
		}
		delete(store.leases, lease.ID)
	}
	return nil
}

func (store *MemoryProviderCapacityStore) Snapshot(_ context.Context, providerAccountID string) (ProviderCapacitySnapshot, error) {
	providerAccountID = strings.TrimSpace(providerAccountID)
	if providerAccountID == "" {
		return ProviderCapacitySnapshot{}, ErrProviderCapacityConfig
	}
	now := store.nowUTC()
	store.mu.Lock()
	defer store.mu.Unlock()
	store.pruneLocked(providerAccountID, now)
	return store.snapshotLocked(providerAccountID), nil
}

func (store *MemoryProviderCapacityStore) pruneLocked(providerAccountID string, now time.Time) {
	for id, lease := range store.leases {
		if lease.ProviderAccountID == providerAccountID && !lease.ExpiresAt.After(now) {
			delete(store.leases, id)
		}
	}
	cutoff := now.Add(-providerCapacityRateWindow)
	samples := store.samples[providerAccountID]
	kept := samples[:0]
	for _, sample := range samples {
		if sample.At.After(cutoff) {
			kept = append(kept, sample)
		}
	}
	store.samples[providerAccountID] = kept
}

func (store *MemoryProviderCapacityStore) snapshotLocked(providerAccountID string) ProviderCapacitySnapshot {
	var snapshot ProviderCapacitySnapshot
	for _, lease := range store.leases {
		if lease.ProviderAccountID == providerAccountID {
			snapshot.CapacityUnits += lease.CapacityUnits
		}
	}
	for _, sample := range store.samples[providerAccountID] {
		snapshot.Requests++
		snapshot.Tokens += sample.EstimatedTokens
	}
	return snapshot
}

func (store *MemoryProviderCapacityStore) nowUTC() time.Time {
	if store != nil && store.now != nil {
		return store.now().UTC()
	}
	return time.Now().UTC()
}
