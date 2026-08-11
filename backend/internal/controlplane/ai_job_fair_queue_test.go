package controlplane

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestRankAIJobFairCandidatesUsesApplicationAndPrincipalHierarchy(t *testing.T) {
	now := time.Date(2026, time.July, 14, 18, 0, 0, 0, time.UTC)
	candidates := []AIJob{
		fairQueueTestJob("application-a-principal-a-1", "application-a", "principal-a", now),
		fairQueueTestJob("application-a-principal-a-2", "application-a", "principal-a", now),
		fairQueueTestJob("application-a-principal-b", "application-a", "principal-b", now),
		fairQueueTestJob("application-b-principal-a", "application-b", "principal-a", now),
	}

	ranked := rankAIJobFairCandidates(candidates, nil, nil, now, len(candidates))
	if len(ranked) != len(candidates) {
		t.Fatalf("ranked jobs=%+v", ranked)
	}
	if ranked[0].ApplicationID == ranked[1].ApplicationID {
		t.Fatalf("first application round was not balanced: %+v", ranked[:2])
	}
	applicationA := make([]AIJob, 0, 3)
	for _, job := range ranked {
		if job.ApplicationID == "application-a" {
			applicationA = append(applicationA, job)
		}
	}
	if len(applicationA) < 2 || applicationA[0].PrincipalID == applicationA[1].PrincipalID {
		t.Fatalf("principal round was not balanced: %+v", applicationA)
	}
}

func TestRankAIJobFairCandidatesUsesInFlightAndDispatchHistory(t *testing.T) {
	now := time.Date(2026, time.July, 14, 18, 30, 0, 0, time.UTC)
	applicationA := fairQueueTestJob("application-a-ready", "application-a", "principal-a", now)
	applicationB := fairQueueTestJob("application-b-ready", "application-b", "principal-b", now)
	inFlight := fairQueueTestJob("application-a-running", "application-a", "principal-a", now.Add(-time.Minute))
	inFlight.Status = AIJobStatusRunning
	ranked := rankAIJobFairCandidates([]AIJob{applicationA, applicationB}, []AIJob{inFlight}, nil, now, 1)
	if len(ranked) != 1 || ranked[0].ApplicationID != "application-b" {
		t.Fatalf("in-flight application was not deprioritized: %+v", ranked)
	}

	activity := aiJobDispatchActivity{Job: applicationA, DispatchedAt: now.Add(-time.Second)}
	ranked = rankAIJobFairCandidates([]AIJob{applicationA, applicationB}, nil, []aiJobDispatchActivity{activity}, now, 1)
	if len(ranked) != 1 || ranked[0].ApplicationID != "application-b" {
		t.Fatalf("recently dispatched application was not rotated: %+v", ranked)
	}
}

func TestRankAIJobFairCandidatesAgesPriorityAndReclaimsExpiredLeaseFirst(t *testing.T) {
	base := time.Date(2026, time.July, 14, 19, 0, 0, 0, time.UTC)
	oldLow := fairQueueTestJob("old-low", "application-a", "principal-a", base)
	newHigh := fairQueueTestJob("new-high", "application-a", "principal-a", base.Add(5*time.Minute))
	newHigh.Priority = aiJobMaxPriority
	beforeAging := rankAIJobFairCandidates([]AIJob{oldLow, newHigh}, nil, nil, base.Add(5*time.Minute), 1)
	if len(beforeAging) != 1 || beforeAging[0].ID != newHigh.ID {
		t.Fatalf("high priority job was not preferred before aging: %+v", beforeAging)
	}
	afterAging := rankAIJobFairCandidates([]AIJob{oldLow, newHigh}, nil, nil, base.Add(10*time.Minute), 1)
	if len(afterAging) != 1 || afterAging[0].ID != oldLow.ID {
		t.Fatalf("old job should catch up after aging: %+v", afterAging)
	}

	reclaim := fairQueueTestJob("expired-dispatch", "application-z", "principal-z", base)
	reclaim.Status = AIJobStatusDispatching
	reclaim.QueueLeaseUntil = timePointer(base.Add(time.Minute))
	queued := fairQueueTestJob("queued", "application-a", "principal-a", base.Add(-time.Hour))
	ranked := rankAIJobFairCandidates([]AIJob{queued, reclaim}, nil, nil, base.Add(2*time.Minute), 1)
	if len(ranked) != 1 || ranked[0].ID != reclaim.ID {
		t.Fatalf("expired dispatch lease was not reclaimed first: %+v", ranked)
	}
}

func TestAIJobFairClaimBalancesBurstingApplicationsAndPrincipals(t *testing.T) {
	forEachAIJobRepository(t, func(t *testing.T, repo Repository) {
		svc := newAIJobTestService(t, repo)
		base := time.Date(2026, time.July, 14, 20, 0, 0, 0, time.UTC)
		svc.now = func() time.Time { return base }
		for index := 0; index < 4; index++ {
			createAIJobForFairQueueTest(t, svc, "application-a", "principal-a", "application-a-job-"+string(rune('a'+index)))
		}
		createAIJobForFairQueueTest(t, svc, "application-b", "principal-b", "application-b-job")
		claimed, err := svc.ClaimReadyAIJobs(context.Background(), "fair-worker", time.Minute, 2)
		if err != nil || len(claimed) != 2 || claimed[0].ApplicationID == claimed[1].ApplicationID {
			t.Fatalf("application fair claim=%+v err=%v", claimed, err)
		}
	})

	forEachAIJobRepository(t, func(t *testing.T, repo Repository) {
		svc := newAIJobTestService(t, repo)
		base := time.Date(2026, time.July, 14, 20, 30, 0, 0, time.UTC)
		svc.now = func() time.Time { return base }
		for index := 0; index < 3; index++ {
			createAIJobForFairQueueTest(t, svc, "application-a", "principal-a", "principal-a-job-"+string(rune('a'+index)))
		}
		createAIJobForFairQueueTest(t, svc, "application-a", "principal-b", "principal-b-job")
		claimed, err := svc.ClaimReadyAIJobs(context.Background(), "fair-worker", time.Minute, 2)
		if err != nil || len(claimed) != 2 || claimed[0].PrincipalID == claimed[1].PrincipalID {
			t.Fatalf("principal fair claim=%+v err=%v", claimed, err)
		}
	})
}

func TestAIJobFairClaimAcrossPostgresInstancesAvoidsSameApplicationBurst(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	repoA, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer repoA.Close()
	repoB, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer repoB.Close()
	svc := newAIJobTestService(t, repoA)
	base := time.Date(2026, time.July, 14, 21, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	for index := 0; index < 4; index++ {
		createAIJobForFairQueueTest(t, svc, "application-a", "principal-a", "multi-a-"+string(rune('a'+index)))
		createAIJobForFairQueueTest(t, svc, "application-b", "principal-b", "multi-b-"+string(rune('a'+index)))
	}

	start := make(chan struct{})
	results := make(chan AIJob, 2)
	errorsSeen := make(chan error, 2)
	var wait sync.WaitGroup
	for index, repo := range []Repository{repoA, repoB} {
		wait.Add(1)
		go func(worker int, current Repository) {
			defer wait.Done()
			<-start
			claimed, claimErr := current.ClaimQueuedAIJobs(ctx, base, base.Add(time.Minute), "worker", "lease-"+string(rune('a'+worker)), 1)
			if claimErr != nil {
				errorsSeen <- claimErr
				return
			}
			if len(claimed) == 1 {
				results <- claimed[0]
			}
		}(index, repo)
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("claim: %v", err)
	}
	claimed := make([]AIJob, 0, 2)
	for job := range results {
		claimed = append(claimed, job)
	}
	if len(claimed) != 2 || claimed[0].ApplicationID == claimed[1].ApplicationID {
		t.Fatalf("multi-instance fair claim=%+v", claimed)
	}
}

func fairQueueTestJob(id, applicationID, principalID string, createdAt time.Time) AIJob {
	return AIJob{
		ID: id, ApplicationID: applicationID, CredentialSource: "api_key",
		PrincipalType: GatewayPrincipalTypeService, PrincipalID: principalID, Status: AIJobStatusQueued,
		CreatedAt: createdAt, UpdatedAt: createdAt, NextEligibleAt: createdAt,
	}
}

func createAIJobForFairQueueTest(t *testing.T, svc *Service, applicationID, principalID, identity string) AIJob {
	t.Helper()
	job, created, err := svc.BeginDurableAIJob(context.Background(), aiJobTestAuth(applicationID, principalID), aiJobTestRequest("idem-"+identity, "fingerprint-"+identity))
	if err != nil || !created {
		t.Fatalf("create fair queue job %s created=%t err=%v", identity, created, err)
	}
	return job
}

func BenchmarkRankAIJobFairCandidates(b *testing.B) {
	now := time.Date(2026, time.July, 14, 22, 0, 0, 0, time.UTC)
	candidates := make([]AIJob, 0, 10_000)
	for index := 0; index < 10_000; index++ {
		job := fairQueueTestJob(
			fmt.Sprintf("job-%05d", index), fmt.Sprintf("application-%03d", index%100), fmt.Sprintf("principal-%03d", index%1000),
			now.Add(-time.Duration(index%20)*time.Minute),
		)
		job.Priority = index % (aiJobMaxPriority + 1)
		candidates = append(candidates, job)
	}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if ranked := rankAIJobFairCandidates(candidates, nil, nil, now, 100); len(ranked) != 100 {
			b.Fatalf("ranked=%d", len(ranked))
		}
	}
}
