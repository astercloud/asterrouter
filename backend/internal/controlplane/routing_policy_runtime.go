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

func (s *Service) applyRoutingPolicyPriceRules(ctx context.Context, policy *RoutingPolicy, protocol string, candidates []GatewayProvider) ([]GatewayProvider, error) {
	if policy == nil || len(candidates) == 0 {
		return candidates, nil
	}
	strategy := policy.Strategy
	priceRulesConfigured := strategy.AbsoluteMaxInputPer1M > 0 || strategy.AbsoluteMaxOutputPer1M > 0 || strategy.MaxPriceMultipleOfCheapest > 0 || oneOf(strategy.LowPricePoolMode, RoutingPolicyLowPriceAuto, RoutingPolicyLowPriceStrict, RoutingPolicyLowPricePercent)
	if !priceRulesConfigured && strategy.Preset != RoutingPolicyPresetCost {
		return candidates, nil
	}
	prices, err := s.repo.ListProcurementPrices(ctx)
	if err != nil {
		return nil, err
	}
	now := s.nowUTC()
	activePrices := activeRoutingPrices(prices, protocol, now)
	if len(activePrices) == 0 {
		return candidates, nil
	}
	priced := make([]routingPolicyCandidatePrice, 0, len(candidates))
	for index, candidate := range candidates {
		price, found := activePrices[routingPriceKey(candidate.AccountID, candidate.UpstreamModel)]
		if !found {
			if !priceRulesConfigured {
				priced = append(priced, routingPolicyCandidatePrice{candidate: candidate, order: index})
			}
			continue
		}
		if strategy.AbsoluteMaxInputPer1M > 0 && price.UncachedInputMicrosPer1MTokens > dollarsToMicros(strategy.AbsoluteMaxInputPer1M) {
			continue
		}
		if strategy.AbsoluteMaxOutputPer1M > 0 && price.OutputMicrosPer1MTokens > dollarsToMicros(strategy.AbsoluteMaxOutputPer1M) {
			continue
		}
		priced = append(priced, routingPolicyCandidatePrice{
			candidate: candidate,
			price:     adjustedRoutingPrice(price),
			priced:    true,
			order:     index,
		})
	}
	if len(priced) == 0 {
		return nil, nil
	}
	cheapest := int64(0)
	for _, candidate := range priced {
		if candidate.priced && (cheapest == 0 || candidate.price < cheapest) {
			cheapest = candidate.price
		}
	}
	if strategy.MaxPriceMultipleOfCheapest > 0 && cheapest > 0 {
		filtered := priced[:0]
		limit := float64(cheapest) * strategy.MaxPriceMultipleOfCheapest
		for _, candidate := range priced {
			if candidate.priced && float64(candidate.price) <= limit {
				filtered = append(filtered, candidate)
			}
		}
		priced = filtered
	}
	if len(priced) == 0 {
		return nil, nil
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
		priced = lowPriceCandidatePool(priced, strategy)
	}
	if strategy.Preset != RoutingPolicyPresetCost {
		sort.SliceStable(priced, func(i, j int) bool {
			return priced[i].order < priced[j].order
		})
	}
	out := make([]GatewayProvider, 0, len(priced))
	for _, candidate := range priced {
		out = append(out, candidate.candidate)
	}
	return out, nil
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
		keep := 1
		if oneOf(strategy.LowPricePoolMode, RoutingPolicyLowPriceAuto, RoutingPolicyLowPricePercent) {
			keep = int(math.Ceil(float64(len(items)) * float64(strategy.LowPricePoolPercent) / 100))
			if strategy.LowPricePoolMinCandidates > keep {
				keep = strategy.LowPricePoolMinCandidates
			}
		}
		if keep > len(items) {
			keep = len(items)
		}
		out = append(out, items[:keep]...)
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
