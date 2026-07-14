package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAIJobAdmissionLimitsAreHierarchicalAndAllowIdempotentReplay(t *testing.T) {
	forEachAIJobRepository(t, func(t *testing.T, repo Repository) {
		t.Run("principal", func(t *testing.T) {
			svc := newAIJobTestService(t, repo)
			if err := svc.SetAIJobAdmissionLimits(AIJobAdmissionLimits{Profile: 3, Tenant: 2, Principal: 1}); err != nil {
				t.Fatal(err)
			}
			authA := aiJobTestAuth("tenant-a", "principal-a")
			request := aiJobTestRequest("admission-principal-a", "fingerprint-principal-a")
			first, created, err := svc.BeginDurableAIJob(context.Background(), authA, request)
			if err != nil || !created {
				t.Fatalf("first=%+v created=%t err=%v", first, created, err)
			}
			replayed, created, err := svc.BeginDurableAIJob(context.Background(), authA, request)
			if err != nil || created || replayed.ID != first.ID {
				t.Fatalf("replayed=%+v created=%t err=%v", replayed, created, err)
			}
			if _, _, err := svc.BeginDurableAIJob(context.Background(), authA, aiJobTestRequest("admission-principal-b", "fingerprint-principal-b")); !errors.Is(err, ErrAIJobQueueCapacityExceeded) {
				t.Fatalf("principal limit error=%v", err)
			}
			authB := aiJobTestAuth("tenant-a", "principal-b")
			if _, created, err := svc.BeginDurableAIJob(context.Background(), authB, aiJobTestRequest("admission-principal-c", "fingerprint-principal-c")); err != nil || !created {
				t.Fatalf("other principal created=%t err=%v", created, err)
			}
			authC := aiJobTestAuth("tenant-a", "principal-c")
			if _, _, err := svc.BeginDurableAIJob(context.Background(), authC, aiJobTestRequest("admission-tenant-limit", "fingerprint-tenant-limit")); !errors.Is(err, ErrAIJobQueueCapacityExceeded) || !strings.Contains(err.Error(), "tenant") {
				t.Fatalf("tenant limit error=%v", err)
			}
			authD := aiJobTestAuth("tenant-b", "principal-d")
			if _, created, err := svc.BeginDurableAIJob(context.Background(), authD, aiJobTestRequest("admission-profile-third", "fingerprint-profile-third")); err != nil || !created {
				t.Fatalf("third profile job created=%t err=%v", created, err)
			}
			authE := aiJobTestAuth("tenant-c", "principal-e")
			if _, _, err := svc.BeginDurableAIJob(context.Background(), authE, aiJobTestRequest("admission-profile-limit", "fingerprint-profile-limit")); !errors.Is(err, ErrAIJobQueueCapacityExceeded) || !strings.Contains(err.Error(), "profile") {
				t.Fatalf("profile limit error=%v", err)
			}
			replayed, created, err = svc.BeginDurableAIJob(context.Background(), authA, request)
			if err != nil || created || replayed.ID != first.ID {
				t.Fatalf("full queue replay=%+v created=%t err=%v", replayed, created, err)
			}
		})
	})
}

func TestAIJobAdmissionLimitIsAtomicAcrossUniqueConcurrentRequests(t *testing.T) {
	forEachAIJobRepository(t, func(t *testing.T, repo Repository) {
		svc := newAIJobTestService(t, repo)
		if err := svc.SetAIJobAdmissionLimits(AIJobAdmissionLimits{Principal: 1}); err != nil {
			t.Fatal(err)
		}
		auth := aiJobTestAuth("tenant-concurrent-limit", "principal-concurrent-limit")
		var admitted atomic.Int32
		var rejected atomic.Int32
		errorsSeen := make(chan error, 20)
		var wait sync.WaitGroup
		for index := 0; index < 20; index++ {
			wait.Add(1)
			go func(index int) {
				defer wait.Done()
				_, created, err := svc.BeginDurableAIJob(context.Background(), auth, aiJobTestRequest(
					fmt.Sprintf("admission-concurrent-%d", index), fmt.Sprintf("fingerprint-concurrent-%d", index),
				))
				switch {
				case err == nil && created:
					admitted.Add(1)
				case errors.Is(err, ErrAIJobQueueCapacityExceeded):
					rejected.Add(1)
				default:
					errorsSeen <- err
				}
			}(index)
		}
		wait.Wait()
		close(errorsSeen)
		for err := range errorsSeen {
			t.Errorf("unexpected admission error: %v", err)
		}
		if admitted.Load() != 1 || rejected.Load() != 19 {
			t.Fatalf("admitted=%d rejected=%d", admitted.Load(), rejected.Load())
		}
	})
}

func TestAIJobAdmissionCapacityReturnsWhenJobLeavesQueue(t *testing.T) {
	forEachAIJobRepository(t, func(t *testing.T, repo Repository) {
		svc := newAIJobTestService(t, repo)
		base := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
		svc.now = func() time.Time { return base }
		if err := svc.SetAIJobAdmissionLimits(AIJobAdmissionLimits{Principal: 1}); err != nil {
			t.Fatal(err)
		}
		auth := aiJobTestAuth("tenant-capacity", "principal-capacity")
		if _, _, err := svc.BeginDurableAIJob(context.Background(), auth, aiJobTestRequest("capacity-first", "capacity-first")); err != nil {
			t.Fatal(err)
		}
		claimed, err := svc.ClaimReadyAIJobs(context.Background(), "capacity-worker", time.Minute, 1)
		if err != nil || len(claimed) != 1 {
			t.Fatalf("claimed=%+v err=%v", claimed, err)
		}
		if _, err := svc.TransitionAIJob(context.Background(), claimed[0].ID, claimed[0].StatusVersion, claimed[0].FenceToken, AIJobStatusRunning, ""); err != nil {
			t.Fatal(err)
		}
		if _, created, err := svc.BeginDurableAIJob(context.Background(), auth, aiJobTestRequest("capacity-second", "capacity-second")); err != nil || !created {
			t.Fatalf("second created=%t err=%v", created, err)
		}
	})
}
