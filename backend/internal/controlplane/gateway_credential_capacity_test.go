package controlplane

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestCredentialCapacityStoreContract(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) CredentialCapacityStore
	}{
		{name: "memory", open: func(*testing.T) CredentialCapacityStore { return NewMemoryRepository() }},
		{name: "postgres", open: func(t *testing.T) CredentialCapacityStore {
			schema := testutil.NewPostgresSchema(t)
			repo, err := NewPostgresRepository(context.Background(), schema.URL)
			if err != nil {
				t.Fatalf("NewPostgresRepository(): %v", err)
			}
			t.Cleanup(func() { _ = repo.Close() })
			return repo
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := test.open(t)
			now := time.Date(2026, time.July, 14, 8, 0, 0, 0, time.UTC)

			concurrency := capacityRequest("concurrency", "lease-concurrency-1", now)
			concurrency.ConcurrencyLimit = 1
			lease, reason, acquired, err := store.AcquireCredentialCapacity(context.Background(), concurrency)
			if err != nil || !acquired || reason != "" {
				t.Fatalf("acquire concurrency lease=%+v reason=%q acquired=%t err=%v", lease, reason, acquired, err)
			}
			blocked := concurrency
			blocked.LeaseID = "lease-concurrency-2"
			if _, reason, acquired, err := store.AcquireCredentialCapacity(context.Background(), blocked); err != nil || acquired || reason != "concurrency_exhausted" {
				t.Fatalf("blocked concurrency reason=%q acquired=%t err=%v", reason, acquired, err)
			}
			if err := store.ReleaseCredentialCapacity(context.Background(), lease); err != nil {
				t.Fatalf("ReleaseCredentialCapacity(): %v", err)
			}
			if _, reason, acquired, err := store.AcquireCredentialCapacity(context.Background(), blocked); err != nil || !acquired || reason != "" {
				t.Fatalf("reacquire concurrency reason=%q acquired=%t err=%v", reason, acquired, err)
			}

			qps := capacityRequest("qps", "lease-qps-1", now)
			qps.QPSLimit = 1
			qpsLease, _, acquired, err := store.AcquireCredentialCapacity(context.Background(), qps)
			if err != nil || !acquired {
				t.Fatalf("acquire qps: %v acquired=%t", err, acquired)
			}
			_ = store.ReleaseCredentialCapacity(context.Background(), qpsLease)
			qps.LeaseID = "lease-qps-2"
			if _, reason, acquired, err := store.AcquireCredentialCapacity(context.Background(), qps); err != nil || acquired || reason != "qps_exhausted" {
				t.Fatalf("qps reason=%q acquired=%t err=%v", reason, acquired, err)
			}
			qps.Now = now.Add(time.Second)
			qps.LeaseUntil = qps.Now.Add(time.Minute)
			if _, reason, acquired, err := store.AcquireCredentialCapacity(context.Background(), qps); err != nil || !acquired || reason != "" {
				t.Fatalf("qps boundary reason=%q acquired=%t err=%v", reason, acquired, err)
			}

			rpm := capacityRequest("rpm", "lease-rpm-1", now)
			rpm.RPMLimit = 2
			for index := 0; index < 2; index++ {
				rpm.LeaseID = "lease-rpm-" + string(rune('1'+index))
				current, _, acquired, err := store.AcquireCredentialCapacity(context.Background(), rpm)
				if err != nil || !acquired {
					t.Fatalf("rpm acquire %d: acquired=%t err=%v", index, acquired, err)
				}
				_ = store.ReleaseCredentialCapacity(context.Background(), current)
			}
			rpm.LeaseID = "lease-rpm-3"
			if _, reason, acquired, err := store.AcquireCredentialCapacity(context.Background(), rpm); err != nil || acquired || reason != "rpm_exhausted" {
				t.Fatalf("rpm reason=%q acquired=%t err=%v", reason, acquired, err)
			}

			tpm := capacityRequest("tpm", "lease-tpm-1", now)
			tpm.TPMLimit = 10
			tpm.EstimatedTokens = 6
			tpmLease, _, acquired, err := store.AcquireCredentialCapacity(context.Background(), tpm)
			if err != nil || !acquired {
				t.Fatalf("tpm first acquired=%t err=%v", acquired, err)
			}
			_ = store.ReleaseCredentialCapacity(context.Background(), tpmLease)
			tpm.LeaseID = "lease-tpm-2"
			tpm.EstimatedTokens = 5
			if _, reason, acquired, err := store.AcquireCredentialCapacity(context.Background(), tpm); err != nil || acquired || reason != "tpm_exhausted" {
				t.Fatalf("tpm reason=%q acquired=%t err=%v", reason, acquired, err)
			}

			expired := capacityRequest("expired", "lease-expired-1", now)
			expired.ConcurrencyLimit = 1
			expired.LeaseUntil = now.Add(time.Second)
			if _, _, acquired, err := store.AcquireCredentialCapacity(context.Background(), expired); err != nil || !acquired {
				t.Fatalf("expired first acquired=%t err=%v", acquired, err)
			}
			expired.LeaseID = "lease-expired-2"
			expired.Now = now.Add(2 * time.Second)
			expired.LeaseUntil = expired.Now.Add(time.Minute)
			if _, reason, acquired, err := store.AcquireCredentialCapacity(context.Background(), expired); err != nil || !acquired || reason != "" {
				t.Fatalf("expired reacquire reason=%q acquired=%t err=%v", reason, acquired, err)
			}
		})
	}
}

func TestCredentialCapacityDoesNotOversellAcrossConcurrentInstances(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) (CredentialCapacityStore, CredentialCapacityStore)
	}{
		{name: "memory", open: func(*testing.T) (CredentialCapacityStore, CredentialCapacityStore) {
			repo := NewMemoryRepository()
			return repo, repo
		}},
		{name: "postgres", open: func(t *testing.T) (CredentialCapacityStore, CredentialCapacityStore) {
			schema := testutil.NewPostgresSchema(t)
			first, err := NewPostgresRepository(context.Background(), schema.URL)
			if err != nil {
				t.Fatalf("open first repository: %v", err)
			}
			second, err := NewPostgresRepository(context.Background(), schema.URL)
			if err != nil {
				_ = first.Close()
				t.Fatalf("open second repository: %v", err)
			}
			t.Cleanup(func() { _ = first.Close(); _ = second.Close() })
			return first, second
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first, second := test.open(t)
			now := time.Date(2026, time.July, 14, 9, 0, 0, 0, time.UTC)
			var acquired atomic.Int32
			leases := make(chan struct {
				store CredentialCapacityStore
				lease CredentialCapacityLease
			}, 20)
			var wait sync.WaitGroup
			for index := 0; index < 20; index++ {
				wait.Add(1)
				go func(index int) {
					defer wait.Done()
					store := first
					if index%2 == 1 {
						store = second
					}
					request := capacityRequest("shared", testutil.UniqueID("lease"), now)
					request.ConcurrencyLimit = 3
					lease, _, ok, err := store.AcquireCredentialCapacity(context.Background(), request)
					if err != nil {
						t.Errorf("AcquireCredentialCapacity(): %v", err)
						return
					}
					if ok {
						acquired.Add(1)
						leases <- struct {
							store CredentialCapacityStore
							lease CredentialCapacityLease
						}{store, lease}
					}
				}(index)
			}
			wait.Wait()
			close(leases)
			if acquired.Load() != 3 {
				t.Fatalf("acquired=%d, want 3", acquired.Load())
			}
			for item := range leases {
				if err := item.store.ReleaseCredentialCapacity(context.Background(), item.lease); err != nil {
					t.Errorf("ReleaseCredentialCapacity(): %v", err)
				}
			}
		})
	}
}

func capacityRequest(credentialID, leaseID string, now time.Time) CredentialCapacityRequest {
	return CredentialCapacityRequest{
		LeaseID: leaseID, ProfileScope: "platform", TenantID: "tenant-capacity", CredentialID: credentialID,
		Now: now, LeaseUntil: now.Add(time.Minute),
	}
}
