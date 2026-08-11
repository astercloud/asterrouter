package controlplane

import (
	"context"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

func TestRoutingPolicyOrdersBatchesAndControlsFailover(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Policy provider", Type: ProviderTypeOpenAICompatible, BaseURL: "https://provider.example/v1", Status: ProviderStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	primary := createRoutingPolicyTestAccount(t, svc, provider.ID, "Primary", "public-model", 2)
	secondary := createRoutingPolicyTestAccount(t, svc, provider.ID, "Secondary", "public-model", 0.5)
	unlisted := createRoutingPolicyTestAccount(t, svc, provider.ID, "Unlisted", "public-model", 0.1)
	model := mustCreateGatewayModelRoutes(t, svc, "public-model", []ProviderAccount{unlisted, secondary, primary})
	policy, err := svc.CreateRoutingPolicy(ctx, "tester", RoutingPolicyRequest{
		Name: "Ordered enterprise policy", RouteGroup: model.DefaultRouteGroup, Status: RoutingPolicyStatusActive,
		Strategy: RoutingPolicyStrategy{
			Preset: RoutingPolicyPresetCost, StickyRouting: true, StickyTTLSeconds: 900, FailoverBeforeFirstByte: true,
			ResourceBatches: []RoutingPolicyBatch{
				{Name: "Primary", ProviderAccountIDs: []string{primary.ID}},
				{Name: "Fallback", ProviderAccountIDs: []string{secondary.ID}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidates, hasRoutes, err := svc.GatewayProviderCandidatesForModel(ctx, "public-model")
	if err != nil || !hasRoutes || len(candidates) != 2 {
		t.Fatalf("candidates=%+v hasRoutes=%t err=%v", candidates, hasRoutes, err)
	}
	if candidates[0].AccountID != primary.ID || candidates[0].PolicyBatchOrder != 0 || candidates[1].AccountID != secondary.ID || candidates[1].PolicyBatchOrder != 1 {
		t.Fatalf("batch order was not enforced: %+v", candidates)
	}
	if !candidates[0].StickyEnabled || candidates[0].StickyTTLSeconds != 900 || candidates[0].RoutingPolicyID != policy.ID || !candidates[0].FailoverEnabled {
		t.Fatalf("routing policy runtime fields were not propagated: %+v", candidates[0])
	}
	binding := RoutingAffinityBinding{ProviderID: provider.ID, ProviderAccountID: secondary.ID, RouteID: candidates[1].RouteID}
	if preferred, ok := preferBoundGatewayCandidate(candidates, binding, true, "test"); ok || preferred[0].AccountID != primary.ID {
		t.Fatalf("affinity crossed a policy batch: ok=%t candidates=%+v", ok, preferred)
	}

	_, err = svc.UpdateRoutingPolicy(ctx, "tester", policy.ID, RoutingPolicyRequest{
		Name: policy.Name, RouteGroup: policy.RouteGroup, Status: policy.Status,
		Strategy: RoutingPolicyStrategy{
			Preset: RoutingPolicyPresetCost, StickyTTLSeconds: 900, FailoverBeforeFirstByte: false,
			ResourceBatches: policy.Strategy.ResourceBatches,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidates, _, err = svc.GatewayProviderCandidatesForModel(ctx, "public-model")
	if err != nil || len(candidates) != 2 {
		t.Fatalf("candidate resolution should preserve eligible resources before request constraints: %+v err=%v", candidates, err)
	}
	if candidates[0].StickyEnabled || candidates[0].FailoverEnabled {
		t.Fatalf("updated routing policy flags were not propagated: %+v", candidates[0])
	}
	plan, err := svc.PlanCanonicalGatewayRequest(ctx, gatewaycore.CanonicalAuthContext{CredentialID: "routing-policy-key"}, gatewaycore.CanonicalRequest{
		Protocol: gatewaycore.ProtocolOpenAIChat, Operation: GatewayOperationChatCompletion,
		Modality: GatewayModalityText, Lane: gatewaycore.LaneDirect, Model: "public-model",
	})
	if err != nil || len(plan.Candidates) != 1 || plan.Candidates[0].AccountID != primary.ID {
		t.Fatalf("disabled failover should expose only the first constrained candidate: %+v err=%v", plan, err)
	}
}

func TestRoutingPolicyModelScopeAndPriceGuardrails(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "test-secret")
	now := time.Now().UTC().Add(-time.Minute)
	for _, price := range []ProcurementPrice{
		{ID: "price-cheap", ProviderAccountID: "cheap", UpstreamModel: "upstream", Protocol: "openai_chat_completions", Currency: "USD", UncachedInputMicrosPer1MTokens: 100_000, OutputMicrosPer1MTokens: 200_000, RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now},
		{ID: "price-expensive", ProviderAccountID: "expensive", UpstreamModel: "upstream", Protocol: "openai_chat_completions", Currency: "USD", UncachedInputMicrosPer1MTokens: 1_000_000, OutputMicrosPer1MTokens: 2_000_000, RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now},
	} {
		if err := repo.SaveProcurementPrice(ctx, price); err != nil {
			t.Fatal(err)
		}
	}
	policy := &RoutingPolicy{Strategy: RoutingPolicyStrategy{
		Preset: RoutingPolicyPresetCost, MaxPriceMultipleOfCheapest: 2, LowPricePoolMode: RoutingPolicyLowPriceNone,
	}}
	candidates := []GatewayProvider{
		{AccountID: "expensive", UpstreamModel: "upstream"},
		{AccountID: "missing", UpstreamModel: "upstream"},
		{AccountID: "cheap", UpstreamModel: "upstream"},
	}
	filtered, err := svc.applyRoutingPolicyPriceRules(ctx, policy, "openai_chat_completions", candidates)
	if err != nil || len(filtered) != 1 || filtered[0].AccountID != "cheap" {
		t.Fatalf("price guardrail result=%+v err=%v", filtered, err)
	}
	if routingPolicyAllowsModel(RoutingPolicyStrategy{AllowedModels: []string{"allowed"}}, "denied") {
		t.Fatal("model outside allowlist must be rejected")
	}
	if routingPolicyAllowsModel(RoutingPolicyStrategy{DeniedModels: []string{"blocked"}}, "blocked:stable") {
		t.Fatal("base model denylist must reject qualified model ids")
	}
}

func TestRoutingPolicyAutomaticLowPricePoolUsesNormalizedDefaults(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "test-secret")
	policy, err := routingPolicyFromRequest(RoutingPolicyRequest{
		Name: "Automatic low price pool",
		Strategy: RoutingPolicyStrategy{
			Preset: RoutingPolicyPresetBalanced, StickyTTLSeconds: 900,
		},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if policy.Strategy.LowPricePoolPercent != 30 || policy.Strategy.LowPricePoolMinCandidates != 2 {
		t.Fatalf("automatic low price defaults were not normalized: %+v", policy.Strategy)
	}

	now := time.Now().UTC().Add(-time.Minute)
	candidates := make([]GatewayProvider, 0, 4)
	for index, accountID := range []string{"account-4", "account-2", "account-1", "account-3"} {
		priceRank := int64(4 - index)
		if accountID == "account-1" {
			priceRank = 1
		} else if accountID == "account-2" {
			priceRank = 2
		} else if accountID == "account-3" {
			priceRank = 3
		}
		if err := repo.SaveProcurementPrice(ctx, ProcurementPrice{
			ID: "price-" + accountID, ProviderAccountID: accountID, UpstreamModel: "upstream",
			Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD",
			UncachedInputMicrosPer1MTokens: priceRank * 100_000, OutputMicrosPer1MTokens: priceRank * 100_000,
			RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now,
		}); err != nil {
			t.Fatal(err)
		}
		candidates = append(candidates, GatewayProvider{AccountID: accountID, UpstreamModel: "upstream"})
	}

	filtered, err := svc.applyRoutingPolicyPriceRules(ctx, &policy, string(gatewaycore.ProtocolOpenAIChat), candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 2 || filtered[0].AccountID != "account-2" || filtered[1].AccountID != "account-1" {
		t.Fatalf("automatic low price pool must preserve the non-cost preference order: %+v", filtered)
	}

	policy.Strategy.Preset = RoutingPolicyPresetCost
	costOrdered, err := svc.applyRoutingPolicyPriceRules(ctx, &policy, string(gatewaycore.ProtocolOpenAIChat), candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(costOrdered) != 2 || costOrdered[0].AccountID != "account-1" || costOrdered[1].AccountID != "account-2" {
		t.Fatalf("cost preference did not order the retained pool by price: %+v", costOrdered)
	}

	policy.Strategy.Preset = RoutingPolicyPresetBalanced
	policy.Strategy.LowPricePoolMode = RoutingPolicyLowPriceStrict
	policy.Strategy.LowPricePoolMinCandidates = 20
	strict, err := svc.applyRoutingPolicyPriceRules(ctx, &policy, string(gatewaycore.ProtocolOpenAIChat), candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(strict) != 1 || strict[0].AccountID != "account-1" {
		t.Fatalf("strict low price pool must retain exactly the cheapest candidate: %+v", strict)
	}

	withoutFacts, err := svc.applyRoutingPolicyPriceRules(ctx, &policy, string(gatewaycore.ProtocolAnthropicMessages), candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutFacts) != len(candidates) || withoutFacts[0].AccountID != candidates[0].AccountID {
		t.Fatalf("candidates changed without comparable price facts: %+v", withoutFacts)
	}
}

func TestRoutingPolicyProtocolMatrixIsCompleteAndRejectsUnknownValues(t *testing.T) {
	tests := []struct {
		protocol gatewaycore.Protocol
		format   string
	}{
		{gatewaycore.ProtocolOpenAIChat, UpstreamFormatOpenAIChat},
		{gatewaycore.ProtocolOpenAIResponses, UpstreamFormatOpenAIResponses},
		{gatewaycore.ProtocolOpenAIEmbeddings, UpstreamFormatOpenAIEmbeddings},
		{gatewaycore.ProtocolAnthropicMessages, UpstreamFormatAnthropic},
		{gatewaycore.ProtocolAnthropicCountTokens, UpstreamFormatAnthropic},
		{gatewaycore.ProtocolGeminiGenerate, UpstreamFormatGemini},
		{gatewaycore.ProtocolOpenAIImages, UpstreamFormatNativeMedia},
		{gatewaycore.ProtocolOpenAIMedia, UpstreamFormatNativeMedia},
		{gatewaycore.ProtocolOpenAIAudioTranscriptions, UpstreamFormatNativeMedia},
		{gatewaycore.ProtocolOpenAIAudioTranslations, UpstreamFormatNativeMedia},
		{gatewaycore.ProtocolOpenAIAudioSpeech, UpstreamFormatNativeMedia},
		{gatewaycore.ProtocolRealtime, UpstreamFormatNativeMedia},
		{gatewaycore.ProtocolAsterJobs, UpstreamFormatNativeMedia},
	}
	for _, test := range tests {
		if !routingPolicyProtocolSupported(string(test.protocol)) {
			t.Errorf("supported protocol %q was rejected", test.protocol)
		}
		if !routingPolicyNativeProtocolMatches(string(test.protocol), test.format) {
			t.Errorf("native protocol %q did not match format %q", test.protocol, test.format)
		}
		if routingPolicyNativeProtocolMatches(string(test.protocol), UpstreamFormatBedrockConverse) {
			t.Errorf("native protocol %q incorrectly matched Bedrock format", test.protocol)
		}
	}
	if routingPolicyProtocolSupported("unknown_protocol") {
		t.Fatal("unknown routing policy protocol was accepted")
	}
}

func TestActiveRoutingPricesUsesLatestComparableFacts(t *testing.T) {
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	expiredAt := now.Add(-time.Minute)
	prices := []ProcurementPrice{
		{ID: "older", ProviderAccountID: "account-a", UpstreamModel: "model-a", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD", Status: ProcurementPriceStatusActive, EffectiveFrom: now.Add(-2 * time.Hour)},
		{ID: "latest", ProviderAccountID: "account-a", UpstreamModel: "model-a", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "usd", Status: ProcurementPriceStatusActive, EffectiveFrom: now.Add(-time.Hour)},
		{ID: "expired", ProviderAccountID: "account-b", UpstreamModel: "model-b", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD", Status: ProcurementPriceStatusActive, EffectiveFrom: now.Add(-2 * time.Hour), ExpiresAt: &expiredAt},
		{ID: "future", ProviderAccountID: "account-c", UpstreamModel: "model-c", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD", Status: ProcurementPriceStatusActive, EffectiveFrom: now.Add(time.Hour)},
		{ID: "wrong-currency", ProviderAccountID: "account-d", UpstreamModel: "model-d", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "EUR", Status: ProcurementPriceStatusActive, EffectiveFrom: now.Add(-time.Hour)},
		{ID: "wrong-protocol", ProviderAccountID: "account-e", UpstreamModel: "model-e", Protocol: string(gatewaycore.ProtocolOpenAIResponses), Currency: "USD", Status: ProcurementPriceStatusActive, EffectiveFrom: now.Add(-time.Hour)},
	}
	active := activeRoutingPrices(prices, string(gatewaycore.ProtocolOpenAIChat), now)
	if len(active) != 1 || active[routingPriceKey("account-a", "model-a")].ID != "latest" {
		t.Fatalf("active routing prices=%+v", active)
	}
}

func TestRoutingPolicyProtocolAdmissionAndNativeProtocolAreEnforcedByPlanner(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Protocol provider", Type: ProviderTypeOpenAICompatible, BaseURL: "https://provider.example/v1", Status: ProviderStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Protocol account", Platform: ProviderTypeOpenAICompatible, AuthType: ProviderAuthAPIKey,
		Status: AccountStatusActive, Models: []string{"chat-upstream", "responses-upstream"}, Secret: "protocol-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := svc.CreateGatewayModel(ctx, "tester", GatewayModelRequest{
		ModelID: "protocol-model", Name: "Protocol model", Modality: "chat", Status: GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []ModelRouteRequest{
		{GatewayModelID: model.ID, ProviderAccountID: account.ID, UpstreamModel: "responses-upstream", UpstreamFormat: UpstreamFormatOpenAIResponses, Priority: 10, Status: ModelRouteStatusActive},
		{GatewayModelID: model.ID, ProviderAccountID: account.ID, UpstreamModel: "chat-upstream", UpstreamFormat: UpstreamFormatOpenAIChat, Priority: 20, Status: ModelRouteStatusActive},
	} {
		if _, err := svc.CreateModelRoute(ctx, "tester", route); err != nil {
			t.Fatal(err)
		}
	}
	policy, err := svc.CreateRoutingPolicy(ctx, "tester", RoutingPolicyRequest{
		Name: "Native protocol policy", RouteGroup: DefaultModelRouteGroup, Status: RoutingPolicyStatusActive,
		Strategy: RoutingPolicyStrategy{
			Preset: RoutingPolicyPresetBalanced, StickyTTLSeconds: 900, NativeProtocolOnly: true,
			LowPricePoolMode: RoutingPolicyLowPriceNone,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := gatewaycore.CanonicalAuthContext{CredentialID: "protocol-key"}
	request := gatewaycore.CanonicalRequest{
		Protocol: gatewaycore.ProtocolOpenAIChat, Operation: GatewayOperationChatCompletion,
		Modality: GatewayModalityText, Lane: gatewaycore.LaneDirect, Model: model.ModelID,
	}
	plan, err := svc.PlanCanonicalGatewayRequest(ctx, auth, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Candidates) != 1 || plan.Candidates[0].UpstreamFormat != UpstreamFormatOpenAIChat {
		t.Fatalf("native protocol filter plan=%+v", plan)
	}

	_, err = svc.UpdateRoutingPolicy(ctx, "tester", policy.ID, RoutingPolicyRequest{
		Name: policy.Name, RouteGroup: policy.RouteGroup, Status: policy.Status,
		Strategy: RoutingPolicyStrategy{
			Preset: RoutingPolicyPresetBalanced, StickyTTLSeconds: 900,
			AllowedProtocols: []string{string(gatewaycore.ProtocolOpenAIChat)},
			DeniedProtocols:  []string{string(gatewaycore.ProtocolOpenAIChat)},
			LowPricePoolMode: RoutingPolicyLowPriceNone,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := svc.PlanCanonicalGatewayRequest(ctx, auth, request)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.RejectionReason != "routing_policy_protocol_blocked" || len(blocked.Candidates) != 0 {
		t.Fatalf("protocol deny rule did not take precedence: %+v", blocked)
	}
}

func createRoutingPolicyTestAccount(t *testing.T, svc *Service, providerID, name, model string, rate float64) ProviderAccount {
	t.Helper()
	account, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID: providerID, Name: name, Platform: ProviderTypeOpenAICompatible, AuthType: ProviderAuthAPIKey,
		Status: AccountStatusActive, RateMultiplier: rate, Models: []string{model}, Secret: name + "-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	return account
}
