package provideradapter

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/aws/aws-sdk-go-v2/aws"
)

type fakeCredentialResolver struct{}

func (fakeCredentialResolver) AWSCredentials(context.Context, controlplane.GatewayProvider) (aws.Credentials, error) {
	return aws.Credentials{AccessKeyID: "AKID", SecretAccessKey: "SECRET", SessionToken: "SESSION", Source: "test"}, nil
}
func (fakeCredentialResolver) GCPToken(context.Context, controlplane.GatewayProvider) (string, error) {
	return "gcp-token", nil
}
func (fakeCredentialResolver) AzureToken(context.Context, controlplane.GatewayProvider) (string, error) {
	return "azure-token", nil
}

type fakeAzureTokenCredential struct {
	token   string
	options policy.TokenRequestOptions
}

func (c *fakeAzureTokenCredential) GetToken(_ context.Context, options policy.TokenRequestOptions) (azcore.AccessToken, error) {
	c.options = options
	return azcore.AccessToken{Token: c.token, ExpiresOn: time.Now().Add(time.Hour)}, nil
}

func TestRegistryBuildsProviderRequests(t *testing.T) {
	registry := NewRegistry(fakeCredentialResolver{})
	tests := []struct {
		name       string
		provider   controlplane.GatewayProvider
		stream     bool
		wantPath   string
		wantQuery  string
		wantHeader string
		wantValue  string
	}{
		{name: "OpenAI compatible", provider: controlplane.GatewayProvider{Type: controlplane.ProviderTypeOpenAICompatible, BaseURL: "https://openai.example/v1", AuthType: controlplane.ProviderAuthAPIKey, APIKey: "key", UpstreamModel: "gpt", UpstreamFormat: controlplane.UpstreamFormatOpenAIChat}, wantPath: "/v1/chat/completions", wantHeader: "Authorization", wantValue: "Bearer key"},
		{name: "Anthropic compatible", provider: controlplane.GatewayProvider{Type: controlplane.ProviderTypeAnthropicCompatible, BaseURL: "https://anthropic.example/v1", AuthType: controlplane.ProviderAuthAPIKey, APIKey: "key", UpstreamModel: "claude", UpstreamFormat: controlplane.UpstreamFormatAnthropic}, wantPath: "/v1/messages", wantHeader: "x-api-key", wantValue: "key"},
		{name: "Gemini compatible stream", provider: controlplane.GatewayProvider{Type: controlplane.ProviderTypeGeminiCompatible, BaseURL: "https://gemini.example/v1beta", AuthType: controlplane.ProviderAuthAPIKey, APIKey: "key", UpstreamModel: "gemini", UpstreamFormat: controlplane.UpstreamFormatGemini}, stream: true, wantPath: "/v1beta/models/gemini:streamGenerateContent", wantQuery: "alt=sse", wantHeader: "x-goog-api-key", wantValue: "key"},
		{name: "AWS Bedrock", provider: controlplane.GatewayProvider{Type: controlplane.ProviderTypeAWSBedrock, BaseURL: "https://bedrock.example", AuthType: controlplane.ProviderAuthAWSDefault, AdapterConfig: map[string]string{"region": "us-east-1"}, UpstreamModel: "claude", UpstreamFormat: controlplane.UpstreamFormatBedrockConverse}, wantPath: "/model/claude/converse", wantHeader: "Authorization", wantValue: "AWS4-HMAC-SHA256"},
		{name: "Vertex Claude", provider: controlplane.GatewayProvider{Type: controlplane.ProviderTypeGCPVertex, BaseURL: "https://vertex.example/v1", AuthType: controlplane.ProviderAuthGCPADC, AdapterConfig: map[string]string{"project": "project-a", "location": "us-central1"}, UpstreamModel: "claude", UpstreamFormat: controlplane.UpstreamFormatAnthropic}, wantPath: "/v1/" + "projects/project-a/locations/us-central1/publishers/anthropic/models/claude:rawPredict", wantHeader: "Authorization", wantValue: "Bearer gcp-token"},
		{name: "Vertex Gemini stream", provider: controlplane.GatewayProvider{Type: controlplane.ProviderTypeGCPVertex, BaseURL: "https://vertex.example/v1", AuthType: controlplane.ProviderAuthGCPADC, AdapterConfig: map[string]string{"project": "project-a", "location": "us-central1"}, UpstreamModel: "gemini", UpstreamFormat: controlplane.UpstreamFormatGemini}, stream: true, wantPath: "/v1/" + "projects/project-a/locations/us-central1/publishers/google/models/gemini:streamGenerateContent", wantQuery: "alt=sse", wantHeader: "Authorization", wantValue: "Bearer gcp-token"},
		{name: "Azure API key", provider: controlplane.GatewayProvider{Type: controlplane.ProviderTypeAzureOpenAI, BaseURL: "https://azure.example", AuthType: controlplane.ProviderAuthAPIKey, APIKey: "azure-key", AdapterConfig: map[string]string{"api_version": "2025-04-01-preview"}, UpstreamModel: "deployment-a", UpstreamFormat: controlplane.UpstreamFormatOpenAIChat}, wantPath: "/openai/deployments/deployment-a/chat/completions", wantQuery: "api-version=2025-04-01-preview", wantHeader: "api-key", wantValue: "azure-key"},
		{name: "Azure managed identity", provider: controlplane.GatewayProvider{Type: controlplane.ProviderTypeAzureOpenAI, BaseURL: "https://azure.example", AuthType: controlplane.ProviderAuthAzureManagedIdentity, AdapterConfig: map[string]string{"api_version": "2025-04-01-preview"}, UpstreamModel: "deployment-a", UpstreamFormat: controlplane.UpstreamFormatOpenAIResponses}, wantPath: "/openai/v1/responses", wantQuery: "api-version=2025-04-01-preview", wantHeader: "Authorization", wantValue: "Bearer azure-token"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req, err := registry.BuildRequest(context.Background(), test.provider, []byte(`{"model":"upstream","messages":[]}`), test.stream)
			if err != nil {
				t.Fatal(err)
			}
			if req.URL.Path != test.wantPath || req.URL.RawQuery != test.wantQuery {
				t.Fatalf("url=%s want path=%s query=%s", req.URL, test.wantPath, test.wantQuery)
			}
			if value := req.Header.Get(test.wantHeader); !strings.Contains(value, test.wantValue) {
				t.Fatalf("header %s=%q", test.wantHeader, value)
			}
			if test.stream && req.Header.Get("Accept") != "text/event-stream" {
				t.Fatalf("Accept=%q", req.Header.Get("Accept"))
			}
		})
	}
}

func TestRegistryRequestsBedrockEventStreamContent(t *testing.T) {
	registry := NewRegistry(fakeCredentialResolver{})
	provider := controlplane.GatewayProvider{
		Type: controlplane.ProviderTypeAWSBedrock, BaseURL: "https://bedrock.example",
		AuthType: controlplane.ProviderAuthAWSDefault, AdapterConfig: map[string]string{"region": "us-east-1"},
		UpstreamModel: "claude", UpstreamFormat: controlplane.UpstreamFormatBedrockConverse,
	}
	req, err := registry.BuildRequest(context.Background(), provider, []byte(`{"messages":[]}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Accept"); got != "application/vnd.amazon.eventstream" {
		t.Fatalf("Accept = %q", got)
	}
}

func TestRegistryKeepsCompatibleStreamsAsSSE(t *testing.T) {
	registry := NewRegistry(fakeCredentialResolver{})
	provider := controlplane.GatewayProvider{
		Type: controlplane.ProviderTypeOpenAICompatible, BaseURL: "https://openai.example/v1",
		AuthType: controlplane.ProviderAuthAPIKey, APIKey: "key",
		UpstreamModel: "gpt", UpstreamFormat: controlplane.UpstreamFormatOpenAIChat,
	}
	req, err := registry.BuildRequest(context.Background(), provider, []byte(`{"messages":[]}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("Accept = %q", got)
	}
}

func TestDefaultCredentialResolverResolvesAzureManagedIdentity(t *testing.T) {
	tests := []struct {
		name         string
		config       map[string]string
		wantAudience string
		wantClientID string
		wantDefault  bool
	}{
		{name: "default chain", config: map[string]string{}, wantAudience: "https://cognitiveservices.azure.com/.default", wantDefault: true},
		{name: "user assigned identity", config: map[string]string{"audience": "api://azure-openai/.default", "managed_identity_client_id": "client-a"}, wantAudience: "api://azure-openai/.default", wantClientID: "client-a"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			credential := &fakeAzureTokenCredential{token: "azure-access-token"}
			defaultCalled := false
			managedClientID := ""
			resolver := DefaultCredentialResolver{
				newDefaultAzureCredential: func() (azureTokenCredential, error) {
					defaultCalled = true
					return credential, nil
				},
				newManagedIdentity: func(clientID string) (azureTokenCredential, error) {
					managedClientID = clientID
					return credential, nil
				},
			}
			token, err := resolver.AzureToken(context.Background(), controlplane.GatewayProvider{
				AuthType: controlplane.ProviderAuthAzureManagedIdentity, AdapterConfig: test.config,
			})
			if err != nil {
				t.Fatal(err)
			}
			if token != "azure-access-token" || defaultCalled != test.wantDefault || managedClientID != test.wantClientID {
				t.Fatalf("token=%q default=%t managed_client_id=%q", token, defaultCalled, managedClientID)
			}
			if len(credential.options.Scopes) != 1 || credential.options.Scopes[0] != test.wantAudience {
				t.Fatalf("scopes = %#v", credential.options.Scopes)
			}
		})
	}
}

func TestDefaultCredentialResolverRejectsEmptyAzureToken(t *testing.T) {
	credential := &fakeAzureTokenCredential{}
	resolver := DefaultCredentialResolver{newDefaultAzureCredential: func() (azureTokenCredential, error) { return credential, nil }}
	_, err := resolver.AzureToken(context.Background(), controlplane.GatewayProvider{
		AuthType: controlplane.ProviderAuthAzureManagedIdentity, AdapterConfig: map[string]string{},
	})
	if err == nil || !strings.Contains(err.Error(), "empty token") {
		t.Fatalf("error = %v", err)
	}
}

func TestVertexAnthropicRequestRemovesModelAndAddsVersion(t *testing.T) {
	registry := NewRegistry(fakeCredentialResolver{})
	provider := controlplane.GatewayProvider{Type: controlplane.ProviderTypeGCPVertex, BaseURL: "https://vertex.example/v1", AuthType: controlplane.ProviderAuthGCPADC, AdapterConfig: map[string]string{"project": "project", "location": "location"}, UpstreamModel: "claude", UpstreamFormat: controlplane.UpstreamFormatAnthropic}
	req, err := registry.BuildRequest(context.Background(), provider, []byte(`{"model":"claude","messages":[{"role":"user","content":"hello"}]}`), false)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(req.Body)
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil || payload["model"] != nil || payload["anthropic_version"] != "vertex-2023-10-16" {
		t.Fatalf("body=%s", body)
	}
}
