package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

type embeddingFixture struct {
	handler http.Handler
	control *controlplane.Service
	key     string
	mu      sync.Mutex
	paths   []string
	bodies  [][]byte
}

func newEmbeddingFixture(t *testing.T, upstreamResponse string) *embeddingFixture {
	t.Helper()
	fixture := &embeddingFixture{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fixture.mu.Lock()
		fixture.paths = append(fixture.paths, r.URL.Path)
		fixture.bodies = append(fixture.bodies, append([]byte(nil), body...))
		fixture.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "embedding-upstream-1")
		_, _ = io.WriteString(w, upstreamResponse)
	}))
	t.Cleanup(upstream.Close)
	handler, control := newTestRuntime(t, RuntimeConfig{})
	fixture.handler, fixture.control = handler, control
	provider, err := control.CreateProvider(context.Background(), "test", controlplane.ProviderRequest{Name: "Embedding provider", Type: controlplane.ProviderTypeOpenAICompatible, BaseURL: upstream.URL + "/v1", Status: controlplane.ProviderStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	schedulable := true
	account, err := control.CreateProviderAccount(context.Background(), "test", controlplane.ProviderAccountRequest{ProviderID: provider.ID, Name: "Embedding account", AuthType: controlplane.ProviderAuthAPIKey, Status: controlplane.AccountStatusActive, Schedulable: &schedulable, Models: []string{"embedding-upstream"}, Secret: "embedding-secret", Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	model, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{ModelID: "published-embedding", Name: "Published embedding", Modality: "embedding", Status: controlplane.GatewayModelStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.CreateModelRoute(context.Background(), "test", controlplane.ModelRouteRequest{GatewayModelID: model.ID, ProviderAccountID: account.ID, UpstreamModel: "embedding-upstream", UpstreamFormat: controlplane.UpstreamFormatOpenAIEmbeddings, Status: controlplane.ModelRouteStatusActive}); err != nil {
		t.Fatal(err)
	}
	key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{Name: "Embedding caller", ModelAllowlist: []string{"published-embedding"}})
	if err != nil {
		t.Fatal(err)
	}
	fixture.key = key.Key
	return fixture
}

func TestGatewayEmbeddingsRecordsValidatedUsageAndTrace(t *testing.T) {
	fixture := newEmbeddingFixture(t, `{"object":"list","data":[{"object":"embedding","embedding":[0.1,0.2,0.3],"index":0},{"object":"embedding","embedding":[0.4,0.5,0.6],"index":1}],"model":"embedding-upstream","usage":{"prompt_tokens":5,"total_tokens":5}}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewBufferString(`{"model":"published-embedding","input":["first","second"],"encoding_format":"float","dimensions":3}`))
	request.Header.Set("Authorization", "Bearer "+fixture.key)
	request.Header.Set("X-Request-ID", "embedding-client-1")
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"model":"published-embedding"`) || !strings.Contains(response.Body.String(), `"embedding":[0.1,0.2,0.3]`) {
		t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	if response.Header().Get("X-AsterRouter-Operation-ID") == "" {
		t.Fatalf("missing operation evidence: %v", response.Header())
	}
	fixture.mu.Lock()
	if len(fixture.paths) != 1 || fixture.paths[0] != "/v1/embeddings" || !bytes.Contains(fixture.bodies[0], []byte(`"model":"embedding-upstream"`)) {
		fixture.mu.Unlock()
		t.Fatalf("paths=%v bodies=%q", fixture.paths, fixture.bodies)
	}
	fixture.mu.Unlock()
	usage, err := fixture.control.UsageReport(context.Background(), 10)
	if err != nil || len(usage.Recent) != 1 || usage.Recent[0].Protocol != "openai_embeddings" || usage.Recent[0].InputTokens != 5 || usage.Recent[0].OutputTokens != 0 || usage.Recent[0].UpstreamModel != "embedding-upstream" {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
	traces, err := fixture.control.ListGatewayTraces(context.Background(), 10)
	if err != nil || len(traces) != 1 || !strings.Contains(traces[0].ResponseSummary, "dimensions=3") || traces[0].MessageCount != 2 {
		t.Fatalf("traces=%+v err=%v", traces, err)
	}
}

func TestGatewayEmbeddingsRejectsMalformedVectors(t *testing.T) {
	fixture := newEmbeddingFixture(t, `{"object":"list","data":[{"object":"embedding","embedding":[0.1,0.2],"index":0},{"object":"embedding","embedding":[0.3],"index":1}],"model":"embedding-upstream","usage":{"prompt_tokens":5,"total_tokens":5}}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewBufferString(`{"model":"published-embedding","input":["first","second"],"encoding_format":"float"}`))
	request.Header.Set("Authorization", "Bearer "+fixture.key)
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), `"type":"upstream_error"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGatewayEmbeddingsRejectsUnsupportedEncodingBeforeRouting(t *testing.T) {
	fixture := newEmbeddingFixture(t, `{}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewBufferString(`{"model":"published-embedding","input":"hello","encoding_format":"hex"}`))
	request.Header.Set("Authorization", "Bearer "+fixture.key)
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"type":"unsupported_feature"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.paths) != 0 {
		t.Fatalf("invalid request reached upstream %d times", len(fixture.paths))
	}
}
