package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMemoryAIJobReadyIndexContract(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, time.July, 15, 8, 0, 0, 0, time.UTC)
	index := NewMemoryAIJobReadyIndex()
	principalAOld := readyIndexTestEntry("job-a-old", "principal-a", 1, base.Add(-3*time.Minute))
	principalANew := readyIndexTestEntry("job-a-new", "principal-a", 1, base.Add(-2*time.Minute))
	principalB := readyIndexTestEntry("job-b", "principal-b", 1, base.Add(-time.Minute))
	future := readyIndexTestEntry("job-future", "principal-c", 1, base.Add(time.Minute))
	for _, entry := range []AIJobReadyEntry{principalAOld, principalANew, principalB, future} {
		if err := index.Register(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}

	candidates, err := index.Candidates(ctx, AIJobReadyQuery{ReadyAt: base, Limit: 2})
	if err != nil || len(candidates) != 2 || candidates[0].JobID != principalAOld.JobID || candidates[1].JobID != principalB.JobID {
		t.Fatalf("fair candidates=%+v err=%v", candidates, err)
	}
	assertAIJobReadyCount(t, index, AIJobReadyScope{Level: AIJobReadyScopeAll}, 4)
	assertAIJobReadyCount(t, index, AIJobReadyScope{Level: AIJobReadyScopeProfile, ProfileScope: "platform"}, 4)
	assertAIJobReadyCount(t, index, AIJobReadyScope{Level: AIJobReadyScopeTenant, ProfileScope: "platform", TenantID: "tenant-a"}, 4)
	assertAIJobReadyCount(t, index, aiJobReadyScopeForEntry(AIJobReadyScopePrincipal, principalAOld), 2)

	newer := principalAOld
	newer.StatusVersion = 2
	newer.FenceToken = 1
	if err := index.Register(ctx, newer); err != nil {
		t.Fatal(err)
	}
	if err := index.Remove(ctx, principalAOld.reference()); err != nil {
		t.Fatal(err)
	}
	current, err := index.Candidates(ctx, AIJobReadyQuery{ReadyAt: base, Limit: 10})
	if err != nil || !containsReadyJobVersion(current, newer.JobID, newer.StatusVersion) {
		t.Fatalf("stale remove deleted newer entry: candidates=%+v err=%v", current, err)
	}
	if err := index.Register(ctx, principalAOld); err != nil {
		t.Fatalf("stale register should be ignored: %v", err)
	}
	conflict := newer
	conflict.Priority++
	if err := index.Register(ctx, conflict); !errors.Is(err, ErrAIJobReadyIndexConflict) {
		t.Fatalf("same-version conflict error=%v", err)
	}
	if err := index.Remove(ctx, newer.reference()); err != nil {
		t.Fatal(err)
	}
	assertAIJobReadyCount(t, index, aiJobReadyScopeForEntry(AIJobReadyScopePrincipal, principalAOld), 1)
}

func TestNewAIJobReadyEntryRejectsPayloadStateAndUsesLeaseDeadline(t *testing.T) {
	base := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
	leaseUntil := base.Add(time.Minute)
	job := AIJob{
		ID: "job-ready", Status: AIJobStatusDispatching, StatusVersion: 2, FenceToken: 1,
		ProfileScope: "platform", TenantID: "tenant-a", PrincipalID: "principal-a",
		RequestPayload: "must-not-be-indexed", RequestPayloadCiphertext: "must-not-be-indexed",
		QueueLeaseUntil: &leaseUntil, NextEligibleAt: base, CreatedAt: base,
	}
	entry, err := newAIJobReadyEntry(job)
	if err != nil || !entry.ReadyAt.Equal(leaseUntil) || entry.StatusVersion != job.StatusVersion || entry.FenceToken != job.FenceToken {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
	payload, err := json.Marshal(entry)
	if err != nil || strings.Contains(string(payload), "must-not-be-indexed") {
		t.Fatalf("ready entry leaked request payload: %s err=%v", payload, err)
	}
}

func readyIndexTestEntry(jobID, principalID string, version int, readyAt time.Time) AIJobReadyEntry {
	return AIJobReadyEntry{
		JobID: jobID, Status: AIJobStatusQueued, StatusVersion: version, ProfileScope: "platform", TenantID: "tenant-a",
		CredentialSource: "api_key", PrincipalType: "customer", PrincipalID: principalID,
		Priority: 1, ReadyAt: readyAt, CreatedAt: readyAt.Add(-time.Minute),
	}
}

func assertAIJobReadyCount(t *testing.T, index AIJobReadyIndex, scope AIJobReadyScope, want int64) {
	t.Helper()
	count, err := index.Count(context.Background(), scope)
	if err != nil || count != want {
		t.Fatalf("ready count scope=%+v count=%d want=%d err=%v", scope, count, want, err)
	}
}

func containsReadyJobVersion(entries []AIJobReadyEntry, jobID string, version int) bool {
	for _, entry := range entries {
		if entry.JobID == jobID && entry.StatusVersion == version {
			return true
		}
	}
	return false
}
