package controlplane

import (
	"context"
	"strings"
	"testing"
)

func TestProviderAccountAdapterValidationMatrix(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		auth      string
		config    map[string]string
		secret    string
		wantError string
	}{
		{name: "OpenAI API key", provider: ProviderTypeOpenAICompatible, auth: ProviderAuthAPIKey, secret: "key"},
		{name: "OpenAI API key with UI settings", provider: ProviderTypeOpenAICompatible, auth: ProviderAuthAPIKey, config: map[string]string{ProviderAccountAdapterConfigSettings: `{"notes":"managed","pool_mode_enabled":"true"}`}, secret: "key"},
		{name: "AWS default chain", provider: ProviderTypeAWSBedrock, auth: ProviderAuthAWSDefault, config: map[string]string{"region": "us-east-1"}},
		{name: "AWS missing region", provider: ProviderTypeAWSBedrock, auth: ProviderAuthAWSDefault, wantError: "adapter_config.region"},
		{name: "GCP ADC", provider: ProviderTypeGCPVertex, auth: ProviderAuthGCPADC, config: map[string]string{"project": "project", "location": "us-central1"}},
		{name: "GCP wrong auth", provider: ProviderTypeGCPVertex, auth: ProviderAuthAPIKey, config: map[string]string{"project": "project", "location": "us-central1"}, secret: "key", wantError: "not valid"},
		{name: "Azure managed identity", provider: ProviderTypeAzureOpenAI, auth: ProviderAuthAzureManagedIdentity, config: map[string]string{"api_version": "2025-04-01-preview"}},
		{name: "unknown config", provider: ProviderTypeAzureOpenAI, auth: ProviderAuthAPIKey, config: map[string]string{"api_version": "2025-04-01-preview", "region": "invalid"}, secret: "key", wantError: "not valid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := NewService(NewMemoryRepository(), "/v1", "multi-cloud-secret")
			provider, err := svc.CreateProvider(context.Background(), "test", ProviderRequest{Name: test.name, Type: test.provider, BaseURL: "https://provider.example", Status: ProviderStatusActive})
			if err != nil {
				t.Fatal(err)
			}
			account, err := svc.CreateProviderAccount(context.Background(), "test", ProviderAccountRequest{ProviderID: provider.ID, Name: test.name, AuthType: test.auth, AdapterConfig: test.config, Status: AccountStatusActive, Models: []string{"model"}, Secret: test.secret})
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("account=%+v err=%v", account, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if account.Platform != test.provider || account.AuthType != test.auth || len(account.AdapterConfig) != len(test.config) {
				t.Fatalf("account=%+v", account)
			}
			if providerAuthRequiresSecret(test.auth) != account.SecretConfigured {
				t.Fatalf("secret state=%+v", account)
			}
		})
	}
}

func TestModelRouteRequiresProviderCompatibleUpstreamFormat(t *testing.T) {
	tests := []struct {
		provider, auth, format, modality string
		config                           map[string]string
		valid                            bool
	}{
		{provider: ProviderTypeOpenAICompatible, auth: ProviderAuthAPIKey, format: UpstreamFormatOpenAIChat, modality: "chat", valid: true},
		{provider: ProviderTypeOpenAICompatible, auth: ProviderAuthAPIKey, format: UpstreamFormatAnthropic, modality: "chat"},
		{provider: ProviderTypeAWSBedrock, auth: ProviderAuthAWSDefault, config: map[string]string{"region": "us-east-1"}, format: UpstreamFormatBedrockConverse, modality: "chat", valid: true},
		{provider: ProviderTypeGCPVertex, auth: ProviderAuthGCPADC, config: map[string]string{"project": "project", "location": "location"}, format: UpstreamFormatAnthropic, modality: "chat", valid: true},
		{provider: ProviderTypeGCPVertex, auth: ProviderAuthGCPADC, config: map[string]string{"project": "project", "location": "location"}, format: UpstreamFormatGemini, modality: "chat", valid: true},
		{provider: ProviderTypeAzureOpenAI, auth: ProviderAuthAzureManagedIdentity, config: map[string]string{"api_version": "version"}, format: UpstreamFormatBedrockConverse, modality: "chat"},
		{provider: ProviderTypeOpenAICompatible, auth: ProviderAuthAPIKey, format: UpstreamFormatNativeMedia, modality: "image", valid: true},
		{provider: ProviderTypeOpenAICompatible, auth: ProviderAuthAPIKey, format: UpstreamFormatNativeMedia, modality: "chat"},
		{provider: ProviderTypeOpenAICompatible, auth: ProviderAuthAPIKey, format: UpstreamFormatOpenAIChat, modality: "audio"},
		{provider: ProviderTypeOpenAICompatible, auth: ProviderAuthAPIKey, format: UpstreamFormatNativeMedia, modality: "audio", valid: true},
		{provider: ProviderTypeAWSBedrock, auth: ProviderAuthAWSDefault, config: map[string]string{"region": "us-east-1"}, format: UpstreamFormatNativeMedia, modality: "audio"},
		{provider: ProviderTypeGCPVertex, auth: ProviderAuthGCPADC, config: map[string]string{"project": "project", "location": "location"}, format: UpstreamFormatNativeMedia, modality: "video", valid: true},
		{provider: ProviderTypeAzureOpenAI, auth: ProviderAuthAzureManagedIdentity, config: map[string]string{"api_version": "version"}, format: UpstreamFormatNativeMedia, modality: "image", valid: true},
		{provider: ProviderTypeOpenAICompatible, auth: ProviderAuthAPIKey, format: UpstreamFormatOpenAIResponses, modality: "multimodal", valid: true},
		{provider: ProviderTypeOpenAICompatible, auth: ProviderAuthAPIKey, format: UpstreamFormatNativeMedia, modality: "multimodal", valid: true},
		{provider: ProviderTypeOpenAICompatible, auth: ProviderAuthAPIKey, format: UpstreamFormatNativeMedia, modality: "embedding"},
	}
	for _, test := range tests {
		t.Run(test.provider+"/"+test.format, func(t *testing.T) {
			ctx := context.Background()
			svc := NewService(NewMemoryRepository(), "/v1", "route-secret")
			provider, err := svc.CreateProvider(ctx, "test", ProviderRequest{Name: "provider", Type: test.provider, BaseURL: "https://provider.example", Status: ProviderStatusActive})
			if err != nil {
				t.Fatal(err)
			}
			secret := ""
			if providerAuthRequiresSecret(test.auth) {
				secret = "secret"
			}
			account, err := svc.CreateProviderAccount(ctx, "test", ProviderAccountRequest{ProviderID: provider.ID, Name: "account", AuthType: test.auth, AdapterConfig: test.config, Status: AccountStatusActive, Models: []string{"upstream"}, Secret: secret})
			if err != nil {
				t.Fatal(err)
			}
			model, _ := svc.CreateGatewayModel(ctx, "test", GatewayModelRequest{ModelID: "public", Name: "Public", Modality: test.modality, Status: GatewayModelStatusActive})
			_, err = svc.CreateModelRoute(ctx, "test", ModelRouteRequest{GatewayModelID: model.ID, ProviderAccountID: account.ID, UpstreamModel: "upstream", UpstreamFormat: test.format, Status: ModelRouteStatusActive})
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected incompatible format error")
			}
		})
	}
}

func TestGatewaySimulatorReportsProtocolCompatibility(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "sim-secret")
	provider, _ := svc.CreateProvider(ctx, "test", ProviderRequest{Name: "Anthropic", Type: ProviderTypeAnthropicCompatible, BaseURL: "https://provider.example/v1", Status: ProviderStatusActive})
	account, _ := svc.CreateProviderAccount(ctx, "test", ProviderAccountRequest{ProviderID: provider.ID, Name: "Claude", AuthType: ProviderAuthAPIKey, Status: AccountStatusActive, Models: []string{"claude"}, Secret: "secret"})
	model, _ := svc.CreateGatewayModel(ctx, "test", GatewayModelRequest{ModelID: "public", Name: "Public", Modality: "chat", Status: GatewayModelStatusActive})
	_, err := svc.CreateModelRoute(ctx, "test", ModelRouteRequest{GatewayModelID: model.ID, ProviderAccountID: account.ID, UpstreamModel: "claude", UpstreamFormat: UpstreamFormatAnthropic, Status: ModelRouteStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.SimulateGatewayRouting(ctx, GatewaySimulationRequest{Model: "public", Protocol: "openai_responses", RequiredFeatures: []string{"response_format"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Eligible || result.Candidates[0].Reason != "protocol_incompatible:response_format" || result.Candidates[0].UpstreamFormat != UpstreamFormatAnthropic {
		t.Fatalf("result=%+v", result)
	}
}
