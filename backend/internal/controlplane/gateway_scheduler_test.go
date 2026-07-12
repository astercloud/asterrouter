package controlplane

import (
	"context"
	"testing"
	"time"
)

func TestProviderAccountPermitEnforcesRPMAndTPM(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	provider := GatewayProvider{AccountID: "acct_rate", RPMLimit: 2, TPMLimit: 10, CircuitState: CircuitStateClosed}

	first, reason, ok := svc.TryAcquireProviderAccountPermit(provider, 4)
	if !ok || reason != "" {
		t.Fatalf("first permit = ok:%v reason:%q", ok, reason)
	}
	first.Release()
	second, reason, ok := svc.TryAcquireProviderAccountPermit(provider, 6)
	if !ok || reason != "" {
		t.Fatalf("second permit = ok:%v reason:%q", ok, reason)
	}
	second.Release()
	if _, reason, ok := svc.TryAcquireProviderAccountPermit(provider, 1); ok || reason != "rpm_exhausted" {
		t.Fatalf("third permit = ok:%v reason:%q, want rpm_exhausted", ok, reason)
	}

	tpmOnly := GatewayProvider{AccountID: "acct_tpm", TPMLimit: 5, CircuitState: CircuitStateClosed}
	permit, _, ok := svc.TryAcquireProviderAccountPermit(tpmOnly, 4)
	if !ok {
		t.Fatal("expected first TPM permit")
	}
	permit.Release()
	if _, reason, ok := svc.TryAcquireProviderAccountPermit(tpmOnly, 2); ok || reason != "tpm_exhausted" {
		t.Fatalf("second TPM permit = ok:%v reason:%q", ok, reason)
	}
}

func TestProviderAccountCircuitOpensAndHalfOpenProbeIsExclusive(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Circuit provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1",
		Status: ProviderStatusActive, Models: []string{"upstream-model"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	account, err := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Circuit account", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Models: []string{"upstream-model"}, Secret: "account-secret",
		CircuitFailureThreshold: 2, CircuitOpenSeconds: 60,
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(): %v", err)
	}
	if err := svc.RecordProviderAccountFailure(ctx, account.ID, 500, "first"); err != nil {
		t.Fatalf("first failure: %v", err)
	}
	afterFirst, _ := svc.providerAccountByID(ctx, account.ID)
	if afterFirst.CircuitState != CircuitStateClosed || afterFirst.ConsecutiveFailures != 1 {
		t.Fatalf("after first failure: %+v", afterFirst)
	}
	if err := svc.RecordProviderAccountFailure(ctx, account.ID, 500, "second"); err != nil {
		t.Fatalf("second failure: %v", err)
	}
	opened, _ := svc.providerAccountByID(ctx, account.ID)
	if opened.CircuitState != CircuitStateOpen || opened.CircuitOpenedUntil == nil {
		t.Fatalf("circuit did not open: %+v", opened)
	}

	past := time.Now().UTC().Add(-time.Second)
	opened.CircuitOpenedUntil = &past
	opened.CooldownUntil = nil
	if err := svc.repo.SaveProviderAccount(ctx, opened); err != nil {
		t.Fatalf("save expired circuit: %v", err)
	}
	state, probe, eligible := effectiveCircuitState(opened, time.Now().UTC())
	if state != CircuitStateHalfOpen || !probe || !eligible {
		t.Fatalf("effective state = %s probe=%v eligible=%v", state, probe, eligible)
	}
	gatewayProvider := GatewayProvider{AccountID: account.ID, CircuitState: state, CircuitProbe: probe}
	permit, _, ok := svc.TryAcquireProviderAccountPermit(gatewayProvider, 0)
	if !ok {
		t.Fatal("expected first half-open probe")
	}
	if _, reason, ok := svc.TryAcquireProviderAccountPermit(gatewayProvider, 0); ok || reason != "circuit_half_open_busy" {
		t.Fatalf("second half-open probe = ok:%v reason:%q", ok, reason)
	}
	permit.Release()
	if err := svc.RecordProviderAccountSuccess(ctx, account.ID); err != nil {
		t.Fatalf("RecordProviderAccountSuccess(): %v", err)
	}
	closed, _ := svc.providerAccountByID(ctx, account.ID)
	if closed.CircuitState != CircuitStateClosed || closed.ConsecutiveFailures != 0 || closed.CircuitOpenedUntil != nil {
		t.Fatalf("circuit did not close after success: %+v", closed)
	}
}

func TestStickyGatewayCandidateIsScopedAndReused(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	candidates := []GatewayProvider{
		{RouteID: "route-a", AccountID: "acct-a", StickyEnabled: true, StickyTTLSeconds: 600, SelectionReason: "a"},
		{RouteID: "route-b", AccountID: "acct-b", StickyEnabled: true, StickyTTLSeconds: 600, SelectionReason: "b"},
	}

	svc.BindStickyGatewayCandidate("key-a", "public-model:stable", "openai_chat", "session-1", candidates[1])
	preferred := svc.PreferStickyGatewayCandidate("key-a", "public-model:stable", "openai_chat", "session-1", candidates)
	if preferred[0].RouteID != "route-b" || preferred[1].RouteID != "route-a" {
		t.Fatalf("sticky candidate not preferred: %+v", preferred)
	}
	if got := svc.PreferStickyGatewayCandidate("key-b", "public-model:stable", "openai_chat", "session-1", candidates); got[0].RouteID != "route-a" {
		t.Fatalf("sticky binding leaked across keys: %+v", got)
	}
	if got := svc.PreferStickyGatewayCandidate("key-a", "public-model:cheap", "openai_chat", "session-1", candidates); got[0].RouteID != "route-a" {
		t.Fatalf("sticky binding leaked across route groups: %+v", got)
	}

	key := stickyBindingKey("key-a", "public-model:stable", "openai_chat", "session-1")
	svc.scheduler.mu.Lock()
	binding := svc.scheduler.stickyBindings[key]
	binding.expiresAt = time.Now().UTC().Add(-time.Second)
	svc.scheduler.stickyBindings[key] = binding
	svc.scheduler.mu.Unlock()
	if got := svc.PreferStickyGatewayCandidate("key-a", "public-model:stable", "openai_chat", "session-1", candidates); got[0].RouteID != "route-a" {
		t.Fatalf("expired sticky binding still applied: %+v", got)
	}
}

func TestGatewaySimulationDoesNotConsumeRateCapacity(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Simulator provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1",
		Status: ProviderStatusActive, Models: []string{"upstream-model"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	account, err := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Simulator account", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Models: []string{"upstream-model"}, Secret: "account-secret", RPMLimit: 1, TPMLimit: 100,
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(): %v", err)
	}
	model, err := svc.CreateGatewayModel(ctx, "tester", GatewayModelRequest{ModelID: "public-model", Name: "Public", Status: GatewayModelStatusActive})
	if err != nil {
		t.Fatalf("CreateGatewayModel(): %v", err)
	}
	if _, err := svc.CreateModelRoute(ctx, "tester", ModelRouteRequest{
		GatewayModelID: model.ID, RouteGroup: "default", ProviderAccountID: account.ID,
		UpstreamModel: "upstream-model", Priority: 10, Weight: 100, Status: ModelRouteStatusActive,
	}); err != nil {
		t.Fatalf("CreateModelRoute(): %v", err)
	}

	for index := 0; index < 2; index++ {
		result, err := svc.SimulateGatewayRouting(ctx, GatewaySimulationRequest{Model: "public-model", EstimatedTokens: 10})
		if err != nil {
			t.Fatalf("SimulateGatewayRouting(%d): %v", index, err)
		}
		if result.Status != "ready" || len(result.Candidates) != 1 || !result.Candidates[0].Eligible {
			t.Fatalf("simulation %d mismatch: %+v", index, result)
		}
	}
	permit, reason, ok := svc.TryAcquireProviderAccountPermit(GatewayProvider{AccountID: account.ID, RPMLimit: 1, TPMLimit: 100, CircuitState: CircuitStateClosed}, 10)
	if !ok || reason != "" {
		t.Fatalf("simulation consumed rate capacity: ok=%v reason=%q", ok, reason)
	}
	permit.Release()
}

func TestGatewaySimulationIncludesSkippedCircuitCandidate(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret")
	provider, _ := svc.CreateProvider(ctx, "tester", ProviderRequest{Name: "Skipped provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1", Status: ProviderStatusActive, Models: []string{"upstream"}, APIKey: "secret"})
	account, _ := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{ProviderID: provider.ID, Name: "Skipped account", Platform: "openai_compatible", AuthType: "api_key", Status: AccountStatusActive, Models: []string{"upstream"}, Secret: "secret", CircuitFailureThreshold: 1})
	model, _ := svc.CreateGatewayModel(ctx, "tester", GatewayModelRequest{ModelID: "public", Name: "Public", Status: GatewayModelStatusActive})
	_, _ = svc.CreateModelRoute(ctx, "tester", ModelRouteRequest{GatewayModelID: model.ID, RouteGroup: "default", ProviderAccountID: account.ID, UpstreamModel: "upstream", Status: ModelRouteStatusActive})
	if err := svc.RecordProviderAccountFailure(ctx, account.ID, 500, "failed"); err != nil {
		t.Fatalf("RecordProviderAccountFailure(): %v", err)
	}
	result, err := svc.SimulateGatewayRouting(ctx, GatewaySimulationRequest{Model: "public"})
	if err != nil {
		t.Fatalf("SimulateGatewayRouting(): %v", err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Eligible || result.Candidates[0].Reason != "account_cooling_down" {
		t.Fatalf("skipped circuit candidate missing: %+v", result)
	}
}
