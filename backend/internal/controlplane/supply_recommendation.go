package controlplane

import (
	"context"
	"sort"
	"strings"
)

const supplyRecommendationMinimumSamples = 20

type supplyRecommendationImpact struct {
	applications map[string]struct{}
	models       map[string]struct{}
	routeGroups  map[string]struct{}
}

func (s *Service) CapacityRecommendations(ctx context.Context, query SupplyUtilizationQuery) (CapacityRecommendationReport, error) {
	utilization, err := s.SupplyUtilization(ctx, query)
	if err != nil {
		return CapacityRecommendationReport{}, err
	}
	normalizedQuery, err := normalizeSupplyUtilizationQuery(query, s.nowUTC())
	if err != nil {
		return CapacityRecommendationReport{}, err
	}
	impacts, err := s.supplyRecommendationImpacts(ctx, normalizedQuery)
	if err != nil {
		return CapacityRecommendationReport{}, err
	}

	items := make([]CapacityRecommendation, 0)
	for _, row := range utilization.Rows {
		if row.Dimension != SupplyDimensionProviderAccount {
			continue
		}
		items = append(items, capacityRecommendationFromRow(row, utilization.Window, impacts[row.ID]))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Status != items[j].Status {
			return items[i].Status == CapacityRecommendationActionable
		}
		return strings.ToLower(items[i].TargetName) < strings.ToLower(items[j].TargetName)
	})
	summary := CapacityRecommendationSummary{Total: len(items)}
	for _, item := range items {
		if item.Status == CapacityRecommendationActionable {
			summary.Actionable++
		} else {
			summary.Inconclusive++
		}
	}
	return CapacityRecommendationReport{
		Mode: CapacityRecommendationObserveOnly, GeneratedAt: s.nowUTC(), Window: utilization.Window, Summary: summary, Items: items,
	}, nil
}

func (s *Service) supplyRecommendationImpacts(ctx context.Context, query SupplyUtilizationQuery) (map[string]supplyRecommendationImpact, error) {
	routes, err := s.repo.ListModelRoutes(ctx)
	if err != nil {
		return nil, err
	}
	models, err := s.repo.ListGatewayModels(ctx)
	if err != nil {
		return nil, err
	}
	traces, _, err := s.listSupplyTraces(ctx, query)
	if err != nil {
		return nil, err
	}
	modelNames := make(map[string]string, len(models))
	for _, model := range models {
		modelNames[model.ID] = model.ModelID
	}
	impacts := map[string]supplyRecommendationImpact{}
	ensure := func(accountID string) supplyRecommendationImpact {
		impact, found := impacts[accountID]
		if !found {
			impact = supplyRecommendationImpact{applications: map[string]struct{}{}, models: map[string]struct{}{}, routeGroups: map[string]struct{}{}}
		}
		return impact
	}
	for _, route := range routes {
		if route.ProviderAccountID == "" {
			continue
		}
		impact := ensure(route.ProviderAccountID)
		if modelName := modelNames[route.GatewayModelID]; modelName != "" {
			impact.models[modelName] = struct{}{}
		}
		group := strings.TrimSpace(route.RouteGroup)
		if group == "" {
			group = DefaultModelRouteGroup
		}
		impact.routeGroups[supplyRouteGroupID(route.GatewayModelID, group)] = struct{}{}
		impacts[route.ProviderAccountID] = impact
	}
	for _, trace := range traces {
		accountIDs := map[string]struct{}{}
		if trace.ProviderAccountID != "" {
			accountIDs[trace.ProviderAccountID] = struct{}{}
		}
		for _, attempt := range parseSupplyRouteAttempts(trace.RouteAttempts) {
			if attempt.AccountID != "" {
				accountIDs[attempt.AccountID] = struct{}{}
			}
		}
		appID := strings.TrimSpace(trace.GatewayPrincipalID)
		if appID == "" {
			appID = strings.TrimSpace(trace.APIKeyID)
		}
		for accountID := range accountIDs {
			impact := ensure(accountID)
			if appID != "" {
				impact.applications[appID] = struct{}{}
			}
			if trace.Model != "" {
				impact.models[trace.Model] = struct{}{}
			}
			if trace.RouteGroup != "" {
				impact.routeGroups[supplyRouteGroupID(trace.GatewayModelID, trace.RouteGroup)] = struct{}{}
			}
			impacts[accountID] = impact
		}
	}
	return impacts, nil
}

func capacityRecommendationFromRow(row SupplyUtilizationRow, window SupplyWindow, impact supplyRecommendationImpact) CapacityRecommendation {
	sampleCount := row.Evidence.TraceCount
	if row.Evidence.AttemptCount > sampleCount {
		sampleCount = row.Evidence.AttemptCount
	}
	constraint, peakWatermark := supplyPrimaryConstraint(row.Watermarks)
	item := CapacityRecommendation{
		Status: CapacityRecommendationInconclusive, Type: CapacityRecommendationIncrease,
		Target: SupplyEvidenceFilter{ProviderAccountID: row.ID}, TargetName: row.Name, PrimaryConstraint: constraint,
		Confidence: SupplyConfidenceLow, ReasonCodes: []string{}, CounterEvidence: []string{}, MissingEvidence: []string{},
		AffectedApplications: sortedSupplySet(impact.applications), AffectedModels: sortedSupplySet(impact.models), AffectedRouteGroups: sortedSupplySet(impact.routeGroups),
		Evidence: CapacityRecommendationEvidence{
			SampleCount: sampleCount, PeakWatermark: peakWatermark, CapacityRejectedRequests: row.Demand.CapacityRejected,
			PolicyRejectedRequests: row.Demand.PolicyRejected, UnclassifiedFailures: row.Demand.UnclassifiedFailures,
			FallbackRate: row.Demand.FallbackRate, SuccessRate: row.Demand.SuccessRate, HealthCoverage: row.Period.HealthCoverage,
			ObservedFrom: window.From, ObservedTo: window.To,
		},
		Rollback: "restore_previous_capacity",
	}

	if row.StrandedCapacity {
		item.ID = supplyRecommendationID(CapacityRecommendationReviewStranded, row)
		item.Status = CapacityRecommendationActionable
		item.Type = CapacityRecommendationReviewStranded
		item.PrimaryConstraint = CapacityConstraintRouting
		item.Confidence = SupplyConfidenceHigh
		item.ReasonCodes = append(item.ReasonCodes, row.StrandedReasons...)
		item.Rollback = "restore_route_configuration"
		return item
	}

	if window.Truncated {
		item.MissingEvidence = append(item.MissingEvidence, "window_truncated")
	}
	if sampleCount < supplyRecommendationMinimumSamples {
		item.MissingEvidence = append(item.MissingEvidence, "insufficient_samples")
	}
	if row.UnknownCapacity {
		item.MissingEvidence = append(item.MissingEvidence, "unknown_capacity")
	}
	if row.Demand.UnclassifiedFailures > 0 {
		item.MissingEvidence = append(item.MissingEvidence, "unclassified_failures")
	}
	if row.Demand.HTTP5xxRequests > 0 || row.Demand.AccountErrors > 0 {
		item.MissingEvidence = append(item.MissingEvidence, "provider_failures_require_classification")
	}
	if row.Period.HealthCoverage < 1 {
		item.MissingEvidence = append(item.MissingEvidence, "health_evidence_incomplete")
	}
	if row.Demand.PolicyRejected > 0 && row.Demand.CapacityRejected == 0 {
		item.MissingEvidence = append(item.MissingEvidence, "policy_limit_is_primary")
	}

	if row.Demand.FallbackRate > 0 {
		item.CounterEvidence = append(item.CounterEvidence, "fallback_capacity_observed")
	}
	if peakWatermark < 0.7 {
		item.CounterEvidence = append(item.CounterEvidence, "peak_below_expansion_threshold")
	}
	if row.Demand.CapacityRejected == 0 {
		item.CounterEvidence = append(item.CounterEvidence, "no_capacity_rejection_observed")
	}
	if row.Period.HealthCoverage == 1 {
		item.CounterEvidence = append(item.CounterEvidence, "health_coverage_complete")
	}

	if len(item.MissingEvidence) > 0 {
		item.ID = supplyRecommendationID(CapacityRecommendationIncrease, row)
		item.ReasonCodes = append(item.ReasonCodes, "evidence_gate_not_met")
		return item
	}

	switch {
	case row.Demand.CapacityRejected > 0 || peakWatermark >= 0.9:
		item.Type = CapacityRecommendationIncrease
		item.Status = CapacityRecommendationActionable
		item.ReasonCodes = append(item.ReasonCodes, "sustained_capacity_pressure")
		if row.Demand.CapacityRejected > 0 {
			item.ReasonCodes = append(item.ReasonCodes, "capacity_rejection_observed")
		}
	case peakWatermark < 0.65 && row.Demand.CapacityRejected == 0:
		item.Type = CapacityRecommendationDefer
		item.Status = CapacityRecommendationActionable
		item.ReasonCodes = append(item.ReasonCodes, "headroom_observed", "no_capacity_rejection_observed")
		item.Rollback = "no_change_required"
	default:
		item.Type = CapacityRecommendationIncrease
		item.ReasonCodes = append(item.ReasonCodes, "no_stable_capacity_signal")
		item.MissingEvidence = append(item.MissingEvidence, "additional_observation_window_required")
	}
	item.ID = supplyRecommendationID(item.Type, row)
	if item.Status == CapacityRecommendationActionable {
		item.Confidence = SupplyConfidenceMedium
		if sampleCount >= 100 && row.Tokens.NormalizationGaps == 0 {
			item.Confidence = SupplyConfidenceHigh
		}
	}
	return item
}

func sortedSupplySet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
