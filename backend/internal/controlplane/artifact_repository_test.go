package controlplane

import (
	"context"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestArtifactRepositoryContract(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) Repository
	}{
		{name: "memory", open: func(*testing.T) Repository { return NewMemoryRepository() }},
		{name: "postgres", open: func(t *testing.T) Repository {
			schema := testutil.NewPostgresSchema(t)
			repo, err := NewPostgresRepository(context.Background(), schema.URL)
			if err != nil {
				t.Fatalf("NewPostgresRepository(): %v", err)
			}
			return repo
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			ctx := context.Background()
			base := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
			operation := artifactTestOperation("operation-artifact", "principal-artifact", base)
			if _, created, err := repo.CreateAIOperation(ctx, operation); err != nil || !created {
				t.Fatalf("CreateAIOperation() created=%t err=%v", created, err)
			}
			artifact, event, outbox := artifactAdmissionRecords(t, operation, "artifact-contract", base)
			if err := repo.CreateArtifact(ctx, artifact, event, outbox); err != nil {
				t.Fatalf("CreateArtifact(): %v", err)
			}
			found, ok, err := repo.FindOwnedArtifact(ctx, artifact.ID, artifactOwnerFromOperation(operation))
			if err != nil || !ok || found.Status != ArtifactStatusPending {
				t.Fatalf("FindOwnedArtifact() artifact=%+v found=%t err=%v", found, ok, err)
			}
			otherOwner := artifactOwnerFromOperation(operation)
			otherOwner.PrincipalID = "other-principal"
			if _, found, err := repo.FindOwnedArtifact(ctx, artifact.ID, otherOwner); err != nil || found {
				t.Fatalf("cross-owner artifact found=%t err=%v", found, err)
			}
			query, err := repo.QueryArtifacts(ctx, ArtifactQuery{Owner: &otherOwner, Limit: 10})
			if err != nil || len(query) != 0 {
				t.Fatalf("cross-owner query=%+v err=%v", query, err)
			}
			query, err = repo.QueryArtifacts(ctx, ArtifactQuery{Role: ArtifactRoleFinal, Limit: 10})
			if err != nil || len(query) != 1 || query[0].ID != artifact.ID {
				t.Fatalf("role query=%+v err=%v", query, err)
			}
			query, err = repo.QueryArtifacts(ctx, ArtifactQuery{
				ProfileScope: operation.ProfileScope, TenantID: operation.TenantID,
				Policy: GatewayArtifactPolicyManaged, Limit: 10,
			})
			if err != nil || len(query) != 1 || query[0].ID != artifact.ID {
				t.Fatalf("scope and policy query=%+v err=%v", query, err)
			}
			query, err = repo.QueryArtifacts(ctx, ArtifactQuery{Search: "CONTRACT", Limit: 10})
			if err != nil || len(query) != 1 || query[0].ID != artifact.ID {
				t.Fatalf("search query=%+v err=%v", query, err)
			}
			query, err = repo.QueryArtifacts(ctx, ArtifactQuery{TenantID: "other-tenant", Limit: 10})
			if err != nil || len(query) != 0 {
				t.Fatalf("other tenant query=%+v err=%v", query, err)
			}
			summary, err := repo.SummarizeArtifacts(ctx, ArtifactQuery{ProfileScope: operation.ProfileScope, Limit: 1, Offset: 99})
			if err != nil || summary.Total != 1 || summary.ByStatus[ArtifactStatusPending] != 1 || summary.SizeBytes != 0 {
				t.Fatalf("artifact summary=%+v err=%v", summary, err)
			}
			query, err = repo.QueryArtifacts(ctx, ArtifactQuery{AttemptID: "attempt-missing", Limit: 10})
			if err != nil || len(query) != 0 {
				t.Fatalf("attempt query=%+v err=%v", query, err)
			}
			updated, changed, err := repo.TransitionArtifact(ctx, ArtifactTransitionInput{
				ArtifactID: artifact.ID, ExpectedVersion: artifact.StatusVersion, ToStatus: ArtifactStatusUploading,
				Content: &ArtifactContentUpdate{MediaType: "image/png", StoreDriver: ArtifactStoreDriverMemory, StoreKey: artifact.ID + "/content"},
			}, base.Add(time.Minute))
			if err != nil || !changed || updated.Status != ArtifactStatusUploading || updated.StatusVersion != 2 {
				t.Fatalf("TransitionArtifact() artifact=%+v changed=%t err=%v", updated, changed, err)
			}
			if _, changed, err := repo.TransitionArtifact(ctx, ArtifactTransitionInput{
				ArtifactID: artifact.ID, ExpectedVersion: artifact.StatusVersion, ToStatus: ArtifactStatusFailed,
			}, base.Add(2*time.Minute)); err != nil || changed {
				t.Fatalf("stale transition changed=%t err=%v", changed, err)
			}
			events, err := repo.ListArtifactEvents(ctx, artifact.ID)
			outboxEvents, outboxErr := repo.ListTransactionalOutboxEvents(ctx, artifact.ID)
			if err != nil || outboxErr != nil || len(events) != 2 || len(outboxEvents) != 2 {
				t.Fatalf("events=%d err=%v outbox=%d outboxErr=%v", len(events), err, len(outboxEvents), outboxErr)
			}
		})
	}
}

func TestArtifactAdmissionRollsBackOnOutboxConflict(t *testing.T) {
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
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			ctx := context.Background()
			base := time.Date(2026, time.July, 15, 11, 0, 0, 0, time.UTC)
			operation := artifactTestOperation("operation-rollback", "principal-rollback", base)
			if _, _, err := repo.CreateAIOperation(ctx, operation); err != nil {
				t.Fatal(err)
			}
			first, firstEvent, firstOutbox := artifactAdmissionRecords(t, operation, "artifact-first", base)
			if err := repo.CreateArtifact(ctx, first, firstEvent, firstOutbox); err != nil {
				t.Fatal(err)
			}
			second, secondEvent, secondOutbox := artifactAdmissionRecords(t, operation, "artifact-second", base.Add(time.Minute))
			secondOutbox.ID = firstOutbox.ID
			if err := repo.CreateArtifact(ctx, second, secondEvent, secondOutbox); err == nil {
				t.Fatal("CreateArtifact() accepted an outbox identity conflict")
			}
			if _, found, err := repo.FindArtifact(ctx, second.ID); err != nil || found {
				t.Fatalf("rolled-back artifact found=%t err=%v", found, err)
			}
			if events, err := repo.ListArtifactEvents(ctx, second.ID); err != nil || len(events) != 0 {
				t.Fatalf("rolled-back events=%+v err=%v", events, err)
			}
		})
	}
}

func artifactTestOperation(id, principalID string, at time.Time) AIOperation {
	return AIOperation{
		ID: id, ProfileScope: ProfileScopePlatform, TenantID: "tenant-artifact", CredentialID: "credential-artifact",
		CredentialSource: "api_key", PrincipalType: GatewayPrincipalTypeService, PrincipalID: principalID,
		RequestFingerprint: id + "-fingerprint", Protocol: "aster_jobs", Operation: "image_generation", Modality: "image",
		Lane: "durable", Model: "image-model", Status: AIOperationStatusAccepted, CreatedAt: at, UpdatedAt: at,
		ArtifactPolicy: GatewayArtifactPolicyManaged,
	}
}

func artifactAdmissionRecords(t *testing.T, operation AIOperation, id string, at time.Time) (Artifact, ArtifactEvent, TransactionalOutboxEvent) {
	t.Helper()
	artifact := Artifact{
		ID: id, OperationID: operation.ID, ProfileScope: operation.ProfileScope, TenantID: operation.TenantID,
		IntegrationID: operation.IntegrationID, PrincipalType: operation.PrincipalType, PrincipalID: operation.PrincipalID,
		ExternalSubjectReference: operation.ExternalSubjectReference, Role: ArtifactRoleFinal, Policy: GatewayArtifactPolicyManaged,
		Status: ArtifactStatusPending, StatusVersion: 1, StoreDriver: ArtifactStoreDriverNone,
		RetainUntil: at.Add(time.Hour), CreatedAt: at, UpdatedAt: at,
	}
	event, outbox, err := newArtifactEventRecords(artifact, "", "", at)
	if err != nil {
		t.Fatal(err)
	}
	return artifact, event, outbox
}
