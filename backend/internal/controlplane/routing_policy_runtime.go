package controlplane

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

type routingPolicyCandidatePrice struct {
	candidate GatewayProvider
	price     int64
	priced    bool
	order     int
}

type routingPolicyPriceExclusion struct {
	candidate GatewayProvider
	reason    string
}

type routingPolicyPriceDecision struct {
	candidates []GatewayProvider
	exclusions []routingPolicyPriceExclusion
}

func (s *Service) applyRoutingPolicyPriceRules(ctx context.Context, policy *RoutingPolicy, protocol string, candidates []GatewayProvider) ([]GatewayProvider, error) {
	decision, err := s.evaluateRoutingPolicyPriceRules(ctx, policy, protocol, candidates)
	return decision.candidates, err
}

func (s *Service) evaluateRoutingPolicyPriceRules(ctx context.Context, policy *RoutingPolicy, protocol string, candidates []GatewayProvider) (routingPolicyPriceDecision, error) {
	decision := routingPolicyPriceDecision{candidates: candidates, exclusions: []routingPolicyPriceExclusion{}}
	if policy == nil || len(candidates) == 0 {
		return decision, nil
	}
	strategy := policy.Strategy
	inputLimit, outputLimit := routingPolicyAbsolutePriceLimits(strategy, routingPolicyRequestedModel(candidates))
	priceRulesConfigured := inputLimit > 0 || outputLimit > 0 || strategy.MaxPriceMultipleOfCheapest > 0 || strategy.MissingPriceAction == RoutingPolicyMissingPriceBlock || oneOf(strategy.LowPricePoolMode, RoutingPolicyLowPriceAuto, RoutingPolicyLowPriceStrict, RoutingPolicyLowPricePercent)
	if !priceRulesConfigured && strategy.Preset != RoutingPolicyPresetCost {
		return decision, nil
	}
	prices, err := s.repo.ListProcurementPrices(ctx)
	if err != nil {
		return routingPolicyPriceDecision{}, err
	}
	now := s.nowUTC()
	activePrices := activeRoutingPrices(prices, protocol, now)
	if len(activePrices) == 0 {
		if strategy.MissingPriceAction == RoutingPolicyMissingPriceBlock && priceRulesConfigured {
			for _, candidate := range candidates {
				decision.exclusions = append(decision.exclusions, routingPolicyPriceExclusion{candidate: candidate, reason: "routing_policy_price_fact_missing"})
			}
			decision.candidates = nil
		}
		return decision, nil
	}
	priced := make([]routingPolicyCandidatePrice, 0, len(candidates))
	for index, candidate := range candidates {
		price, found := activePrices[routingPriceKey(candidate.AccountID, candidate.UpstreamModel)]
		if !found {
			if strategy.MissingPriceAction != RoutingPolicyMissingPriceBlock {
				priced = append(priced, routingPolicyCandidatePrice{candidate: candidate, order: index})
			} else {
				decision.exclusions = append(decision.exclusions, routingPolicyPriceExclusion{candidate: candidate, reason: "routing_policy_price_fact_missing"})
			}
			continue
		}
		candidate.PriceFactPresent = true
		candidate.EstimatedInputMicrosPer1M = price.UncachedInputMicrosPer1MTokens
		candidate.EstimatedOutputMicrosPer1M = price.OutputMicrosPer1MTokens
		if inputLimit > 0 && routingPolicyInputPriceExceeded(price, dollarsToMicros(inputLimit)) {
			decision.exclusions = append(decision.exclusions, routingPolicyPriceExclusion{candidate: candidate, reason: "routing_policy_input_price_exceeded"})
			continue
		}
		if outputLimit > 0 && price.OutputMicrosPer1MTokens > dollarsToMicros(outputLimit) {
			decision.exclusions = append(decision.exclusions, routingPolicyPriceExclusion{candidate: candidate, reason: "routing_policy_output_price_exceeded"})
			continue
		}
		candidate.SelectionReason = appendSelectionReason(candidate.SelectionReason, "price facts applied")
		priced = append(priced, routingPolicyCandidatePrice{
			candidate: candidate,
			price:     adjustedRoutingPrice(price),
			priced:    true,
			order:     index,
		})
	}
	if len(priced) == 0 {
		decision.candidates = nil
		return decision, nil
	}
	cheapestByBatch := make(map[int]int64)
	for _, candidate := range priced {
		batch := candidate.candidate.PolicyBatchOrder
		current, found := cheapestByBatch[batch]
		if candidate.priced && (!found || candidate.price < current) {
			cheapestByBatch[batch] = candidate.price
		}
	}
	if strategy.MaxPriceMultipleOfCheapest > 0 {
		filtered := priced[:0]
		for _, candidate := range priced {
			if !candidate.priced {
				filtered = append(filtered, candidate)
				continue
			}
			cheapest, found := cheapestByBatch[candidate.candidate.PolicyBatchOrder]
			limit := float64(cheapest) * strategy.MaxPriceMultipleOfCheapest
			if candidate.priced && found && float64(candidate.price) <= limit {
				filtered = append(filtered, candidate)
			} else {
				decision.exclusions = append(decision.exclusions, routingPolicyPriceExclusion{candidate: candidate.candidate, reason: "routing_policy_relative_price_exceeded"})
			}
		}
		priced = filtered
	}
	if len(priced) == 0 {
		decision.candidates = nil
		return decision, nil
	}
	sort.SliceStable(priced, func(i, j int) bool {
		if priced[i].candidate.PolicyBatchOrder != priced[j].candidate.PolicyBatchOrder {
			return priced[i].candidate.PolicyBatchOrder < priced[j].candidate.PolicyBatchOrder
		}
		if priced[i].priced != priced[j].priced {
			return priced[i].priced
		}
		if priced[i].priced && priced[i].price != priced[j].price {
			return priced[i].price < priced[j].price
		}
		return false
	})
	if oneOf(strategy.LowPricePoolMode, RoutingPolicyLowPriceAuto, RoutingPolicyLowPriceStrict, RoutingPolicyLowPricePercent) {
		beforePool := append([]routingPolicyCandidatePrice(nil), priced...)
		priced = lowPriceCandidatePool(priced, strategy)
		retained := make(map[string]struct{}, len(priced))
		for _, candidate := range priced {
			retained[routingPolicyPriceCandidateKey(candidate.candidate)] = struct{}{}
		}
		for _, candidate := range beforePool {
			if _, found := retained[routingPolicyPriceCandidateKey(candidate.candidate)]; !found {
				decision.exclusions = append(decision.exclusions, routingPolicyPriceExclusion{candidate: candidate.candidate, reason: "routing_policy_low_price_pool_excluded"})
			}
		}
	}
	if strategy.Preset != RoutingPolicyPresetCost || strategy.StrictOrder {
		sort.SliceStable(priced, func(i, j int) bool {
			return priced[i].order < priced[j].order
		})
	} else {
		sort.SliceStable(priced, func(i, j int) bool {
			left, right := priced[i], priced[j]
			if left.candidate.PolicyBatchOrder != right.candidate.PolicyBatchOrder {
				return left.candidate.PolicyBatchOrder < right.candidate.PolicyBatchOrder
			}
			leftPreferred := contains(strategy.PreferredProviderAccountIDs, left.candidate.AccountID)
			rightPreferred := contains(strategy.PreferredProviderAccountIDs, right.candidate.AccountID)
			if leftPreferred != rightPreferred {
				return leftPreferred
			}
			if left.priced != right.priced {
				return left.priced
			}
			if left.priced && left.price != right.price {
				return left.price < right.price
			}
			return left.order < right.order
		})
	}
	decision.candidates = make([]GatewayProvider, 0, len(priced))
	for _, candidate := range priced {
		decision.candidates = append(decision.candidates, candidate.candidate)
	}
	return decision, nil
}

func routingPolicyRequestedModel(candidates []GatewayProvider) string {
	for _, candidate := range candidates {
		if model := strings.TrimSpace(candidate.RequestedModel); model != "" {
			return model
		}
	}
	return ""
}

func routingPolicyAbsolutePriceLimits(strategy RoutingPolicyStrategy, model string) (float64, float64) {
	input := strategy.AbsoluteMaxInputPer1M
	output := strategy.AbsoluteMaxOutputPer1M
	baseModel := strings.TrimSpace(model)
	if separator := strings.LastIndex(baseModel, ":"); separator > 0 {
		baseModel = baseModel[:separator]
	}
	for _, limit := range strategy.ModelPriceLimits {
		if limit.Model != strings.TrimSpace(model) && limit.Model != baseModel {
			continue
		}
		input = lowerPositiveFloat(input, limit.AbsoluteMaxInputPer1M)
		output = lowerPositiveFloat(output, limit.AbsoluteMaxOutputPer1M)
	}
	return input, output
}

func lowerPositiveFloat(left, right float64) float64 {
	if left <= 0 {
		return right
	}
	if right <= 0 || left <= right {
		return left
	}
	return right
}

func routingPolicyPriceCandidateKey(candidate GatewayProvider) string {
	if candidate.RouteID != "" {
		return candidate.RouteID
	}
	return routingPriceKey(candidate.AccountID, candidate.UpstreamModel)
}

func activeRoutingPrices(prices []ProcurementPrice, protocol string, now time.Time) map[string]ProcurementPrice {
	out := map[string]ProcurementPrice{}
	for _, price := range prices {
		if price.Status != ProcurementPriceStatusActive || price.Protocol != protocol || strings.ToUpper(price.Currency) != "USD" || price.EffectiveFrom.After(now) || (price.ExpiresAt != nil && !price.ExpiresAt.After(now)) {
			continue
		}
		key := routingPriceKey(price.ProviderAccountID, price.UpstreamModel)
		if existing, found := out[key]; !found || price.EffectiveFrom.After(existing.EffectiveFrom) {
			out[key] = price
		}
	}
	return out
}

func routingPriceKey(accountID, model string) string {
	return strings.TrimSpace(accountID) + "\x00" + strings.TrimSpace(model)
}

func adjustedRoutingPrice(price ProcurementPrice) int64 {
	multiplier := price.RechargeMultiplier
	if multiplier <= 0 {
		multiplier = 1
	}
	combined := price.UncachedInputMicrosPer1MTokens + price.OutputMicrosPer1MTokens
	return int64(math.Round(float64(combined) * multiplier))
}

// routingPolicyInputPriceExceeded treats an unquoted cache price as the
// uncached input price. A policy cap must therefore protect every input path,
// while an explicitly quoted cache price can be cheaper or more expensive.
func routingPolicyInputPriceExceeded(price ProcurementPrice, limit int64) bool {
	uncached := price.UncachedInputMicrosPer1MTokens
	for _, quoted := range []int64{
		price.CacheReadMicrosPer1MTokens,
		price.CacheWrite5mMicrosPer1MTokens,
		price.CacheWrite1hMicrosPer1MTokens,
	} {
		if quoted <= 0 {
			quoted = uncached
		}
		if quoted > uncached {
			uncached = quoted
		}
	}
	return uncached > limit
}

func dollarsToMicros(value float64) int64 {
	return int64(math.Round(value * 1_000_000))
}

func lowPriceCandidatePool(candidates []routingPolicyCandidatePrice, strategy RoutingPolicyStrategy) []routingPolicyCandidatePrice {
	byBatch := map[int][]routingPolicyCandidatePrice{}
	batchOrder := make([]int, 0)
	for _, candidate := range candidates {
		batch := candidate.candidate.PolicyBatchOrder
		if _, found := byBatch[batch]; !found {
			batchOrder = append(batchOrder, batch)
		}
		byBatch[batch] = append(byBatch[batch], candidate)
	}
	out := make([]routingPolicyCandidatePrice, 0, len(candidates))
	for _, batch := range batchOrder {
		items := byBatch[batch]
		knownPriceItems := make([]routingPolicyCandidatePrice, 0, len(items))
		unknownPriceItems := make([]routingPolicyCandidatePrice, 0, len(items))
		for _, item := range items {
			if item.priced {
				knownPriceItems = append(knownPriceItems, item)
			} else {
				unknownPriceItems = append(unknownPriceItems, item)
			}
		}
		if len(knownPriceItems) == 0 {
			out = append(out, items...)
			continue
		}
		percent := strategy.LowPricePoolPercent
		if strategy.LowPricePoolMode == RoutingPolicyLowPriceAuto && percent == 0 {
			percent = 70
		}
		if strategy.LowPricePoolMode == RoutingPolicyLowPriceStrict && percent == 0 {
			percent = 30
		}
		keep := int(math.Ceil(float64(len(knownPriceItems)) * float64(percent) / 100))
		if strategy.LowPricePoolMinCandidates > keep {
			keep = strategy.LowPricePoolMinCandidates
		}
		if keep > len(knownPriceItems) {
			keep = len(knownPriceItems)
		}
		out = append(out, knownPriceItems[:keep]...)
		out = append(out, unknownPriceItems...)
	}
	return out
}

func routingPolicyProtocolSupported(protocol string) bool {
	switch gatewaycore.Protocol(strings.TrimSpace(protocol)) {
	case gatewaycore.ProtocolOpenAIChat,
		gatewaycore.ProtocolOpenAIResponses,
		gatewaycore.ProtocolOpenAIEmbeddings,
		gatewaycore.ProtocolAnthropicMessages,
		gatewaycore.ProtocolAnthropicCountTokens,
		gatewaycore.ProtocolGeminiGenerate,
		gatewaycore.ProtocolOpenAIImages,
		gatewaycore.ProtocolOpenAIMedia,
		gatewaycore.ProtocolOpenAIAudioTranscriptions,
		gatewaycore.ProtocolOpenAIAudioTranslations,
		gatewaycore.ProtocolOpenAIAudioSpeech,
		gatewaycore.ProtocolRealtime,
		gatewaycore.ProtocolAsterJobs:
		return true
	default:
		return false
	}
}
