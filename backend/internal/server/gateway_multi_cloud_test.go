package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/provideradapter"
	"github.com/aws/aws-sdk-go-v2/aws"
)

type fakeMultiCloudCredentialResolver struct{}

func (fakeMultiCloudCredentialResolver) AWSCredentials(context.Context, controlplane.GatewayProvider) (aws.Credentials, error) {
	return aws.Credentials{AccessKeyID: "AKID", SecretAccessKey: "SECRET", SessionToken: "SESSION", Source: "test"}, nil
}

func (fakeMultiCloudCredentialResolver) GCPToken(context.Context, controlplane.GatewayProvider) (string, error) {
	return "gcp-token", nil
}

func (fakeMultiCloudCredentialResolver) AzureToken(context.Context, controlplane.GatewayProvider) (string, error) {
	return "azure-token", nil
}

func TestGatewayFallsBackFromAWSClaudeToGCPClaudeAcrossClientProtocol(t *testing.T) {
	originalAdapters := gatewayProviderAdapters
	gatewayProviderAdapters = provideradapter.NewRegistry(fakeMultiCloudCredentialResolver{})
	t.Cleanup(func() { gatewayProviderAdapters = originalAdapters })

	awsCalls := 0
	awsUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		awsCalls++
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/model/aws-claude/converse" || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256") || !bytes.Contains(body, []byte(`"messages"`)) {
			t.Errorf("AWS request path=%q authorization=%q body=%s", r.URL.Path, r.Header.Get("Authorization"), body)
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"message":"synthetic AWS failure"}`)
	}))
	defer awsUpstream.Close()

	gcpCalls := 0
	gcpUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gcpCalls++
		body, _ := io.ReadAll(r.Body)
		wantPath := "/" + "projects/project-a/locations/us-central1/publishers/anthropic/models/gcp-claude:rawPredict"
		if r.URL.Path != wantPath || r.Header.Get("Authorization") != "Bearer gcp-token" || bytes.Contains(body, []byte(`"model"`)) {
			t.Errorf("GCP request path=%q authorization=%q body=%s", r.URL.Path, r.Header.Get("Authorization"), body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_gcp","model":"gcp-claude","content":[{"type":"text","text":"backup-ok"}],"stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":2}}`)
	}))
	defer gcpUpstream.Close()

	handler, control := newTestRuntime(t, RuntimeConfig{})
	awsProvider, err := control.CreateProvider(context.Background(), "test", controlplane.ProviderRequest{Name: "AWS Claude", Type: controlplane.ProviderTypeAWSBedrock, BaseURL: awsUpstream.URL, Status: controlplane.ProviderStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	gcpProvider, err := control.CreateProvider(context.Background(), "test", controlplane.ProviderRequest{Name: "GCP Claude", Type: controlplane.ProviderTypeGCPVertex, BaseURL: gcpUpstream.URL, Status: controlplane.ProviderStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	schedulable := true
	awsAccount, err := control.CreateProviderAccount(context.Background(), "test", controlplane.ProviderAccountRequest{ProviderID: awsProvider.ID, Name: "AWS primary", AuthType: controlplane.ProviderAuthAWSDefault, AdapterConfig: map[string]string{"region": "us-east-1"}, Status: controlplane.AccountStatusActive, Schedulable: &schedulable, Priority: 10, Models: []string{"aws-claude"}, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	gcpAccount, err := control.CreateProviderAccount(context.Background(), "test", controlplane.ProviderAccountRequest{ProviderID: gcpProvider.ID, Name: "GCP backup", AuthType: controlplane.ProviderAuthGCPADC, AdapterConfig: map[string]string{"project": "project-a", "location": "us-central1"}, Status: controlplane.AccountStatusActive, Schedulable: &schedulable, Priority: 20, Models: []string{"gcp-claude"}, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	model, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{ModelID: "public-claude", Name: "Claude", Modality: "chat", Status: controlplane.GatewayModelStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []controlplane.ModelRouteRequest{
		{GatewayModelID: model.ID, ProviderAccountID: awsAccount.ID, UpstreamModel: "aws-claude", UpstreamFormat: controlplane.UpstreamFormatBedrockConverse, Priority: 10, Status: controlplane.ModelRouteStatusActive},
		{GatewayModelID: model.ID, ProviderAccountID: gcpAccount.ID, UpstreamModel: "gcp-claude", UpstreamFormat: controlplane.UpstreamFormatAnthropic, Priority: 20, Status: controlplane.ModelRouteStatusActive},
	} {
		if _, err := control.CreateModelRoute(context.Background(), "test", route); err != nil {
			t.Fatal(err)
		}
	}
	key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{Name: "multi-cloud", ModelAllowlist: []string{model.ModelID}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"public-claude","max_completion_tokens":32,"messages":[{"role":"user","content":"hello"}]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+key.Key)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "backup-ok") || awsCalls != 1 || gcpCalls != 1 {
		t.Fatalf("status=%d aws=%d gcp=%d body=%s", response.Code, awsCalls, gcpCalls, response.Body.String())
	}
	traces, err := control.ListGatewayTraces(context.Background(), 10)
	if err != nil || len(traces) != 1 || !strings.Contains(traces[0].RouteAttempts, awsAccount.ID) || !strings.Contains(traces[0].RouteAttempts, gcpAccount.ID) || !strings.Contains(traces[0].RouteAttempts, `"outcome":"failed"`) || !strings.Contains(traces[0].RouteAttempts, `"outcome":"selected"`) {
		t.Fatalf("traces=%+v err=%v", traces, err)
	}
}
