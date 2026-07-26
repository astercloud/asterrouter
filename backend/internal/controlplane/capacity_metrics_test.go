package controlplane

import (
	"context"
	"sync"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

type capacityAdmissionRecorder struct {
	mu     sync.Mutex
	events []CapacityAdmissionEvent
}

func (r *capacityAdmissionRecorder) ObserveCapacityAdmission(event CapacityAdmissionEvent) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
}

func (r *capacityAdmissionRecorder) contains(scope, result, reason string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range r.events {
		if event.Scope == scope && event.Result == result && event.Reason == reason {
			return true
		}
	}
	return false
}

func TestCapacityMetricsObserveAdmissionsAndSnapshotProviderState(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	recorder := &capacityAdmissionRecorder{}
	svc.SetCapacityAdmissionObserver(recorder)

	first, reason, acquired, err := svc.TryAcquireGatewayCredentialPermit(ctx, gatewaycore.CanonicalAuthContext{
		ProfileScope: ProfileScopePlatform, TenantID: "tenant-1", CredentialID: "application-a",
		Limits: gatewaycore.CanonicalLimits{ConcurrencyLimit: 2, TenantConcurrencyLimit: 1},
	}, 0)
	if err != nil || !acquired || reason != "" {
		t.Fatalf("first credential admission reason=%q acquired=%t err=%v", reason, acquired, err)
	}
	defer first.Release()
	if _, reason, acquired, err := svc.TryAcquireGatewayCredentialPermit(ctx, gatewaycore.CanonicalAuthContext{
		ProfileScope: ProfileScopePlatform, TenantID: "tenant-1", CredentialID: "application-b",
		Limits: gatewaycore.CanonicalLimits{ConcurrencyLimit: 2, TenantConcurrencyLimit: 1},
	}, 0); err != nil || acquired || reason != "tenant_concurrency_exhausted" {
		t.Fatalf("second credential admission reason=%q acquired=%t err=%v", reason, acquired, err)
	}
	if !recorder.contains("application", "acquired", "") || !recorder.contains("tenant", "acquired", "") || !recorder.contains("tenant", "rejected", "tenant_concurrency_exhausted") {
		t.Fatalf("credential capacity events=%+v", recorder.events)
	}

	account := ProviderAccount{
		ID: "account-1", ProviderID: "provider-1", Name: "Primary", Status: AccountStatusActive, Schedulable: true,
		Concurrency: 2, RPMLimit: 10, TPMLimit: 100, CircuitState: CircuitStateClosed,
	}
	if err := repo.SaveProviderAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	providerPermit, reason, acquired, err := svc.TryAcquireProviderAccountPermitContext(ctx, GatewayProvider{
		AccountID: account.ID, Concurrency: account.Concurrency, RPMLimit: account.RPMLimit, TPMLimit: account.TPMLimit, CircuitState: CircuitStateClosed,
	}, 7, "provider-metrics-lease")
	if err != nil || !acquired || reason != "" {
		t.Fatalf("provider admission reason=%q acquired=%t err=%v", reason, acquired, err)
	}
	if !recorder.contains("provider_account", "acquired", "") {
		t.Fatalf("provider capacity events=%+v", recorder.events)
	}
	snapshots, err := svc.ProviderCapacityMetrics(ctx)
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("provider snapshots=%+v err=%v", snapshots, err)
	}
	snapshot := snapshots[0]
	if snapshot.ProviderAccountID != account.ID || snapshot.Current.CapacityUnits != 1 || snapshot.Current.Requests != 1 || snapshot.Current.Tokens != 7 || snapshot.ConcurrencyLimit != 2 || snapshot.RPMLimit != 10 || snapshot.TPMLimit != 100 || !snapshot.Schedulable || snapshot.CircuitOpen {
		t.Fatalf("provider snapshot=%+v", snapshot)
	}
	providerPermit.Release()
	snapshots, err = svc.ProviderCapacityMetrics(ctx)
	if err != nil || snapshots[0].Current.CapacityUnits != 0 {
		t.Fatalf("released provider snapshots=%+v err=%v", snapshots, err)
	}
}
