package controlplane

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestAPIKeyRotationFamilyContract(t *testing.T) {
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
			svc := NewService(repo, "/v1")
			ctx := context.Background()
			created, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{Name: "rotation", ModelAllowlist: []string{"model-a"}})
			if err != nil {
				t.Fatalf("CreateAPIKey(): %v", err)
			}
			rotated, err := svc.RotateAPIKey(ctx, "tester", created.Record.ID)
			if err != nil {
				t.Fatalf("RotateAPIKey(): %v", err)
			}
			if rotated.Record.ID == created.Record.ID || rotated.Record.ReplacesKeyID != created.Record.ID || rotated.Record.RotationFamilyID == "" || rotated.Record.RotationFamilyID != created.Record.RotationFamilyID || rotated.Key == created.Key {
				t.Fatalf("rotated = %+v", rotated)
			}
			if _, err := svc.AuthenticateGatewayKey(ctx, created.Key); !errors.Is(err, ErrGatewayUnauthorized) {
				t.Fatalf("previous key auth error=%v", err)
			}
			if auth, err := svc.AuthenticateGatewayKey(ctx, rotated.Key); err != nil || auth.APIKey.ID != rotated.Record.ID {
				t.Fatalf("replacement auth=%+v err=%v", auth, err)
			}
			keys, err := svc.ListAPIKeys(ctx)
			if err != nil || len(keys) != 2 {
				t.Fatalf("keys=%+v err=%v", keys, err)
			}
			previous := apiKeyRecordByID(keys, created.Record.ID)
			if previous.Status != APIKeyStatusDisabled || previous.ReplacedByKeyID != rotated.Record.ID || previous.RotationGraceExpiresAt == nil {
				t.Fatalf("previous = %+v", previous)
			}
			if _, err := svc.RotateAPIKey(ctx, "tester", created.Record.ID); !errors.Is(err, ErrAPIKeyAlreadyRotated) {
				t.Fatalf("second rotation error=%v", err)
			}
			audit, err := svc.ListAuditLogs(ctx, 10)
			if err != nil {
				t.Fatal(err)
			}
			if !containsAPIKeyRotationAudit(audit, rotated.Record.ID) {
				t.Fatalf("rotation audit missing for %s: %+v", rotated.Record.ID, audit)
			}
		})
	}
}

func TestAPIKeyRotationGracePeriodAllowsBoundedOverlap(t *testing.T) {
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
			svc := NewService(repo, "/v1")
			ctx := context.Background()
			created, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{Name: "grace", ModelAllowlist: []string{"model-a"}})
			if err != nil {
				t.Fatal(err)
			}
			base := time.Now().UTC()
			svc.now = func() time.Time { return base }
			rotated, err := svc.RotateAPIKeyWithGrace(ctx, "tester", created.Record.ID, 3600)
			if err != nil {
				t.Fatalf("RotateAPIKeyWithGrace(): %v", err)
			}
			if _, err := svc.AuthenticateGatewayKey(ctx, created.Key); err != nil {
				t.Fatalf("previous key rejected during grace: %v", err)
			}
			if _, err := svc.AuthenticateGatewayKey(ctx, rotated.Key); err != nil {
				t.Fatalf("replacement key rejected: %v", err)
			}
			svc.now = func() time.Time { return base.Add(2 * time.Hour) }
			if _, err := svc.AuthenticateGatewayKey(ctx, created.Key); !errors.Is(err, ErrGatewayUnauthorized) {
				t.Fatalf("previous key after grace error=%v", err)
			}
			if _, err := svc.AuthenticateGatewayKey(ctx, rotated.Key); err != nil {
				t.Fatalf("replacement key after grace rejected: %v", err)
			}
			keys, err := svc.ListAPIKeys(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if previous := apiKeyRecordByID(keys, created.Record.ID); previous.LifecycleStatus != APIKeyLifecycleRetired {
				t.Fatalf("previous lifecycle=%q record=%+v", previous.LifecycleStatus, previous)
			}
			if replacement := apiKeyRecordByID(keys, rotated.Record.ID); replacement.LifecycleStatus != APIKeyLifecycleActive {
				t.Fatalf("replacement lifecycle=%q record=%+v", replacement.LifecycleStatus, replacement)
			}
		})
	}
}

func TestAPIKeyLifecycleStatus(t *testing.T) {
	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	grace := now.Add(time.Hour)
	tests := []struct {
		name string
		key  APIKeyRecord
		want string
	}{
		{name: "active", key: APIKeyRecord{Status: APIKeyStatusActive}, want: APIKeyLifecycleActive},
		{name: "disabled", key: APIKeyRecord{Status: APIKeyStatusDisabled}, want: APIKeyLifecycleDisabled},
		{name: "retiring", key: APIKeyRecord{Status: APIKeyStatusActive, ReplacedByKeyID: "replacement", RotationGraceExpiresAt: &grace}, want: APIKeyLifecycleRetiring},
		{name: "retired without grace", key: APIKeyRecord{Status: APIKeyStatusDisabled, ReplacedByKeyID: "replacement"}, want: APIKeyLifecycleRetired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := apiKeyLifecycleStatus(test.key, now); got != test.want {
				t.Fatalf("apiKeyLifecycleStatus()=%q want=%q", got, test.want)
			}
		})
	}
}

func TestAPIKeyRotationCreatesOneReplacementUnderConcurrency(t *testing.T) {
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
			svc := NewService(repo, "/v1")
			created, err := svc.CreateAPIKey(context.Background(), "tester", APIKeyCreateRequest{Name: "concurrent rotation", ModelAllowlist: []string{"model-a"}})
			if err != nil {
				t.Fatal(err)
			}
			var successes atomic.Int32
			var wait sync.WaitGroup
			for index := 0; index < 20; index++ {
				wait.Add(1)
				go func() {
					defer wait.Done()
					_, err := svc.RotateAPIKey(context.Background(), "tester", created.Record.ID)
					if err == nil {
						successes.Add(1)
						return
					}
					if !errors.Is(err, ErrAPIKeyAlreadyRotated) && !errors.Is(err, ErrAPIKeyChangedDuringRotation) {
						t.Errorf("RotateAPIKey(): %v", err)
					}
				}()
			}
			wait.Wait()
			keys, err := svc.ListAPIKeys(context.Background())
			if err != nil || successes.Load() != 1 || len(keys) != 2 {
				t.Fatalf("successes=%d keys=%+v err=%v", successes.Load(), keys, err)
			}
		})
	}
}

func TestAPIKeyRotationPairRollsBackWhenReplacementExists(t *testing.T) {
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
			ctx := context.Background()
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			now := time.Now().UTC().Truncate(time.Microsecond)
			previous := APIKeyRecord{
				ID: "key-previous", Name: "Previous", KeyHash: "hash-previous", Fingerprint: "fp-previous",
				Prefix: "ar_prev", Status: APIKeyStatusActive, KeyType: APIKeyTypeService,
				RotationFamilyID: "family-previous", CreatedAt: now, UpdatedAt: now,
			}
			existing := APIKeyRecord{
				ID: "key-existing", Name: "Existing", KeyHash: "hash-existing", Fingerprint: "fp-existing",
				Prefix: "ar_existing", Status: APIKeyStatusActive, KeyType: APIKeyTypeService,
				RotationFamilyID: "family-existing", CreatedAt: now, UpdatedAt: now,
			}
			if err := repo.SaveAPIKey(ctx, previous); err != nil {
				t.Fatal(err)
			}
			if err := repo.SaveAPIKey(ctx, existing); err != nil {
				t.Fatal(err)
			}

			updatedPrevious := previous
			updatedPrevious.Status = APIKeyStatusDisabled
			updatedPrevious.ReplacedByKeyID = existing.ID
			updatedPrevious.UpdatedAt = now.Add(time.Second)
			replacement := existing
			replacement.Name = "Must not overwrite"
			replacement.KeyHash = "hash-replacement"
			replacement.ReplacesKeyID = previous.ID
			audit := AuditLog{ID: "audit-replacement-collision", Actor: "tester", Action: "rotate", ResourceType: "api_key", ResourceID: replacement.ID, CreatedAt: now}
			if err := repo.RotateAPIKeyPair(ctx, updatedPrevious, replacement, audit, previous.UpdatedAt); err == nil {
				t.Fatal("RotateAPIKeyPair() error = nil")
			}

			foundPrevious, ok, err := repo.FindAPIKeyByHash(ctx, previous.KeyHash)
			if err != nil || !ok || foundPrevious.Status != APIKeyStatusActive || foundPrevious.ReplacedByKeyID != "" {
				t.Fatalf("previous ok=%t record=%+v err=%v", ok, foundPrevious, err)
			}
			foundExisting, ok, err := repo.FindAPIKeyByHash(ctx, existing.KeyHash)
			if err != nil || !ok || foundExisting.Name != existing.Name || foundExisting.ReplacesKeyID != "" {
				t.Fatalf("existing ok=%t record=%+v err=%v", ok, foundExisting, err)
			}
		})
	}
}

func TestAPIKeyRotationPairRollsBackWhenAuditInsertFails(t *testing.T) {
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
			ctx := context.Background()
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			now := time.Now().UTC().Truncate(time.Microsecond)
			previous := APIKeyRecord{
				ID: "key-audit-previous", Name: "Previous", KeyHash: "hash-audit-previous", Fingerprint: "fp-audit-previous",
				Prefix: "ar_previous", Status: APIKeyStatusActive, KeyType: APIKeyTypeService,
				RotationFamilyID: "family-audit", CreatedAt: now, UpdatedAt: now,
			}
			if err := repo.SaveAPIKey(ctx, previous); err != nil {
				t.Fatal(err)
			}
			existingAudit := AuditLog{ID: "audit-existing", Actor: "tester", Action: "existing", ResourceType: "api_key", ResourceID: previous.ID, CreatedAt: now}
			if err := repo.AddAuditLog(ctx, existingAudit); err != nil {
				t.Fatal(err)
			}

			updatedPrevious := previous
			updatedPrevious.Status = APIKeyStatusDisabled
			updatedPrevious.ReplacedByKeyID = "key-audit-replacement"
			updatedPrevious.UpdatedAt = now.Add(time.Second)
			replacement := previous
			replacement.ID = updatedPrevious.ReplacedByKeyID
			replacement.KeyHash = "hash-audit-replacement"
			replacement.Fingerprint = "fp-audit-replacement"
			replacement.ReplacesKeyID = previous.ID
			replacement.ReplacedByKeyID = ""
			replacement.CreatedAt = updatedPrevious.UpdatedAt
			replacement.UpdatedAt = updatedPrevious.UpdatedAt

			if err := repo.RotateAPIKeyPair(ctx, updatedPrevious, replacement, existingAudit, previous.UpdatedAt); err == nil {
				t.Fatal("RotateAPIKeyPair() error = nil")
			}
			foundPrevious, ok, err := repo.FindAPIKeyByHash(ctx, previous.KeyHash)
			if err != nil || !ok || foundPrevious.Status != APIKeyStatusActive || foundPrevious.ReplacedByKeyID != "" {
				t.Fatalf("previous ok=%t record=%+v err=%v", ok, foundPrevious, err)
			}
			if _, ok, err := repo.FindAPIKeyByHash(ctx, replacement.KeyHash); err != nil || ok {
				t.Fatalf("replacement persisted ok=%t err=%v", ok, err)
			}
		})
	}
}

func containsAPIKeyRotationAudit(events []AuditLog, replacementID string) bool {
	for _, event := range events {
		if event.Action == "rotate" && event.ResourceType == "api_key" && event.ResourceID == replacementID {
			return true
		}
	}
	return false
}

func apiKeyRecordByID(keys []APIKeyRecord, id string) APIKeyRecord {
	for _, key := range keys {
		if key.ID == id {
			return key
		}
	}
	return APIKeyRecord{}
}
