package controlplane

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type GatewaySimulationRequest struct {
	Model            string   `json:"model"`
	EstimatedTokens  int      `json:"estimated_tokens"`
	Protocol         string   `json:"protocol"`
	RequiredFeatures []string `json:"required_features"`
}

type GatewaySimulationCandidate struct {
	Rank              int     `json:"rank"`
	RouteID           string  `json:"route_id"`
	RouteGroup        string  `json:"route_group"`
	ProviderID        string  `json:"provider_id"`
	ProviderAccountID string  `json:"provider_account_id"`
	UpstreamModel     string  `json:"upstream_model"`
	ProviderType      string  `json:"provider_type"`
	UpstreamFormat    string  `json:"upstream_format"`
	Adapter           string  `json:"adapter"`
	Headroom          float64 `json:"headroom"`
	RPMLimit          int     `json:"rpm_limit"`
	TPMLimit          int     `json:"tpm_limit"`
	Concurrency       int     `json:"concurrency"`
	CircuitState      string  `json:"circuit_state"`
	Eligible          bool    `json:"eligible"`
	Reason            string  `json:"reason"`
}

type GatewaySimulation struct {
	RequestedModel string                       `json:"requested_model"`
	ResolvedModel  string                       `json:"resolved_model"`
	RouteGroup     string                       `json:"route_group"`
	Status         string                       `json:"status"`
	Summary        string                       `json:"summary"`
	Candidates     []GatewaySimulationCandidate `json:"candidates"`
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
	ranked, hasRoutes, err := s.rankedModelRouteCandidates(ctx, resolved)
	if err != nil {
		return GatewaySimulation{}, err
	}
	if !hasRoutes {
		result.Status = "no_routes"
		result.Summary = "no model routes exist for the resolved route group"
		return result, nil
	}
	rankedByRouteID := make(map[string]struct{}, len(ranked))
	for _, candidate := range ranked {
		rankedByRouteID[candidate.route.ID] = struct{}{}
	}
	for index, candidate := range ranked {
		provider := GatewayProvider{
			AccountID: candidate.account.ID, RPMLimit: candidate.account.RPMLimit, TPMLimit: candidate.account.TPMLimit,
			CircuitState: candidate.circuitState, CircuitProbe: candidate.circuitProbe, Concurrency: candidate.account.Concurrency,
		}
		reason := simulationPermitReason(s, provider, req.EstimatedTokens)
		if reason == "" {
			reason = simulationProtocolReason(req.Protocol, req.RequiredFeatures, candidate.route.UpstreamFormat)
		}
		result.Candidates = append(result.Candidates, GatewaySimulationCandidate{
			Rank: index + 1, RouteID: candidate.route.ID, RouteGroup: candidate.route.RouteGroup,
			ProviderID: candidate.provider.ID, ProviderAccountID: candidate.account.ID, UpstreamModel: ProviderAccountDispatchModel(candidate.account, candidate.route.UpstreamModel, resolved.RequestedID),
			ProviderType: candidate.provider.Type, UpstreamFormat: candidate.route.UpstreamFormat, Adapter: candidate.provider.Type,
			Headroom: candidate.headroom, RPMLimit: candidate.account.RPMLimit, TPMLimit: candidate.account.TPMLimit,
			Concurrency: candidate.account.Concurrency, CircuitState: candidate.circuitState,
			Eligible: reason == "", Reason: reason,
		})
	}
	skipped, err := s.skippedSimulationCandidates(ctx, resolved, rankedByRouteID, len(result.Candidates)+1)
	if err != nil {
		return GatewaySimulation{}, err
	}
	result.Candidates = append(result.Candidates, skipped...)
	result.Status = "ready"
	result.Summary = fmt.Sprintf("resolved %d candidates without consuming scheduling capacity", len(result.Candidates))
	return result, nil
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
	if protocol != "" && !oneOf(protocol, "openai_chat_completions", "openai_responses", "anthropic_messages", "gemini_generate_content") {
		return "client_protocol_unsupported"
	}
	if protocol != "" && upstreamFormat == UpstreamFormatNativeMedia {
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
