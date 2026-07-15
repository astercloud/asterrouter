package controlplane

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAIJobAdminQueriesRedactSecretsAndSupportsSafeActions(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	service := NewService(NewMemoryRepository(), "/v1", "test-secret")
	service.now = func() time.Time { return base }
	setupDurableWorkerRoutes(t, service)
	job := beginDurableWorkerJob(t, service, "admin-query")
	claimed, err := service.ClaimReadyAIJobs(ctx, "admin-worker", time.Minute, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimReadyAIJobs() jobs=%+v err=%v", claimed, err)
	}
	job = claimed[0]
	attempt, err := service.BeginAIAttempt(ctx, job.OperationID, 1, GatewayProvider{
		ID: "provider-id", AccountID: "account-id", AdapterID: "adapter-id", RouteID: "route-id", UpstreamModel: "upstream-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	requested := attempt
	requested.DispatchState = AIAttemptDispatchUnknown
	requested.ProviderTaskID = "provider-task-id"
	requested.ProviderRequestID = "provider-request-id"
	requested.DispatchIntentJSON = `{"secret":"must-not-leak"}`
	requested.ReconcileAfter = timePointer(base.Add(time.Hour))
	requested.UpdatedAt = base
	if _, changed, err := service.repo.UpdateAIAttemptDispatch(ctx, requested, attempt.DispatchVersion); err != nil || !changed {
		t.Fatalf("UpdateAIAttemptDispatch() changed=%t err=%v", changed, err)
	}

	detail, err := service.AIJobAdmin(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, forbidden := range []string{`"request_payload"`, "must-not-leak", "provider-request-id", "queue_lease_token", "dispatch_intent_json"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("admin detail disclosed %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, "provider-task-id") || len(detail.Attempts) != 1 || len(detail.Events) < 1 {
		t.Fatalf("detail=%+v body=%s", detail, body)
	}

	result, err := service.ScheduleAIAttemptReconciliationAdmin(ctx, "admin@example.test", job.ID, attempt.ID)
	if err != nil || result.Status != "scheduled" {
		t.Fatalf("ScheduleAIAttemptReconciliationAdmin() result=%+v err=%v", result, err)
	}
	updatedAttempt, found, err := service.repo.FindAIAttempt(ctx, attempt.ID)
	if err != nil || !found || updatedAttempt.ReconcileAfter == nil || !updatedAttempt.ReconcileAfter.Equal(base) {
		t.Fatalf("updated attempt=%+v found=%t err=%v", updatedAttempt, found, err)
	}

	cancelResult, err := service.CancelAIJobAdmin(ctx, "admin@example.test", job.ID)
	if err != nil || cancelResult.Status != AIJobStatusCanceling || !cancelResult.Changed {
		t.Fatalf("CancelAIJobAdmin() result=%+v err=%v", cancelResult, err)
	}
	if _, err := service.CancelAIJobAdmin(ctx, "admin@example.test", job.ID); err != nil {
		t.Fatalf("idempotent cancellation error=%v", err)
	}
	audits, err := service.ListAuditLogsQuery(ctx, AuditLogQuery{ResourceType: "ai_job", Action: "cancel", Limit: 10})
	if err != nil || len(audits) != 1 {
		t.Fatalf("cancel audits=%+v err=%v", audits, err)
	}
	audits, err = service.ListAuditLogsQuery(ctx, AuditLogQuery{ResourceType: "ai_attempt", Action: "schedule_reconciliation", Limit: 10})
	if err != nil || len(audits) != 1 {
		t.Fatalf("reconcile audits=%+v err=%v", audits, err)
	}
}

func TestAIJobAdminRejectsInvalidQueriesAndUnsafeReconciliation(t *testing.T) {
	service := NewService(NewMemoryRepository(), "/v1")
	if _, err := service.ListAIJobsAdmin(context.Background(), AIJobQuery{Status: "not-a-status"}); err != ErrAIJobAdminQueryInvalid {
		t.Fatalf("invalid status error=%v", err)
	}
	if _, err := service.AIJobAdmin(context.Background(), "missing"); err != ErrAIJobNotFound {
		t.Fatalf("missing job error=%v", err)
	}
}
