package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *Service) ListGatewayModels(ctx context.Context) ([]GatewayModel, error) {
	return s.repo.ListGatewayModels(ctx)
}

func (s *Service) CreateGatewayModel(ctx context.Context, actor string, req GatewayModelRequest) (GatewayModel, error) {
	model, err := gatewayModelFromRequest(req, time.Now().UTC())
	if err != nil {
		return GatewayModel{}, err
	}
	if err := s.ensureGatewayModelIDUnique(ctx, model.ModelID, ""); err != nil {
		return GatewayModel{}, err
	}
	model.ID = "gmodel_" + randomID(10)
	if err := s.repo.SaveGatewayModel(ctx, model); err != nil {
		return GatewayModel{}, err
	}
	if err := s.audit(ctx, actor, "create", "gateway_model", model.ID, fmt.Sprintf("Created gateway model %s", model.ModelID)); err != nil {
		return GatewayModel{}, err
	}
	return model, nil
}

func (s *Service) UpdateGatewayModel(ctx context.Context, actor string, id string, req GatewayModelRequest) (GatewayModel, error) {
	existing, err := s.gatewayModelByID(ctx, id)
	if err != nil {
		return GatewayModel{}, err
	}
	model, err := gatewayModelFromRequest(req, existing.CreatedAt)
	if err != nil {
		return GatewayModel{}, err
	}
	if err := s.ensureGatewayModelIDUnique(ctx, model.ModelID, existing.ID); err != nil {
		return GatewayModel{}, err
	}
	model.ID = existing.ID
	model.CreatedAt = existing.CreatedAt
	model.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveGatewayModel(ctx, model); err != nil {
		return GatewayModel{}, err
	}
	if err := s.audit(ctx, actor, "update", "gateway_model", model.ID, fmt.Sprintf("Updated gateway model %s", model.ModelID)); err != nil {
		return GatewayModel{}, err
	}
	return model, nil
}

func (s *Service) DeleteGatewayModel(ctx context.Context, actor string, id string) error {
	model, err := s.gatewayModelByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteGatewayModel(ctx, model.ID); err != nil {
		return err
	}
	if err := s.audit(ctx, actor, "delete", "gateway_model", model.ID, fmt.Sprintf("Deleted gateway model %s and its routes", model.ModelID)); err != nil {
		return err
	}
	return nil
}

func (s *Service) ListModelRoutes(ctx context.Context) ([]ModelRoute, error) {
	return s.repo.ListModelRoutes(ctx)
}

func (s *Service) CreateModelRoute(ctx context.Context, actor string, req ModelRouteRequest) (ModelRoute, error) {
	route, err := s.modelRouteFromRequest(ctx, req, time.Now().UTC())
	if err != nil {
		return ModelRoute{}, err
	}
	if err := s.ensureModelRouteUnique(ctx, route, ""); err != nil {
		return ModelRoute{}, err
	}
	route.ID = "mroute_" + randomID(10)
	if err := s.repo.SaveModelRoute(ctx, route); err != nil {
		return ModelRoute{}, err
	}
	if err := s.audit(ctx, actor, "create", "model_route", route.ID, fmt.Sprintf("Created model route to %s", route.UpstreamModel)); err != nil {
		return ModelRoute{}, err
	}
	return route, nil
}

func (s *Service) BulkCreateModelRoutes(ctx context.Context, actor string, req ModelRouteBulkCreateRequest) (ModelRouteBulkCreateResult, error) {
	if len(req.Routes) == 0 {
		return ModelRouteBulkCreateResult{}, errors.New("routes must not be empty")
	}
	if len(req.Routes) > 500 {
		return ModelRouteBulkCreateResult{}, errors.New("routes must contain at most 500 entries")
	}
	existing, err := s.repo.ListModelRoutes(ctx)
	if err != nil {
		return ModelRouteBulkCreateResult{}, err
	}
	keys := make(map[string]struct{}, len(existing)+len(req.Routes))
	for _, route := range existing {
		keys[modelRouteUniqueKey(route)] = struct{}{}
	}
	now := time.Now().UTC()
	routes := make([]ModelRoute, 0, len(req.Routes))
	for index, routeRequest := range req.Routes {
		route, err := s.modelRouteFromRequest(ctx, routeRequest, now)
		if err != nil {
			return ModelRouteBulkCreateResult{}, fmt.Errorf("routes[%d]: %w", index, err)
		}
		key := modelRouteUniqueKey(route)
		if _, exists := keys[key]; exists {
			return ModelRouteBulkCreateResult{}, fmt.Errorf("routes[%d]: model route already exists", index)
		}
		keys[key] = struct{}{}
		route.ID = "mroute_" + randomID(10)
		routes = append(routes, route)
	}
	if err := s.repo.SaveModelRoutes(ctx, routes); err != nil {
		return ModelRouteBulkCreateResult{}, err
	}
	if err := s.audit(ctx, actor, "create", "model_route_batch", routes[0].ID, fmt.Sprintf("Created %d model routes", len(routes))); err != nil {
		return ModelRouteBulkCreateResult{}, err
	}
	return ModelRouteBulkCreateResult{Routes: routes}, nil
}

func (s *Service) UpdateModelRoute(ctx context.Context, actor string, id string, req ModelRouteRequest) (ModelRoute, error) {
	existing, err := s.modelRouteByID(ctx, id)
	if err != nil {
		return ModelRoute{}, err
	}
	route, err := s.modelRouteFromRequest(ctx, req, existing.CreatedAt)
	if err != nil {
		return ModelRoute{}, err
	}
	if err := s.ensureModelRouteUnique(ctx, route, existing.ID); err != nil {
		return ModelRoute{}, err
	}
	route.ID = existing.ID
	route.CreatedAt = existing.CreatedAt
	route.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveModelRoute(ctx, route); err != nil {
		return ModelRoute{}, err
	}
	if err := s.audit(ctx, actor, "update", "model_route", route.ID, fmt.Sprintf("Updated model route to %s", route.UpstreamModel)); err != nil {
		return ModelRoute{}, err
	}
	return route, nil
}

func (s *Service) DeleteModelRoute(ctx context.Context, actor string, id string) error {
	route, err := s.modelRouteByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteModelRoute(ctx, route.ID); err != nil {
		return err
	}
	return s.audit(ctx, actor, "delete", "model_route", route.ID, fmt.Sprintf("Deleted model route to %s", route.UpstreamModel))
}

func (s *Service) ResolveGatewayModel(ctx context.Context, requestedID string) (ResolvedGatewayModel, bool, error) {
	requestedID = strings.TrimSpace(requestedID)
	if requestedID == "" {
		return ResolvedGatewayModel{}, false, nil
	}
	models, err := s.repo.ListGatewayModels(ctx)
	if err != nil {
		return ResolvedGatewayModel{}, false, err
	}
	for _, model := range models {
		if model.Status == GatewayModelStatusActive && model.ModelID == requestedID {
			return ResolvedGatewayModel{GatewayModel: model, RequestedID: requestedID, RouteGroup: model.DefaultRouteGroup}, true, nil
		}
	}
	separator := strings.LastIndex(requestedID, ":")
	if separator <= 0 || separator == len(requestedID)-1 {
		return ResolvedGatewayModel{}, false, nil
	}
	modelID := requestedID[:separator]
	routeGroup := requestedID[separator+1:]
	for _, model := range models {
		if model.Status == GatewayModelStatusActive && model.ModelID == modelID {
			return ResolvedGatewayModel{GatewayModel: model, RequestedID: requestedID, RouteGroup: routeGroup}, true, nil
		}
	}
	return ResolvedGatewayModel{}, false, nil
}

func gatewayModelFromRequest(req GatewayModelRequest, now time.Time) (GatewayModel, error) {
	modelID := strings.TrimSpace(req.ModelID)
	name := strings.TrimSpace(req.Name)
	modality := strings.TrimSpace(req.Modality)
	defaultRouteGroup := strings.TrimSpace(req.DefaultRouteGroup)
	status := strings.TrimSpace(req.Status)
	if modelID == "" {
		return GatewayModel{}, errors.New("model_id is required")
	}
	if strings.ContainsAny(modelID, " \t\r\n") {
		return GatewayModel{}, errors.New("model_id must not contain whitespace")
	}
	if name == "" {
		name = modelID
	}
	if modality == "" {
		modality = "chat"
	}
	if !oneOf(modality, "chat", "embedding", "image", "video", "audio", "multimodal") {
		return GatewayModel{}, errors.New("modality must be chat, embedding, image, video, audio, or multimodal")
	}
	if defaultRouteGroup == "" {
		defaultRouteGroup = DefaultModelRouteGroup
	}
	if strings.ContainsAny(defaultRouteGroup, " :\t\r\n") {
		return GatewayModel{}, errors.New("default_route_group must not contain whitespace or colon")
	}
	if status == "" {
		status = GatewayModelStatusActive
	}
	if !oneOf(status, GatewayModelStatusActive, GatewayModelStatusDisabled) {
		return GatewayModel{}, errors.New("status must be active or disabled")
	}
	stickyTTLSeconds := req.StickyTTLSeconds
	if stickyTTLSeconds == 0 {
		stickyTTLSeconds = 1800
	}
	if stickyTTLSeconds < 60 || stickyTTLSeconds > 604800 {
		return GatewayModel{}, errors.New("sticky_ttl_seconds must be between 60 and 604800")
	}
	return GatewayModel{
		ModelID:           modelID,
		Name:              name,
		Description:       strings.TrimSpace(req.Description),
		Modality:          modality,
		DefaultRouteGroup: defaultRouteGroup,
		StickyEnabled:     req.StickyEnabled,
		StickyTTLSeconds:  stickyTTLSeconds,
		Status:            status,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

func (s *Service) modelRouteFromRequest(ctx context.Context, req ModelRouteRequest, now time.Time) (ModelRoute, error) {
	gatewayModelID := strings.TrimSpace(req.GatewayModelID)
	routeGroup := strings.TrimSpace(req.RouteGroup)
	providerAccountID := strings.TrimSpace(req.ProviderAccountID)
	upstreamModel := strings.TrimSpace(req.UpstreamModel)
	upstreamFormat := strings.TrimSpace(req.UpstreamFormat)
	status := strings.TrimSpace(req.Status)
	gatewayModel, err := s.gatewayModelByID(ctx, gatewayModelID)
	if err != nil {
		return ModelRoute{}, err
	}
	account, err := s.providerAccountByID(ctx, providerAccountID)
	if err != nil {
		return ModelRoute{}, err
	}
	provider, err := s.providerByID(ctx, account.ProviderID)
	if err != nil {
		return ModelRoute{}, err
	}
	if routeGroup == "" {
		routeGroup = DefaultModelRouteGroup
	}
	if strings.ContainsAny(routeGroup, " :\t\r\n") {
		return ModelRoute{}, errors.New("route_group must not contain whitespace or colon")
	}
	if upstreamModel == "" {
		return ModelRoute{}, errors.New("upstream_model is required")
	}
	if upstreamFormat == "" {
		return ModelRoute{}, errors.New("upstream_format is required")
	}
	if !gatewayModelSupportsUpstreamFormat(gatewayModel.Modality, upstreamFormat) {
		return ModelRoute{}, fmt.Errorf("gateway model modality %q does not support upstream_format %q", gatewayModel.Modality, upstreamFormat)
	}
	if !ProviderSupportsGatewayModelRoute(provider.Type, gatewayModel.Modality, upstreamFormat) {
		return ModelRoute{}, fmt.Errorf("provider type %q does not support gateway model modality %q with upstream_format %q", provider.Type, gatewayModel.Modality, upstreamFormat)
	}
	if !contains(account.Models, upstreamModel) {
		return ModelRoute{}, fmt.Errorf("provider account %q does not expose upstream model %q", account.ID, upstreamModel)
	}
	if req.Priority < 0 {
		return ModelRoute{}, errors.New("priority must be greater than or equal to 0")
	}
	weight := req.Weight
	if weight == 0 {
		weight = 100
	}
	if weight < 1 || weight > 10000 {
		return ModelRoute{}, errors.New("weight must be between 1 and 10000")
	}
	if status == "" {
		status = ModelRouteStatusActive
	}
	if !oneOf(status, ModelRouteStatusActive, ModelRouteStatusDisabled) {
		return ModelRoute{}, errors.New("status must be active or disabled")
	}
	if status == ModelRouteStatusActive && gatewayModel.Status != GatewayModelStatusActive {
		return ModelRoute{}, errors.New("active model routes require an active gateway model")
	}
	return ModelRoute{
		GatewayModelID:    gatewayModelID,
		RouteGroup:        routeGroup,
		ProviderAccountID: providerAccountID,
		UpstreamModel:     upstreamModel,
		UpstreamFormat:    upstreamFormat,
		Priority:          req.Priority,
		Weight:            weight,
		Status:            status,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

// ProviderSupportsGatewayModelRoute validates the executable route boundary.
// Media plugins may extend image and video providers, while the built-in audio
// and Realtime transports are deliberately limited to OpenAI-compatible APIs.
func ProviderSupportsGatewayModelRoute(providerType, modality, upstreamFormat string) bool {
	providerType = strings.TrimSpace(providerType)
	modality = strings.TrimSpace(modality)
	upstreamFormat = strings.TrimSpace(upstreamFormat)
	if !gatewayModelSupportsUpstreamFormat(modality, upstreamFormat) {
		return false
	}
	providerTypes := []string{ProviderTypeOpenAICompatible, ProviderTypeAnthropicCompatible, ProviderTypeGeminiCompatible, ProviderTypeAWSBedrock, ProviderTypeGCPVertex, ProviderTypeAzureOpenAI}
	if upstreamFormat == UpstreamFormatNativeMedia {
		switch modality {
		case GatewayModalityAudio:
			return providerType == ProviderTypeOpenAICompatible
		case GatewayModalityImage, GatewayModalityVideo, "multimodal":
			return contains(providerTypes, providerType)
		default:
			return false
		}
	}
	formats := map[string][]string{
		ProviderTypeOpenAICompatible:    {UpstreamFormatOpenAIChat, UpstreamFormatOpenAIResponses, UpstreamFormatOpenAIEmbeddings},
		ProviderTypeAnthropicCompatible: {UpstreamFormatAnthropic},
		ProviderTypeGeminiCompatible:    {UpstreamFormatGemini},
		ProviderTypeAWSBedrock:          {UpstreamFormatBedrockConverse},
		ProviderTypeGCPVertex:           {UpstreamFormatAnthropic, UpstreamFormatGemini},
		ProviderTypeAzureOpenAI:         {UpstreamFormatOpenAIChat, UpstreamFormatOpenAIResponses, UpstreamFormatOpenAIEmbeddings},
	}
	return contains(formats[providerType], upstreamFormat)
}

func gatewayModelSupportsUpstreamFormat(modality, upstreamFormat string) bool {
	switch strings.TrimSpace(modality) {
	case "chat":
		return upstreamFormat != UpstreamFormatNativeMedia
	case "multimodal":
		return true
	case GatewayModalityImage, GatewayModalityVideo, GatewayModalityAudio:
		return upstreamFormat == UpstreamFormatNativeMedia
	case GatewayModalityEmbedding:
		return upstreamFormat == UpstreamFormatOpenAIEmbeddings
	default:
		return false
	}
}

func (s *Service) gatewayModelByID(ctx context.Context, id string) (GatewayModel, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return GatewayModel{}, errors.New("gateway model id is required")
	}
	models, err := s.repo.ListGatewayModels(ctx)
	if err != nil {
		return GatewayModel{}, err
	}
	for _, model := range models {
		if model.ID == id {
			return model, nil
		}
	}
	return GatewayModel{}, fmt.Errorf("gateway model %q not found", id)
}

func (s *Service) modelRouteByID(ctx context.Context, id string) (ModelRoute, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ModelRoute{}, errors.New("model route id is required")
	}
	routes, err := s.repo.ListModelRoutes(ctx)
	if err != nil {
		return ModelRoute{}, err
	}
	for _, route := range routes {
		if route.ID == id {
			return route, nil
		}
	}
	return ModelRoute{}, fmt.Errorf("model route %q not found", id)
}

func (s *Service) ensureGatewayModelIDUnique(ctx context.Context, modelID string, exceptID string) error {
	models, err := s.repo.ListGatewayModels(ctx)
	if err != nil {
		return err
	}
	for _, model := range models {
		if model.ID != exceptID && model.ModelID == modelID {
			return fmt.Errorf("gateway model_id %q already exists", modelID)
		}
	}
	return nil
}

func (s *Service) ensureModelRouteUnique(ctx context.Context, candidate ModelRoute, exceptID string) error {
	routes, err := s.repo.ListModelRoutes(ctx)
	if err != nil {
		return err
	}
	for _, route := range routes {
		if route.ID == exceptID {
			continue
		}
		if route.GatewayModelID == candidate.GatewayModelID && route.RouteGroup == candidate.RouteGroup && route.ProviderAccountID == candidate.ProviderAccountID && route.UpstreamModel == candidate.UpstreamModel {
			return errors.New("an equivalent model route already exists")
		}
	}
	return nil
}

func modelRouteUniqueKey(route ModelRoute) string {
	return route.GatewayModelID + "\x00" + route.RouteGroup + "\x00" + route.ProviderAccountID + "\x00" + route.UpstreamModel
}

type rankedModelRouteCandidate struct {
	route               ModelRoute
	account             ProviderAccount
	provider            ProviderConnection
	loadRatio           float64
	headroom            float64
	weightScore         float64
	circuitState        string
	circuitProbe        bool
	policyBatch         int
	policyBatchName     string
	policyBatchPosition int
	preferred           bool
	routingMetrics      ProviderAccountRoutingMetrics
	policy              *RoutingPolicy
}

func (s *Service) rankedModelRouteCandidates(ctx context.Context, resolved ResolvedGatewayModel) ([]rankedModelRouteCandidate, bool, error) {
	policy, err := s.activeRoutingPolicyForGroup(ctx, resolved.RouteGroup)
	if err != nil {
		return nil, false, err
	}
	return s.rankedModelRouteCandidatesWithPolicy(ctx, resolved, policy)
}

func (s *Service) rankedModelRouteCandidatesWithPolicy(ctx context.Context, resolved ResolvedGatewayModel, policy *RoutingPolicy) ([]rankedModelRouteCandidate, bool, error) {
	routes, err := s.repo.ListModelRoutes(ctx)
	if err != nil {
		return nil, false, err
	}
	matchingRoutes := make([]ModelRoute, 0)
	for _, route := range routes {
		if route.GatewayModelID == resolved.GatewayModel.ID && route.RouteGroup == resolved.RouteGroup {
			matchingRoutes = append(matchingRoutes, route)
		}
	}
	if len(matchingRoutes) == 0 {
		return nil, false, nil
	}
	if policy != nil && !routingPolicyAllowsModel(policy.Strategy, resolved.RequestedID) {
		return nil, true, nil
	}
	accounts, err := s.repo.ListProviderAccounts(ctx)
	if err != nil {
		return nil, true, err
	}
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return nil, true, err
	}
	accountsByID := make(map[string]ProviderAccount, len(accounts))
	for _, account := range accounts {
		accountsByID[account.ID] = account
	}
	providersByID := providerByIDMap(providers)
	now := time.Now().UTC()
	routingMetrics, err := s.repo.SummarizeProviderAccountRoutingMetrics(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return nil, true, err
	}
	routingMetricsByAccount := make(map[string]ProviderAccountRoutingMetrics, len(routingMetrics))
	for _, metrics := range routingMetrics {
		routingMetricsByAccount[metrics.ProviderAccountID] = metrics
	}
	billingHealthByAccount, _ := s.providerBillingRoutingHealthByAccount(ctx, now)
	candidates := make([]rankedModelRouteCandidate, 0, len(matchingRoutes))
	policyPlacementByAccount := routingPolicyPlacementByAccount(policy)
	for _, route := range matchingRoutes {
		if route.Status != ModelRouteStatusActive {
			continue
		}
		account, ok := accountsByID[route.ProviderAccountID]
		dispatchModel := ProviderAccountDispatchModel(account, route.UpstreamModel, resolved.RequestedID)
		if !ok || !accountEligibleForRouting(account, dispatchModel, now) {
			continue
		}
		if health, found := billingHealthByAccount[account.ID]; found && health.HardBlocked {
			continue
		}
		provider, ok := providersByID[account.ProviderID]
		if !ok || provider.Status == ProviderStatusDisabled || !validHTTPURL(EffectiveProviderAccountBaseURL(account, provider)) {
			continue
		}
		circuitState, circuitProbe, eligible := effectiveCircuitState(account, now)
		if !eligible {
			continue
		}
		placement := routingPolicyPlacement{}
		if policy != nil && len(policy.Strategy.ResourceBatches) > 0 {
			var listed bool
			placement, listed = policyPlacementByAccount[account.ID]
			if !listed {
				continue
			}
		}
		loadRatio := float64(s.providerAccountSlotUsage(account.ID)) / float64(account.EffectiveLoadFactor())
		headroom := s.providerAccountRateHeadroom(account, now)
		weightScore := 0.0
		if policy == nil || policy.Strategy.SmartOptimization {
			weightScore = weightedCandidateScore(route.Weight * account.Weight)
		}
		candidates = append(candidates, rankedModelRouteCandidate{
			route: route, account: account, provider: provider, loadRatio: loadRatio,
			headroom: headroom, weightScore: weightScore, preferred: policy != nil && contains(policy.Strategy.PreferredProviderAccountIDs, account.ID),
			circuitState: circuitState, circuitProbe: circuitProbe,
			policyBatch: placement.batch, policyBatchName: placement.name, policyBatchPosition: placement.position,
			routingMetrics: routingMetricsByAccount[account.ID], policy: policy,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.policyBatch != right.policyBatch {
			return left.policyBatch < right.policyBatch
		}
		if policy != nil && policy.Strategy.StrictOrder {
			if left.policyBatchPosition != right.policyBatchPosition {
				return left.policyBatchPosition < right.policyBatchPosition
			}
			if left.route.Priority != right.route.Priority {
				return left.route.Priority < right.route.Priority
			}
			return left.route.ID < right.route.ID
		}
		if left.preferred != right.preferred {
			return left.preferred
		}
		preset := RoutingPolicyPresetBalanced
		if policy != nil {
			preset = policy.Strategy.Preset
		}
		switch preset {
		case RoutingPolicyPresetCost:
			if left.account.RateMultiplier != right.account.RateMultiplier {
				return left.account.RateMultiplier < right.account.RateMultiplier
			}
		case RoutingPolicyPresetSpeed:
			if left.routingMetrics.RequestCount > 0 && right.routingMetrics.RequestCount > 0 && left.routingMetrics.AvgLatencyMS != right.routingMetrics.AvgLatencyMS {
				return left.routingMetrics.AvgLatencyMS < right.routingMetrics.AvgLatencyMS
			}
			if left.headroom != right.headroom {
				return left.headroom > right.headroom
			}
			if left.loadRatio != right.loadRatio {
				return left.loadRatio < right.loadRatio
			}
		case RoutingPolicyPresetStability:
			if left.routingMetrics.RequestCount > 0 && right.routingMetrics.RequestCount > 0 && left.routingMetrics.SuccessRate != right.routingMetrics.SuccessRate {
				return left.routingMetrics.SuccessRate > right.routingMetrics.SuccessRate
			}
			if left.circuitProbe != right.circuitProbe {
				return !left.circuitProbe
			}
		}
		if preset == RoutingPolicyPresetBalanced && left.routingMetrics.RequestCount > 0 && right.routingMetrics.RequestCount > 0 && left.routingMetrics.SuccessRate != right.routingMetrics.SuccessRate {
			return left.routingMetrics.SuccessRate > right.routingMetrics.SuccessRate
		}
		if left.route.Priority != right.route.Priority {
			return left.route.Priority < right.route.Priority
		}
		if left.account.Priority != right.account.Priority {
			return left.account.Priority < right.account.Priority
		}
		if left.circuitProbe != right.circuitProbe {
			return !left.circuitProbe
		}
		if left.headroom != right.headroom {
			return left.headroom > right.headroom
		}
		if left.loadRatio != right.loadRatio {
			return left.loadRatio < right.loadRatio
		}
		if left.weightScore != right.weightScore {
			return left.weightScore < right.weightScore
		}
		if left.account.RateMultiplier != right.account.RateMultiplier {
			return left.account.RateMultiplier < right.account.RateMultiplier
		}
		return left.route.ID < right.route.ID
	})
	return candidates, true, nil
}

func (s *Service) activeRoutingPolicyForGroup(ctx context.Context, routeGroup string) (*RoutingPolicy, error) {
	policies, err := s.repo.ListRoutingPolicies(ctx)
	if err != nil {
		return nil, err
	}
	for _, policy := range policies {
		if policy.Status == RoutingPolicyStatusActive && policy.RouteGroup == routeGroup {
			if policy.IsDefault {
				selected := policy
				return &selected, nil
			}
		}
	}
	return nil, nil
}

func routingPolicyAllowsModel(strategy RoutingPolicyStrategy, model string) bool {
	model = strings.TrimSpace(model)
	baseModel := model
	if separator := strings.LastIndex(model, ":"); separator > 0 {
		baseModel = model[:separator]
	}
	if contains(strategy.DeniedModels, model) || contains(strategy.DeniedModels, baseModel) {
		return false
	}
	return len(strategy.AllowedModels) == 0 || contains(strategy.AllowedModels, model) || contains(strategy.AllowedModels, baseModel)
}

func routingPolicyAllowsProtocol(strategy RoutingPolicyStrategy, protocol string) bool {
	protocol = strings.TrimSpace(protocol)
	if contains(strategy.DeniedProtocols, protocol) {
		return false
	}
	return len(strategy.AllowedProtocols) == 0 || contains(strategy.AllowedProtocols, protocol)
}

func routingPolicyNativeProtocolMatches(protocol, upstreamFormat string) bool {
	switch strings.TrimSpace(protocol) {
	case "openai_chat_completions":
		return upstreamFormat == UpstreamFormatOpenAIChat
	case "openai_responses":
		return upstreamFormat == UpstreamFormatOpenAIResponses
	case "openai_embeddings":
		return upstreamFormat == UpstreamFormatOpenAIEmbeddings
	case "anthropic_messages":
		return upstreamFormat == UpstreamFormatAnthropic
	case "anthropic_count_tokens":
		return upstreamFormat == UpstreamFormatAnthropic
	case "gemini_generate_content":
		return upstreamFormat == UpstreamFormatGemini
	case "openai_images_generations", "openai_media_generations", "openai_audio_transcriptions", "openai_audio_translations", "openai_audio_speech", "realtime", "aster_jobs":
		return upstreamFormat == UpstreamFormatNativeMedia
	default:
		return false
	}
}

type routingPolicyPlacement struct {
	batch    int
	name     string
	position int
}

func routingPolicyPlacementByAccount(policy *RoutingPolicy) map[string]routingPolicyPlacement {
	out := map[string]routingPolicyPlacement{}
	if policy == nil {
		return out
	}
	for batchIndex, batch := range policy.Strategy.ResourceBatches {
		for position, accountID := range batch.ProviderAccountIDs {
			if _, exists := out[accountID]; !exists {
				out[accountID] = routingPolicyPlacement{batch: batchIndex, name: batch.Name, position: position}
			}
		}
	}
	return out
}

func effectiveCircuitState(account ProviderAccount, now time.Time) (state string, probe bool, eligible bool) {
	state = account.CircuitState
	if state == "" {
		state = CircuitStateClosed
	}
	switch state {
	case CircuitStateClosed:
		return state, false, true
	case CircuitStateOpen:
		if account.CircuitOpenedUntil != nil && !now.Before(*account.CircuitOpenedUntil) {
			return CircuitStateHalfOpen, true, true
		}
		return state, false, false
	case CircuitStateHalfOpen:
		return state, true, true
	default:
		return state, false, false
	}
}
