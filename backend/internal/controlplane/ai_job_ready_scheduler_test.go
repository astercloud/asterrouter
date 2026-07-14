package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDurableAIJobSchedulerFallsBackWhenReadyIndexIsUnavailable(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, time.July, 15, 10, 30, 0, 0, time.UTC)
	svc := newAIJobTestService(t, NewMemoryRepository())
	svc.now = func() time.Time { return base }
	svc.SetAIJobReadyIndex(unavailableAIJobReadyIndex{err: errors.New("redis unavailable")})
	if _, _, err := svc.BeginDurableAIJob(ctx, aiJobTestAuth("tenant-fallback", "principal-fallback"), aiJobTestRequest("index-fallback", "index-fallback")); err != nil {
		t.Fatal(err)
	}
	queue, err := NewMemoryAIJobDeliveryQueue(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	queue.now = func() time.Time { return base }
	report, err := svc.RunDurableAIJobSchedulerOnce(ctx, "fallback-scheduler", time.Minute, 1, queue)
	if err != nil || report.Claimed != 1 || report.Published != 1 || report.Errors != 0 {
		t.Fatalf("fallback report=%+v err=%v", report, err)
	}
}

func TestDurableAIJobSchedulerUsesReadyIndexWithoutTrustingStaleVersion(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, time.July, 15, 11, 0, 0, 0, time.UTC)
	now := base
	repo := NewMemoryRepository()
	svc := newAIJobTestService(t, repo)
	svc.now = func() time.Time { return now }
	index := NewMemoryAIJobReadyIndex()
	svc.SetAIJobReadyIndex(index)
	job, _, err := svc.BeginDurableAIJob(ctx, aiJobTestAuth("tenant-index", "principal-index"), aiJobTestRequest("index-stale", "index-stale"))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimQueuedAIJobs(ctx, now, now.Add(time.Minute), "other-scheduler", "other-lease", 1)
	if err != nil || len(claimed) != 1 || claimed[0].StatusVersion != 2 {
		t.Fatalf("direct claim=%+v err=%v", claimed, err)
	}
	queue, err := NewMemoryAIJobDeliveryQueue(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	queue.now = func() time.Time { return now }
	report, err := svc.RunDurableAIJobSchedulerOnce(ctx, "indexed-scheduler", time.Minute, 1, queue)
	if err != nil || report.Claimed != 0 || report.Published != 0 {
		t.Fatalf("stale scheduler report=%+v err=%v", report, err)
	}
	entries, err := index.Candidates(ctx, AIJobReadyQuery{ReadyAt: now.Add(time.Minute), Limit: 10})
	if err != nil || !containsReadyJobVersion(entries, job.ID, claimed[0].StatusVersion) {
		t.Fatalf("reconciled entries=%+v err=%v", entries, err)
	}
	now = base.Add(time.Minute)
	report, err = svc.RunDurableAIJobSchedulerOnce(ctx, "indexed-scheduler", time.Minute, 1, queue)
	if err != nil || report.Claimed != 1 || report.Published != 1 {
		t.Fatalf("reclaim scheduler report=%+v err=%v", report, err)
	}
	reclaimed, found, err := repo.FindAIJob(ctx, job.ID)
	if err != nil || !found || reclaimed.StatusVersion != 3 || reclaimed.FenceToken != 2 {
		t.Fatalf("reclaimed=%+v found=%t err=%v", reclaimed, found, err)
	}
}

func TestDurableAIJobRebuilderRestoresLostReadyIndex(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	now := base
	svc := newAIJobTestService(t, NewMemoryRepository())
	svc.now = func() time.Time { return now }
	job, _, err := svc.BeginDurableAIJob(ctx, aiJobTestAuth("tenant-reindex", "principal-reindex"), aiJobTestRequest("reindex-job", "reindex-job"))
	if err != nil {
		t.Fatal(err)
	}
	index := NewMemoryAIJobReadyIndex()
	svc.SetAIJobReadyIndex(index)
	queue, err := NewMemoryAIJobDeliveryQueue(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	queue.now = func() time.Time { return now }
	report, err := svc.RebuildDurableAIJobDeliveriesOnce(ctx, "reindex-scheduler", time.Minute, 10, queue)
	if err != nil || report.Reindexed != 1 || report.Claimed != 1 || report.Published != 1 || report.Errors != 0 {
		t.Fatalf("rebuild report=%+v err=%v", report, err)
	}
	current, found, err := svc.repo.FindAIJob(ctx, job.ID)
	if err != nil || !found || current.Status != AIJobStatusDispatching {
		t.Fatalf("current=%+v found=%t err=%v", current, found, err)
	}
}

func TestAIJobReadyIndexLifecycleRemovesRunningJob(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, time.July, 15, 13, 0, 0, 0, time.UTC)
	svc := newAIJobTestService(t, NewMemoryRepository())
	svc.now = func() time.Time { return base }
	index := NewMemoryAIJobReadyIndex()
	svc.SetAIJobReadyIndex(index)
	if _, _, err := svc.BeginDurableAIJob(ctx, aiJobTestAuth("tenant-lifecycle", "principal-lifecycle"), aiJobTestRequest("ready-lifecycle", "ready-lifecycle")); err != nil {
		t.Fatal(err)
	}
	assertAIJobReadyCount(t, index, AIJobReadyScope{Level: AIJobReadyScopeAll}, 1)
	claimed, err := svc.ClaimReadyAIJobs(ctx, "lifecycle-scheduler", time.Minute, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%+v err=%v", claimed, err)
	}
	assertAIJobReadyCount(t, index, AIJobReadyScope{Level: AIJobReadyScopeAll}, 1)
	if _, err := svc.TransitionAIJob(ctx, claimed[0].ID, claimed[0].StatusVersion, claimed[0].FenceToken, AIJobStatusRunning, ""); err != nil {
		t.Fatal(err)
	}
	assertAIJobReadyCount(t, index, AIJobReadyScope{Level: AIJobReadyScopeAll}, 0)
}

type unavailableAIJobReadyIndex struct {
	err error
}

func (index unavailableAIJobReadyIndex) Register(context.Context, AIJobReadyEntry) error {
	return index.err
}

func (index unavailableAIJobReadyIndex) Remove(context.Context, AIJobReadyReference) error {
	return index.err
}

func (index unavailableAIJobReadyIndex) Candidates(context.Context, AIJobReadyQuery) ([]AIJobReadyEntry, error) {
	return nil, index.err
}

func (index unavailableAIJobReadyIndex) Count(context.Context, AIJobReadyScope) (int64, error) {
	return 0, index.err
}
