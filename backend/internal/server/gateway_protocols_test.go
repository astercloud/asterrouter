package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

type nativeProtocolFixture struct {
	handler http.Handler
	control *controlplane.Service
	key     string
	mu      sync.Mutex
	paths   []string
	headers []http.Header
	bodies  [][]byte
}

func newNativeProtocolFixture(t *testing.T) *nativeProtocolFixture {
	t.Helper()
	fixture := &nativeProtocolFixture{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fixture.mu.Lock()
		fixture.paths = append(fixture.paths, r.URL.Path)
		fixture.headers = append(fixture.headers, r.Header.Clone())
		fixture.bodies = append(fixture.bodies, append([]byte(nil), body...))
		fixture.mu.Unlock()
		if bytes.Contains(body, []byte("synthetic-failure")) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"type":"upstream_rate_limit","message":"synthetic upstream limit"}}`)
			return
		}
		stream := strings.Contains(r.Header.Get("Accept"), "text/event-stream")
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_1\",\"model\":\"native-upstream\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_1\",\"model\":\"native-upstream\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_1\",\"model\":\"native-upstream\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl_1","object":"chat.completion","model":"native-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
	}))
	t.Cleanup(upstream.Close)
	handler, control := newTestRuntime(t, RuntimeConfig{})
	fixture.handler, fixture.control = handler, control
	provider, err := control.CreateProvider(context.Background(), "test", controlplane.ProviderRequest{
		Name: "Native protocol provider", Type: "openai_compatible", BaseURL: upstream.URL + "/v1", Status: controlplane.ProviderStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "native-chat", Name: "Native chat", Modality: "chat", DefaultRouteGroup: "default", Status: controlplane.GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	account := createGatewayTestAccount(t, control, provider, "native-upstream", "provider-secret", 10, 4)
	if _, err := control.CreateModelRoute(context.Background(), "test", controlplane.ModelRouteRequest{
		GatewayModelID: model.ID, RouteGroup: "default", ProviderAccountID: account.ID, UpstreamModel: "native-upstream", Priority: 10, Weight: 100, Status: controlplane.ModelRouteStatusActive, UpstreamFormat: "openai_chat",
	}); err != nil {
		t.Fatal(err)
	}
	key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "native protocol caller", ModelAllowlist: []string{"native-chat"}, Scopes: []string{controlplane.GatewayScopeInvoke}, MonthlyTokenLimit: 10000,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.key = key.Key
	return fixture
}

func TestGatewayClientProtocolsTranslateThroughConfiguredUpstreamFormat(t *testing.T) {
	fixture := newNativeProtocolFixture(t)
	tests := []struct {
		name       string
		path       string
		credential string
		body       string
		wantPath   string
		wantHeader string
		wantValue  string
		wantBody   string
		wantClient string
	}{
		{name: "responses", path: "/v1/responses", credential: "Bearer ", body: `{"model":"native-chat","input":"hello","stream":false}`, wantPath: "/v1/chat/completions", wantHeader: "Authorization", wantValue: "Bearer provider-secret", wantBody: `"model":"native-upstream"`, wantClient: `"object":"response"`},
		{name: "anthropic", path: "/v1/messages", credential: "X-API-Key: ", body: `{"model":"native-chat","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`, wantPath: "/v1/chat/completions", wantHeader: "Authorization", wantValue: "Bearer provider-secret", wantBody: `"model":"native-upstream"`, wantClient: `"type":"message"`},
		{name: "gemini", path: "/v1beta/models/native-chat:generateContent", credential: "X-Goog-API-Key: ", body: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`, wantPath: "/v1/chat/completions", wantHeader: "Authorization", wantValue: "Bearer provider-secret", wantBody: `"model":"native-upstream"`, wantClient: `"candidates"`},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "native-json-"+test.name+"-"+string(rune('a'+index)))
			if strings.HasPrefix(test.credential, "Bearer") {
				request.Header.Set("Authorization", "Bearer "+fixture.key)
			} else {
				parts := strings.SplitN(test.credential, ": ", 2)
				request.Header.Set(parts[0], fixture.key)
			}
			response := httptest.NewRecorder()
			fixture.handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			fixture.mu.Lock()
			if len(fixture.paths) == 0 {
				fixture.mu.Unlock()
				t.Fatalf("upstream was not called; body=%s", response.Body.String())
			}
			path := fixture.paths[len(fixture.paths)-1]
			headers := fixture.headers[len(fixture.headers)-1]
			body := fixture.bodies[len(fixture.bodies)-1]
			fixture.mu.Unlock()
			if path != test.wantPath || headers.Get(test.wantHeader) != test.wantValue {
				t.Fatalf("upstream path=%q headers=%v", path, headers)
			}
			if !bytes.Contains(body, []byte(test.wantBody)) || !bytes.Contains(body, []byte(`"messages"`)) {
				t.Fatalf("rewritten body=%s missing %s", body, test.wantBody)
			}
			if !strings.Contains(response.Body.String(), test.wantClient) || !strings.Contains(response.Body.String(), "ok") {
				t.Fatalf("client response was not translated: %s", response.Body.String())
			}
		})
	}
}

func TestGatewayNativeProtocolsRecognizeSSETerminalEventsAndUsage(t *testing.T) {
	fixture := newNativeProtocolFixture(t)
	tests := []struct {
		name       string
		path       string
		body       string
		wantStream string
		set        func(*http.Request)
	}{
		{name: "responses", path: "/v1/responses", body: `{"model":"native-chat","input":"hello","stream":true}`, wantStream: "event: response.output_text.delta", set: func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+fixture.key) }},
		{name: "anthropic", path: "/v1/messages", body: `{"model":"native-chat","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":true}`, wantStream: "event: content_block_delta", set: func(r *http.Request) { r.Header.Set("X-API-Key", fixture.key) }},
		{name: "gemini", path: "/v1beta/models/native-chat:streamGenerateContent", body: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`, wantStream: `"candidates"`, set: func(r *http.Request) { r.Header.Set("X-Goog-API-Key", fixture.key) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "native-stream-"+test.name)
			test.set(request)
			response := httptest.NewRecorder()
			fixture.handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/event-stream") {
				t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
			}
			if !strings.Contains(response.Body.String(), test.wantStream) || !strings.Contains(response.Body.String(), "ok") {
				t.Fatalf("translated stream is invalid: %s", response.Body.String())
			}
		})
	}
	usage, err := fixture.control.UsageReport(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage.Recent) < len(tests) {
		encoded, _ := json.Marshal(usage.Recent)
		t.Fatalf("usage records=%s", encoded)
	}
}

func TestGatewayTextUpstreamErrorsUseClientProtocolEnvelope(t *testing.T) {
	tests := []struct {
		name, path, body, want string
	}{
		{name: "Anthropic JSON", path: "/v1/messages", body: `{"model":"native-chat","max_tokens":32,"messages":[{"role":"user","content":"synthetic-failure"}]}`, want: `"type":"rate_limit_error"`},
		{name: "Anthropic stream", path: "/v1/messages", body: `{"model":"native-chat","max_tokens":32,"messages":[{"role":"user","content":"synthetic-failure"}],"stream":true}`, want: `"type":"rate_limit_error"`},
		{name: "Gemini JSON", path: "/v1beta/models/native-chat:generateContent", body: `{"contents":[{"role":"user","parts":[{"text":"synthetic-failure"}]}]}`, want: `"status":"RESOURCE_EXHAUSTED"`},
		{name: "Gemini stream", path: "/v1beta/models/native-chat:streamGenerateContent", body: `{"contents":[{"role":"user","parts":[{"text":"synthetic-failure"}]}]}`, want: `"status":"RESOURCE_EXHAUSTED"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newNativeProtocolFixture(t)
			request := httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "native-error-"+strings.ReplaceAll(test.name, " ", "-"))
			if strings.HasPrefix(test.name, "Anthropic") {
				request.Header.Set("X-API-Key", fixture.key)
			} else {
				request.Header.Set("X-Goog-API-Key", fixture.key)
			}
			response := httptest.NewRecorder()
			fixture.handler.ServeHTTP(response, request)
			if response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), test.want) || !strings.Contains(response.Body.String(), "synthetic upstream limit") {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Header().Get("Content-Type"), "text/event-stream") {
				t.Fatalf("upstream error was exposed as a stream: headers=%v", response.Header())
			}
		})
	}
}

func TestGatewayOpenAIChatTranslatesAcrossUpstreamFormats(t *testing.T) {
	tests := []struct {
		name, providerType, basePath, format, wantPath, wantHeader, response string
	}{
		{
			name: "Anthropic", providerType: controlplane.ProviderTypeAnthropicCompatible, basePath: "/v1", format: controlplane.UpstreamFormatAnthropic,
			wantPath: "/v1/messages", wantHeader: "x-api-key",
			response: `{"id":"msg_1","model":"claude-upstream","content":[{"type":"text","text":"translated"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`,
		},
		{
			name: "Gemini", providerType: controlplane.ProviderTypeGeminiCompatible, basePath: "/v1beta", format: controlplane.UpstreamFormatGemini,
			wantPath: "/v1beta/models/gemini-upstream:generateContent", wantHeader: "x-goog-api-key",
			response: `{"responseId":"gemini_1","modelVersion":"gemini-upstream","candidates":[{"content":{"role":"model","parts":[{"text":"translated"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstreamModel := strings.ToLower(test.name) + "-upstream"
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.wantPath || r.Header.Get(test.wantHeader) != "provider-secret" {
					t.Errorf("upstream path=%q headers=%v", r.URL.Path, r.Header)
				}
				body, _ := io.ReadAll(r.Body)
				if !bytes.Contains(body, []byte("hello")) {
					t.Errorf("canonical request was not encoded for %s: %s", test.format, body)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, test.response)
			}))
			defer upstream.Close()

			handler, control := newTestRuntime(t, RuntimeConfig{})
			provider, err := control.CreateProvider(context.Background(), "test", controlplane.ProviderRequest{Name: test.name, Type: test.providerType, BaseURL: upstream.URL + test.basePath, Status: controlplane.ProviderStatusActive})
			if err != nil {
				t.Fatal(err)
			}
			schedulable := true
			account, err := control.CreateProviderAccount(context.Background(), "test", controlplane.ProviderAccountRequest{ProviderID: provider.ID, Name: test.name, AuthType: controlplane.ProviderAuthAPIKey, Status: controlplane.AccountStatusActive, Schedulable: &schedulable, Models: []string{upstreamModel}, Secret: "provider-secret", Concurrency: 2})
			if err != nil {
				t.Fatal(err)
			}
			model, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{ModelID: "cross-protocol-" + strings.ToLower(test.name), Name: test.name, Modality: "chat", Status: controlplane.GatewayModelStatusActive})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := control.CreateModelRoute(context.Background(), "test", controlplane.ModelRouteRequest{GatewayModelID: model.ID, ProviderAccountID: account.ID, UpstreamModel: upstreamModel, UpstreamFormat: test.format, Status: controlplane.ModelRouteStatusActive}); err != nil {
				t.Fatal(err)
			}
			key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{Name: test.name, ModelAllowlist: []string{model.ModelID}})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"`+model.ModelID+`","max_completion_tokens":32,"messages":[{"role":"user","content":"hello"}]}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+key.Key)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"object":"chat.completion"`) || !strings.Contains(response.Body.String(), "translated") {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestGatewaySkipsProtocolIncompatibleCandidateBeforeUpstreamCall(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()
	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "test", controlplane.ProviderRequest{Name: "Anthropic", Type: controlplane.ProviderTypeAnthropicCompatible, BaseURL: upstream.URL + "/v1", Status: controlplane.ProviderStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	schedulable := true
	account, err := control.CreateProviderAccount(context.Background(), "test", controlplane.ProviderAccountRequest{ProviderID: provider.ID, Name: "Anthropic", AuthType: controlplane.ProviderAuthAPIKey, Status: controlplane.AccountStatusActive, Schedulable: &schedulable, Models: []string{"claude"}, Secret: "secret", Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	model, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{ModelID: "strict-json", Name: "Strict JSON", Modality: "chat", Status: controlplane.GatewayModelStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.CreateModelRoute(context.Background(), "test", controlplane.ModelRouteRequest{GatewayModelID: model.ID, ProviderAccountID: account.ID, UpstreamModel: "claude", UpstreamFormat: controlplane.UpstreamFormatAnthropic, Status: controlplane.ModelRouteStatusActive}); err != nil {
		t.Fatal(err)
	}
	key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{Name: "strict-json", ModelAllowlist: []string{model.ModelID}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"strict-json","messages":[{"role":"user","content":"hello"}],"response_format":{"type":"json_object"}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+key.Key)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"type":"unsupported_feature"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if upstreamCalls != 0 {
		t.Fatalf("incompatible candidate reached upstream %d times", upstreamCalls)
	}
	accounts, err := control.ListProviderAccounts(context.Background())
	if err != nil || len(accounts) != 1 || accounts[0].ConsecutiveFailures != 0 || accounts[0].CircuitState != controlplane.CircuitStateClosed {
		t.Fatalf("account state=%+v err=%v", accounts, err)
	}
	traces, err := control.ListGatewayTraces(context.Background(), 10)
	if err != nil || len(traces) != 1 || !strings.Contains(traces[0].RouteAttempts, `"outcome":"skipped"`) || !strings.Contains(traces[0].RouteAttempts, "protocol_incompatible") {
		t.Fatalf("traces=%+v err=%v", traces, err)
	}
}
