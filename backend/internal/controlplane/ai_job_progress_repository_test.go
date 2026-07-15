package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestAIJobProgressRepositoryContract(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) Repository
	}{
		{name: "memory", open: func(*testing.T) Repository { return NewMemoryRepository() }},
		{name: "postgres", open: func(t *testing.T) Repository {
			schema := testutil.NewPostgresSchema(t)
			repo, err := NewPostgresRepository(context.Background(), schema.URL)
			if err != nil {
				t.Fatal(err)
			}
			return repo
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			base := time.Date(2026, time.July, 16, 8, 0, 0, 0, time.UTC)
			svc := NewService(repo, "/v1", "progress-contract-secret")
			svc.now = func() time.Time { return base }
			if err := svc.SetArtifactStore(NewMemoryArtifactStore()); err != nil {
				t.Fatal(err)
			}
			setupDurableWorkerRoutes(t, svc)
			job := beginDurableWorkerJob(t, svc, "progress-contract")
			adapter := &durableAIJobAdapterStub{dispatchSteps: []durableDispatchStep{{result: ProviderDispatchResult{
				Outcome:        ProviderDispatchOutcomeAccepted,
				Task:           ProviderTaskReference{ProviderTaskID: "progress-task", ProviderRequestID: "progress-request", Status: "running"},
				ReconcileAfter: base.Add(time.Hour),
			}}}}
			if report, err := svc.RunDurableAIJobWorkerOnce(ctx, "progress-worker", time.Minute, 1, adapter); err != nil || report.Accepted != 1 {
				t.Fatalf("worker report=%+v err=%v", report, err)
			}
			attempts, err := repo.ListAIAttemptsByOperationID(ctx, job.OperationID)
			if err != nil || len(attempts) != 1 || attempts[0].ProviderTaskID != "progress-task" {
				t.Fatalf("attempts=%+v err=%v", attempts, err)
			}
			attempt := attempts[0]
			percent25 := 25
			first, created, err := svc.RecordAIJobProgress(ctx, job, attempt, ProviderProgressObservation{Sequence: 1, Percent: &percent25, Stage: "GENERATING"})
			if err != nil || !created || first.Stage != "generating" || first.Percent == nil || *first.Percent != 25 {
				t.Fatalf("first=%+v created=%t err=%v", first, created, err)
			}
			percent25 = 99
			svc.now = func() time.Time { return base.Add(time.Minute) }
			percentReplay := 25
			replayed, created, err := svc.RecordAIJobProgress(ctx, job, attempt, ProviderProgressObservation{Sequence: 1, Percent: &percentReplay, Stage: "generating"})
			if err != nil || created || replayed.ID != first.ID || !replayed.CreatedAt.Equal(base) || replayed.Percent == nil || *replayed.Percent != 25 {
				t.Fatalf("replayed=%+v created=%t err=%v", replayed, created, err)
			}

			percent50 := 50
			third, created, err := svc.RecordAIJobProgress(ctx, job, attempt, ProviderProgressObservation{Sequence: 3, Percent: &percent50, Stage: "rendering"})
			if err != nil || !created || third.ProviderSequence != 3 {
				t.Fatalf("third=%+v created=%t err=%v", third, created, err)
			}
			percent40 := 40
			stale, created, err := svc.RecordAIJobProgress(ctx, job, attempt, ProviderProgressObservation{Sequence: 2, Percent: &percent40, Stage: "queued"})
			if err != nil || created || stale.ID != "" {
				t.Fatalf("stale=%+v created=%t err=%v", stale, created, err)
			}
			if _, _, err := svc.RecordAIJobProgress(ctx, job, attempt, ProviderProgressObservation{Sequence: 1, Percent: &percent40, Stage: "generating"}); !errors.Is(err, ErrAIJobProgressConflict) {
				t.Fatalf("conflicting replay error=%v", err)
			}
			if _, _, err := svc.RecordAIJobProgress(ctx, job, attempt, ProviderProgressObservation{Sequence: 4, Percent: &percent40, Stage: "rendering"}); !errors.Is(err, ErrAIJobProgressInvalid) {
				t.Fatalf("regressing progress error=%v", err)
			}
			if _, _, err := svc.RecordAIJobProgress(ctx, job, attempt, ProviderProgressObservation{Sequence: 5, Stage: "contains spaces"}); !errors.Is(err, ErrAIJobProgressInvalid) {
				t.Fatalf("unsafe stage error=%v", err)
			}

			events, err := svc.AIJobProgressEvents(ctx, job.ID)
			if err != nil || len(events) != 2 || events[0].ProviderSequence != 1 || events[1].ProviderSequence != 3 {
				t.Fatalf("events=%+v err=%v", events, err)
			}
			detail, err := svc.AIJobAdmin(ctx, job.ID)
			if err != nil || len(detail.ProgressEvents) != 2 {
				t.Fatalf("admin detail=%+v err=%v", detail, err)
			}
			owner := gatewaycore.CanonicalAuthContext{
				ProfileScope: ProfileScopePlatform, TenantID: "worker-tenant", PrincipalType: APIKeyTypeService, PrincipalID: "worker-principal",
			}
			owned, found, err := svc.AIJobProgressEventsForAuth(ctx, owner, job.ID)
			if err != nil || !found || len(owned) != 2 {
				t.Fatalf("owned=%+v found=%t err=%v", owned, found, err)
			}
			owner.PrincipalID = "other-principal"
			if hidden, found, err := svc.AIJobProgressEventsForAuth(ctx, owner, job.ID); err != nil || found || hidden != nil {
				t.Fatalf("hidden=%+v found=%t err=%v", hidden, found, err)
			}
		})
	}
}
