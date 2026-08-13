package controlplane

import (
	"context"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

type GatewayExecutionPlan struct {
	Request              gatewaycore.CanonicalRequest     `json:"request"`
	Auth                 gatewaycore.CanonicalAuthContext `json:"auth"`
	GatewayModelID       string                           `json:"gateway_model_id"`
	RouteGroup           string                           `json:"route_group"`
	RoutingPolicyID      string                           `json:"routing_policy_id,omitempty"`
	RoutingPolicyVersion int                              `json:"routing_policy_version,omitempty"`
	RoutingPolicyPreset  string                           `json:"routing_policy_preset,omitempty"`
	Candidates           []GatewayProvider                `json:"-"`
	Exclusions           []GatewayCandidateExclusion      `json:"exclusions"`
	HasRoutes            bool                             `json:"has_routes"`
	RejectionReason      string                           `json:"rejection_reason,omitempty"`
}

type GatewayCandidateExclusion struct {
	RouteID           string `json:"route_id"`
	ProviderID        string `json:"provider_id,omitempty"`
	ProviderAccountID string `json:"provider_account_id,omitempty"`
	UpstreamModel     string `json:"upstream_model,omitempty"`
	Reason            string `json:"reason"`
}

type routingPolicyCandidateDecision struct {
	Candidates []GatewayProvider
	Exclusions []GatewayCandidateExclusion
}

func (s *Service) AuthorizeCanonicalGatewayRequest(ctx context.Context, credential gatewaycore.CredentialEnvelope, request gatewaycore.CanonicalRequest) (GatewayAuthContext, gatewaycore.CanonicalAuthContext, error) {
	return s.authorizeCanonicalGatewayRequest(ctx, credential, request, true)
}

// RevalidateCanonicalGatewayRequest repeats credential and canonical policy
// checks for a live connection without updating API key LastUsedAt.
func (s *Service) RevalidateCanonicalGatewayRequest(ctx context.Context, credential gatewaycore.CredentialEnvelope, request gatewaycore.CanonicalRequest) (GatewayAuthContext, gatewaycore.CanonicalAuthContext, error) {
	return s.authorizeCanonicalGatewayRequest(ctx, credential, request, false)
}

func (s *Service) authorizeCanonicalGatewayRequest(ctx context.Context, credential gatewaycore.CredentialEnvelope, request gatewaycore.CanonicalRequest, recordLastUsed bool) (GatewayAuthContext, gatewaycore.CanonicalAuthContext, error) {
	if request.Protocol == "" || request.Operation == "" || request.Modality == "" || request.Lane == "" {
		return GatewayAuthContext{}, gatewaycore.CanonicalAuthContext{}, gatewaycore.ErrInvalidCanonicalRequest
	}
	if request.Protocol != gatewaycore.ProtocolOpenAIModels && strings.TrimSpace(request.Model) == "" {
		return GatewayAuthContext{}, gatewaycore.CanonicalAuthContext{}, gatewaycore.ErrInvalidCanonicalRequest
	}
	var auth GatewayAuthContext
	var err error
	auth, err = s.authenticateGatewayCredential(ctx, credential.BearerToken, credential.SignedContext, recordLastUsed)
	if err == nil && request.Model != "" && !s.gatewayModelAllowed(auth, request.Model) {
		err = ErrGatewayForbidden
	}
	if err != nil {
		return GatewayAuthContext{}, gatewaycore.CanonicalAuthContext{}, err
	}
	if !apiKeyAllowsCanonicalRequest(auth.APIKey, request) {
		return GatewayAuthContext{}, gatewaycore.CanonicalAuthContext{}, ErrGatewayPolicyForbidden
	}
	return auth, s.canonicalAuthContext(auth), nil
}

func (s *Service) PlanCanonicalGatewayRequest(ctx context.Context, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) (GatewayExecutionPlan, error) {
	if strings.TrimSpace(auth.CredentialID) == "" || strings.TrimSpace(request.Model) == "" {
		return GatewayExecutionPlan{}, ErrGatewayUnauthorized
	}
	return s.planCanonicalGatewayRequest(ctx, auth, request)
}

func (s *Service) planCanonicalGatewayRequest(ctx context.Context, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) (GatewayExecutionPlan, error) {
	resolved, found, err := s.ResolveGatewayModel(ctx, request.Model)
	if err != nil {
		return GatewayExecutionPlan{}, err
	}
	if !found {
		return GatewayExecutionPlan{Request: request, Auth: auth, RejectionReason: "model_not_found"}, nil
	}
	if !gatewayModelSupportsCanonicalRequest(resolved.GatewayModel, request) {
		return GatewayExecutionPlan{Request: request, Auth: auth, GatewayModelID: resolved.GatewayModel.ID, RouteGroup: resolved.RouteGroup, RejectionReason: "capability_mismatch"}, nil
	}
	routingPolicy, err := s.activeRoutingPolicyForGroup(ctx, resolved.RouteGroup)
	if err != nil {
		return GatewayExecutionPlan{}, err
	}
	if routingPolicy != nil && !routingPolicyAllowsModel(routingPolicy.Strategy, resolved.RequestedID) {
		return GatewayExecutionPlan{
			Request: request, Auth: auth, GatewayModelID: resolved.GatewayModel.ID, RouteGroup: resolved.RouteGroup,
			RoutingPolicyID: routingPolicy.ID, RoutingPolicyVersion: routingPolicy.Version, RoutingPolicyPreset: routingPolicy.Strategy.Preset,
			HasRoutes: true, RejectionReason: "routing_policy_model_blocked",
		}, nil
	}
	if routingPolicy != nil && !routingPolicyAllowsProtocol(routingPolicy.Strategy, string(request.Protocol)) {
		return GatewayExecutionPlan{
			Request: request, Auth: auth, GatewayModelID: resolved.GatewayModel.ID, RouteGroup: resolved.RouteGroup,
			RoutingPolicyID: routingPolicy.ID, RoutingPolicyVersion: routingPolicy.Version, RoutingPolicyPreset: routingPolicy.Strategy.Preset,
			HasRoutes: true, RejectionReason: "routing_policy_protocol_blocked",
		}, nil
	}
	candidates, hasRoutes, err := s.GatewayProviderCandidatesForModel(ctx, request.Model)
	if err != nil {
		return GatewayExecutionPlan{}, err
	}
	consideredCandidates := append([]GatewayProvider(nil), candidates...)
	decision, err := s.applyRoutingPolicyCandidateRules(ctx, routingPolicy, string(request.Protocol), candidates)
	if err != nil {
		return GatewayExecutionPlan{}, err
	}
	candidates = decision.Candidates
	exclusions, err := s.gatewayCandidateExclusions(ctx, resolved, consideredCandidates, decision.Exclusions)
	if err != nil {
		return GatewayExecutionPlan{}, err
	}
	rejectionReason := ""
	if len(candidates) == 0 && hasRoutes {
		rejectionReason = "all_candidates_excluded"
	}
	plan := GatewayExecutionPlan{
		Request:         request,
		Auth:            auth,
		GatewayModelID:  resolved.GatewayModel.ID,
		RouteGroup:      resolved.RouteGroup,
		Candidates:      candidates,
		Exclusions:      exclusions,
		HasRoutes:       hasRoutes,
		RejectionReason: rejectionReason,
	}
	if routingPolicy != nil {
		plan.RoutingPolicyID = routingPolicy.ID
		plan.RoutingPolicyVersion = routingPolicy.Version
		plan.RoutingPolicyPreset = routingPolicy.Strategy.Preset
	}
	return plan, nil
}

func (s *Service) applyRoutingPolicyCandidateRules(ctx context.Context, policy *RoutingPolicy, protocol string, candidates []GatewayProvider) (routingPolicyCandidateDecision, error) {
	decision := routingPolicyCandidateDecision{Candidates: append([]GatewayProvider(nil), candidates...), Exclusions: []GatewayCandidateExclusion{}}
	if policy == nil || len(candidates) == 0 {
		return decision, nil
	}

	if policy.Strategy.NativeProtocolOnly {
		retained := make([]GatewayProvider, 0, len(decision.Candidates))
		for _, candidate := range decision.Candidates {
			if routingPolicyNativeProtocolMatches(protocol, candidate.UpstreamFormat) {
				retained = append(retained, candidate)
				continue
			}
			decision.Exclusions = append(decision.Exclusions, gatewayCandidatePolicyExclusion(candidate, "routing_policy_native_protocol_required"))
		}
		decision.Candidates = retained
	}

	priceDecision, err := s.evaluateRoutingPolicyPriceRules(ctx, policy, protocol, decision.Candidates)
	if err != nil {
		return routingPolicyCandidateDecision{}, err
	}
	for _, exclusion := range priceDecision.exclusions {
		decision.Exclusions = append(decision.Exclusions, gatewayCandidatePolicyExclusion(exclusion.candidate, exclusion.reason))
	}
	decision.Candidates = priceDecision.candidates

	if len(decision.Candidates) > 1 && !policy.Strategy.FailoverBeforeFirstByte {
		for _, candidate := range decision.Candidates[1:] {
			decision.Exclusions = append(decision.Exclusions, gatewayCandidatePolicyExclusion(candidate, "routing_policy_failover_disabled"))
		}
		decision.Candidates = decision.Candidates[:1]
	}
	return decision, nil
}

func gatewayCandidatePolicyExclusion(candidate GatewayProvider, reason string) GatewayCandidateExclusion {
	return GatewayCandidateExclusion{
		RouteID: candidate.RouteID, ProviderID: candidate.ID, ProviderAccountID: candidate.AccountID,
		UpstreamModel: candidate.UpstreamModel, Reason: reason,
	}
}

func (s *Service) gatewayCandidateExclusions(ctx context.Context, resolved ResolvedGatewayModel, considered []GatewayProvider, policyExclusions []GatewayCandidateExclusion) ([]GatewayCandidateExclusion, error) {
	included := make(map[string]struct{}, len(considered))
	for _, candidate := range considered {
		if candidate.RouteID != "" {
			included[candidate.RouteID] = struct{}{}
		}
	}
	skipped, err := s.skippedSimulationCandidates(ctx, resolved, included, 1)
	if err != nil {
		return nil, err
	}
	exclusions := make([]GatewayCandidateExclusion, 0, len(skipped)+len(policyExclusions))
	for _, candidate := range skipped {
		exclusions = append(exclusions, GatewayCandidateExclusion{
			RouteID: candidate.RouteID, ProviderID: candidate.ProviderID, ProviderAccountID: candidate.ProviderAccountID,
			UpstreamModel: candidate.UpstreamModel, Reason: candidate.Reason,
		})
	}
	exclusions = append(exclusions, policyExclusions...)
	return exclusions, nil
}

func (s *Service) canonicalAuthContext(auth GatewayAuthContext) gatewaycore.CanonicalAuthContext {
	source := gatewaycore.CredentialSourceAPIKey
	credentialID := auth.APIKey.ID
	integrationID := ""
	if auth.ExternalAuthIntegration != nil {
		integrationID = auth.ExternalAuthIntegration.ID
		switch auth.ExternalAuthIntegration.Protocol {
		case ExternalAuthIntegrationProtocolHMAC:
			source = gatewaycore.CredentialSourceHMACContext
		case ExternalAuthIntegrationProtocolJWT:
			source = gatewaycore.CredentialSourceJWTJWKS
		}
	}
	applicationID := strings.TrimSpace(auth.APIKey.ApplicationID)
	if applicationID == "" {
		applicationID = gatewayDefaultApplicationID
	}
	principalType := strings.TrimSpace(auth.APIKey.PrincipalType)
	if principalType == "" {
		principalType = strings.TrimSpace(auth.APIKey.KeyType)
	}
	principalID := strings.TrimSpace(auth.APIKey.PrincipalReference)
	if principalID == "" {
		principalID = strings.TrimSpace(auth.APIKey.ID)
	}
	if auth.APIKey.OwnerUserID != "" {
		principalID = auth.APIKey.OwnerUserID
	}
	if auth.Application != nil {
		applicationID = auth.Application.ID
	}
	if auth.GatewayPrincipal != nil {
		principalType = auth.GatewayPrincipal.PrincipalType
		principalID = auth.GatewayPrincipal.ID
	}
	keyPolicy := effectiveAPIKeyPolicy(auth.APIKey)
	policyID := ""
	policyVersion := 0
	if auth.Policy != nil {
		policyID = auth.Policy.ID
		policyVersion = governancePolicyVersion(*auth.Policy)
	}
	allowedModels := make([]string, 0, len(auth.APIKey.ModelAllowlist))
	for _, model := range auth.APIKey.ModelAllowlist {
		if model = strings.TrimSpace(model); model != "" && s.gatewayModelAllowed(auth, model) && !contains(allowedModels, model) {
			allowedModels = append(allowedModels, model)
		}
	}
	return gatewaycore.CanonicalAuthContext{
		CredentialSource:         source,
		CredentialID:             credentialID,
		CredentialFingerprint:    auth.APIKey.Fingerprint,
		IntegrationID:            integrationID,
		ApplicationID:            applicationID,
		PrincipalType:            principalType,
		PrincipalID:              principalID,
		ExternalSubjectReference: auth.ExternalSubjectReference,
		PolicyID:                 policyID,
		PolicyVersion:            policyVersion,
		Scopes:                   append([]string(nil), keyPolicy.scopes...),
		AllowedModels:            allowedModels,
		AllowedModalities:        append([]string(nil), keyPolicy.allowedModalities...),
		AllowedOperations:        append([]string(nil), keyPolicy.allowedOperations...),
		AllowedCIDRs:             append([]string(nil), keyPolicy.allowedCIDRs...),
		Limits: gatewaycore.CanonicalLimits{
			QPSLimit:                    auth.effectiveQPSLimit(),
			RPMLimit:                    auth.APIKey.RPMLimit,
			TPMLimit:                    auth.APIKey.TPMLimit,
			ConcurrencyLimit:            auth.APIKey.ConcurrencyLimit,
			ApplicationConcurrencyLimit: applicationConcurrencyLimit(auth.Application),
			MonthlyTokenLimit:           auth.effectiveMonthlyTokenLimit(),
			MonthlyBudgetMicros:         auth.effectiveMonthlyBudgetMicros(),
			MonthlyImageLimit:           auth.APIKey.MonthlyImageLimit,
			MonthlyVideoSecondsLimit:    auth.APIKey.MonthlyVideoSecondsLimit,
			MonthlyAudioSecondsLimit:    auth.APIKey.MonthlyAudioSecondsLimit,
		},
		LanePolicy:     keyPolicy.lanePolicy,
		ArtifactPolicy: keyPolicy.artifactPolicy,
		ArtifactSinkID: keyPolicy.artifactSinkID,
	}
}

func applicationConcurrencyLimit(application *Application) int {
	if application == nil || application.ConcurrencyLimit <= 0 {
		return 0
	}
	return application.ConcurrencyLimit
}

func gatewayModelSupportsCanonicalRequest(model GatewayModel, request gatewaycore.CanonicalRequest) bool {
	switch request.Protocol {
	case gatewaycore.ProtocolOpenAIChat:
		return model.Modality == "chat" || model.Modality == "multimodal"
	case gatewaycore.ProtocolOpenAIResponses, gatewaycore.ProtocolAnthropicMessages, gatewaycore.ProtocolAnthropicCountTokens, gatewaycore.ProtocolGeminiGenerate:
		return model.Modality == "chat" || model.Modality == "multimodal"
	case gatewaycore.ProtocolOpenAIEmbeddings:
		return model.Modality == GatewayModalityEmbedding
	case gatewaycore.ProtocolOpenAIImages:
		return model.Modality == GatewayModalityImage || model.Modality == "multimodal"
	case gatewaycore.ProtocolOpenAIMedia:
		return model.Modality == request.Modality || model.Modality == "multimodal"
	case gatewaycore.ProtocolOpenAIAudioTranscriptions, gatewaycore.ProtocolOpenAIAudioTranslations, gatewaycore.ProtocolOpenAIAudioSpeech:
		return model.Modality == GatewayModalityAudio || model.Modality == "multimodal"
	case gatewaycore.ProtocolRealtime:
		return request.Operation == GatewayOperationRealtimeSession && (model.Modality == GatewayModalityAudio || model.Modality == "multimodal")
	case gatewaycore.ProtocolAsterJobs:
		return model.Modality == request.Modality || model.Modality == "multimodal"
	default:
		return false
	}
}
