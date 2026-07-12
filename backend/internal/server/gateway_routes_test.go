package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestGatewayModelsRequiresAPIKey(t *testing.T) {
	handler := newTestHandler(t, config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayModelsUsesAPIKeyAllowlist(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	if _, err := control.CreateGatewayModel(context.Background(), "tester", controlplane.GatewayModelRequest{ModelID: "gpt-4o-mini", Name: "GPT", Status: controlplane.GatewayModelStatusActive}); err != nil {
		t.Fatalf("CreateGatewayModel(): %v", err)
	}
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != "gpt-4o-mini" {
		t.Fatalf("unexpected models: %+v", resp.Data)
	}
}

func TestGatewayChatCompletionAuthorizesModelAndAudits(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	audit, err := control.ListAuditLogs(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	for _, event := range audit {
		if event.ResourceType == "gateway_call" && event.Action == "invoke" {
			return
		}
	}
	t.Fatalf("gateway audit event not found: %+v", audit)
}

func TestGatewayChatCompletionEnforcesQPSLimitAndRecordsTrace(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "gateway limited",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          1,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	for i := 0; i < 2; i++ {
		body := bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+created.Key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if i == 0 && rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("first status = %d body=%s", rec.Code, rec.Body.String())
		}
		if i == 1 {
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("second status = %d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "rate_limit_exceeded") {
				t.Fatalf("rate limit error not returned: %s", rec.Body.String())
			}
		}
	}

	usage, err := control.UsageReport(context.Background(), 10)
	if err != nil {
		t.Fatalf("UsageReport(): %v", err)
	}
	var foundUsage bool
	for _, record := range usage.Recent {
		if record.ErrorType == "rate_limit_exceeded" && record.Status == "error" {
			foundUsage = true
		}
	}
	if !foundUsage {
		t.Fatalf("rate limited usage record not found: %+v", usage.Recent)
	}

	traces, err := control.ListGatewayTraces(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListGatewayTraces(): %v", err)
	}
	for _, trace := range traces {
		if trace.ErrorType == "rate_limit_exceeded" && trace.HTTPStatus == http.StatusTooManyRequests {
			return
		}
	}
	t.Fatalf("rate limited trace not found: %+v", traces)
}

func TestGatewayChatCompletionEnforcesWorkspaceKeyBudgetAndRecordsTrace(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	policy, err := control.CreateGovernancePolicy(context.Background(), "tester", controlplane.GovernancePolicyRequest{
		Name:               "Workspace key budget",
		ScopeType:          controlplane.GovernancePolicyScopeGlobal,
		MonthlyBudgetCents: 100,
		OverageAction:      controlplane.GovernancePolicyOverageBlock,
		Status:             controlplane.GovernancePolicyStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateGovernancePolicy(): %v", err)
	}
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "gateway budget limited",
		PolicyID:          policy.ID,
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := control.AuthorizeGatewayModel(context.Background(), created.Key, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := control.RecordGatewayUsage(context.Background(), auth, controlplane.GatewayUsageInput{
		Model:     "gpt-4o-mini",
		Status:    "forwarded",
		CostCents: 100,
	}); err != nil {
		t.Fatalf("RecordGatewayUsage(): %v", err)
	}

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("budget status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "workspace key monthly budget exceeded") {
		t.Fatalf("budget error not returned: %s", rec.Body.String())
	}

	usage, err := control.UsageReport(context.Background(), 10)
	if err != nil {
		t.Fatalf("UsageReport(): %v", err)
	}
	var foundUsage bool
	for _, record := range usage.Recent {
		if record.ErrorType == "budget_exceeded" && record.Status == "error" {
			foundUsage = true
		}
	}
	if !foundUsage {
		t.Fatalf("budget usage record not found: %+v", usage.Recent)
	}

	traces, err := control.ListGatewayTraces(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListGatewayTraces(): %v", err)
	}
	for _, trace := range traces {
		if trace.ErrorType == "budget_exceeded" && trace.HTTPStatus == http.StatusTooManyRequests {
			return
		}
	}
	t.Fatalf("budget trace not found: %+v", traces)
}

func TestGatewayChatCompletionRejectsDisallowedModel(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	body := bytes.NewBufferString(`{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayChatCompletionForwardsToConfiguredProvider(t *testing.T) {
	var gotAuthorization string
	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %s", r.URL.Path)
		}
		gotAuthorization = r.Header.Get("Authorization")
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream payload: %v", err)
		}
		gotModel = payload.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"upstream-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"upstream-ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	handler, control := newTestRuntime(t, config.Config{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name:    "test provider",
		Type:    "openai_compatible",
		BaseURL: upstream.URL + "/v1",
		Status:  "active",
		Models:  []string{"upstream-gpt"},
		APIKey:  "upstream-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	account := createGatewayTestAccount(t, control, provider, "upstream-gpt", "upstream-secret", 10, 3)
	createGatewayTestModelAndRoutes(t, control, "gpt-4o-mini", "default", []gatewayTestRoute{{account: account, upstreamModel: "upstream-gpt", priority: 10}})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotAuthorization != "Bearer upstream-secret" {
		t.Fatalf("upstream authorization = %q", gotAuthorization)
	}
	if gotModel != "upstream-gpt" {
		t.Fatalf("upstream model = %q", gotModel)
	}
	if !strings.Contains(rec.Body.String(), "upstream-ok") {
		t.Fatalf("upstream response not returned: %s", rec.Body.String())
	}
}

func TestGatewayChatCompletionRoutesThroughProviderAccountPool(t *testing.T) {
	var gotAuthorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %s", r.URL.Path)
		}
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"upstream-account-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"account-ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":11}}`))
	}))
	defer upstream.Close()

	handler, control := newTestRuntime(t, config.Config{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name:    "account route provider",
		Type:    "openai_compatible",
		BaseURL: upstream.URL + "/v1",
		Status:  "active",
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	schedulable := true
	account, err := control.CreateProviderAccount(context.Background(), "tester", controlplane.ProviderAccountRequest{
		ProviderID:     provider.ID,
		Name:           "Primary account",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         controlplane.AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       10,
		Concurrency:    3,
		RateMultiplier: 1,
		Models:         []string{"gpt-4o-mini"},
		Secret:         "account-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(): %v", err)
	}
	createGatewayTestModelAndRoutes(t, control, "gpt-4o-mini", "default", []gatewayTestRoute{{account: account, upstreamModel: "gpt-4o-mini", priority: 10}})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotAuthorization != "Bearer account-secret" {
		t.Fatalf("upstream authorization = %q", gotAuthorization)
	}
	if !strings.Contains(rec.Body.String(), "account-ok") {
		t.Fatalf("upstream response not returned: %s", rec.Body.String())
	}
	usage, err := control.UsageReport(context.Background(), 10)
	if err != nil {
		t.Fatalf("UsageReport(): %v", err)
	}
	if len(usage.Recent) != 1 || usage.Recent[0].ProviderID != provider.ID || usage.Recent[0].ProviderAccountID != account.ID {
		t.Fatalf("usage route metadata not recorded: %+v", usage.Recent)
	}
	if usage.Recent[0].InputTokens != 7 || usage.Recent[0].OutputTokens != 11 {
		t.Fatalf("usage tokens not parsed: %+v", usage.Recent[0])
	}

	traces, err := control.ListGatewayTraces(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListGatewayTraces(): %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("trace count = %d traces=%+v", len(traces), traces)
	}
	trace := traces[0]
	if trace.ProviderID != provider.ID || trace.ProviderAccountID != account.ID || trace.RouteSource != "model_route" {
		t.Fatalf("trace route metadata not recorded: %+v", trace)
	}
	if trace.Status != "forwarded" || trace.HTTPStatus != http.StatusOK || trace.InputTokens != 7 || trace.OutputTokens != 11 {
		t.Fatalf("trace response metadata not recorded: %+v", trace)
	}

	traceReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-traces", nil)
	traceRec := httptest.NewRecorder()
	handler.ServeHTTP(traceRec, traceReq)
	if traceRec.Code != http.StatusOK {
		t.Fatalf("trace endpoint status = %d body=%s", traceRec.Code, traceRec.Body.String())
	}
	var traceResp struct {
		Data []controlplane.GatewayTrace `json:"data"`
	}
	if err := json.Unmarshal(traceRec.Body.Bytes(), &traceResp); err != nil {
		t.Fatalf("decode trace response: %v", err)
	}
	if len(traceResp.Data) != 1 || traceResp.Data[0].ProviderAccountID != account.ID {
		t.Fatalf("unexpected trace endpoint data: %+v", traceResp.Data)
	}
}

func TestGatewayChatCompletionFallsBackToNextAccountAfterUpstreamFailure(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_api_key","message":"revoked"}}`))
	}))
	defer failing.Close()
	var healthyAuthorization string
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthyAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"upstream-2","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"fallback-ok"},"finish_reason":"stop"}]}`))
	}))
	defer healthy.Close()

	handler, control := newTestRuntime(t, config.Config{})
	failingProvider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name:    "failing provider",
		Type:    "openai_compatible",
		BaseURL: failing.URL + "/v1",
		Status:  "active",
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "failing-provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(failing): %v", err)
	}
	healthyProvider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name:    "healthy provider",
		Type:    "openai_compatible",
		BaseURL: healthy.URL + "/v1",
		Status:  "active",
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "healthy-provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(healthy): %v", err)
	}
	schedulable := true
	primaryAccount, err := control.CreateProviderAccount(context.Background(), "tester", controlplane.ProviderAccountRequest{
		ProviderID:     failingProvider.ID,
		Name:           "Primary account",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         controlplane.AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       10,
		Concurrency:    3,
		RateMultiplier: 1,
		Models:         []string{"gpt-4o-mini"},
		Secret:         "failing-account-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(primary): %v", err)
	}
	backupAccount, err := control.CreateProviderAccount(context.Background(), "tester", controlplane.ProviderAccountRequest{
		ProviderID:     healthyProvider.ID,
		Name:           "Backup account",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         controlplane.AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       20,
		Concurrency:    3,
		RateMultiplier: 1,
		Models:         []string{"gpt-4o-mini"},
		Secret:         "healthy-account-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(backup): %v", err)
	}
	createGatewayTestModelAndRoutes(t, control, "gpt-4o-mini", "default", []gatewayTestRoute{
		{account: primaryAccount, upstreamModel: "gpt-4o-mini", priority: 10},
		{account: backupAccount, upstreamModel: "gpt-4o-mini", priority: 20},
	})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if healthyAuthorization != "Bearer healthy-account-secret" {
		t.Fatalf("expected fallback account to be used, got authorization = %q", healthyAuthorization)
	}
	if !strings.Contains(rec.Body.String(), "fallback-ok") {
		t.Fatalf("fallback response not returned: %s", rec.Body.String())
	}

	traces, err := control.ListGatewayTraces(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListGatewayTraces(): %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("trace count = %d traces=%+v", len(traces), traces)
	}
	if !strings.Contains(traces[0].RouteAttempts, `"outcome":"failed"`) || !strings.Contains(traces[0].RouteAttempts, `"outcome":"selected"`) {
		t.Fatalf("expected route attempts to record both failure and selection: %s", traces[0].RouteAttempts)
	}
}

func TestGatewayChatCompletionSkipsAccountAtConcurrencyCapacity(t *testing.T) {
	var busyAuthorization, freeAuthorization string
	busy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		busyAuthorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer busy.Close()
	free := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		freeAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"upstream-3","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"free-ok"},"finish_reason":"stop"}]}`))
	}))
	defer free.Close()

	handler, control := newTestRuntime(t, config.Config{})
	busyProvider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name:    "busy provider",
		Type:    "openai_compatible",
		BaseURL: busy.URL + "/v1",
		Status:  "active",
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "busy-provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(busy): %v", err)
	}
	freeProvider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name:    "free provider",
		Type:    "openai_compatible",
		BaseURL: free.URL + "/v1",
		Status:  "active",
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "free-provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(free): %v", err)
	}
	schedulable := true
	busyAccount, err := control.CreateProviderAccount(context.Background(), "tester", controlplane.ProviderAccountRequest{
		ProviderID:     busyProvider.ID,
		Name:           "Busy account",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         controlplane.AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       10,
		Concurrency:    1,
		RateMultiplier: 1,
		Models:         []string{"gpt-4o-mini"},
		Secret:         "busy-account-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(busy): %v", err)
	}
	freeAccount, err := control.CreateProviderAccount(context.Background(), "tester", controlplane.ProviderAccountRequest{
		ProviderID:     freeProvider.ID,
		Name:           "Free account",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         controlplane.AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       20,
		Concurrency:    3,
		RateMultiplier: 1,
		Models:         []string{"gpt-4o-mini"},
		Secret:         "free-account-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(free): %v", err)
	}
	createGatewayTestModelAndRoutes(t, control, "gpt-4o-mini", "default", []gatewayTestRoute{
		{account: busyAccount, upstreamModel: "gpt-4o-mini", priority: 10},
		{account: freeAccount, upstreamModel: "gpt-4o-mini", priority: 20},
	})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	// Occupy the busy account's only concurrency slot before sending the
	// request, so the gateway must skip it without dialing the busy upstream
	// at all and route to the free account instead.
	release, ok := control.TryAcquireProviderAccountSlot(busyAccount.ID, busyAccount.Concurrency)
	if !ok {
		t.Fatal("expected to occupy busy account's only slot")
	}
	defer release()

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if busyAuthorization != "" {
		t.Fatalf("expected busy upstream to never be dialed, got authorization = %q", busyAuthorization)
	}
	if freeAuthorization != "Bearer free-account-secret" {
		t.Fatalf("expected free account to be used, got authorization = %q", freeAuthorization)
	}
	if !strings.Contains(rec.Body.String(), "free-ok") {
		t.Fatalf("free account response not returned: %s", rec.Body.String())
	}

	traces, err := control.ListGatewayTraces(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListGatewayTraces(): %v", err)
	}
	if len(traces) != 1 || !strings.Contains(traces[0].RouteAttempts, `"outcome":"skipped"`) {
		t.Fatalf("expected route attempts to record a skipped candidate: %+v", traces)
	}
}

func TestGatewayChatCompletionRejectsOversizedRequestBody(t *testing.T) {
	handler := newTestHandler(t, config.Config{})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(strings.Repeat("x", gatewayRequestBodyLimit+1)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestEstimateGatewayRequestTokensIncludesCompletionLimit(t *testing.T) {
	body := []byte(`{"model":"test","max_completion_tokens":250}`)
	got := estimateGatewayRequestTokens(body)
	if got < 250+(len(body)+3)/4 {
		t.Fatalf("estimated tokens = %d", got)
	}
}

func TestGatewayChatCompletionPassesThroughUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	}))
	defer upstream.Close()

	handler, control := newTestRuntime(t, config.Config{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name:    "limited provider",
		Type:    "openai_compatible",
		BaseURL: upstream.URL + "/v1",
		Status:  "active",
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "upstream-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	account := createGatewayTestAccount(t, control, provider, "gpt-4o-mini", "upstream-secret", 10, 3)
	createGatewayTestModelAndRoutes(t, control, "gpt-4o-mini", "default", []gatewayTestRoute{{account: account, upstreamModel: "gpt-4o-mini", priority: 10}})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "rate_limit_error") {
		t.Fatalf("upstream error body not returned: %s", rec.Body.String())
	}
}

func TestGatewayChatCompletionStreamsConfiguredProvider(t *testing.T) {
	var gotAccept string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chunk-1\"}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	handler, control := newTestRuntime(t, config.Config{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name:    "stream provider",
		Type:    "openai_compatible",
		BaseURL: upstream.URL + "/v1",
		Status:  "active",
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "upstream-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	account := createGatewayTestAccount(t, control, provider, "gpt-4o-mini", "upstream-secret", 10, 3)
	createGatewayTestModelAndRoutes(t, control, "gpt-4o-mini", "default", []gatewayTestRoute{{account: account, upstreamModel: "gpt-4o-mini", priority: 10}})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotAccept != "text/event-stream" {
		t.Fatalf("upstream accept = %q", gotAccept)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "chunk-1") || !strings.Contains(rec.Body.String(), "[DONE]") {
		t.Fatalf("stream body not returned: %s", rec.Body.String())
	}
}

func TestGatewayChatCompletionRejectsStreamingWithoutProvider(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

type gatewayTestRoute struct {
	account       controlplane.ProviderAccount
	upstreamModel string
	priority      int
}

func createGatewayTestAccount(t *testing.T, control *controlplane.Service, provider controlplane.ProviderConnection, model string, secret string, priority int, concurrency int) controlplane.ProviderAccount {
	t.Helper()
	schedulable := true
	account, err := control.CreateProviderAccount(context.Background(), "tester", controlplane.ProviderAccountRequest{
		ProviderID: provider.ID, Name: provider.Name + " account", Platform: "openai_compatible", AuthType: "api_key",
		Status: controlplane.AccountStatusActive, Schedulable: &schedulable, Priority: priority,
		Concurrency: concurrency, RateMultiplier: 1, Models: []string{model}, Secret: secret,
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(%s): %v", provider.ID, err)
	}
	return account
}

func createGatewayTestModelAndRoutes(t *testing.T, control *controlplane.Service, modelID string, routeGroup string, routes []gatewayTestRoute) controlplane.GatewayModel {
	t.Helper()
	model, err := control.CreateGatewayModel(context.Background(), "tester", controlplane.GatewayModelRequest{
		ModelID: modelID, Name: modelID, DefaultRouteGroup: routeGroup, Status: controlplane.GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateGatewayModel(%s): %v", modelID, err)
	}
	for _, route := range routes {
		if _, err := control.CreateModelRoute(context.Background(), "tester", controlplane.ModelRouteRequest{
			GatewayModelID: model.ID, RouteGroup: routeGroup, ProviderAccountID: route.account.ID,
			UpstreamModel: route.upstreamModel, Priority: route.priority, Weight: 100, Status: controlplane.ModelRouteStatusActive,
		}); err != nil {
			t.Fatalf("CreateModelRoute(%s, %s): %v", modelID, route.account.ID, err)
		}
	}
	return model
}
