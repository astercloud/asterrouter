package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderAccountModelSyncTracksDiffAndAffectedRoutes(t *testing.T) {
	ctx := context.Background()
	upstreamModels := []string{"legacy-upstream", "new-upstream"}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		data := make([]map[string]string, 0, len(upstreamModels))
		for _, model := range upstreamModels {
			data = append(data, map[string]string{"id": model})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	}))
	defer upstream.Close()

	svc := NewService(NewMemoryRepository(), "/v1", "model-sync-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Model sync provider", Type: "openai_compatible", BaseURL: upstream.URL + "/v1",
		Status: ProviderStatusActive, Models: []string{"legacy-upstream"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Model sync account", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Models: []string{"legacy-upstream", "manual-upstream"}, Secret: "account-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	gatewayModel, err := svc.CreateGatewayModel(ctx, "tester", GatewayModelRequest{ModelID: "public-legacy", Name: "Public legacy", Modality: "chat", Status: GatewayModelStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	route, err := svc.CreateModelRoute(ctx, "tester", ModelRouteRequest{
		GatewayModelID: gatewayModel.ID, RouteGroup: DefaultModelRouteGroup, ProviderAccountID: account.ID,
		UpstreamModel: "legacy-upstream", Priority: 10, Weight: 100, Status: ModelRouteStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}

	synced, err := svc.SyncProviderAccountModels(ctx, "tester", account.ID, ProviderAccountModelSyncRequest{
		EnabledModels: []string{"legacy-upstream", "manual-upstream"},
	})
	if err != nil {
		t.Fatalf("SyncProviderAccountModels(): %v", err)
	}
	newModel := findProviderAccountModel(synced.Inventory.Models, "new-upstream")
	if newModel == nil || newModel.Source != ProviderAccountModelSourceDiscovered || newModel.Availability != ProviderAccountModelAvailabilityAvailable || newModel.Enabled {
		t.Fatalf("new model mismatch: %+v", newModel)
	}
	manualModel := findProviderAccountModel(synced.Inventory.Models, "manual-upstream")
	if manualModel == nil || manualModel.Source != ProviderAccountModelSourceManual || manualModel.Availability != ProviderAccountModelAvailabilityUnverified || !manualModel.Enabled {
		t.Fatalf("manual model mismatch: %+v", manualModel)
	}

	upstreamModels = []string{"new-upstream"}
	preview, err := svc.DiscoverProviderAccountModels(ctx, "tester", account.ID)
	if err != nil {
		t.Fatalf("DiscoverProviderAccountModels(): %v", err)
	}
	if len(preview.MissingModels) != 1 || preview.MissingModels[0] != "legacy-upstream" {
		t.Fatalf("missing models = %+v", preview.MissingModels)
	}
	if len(preview.AffectedRouteIDs) != 1 || preview.AffectedRouteIDs[0] != route.ID {
		t.Fatalf("affected routes = %+v", preview.AffectedRouteIDs)
	}

	result, err := svc.SyncProviderAccountModels(ctx, "tester", account.ID, ProviderAccountModelSyncRequest{
		EnabledModels: []string{"new-upstream", "manual-upstream"},
	})
	if err != nil {
		t.Fatalf("second SyncProviderAccountModels(): %v", err)
	}
	if contains(result.Account.Models, "legacy-upstream") || !contains(result.Account.Models, "new-upstream") {
		t.Fatalf("enabled models = %+v", result.Account.Models)
	}
	legacy := findProviderAccountModel(result.Inventory.Models, "legacy-upstream")
	if legacy == nil || legacy.Availability != ProviderAccountModelAvailabilityMissing || legacy.Enabled {
		t.Fatalf("legacy inventory mismatch: %+v", legacy)
	}
}

func TestCreateProviderAccountAllowsEmptyInventoryBeforeDiscovery(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"newly-discovered"}]}`))
	}))
	defer upstream.Close()

	svc := NewService(NewMemoryRepository(), "/v1", "empty-inventory-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Empty inventory provider", Type: "openai_compatible", BaseURL: upstream.URL + "/v1",
		Status: ProviderStatusActive, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Empty inventory account", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Models: nil, Secret: "account-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(): %v", err)
	}
	if len(account.Models) != 0 {
		t.Fatalf("models = %+v", account.Models)
	}
	inventory, err := svc.GetProviderAccountModelInventory(ctx, account.ID)
	if err != nil || len(inventory.Models) != 0 {
		t.Fatalf("inventory=%+v err=%v", inventory, err)
	}
	discovery, err := svc.DiscoverProviderAccountModels(ctx, "tester", account.ID)
	if err != nil {
		t.Fatalf("DiscoverProviderAccountModels(): %v", err)
	}
	if len(discovery.AddedModels) != 1 || discovery.AddedModels[0] != "newly-discovered" {
		t.Fatalf("discovery = %+v", discovery)
	}
}

func TestProviderAccountModelSyncAllowsDisablingEveryModel(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"available-model"}]}`))
	}))
	defer upstream.Close()

	svc := NewService(NewMemoryRepository(), "/v1", "disable-all-models-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Disable all provider", Type: "openai_compatible", BaseURL: upstream.URL + "/v1",
		Status: ProviderStatusActive, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Disable all account", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Models: []string{"available-model"}, Secret: "account-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.SyncProviderAccountModels(ctx, "tester", account.ID, ProviderAccountModelSyncRequest{})
	if err != nil {
		t.Fatalf("SyncProviderAccountModels(): %v", err)
	}
	if len(result.Account.Models) != 0 {
		t.Fatalf("enabled models = %+v", result.Account.Models)
	}
	model := findProviderAccountModel(result.Inventory.Models, "available-model")
	if model == nil || model.Enabled || model.Availability != ProviderAccountModelAvailabilityAvailable {
		t.Fatalf("inventory model = %+v", model)
	}
}

func TestActiveModelRouteRejectsDisabledGatewayModel(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "disabled-route-model-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Disabled route provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1",
		Status: ProviderStatusActive, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Disabled route account", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Models: []string{"upstream-model"}, Secret: "account-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := svc.CreateGatewayModel(ctx, "tester", GatewayModelRequest{
		ModelID: "disabled-public", Name: "Disabled public", Status: GatewayModelStatusDisabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := ModelRouteRequest{
		GatewayModelID: model.ID, ProviderAccountID: account.ID, UpstreamModel: "upstream-model",
		RouteGroup: DefaultModelRouteGroup, Status: ModelRouteStatusActive,
	}
	if _, err := svc.CreateModelRoute(ctx, "tester", request); err == nil || !strings.Contains(err.Error(), "active gateway model") {
		t.Fatalf("active route error = %v", err)
	}
	request.Status = ModelRouteStatusDisabled
	if _, err := svc.CreateModelRoute(ctx, "tester", request); err != nil {
		t.Fatalf("disabled historical route: %v", err)
	}
}

func TestProviderAccountHealthCheckAutoEnablesNewModelsOnlyWhenConfigured(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"existing"},{"id":"new"}]}`))
	}))
	defer upstream.Close()
	svc := NewService(NewMemoryRepository(), "/v1", "auto-model-secret")
	provider, _ := svc.CreateProvider(ctx, "tester", ProviderRequest{Name: "Auto provider", Type: "openai_compatible", BaseURL: upstream.URL + "/v1", Status: ProviderStatusActive, Models: []string{"existing"}, APIKey: "provider-secret"})
	autoEnable := true
	account, err := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Auto account", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Models: []string{"existing"}, AutoEnableNewModels: &autoEnable, Secret: "account-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CheckProviderAccount(ctx, "tester", account.ID); err != nil {
		t.Fatal(err)
	}
	accounts, _ := svc.ListProviderAccounts(ctx)
	if len(accounts) != 1 || !contains(accounts[0].Models, "new") {
		t.Fatalf("auto enabled account models = %+v", accounts)
	}
}

func TestProviderModelDiscoveryAdaptersHandleNativePagination(t *testing.T) {
	t.Run("anthropic", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" || r.Header.Get("x-api-key") != "anthropic-secret" || r.Header.Get("anthropic-version") == "" {
				t.Fatalf("unexpected Anthropic request: path=%s headers=%v", r.URL.Path, r.Header)
			}
			if r.URL.Query().Get("after_id") == "model-a" {
				_, _ = w.Write([]byte(`{"data":[{"id":"model-b"}],"has_more":false,"last_id":"model-b"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"model-a"}],"has_more":true,"last_id":"model-a"}`))
		}))
		defer upstream.Close()

		models, err := (anthropicModelDiscoveryAdapter{}).Discover(context.Background(), ProviderConnection{BaseURL: upstream.URL + "/v1"}, ProviderAccount{}, "anthropic-secret")
		if err != nil || strings.Join(models, ",") != "model-a,model-b" {
			t.Fatalf("models=%v err=%v", models, err)
		}
	})

	t.Run("gemini", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1beta/models" || r.Header.Get("x-goog-api-key") != "gemini-secret" {
				t.Fatalf("unexpected Gemini request: path=%s headers=%v", r.URL.Path, r.Header)
			}
			if r.URL.Query().Get("pageToken") == "next" {
				_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-b"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-a"}],"nextPageToken":"next"}`))
		}))
		defer upstream.Close()

		models, err := (geminiModelDiscoveryAdapter{}).Discover(context.Background(), ProviderConnection{BaseURL: upstream.URL + "/v1beta"}, ProviderAccount{}, "gemini-secret")
		if err != nil || strings.Join(models, ",") != "gemini-a,gemini-b" {
			t.Fatalf("models=%v err=%v", models, err)
		}
	})
}

func TestProviderModelDiscoveryRejectsRedirectsBeforeForwardingSecret(t *testing.T) {
	forwardedSecret := ""
	sink := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		forwardedSecret = r.Header.Get("Authorization")
	}))
	defer sink.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, sink.URL, http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()

	_, _, err := probeOpenAICompatibleModelsWithKey(context.Background(), upstream.URL, "sensitive-secret", "Provider account")
	if err == nil || !strings.Contains(err.Error(), "redirects are not allowed") {
		t.Fatalf("expected redirect rejection, got %v", err)
	}
	if forwardedSecret != "" {
		t.Fatalf("secret was forwarded across redirect: %q", forwardedSecret)
	}
}

func TestProviderModelDiscoveryAdapterCoverage(t *testing.T) {
	for _, providerType := range []string{"openai_compatible", "self_hosted", "anthropic", "gemini"} {
		if _, ok := providerModelDiscoveryAdapterFor(providerType); !ok {
			t.Fatalf("expected discovery adapter for %s", providerType)
		}
	}
	if _, ok := providerModelDiscoveryAdapterFor("azure_openai"); ok {
		t.Fatal("Azure OpenAI discovery must stay manual without management-plane credentials")
	}
}

func TestBulkCreateModelRoutesValidatesWholeBatchBeforeWrite(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "bulk-route-secret")
	provider, _ := svc.CreateProvider(ctx, "tester", ProviderRequest{Name: "Bulk provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1", Status: ProviderStatusActive, Models: []string{"upstream-a", "upstream-b"}, APIKey: "provider-secret"})
	account, _ := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{ProviderID: provider.ID, Name: "Bulk account", Platform: "openai_compatible", AuthType: "api_key", Status: AccountStatusActive, Models: []string{"upstream-a", "upstream-b"}, Secret: "account-secret"})
	modelA, _ := svc.CreateGatewayModel(ctx, "tester", GatewayModelRequest{ModelID: "public-a", Name: "Public A", Modality: "chat", Status: GatewayModelStatusActive})
	modelB, _ := svc.CreateGatewayModel(ctx, "tester", GatewayModelRequest{ModelID: "public-b", Name: "Public B", Modality: "chat", Status: GatewayModelStatusActive})

	_, err := svc.BulkCreateModelRoutes(ctx, "tester", ModelRouteBulkCreateRequest{Routes: []ModelRouteRequest{
		{GatewayModelID: modelA.ID, ProviderAccountID: account.ID, UpstreamModel: "upstream-a", Status: ModelRouteStatusActive},
		{GatewayModelID: modelB.ID, ProviderAccountID: account.ID, UpstreamModel: "not-exposed", Status: ModelRouteStatusActive},
	}})
	if err == nil {
		t.Fatal("BulkCreateModelRoutes() accepted an invalid second route")
	}
	routes, _ := svc.ListModelRoutes(ctx)
	if len(routes) != 0 {
		t.Fatalf("partial routes persisted: %+v", routes)
	}

	created, err := svc.BulkCreateModelRoutes(ctx, "tester", ModelRouteBulkCreateRequest{Routes: []ModelRouteRequest{
		{GatewayModelID: modelA.ID, ProviderAccountID: account.ID, UpstreamModel: "upstream-a", Status: ModelRouteStatusActive},
		{GatewayModelID: modelB.ID, ProviderAccountID: account.ID, UpstreamModel: "upstream-b", Status: ModelRouteStatusActive},
	}})
	if err != nil || len(created.Routes) != 2 {
		t.Fatalf("created=%+v err=%v", created, err)
	}
}

func findProviderAccountModel(models []ProviderAccountModel, modelID string) *ProviderAccountModel {
	for index := range models {
		if models[index].ModelID == modelID {
			return &models[index]
		}
	}
	return nil
}
