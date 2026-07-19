package controlplane

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestValidateProviderAccountRuntimeSettings(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "base url", raw: `{"base_url":"ftp://provider.example"}`, want: "base_url"},
		{name: "mapping requires both sides", raw: `{"model_restriction_mode":"mapping","model_mappings":[{"from":"public"}]}`, want: "requires both"},
		{name: "headers must be object", raw: `{"header_override_enabled":true,"header_override_json":"[]"}`, want: "JSON object"},
		{name: "hop by hop header", raw: `{"header_override_enabled":true,"header_override_json":"{\"Connection\":\"keep-alive\"}"}`, want: "not overrideable"},
		{name: "newline header value", raw: `{"header_override_enabled":true,"header_override_json":"{\"X-Test\":\"bad\\nvalue\"}"}`, want: "invalid control character"},
		{name: "invalid header name", raw: `{"header_override_enabled":true,"header_override_json":"{\"Bad Header\":\"value\"}"}`, want: "header name is invalid"},
		{name: "duplicate header names", raw: `{"header_override_enabled":true,"header_override_json":"{\"X-Test\":\"one\",\"x-test\":\"two\"}"}`, want: "duplicates"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProviderAccountRuntimeSettings(map[string]string{ProviderAccountAdapterConfigSettings: tc.raw})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateProviderAccountRuntimeSettings() error = %v, want substring %q", err, tc.want)
			}
		})
	}
	valid := map[string]string{ProviderAccountAdapterConfigSettings: `{"base_url":"https://account.example/v1","model_restriction_mode":"mapping","model_mappings":[{"from":"public","to":"actual"}],"header_override_enabled":true,"header_override_json":"{\"X-Account\":\"account-a\"}"}`}
	if err := ValidateProviderAccountRuntimeSettings(valid); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, "https://provider.example/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyProviderAccountHeaderOverrides(req, valid); err != nil {
		t.Fatalf("ApplyProviderAccountHeaderOverrides(): %v", err)
	}
	if got := req.Header.Get("X-Account"); got != "account-a" {
		t.Fatalf("X-Account = %q, want account-a", got)
	}
}

func TestGatewayProviderCandidatesApplyAccountRuntimeSettings(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name: "Runtime provider", Type: ProviderTypeOpenAICompatible,
		BaseURL: "https://provider.example/v1", Status: ProviderStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	schedulable := true
	settings := `{"base_url":"https://account.example/v1","model_restriction_mode":"mapping","model_mappings":[{"from":"public-model","to":"actual-model"}]}`
	account, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Runtime account", Platform: ProviderTypeOpenAICompatible,
		AuthType: ProviderAuthAPIKey, Status: AccountStatusActive, Schedulable: &schedulable,
		Priority: 10, Concurrency: 2, Models: []string{"route-model", "actual-model"}, Secret: "runtime-secret",
		AdapterConfig: map[string]string{ProviderAccountAdapterConfigSettings: settings},
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(): %v", err)
	}
	model, err := svc.CreateGatewayModel(context.Background(), "tester", GatewayModelRequest{
		ModelID: "public-model", Name: "public-model", Status: GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateGatewayModel(): %v", err)
	}
	if _, err := svc.CreateModelRoute(context.Background(), "tester", ModelRouteRequest{
		GatewayModelID: model.ID, ProviderAccountID: account.ID, UpstreamModel: "route-model",
		UpstreamFormat: UpstreamFormatOpenAIChat, Status: ModelRouteStatusActive,
	}); err != nil {
		t.Fatalf("CreateModelRoute(): %v", err)
	}
	// The route is intentionally bound to the account's declared model. The
	// account mapping only changes the final upstream model sent on dispatch.
	routes, err := svc.ListModelRoutes(context.Background())
	if err != nil || len(routes) != 1 {
		t.Fatalf("ListModelRoutes(): routes=%+v err=%v", routes, err)
	}
	if routes[0].UpstreamModel != "route-model" || routes[0].GatewayModelID != model.ID {
		t.Fatalf("unexpected model route: %+v", routes[0])
	}
	candidates, hasRoutes, err := svc.GatewayProviderCandidatesForModel(context.Background(), "public-model")
	if err != nil {
		t.Fatalf("GatewayProviderCandidatesForModel(): %v", err)
	}
	if !hasRoutes || len(candidates) != 1 {
		t.Fatalf("candidates=%+v hasRoutes=%v", candidates, hasRoutes)
	}
	if candidates[0].BaseURL != "https://account.example/v1" {
		t.Fatalf("candidate BaseURL = %q, want account override", candidates[0].BaseURL)
	}
	if candidates[0].UpstreamModel != "actual-model" {
		t.Fatalf("mapped request UpstreamModel = %q, want actual-model", candidates[0].UpstreamModel)
	}
	// Mapping the route's model is the dispatch-facing use case.
	updated := account
	updated.AdapterConfig[ProviderAccountAdapterConfigSettings] = `{"model_restriction_mode":"mapping","model_mappings":[{"from":"route-model","to":"actual-model"}]}`
	if err := svc.repo.SaveProviderAccount(context.Background(), updated); err != nil {
		t.Fatalf("SaveProviderAccount(): %v", err)
	}
	candidates, _, err = svc.GatewayProviderCandidatesForModel(context.Background(), "public-model")
	if err != nil || len(candidates) != 1 {
		t.Fatalf("GatewayProviderCandidatesForModel() mapped: candidates=%+v err=%v", candidates, err)
	}
	if candidates[0].UpstreamModel != "actual-model" {
		t.Fatalf("mapped UpstreamModel = %q, want actual-model", candidates[0].UpstreamModel)
	}
	updated.AdapterConfig[ProviderAccountAdapterConfigSettings] = `{"model_restriction_mode":"mapping","model_mappings":[{"from":"route-model","to":"route-model"},{"from":"public-model","to":"other-model"}]}`
	if err := svc.repo.SaveProviderAccount(context.Background(), updated); err != nil {
		t.Fatalf("SaveProviderAccount() identity mapping: %v", err)
	}
	candidates, _, err = svc.GatewayProviderCandidatesForModel(context.Background(), "public-model")
	if err != nil || len(candidates) != 1 {
		t.Fatalf("GatewayProviderCandidatesForModel() identity: candidates=%+v err=%v", candidates, err)
	}
	if candidates[0].UpstreamModel != "route-model" {
		t.Fatalf("identity route mapping = %q, want route-model", candidates[0].UpstreamModel)
	}
	updated.AdapterConfig[ProviderAccountAdapterConfigSettings] = `{"model_restriction_mode":"mapping","model_mappings":[{"from":"route-model","to":"undeclared-model"}]}`
	if err := svc.repo.SaveProviderAccount(context.Background(), updated); err != nil {
		t.Fatalf("SaveProviderAccount() undeclared mapping: %v", err)
	}
	candidates, _, err = svc.GatewayProviderCandidatesForModel(context.Background(), "public-model")
	if err != nil {
		t.Fatalf("GatewayProviderCandidatesForModel() undeclared: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("undeclared mapping unexpectedly routed: %+v", candidates)
	}
}

func TestDeleteProviderAccountProtectsModelRoutes(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "delete-account-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Delete provider", Type: ProviderTypeOpenAICompatible,
		BaseURL: "https://provider.example/v1", Status: ProviderStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Referenced account", Platform: ProviderTypeOpenAICompatible,
		AuthType: ProviderAuthAPIKey, Status: AccountStatusActive, Models: []string{"model"}, Secret: "account-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := svc.CreateGatewayModel(ctx, "tester", GatewayModelRequest{ModelID: "delete-model", Name: "delete-model", Status: GatewayModelStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateModelRoute(ctx, "tester", ModelRouteRequest{GatewayModelID: model.ID, ProviderAccountID: account.ID, UpstreamModel: "model", UpstreamFormat: UpstreamFormatOpenAIChat, Status: ModelRouteStatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteProviderAccount(ctx, "tester", account.ID); err == nil || !strings.Contains(err.Error(), "referenced by model route") {
		t.Fatalf("DeleteProviderAccount() error = %v, want route protection", err)
	}

	orphan, err := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Orphan account", Platform: ProviderTypeOpenAICompatible,
		AuthType: ProviderAuthAPIKey, Status: AccountStatusActive, Secret: "orphan-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteProviderAccount(ctx, "tester", orphan.ID); err != nil {
		t.Fatalf("DeleteProviderAccount() orphan: %v", err)
	}
	accounts, err := svc.ListProviderAccounts(ctx)
	if err != nil || len(accounts) != 1 || accounts[0].ID != account.ID {
		t.Fatalf("accounts after delete = %+v, err=%v", accounts, err)
	}
}
