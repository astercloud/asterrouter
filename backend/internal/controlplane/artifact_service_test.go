package controlplane

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestArtifactServiceLifecycleIsolationRangeAndDeletion(t *testing.T) {
	repo := NewMemoryRepository()
	svc := newAIJobTestService(t, repo)
	store := NewMemoryArtifactStore()
	if err := svc.SetArtifactStore(store); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	auth := aiJobTestAuth("tenant-artifact-service", "principal-artifact-service")
	job, _, err := svc.BeginDurableAIJob(context.Background(), auth, aiJobTestRequest("artifact-service-idem", "artifact-service-fingerprint"))
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("synthetic-image-bytes")
	artifact, err := svc.CreateArtifactFromReader(context.Background(), ArtifactCreateInput{
		OperationID: job.OperationID, JobID: job.ID, Role: ArtifactRoleFinal, MediaType: "image/png",
		StoreDriver: ArtifactStoreDriverMemory, ExpectedSizeBytes: int64(len(payload)), MaxBytes: 1024,
	}, bytes.NewReader(payload))
	if err != nil || artifact.Status != ArtifactStatusReady || artifact.SizeBytes != int64(len(payload)) || artifact.SHA256 == "" {
		t.Fatalf("CreateArtifactFromReader() artifact=%+v err=%v", artifact, err)
	}
	rotatedAuth := auth
	rotatedAuth.CredentialID = "rotated-credential"
	if found, ok, err := svc.ArtifactForAuth(context.Background(), rotatedAuth, artifact.ID); err != nil || !ok || found.ID != artifact.ID {
		t.Fatalf("rotated credential artifact=%+v found=%t err=%v", found, ok, err)
	}
	otherAuth := auth
	otherAuth.PrincipalID = "other-principal"
	if _, found, err := svc.ArtifactForAuth(context.Background(), otherAuth, artifact.ID); err != nil || found {
		t.Fatalf("cross-owner artifact found=%t err=%v", found, err)
	}
	_, opened, found, err := svc.OpenArtifactForAuth(context.Background(), rotatedAuth, artifact.ID, &ArtifactByteRange{Offset: 2, Length: 5})
	if err != nil || !found {
		t.Fatalf("OpenArtifactForAuth() found=%t err=%v", found, err)
	}
	ranged, readErr := io.ReadAll(opened.Body)
	_ = opened.Body.Close()
	if readErr != nil || string(ranged) != string(payload[2:7]) || opened.TotalBytes != int64(len(payload)) {
		t.Fatalf("range=%q opened=%+v err=%v", ranged, opened, readErr)
	}
	listed, jobFound, err := svc.ArtifactsForJobAndAuth(context.Background(), rotatedAuth, job.ID)
	if err != nil || !jobFound || len(listed) != 1 {
		t.Fatalf("ArtifactsForJobAndAuth() artifacts=%+v found=%t err=%v", listed, jobFound, err)
	}
	requested, found, err := svc.RequestArtifactDeletionForAuth(context.Background(), rotatedAuth, artifact.ID)
	if err != nil || !found || requested.Status != ArtifactStatusDeleteRequested {
		t.Fatalf("RequestArtifactDeletionForAuth() artifact=%+v found=%t err=%v", requested, found, err)
	}
	var processed atomic.Int32
	var wait sync.WaitGroup
	for index := 0; index < 12; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			count, workerErr := svc.RunArtifactDeletionWorkerOnce(context.Background(), 1)
			if workerErr != nil {
				t.Errorf("RunArtifactDeletionWorkerOnce(): %v", workerErr)
			}
			processed.Add(int32(count))
		}()
	}
	wait.Wait()
	if processed.Load() != 1 {
		t.Fatalf("delete workers processed=%d, want 1", processed.Load())
	}
	deleted, _, _ := svc.ArtifactForAuth(context.Background(), rotatedAuth, artifact.ID)
	if deleted.Status != ArtifactStatusDeleted || deleted.StoreKey != "" {
		t.Fatalf("deleted artifact=%+v", deleted)
	}
	if _, _, _, err := svc.OpenArtifactForAuth(context.Background(), rotatedAuth, artifact.ID, nil); !errors.Is(err, ErrArtifactUnavailable) {
		t.Fatalf("deleted download error=%v", err)
	}
}

func TestArtifactServiceIntegrityFailureMetadataAndRetention(t *testing.T) {
	repo := NewMemoryRepository()
	svc := newAIJobTestService(t, repo)
	store := NewMemoryArtifactStore()
	if err := svc.SetArtifactStore(store); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, time.July, 15, 13, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	auth := aiJobTestAuth("tenant-artifact-integrity", "principal-artifact-integrity")
	job, _, err := svc.BeginDurableAIJob(context.Background(), auth, aiJobTestRequest("artifact-integrity-idem", "artifact-integrity-fingerprint"))
	if err != nil {
		t.Fatal(err)
	}
	failed, err := svc.CreateArtifactFromReader(context.Background(), ArtifactCreateInput{
		OperationID: job.OperationID, JobID: job.ID, Role: ArtifactRoleFinal, MediaType: "image/png",
		StoreDriver: ArtifactStoreDriverMemory, ExpectedSizeBytes: 999, MaxBytes: 1024,
	}, bytes.NewBufferString("short"))
	if !errors.Is(err, ErrArtifactIntegrity) || failed.Status != ArtifactStatusFailed || failed.ErrorType != "integrity_failed" {
		t.Fatalf("integrity artifact=%+v err=%v", failed, err)
	}
	if _, err := store.Open(context.Background(), failed.StoreKey, nil); !errors.Is(err, ErrArtifactUnavailable) {
		t.Fatalf("failed upload remained in store: %v", err)
	}
	metadata, err := svc.CreateArtifactFromReader(context.Background(), ArtifactCreateInput{
		OperationID: job.OperationID, JobID: job.ID, Role: ArtifactRoleMetadata,
		RetainUntil: base.Add(time.Minute), ExpectedSizeBytes: 0,
	}, nil)
	if err != nil || metadata.Status != ArtifactStatusReady || metadata.StoreDriver != ArtifactStoreDriverNone {
		t.Fatalf("metadata artifact=%+v err=%v", metadata, err)
	}
	svc.now = func() time.Time { return base.Add(2 * time.Minute) }
	processed, err := svc.RunArtifactRetentionOnce(context.Background(), 10)
	if err != nil || processed != 1 {
		t.Fatalf("RunArtifactRetentionOnce() processed=%d err=%v", processed, err)
	}
	expired, _, _ := svc.ArtifactForAuth(context.Background(), auth, metadata.ID)
	if expired.Status != ArtifactStatusExpired {
		t.Fatalf("expired artifact=%+v", expired)
	}
	if processed, err := svc.RunArtifactDeletionWorkerOnce(context.Background(), 10); err != nil || processed != 1 {
		t.Fatalf("expired deletion processed=%d err=%v", processed, err)
	}
	deleted, _, _ := svc.ArtifactForAuth(context.Background(), auth, metadata.ID)
	if deleted.Status != ArtifactStatusDeleted {
		t.Fatalf("expired cleanup artifact=%+v", deleted)
	}
}

func TestArtifactDeletionFailureCanRetry(t *testing.T) {
	repo := NewMemoryRepository()
	svc := newAIJobTestService(t, repo)
	store := &flakyDeleteArtifactStore{MemoryArtifactStore: NewMemoryArtifactStore(), failures: 1}
	if err := svc.SetArtifactStore(store); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, time.July, 15, 14, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	auth := aiJobTestAuth("tenant-delete-retry", "principal-delete-retry")
	job, _, err := svc.BeginDurableAIJob(context.Background(), auth, aiJobTestRequest("delete-retry-idem", "delete-retry-fingerprint"))
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := svc.CreateArtifactFromReader(context.Background(), ArtifactCreateInput{
		OperationID: job.OperationID, JobID: job.ID, Role: ArtifactRoleFinal, StoreDriver: ArtifactStoreDriverMemory,
		ExpectedSizeBytes: 4, MaxBytes: 10,
	}, bytes.NewBufferString("data"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.RequestArtifactDeletionForAuth(context.Background(), auth, artifact.ID); err != nil {
		t.Fatal(err)
	}
	if processed, err := svc.RunArtifactDeletionWorkerOnce(context.Background(), 1); processed != 1 || err == nil {
		t.Fatalf("first delete processed=%d err=%v", processed, err)
	}
	failed, _, _ := svc.ArtifactForAuth(context.Background(), auth, artifact.ID)
	if failed.Status != ArtifactStatusDeleteFailed {
		t.Fatalf("delete failure artifact=%+v", failed)
	}
	if processed, err := svc.RunArtifactDeletionWorkerOnce(context.Background(), 1); processed != 1 || err != nil {
		t.Fatalf("retry delete processed=%d err=%v", processed, err)
	}
	deleted, _, _ := svc.ArtifactForAuth(context.Background(), auth, artifact.ID)
	if deleted.Status != ArtifactStatusDeleted {
		t.Fatalf("retried delete artifact=%+v", deleted)
	}
}

type flakyDeleteArtifactStore struct {
	*MemoryArtifactStore
	mu       sync.Mutex
	failures int
}

func (s *flakyDeleteArtifactStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	if s.failures > 0 {
		s.failures--
		s.mu.Unlock()
		return errors.New("synthetic delete failure")
	}
	s.mu.Unlock()
	return s.MemoryArtifactStore.Delete(ctx, key)
}
