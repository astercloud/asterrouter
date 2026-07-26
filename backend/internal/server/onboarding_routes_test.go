package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
	"github.com/pelletier/go-toml/v2"
)

func TestOnboardingHTTPJourneyPerformsGovernedVerification(t *testing.T) {
	const providerSecret = "official-provider-secret"
	const upstreamModel = "upstream-model"
	var modelRequests atomic.Int64
	var inferenceRequests atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+providerSecret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			modelRequests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"upstream-model","object":"model"}]}`)
		case "/v1/responses":
			inferenceRequests.Add(1)
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte(`"model":"upstream-model"`)) || bytes.Contains(body, []byte(providerSecret)) {
				http.Error(w, "invalid routed request", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Request-ID", "upstream-onboarding-1")
			_, _ = io.WriteString(w, `{"id":"resp_onboarding","object":"response","status":"completed","model":"upstream-model","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":4,"output_tokens":1,"total_tokens":5}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	handler, control := newTestRuntime(t, RuntimeConfig{AdminToken: "admin-token"})
	session := createHTTPOnboardingSession(t, handler, "journey-session-idempotency")

	sourcePayload := map[string]any{
		"provider_name": "Primary", "provider_type": "openai_compatible", "base_url": upstream.URL + "/v1",
		"account_name": "Primary account", "auth_type": "api_key", "secret": providerSecret, "upstream_model": upstreamModel,
	}
	var source struct {
		Session controlplane.OnboardingSession `json:"session"`
	}
	requestOnboardingJSON(t, handler, http.MethodPost, "/api/v1/onboarding/sessions/"+session.ID+"/model-source", sourcePayload, "", &source)
	if source.Session.CurrentStep != controlplane.OnboardingStepModelSource || modelRequests.Load() != 1 {
		t.Fatalf("source session=%+v model_requests=%d", source.Session, modelRequests.Load())
	}

	publishedPayload := map[string]any{
		"model_id": "team-model", "name": "Team model", "modality": "chat", "upstream_model": upstreamModel,
	}
	var published controlplane.OnboardingPublishedModelResult
	requestOnboardingJSON(t, handler, http.MethodPost, "/api/v1/onboarding/sessions/"+session.ID+"/published-model", publishedPayload, "", &published)
	if published.Route.UpstreamFormat != controlplane.UpstreamFormatOpenAIResponses {
		t.Fatalf("published result=%+v", published)
	}

	var apiKey controlplane.OnboardingAPIKeyResult
	requestOnboardingJSON(t, handler, http.MethodPost, "/api/v1/onboarding/sessions/"+session.ID+"/api-key", map[string]any{
		"name": "Engineering", "concurrency_limit": 2, "monthly_token_limit": 10000,
	}, "", &apiKey)
	if apiKey.Credential == "" || apiKey.APIKey.ID == "" {
		t.Fatalf("API key result=%+v", apiKey)
	}

	configRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/api-keys/"+apiKey.APIKey.ID+"/client-config?client=codex&model=team-model", nil)
	configRequest.Header.Set("Authorization", "Bearer admin-token")
	configRequest.Host = "router.example.test"
	configRecorder := httptest.NewRecorder()
	handler.ServeHTTP(configRecorder, configRequest)
	if configRecorder.Code != http.StatusOK {
		t.Fatalf("client config status=%d body=%s", configRecorder.Code, configRecorder.Body.String())
	}
	var configEnvelope struct {
		Data controlplane.APIKeyClientConfig `json:"data"`
	}
	if err := json.Unmarshal(configRecorder.Body.Bytes(), &configEnvelope); err != nil {
		t.Fatalf("decode client config: %v", err)
	}
	var codexConfig struct {
		ModelProvider  string `toml:"model_provider"`
		Model          string `toml:"model"`
		ModelProviders map[string]struct {
			BaseURL string `toml:"base_url"`
			EnvKey  string `toml:"env_key"`
			WireAPI string `toml:"wire_api"`
		} `toml:"model_providers"`
	}
	if err := toml.Unmarshal([]byte(configEnvelope.Data.Content), &codexConfig); err != nil {
		t.Fatalf("parse client TOML: %v\n%s", err, configEnvelope.Data.Content)
	}
	provider := codexConfig.ModelProviders["asterrouter"]
	if strings.Contains(configRecorder.Body.String(), apiKey.Credential) || codexConfig.ModelProvider != "asterrouter" || codexConfig.Model != "team-model" || provider.BaseURL != "http://router.example.test/v1" || provider.EnvKey != "ASTERROUTER_API_KEY" || provider.WireAPI != "responses" {
		t.Fatalf("client config leaked or is incomplete: %s", configRecorder.Body.String())
	}

	verificationPayload := map[string]any{"client": "codex", "model": "team-model", "credential": apiKey.Credential}
	var verification controlplane.OnboardingVerificationResult
	requestOnboardingJSON(t, handler, http.MethodPost, "/api/v1/onboarding/sessions/"+session.ID+"/verification", verificationPayload, "journey-verification-idempotency", &verification)
	if verification.Session.Status != controlplane.OnboardingStatusCompleted || verification.Verification.Status != "success" || verification.Verification.OperationID == "" || verification.Verification.TraceID == "" {
		t.Fatalf("verification result=%+v", verification)
	}
	if inferenceRequests.Load() != 1 {
		t.Fatalf("inference requests=%d, want 1", inferenceRequests.Load())
	}

	var replay controlplane.OnboardingVerificationResult
	requestOnboardingJSON(t, handler, http.MethodPost, "/api/v1/onboarding/sessions/"+session.ID+"/verification", map[string]any{}, "journey-verification-idempotency", &replay)
	if replay.Verification.OperationID != verification.Verification.OperationID || inferenceRequests.Load() != 1 {
		t.Fatalf("verification replay=%+v inference_requests=%d", replay, inferenceRequests.Load())
	}
	if replay.Verification.Model != "team-model" {
		t.Fatalf("verification replay model=%q, want team-model", replay.Verification.Model)
	}
	usage, err := control.UsageReport(context.Background(), 10)
	if err != nil || len(usage.Recent) != 1 || usage.Recent[0].OperationID != verification.Verification.OperationID || usage.Recent[0].APIKeyID != apiKey.APIKey.ID {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
	traces, err := control.ListGatewayTraces(context.Background(), 10)
	if err != nil || len(traces) != 1 || traces[0].ID != verification.Verification.TraceID || traces[0].RouteID != published.Route.ID {
		t.Fatalf("traces=%+v err=%v", traces, err)
	}
	audits, err := control.ListAuditLogs(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, audit := range audits {
		if strings.Contains(audit.Summary, providerSecret) || strings.Contains(audit.Summary, apiKey.Credential) || strings.Contains(audit.Summary, "Reply with OK") {
			t.Fatalf("audit leaked verification content: %+v", audit)
		}
	}
}

func TestNormalizedPublicHTTPBaseRejectsUnsafeAddresses(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "domain", value: "https://router.example.test/edge/", want: "https://router.example.test/edge"},
		{name: "local port", value: "http://127.0.0.1:8080", want: "http://127.0.0.1:8080"},
		{name: "userinfo", value: "https://operator@router.example.test", wantErr: true},
		{name: "query", value: "https://router.example.test?target=external", wantErr: true},
		{name: "fragment", value: "https://router.example.test/#config", wantErr: true},
		{name: "invalid port", value: "https://router.example.test:70000", wantErr: true},
		{name: "missing host", value: "https:///v1", wantErr: true},
		{name: "unsupported scheme", value: "file:///tmp/router", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizedPublicHTTPBase(test.value)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("normalizedPublicHTTPBase(%q)=(%q, %v), want (%q, error=%v)", test.value, got, err, test.want, test.wantErr)
			}
		})
	}
}

func TestWriteOnboardingErrorDoesNotExposeInternalFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	writeOnboardingError(context, errors.New("database password appeared in driver error"))
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "database password") {
		t.Fatalf("internal error response status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(context.Errors) != 1 {
		t.Fatalf("recorded context errors=%d, want 1", len(context.Errors))
	}

	recorder = httptest.NewRecorder()
	context, _ = gin.CreateTestContext(recorder)
	writeOnboardingError(context, fmtOnboardingInput("client is not supported"))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "client is not supported") {
		t.Fatalf("input error response status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestOnboardingHTTPRejectsMissingAdminAuthenticationAndWrongCredential(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{AdminToken: "admin-token"})
	unauthorized := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/sessions", nil)
	unauthorized.Header.Set("Idempotency-Key", "unauthorized-session")
	unauthorizedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedRecorder, unauthorized)
	if unauthorizedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorizedRecorder.Code, unauthorizedRecorder.Body.String())
	}

	key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{Name: "application", ModelAllowlist: []string{"model-a"}})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(clientVerificationRequest{Client: controlplane.ClientOpenAISDK, Model: "model-a", Credential: "wrong-credential"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/api-keys/"+key.Record.ID+"/client-verifications", bytes.NewReader(payload))
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "wrong-credential-verification")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "credential does not match API key") {
		t.Fatalf("wrong credential status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	usage, err := control.UsageReport(context.Background(), 10)
	if err != nil || len(usage.Recent) != 0 {
		t.Fatalf("wrong credential produced usage=%+v err=%v", usage, err)
	}
}

func TestCompatibilityManifestHTTPIsAuthenticatedReadOnlyProjection(t *testing.T) {
	handler, _ := newTestRuntime(t, RuntimeConfig{AdminToken: "admin-token"})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/onboarding/compatibility-records", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("compatibility records status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data controlplane.CompatibilityManifest `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.SchemaVersion != controlplane.CompatibilitySchemaVersion || response.Data.RouterVersion != "test" || len(response.Data.Records) != 12 {
		t.Fatalf("compatibility manifest=%+v", response.Data)
	}
	for _, record := range response.Data.Records {
		if record.RouterVersion != response.Data.RouterVersion || record.EvidenceLevel == controlplane.CompatibilityEvidenceSDKRuntime {
			t.Fatalf("compatibility record overstates evidence: %+v", record)
		}
	}

	unauthorized := httptest.NewRequest(http.MethodGet, "/api/v1/onboarding/compatibility-records", nil)
	unauthorizedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedRecorder, unauthorized)
	if unauthorizedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorizedRecorder.Code, unauthorizedRecorder.Body.String())
	}

	write := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/compatibility-records", nil)
	write.Header.Set("Authorization", "Bearer admin-token")
	writeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(writeRecorder, write)
	if writeRecorder.Code != http.StatusNotFound {
		t.Fatalf("write status=%d body=%s", writeRecorder.Code, writeRecorder.Body.String())
	}
}

func TestAPIKeyClientVerificationProtocolMatrixRecordsSuccessAndFailure(t *testing.T) {
	const providerSecret = "matrix-provider-secret"
	const upstreamModel = "matrix-upstream"
	var inferenceRequests atomic.Int64
	var failInference atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+providerSecret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"matrix-upstream","object":"model"}]}`)
		case "/v1/responses":
			inferenceRequests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			if failInference.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"error":{"type":"upstream_failure","message":"synthetic failure"}}`)
				return
			}
			_, _ = io.WriteString(w, `{"id":"resp_matrix","object":"response","status":"completed","model":"matrix-upstream","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":4,"output_tokens":1,"total_tokens":5}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	handler, control := newTestRuntime(t, RuntimeConfig{AdminToken: "admin-token"})
	apiKey := createHTTPOnboardingAPIKey(t, handler, upstream.URL, providerSecret, upstreamModel, "matrix-model", "matrix")
	clients := []string{
		controlplane.ClientCodex,
		controlplane.ClientClaudeCode,
		controlplane.ClientOpenAISDK,
		controlplane.ClientAnthropicSDK,
	}
	for _, client := range clients {
		var result controlplane.ClientVerificationResult
		requestOnboardingJSON(t, handler, http.MethodPost, "/api/v1/admin/api-keys/"+apiKey.APIKey.ID+"/client-verifications", clientVerificationRequest{
			Client: client, Model: "matrix-model", Credential: apiKey.Credential,
		}, "matrix-success-"+client, &result)
		if result.Status != "success" || result.Client != client || result.OperationID == "" || result.TraceID == "" || result.HTTPStatus != http.StatusOK {
			t.Fatalf("client %s verification=%+v", client, result)
		}
	}

	failInference.Store(true)
	var failed controlplane.ClientVerificationResult
	requestOnboardingJSON(t, handler, http.MethodPost, "/api/v1/admin/api-keys/"+apiKey.APIKey.ID+"/client-verifications", clientVerificationRequest{
		Client: controlplane.ClientOpenAISDK, Model: "matrix-model", Credential: apiKey.Credential,
	}, "matrix-failed-upstream", &failed)
	if failed.Status != "failed" || failed.OperationID == "" || failed.TraceID == "" || failed.ErrorCode == "" || failed.RecoveryAction != "check_route_and_provider_health" {
		t.Fatalf("failed client verification=%+v", failed)
	}
	if inferenceRequests.Load() != int64(len(clients)+1) {
		t.Fatalf("inference requests=%d, want %d", inferenceRequests.Load(), len(clients)+1)
	}

	usage, err := control.UsageReport(context.Background(), 20)
	if err != nil || len(usage.Recent) != len(clients)+1 {
		t.Fatalf("usage records=%+v err=%v", usage.Recent, err)
	}
	traces, err := control.ListGatewayTraces(context.Background(), 20)
	if err != nil || len(traces) != len(clients)+1 {
		t.Fatalf("gateway traces=%+v err=%v", traces, err)
	}
	audits, err := control.ListAuditLogs(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	verificationAudits := 0
	failedAudit := false
	for _, audit := range audits {
		if audit.Action == "verify" && audit.ResourceType == "api_key" && audit.ResourceID == apiKey.APIKey.ID {
			verificationAudits++
			failedAudit = failedAudit || strings.Contains(audit.Summary, "failed")
		}
	}
	if verificationAudits != len(clients)+1 || !failedAudit {
		t.Fatalf("verification audits=%d failed_audit=%t audits=%+v", verificationAudits, failedAudit, audits)
	}
}

func createHTTPOnboardingSession(t *testing.T, handler http.Handler, idempotencyKey string) controlplane.OnboardingSession {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/sessions", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("Idempotency-Key", idempotencyKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("create onboarding session status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data controlplane.OnboardingSession `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}

func createHTTPOnboardingAPIKey(t *testing.T, handler http.Handler, upstreamURL, providerSecret, upstreamModel, publicModel, prefix string) controlplane.OnboardingAPIKeyResult {
	t.Helper()
	session := createHTTPOnboardingSession(t, handler, prefix+"-session-idempotency")
	var source controlplane.OnboardingModelSourceResult
	requestOnboardingJSON(t, handler, http.MethodPost, "/api/v1/onboarding/sessions/"+session.ID+"/model-source", controlplane.OnboardingModelSourceRequest{
		ProviderName: "Matrix source", ProviderType: controlplane.ProviderTypeOpenAICompatible, BaseURL: upstreamURL + "/v1",
		AccountName: "Matrix account", AuthType: controlplane.ProviderAuthAPIKey, Secret: providerSecret, UpstreamModel: upstreamModel,
	}, "", &source)
	var published controlplane.OnboardingPublishedModelResult
	requestOnboardingJSON(t, handler, http.MethodPost, "/api/v1/onboarding/sessions/"+session.ID+"/published-model", controlplane.OnboardingPublishedModelRequest{
		ModelID: publicModel, Name: "Matrix model", Modality: "chat", UpstreamModel: upstreamModel,
	}, "", &published)
	var apiKey controlplane.OnboardingAPIKeyResult
	requestOnboardingJSON(t, handler, http.MethodPost, "/api/v1/onboarding/sessions/"+session.ID+"/api-key", controlplane.OnboardingAPIKeyRequest{
		Name: "Matrix application", MonthlyTokenLimit: 10000,
	}, "", &apiKey)
	return apiKey
}

func requestOnboardingJSON(t *testing.T, handler http.Handler, method, path string, payload any, idempotencyKey string, destination any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s %s status=%d body=%s", method, path, recorder.Code, recorder.Body.String())
	}
	var response httpx.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(response.Data)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, destination); err != nil {
		t.Fatal(err)
	}
}
