package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestArtifactAdminQueryDetailAndRuntimeDisclosure(t *testing.T) {
	fixture := newProviderOutputFixture(t, "image", GatewayArtifactPolicyCustomerSink, []byte("admin-artifact"))
	if report, err := fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter); err != nil || report.Completed != 1 {
		t.Fatalf("reconcile report=%+v err=%v", report, err)
	}
	records, err := fixture.service.ListArtifactsAdmin(context.Background(), ArtifactQuery{
		ProfileScope: ProfileScopePlatform, TenantID: "output-tenant", Policy: GatewayArtifactPolicyCustomerSink,
		Status: ArtifactStatusDelivered, Limit: 10,
	})
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	record := records[0]
	if record.JobID != fixture.job.ID || record.SinkID != fixture.sink.ID() || record.RuntimeStatus != "registered" || record.ProviderID == "" {
		t.Fatalf("record=%+v", record)
	}
	detail, err := fixture.service.ArtifactAdmin(context.Background(), record.ID)
	if err != nil || detail.Artifact.ID != record.ID || len(detail.Events) < 4 {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
	summary, err := fixture.service.ArtifactSummaryAdmin(context.Background(), ArtifactQuery{Limit: 1, Offset: 100})
	if err != nil || summary.Total != 1 || summary.ByStatus[ArtifactStatusDelivered] != 1 || summary.SizeBytes != int64(len("admin-artifact")) {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	runtimes := fixture.service.ArtifactRuntimes()
	if len(runtimes) != 1 || runtimes[0] != (ArtifactRuntime{Kind: "sink", ID: fixture.sink.ID(), Status: "registered"}) {
		t.Fatalf("runtimes=%+v", runtimes)
	}
	for _, query := range []ArtifactQuery{{Status: "secret-state"}, {Policy: "foreign"}, {Role: "foreign"}, {Offset: -1}} {
		if _, err := fixture.service.ListArtifactsAdmin(context.Background(), query); !errors.Is(err, ErrArtifactAdminQueryInvalid) {
			t.Fatalf("query=%+v err=%v", query, err)
		}
	}
}

func TestRetryArtifactDeliverySchedulesReconciliationAndAudits(t *testing.T) {
	fixture := newProviderOutputFixture(t, "video", GatewayArtifactPolicyCustomerSink, []byte("admin-retry-video"))
	fixture.sink.failures = 1
	if report, err := fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter); err == nil || report.Errors != 1 {
		t.Fatalf("first reconcile report=%+v err=%v", report, err)
	}
	artifacts, err := fixture.service.ListArtifactsAdmin(context.Background(), ArtifactQuery{Status: ArtifactStatusDeliveryFailed, Limit: 10})
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("failed artifacts=%+v err=%v", artifacts, err)
	}
	attemptBefore, found, err := fixture.service.AIAttempt(context.Background(), artifacts[0].AttemptID)
	if err != nil || !found {
		t.Fatalf("attempt before=%+v found=%t err=%v", attemptBefore, found, err)
	}
	result, err := fixture.service.RetryArtifactDelivery(context.Background(), "admin@example.test", artifacts[0].ID)
	if err != nil || result.Status != "scheduled" || result.ArtifactID != artifacts[0].ID || result.AttemptID != attemptBefore.ID {
		t.Fatalf("retry result=%+v err=%v", result, err)
	}
	attemptAfter, found, err := fixture.service.AIAttempt(context.Background(), attemptBefore.ID)
	if err != nil || !found || attemptAfter.DispatchVersion != attemptBefore.DispatchVersion+1 || attemptAfter.ReconcileAfter == nil || !attemptAfter.ReconcileAfter.Equal(result.ScheduledAt) {
		t.Fatalf("attempt after=%+v found=%t err=%v", attemptAfter, found, err)
	}
	logs, err := fixture.service.ListAuditLogs(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	foundAudit := false
	for _, log := range logs {
		if log.Action == "retry_delivery" && log.ResourceType == "artifact" && log.ResourceID == artifacts[0].ID && log.Actor == "admin@example.test" &&
			log.ProfileScope == ProfileScopePlatform && log.PlatformTenantID == artifacts[0].TenantID {
			foundAudit = true
		}
	}
	if !foundAudit {
		t.Fatalf("retry audit missing: %+v", logs)
	}
	fixture.now = fixture.now.Add(time.Second)
	if report, err := fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter); err != nil || report.Completed != 1 {
		t.Fatalf("retry reconcile report=%+v err=%v", report, err)
	}
	if fixture.adapter.DispatchCalls() != 1 || fixture.sink.Deliveries() != 2 {
		t.Fatalf("dispatches=%d deliveries=%d", fixture.adapter.DispatchCalls(), fixture.sink.Deliveries())
	}
	if _, err := fixture.service.RetryArtifactDelivery(context.Background(), "admin@example.test", artifacts[0].ID); !errors.Is(err, ErrArtifactDeliveryRetry) {
		t.Fatalf("delivered retry err=%v", err)
	}
}

func TestRetryArtifactDeliveryFailsClosedWithoutRegisteredSink(t *testing.T) {
	fixture := newProviderOutputFixture(t, "audio", GatewayArtifactPolicyCustomerSink, []byte("missing-runtime"))
	fixture.sink.failures = 1
	_, _ = fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter)
	artifacts, _ := fixture.service.ListArtifactsAdmin(context.Background(), ArtifactQuery{Status: ArtifactStatusDeliveryFailed, Limit: 10})
	if len(artifacts) != 1 {
		t.Fatalf("artifacts=%+v", artifacts)
	}
	fixture.service.artifactSinkMu.Lock()
	delete(fixture.service.artifactSinks, fixture.sink.ID())
	fixture.service.artifactSinkMu.Unlock()
	if _, err := fixture.service.RetryArtifactDelivery(context.Background(), "admin", artifacts[0].ID); !errors.Is(err, ErrArtifactSinkRequired) {
		t.Fatalf("missing sink retry err=%v", err)
	}
	logs, _ := fixture.service.ListAuditLogs(context.Background(), 20)
	for _, log := range logs {
		if log.Action == "retry_delivery" && log.ResourceID == artifacts[0].ID {
			t.Fatalf("failed retry was audited as scheduled: %+v", log)
		}
	}
}

func TestArtifactDeliveryRetryRollsBackWhenAuditCannotBeWritten(t *testing.T) {
	fixture := newProviderOutputFixture(t, "image", GatewayArtifactPolicyCustomerSink, []byte("atomic-retry"))
	fixture.sink.failures = 1
	_, _ = fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter)
	artifacts, _ := fixture.service.repo.QueryArtifacts(context.Background(), ArtifactQuery{Status: ArtifactStatusDeliveryFailed, Limit: 10})
	if len(artifacts) != 1 {
		t.Fatalf("artifacts=%+v", artifacts)
	}
	attempt, found, err := fixture.service.repo.FindAIAttempt(context.Background(), artifacts[0].AttemptID)
	if err != nil || !found {
		t.Fatalf("attempt=%+v found=%t err=%v", attempt, found, err)
	}
	now := fixture.service.nowUTC()
	requested := attempt
	requested.ReconcileAfter = &now
	requested.UpdatedAt = now
	audit := fixture.service.newAuditLog("admin", "retry_delivery", "artifact", artifacts[0].ID, "Synthetic duplicate audit")
	if err := fixture.service.repo.AddAuditLog(context.Background(), audit); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := fixture.service.repo.ScheduleArtifactDeliveryRetry(context.Background(), artifacts[0].ID, requested, attempt.DispatchVersion, audit); err == nil || changed {
		t.Fatalf("retry changed=%t err=%v", changed, err)
	}
	current, found, err := fixture.service.repo.FindAIAttempt(context.Background(), attempt.ID)
	reconcileChanged := (current.ReconcileAfter == nil) != (attempt.ReconcileAfter == nil)
	if current.ReconcileAfter != nil && attempt.ReconcileAfter != nil && !current.ReconcileAfter.Equal(*attempt.ReconcileAfter) {
		reconcileChanged = true
	}
	if err != nil || !found || current.DispatchVersion != attempt.DispatchVersion || reconcileChanged {
		t.Fatalf("attempt changed despite audit failure: before=%+v after=%+v found=%t err=%v", attempt, current, found, err)
	}
}
