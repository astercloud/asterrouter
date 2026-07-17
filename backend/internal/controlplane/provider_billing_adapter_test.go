package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSub2APICompatibleBillingReaderInspectsWalletAndAggregateUsage(t *testing.T) {
	observedAt := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/usage" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer purchase-secret" {
			t.Fatalf("authorization header was not set")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"mode":"unrestricted","isValid":true,"unit":"USD","remaining":19.5,"balance":"19.500001",
			"usage":{"today":{"requests":3,"input_tokens":100,"output_tokens":20,"cache_creation_tokens":50,"cache_read_tokens":40,"cost":2.5,"actual_cost":1.234567},"total":{"requests":10,"input_tokens":500,"output_tokens":80,"cache_creation_tokens":120,"cache_read_tokens":200,"cost":8,"actual_cost":4.25}},
			"model_stats":[{"model":"claude-sonnet","requests":7,"input_tokens":350,"output_tokens":60,"cache_creation_tokens":100,"cache_read_tokens":180,"cost":6.5,"actual_cost":3.25}]
		}`))
	}))
	defer upstream.Close()

	reader := &sub2APICompatibleBillingReader{client: upstream.Client()}
	result, err := reader.Inspect(context.Background(), ProviderBillingReadTarget{
		BaseURL: upstream.URL + "/v1", Secret: "purchase-secret", ObservedAt: observedAt,
	})
	if err != nil {
		t.Fatalf("Inspect(): %v", err)
	}
	if result.AdapterID != ProviderBillingAdapterSub2APICompatible || result.DetectionStatus != ProviderBillingDetectionSchemaMatch || result.ContractVersion != "sub2api_v1_usage" {
		t.Fatalf("detection = %+v", result)
	}
	if result.Balance == nil || result.Balance.Kind != ProviderBalanceKindWallet || result.Balance.AmountMicros != 19_500_001 || result.Balance.Currency != "USD" {
		t.Fatalf("balance = %+v", result.Balance)
	}
	if !result.Capabilities.Balance || !result.Capabilities.AggregateUsage || result.Capabilities.UsageCostLines || result.Capabilities.IncrementalSync || result.Capabilities.PriceFeed {
		t.Fatalf("capabilities = %+v", result.Capabilities)
	}
	if len(result.UsageAggregates) != 3 || result.UsageAggregates[0].Scope != "today" || result.UsageAggregates[0].ActualCostMicros == nil || *result.UsageAggregates[0].ActualCostMicros != 1_234_567 || result.UsageAggregates[0].CacheReadTokens != 40 {
		t.Fatalf("aggregates = %+v", result.UsageAggregates)
	}
	modelAggregate := result.UsageAggregates[2]
	if modelAggregate.Scope != "model_30d" || modelAggregate.Model != "claude-sonnet" || modelAggregate.ActualCostMicros == nil || *modelAggregate.ActualCostMicros != 3_250_000 || modelAggregate.CacheReadTokens != 180 {
		t.Fatalf("model aggregate = %+v", modelAggregate)
	}
	if result.DiscoveredLines != 0 || result.EvidenceHash == "" || !containsString(result.Warnings, "aggregate_totals_are_not_billing_lines") {
		t.Fatalf("inspection evidence = %+v", result)
	}
}

func TestSub2APICompatibleBillingReaderDistinguishesQuotaFromWalletBalance(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"mode":"quota_limited","isValid":true,"unit":"USD","remaining":7.5,"balance":99,"quota":{"limit":10,"used":2.5,"remaining":7.5}}`))
	}))
	defer upstream.Close()

	result, err := (&sub2APICompatibleBillingReader{client: upstream.Client()}).Inspect(context.Background(), ProviderBillingReadTarget{
		BaseURL: upstream.URL, Secret: "secret", ObservedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Balance == nil || result.Balance.Kind != ProviderBalanceKindKeyQuota || result.Balance.AmountMicros != 7_500_000 {
		t.Fatalf("quota snapshot = %+v", result.Balance)
	}
	if !containsString(result.Warnings, "remaining_is_quota_not_wallet_balance") {
		t.Fatalf("warnings = %+v", result.Warnings)
	}
}

func TestSub2APICompatibleBillingReaderPreservesUnlimitedSubscriptionSemantics(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"mode":"unrestricted","isValid":false,"unit":"USD","remaining":-1}`))
	}))
	defer upstream.Close()

	result, err := (&sub2APICompatibleBillingReader{client: upstream.Client()}).Inspect(context.Background(), ProviderBillingReadTarget{
		BaseURL: upstream.URL, Secret: "secret", ObservedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Balance == nil || result.Balance.Kind != ProviderBalanceKindSubscription || !result.Balance.Unlimited || result.Balance.AmountMicros != 0 {
		t.Fatalf("subscription snapshot = %+v", result.Balance)
	}
	if !containsString(result.Warnings, "subscription_remaining_unlimited") || !containsString(result.Warnings, "account_key_reported_invalid") {
		t.Fatalf("warnings = %+v", result.Warnings)
	}
}

func TestSub2APICompatibleBillingReaderRejectsUnknownSchemaAndAuthenticationFailure(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       error
		contains   string
	}{
		{name: "schema mismatch", statusCode: http.StatusOK, body: `{"object":"usage"}`, want: ErrProviderBillingAdapterMismatch},
		{name: "not found", statusCode: http.StatusNotFound, body: `{}`, want: ErrProviderBillingAdapterMismatch},
		{name: "authentication", statusCode: http.StatusUnauthorized, body: `{"secret":"must-not-surface"}`, contains: "rejected the account API key"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
				_, _ = w.Write([]byte(test.body))
			}))
			defer upstream.Close()
			_, err := (&sub2APICompatibleBillingReader{client: upstream.Client()}).Inspect(context.Background(), ProviderBillingReadTarget{BaseURL: upstream.URL, Secret: "secret"})
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if test.contains != "" && (err == nil || !strings.Contains(err.Error(), test.contains) || strings.Contains(err.Error(), "must-not-surface")) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestProviderBillingAdapterRegistryAutoDetectionAndExplicitAdapter(t *testing.T) {
	registry := NewProviderBillingAdapterRegistry()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"mode":"unrestricted","isValid":true,"unit":"USD","balance":1}`))
	}))
	defer upstream.Close()
	target := ProviderBillingReadTarget{BaseURL: upstream.URL, Secret: "secret", ObservedAt: time.Now().UTC()}

	for _, adapterID := range []string{"", ProviderBillingAdapterAuto, ProviderBillingAdapterSub2APICompatible} {
		result, err := registry.Inspect(context.Background(), upstream.Client(), adapterID, target)
		if err != nil || result.AdapterID != ProviderBillingAdapterSub2APICompatible {
			t.Fatalf("adapter %q result=%+v err=%v", adapterID, result, err)
		}
	}
	if _, err := registry.Inspect(context.Background(), upstream.Client(), "unknown", target); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("unknown adapter error = %v", err)
	}
}

func TestInspectProviderBillingSourceUsesProcurementAccountSecretAndAudits(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer account-secret" {
			t.Fatalf("unexpected authorization header")
		}
		_, _ = w.Write([]byte(`{"mode":"unrestricted","isValid":true,"unit":"USD","balance":12}`))
	}))
	defer upstream.Close()

	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "billing-inspection-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Sub2API compatible", Type: "openai_compatible", BaseURL: upstream.URL + "/v1", Status: ProviderStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Purchase account", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Models: []string{"model"}, Secret: "account-secret", Concurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.InspectProviderBillingSource(ctx, "auditor", ProviderBillingSourceInspectionRequest{ProviderAccountID: account.ID})
	if err != nil {
		t.Fatalf("InspectProviderBillingSource(): %v", err)
	}
	if result.ProviderID != provider.ID || result.ProviderAccountID != account.ID || result.ProviderName != provider.Name || result.ProviderAccount != account.Name {
		t.Fatalf("source identity = %+v", result)
	}
	audit, err := repo.ListAuditLogs(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range audit {
		if entry.Actor == "auditor" && entry.Action == "inspect" && entry.ResourceType == "provider_billing_source" && entry.ResourceID == account.ID {
			found = true
		}
	}
	if !found {
		encoded, _ := json.Marshal(audit)
		t.Fatalf("billing inspection audit missing: %s", encoded)
	}
}

func TestDecimalJSONMicrosRoundsWithoutFloatingPointMoney(t *testing.T) {
	tests := []struct {
		value string
		want  int64
	}{
		{value: "0", want: 0},
		{value: "1.2345674", want: 1_234_567},
		{value: "1.2345675", want: 1_234_568},
		{value: "-0.0000005", want: -1},
		{value: "1e-6", want: 1},
		{value: `"2.5"`, want: 2_500_000},
	}
	for _, test := range tests {
		got, present, err := decimalJSONMicros(json.RawMessage(test.value))
		if err != nil || !present || got != test.want {
			t.Fatalf("decimalJSONMicros(%s) = %d, %t, %v; want %d", test.value, got, present, err, test.want)
		}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
