package controlplane

import (
	"context"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

func TestDurableAIJobProviderCapacityRetainedUntilProviderTerminalState(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, time.July, 15, 15, 0, 0, 0, time.UTC)
	now := base
	svc := NewService(NewMemoryRepository(), "/v1", "provider-capacity-durable-secret")
	svc.now = func() time.Time { return now }
	accountID := setupSingleDurableCapacityRoute(t, svc)
	adapter := &durableAIJobAdapterStub{
		dispatchSteps: []durableDispatchStep{
			{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeAccepted, Task: ProviderTaskReference{ProviderTaskID: "task-capacity-first", Status: "running"}, ReconcileAfter: base}},
			{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeAccepted, Task: ProviderTaskReference{ProviderTaskID: "task-capacity-second", Status: "running"}, ReconcileAfter: base.Add(time.Hour)}},
		},
		reconcileResult: ProviderDispatchResult{
			Outcome: ProviderDispatchOutcomeAccepted, Task: ProviderTaskReference{ProviderTaskID: "task-capacity-first", Status: "succeeded"}, ReconcileAfter: base.Add(time.Hour),
		},
	}

	first := beginDurableCapacityJob(t, svc, "capacity-first")
	report, err := svc.RunDurableAIJobWorkerOnce(ctx, "capacity-worker-a", time.Minute, 1, adapter)
	if err != nil || report.Accepted != 1 || adapter.DispatchCalls() != 1 {
		t.Fatalf("first report=%+v calls=%d err=%v", report, adapter.DispatchCalls(), err)
	}
	assertAIJobStatus(t, svc, first.ID, AIJobStatusRunning)
	assertProviderCapacityUnits(t, svc.currentProviderCapacityStore(), accountID, 1)

	second := beginDurableCapacityJob(t, svc, "capacity-second")
	report, err = svc.RunDurableAIJobWorkerOnce(ctx, "capacity-worker-b", time.Minute, 1, adapter)
	if err != nil || report.Requeued != 1 || adapter.DispatchCalls() != 1 {
		t.Fatalf("blocked report=%+v calls=%d err=%v", report, adapter.DispatchCalls(), err)
	}
	assertAIJobStatus(t, svc, second.ID, AIJobStatusQueued)
	assertProviderCapacityUnits(t, svc.currentProviderCapacityStore(), accountID, 1)

	reconcileReport, err := svc.RunDurableAIJobReconcilerOnce(ctx, 10, adapter)
	if err != nil || reconcileReport.Completed != 1 || reconcileReport.Errors != 0 {
		t.Fatalf("reconcile report=%+v err=%v", reconcileReport, err)
	}
	assertAIJobStatus(t, svc, first.ID, AIJobStatusSucceeded)
	assertProviderCapacityUnits(t, svc.currentProviderCapacityStore(), accountID, 0)

	now = base.Add(AIJobDefaultRetryAfter + time.Second)
	report, err = svc.RunDurableAIJobWorkerOnce(ctx, "capacity-worker-c", time.Minute, 1, adapter)
	if err != nil || report.Accepted != 1 || adapter.DispatchCalls() != 2 {
		t.Fatalf("retried report=%+v calls=%d err=%v", report, adapter.DispatchCalls(), err)
	}
	assertAIJobStatus(t, svc, second.ID, AIJobStatusRunning)
	assertProviderCapacityUnits(t, svc.currentProviderCapacityStore(), accountID, 1)
}

func setupSingleDurableCapacityRoute(t *testing.T, svc *Service) string {
	t.Helper()
	ctx := context.Background()
	provider, err := svc.CreateProvider(ctx, "test", ProviderRequest{
		Name: "Capacity provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1",
		Status: ProviderStatusActive, Models: []string{"capacity-upstream"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateProviderAccount(ctx, "test", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Capacity account", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Models: []string{"capacity-upstream"}, Secret: "account-secret", Concurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := svc.CreateGatewayModel(ctx, "test", GatewayModelRequest{
		ModelID: "capacity-image", Name: "Capacity image", Modality: "image", Status: GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateModelRoute(ctx, "test", ModelRouteRequest{
		GatewayModelID: model.ID, RouteGroup: DefaultModelRouteGroup, ProviderAccountID: account.ID,
		UpstreamModel: "capacity-upstream", Priority: 10, Weight: 100, Status: ModelRouteStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	return account.ID
}

func beginDurableCapacityJob(t *testing.T, svc *Service, marker string) AIJob {
	t.Helper()
	job, created, err := svc.BeginDurableAIJob(context.Background(), gatewaycore.CanonicalAuthContext{
		CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: "capacity-key", ProfileScope: ProfileScopePlatform,
		TenantID: "capacity-tenant", PrincipalType: APIKeyTypeService, PrincipalID: "capacity-principal", ArtifactPolicy: GatewayArtifactPolicyTemporary,
	}, gatewaycore.CanonicalRequest{
		ID: "request-" + marker, ClientRequestID: "client-" + marker, Fingerprint: "fingerprint-" + marker, IdempotencyKey: marker,
		Protocol: gatewaycore.ProtocolAsterJobs, Operation: "image_generation", Modality: "image", Lane: gatewaycore.LaneDurable,
		Model: "capacity-image", Payload: []byte(`{"model":"capacity-image","operation":"image_generation","modality":"image","input":{"prompt":"synthetic"}}`),
	})
	if err != nil || !created {
		t.Fatalf("job=%+v created=%t err=%v", job, created, err)
	}
	return job
}

func assertProviderCapacityUnits(t *testing.T, store ProviderCapacityStore, accountID string, want int) {
	t.Helper()
	snapshot, err := store.Snapshot(context.Background(), accountID)
	if err != nil || snapshot.CapacityUnits != want {
		t.Fatalf("capacity snapshot=%+v want=%d err=%v", snapshot, want, err)
	}
}
