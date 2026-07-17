package controlplane

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProviderBillingSourceServicePersistsAggregateEvidenceWithoutBalance(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.July, 15, 21, 0, 0, 0, time.UTC)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"mode":"unrestricted","isValid":true,"unit":"EUR","usage":{"today":{"requests":2,"input_tokens":100,"output_tokens":20,"cache_read_tokens":60,"cost":1,"actual_cost":0.4}}}`))
	}))
	defer upstream.Close()

	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "billing-sync-test-secret")
	svc.now = func() time.Time { return now }
	provider, account := createProviderBillingSyncAccount(t, svc, upstream.URL, "success")
	source, err := svc.UpsertProviderBillingSource(ctx, "admin", ProviderBillingSourceRequest{
		ProviderAccountID: account.ID, AdapterID: ProviderBillingAdapterSub2APICompatible,
		Status: ProviderBillingSourceObserveOnly, AutomaticSyncEnabled: true, SyncIntervalSeconds: 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if source.ProviderID != provider.ID || source.Version != 1 || source.NextSyncAt == nil {
		t.Fatalf("created source=%+v", source)
	}
	now = now.Add(time.Second)
	result, err := svc.SyncProviderBillingSource(ctx, "admin", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Status != ProviderBillingSyncSucceeded || result.Balance != nil || len(result.Aggregates) != 1 || result.Aggregates[0].Currency != "EUR" {
		t.Fatalf("sync result=%+v", result)
	}
	if result.Aggregates[0].SourceID != source.ID || result.Aggregates[0].SyncRunID != result.Run.ID || result.Aggregates[0].ProviderAccountID != account.ID {
		t.Fatalf("aggregate identity=%+v", result.Aggregates[0])
	}
	evidence, err := svc.ProviderBillingSourceEvidence(ctx, source.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Runs) != 1 || len(evidence.Balances) != 0 || len(evidence.Aggregates) != 1 || evidence.Aggregates[0].ActualCostMicros == nil || *evidence.Aggregates[0].ActualCostMicros != 400_000 {
		t.Fatalf("evidence=%+v", evidence)
	}
	if evidence.Source.LastSuccessAt == nil || evidence.Source.NextSyncAt == nil || !evidence.Source.NextSyncAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("source schedule=%+v", evidence.Source)
	}
}

func TestNextProviderBillingSyncAtRequiresAutomaticSync(t *testing.T) {
	now := time.Date(2026, time.July, 16, 0, 30, 0, 0, time.UTC)
	manual := ProviderBillingSource{Status: ProviderBillingSourceObserveOnly, SyncIntervalSeconds: 3600}
	if next := nextProviderBillingSyncAt(manual, now, false); next != nil {
		t.Fatalf("manual source next sync=%v, want nil", next)
	}
	manual.AutomaticSyncEnabled = true
	if next := nextProviderBillingSyncAt(manual, now, false); next == nil || !next.Equal(now.Add(time.Hour)) {
		t.Fatalf("automatic source next sync=%v", next)
	}
	manual.Status = ProviderBillingSourceDisabled
	if next := nextProviderBillingSyncAt(manual, now, true); next != nil {
		t.Fatalf("disabled source next sync=%v, want nil", next)
	}
}

func TestProviderBillingSourceServicePersistsStableFailureCodeWithoutUpstreamBody(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.July, 15, 22, 0, 0, 0, time.UTC)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"secret":"must-not-persist","message":"vendor-private-error"}`))
	}))
	defer upstream.Close()

	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "billing-sync-test-secret")
	svc.now = func() time.Time { return now }
	_, account := createProviderBillingSyncAccount(t, svc, upstream.URL, "failure")
	source, err := svc.UpsertProviderBillingSource(ctx, "admin", ProviderBillingSourceRequest{
		ProviderAccountID: account.ID, AdapterID: ProviderBillingAdapterSub2APICompatible,
		Status: ProviderBillingSourceObserveOnly, AutomaticSyncEnabled: true, SyncIntervalSeconds: 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	result, err := svc.SyncProviderBillingSource(ctx, "admin", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Status != ProviderBillingSyncFailed || result.Run.ErrorCode != "upstream_auth_rejected" || len(result.Aggregates) != 0 || result.Balance != nil {
		t.Fatalf("failed sync result=%+v", result)
	}
	evidence, err := svc.ProviderBillingSourceEvidence(ctx, source.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	encoded := strings.ToLower(providerBillingEvidenceText(evidence))
	if strings.Contains(encoded, "must-not-persist") || strings.Contains(encoded, "vendor-private-error") {
		t.Fatalf("upstream body leaked into evidence: %s", encoded)
	}
	if evidence.Source.ConsecutiveFailures != 1 || evidence.Source.LastErrorCode != "upstream_auth_rejected" || len(evidence.Runs) != 1 || evidence.Runs[0].ErrorCode != "upstream_auth_rejected" {
		t.Fatalf("failure evidence=%+v", evidence)
	}
	logs, err := repo.ListAuditLogs(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, log := range logs {
		if log.ResourceID == source.ID && log.Action == "sync_failed" && strings.Contains(log.Summary, "upstream_auth_rejected") {
			found = true
		}
		if strings.Contains(log.Summary, "must-not-persist") || strings.Contains(log.Summary, "vendor-private-error") {
			t.Fatalf("upstream body leaked into audit: %+v", log)
		}
	}
	if !found {
		t.Fatalf("failure audit missing: %+v", logs)
	}
}

func TestProviderBillingSourceServiceCASAndDisabledManualSync(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.July, 15, 23, 0, 0, 0, time.UTC)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"mode":"unrestricted","isValid":true,"unit":"USD","balance":1}`))
	}))
	defer upstream.Close()

	svc := NewService(NewMemoryRepository(), "/v1", "billing-sync-test-secret")
	svc.now = func() time.Time { return now }
	_, account := createProviderBillingSyncAccount(t, svc, upstream.URL, "cas")
	created, err := svc.UpsertProviderBillingSource(ctx, "admin", ProviderBillingSourceRequest{ProviderAccountID: account.ID, Status: ProviderBillingSourceObserveOnly, SyncIntervalSeconds: 3600})
	if err != nil {
		t.Fatal(err)
	}
	staleVersion := created.Version - 1
	if _, err := svc.UpsertProviderBillingSource(ctx, "admin", ProviderBillingSourceRequest{ProviderAccountID: account.ID, Status: ProviderBillingSourceDisabled, SyncIntervalSeconds: 3600, Version: &staleVersion}); !errors.Is(err, ErrProviderBillingSourceConflict) {
		t.Fatalf("stale update error=%v", err)
	}
	version := created.Version
	disabled, err := svc.UpsertProviderBillingSource(ctx, "admin", ProviderBillingSourceRequest{ProviderAccountID: account.ID, Status: ProviderBillingSourceDisabled, SyncIntervalSeconds: 3600, Version: &version})
	if err != nil || disabled.Status != ProviderBillingSourceDisabled {
		t.Fatalf("disabled source=%+v err=%v", disabled, err)
	}
	if _, err := svc.SyncProviderBillingSource(ctx, "admin", disabled.ID); !errors.Is(err, ErrProviderBillingSourceDisabled) {
		t.Fatalf("disabled sync error=%v", err)
	}
}

func TestProviderBillingSourceScheduledBatchIsolatesFailures(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.July, 16, 0, 0, 0, 0, time.UTC)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"mode":"unrestricted","isValid":true,"unit":"USD","balance":2}`))
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer bad.Close()

	svc := NewService(NewMemoryRepository(), "/v1", "billing-sync-test-secret")
	svc.now = func() time.Time { return now }
	_, goodAccount := createProviderBillingSyncAccount(t, svc, good.URL, "batch-good")
	_, badAccount := createProviderBillingSyncAccount(t, svc, bad.URL, "batch-bad")
	for _, account := range []ProviderAccount{goodAccount, badAccount} {
		if _, err := svc.UpsertProviderBillingSource(ctx, "admin", ProviderBillingSourceRequest{ProviderAccountID: account.ID, Status: ProviderBillingSourceObserveOnly, AutomaticSyncEnabled: true, SyncIntervalSeconds: 3600}); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(time.Second)
	report, err := svc.SyncDueProviderBillingSources(ctx, "test-worker", 10)
	if err != nil {
		t.Fatal(err)
	}
	if report.Claimed != 2 || report.Succeeded != 1 || report.Failed != 1 || len(report.Results) != 2 {
		t.Fatalf("batch report=%+v", report)
	}
}

func createProviderBillingSyncAccount(t *testing.T, svc *Service, baseURL, suffix string) (ProviderConnection, ProviderAccount) {
	t.Helper()
	ctx := context.Background()
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Billing provider " + suffix, Type: "openai_compatible", BaseURL: baseURL,
		Status: ProviderStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Billing account " + suffix, Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Models: []string{"model"}, Secret: "account-secret-" + suffix, Concurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return provider, account
}

func providerBillingEvidenceText(evidence ProviderBillingSourceEvidence) string {
	parts := []string{evidence.Source.LastErrorCode, evidence.Source.DetectionStatus, evidence.Source.EvidenceHash}
	parts = append(parts, evidence.Source.Warnings...)
	for _, run := range evidence.Runs {
		parts = append(parts, run.ErrorCode, run.DetectionStatus, run.EvidenceHash)
		parts = append(parts, run.Warnings...)
	}
	return strings.Join(parts, " ")
}
