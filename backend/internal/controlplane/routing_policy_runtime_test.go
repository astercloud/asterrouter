package controlplane

import (
	"context"
	"fmt"
	"strings"
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
	now := time.Now().UTC().Add(-time.Minute)
	for _, price := range []ProcurementPrice{
		{ID: "price-primary", ProviderAccountID: primary.ID, UpstreamModel: "public-model", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD", UncachedInputMicrosPer1MTokens: 1_000_000, OutputMicrosPer1MTokens: 1_000_000, RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now},
		{ID: "price-secondary", ProviderAccountID: secondary.ID, UpstreamModel: "public-model", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD", RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now},
	} {
		if err := svc.repo.SaveProcurementPrice(ctx, price); err != nil {
			t.Fatal(err)
		}
	}
	policy, err = svc.UpdateRoutingPolicy(ctx, "tester", policy.ID, RoutingPolicyRequest{
		Name: policy.Name, RouteGroup: policy.RouteGroup, Status: policy.Status,
		Strategy: RoutingPolicyStrategy{
			Preset: RoutingPolicyPresetCost, StickyRouting: true, StickyTTLSeconds: 900, FailoverBeforeFirstByte: true,
			MaxPriceMultipleOfCheapest: 2, LowPricePoolMode: RoutingPolicyLowPriceNone, ResourceBatches: policy.Strategy.ResourceBatches,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidates, _, err = svc.GatewayProviderCandidatesForModel(ctx, "public-model")
	if err != nil {
		t.Fatal(err)
	}
	priceDecision, err := svc.applyRoutingPolicyCandidateRules(ctx, &policy, string(gatewaycore.ProtocolOpenAIChat), candidates)
	if err != nil || len(priceDecision.Candidates) != 2 || priceDecision.Candidates[0].AccountID != primary.ID || priceDecision.Candidates[1].AccountID != secondary.ID {
		t.Fatalf("relative price guardrail crossed resource batches: decision=%+v err=%v", priceDecision, err)
	}
	unpricedPrimary := candidates[0]
	unpricedPrimary.AccountID = "unpriced-primary"
	priceDecision, err = svc.applyRoutingPolicyCandidateRules(ctx, &policy, string(gatewaycore.ProtocolOpenAIChat), []GatewayProvider{unpricedPrimary, candidates[1]})
	if err != nil || len(priceDecision.Candidates) != 2 || priceDecision.Candidates[0].AccountID != unpricedPrimary.AccountID || priceDecision.Candidates[1].AccountID != secondary.ID {
		t.Fatalf("fallback price facts excluded an unpriced primary batch: decision=%+v err=%v", priceDecision, err)
	}
	pricePlan, err := svc.PlanCanonicalGatewayRequest(ctx, gatewaycore.CanonicalAuthContext{CredentialID: "routing-policy-key"}, gatewaycore.CanonicalRequest{
		Protocol: gatewaycore.ProtocolOpenAIChat, Operation: GatewayOperationChatCompletion,
		Modality: GatewayModalityText, Lane: gatewaycore.LaneDirect, Model: "public-model",
	})
	if err != nil || len(pricePlan.Candidates) != 2 || pricePlan.Candidates[0].AccountID != primary.ID || pricePlan.Candidates[1].AccountID != secondary.ID {
		t.Fatalf("planner relative price guardrail crossed resource batches: plan=%+v err=%v", pricePlan, err)
	}
	priceSimulation, err := svc.SimulateGatewayRouting(ctx, GatewaySimulationRequest{Model: "public-model", Protocol: string(gatewaycore.ProtocolOpenAIChat)})
	if err != nil {
		t.Fatal(err)
	}
	assertSimulationCandidateReason(t, priceSimulation, primary.ID, "")
	assertSimulationCandidateReason(t, priceSimulation, secondary.ID, "")
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
		Preset: RoutingPolicyPresetCost, MaxPriceMultipleOfCheapest: 2, LowPricePoolMode: RoutingPolicyLowPriceNone, MissingPriceAction: RoutingPolicyMissingPriceBlock,
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

func TestRoutingPolicyModelPriceLimitsAndMissingPriceAction(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "test-secret")
	now := time.Now().UTC().Add(-time.Minute)
	if err := repo.SaveProcurementPrice(ctx, ProcurementPrice{
		ID: "priced", ProviderAccountID: "priced", UpstreamModel: "upstream",
		Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD",
		UncachedInputMicrosPer1MTokens: 600_000, OutputMicrosPer1MTokens: 600_000,
		RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now,
	}); err != nil {
		t.Fatal(err)
	}
	candidates := []GatewayProvider{
		{RouteID: "unknown", AccountID: "unknown", RequestedModel: "enterprise-model:stable", UpstreamModel: "upstream"},
		{RouteID: "priced", AccountID: "priced", RequestedModel: "enterprise-model:stable", UpstreamModel: "upstream"},
	}
	policy := &RoutingPolicy{Strategy: RoutingPolicyStrategy{
		Preset: RoutingPolicyPresetBalanced, MissingPriceAction: RoutingPolicyMissingPriceAllow,
		AbsoluteMaxInputPer1M: 1, AbsoluteMaxOutputPer1M: 1,
		ModelPriceLimits: []RoutingPolicyModelPriceLimit{{Model: "enterprise-model", AbsoluteMaxInputPer1M: 0.5, AbsoluteMaxOutputPer1M: 0.5}},
	}}
	decision, err := svc.evaluateRoutingPolicyPriceRules(ctx, policy, string(gatewaycore.ProtocolOpenAIChat), candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.candidates) != 1 || decision.candidates[0].AccountID != "unknown" {
		t.Fatalf("allow must retain unknown price while the stricter model cap rejects priced candidate: %+v", decision)
	}
	if len(decision.exclusions) != 1 || decision.exclusions[0].reason != "routing_policy_input_price_exceeded" {
		t.Fatalf("model price cap exclusion=%+v", decision.exclusions)
	}
	policy.Strategy.MissingPriceAction = RoutingPolicyMissingPriceBlock
	decision, err = svc.evaluateRoutingPolicyPriceRules(ctx, policy, string(gatewaycore.ProtocolOpenAIChat), candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.candidates) != 0 || len(decision.exclusions) != 2 {
		t.Fatalf("block must reject both unknown and over-cap candidates: %+v", decision)
	}
	wantReasons := map[string]bool{
		"routing_policy_price_fact_missing":   false,
		"routing_policy_input_price_exceeded": false,
	}
	for _, exclusion := range decision.exclusions {
		if _, ok := wantReasons[exclusion.reason]; !ok {
			t.Fatalf("unexpected exclusion=%+v", exclusion)
		}
		wantReasons[exclusion.reason] = true
	}
	for reason, found := range wantReasons {
		if !found {
			t.Fatalf("missing exclusion %q: %+v", reason, decision.exclusions)
		}
	}

	policy.Strategy = RoutingPolicyStrategy{
		Preset: RoutingPolicyPresetBalanced, MissingPriceAction: RoutingPolicyMissingPriceBlock,
		LowPricePoolMode: RoutingPolicyLowPriceNone,
	}
	decision, err = svc.evaluateRoutingPolicyPriceRules(ctx, policy, string(gatewaycore.ProtocolOpenAIChat), candidates[:1])
	if err != nil || len(decision.candidates) != 0 || len(decision.exclusions) != 1 || decision.exclusions[0].reason != "routing_policy_price_fact_missing" {
		t.Fatalf("missing-price block must remain a standalone hard constraint: decision=%+v err=%v", decision, err)
	}
}

func TestRoutingPolicyInputCapCoversUnquotedCachePrices(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "test-secret")
	now := time.Now().UTC().Add(-time.Minute)
	if err := repo.SaveProcurementPrice(ctx, ProcurementPrice{
		ID: "cache-expensive", ProviderAccountID: "cache-expensive", UpstreamModel: "upstream",
		Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD",
		UncachedInputMicrosPer1MTokens: 100_000, CacheReadMicrosPer1MTokens: 2_000_000,
		OutputMicrosPer1MTokens: 100_000, RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveProcurementPrice(ctx, ProcurementPrice{
		ID: "cache-unquoted", ProviderAccountID: "cache-unquoted", UpstreamModel: "upstream",
		Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD",
		UncachedInputMicrosPer1MTokens: 1_500_000, CacheReadMicrosPer1MTokens: 0,
		OutputMicrosPer1MTokens: 100_000, RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now,
	}); err != nil {
		t.Fatal(err)
	}
	policy := &RoutingPolicy{Strategy: RoutingPolicyStrategy{
		Preset: RoutingPolicyPresetBalanced, AbsoluteMaxInputPer1M: 1,
		LowPricePoolMode: RoutingPolicyLowPriceNone, MissingPriceAction: RoutingPolicyMissingPriceBlock,
	}}
	decision, err := svc.evaluateRoutingPolicyPriceRules(ctx, policy, string(gatewaycore.ProtocolOpenAIChat), []GatewayProvider{
		{RouteID: "cache-expensive", AccountID: "cache-expensive", UpstreamModel: "upstream"},
		{RouteID: "cache-unquoted", AccountID: "cache-unquoted", UpstreamModel: "upstream"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.candidates) != 0 || len(decision.exclusions) != 2 {
		t.Fatalf("input cap must reject quoted and unquoted cache prices: %+v", decision)
	}
	for _, exclusion := range decision.exclusions {
		if exclusion.reason != "routing_policy_input_price_exceeded" {
			t.Fatalf("unexpected cache price exclusion: %+v", exclusion)
		}
	}
}

func TestRoutingPolicyInputPriceCapChecksEveryCachePriceBoundary(t *testing.T) {
	const limit = int64(1_000_000)
	tests := []struct {
		name  string
		price ProcurementPrice
		want  bool
	}{
		{name: "all below", price: ProcurementPrice{UncachedInputMicrosPer1MTokens: 900_000, CacheReadMicrosPer1MTokens: 800_000, CacheWrite5mMicrosPer1MTokens: 900_000, CacheWrite1hMicrosPer1MTokens: 950_000}},
		{name: "equal limit", price: ProcurementPrice{UncachedInputMicrosPer1MTokens: limit, CacheReadMicrosPer1MTokens: limit, CacheWrite5mMicrosPer1MTokens: limit, CacheWrite1hMicrosPer1MTokens: limit}},
		{name: "cache read above", price: ProcurementPrice{UncachedInputMicrosPer1MTokens: 100_000, CacheReadMicrosPer1MTokens: limit + 1}, want: true},
		{name: "five minute cache write above", price: ProcurementPrice{UncachedInputMicrosPer1MTokens: 100_000, CacheWrite5mMicrosPer1MTokens: limit + 1}, want: true},
		{name: "one hour cache write above", price: ProcurementPrice{UncachedInputMicrosPer1MTokens: 100_000, CacheWrite1hMicrosPer1MTokens: limit + 1}, want: true},
		{name: "unquoted cache uses uncached below", price: ProcurementPrice{UncachedInputMicrosPer1MTokens: limit - 1}},
		{name: "unquoted cache uses uncached above", price: ProcurementPrice{UncachedInputMicrosPer1MTokens: limit + 1}, want: true},
		{name: "mixed quotes reject highest", price: ProcurementPrice{UncachedInputMicrosPer1MTokens: 100_000, CacheReadMicrosPer1MTokens: 500_000, CacheWrite5mMicrosPer1MTokens: limit + 1, CacheWrite1hMicrosPer1MTokens: 300_000}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := routingPolicyInputPriceExceeded(test.price, limit); got != test.want {
				t.Fatalf("routingPolicyInputPriceExceeded()=%v want=%v price=%+v", got, test.want, test.price)
			}
		})
	}
}

func TestRoutingPolicyStrictLowPricePoolUsesTopThirtyPercentWithFloor(t *testing.T) {
	policy, err := routingPolicyFromRequest(RoutingPolicyRequest{
		Name: "Strict low price pool", Strategy: RoutingPolicyStrategy{
			Preset: RoutingPolicyPresetBalanced, StickyTTLSeconds: 900, LowPricePoolMode: RoutingPolicyLowPriceStrict,
			LowPricePoolPercent: 40, LowPricePoolMinCandidates: 1,
		},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if policy.Strategy.LowPricePoolPercent != 30 || policy.Strategy.LowPricePoolMinCandidates != 2 {
		t.Fatalf("strict low price defaults were not normalized: %+v", policy.Strategy)
	}
	candidates := make([]routingPolicyCandidatePrice, 0, 5)
	for index, price := range []int64{100, 200, 300, 400, 500} {
		candidates = append(candidates, routingPolicyCandidatePrice{
			candidate: GatewayProvider{AccountID: fmt.Sprintf("account-%d", index+1), PolicyBatchOrder: 0},
			price:     price, priced: true, order: index,
		})
	}
	retained := lowPriceCandidatePool(candidates, policy.Strategy)
	if len(retained) != 2 || retained[0].price != 100 || retained[1].price != 200 {
		t.Fatalf("strict pool must retain top 30%% with a two-candidate floor: %+v", retained)
	}
}

func TestRoutingPolicyStrictOrderKeepsDeclaredOrder(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "test-secret")
	now := time.Now().UTC().Add(-time.Minute)
	for _, price := range []ProcurementPrice{
		{ID: "first", ProviderAccountID: "first", UpstreamModel: "upstream", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD", UncachedInputMicrosPer1MTokens: 900_000, OutputMicrosPer1MTokens: 900_000, RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now},
		{ID: "second", ProviderAccountID: "second", UpstreamModel: "upstream", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD", UncachedInputMicrosPer1MTokens: 100_000, OutputMicrosPer1MTokens: 100_000, RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now},
	} {
		if err := repo.SaveProcurementPrice(ctx, price); err != nil {
			t.Fatal(err)
		}
	}
	policy := &RoutingPolicy{Strategy: RoutingPolicyStrategy{
		Preset: RoutingPolicyPresetCost, StrictOrder: true, LowPricePoolMode: RoutingPolicyLowPriceNone,
	}}
	candidates := []GatewayProvider{
		{RouteID: "first", AccountID: "first", RequestedModel: "model", UpstreamModel: "upstream"},
		{RouteID: "second", AccountID: "second", RequestedModel: "model", UpstreamModel: "upstream"},
	}
	decision, err := svc.evaluateRoutingPolicyPriceRules(ctx, policy, string(gatewaycore.ProtocolOpenAIChat), candidates)
	if err != nil || len(decision.candidates) != 2 || decision.candidates[0].AccountID != "first" || decision.candidates[1].AccountID != "second" {
		t.Fatalf("strict order changed by price: decision=%+v err=%v", decision, err)
	}
}

func TestRoutingPolicyPreferredResourcesStayWithinTheirDeclaredBatch(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Preferred provider", Type: ProviderTypeOpenAICompatible, BaseURL: "https://provider.example/v1", Status: ProviderStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	primary := createRoutingPolicyTestAccount(t, svc, provider.ID, "Primary", "preferred-model", 1)
	preferredPrimary := createRoutingPolicyTestAccount(t, svc, provider.ID, "Preferred primary", "preferred-model", 1)
	preferredFallback := createRoutingPolicyTestAccount(t, svc, provider.ID, "Preferred fallback", "preferred-model", 1)
	model := mustCreateGatewayModelRoutes(t, svc, "preferred-model", []ProviderAccount{primary, preferredPrimary, preferredFallback})
	now := time.Now().UTC().Add(-time.Minute)
	for index, account := range []ProviderAccount{primary, preferredPrimary, preferredFallback} {
		if err := svc.repo.SaveProcurementPrice(ctx, ProcurementPrice{
			ID: "preferred-price-" + fmt.Sprint(index), ProviderAccountID: account.ID, UpstreamModel: "preferred-model",
			Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD",
			UncachedInputMicrosPer1MTokens: int64(index+1) * 100_000, OutputMicrosPer1MTokens: int64(index+1) * 100_000,
			RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	policy, err := svc.CreateRoutingPolicy(ctx, "tester", RoutingPolicyRequest{
		Name: "Preferred resources", RouteGroup: model.DefaultRouteGroup, Status: RoutingPolicyStatusActive,
		Strategy: RoutingPolicyStrategy{
			Preset: RoutingPolicyPresetCost, StickyTTLSeconds: 900, FailoverBeforeFirstByte: true, LowPricePoolMode: RoutingPolicyLowPriceNone,
			PreferredProviderAccountIDs: []string{preferredPrimary.ID, preferredFallback.ID},
			ResourceBatches: []RoutingPolicyBatch{
				{Name: "Primary", ProviderAccountIDs: []string{primary.ID, preferredPrimary.ID}},
				{Name: "Fallback", ProviderAccountIDs: []string{preferredFallback.ID}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidates, _, err := svc.GatewayProviderCandidatesForModel(ctx, model.ModelID)
	if err != nil || len(candidates) != 3 {
		t.Fatalf("preferred candidates=%+v err=%v", candidates, err)
	}
	if candidates[0].AccountID != preferredPrimary.ID || candidates[1].AccountID != primary.ID || candidates[2].AccountID != preferredFallback.ID {
		t.Fatalf("preferred resources must reorder only inside a batch: %+v", candidates)
	}

	policy.Strategy.StrictOrder = true
	if _, err := svc.UpdateRoutingPolicy(ctx, "tester", policy.ID, RoutingPolicyRequest{
		Name: policy.Name, RouteGroup: policy.RouteGroup, Status: policy.Status, Strategy: policy.Strategy,
	}); err != nil {
		t.Fatal(err)
	}
	candidates, _, err = svc.GatewayProviderCandidatesForModel(ctx, model.ModelID)
	if err != nil || len(candidates) != 3 || candidates[0].AccountID != primary.ID || candidates[1].AccountID != preferredPrimary.ID || candidates[2].AccountID != preferredFallback.ID {
		t.Fatalf("strict order must override preferred resources: candidates=%+v err=%v", candidates, err)
	}
}

func TestRoutingPolicyDisablesRandomWeightTieBreakWhenSmartOptimizationIsOff(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Stable ordering provider", Type: ProviderTypeOpenAICompatible, BaseURL: "https://provider.example/v1", Status: ProviderStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	createAccount := func(name string, weight int) ProviderAccount {
		account, createErr := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{
			ProviderID: provider.ID, Name: name, Platform: ProviderTypeOpenAICompatible, AuthType: ProviderAuthAPIKey,
			Status: AccountStatusActive, Priority: 10, Weight: weight, Concurrency: 10,
			RateMultiplier: 1, Models: []string{"stable-order-upstream"}, Secret: name + "-secret",
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return account
	}
	first := createAccount("First", 1)
	second := createAccount("Second", 1000)
	model, err := svc.CreateGatewayModel(ctx, "tester", GatewayModelRequest{ModelID: "stable-order-model", Name: "Stable order", Status: GatewayModelStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range []ProviderAccount{first, second} {
		if _, err := svc.CreateModelRoute(ctx, "tester", ModelRouteRequest{
			GatewayModelID: model.ID, RouteGroup: DefaultModelRouteGroup, ProviderAccountID: account.ID,
			UpstreamModel: "stable-order-upstream", UpstreamFormat: UpstreamFormatOpenAIChat,
			Priority: 10, Weight: 100, Status: ModelRouteStatusActive,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.CreateRoutingPolicy(ctx, "tester", RoutingPolicyRequest{
		Name: "Stable order policy", RouteGroup: DefaultModelRouteGroup, Status: RoutingPolicyStatusActive,
		Strategy: RoutingPolicyStrategy{
			Preset: RoutingPolicyPresetBalanced, SmartOptimization: false, LowPricePoolMode: RoutingPolicyLowPriceNone,
			ResourceBatches: []RoutingPolicyBatch{{Name: "Declared", ProviderAccountIDs: []string{first.ID, second.ID}}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	var firstOrder string
	for attempt := 0; attempt < 30; attempt++ {
		candidates, _, err := svc.GatewayProviderCandidatesForModel(ctx, model.ModelID)
		if err != nil || len(candidates) != 2 {
			t.Fatalf("candidate resolution attempt %d: candidates=%+v err=%v", attempt, candidates, err)
		}
		order := candidates[0].AccountID + "," + candidates[1].AccountID
		if attempt == 0 {
			firstOrder = order
		} else if order != firstOrder {
			t.Fatalf("smart optimization disabled but tie-break order changed: first=%s current=%s", firstOrder, order)
		}
	}
}

func TestRoutingPolicyPriceExclusionsRemainExplainable(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "test-secret")
	now := time.Now().UTC().Add(-time.Minute)
	for _, price := range []ProcurementPrice{
		{ID: "price-cheap", ProviderAccountID: "cheap", UpstreamModel: "upstream", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD", UncachedInputMicrosPer1MTokens: 100_000, OutputMicrosPer1MTokens: 100_000, RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now},
		{ID: "price-pool", ProviderAccountID: "pool", UpstreamModel: "upstream", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD", UncachedInputMicrosPer1MTokens: 150_000, OutputMicrosPer1MTokens: 150_000, RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now},
		{ID: "price-relative", ProviderAccountID: "relative", UpstreamModel: "upstream", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD", UncachedInputMicrosPer1MTokens: 800_000, OutputMicrosPer1MTokens: 800_000, RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now},
		{ID: "price-input", ProviderAccountID: "input", UpstreamModel: "upstream", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD", UncachedInputMicrosPer1MTokens: 2_000_000, OutputMicrosPer1MTokens: 100_000, RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now},
		{ID: "price-output", ProviderAccountID: "output", UpstreamModel: "upstream", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD", UncachedInputMicrosPer1MTokens: 100_000, OutputMicrosPer1MTokens: 2_000_000, RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now},
	} {
		if err := repo.SaveProcurementPrice(ctx, price); err != nil {
			t.Fatal(err)
		}
	}
	policy := &RoutingPolicy{Strategy: RoutingPolicyStrategy{
		Preset: RoutingPolicyPresetBalanced, AbsoluteMaxInputPer1M: 1, AbsoluteMaxOutputPer1M: 1,
		MaxPriceMultipleOfCheapest: 2, LowPricePoolMode: RoutingPolicyLowPriceStrict, MissingPriceAction: RoutingPolicyMissingPriceBlock,
	}}
	candidates := []GatewayProvider{
		{RouteID: "route-missing", AccountID: "missing", UpstreamModel: "upstream"},
		{RouteID: "route-input", AccountID: "input", UpstreamModel: "upstream"},
		{RouteID: "route-output", AccountID: "output", UpstreamModel: "upstream"},
		{RouteID: "route-relative", AccountID: "relative", UpstreamModel: "upstream"},
		{RouteID: "route-pool", AccountID: "pool", UpstreamModel: "upstream"},
		{RouteID: "route-cheap", AccountID: "cheap", UpstreamModel: "upstream"},
	}
	decision, err := svc.evaluateRoutingPolicyPriceRules(ctx, policy, string(gatewaycore.ProtocolOpenAIChat), candidates)
	if err != nil || len(decision.candidates) != 1 || decision.candidates[0].AccountID != "cheap" {
		t.Fatalf("price decision=%+v err=%v", decision, err)
	}
	wantReasons := map[string]string{
		"missing":  "routing_policy_price_fact_missing",
		"input":    "routing_policy_input_price_exceeded",
		"output":   "routing_policy_output_price_exceeded",
		"relative": "routing_policy_relative_price_exceeded",
		"pool":     "routing_policy_low_price_pool_excluded",
	}
	if len(decision.exclusions) != len(wantReasons) {
		t.Fatalf("price exclusions=%+v", decision.exclusions)
	}
	for _, exclusion := range decision.exclusions {
		if wantReasons[exclusion.candidate.AccountID] != exclusion.reason {
			t.Fatalf("unexpected price exclusion=%+v", exclusion)
		}
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
	if policy.Strategy.LowPricePoolPercent != 70 || policy.Strategy.LowPricePoolMinCandidates != 2 {
		t.Fatalf("automatic low price defaults were not normalized: %+v", policy.Strategy)
	}
	customized, err := routingPolicyFromRequest(RoutingPolicyRequest{
		Name: "Automatic pool with custom floor",
		Strategy: RoutingPolicyStrategy{
			Preset: RoutingPolicyPresetBalanced, StickyTTLSeconds: 900, LowPricePoolMode: RoutingPolicyLowPriceAuto,
			LowPricePoolPercent: 40, LowPricePoolMinCandidates: 3,
		},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if customized.Strategy.LowPricePoolPercent != 70 || customized.Strategy.LowPricePoolMinCandidates != 3 {
		t.Fatalf("automatic mode must fix the percentile while retaining its configurable candidate floor: %+v", customized.Strategy)
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
	if len(filtered) != 3 || filtered[0].AccountID != "account-2" || filtered[1].AccountID != "account-1" || filtered[2].AccountID != "account-3" {
		t.Fatalf("automatic low price pool must retain the cheapest 70%% and preserve non-cost preference order: %+v", filtered)
	}

	policy.Strategy.Preset = RoutingPolicyPresetCost
	costOrdered, err := svc.applyRoutingPolicyPriceRules(ctx, &policy, string(gatewaycore.ProtocolOpenAIChat), candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(costOrdered) != 3 || costOrdered[0].AccountID != "account-1" || costOrdered[1].AccountID != "account-2" || costOrdered[2].AccountID != "account-3" {
		t.Fatalf("cost preference did not order the retained pool by price: %+v", costOrdered)
	}

	policy.Strategy.Preset = RoutingPolicyPresetBalanced
	policy.Strategy.LowPricePoolMode = RoutingPolicyLowPriceStrict
	policy.Strategy.LowPricePoolMinCandidates = 20
	strict, err := svc.applyRoutingPolicyPriceRules(ctx, &policy, string(gatewaycore.ProtocolOpenAIChat), candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(strict) != 4 {
		t.Fatalf("strict low price pool floor must not be capped below the requested minimum: %+v", strict)
	}

	withoutFacts, err := svc.applyRoutingPolicyPriceRules(ctx, &policy, string(gatewaycore.ProtocolAnthropicMessages), candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutFacts) != len(candidates) || withoutFacts[0].AccountID != candidates[0].AccountID {
		t.Fatalf("candidates changed without comparable price facts: %+v", withoutFacts)
	}
	withoutComparableFacts := []GatewayProvider{
		{AccountID: "unpriced-a", UpstreamModel: "unpriced-model"},
		{AccountID: "unpriced-b", UpstreamModel: "unpriced-model"},
	}
	withoutFacts, err = svc.applyRoutingPolicyPriceRules(ctx, &policy, string(gatewaycore.ProtocolOpenAIChat), withoutComparableFacts)
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutFacts) != len(withoutComparableFacts) || withoutFacts[0].AccountID != withoutComparableFacts[0].AccountID {
		t.Fatalf("unrelated protocol prices changed candidates without comparable facts: %+v", withoutFacts)
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

func TestRoutingPolicyPresetsResolveConflictingSignalsDeterministically(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "test-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Preset provider", Type: ProviderTypeOpenAICompatible, BaseURL: "https://provider.example/v1", Status: ProviderStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	cost := createRoutingPolicyTestAccountWithLimits(t, svc, provider.ID, "Cost", 0.1, 10)
	speed := createRoutingPolicyTestAccountWithLimits(t, svc, provider.ID, "Speed", 2, 10)
	stability := createRoutingPolicyTestAccountWithLimits(t, svc, provider.ID, "Stability", 1, 10)
	balanced := createRoutingPolicyTestAccountWithLimits(t, svc, provider.ID, "Balanced", 1, 10)
	model := mustCreateGatewayModelRoutes(t, svc, "preset-model", []ProviderAccount{balanced, stability, speed, cost})

	balanced.CircuitState = CircuitStateHalfOpen
	if err := repo.SaveProviderAccount(ctx, balanced); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	svc.scheduler.rateSamples[cost.ID] = []gatewayRateSample{{at: now}, {at: now}, {at: now}}
	svc.scheduler.rateSamples[stability.ID] = []gatewayRateSample{{at: now}}
	svc.scheduler.rateSamples[balanced.ID] = []gatewayRateSample{{at: now}, {at: now}}

	policy, err := svc.CreateRoutingPolicy(ctx, "tester", RoutingPolicyRequest{
		Name: "Preset matrix", RouteGroup: model.DefaultRouteGroup, Status: RoutingPolicyStatusActive,
		Strategy: RoutingPolicyStrategy{Preset: RoutingPolicyPresetBalanced, StickyTTLSeconds: 900, LowPricePoolMode: RoutingPolicyLowPriceNone, FailoverBeforeFirstByte: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		preset string
		want   string
	}{
		{preset: RoutingPolicyPresetCost, want: cost.ID},
		{preset: RoutingPolicyPresetSpeed, want: speed.ID},
		{preset: RoutingPolicyPresetStability, want: stability.ID},
		{preset: RoutingPolicyPresetBalanced, want: balanced.ID},
	}
	for _, test := range tests {
		t.Run(test.preset, func(t *testing.T) {
			updated, err := svc.UpdateRoutingPolicy(ctx, "tester", policy.ID, RoutingPolicyRequest{
				Name: policy.Name, RouteGroup: policy.RouteGroup, Status: policy.Status,
				Strategy: RoutingPolicyStrategy{Preset: test.preset, StickyTTLSeconds: 900, LowPricePoolMode: RoutingPolicyLowPriceNone, FailoverBeforeFirstByte: true},
			})
			if err != nil {
				t.Fatal(err)
			}
			policy = updated
			candidates, hasRoutes, err := svc.GatewayProviderCandidatesForModel(ctx, model.ModelID)
			if err != nil || !hasRoutes || len(candidates) != 4 {
				t.Fatalf("candidates=%+v hasRoutes=%t err=%v", candidates, hasRoutes, err)
			}
			if candidates[0].AccountID != test.want || !strings.Contains(candidates[0].SelectionReason, "preset="+test.preset) {
				t.Fatalf("preset %s selected %s, want %s; candidates=%+v", test.preset, candidates[0].AccountID, test.want, candidates)
			}
		})
	}
}

func TestRoutingPolicySimulatorMatchesPlannerHardConstraints(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "test-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Simulation provider", Type: ProviderTypeOpenAICompatible, BaseURL: "https://provider.example/v1", Status: ProviderStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	cheap := createRoutingPolicyTestAccount(t, svc, provider.ID, "Cheap", "cheap-upstream", 1)
	backup := createRoutingPolicyTestAccount(t, svc, provider.ID, "Backup", "backup-upstream", 1)
	expensive := createRoutingPolicyTestAccount(t, svc, provider.ID, "Expensive", "expensive-upstream", 1)
	model, err := svc.CreateGatewayModel(ctx, "tester", GatewayModelRequest{ModelID: "simulation-model", Name: "Simulation", Modality: "chat", Status: GatewayModelStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	for index, route := range []struct {
		account ProviderAccount
		model   string
		format  string
	}{
		{account: backup, model: "backup-upstream", format: UpstreamFormatOpenAIChat},
		{account: expensive, model: "expensive-upstream", format: UpstreamFormatOpenAIChat},
		{account: cheap, model: "cheap-upstream", format: UpstreamFormatOpenAIChat},
	} {
		if _, err := svc.CreateModelRoute(ctx, "tester", ModelRouteRequest{
			GatewayModelID: model.ID, ProviderAccountID: route.account.ID, UpstreamModel: route.model,
			UpstreamFormat: route.format, Priority: (index + 1) * 10, Weight: 100, Status: ModelRouteStatusActive,
		}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Add(-time.Minute)
	for _, price := range []ProcurementPrice{
		{ID: "price-cheap", ProviderAccountID: cheap.ID, UpstreamModel: "cheap-upstream", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD", UncachedInputMicrosPer1MTokens: 100_000, OutputMicrosPer1MTokens: 100_000, RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now},
		{ID: "price-backup", ProviderAccountID: backup.ID, UpstreamModel: "backup-upstream", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD", UncachedInputMicrosPer1MTokens: 150_000, OutputMicrosPer1MTokens: 150_000, RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now},
		{ID: "price-expensive", ProviderAccountID: expensive.ID, UpstreamModel: "expensive-upstream", Protocol: string(gatewaycore.ProtocolOpenAIChat), Currency: "USD", UncachedInputMicrosPer1MTokens: 1_000_000, OutputMicrosPer1MTokens: 1_000_000, RechargeMultiplier: 1, Status: ProcurementPriceStatusActive, EffectiveFrom: now},
	} {
		if err := repo.SaveProcurementPrice(ctx, price); err != nil {
			t.Fatal(err)
		}
	}
	policy, err := svc.CreateRoutingPolicy(ctx, "tester", RoutingPolicyRequest{
		Name: "Simulation parity", RouteGroup: DefaultModelRouteGroup, Status: RoutingPolicyStatusActive,
		Strategy: RoutingPolicyStrategy{
			Preset: RoutingPolicyPresetCost, StickyTTLSeconds: 900, FailoverBeforeFirstByte: false,
			MaxPriceMultipleOfCheapest: 2, LowPricePoolMode: RoutingPolicyLowPriceNone,
			AllowedProtocols: []string{string(gatewaycore.ProtocolOpenAIChat)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := gatewaycore.CanonicalAuthContext{CredentialID: "simulation-key"}
	request := gatewaycore.CanonicalRequest{Protocol: gatewaycore.ProtocolOpenAIChat, Operation: GatewayOperationChatCompletion, Modality: GatewayModalityText, Lane: gatewaycore.LaneDirect, Model: model.ModelID}
	plan, err := svc.PlanCanonicalGatewayRequest(ctx, auth, request)
	if err != nil || len(plan.Candidates) != 1 || plan.Candidates[0].AccountID != cheap.ID {
		t.Fatalf("planner candidates=%+v exclusions=%+v err=%v", plan.Candidates, plan.Exclusions, err)
	}
	simulation, err := svc.SimulateGatewayRouting(ctx, GatewaySimulationRequest{Model: model.ModelID, Protocol: string(gatewaycore.ProtocolOpenAIChat), EstimatedTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	assertSimulationCandidateReason(t, simulation, cheap.ID, "")
	assertSimulationCandidateReason(t, simulation, backup.ID, "routing_policy_failover_disabled")
	assertSimulationCandidateReason(t, simulation, expensive.ID, "routing_policy_relative_price_exceeded")
	if simulation.RoutingPolicyID != policy.ID || simulation.RoutingPolicyVersion != policy.Version || simulation.RoutingPolicyPreset != RoutingPolicyPresetCost {
		t.Fatalf("simulation policy evidence mismatch: %+v", simulation)
	}

	_, err = svc.UpdateRoutingPolicy(ctx, "tester", policy.ID, RoutingPolicyRequest{
		Name: policy.Name, RouteGroup: policy.RouteGroup, Status: policy.Status,
		Strategy: RoutingPolicyStrategy{
			Preset: RoutingPolicyPresetBalanced, StickyTTLSeconds: 900, FailoverBeforeFirstByte: true, LowPricePoolMode: RoutingPolicyLowPriceNone,
			AllowedProtocols: []string{string(gatewaycore.ProtocolOpenAIChat)}, DeniedProtocols: []string{string(gatewaycore.ProtocolOpenAIChat)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	blockedPlan, err := svc.PlanCanonicalGatewayRequest(ctx, auth, request)
	if err != nil || blockedPlan.RejectionReason != "routing_policy_protocol_blocked" || len(blockedPlan.Candidates) != 0 {
		t.Fatalf("blocked planner=%+v err=%v", blockedPlan, err)
	}
	blockedSimulation, err := svc.SimulateGatewayRouting(ctx, GatewaySimulationRequest{Model: model.ModelID, Protocol: string(gatewaycore.ProtocolOpenAIChat)})
	if err != nil || blockedSimulation.Status != "blocked" || blockedSimulation.RejectionReason != "routing_policy_protocol_blocked" {
		t.Fatalf("blocked simulation=%+v err=%v", blockedSimulation, err)
	}
	for _, candidate := range blockedSimulation.Candidates {
		if candidate.Eligible || candidate.Reason != "routing_policy_protocol_blocked" {
			t.Fatalf("protocol-blocked simulation candidate=%+v", candidate)
		}
	}

	_, err = svc.UpdateRoutingPolicy(ctx, "tester", policy.ID, RoutingPolicyRequest{
		Name: policy.Name, RouteGroup: policy.RouteGroup, Status: policy.Status,
		Strategy: RoutingPolicyStrategy{
			Preset: RoutingPolicyPresetBalanced, StickyTTLSeconds: 900, FailoverBeforeFirstByte: true, LowPricePoolMode: RoutingPolicyLowPriceNone,
			AllowedModels: []string{"other-model"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	modelBlockedPlan, err := svc.PlanCanonicalGatewayRequest(ctx, auth, request)
	if err != nil || modelBlockedPlan.RejectionReason != "routing_policy_model_blocked" || len(modelBlockedPlan.Candidates) != 0 {
		t.Fatalf("model-blocked planner=%+v err=%v", modelBlockedPlan, err)
	}
	modelBlockedSimulation, err := svc.SimulateGatewayRouting(ctx, GatewaySimulationRequest{Model: model.ModelID, Protocol: string(gatewaycore.ProtocolOpenAIChat)})
	if err != nil || modelBlockedSimulation.Status != "blocked" || modelBlockedSimulation.RejectionReason != "routing_policy_model_blocked" {
		t.Fatalf("model-blocked simulation=%+v err=%v", modelBlockedSimulation, err)
	}
	for _, candidate := range modelBlockedSimulation.Candidates {
		if candidate.Eligible || candidate.Reason != "routing_policy_model_blocked" {
			t.Fatalf("model-blocked simulation candidate=%+v", candidate)
		}
	}
}

func TestRoutingPolicyUsesObservedMetricsAndExplainsSimulation(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "observed-routing-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Observed provider", Type: ProviderTypeOpenAICompatible, BaseURL: "https://provider.example/v1", Status: ProviderStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	reliable := createRoutingPolicyTestAccount(t, svc, provider.ID, "Reliable", "observed-model", 1)
	fast := createRoutingPolicyTestAccount(t, svc, provider.ID, "Fast", "observed-model", 1)
	model := mustCreateGatewayModelRoutes(t, svc, "observed-model", []ProviderAccount{reliable, fast})
	now := time.Now().UTC().Add(-time.Hour)
	for index := 0; index < 10; index++ {
		if err := repo.SaveGatewayTrace(ctx, GatewayTrace{
			ID: "reliable-" + fmt.Sprint(index), ProviderAccountID: reliable.ID,
			HTTPStatus: 200, LatencyMS: 500, CreatedAt: now.Add(time.Duration(index) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
		status := 500
		errorType := "upstream"
		if index < 5 {
			status = 200
			errorType = ""
		}
		if err := repo.SaveGatewayTrace(ctx, GatewayTrace{
			ID: "fast-" + fmt.Sprint(index), ProviderAccountID: fast.ID,
			HTTPStatus: status, ErrorType: errorType, LatencyMS: 50, CreatedAt: now.Add(time.Duration(index) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}
	policy, err := svc.CreateRoutingPolicy(ctx, "tester", RoutingPolicyRequest{
		Name: "Observed metrics", RouteGroup: model.DefaultRouteGroup, Status: RoutingPolicyStatusActive,
		Strategy: RoutingPolicyStrategy{Preset: RoutingPolicyPresetSpeed, StickyTTLSeconds: 900, FailoverBeforeFirstByte: true, LowPricePoolMode: RoutingPolicyLowPriceNone,
			ResourceBatches: []RoutingPolicyBatch{{Name: "Production", ProviderAccountIDs: []string{reliable.ID, fast.ID}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidates, _, err := svc.GatewayProviderCandidatesForModel(ctx, model.ModelID)
	if err != nil || len(candidates) != 2 || candidates[0].AccountID != fast.ID {
		t.Fatalf("speed candidates=%+v err=%v", candidates, err)
	}
	if candidates[0].ObservedAvgLatencyMS != 50 || candidates[0].ObservedSampleCount != 10 {
		t.Fatalf("speed evidence=%+v", candidates[0])
	}
	policy, err = svc.UpdateRoutingPolicy(ctx, "tester", policy.ID, RoutingPolicyRequest{
		Name: policy.Name, RouteGroup: policy.RouteGroup, Status: policy.Status,
		Strategy: RoutingPolicyStrategy{Preset: RoutingPolicyPresetStability, StickyTTLSeconds: 900, FailoverBeforeFirstByte: true, LowPricePoolMode: RoutingPolicyLowPriceNone,
			ResourceBatches: policy.Strategy.ResourceBatches},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidates, _, err = svc.GatewayProviderCandidatesForModel(ctx, model.ModelID)
	if err != nil || len(candidates) != 2 || candidates[0].AccountID != reliable.ID || candidates[0].ObservedSuccessRate != 1 {
		t.Fatalf("stability candidates=%+v err=%v", candidates, err)
	}
	simulation, err := svc.SimulateGatewayRouting(ctx, GatewaySimulationRequest{Model: model.ModelID, Protocol: string(gatewaycore.ProtocolOpenAIChat)})
	if err != nil || len(simulation.Candidates) != 2 {
		t.Fatalf("simulation=%+v err=%v", simulation, err)
	}
	if simulation.Candidates[0].PolicyBatchName != "Production" || simulation.Candidates[0].PolicyBatchPosition != 0 || simulation.Candidates[0].ObservedSampleCount != 10 || simulation.Candidates[0].SelectionReason == "" {
		t.Fatalf("simulation evidence=%+v", simulation.Candidates[0])
	}
}

func TestRoutingPolicyVersionInvalidatesAffinityScope(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "affinity-policy-version-secret")
	input := GatewayAffinityInput{
		ApplicationID: "application", PrincipalID: "principal", CredentialID: "credential",
		Model: "public-model", Protocol: string(gatewaycore.ProtocolOpenAIChat), RouteGroup: "default",
		StickyKey: "session", AccessPolicyVersion: 7, RoutingPolicyID: "routing-policy", RoutingPolicyVersion: 1,
	}
	candidates := []GatewayProvider{
		{ID: "provider-a", AccountID: "account-a", RouteID: "route-a", RoutingPolicyID: input.RoutingPolicyID, StickyEnabled: true},
		{ID: "provider-b", AccountID: "account-b", RouteID: "route-b", RoutingPolicyID: input.RoutingPolicyID, StickyEnabled: true},
	}
	if err := svc.BindGatewayCandidateAffinity(context.Background(), input, candidates[1]); err != nil {
		t.Fatal(err)
	}
	if got := svc.PreferGatewayCandidatesWithAffinity(context.Background(), input, candidates); got[0].AccountID != "account-b" {
		t.Fatalf("version one did not reuse affinity: %+v", got)
	}
	input.RoutingPolicyVersion = 2
	if got := svc.PreferGatewayCandidatesWithAffinity(context.Background(), input, candidates); got[0].AccountID != "account-a" {
		t.Fatalf("version two reused a stale routing-policy binding: %+v", got)
	}
}

func TestAPIKeyRoutingPolicyBindingUsesExactlyOneCompatiblePolicy(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "routing-policy-key-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Binding provider", Type: ProviderTypeOpenAICompatible, BaseURL: "https://provider.example/v1", Status: ProviderStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	defaultAccount := createRoutingPolicyTestAccount(t, svc, provider.ID, "Default account", "binding-model", 1)
	explicitAccount := createRoutingPolicyTestAccount(t, svc, provider.ID, "Explicit account", "binding-model", 1)
	model := mustCreateGatewayModelRoutes(t, svc, "binding-model", []ProviderAccount{defaultAccount, explicitAccount})
	defaultPolicy, err := svc.CreateRoutingPolicy(ctx, "tester", RoutingPolicyRequest{
		Name: "Default policy", RouteGroup: model.DefaultRouteGroup, Status: RoutingPolicyStatusActive,
		Strategy: RoutingPolicyStrategy{Preset: RoutingPolicyPresetStability, StickyTTLSeconds: 900, FailoverBeforeFirstByte: true, LowPricePoolMode: RoutingPolicyLowPriceNone,
			ResourceBatches: []RoutingPolicyBatch{{Name: "Default only", ProviderAccountIDs: []string{defaultAccount.ID}}}},
	})
	if err != nil || !defaultPolicy.IsDefault {
		t.Fatalf("default policy=%+v err=%v", defaultPolicy, err)
	}
	explicitPolicy, err := svc.CreateRoutingPolicy(ctx, "tester", RoutingPolicyRequest{
		Name: "Explicit policy", RouteGroup: model.DefaultRouteGroup, Status: RoutingPolicyStatusActive,
		Strategy: RoutingPolicyStrategy{Preset: RoutingPolicyPresetCost, StickyTTLSeconds: 900, FailoverBeforeFirstByte: true, LowPricePoolMode: RoutingPolicyLowPriceNone,
			ResourceBatches: []RoutingPolicyBatch{{Name: "Explicit only", ProviderAccountIDs: []string{explicitAccount.ID}}}},
	})
	if err != nil || explicitPolicy.IsDefault {
		t.Fatalf("explicit policy=%+v err=%v", explicitPolicy, err)
	}

	created, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name: "Explicit routing key", ModelAllowlist: []string{model.ModelID}, RoutingPolicyID: explicitPolicy.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := gatewaycore.CanonicalRequest{
		Protocol: gatewaycore.ProtocolOpenAIChat, Operation: GatewayOperationChatCompletion,
		Modality: GatewayModalityText, Lane: gatewaycore.LaneDirect, Model: model.ModelID,
	}
	_, canonical, err := svc.AuthorizeCanonicalGatewayRequest(ctx, gatewaycore.CredentialEnvelope{BearerToken: created.Key}, request)
	if err != nil || canonical.RoutingPolicyID != explicitPolicy.ID {
		t.Fatalf("canonical=%+v err=%v", canonical, err)
	}
	plan, err := svc.PlanCanonicalGatewayRequest(ctx, canonical, request)
	if err != nil || plan.RoutingPolicyID != explicitPolicy.ID || len(plan.Candidates) != 1 || plan.Candidates[0].AccountID != explicitAccount.ID {
		t.Fatalf("explicit plan=%+v err=%v", plan, err)
	}
	defaultPlan, err := svc.PlanCanonicalGatewayRequest(ctx, gatewaycore.CanonicalAuthContext{CredentialID: "unbound"}, request)
	if err != nil || defaultPlan.RoutingPolicyID != defaultPolicy.ID || len(defaultPlan.Candidates) != 1 || defaultPlan.Candidates[0].AccountID != defaultAccount.ID {
		t.Fatalf("default plan=%+v err=%v", defaultPlan, err)
	}
	rotated, err := svc.RotateAPIKey(ctx, "tester", created.Record.ID)
	if err != nil || rotated.Record.RoutingPolicyID != explicitPolicy.ID {
		t.Fatalf("rotated=%+v err=%v", rotated.Record, err)
	}

	otherModel, err := svc.CreateGatewayModel(ctx, "tester", GatewayModelRequest{
		ModelID: "other-binding-model", Name: "Other binding model", DefaultRouteGroup: "other", Status: GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name: "Mismatch", ModelAllowlist: []string{otherModel.ModelID}, RoutingPolicyID: explicitPolicy.ID,
	}); err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("cross-group binding error=%v", err)
	}
	disabledPolicy, err := svc.CreateRoutingPolicy(ctx, "tester", RoutingPolicyRequest{
		Name: "Disabled policy", RouteGroup: model.DefaultRouteGroup, Status: RoutingPolicyStatusDisabled,
		Strategy: RoutingPolicyStrategy{Preset: RoutingPolicyPresetBalanced, StickyTTLSeconds: 900},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name: "Disabled", ModelAllowlist: []string{model.ModelID}, RoutingPolicyID: disabledPolicy.ID,
	}); err == nil || !strings.Contains(err.Error(), "active routing policy") {
		t.Fatalf("disabled binding error=%v", err)
	}
}

func TestUnboundGatewayRequestDoesNotUseNonDefaultRoutingPolicy(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "routing-policy-default-secret")
	nonDefault := false
	policy, err := svc.CreateRoutingPolicy(ctx, "tester", RoutingPolicyRequest{
		Name: "Explicit-only", RouteGroup: "enterprise", Status: RoutingPolicyStatusActive, IsDefault: &nonDefault,
		Strategy: RoutingPolicyStrategy{Preset: RoutingPolicyPresetCost, StickyTTLSeconds: 900, LowPricePoolMode: RoutingPolicyLowPriceNone},
	})
	if err != nil {
		t.Fatal(err)
	}
	if policy.IsDefault {
		t.Fatalf("explicit non-default policy became default: %+v", policy)
	}
	selected, err := svc.activeRoutingPolicyForGroup(ctx, "enterprise")
	if err != nil {
		t.Fatal(err)
	}
	if selected != nil {
		t.Fatalf("unbound requests must not inherit non-default policy: %+v", selected)
	}
}

func createRoutingPolicyTestAccountWithLimits(t *testing.T, svc *Service, providerID, name string, rate float64, rpm int) ProviderAccount {
	t.Helper()
	account, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID: providerID, Name: name, Platform: ProviderTypeOpenAICompatible, AuthType: ProviderAuthAPIKey,
		Status: AccountStatusActive, Priority: 50, Concurrency: 10, RPMLimit: rpm, RateMultiplier: rate,
		Models: []string{"preset-model"}, Secret: name + "-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func assertSimulationCandidateReason(t *testing.T, simulation GatewaySimulation, accountID, reason string) {
	t.Helper()
	for _, candidate := range simulation.Candidates {
		if candidate.ProviderAccountID != accountID {
			continue
		}
		if candidate.Eligible != (reason == "") || candidate.Reason != reason {
			t.Fatalf("simulation candidate %s=%+v, want reason %q", accountID, candidate, reason)
		}
		return
	}
	t.Fatalf("simulation candidate %s missing: %+v", accountID, simulation.Candidates)
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
