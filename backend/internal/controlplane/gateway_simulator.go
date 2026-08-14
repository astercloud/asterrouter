package controlplane

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

type GatewaySimulationRequest struct {
	Model            string   `json:"model"`
	EstimatedTokens  int      `json:"estimated_tokens"`
	Protocol         string   `json:"protocol"`
	RequiredFeatures []string `json:"required_features"`
	RoutingPolicyID  string   `json:"routing_policy_id"`
}

type GatewaySimulationCandidate struct {
	Rank                       int     `json:"rank"`
	RouteID                    string  `json:"route_id"`
	RouteGroup                 string  `json:"route_group"`
	ProviderID                 string  `json:"provider_id"`
	ProviderAccountID          string  `json:"provider_account_id"`
	UpstreamModel              string  `json:"upstream_model"`
	ProviderType               string  `json:"provider_type"`
	UpstreamFormat             string  `json:"upstream_format"`
	Adapter                    string  `json:"adapter"`
	Headroom                   float64 `json:"headroom"`
	RPMLimit                   int     `json:"rpm_limit"`
	TPMLimit                   int     `json:"tpm_limit"`
	Concurrency                int     `json:"concurrency"`
	CircuitState               string  `json:"circuit_state"`
	Eligible                   bool    `json:"eligible"`
	Reason                     string  `json:"reason"`
	PolicyBatchOrder           int     `json:"policy_batch_order"`
	PolicyBatchName            string  `json:"policy_batch_name"`
	PolicyBatchPosition        int     `json:"policy_batch_position"`
	PriceFactPresent           bool    `json:"price_fact_present"`
	EstimatedInputMicrosPer1M  int64   `json:"estimated_input_micros_per_1m"`
	EstimatedOutputMicrosPer1M int64   `json:"estimated_output_micros_per_1m"`
	ObservedSuccessRate        float64 `json:"observed_success_rate"`
	ObservedAvgLatencyMS       int64   `json:"observed_avg_latency_ms"`
	ObservedSampleCount        int     `json:"observed_sample_count"`
	SelectionReason            string  `json:"selection_reason"`
}

type GatewaySimulation struct {
	RequestedModel       string                       `json:"requested_model"`
	ResolvedModel        string                       `json:"resolved_model"`
	RouteGroup           string                       `json:"route_group"`
	Status               string                       `json:"status"`
	Summary              string                       `json:"summary"`
	RejectionReason      string                       `json:"rejection_reason,omitempty"`
	RoutingPolicyID      string                       `json:"routing_policy_id,omitempty"`
	RoutingPolicyVersion int                          `json:"routing_policy_version,omitempty"`
	RoutingPolicyPreset  string                       `json:"routing_policy_preset,omitempty"`
	Candidates           []GatewaySimulationCandidate `json:"candidates"`
}

func (s *Service) SimulateGatewayRouting(ctx context.Context, req GatewaySimulationRequest) (GatewaySimulation, error) {
	resolved, found, err := s.ResolveGatewayModel(ctx, req.Model)
	if err != nil {
		return GatewaySimulation{}, err
	}
	result := GatewaySimulation{RequestedModel: req.Model, Status: "unresolved", Candidates: []GatewaySimulationCandidate{}}
	if !found {
		result.Summary = "gateway model is not active or does not exist"
		return result, nil
	}
	result.ResolvedModel = resolved.GatewayModel.ModelID
	result.RouteGroup = resolved.RouteGroup
	policy, err := s.routingPolicyForCanonicalAuth(ctx, gatewaycore.CanonicalAuthContext{RoutingPolicyID: strings.TrimSpace(req.RoutingPolicyID)}, resolved.RouteGroup)
	if err != nil {
		return GatewaySimulation{}, err
	}
	if policy != nil {
		result.RoutingPolicyID = policy.ID
		result.RoutingPolicyVersion = policy.Version
		result.RoutingPolicyPreset = policy.Strategy.Preset
	}
	if policy != nil && !routingPolicyAllowsModel(policy.Strategy, resolved.RequestedID) {
		return s.blockedGatewaySimulation(ctx, result, resolved, policy, "routing_policy_model_blocked")
	}
	if protocol := strings.TrimSpace(req.Protocol); protocol != "" {
		if !routingPolicyProtocolSupported(protocol) {
			return s.blockedGatewaySimulation(ctx, result, resolved, policy, "client_protocol_unsupported")
		}
		if policy != nil && !routingPolicyAllowsProtocol(policy.Strategy, protocol) {
			return s.blockedGatewaySimulation(ctx, result, resolved, policy, "routing_policy_protocol_blocked")
		}
	}

	baseCandidates, hasRoutes, err := s.gatewayProviderCandidatesForResolvedModel(ctx, resolved, policy)
	if err != nil {
		return GatewaySimulation{}, err
	}
	if !hasRoutes {
		result.Status = "no_routes"
		result.Summary = "no model routes exist for the resolved route group"
		return result, nil
	}
	decision, err := s.applyRoutingPolicyCandidateRules(ctx, policy, strings.TrimSpace(req.Protocol), baseCandidates)
	if err != nil {
		return GatewaySimulation{}, err
	}
	baseByRouteID := make(map[string]GatewayProvider, len(baseCandidates))
	for _, candidate := range baseCandidates {
		baseByRouteID[candidate.RouteID] = candidate
	}
	for index, candidate := range decision.Candidates {
		reason := simulationPermitReason(s, candidate, req.EstimatedTokens)
		if reason == "" {
			reason = simulationProtocolReason(req.Protocol, req.RequiredFeatures, candidate.UpstreamFormat)
		}
		result.Candidates = append(result.Candidates, gatewaySimulationCandidate(candidate, index+1, reason))
	}
	for _, exclusion := range decision.Exclusions {
		candidate, found := baseByRouteID[exclusion.RouteID]
		if !found {
			continue
		}
		candidate.PolicyBatchOrder = exclusion.PolicyBatchOrder
		candidate.PolicyBatchName = exclusion.PolicyBatchName
		candidate.PolicyBatchPosition = exclusion.PolicyBatchPosition
		candidate.PriceFactPresent = exclusion.PriceFactPresent
		candidate.EstimatedInputMicrosPer1M = exclusion.EstimatedInputMicrosPer1M
		candidate.EstimatedOutputMicrosPer1M = exclusion.EstimatedOutputMicrosPer1M
		candidate.SelectionReason = exclusion.SelectionReason
		result.Candidates = append(result.Candidates, gatewaySimulationCandidate(candidate, len(result.Candidates)+1, exclusion.Reason))
	}
	consideredByRouteID := make(map[string]struct{}, len(baseCandidates))
	for _, candidate := range baseCandidates {
		consideredByRouteID[candidate.RouteID] = struct{}{}
	}
	skipped, err := s.skippedSimulationCandidates(ctx, resolved, consideredByRouteID, len(result.Candidates)+1)
	if err != nil {
		return GatewaySimulation{}, err
	}
	result.Candidates = append(result.Candidates, skipped...)
	eligible := 0
	for _, candidate := range result.Candidates {
		if candidate.Eligible {
			eligible++
		}
	}
	if eligible == 0 {
		result.Status = "blocked"
		result.RejectionReason = "all_candidates_excluded"
	} else {
		result.Status = "ready"
	}
	result.Summary = fmt.Sprintf("resolved %d eligible candidates from %d routes without consuming scheduling capacity", eligible, len(result.Candidates))
	return result, nil
}

func (s *Service) blockedGatewaySimulation(ctx context.Context, result GatewaySimulation, resolved ResolvedGatewayModel, policy *RoutingPolicy, reason string) (GatewaySimulation, error) {
	result.Status = "blocked"
	result.RejectionReason = reason
	ranked, _, err := s.rankedModelRouteCandidatesWithPolicy(ctx, resolved, policy)
	if err != nil {
		return GatewaySimulation{}, err
	}
	considered := make(map[string]struct{}, len(ranked))
	for _, candidate := range ranked {
		considered[candidate.route.ID] = struct{}{}
		provider := GatewayProvider{
			ID: candidate.provider.ID, Type: candidate.provider.Type, AccountID: candidate.account.ID,
			UpstreamModel:  ProviderAccountDispatchModel(candidate.account, candidate.route.UpstreamModel, resolved.RequestedID),
			UpstreamFormat: candidate.route.UpstreamFormat, RouteID: candidate.route.ID, RouteGroup: candidate.route.RouteGroup,
			RPMLimit: candidate.account.RPMLimit, TPMLimit: candidate.account.TPMLimit, Concurrency: candidate.account.Concurrency,
			CircuitState: candidate.circuitState, Headroom: candidate.headroom,
			PolicyBatchOrder: candidate.policyBatch, PolicyBatchName: candidate.policyBatchName, PolicyBatchPosition: candidate.policyBatchPosition,
			ObservedSuccessRate: candidate.routingMetrics.SuccessRate, ObservedAvgLatencyMS: candidate.routingMetrics.AvgLatencyMS,
			ObservedSampleCount: candidate.routingMetrics.RequestCount,
		}
		result.Candidates = append(result.Candidates, gatewaySimulationCandidate(provider, len(result.Candidates)+1, reason))
	}
	skipped, err := s.skippedSimulationCandidates(ctx, resolved, considered, len(result.Candidates)+1)
	if err != nil {
		return GatewaySimulation{}, err
	}
	for index := range skipped {
		skipped[index].Reason = reason
	}
	result.Candidates = append(result.Candidates, skipped...)
	result.Summary = fmt.Sprintf("routing policy blocked all %d routes: %s", len(result.Candidates), reason)
	return result, nil
}

func gatewaySimulationCandidate(candidate GatewayProvider, rank int, reason string) GatewaySimulationCandidate {
	return GatewaySimulationCandidate{
		Rank: rank, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup,
		ProviderID: candidate.ID, ProviderAccountID: candidate.AccountID, UpstreamModel: candidate.UpstreamModel,
		ProviderType: candidate.Type, UpstreamFormat: candidate.UpstreamFormat, Adapter: candidate.Type,
		Headroom: candidate.Headroom, RPMLimit: candidate.RPMLimit, TPMLimit: candidate.TPMLimit,
		Concurrency: candidate.Concurrency, CircuitState: candidate.CircuitState,
		Eligible: reason == "", Reason: reason,
		PolicyBatchOrder: candidate.PolicyBatchOrder, PolicyBatchName: candidate.PolicyBatchName,
		PolicyBatchPosition:        candidate.PolicyBatchPosition,
		PriceFactPresent:           candidate.PriceFactPresent,
		EstimatedInputMicrosPer1M:  candidate.EstimatedInputMicrosPer1M,
		EstimatedOutputMicrosPer1M: candidate.EstimatedOutputMicrosPer1M,
		ObservedSuccessRate:        candidate.ObservedSuccessRate, ObservedAvgLatencyMS: candidate.ObservedAvgLatencyMS,
		ObservedSampleCount: candidate.ObservedSampleCount, SelectionReason: candidate.SelectionReason,
	}
}

func (s *Service) skippedSimulationCandidates(ctx context.Context, resolved ResolvedGatewayModel, ranked map[string]struct{}, rankStart int) ([]GatewaySimulationCandidate, error) {
	routes, err := s.repo.ListModelRoutes(ctx)
	if err != nil {
		return nil, err
	}
	accounts, err := s.repo.ListProviderAccounts(ctx)
	if err != nil {
		return nil, err
	}
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	accountsByID := make(map[string]ProviderAccount, len(accounts))
	for _, account := range accounts {
		accountsByID[account.ID] = account
	}
	providersByID := providerByIDMap(providers)
	now := time.Now().UTC()
	out := make([]GatewaySimulationCandidate, 0)
	for _, route := range routes {
		if route.GatewayModelID != resolved.GatewayModel.ID || route.RouteGroup != resolved.RouteGroup {
			continue
		}
		if _, ok := ranked[route.ID]; ok {
			continue
		}
		candidate := GatewaySimulationCandidate{
			Rank: rankStart + len(out), RouteID: route.ID, RouteGroup: route.RouteGroup,
			ProviderAccountID: route.ProviderAccountID, UpstreamModel: route.UpstreamModel, UpstreamFormat: route.UpstreamFormat,
			Eligible: false,
		}
		if route.Status != ModelRouteStatusActive {
			candidate.Reason = "route_disabled"
			out = append(out, candidate)
			continue
		}
		account, ok := accountsByID[route.ProviderAccountID]
		if !ok {
			candidate.Reason = "account_not_found"
			out = append(out, candidate)
			continue
		}
		candidate.UpstreamModel = ProviderAccountDispatchModel(account, route.UpstreamModel, resolved.RequestedID)
		candidate.RPMLimit = account.RPMLimit
		candidate.TPMLimit = account.TPMLimit
		candidate.Concurrency = account.Concurrency
		candidate.CircuitState = account.CircuitState
		candidate.ProviderID = account.ProviderID
		if reason := accountRoutingIneligibilityReason(account, route.UpstreamModel, now); reason != "" {
			candidate.Reason = reason
			out = append(out, candidate)
			continue
		}
		provider, ok := providersByID[account.ProviderID]
		if !ok {
			candidate.Reason = "provider_not_found"
		} else if provider.Status == ProviderStatusDisabled {
			candidate.Reason = "provider_disabled"
		} else if !validHTTPURL(EffectiveProviderAccountBaseURL(account, provider)) {
			candidate.Reason = "provider_url_invalid"
		} else if state, _, eligible := effectiveCircuitState(account, now); !eligible {
			candidate.CircuitState = state
			candidate.Reason = "circuit_open"
		} else {
			candidate.Reason = "not_schedulable"
		}
		if ok {
			candidate.ProviderType = provider.Type
			candidate.Adapter = provider.Type
		}
		out = append(out, candidate)
	}
	return out, nil
}

func simulationProtocolReason(protocol string, features []string, upstreamFormat string) string {
	protocol = strings.TrimSpace(protocol)
	if protocol != "" && !routingPolicyProtocolSupported(protocol) {
		return "client_protocol_unsupported"
	}
	if protocol == "openai_embeddings" && upstreamFormat != UpstreamFormatOpenAIEmbeddings {
		return "protocol_incompatible:openai_embeddings"
	}
	if protocol != "" && upstreamFormat == UpstreamFormatNativeMedia && !routingPolicyNativeProtocolMatches(protocol, upstreamFormat) {
		return "protocol_incompatible:native_media"
	}
	for _, feature := range cleanStringList(features) {
		switch feature {
		case "text", "tools", "stream":
		case "response_format":
			if !oneOf(upstreamFormat, UpstreamFormatOpenAIChat, UpstreamFormatOpenAIResponses, UpstreamFormatGemini) {
				return "protocol_incompatible:response_format"
			}
		case "top_k":
			if !oneOf(upstreamFormat, UpstreamFormatAnthropic, UpstreamFormatGemini) {
				return "protocol_incompatible:top_k"
			}
		default:
			return "feature_unsupported:" + feature
		}
	}
	return ""
}

func accountRoutingIneligibilityReason(account ProviderAccount, model string, now time.Time) string {
	switch {
	case account.Status != AccountStatusActive:
		return "account_" + account.Status
	case !account.Schedulable:
		return "account_not_schedulable"
	case providerAuthRequiresSecret(account.AuthType) && (!account.SecretConfigured || account.SecretCiphertext == ""):
		return "secret_missing"
	case account.ExpiresAt != nil && now.After(*account.ExpiresAt):
		return "account_expired"
	case account.CooldownUntil != nil && now.Before(*account.CooldownUntil):
		return "account_cooling_down"
	case !contains(account.Models, model):
		return "upstream_model_not_exposed"
	default:
		return ""
	}
}

func simulationPermitReason(s *Service, provider GatewayProvider, estimatedTokens int) string {
	if provider.CircuitState == CircuitStateOpen && !provider.CircuitProbe {
		return "circuit_open"
	}
	if s.providerAccountSlotUsage(provider.AccountID) >= provider.Concurrency && provider.Concurrency > 0 {
		return "at_capacity"
	}
	if s.scheduler == nil {
		return ""
	}
	s.scheduler.mu.Lock()
	defer s.scheduler.mu.Unlock()
	samples := s.scheduler.pruneSamples(provider.AccountID, time.Now().UTC())
	requests, tokens := rateWindowUsage(samples)
	if provider.RPMLimit > 0 && requests >= provider.RPMLimit {
		return "rpm_exhausted"
	}
	if provider.TPMLimit > 0 && tokens+nonNegative(estimatedTokens) > provider.TPMLimit {
		return "tpm_exhausted"
	}
	return ""
}
