package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestGatewayLastCandidateFailureMatchesCooldownRuleAndPreservesBody(t *testing.T) {
	const upstreamError = `{"error":{"type":"upstream_error","message":"synthetic upstream failure"}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(upstreamError))
	}))
	defer upstream.Close()

	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name: "cooldown provider", Type: controlplane.ProviderTypeOpenAICompatible,
		BaseURL: upstream.URL + "/v1", Status: controlplane.ProviderStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	schedulable := true
	account, err := control.CreateProviderAccount(context.Background(), "tester", controlplane.ProviderAccountRequest{
		ProviderID: provider.ID, Name: "cooldown account", Platform: controlplane.ProviderTypeOpenAICompatible,
		AuthType: controlplane.ProviderAuthAPIKey, Status: controlplane.AccountStatusActive,
		Schedulable: &schedulable, Priority: 10, Concurrency: 1, RateMultiplier: 1,
		Models: []string{"upstream-model"}, Secret: "cooldown-secret",
		TempUnschedulableRules: []controlplane.ProviderAccountTempUnschedulableRule{{
			StatusCode: http.StatusInternalServerError, Keywords: []string{"synthetic upstream failure"}, DurationMinutes: 10,
		}},
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(): %v", err)
	}
	createGatewayTestModelAndRoutes(t, control, "cooldown-model", "default", []gatewayTestRoute{{
		account: account, upstreamModel: "upstream-model", priority: 10,
	}})
	key, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name: "cooldown key", ModelAllowlist: []string{"cooldown-model"}, QPSLimit: 10, MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"cooldown-model","messages":[{"role":"user","content":"ping"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("gateway response status=%d body=%q", rec.Code, rec.Body.String())
	}
	var responseBody map[string]map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &responseBody); err != nil {
		t.Fatalf("decode gateway response: %v", err)
	}
	if responseBody["error"]["type"] != "upstream_error" || responseBody["error"]["message"] != "synthetic upstream failure" {
		t.Fatalf("gateway response body=%q", rec.Body.String())
	}
	accounts, err := control.ListProviderAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListProviderAccounts(): %v", err)
	}
	var cooled controlplane.ProviderAccount
	for _, candidate := range accounts {
		if candidate.ID == account.ID {
			cooled = candidate
			break
		}
	}
	if cooled.CooldownUntil == nil || time.Until(*cooled.CooldownUntil) < 9*time.Minute {
		t.Fatalf("matched cooldown was not applied: %+v", cooled)
	}
	if !strings.Contains(cooled.TempUnschedulableReason, `keyword="synthetic upstream failure"`) {
		t.Fatalf("matched cooldown reason missing: %q", cooled.TempUnschedulableReason)
	}
}
