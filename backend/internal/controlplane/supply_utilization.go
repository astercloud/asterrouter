package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	supplyDefaultWindow   = 24 * time.Hour
	supplyMaximumWindow   = 31 * 24 * time.Hour
	supplyFactPageSize    = 500
	supplyMaximumFactRows = 10_000
	supplyHealthFreshness = time.Hour
)

type supplyRouteAttempt struct {
	AccountID  string `json:"account_id"`
	ProviderID string `json:"provider_id"`
	RouteID    string `json:"route_id"`
	RouteGroup string `json:"route_group"`
	Outcome    string `json:"outcome"`
	Detail     string `json:"detail"`
}

type supplyInterval struct {
	start time.Time
	end   time.Time
}

type supplyCostBuilder struct {
	costMicros     int64
	pricedRequests int
}

type supplyRowBuilder struct {
	row                SupplyUtilizationRow
	requestMinutes     map[time.Time]int
	tokenMinutes       map[time.Time]int64
	activeHours        map[time.Time]struct{}
	intervals          []supplyInterval
	costs              map[string]*supplyCostBuilder
	expectedHealth     map[string]struct{}
	freshHealth        map[string]struct{}
	associatedAccounts map[string]struct{}
	activeAccounts     map[string]struct{}
	associatedModels   map[string]struct{}
	associatedApps     map[string]struct{}
	associatedGroups   map[string]struct{}
	account            *ProviderAccount
	capacitySnapshot   ProviderCapacitySnapshot
	capacitySnapshotOK bool
}

func newSupplyRowBuilder(dimension, id, name string) *supplyRowBuilder {
	return &supplyRowBuilder{
		row: SupplyUtilizationRow{
			Dimension: dimension, ID: id, Name: name, CapacityStatus: SupplyCapacityNoEvidence,
			PrimaryConstraint: CapacityConstraintUnknown, StrandedReasons: []string{}, Costs: []SupplyCostTotal{},
			Tokens: SupplyTokenMetrics{ReasoningStatus: SupplyEvidenceUnknown},
			Watermarks: SupplyWatermarks{
				RPM:         SupplyWatermark{Status: SupplyEvidenceNotComparable, Source: "shared_supply"},
				TPM:         SupplyWatermark{Status: SupplyEvidenceNotComparable, Source: "shared_supply"},
				Concurrency: SupplyWatermark{Status: SupplyEvidenceNotComparable, Source: "shared_supply"},
			},
			Evidence: SupplyEvidenceSummary{Sources: []string{}, Complete: true},
		},
		requestMinutes: map[time.Time]int{}, tokenMinutes: map[time.Time]int64{}, activeHours: map[time.Time]struct{}{},
		costs: map[string]*supplyCostBuilder{}, expectedHealth: map[string]struct{}{}, freshHealth: map[string]struct{}{},
		associatedAccounts: map[string]struct{}{}, activeAccounts: map[string]struct{}{}, associatedModels: map[string]struct{}{}, associatedApps: map[string]struct{}{}, associatedGroups: map[string]struct{}{},
	}
}

func (s *Service) SupplyUtilization(ctx context.Context, query SupplyUtilizationQuery) (SupplyUtilizationReport, error) {
	now := s.nowUTC()
	query, err := normalizeSupplyUtilizationQuery(query, now)
	if err != nil {
		return SupplyUtilizationReport{}, err
	}

	accounts, err := s.repo.ListProviderAccounts(ctx)
	if err != nil {
		return SupplyUtilizationReport{}, err
	}
	routes, err := s.repo.ListModelRoutes(ctx)
	if err != nil {
		return SupplyUtilizationReport{}, err
	}
	models, err := s.repo.ListGatewayModels(ctx)
	if err != nil {
		return SupplyUtilizationReport{}, err
	}
	keys, err := s.repo.ListAPIKeys(ctx)
	if err != nil {
		return SupplyUtilizationReport{}, err
	}
	principals, err := s.repo.ListGatewayPrincipals(ctx)
	if err != nil {
		return SupplyUtilizationReport{}, err
	}
	healthChecks, err := s.repo.ListLatestProviderAccountHealthChecks(ctx)
	if err != nil {
		return SupplyUtilizationReport{}, err
	}
	traces, tracesTruncated, err := s.listSupplyTraces(ctx, query)
	if err != nil {
		return SupplyUtilizationReport{}, err
	}
	usage, usageTruncated, err := s.listSupplyUsage(ctx, query)
	if err != nil {
		return SupplyUtilizationReport{}, err
	}

	projection := newSupplyProjection(ctx, query, accounts, routes, models, keys, principals, healthChecks, s.currentProviderCapacityStore(), now)
	for _, trace := range traces {
		projection.addTrace(trace)
	}
	for _, record := range usage {
		projection.addUsage(record)
	}
	truncated := tracesTruncated || usageTruncated
	return projection.report(traces, usage, truncated, now), nil
}

func normalizeSupplyUtilizationQuery(query SupplyUtilizationQuery, now time.Time) (SupplyUtilizationQuery, error) {
	if query.To.IsZero() {
		query.To = now
	} else {
		query.To = query.To.UTC()
	}
	if query.From.IsZero() {
		query.From = query.To.Add(-supplyDefaultWindow)
	} else {
		query.From = query.From.UTC()
	}
	duration := query.To.Sub(query.From)
	if duration <= 0 || duration > supplyMaximumWindow || query.To.After(now.Add(time.Minute)) {
		return SupplyUtilizationQuery{}, ErrSupplyWindowInvalid
	}
	query.ProfileScope = strings.TrimSpace(query.ProfileScope)
	query.APIKeyIDs = normalizedSupplyIDs(query.APIKeyIDs)
	return query, nil
}

func normalizedSupplyIDs(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func (s *Service) listSupplyTraces(ctx context.Context, query SupplyUtilizationQuery) ([]GatewayTrace, bool, error) {
	out := make([]GatewayTrace, 0, supplyFactPageSize)
	for offset := 0; offset < supplyMaximumFactRows; offset += supplyFactPageSize {
		page, err := s.repo.QueryGatewayTraces(ctx, GatewayTraceQuery{
			Limit: supplyFactPageSize, Offset: offset, APIKeyIDs: query.APIKeyIDs, ProfileScope: query.ProfileScope,
			CreatedFrom: query.From, CreatedTo: query.To,
		})
		if err != nil {
			return nil, false, err
		}
		out = append(out, page...)
		if len(page) < supplyFactPageSize {
			return out, false, nil
		}
	}
	return out, true, nil
}

func (s *Service) listSupplyUsage(ctx context.Context, query SupplyUtilizationQuery) ([]UsageRecord, bool, error) {
	out := make([]UsageRecord, 0, supplyFactPageSize)
	for offset := 0; offset < supplyMaximumFactRows; offset += supplyFactPageSize {
		page, err := s.repo.QueryUsageRecords(ctx, UsageQuery{
			Limit: supplyFactPageSize, Offset: offset, APIKeyIDs: query.APIKeyIDs, ProfileScope: query.ProfileScope,
			CreatedFrom: query.From, CreatedTo: query.To,
		})
		if err != nil {
			return nil, false, err
		}
		out = append(out, page...)
		if len(page) < supplyFactPageSize {
			return out, false, nil
		}
	}
	return out, true, nil
}

type supplyProjection struct {
	ctx                context.Context
	query              SupplyUtilizationQuery
	now                time.Time
	rows               map[string]*supplyRowBuilder
	accounts           map[string]ProviderAccount
	routes             map[string]ModelRoute
	models             map[string]GatewayModel
	modelIDsByPublicID map[string]string
	keys               map[string]APIKeyRecord
	principals         map[string]GatewayPrincipal
	health             map[string]ProviderAccountHealthCheck
	tracesByOperation  map[string]GatewayTrace
	tracesByAttempt    map[string]GatewayTrace
	capacityStore      ProviderCapacityStore
}

func newSupplyProjection(ctx context.Context, query SupplyUtilizationQuery, accounts []ProviderAccount, routes []ModelRoute, models []GatewayModel, keys []APIKeyRecord, principals []GatewayPrincipal, healthChecks []ProviderAccountHealthCheck, capacityStore ProviderCapacityStore, now time.Time) *supplyProjection {
	projection := &supplyProjection{
		ctx: ctx, query: query, now: now, rows: map[string]*supplyRowBuilder{}, accounts: map[string]ProviderAccount{}, routes: map[string]ModelRoute{},
		models: map[string]GatewayModel{}, modelIDsByPublicID: map[string]string{}, keys: map[string]APIKeyRecord{}, principals: map[string]GatewayPrincipal{},
		health: map[string]ProviderAccountHealthCheck{}, tracesByOperation: map[string]GatewayTrace{}, tracesByAttempt: map[string]GatewayTrace{}, capacityStore: capacityStore,
	}
	for _, account := range accounts {
		projection.accounts[account.ID] = account
		row := projection.ensureRow(SupplyDimensionProviderAccount, account.ID, account.Name)
		row.row.ProviderID = account.ProviderID
		copy := account
		row.account = &copy
		row.associatedAccounts[account.ID] = struct{}{}
		row.expectedHealth[account.ID] = struct{}{}
	}
	for _, model := range models {
		projection.models[model.ID] = model
		projection.modelIDsByPublicID[model.ModelID] = model.ID
		row := projection.ensureRow(SupplyDimensionPublishedModel, model.ID, model.Name)
		row.row.GatewayModelID = model.ID
		row.associatedModels[model.ModelID] = struct{}{}
	}
	for _, route := range routes {
		projection.routes[route.ID] = route
		model := projection.models[route.GatewayModelID]
		group := strings.TrimSpace(route.RouteGroup)
		if group == "" {
			group = DefaultModelRouteGroup
		}
		name := strings.TrimSpace(model.Name)
		if name == "" {
			name = strings.TrimSpace(model.ModelID)
		}
		row := projection.ensureRow(SupplyDimensionRouteGroup, supplyRouteGroupID(route.GatewayModelID, group), name+" / "+group)
		row.row.GatewayModelID = route.GatewayModelID
		row.row.RouteGroup = group
		row.associatedAccounts[route.ProviderAccountID] = struct{}{}
		if route.Status == ModelRouteStatusActive {
			row.activeAccounts[route.ProviderAccountID] = struct{}{}
		}
		row.associatedModels[model.ModelID] = struct{}{}
		row.associatedGroups[group] = struct{}{}
		if route.ProviderAccountID != "" {
			row.expectedHealth[route.ProviderAccountID] = struct{}{}
		}
		modelRow := projection.ensureRow(SupplyDimensionPublishedModel, route.GatewayModelID, name)
		modelRow.associatedAccounts[route.ProviderAccountID] = struct{}{}
		if route.Status == ModelRouteStatusActive {
			modelRow.activeAccounts[route.ProviderAccountID] = struct{}{}
		}
		modelRow.associatedGroups[group] = struct{}{}
		if route.ProviderAccountID != "" {
			modelRow.expectedHealth[route.ProviderAccountID] = struct{}{}
		}
	}
	for _, principal := range principals {
		projection.principals[principal.ID] = principal
	}
	allowedKeys := make(map[string]struct{}, len(query.APIKeyIDs))
	for _, id := range query.APIKeyIDs {
		allowedKeys[id] = struct{}{}
	}
	for _, key := range keys {
		projection.keys[key.ID] = key
		if len(allowedKeys) > 0 {
			if _, visible := allowedKeys[key.ID]; !visible {
				continue
			}
		}
		appID, appName := projection.applicationIdentity(key.ID, key.GatewayPrincipalID, "")
		row := projection.ensureRow(SupplyDimensionApplication, appID, appName)
		if key.GatewayPrincipalID != "" {
			row.row.GatewayPrincipalID = key.GatewayPrincipalID
		} else {
			row.row.APIKeyID = key.ID
		}
		row.associatedApps[appID] = struct{}{}
	}
	for _, check := range healthChecks {
		projection.health[check.AccountID] = check
		if check.Status == "ok" && now.Sub(check.CheckedAt) <= supplyHealthFreshness {
			for _, row := range projection.rows {
				if _, expected := row.expectedHealth[check.AccountID]; expected {
					row.freshHealth[check.AccountID] = struct{}{}
				}
			}
		}
	}
	return projection
}

func (p *supplyProjection) ensureRow(dimension, id, name string) *supplyRowBuilder {
	key := dimension + "\x00" + id
	if row, found := p.rows[key]; found {
		if row.row.Name == "" && strings.TrimSpace(name) != "" {
			row.row.Name = strings.TrimSpace(name)
		}
		return row
	}
	if strings.TrimSpace(name) == "" {
		name = id
	}
	row := newSupplyRowBuilder(dimension, id, name)
	p.rows[key] = row
	return row
}

func supplyRouteGroupID(modelID, group string) string {
	return strings.TrimSpace(modelID) + ":" + strings.TrimSpace(group)
}

func (p *supplyProjection) addTrace(trace GatewayTrace) {
	if trace.OperationID != "" {
		p.tracesByOperation[trace.OperationID] = trace
	}
	if trace.AttemptID != "" {
		p.tracesByAttempt[trace.AttemptID] = trace
	}
	attempts := parseSupplyRouteAttempts(trace.RouteAttempts)
	fallback := supplyTraceFallback(attempts)
	rows := p.traceRows(trace)
	for _, row := range rows {
		row.addTrace(trace, attempts, fallback)
		p.associateTraceDimensions(row, trace)
	}
	for _, attempt := range attempts {
		if attempt.AccountID == "" {
			continue
		}
		account := p.accounts[attempt.AccountID]
		row := p.ensureRow(SupplyDimensionProviderAccount, attempt.AccountID, account.Name)
		row.row.Evidence.AttemptCount++
		row.associatedModels[trace.Model] = struct{}{}
		if trace.RouteGroup != "" {
			row.associatedGroups[trace.RouteGroup] = struct{}{}
		}
		appID, _ := p.applicationIdentity(trace.APIKeyID, trace.GatewayPrincipalID, trace.GatewayPrincipalName)
		if appID != "" {
			row.associatedApps[appID] = struct{}{}
		}
		row.addAttemptEvidence(attempt)
	}
}

func (p *supplyProjection) traceRows(trace GatewayTrace) []*supplyRowBuilder {
	rows := make([]*supplyRowBuilder, 0, 4)
	seen := map[string]struct{}{}
	appendRow := func(row *supplyRowBuilder) {
		if row == nil {
			return
		}
		key := row.row.Dimension + "\x00" + row.row.ID
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		rows = append(rows, row)
	}
	if trace.ProviderAccountID != "" {
		account := p.accounts[trace.ProviderAccountID]
		appendRow(p.ensureRow(SupplyDimensionProviderAccount, trace.ProviderAccountID, account.Name))
	}
	modelID := strings.TrimSpace(trace.GatewayModelID)
	if modelID == "" {
		modelID = p.modelIDsByPublicID[trace.Model]
	}
	if modelID == "" {
		modelID = trace.Model
	}
	if modelID != "" {
		model := p.models[modelID]
		name := model.Name
		if name == "" {
			name = trace.Model
		}
		modelRow := p.ensureRow(SupplyDimensionPublishedModel, modelID, name)
		modelRow.row.GatewayModelID = modelID
		appendRow(modelRow)
		if trace.RouteGroup != "" {
			groupRow := p.ensureRow(SupplyDimensionRouteGroup, supplyRouteGroupID(modelID, trace.RouteGroup), name+" / "+trace.RouteGroup)
			groupRow.row.GatewayModelID = modelID
			groupRow.row.RouteGroup = trace.RouteGroup
			appendRow(groupRow)
		}
	}
	appID, appName := p.applicationIdentity(trace.APIKeyID, trace.GatewayPrincipalID, trace.GatewayPrincipalName)
	if appID != "" {
		appRow := p.ensureRow(SupplyDimensionApplication, appID, appName)
		if trace.GatewayPrincipalID != "" {
			appRow.row.GatewayPrincipalID = trace.GatewayPrincipalID
		} else {
			appRow.row.APIKeyID = trace.APIKeyID
		}
		appendRow(appRow)
	}
	return rows
}

func (p *supplyProjection) associateTraceDimensions(row *supplyRowBuilder, trace GatewayTrace) {
	if trace.ProviderAccountID != "" {
		row.associatedAccounts[trace.ProviderAccountID] = struct{}{}
		row.expectedHealth[trace.ProviderAccountID] = struct{}{}
		if check, exists := p.health[trace.ProviderAccountID]; exists && check.Status == "ok" && p.now.Sub(check.CheckedAt) <= supplyHealthFreshness {
			row.freshHealth[trace.ProviderAccountID] = struct{}{}
		}
	}
	if trace.Model != "" {
		row.associatedModels[trace.Model] = struct{}{}
	}
	if trace.RouteGroup != "" {
		row.associatedGroups[trace.RouteGroup] = struct{}{}
	}
	appID, _ := p.applicationIdentity(trace.APIKeyID, trace.GatewayPrincipalID, trace.GatewayPrincipalName)
	if appID != "" {
		row.associatedApps[appID] = struct{}{}
	}
}

func (p *supplyProjection) applicationIdentity(apiKeyID, principalID, principalName string) (string, string) {
	principalID = strings.TrimSpace(principalID)
	if principalID != "" {
		name := strings.TrimSpace(principalName)
		if principal, found := p.principals[principalID]; found && strings.TrimSpace(principal.Name) != "" {
			name = principal.Name
		}
		if name == "" {
			name = principalID
		}
		return principalID, name
	}
	apiKeyID = strings.TrimSpace(apiKeyID)
	if apiKeyID == "" {
		return "", ""
	}
	name := apiKeyID
	if key, found := p.keys[apiKeyID]; found && strings.TrimSpace(key.Name) != "" {
		name = key.Name
	}
	return apiKeyID, name
}

func (row *supplyRowBuilder) addTrace(trace GatewayTrace, attempts []supplyRouteAttempt, fallback bool) {
	row.row.Evidence.TraceCount++
	row.row.Demand.Requests++
	minute := trace.CreatedAt.UTC().Truncate(time.Minute)
	row.requestMinutes[minute]++
	row.activeHours[trace.CreatedAt.UTC().Truncate(time.Hour)] = struct{}{}
	latency := time.Duration(trace.LatencyMS) * time.Millisecond
	start := trace.CreatedAt.Add(-latency)
	if latency <= 0 {
		start = trace.CreatedAt
	}
	row.intervals = append(row.intervals, supplyInterval{start: start, end: trace.CreatedAt})
	if supplyTraceSuccess(trace) {
		row.row.Demand.SuccessfulRequests++
	} else {
		row.row.Demand.RejectedRequests++
	}
	if trace.HTTPStatus == 429 {
		row.row.Demand.HTTP429Requests++
	}
	if trace.HTTPStatus >= 500 {
		row.row.Demand.HTTP5xxRequests++
	}
	if fallback {
		row.row.Demand.FallbackRequests++
	}
	classification := classifySupplyTrace(trace, attempts)
	if classification.noCandidate {
		row.row.Demand.NoCandidateRequests++
	}
	if classification.capacity {
		row.row.Demand.CapacityRejected++
	}
	if classification.policy {
		row.row.Demand.PolicyRejected++
	}
	if classification.account {
		row.row.Demand.AccountErrors++
	}
	if classification.protocol {
		row.row.Demand.ProtocolIncompatible++
	}
	if !supplyTraceSuccess(trace) && !classification.any() {
		row.row.Demand.UnclassifiedFailures++
	}
}

func (row *supplyRowBuilder) addAttemptEvidence(attempt supplyRouteAttempt) {
	detail := strings.ToLower(attempt.Detail)
	if strings.Contains(detail, "http status 429") {
		row.row.Demand.HTTP429Requests++
	}
	if supplyDetailHas5xx(detail) {
		row.row.Demand.HTTP5xxRequests++
	}
	if supplyCapacityEvidence(detail) {
		row.row.Demand.CapacityRejected++
	}
	if supplyProtocolEvidence(detail) {
		row.row.Demand.ProtocolIncompatible++
	}
	if attempt.Outcome == "failed" && !supplyCapacityEvidence(detail) && !supplyProtocolEvidence(detail) {
		row.row.Demand.AccountErrors++
	}
}

type supplyTraceClassification struct {
	noCandidate bool
	capacity    bool
	policy      bool
	account     bool
	protocol    bool
}

func (classification supplyTraceClassification) any() bool {
	return classification.noCandidate || classification.capacity || classification.policy || classification.account || classification.protocol
}

func classifySupplyTrace(trace GatewayTrace, attempts []supplyRouteAttempt) supplyTraceClassification {
	text := strings.ToLower(strings.Join([]string{trace.Status, trace.ErrorType, trace.RouteReason, trace.ResponseSummary}, " "))
	classification := supplyTraceClassification{
		noCandidate: trace.Status == "route_unavailable" || strings.Contains(text, "no schedulable") || strings.Contains(text, "no candidate"),
		capacity:    strings.Contains(text, "capacity") || strings.Contains(text, "rate_limit") || strings.Contains(text, "quota"),
		policy:      strings.Contains(text, "policy") || strings.Contains(text, "budget"),
		account:     trace.HTTPStatus == 401 || trace.HTTPStatus == 403 || strings.Contains(text, "account_error") || strings.Contains(text, "provider_auth"),
		protocol:    strings.Contains(text, "protocol_incompatible") || strings.Contains(text, "unsupported_feature") || strings.Contains(text, "capability_incompatible"),
	}
	for _, attempt := range attempts {
		detail := strings.ToLower(attempt.Detail)
		classification.capacity = classification.capacity || supplyCapacityEvidence(detail)
		classification.protocol = classification.protocol || supplyProtocolEvidence(detail)
		classification.account = classification.account || (attempt.Outcome == "failed" && !supplyCapacityEvidence(detail) && !supplyProtocolEvidence(detail))
	}
	return classification
}

func supplyCapacityEvidence(value string) bool {
	return strings.Contains(value, "at_capacity") || strings.Contains(value, "concurrency_exhausted") || strings.Contains(value, "rpm_exhausted") ||
		strings.Contains(value, "tpm_exhausted") || strings.Contains(value, "capacity_store") || strings.Contains(value, "capacity_exhausted")
}

func supplyProtocolEvidence(value string) bool {
	return strings.Contains(value, "protocol_incompatible") || strings.Contains(value, "capability_incompatible") || strings.Contains(value, "unsupported_feature")
}

func supplyDetailHas5xx(value string) bool {
	for status := 500; status <= 599; status++ {
		if strings.Contains(value, "http status "+strconv.Itoa(status)) {
			return true
		}
	}
	return false
}

func parseSupplyRouteAttempts(value string) []supplyRouteAttempt {
	var attempts []supplyRouteAttempt
	if err := json.Unmarshal([]byte(strings.TrimSpace(value)), &attempts); err != nil {
		return []supplyRouteAttempt{}
	}
	return attempts
}

func supplyTraceFallback(attempts []supplyRouteAttempt) bool {
	failed := false
	for _, attempt := range attempts {
		if attempt.Outcome == "failed" || attempt.Outcome == "skipped" {
			failed = true
		}
		if failed && attempt.Outcome == "selected" {
			return true
		}
	}
	return false
}

func supplyTraceSuccess(trace GatewayTrace) bool {
	if strings.TrimSpace(trace.ErrorType) != "" {
		return false
	}
	if trace.HTTPStatus >= 200 && trace.HTTPStatus < 400 {
		return true
	}
	switch trace.Status {
	case "forwarded", "completed", "succeeded":
		return true
	default:
		return false
	}
}

func (p *supplyProjection) addUsage(record UsageRecord) {
	rows := p.usageRows(record)
	for _, row := range rows {
		row.addUsage(record)
		if record.ProviderAccountID != "" {
			row.associatedAccounts[record.ProviderAccountID] = struct{}{}
		}
		if record.Model != "" {
			row.associatedModels[record.Model] = struct{}{}
		}
		appID, _ := p.applicationIdentity(record.APIKeyID, record.GatewayPrincipalID, record.GatewayPrincipalName)
		if appID != "" {
			row.associatedApps[appID] = struct{}{}
		}
	}
}

func (p *supplyProjection) usageRows(record UsageRecord) []*supplyRowBuilder {
	rows := make([]*supplyRowBuilder, 0, 4)
	seen := map[string]struct{}{}
	appendRow := func(row *supplyRowBuilder) {
		if row == nil {
			return
		}
		key := row.row.Dimension + "\x00" + row.row.ID
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		rows = append(rows, row)
	}
	if record.ProviderAccountID != "" {
		account := p.accounts[record.ProviderAccountID]
		appendRow(p.ensureRow(SupplyDimensionProviderAccount, record.ProviderAccountID, account.Name))
	}
	modelID := p.modelIDsByPublicID[record.Model]
	if modelID == "" {
		modelID = record.Model
	}
	if modelID != "" {
		model := p.models[modelID]
		name := model.Name
		if name == "" {
			name = record.Model
		}
		appendRow(p.ensureRow(SupplyDimensionPublishedModel, modelID, name))
	}
	appID, appName := p.applicationIdentity(record.APIKeyID, record.GatewayPrincipalID, record.GatewayPrincipalName)
	if appID != "" {
		row := p.ensureRow(SupplyDimensionApplication, appID, appName)
		if record.GatewayPrincipalID != "" {
			row.row.GatewayPrincipalID = record.GatewayPrincipalID
		} else {
			row.row.APIKeyID = record.APIKeyID
		}
		appendRow(row)
	}
	trace, found := p.tracesByOperation[record.OperationID]
	if !found {
		trace, found = p.tracesByAttempt[record.AttemptID]
	}
	if found && trace.RouteGroup != "" {
		gatewayModelID := trace.GatewayModelID
		if gatewayModelID == "" {
			gatewayModelID = p.modelIDsByPublicID[record.Model]
		}
		model := p.models[gatewayModelID]
		name := model.Name
		if name == "" {
			name = record.Model
		}
		row := p.ensureRow(SupplyDimensionRouteGroup, supplyRouteGroupID(gatewayModelID, trace.RouteGroup), name+" / "+trace.RouteGroup)
		row.row.GatewayModelID = gatewayModelID
		row.row.RouteGroup = trace.RouteGroup
		appendRow(row)
	}
	return rows
}

func (row *supplyRowBuilder) addUsage(record UsageRecord) {
	row.row.Evidence.UsageRecordCount++
	inputTokens := int64(record.InputTokens)
	if record.TotalInputTokens != nil && *record.TotalInputTokens >= 0 {
		inputTokens = int64(*record.TotalInputTokens)
	}
	row.row.Tokens.InputTokens += inputTokens
	row.row.Tokens.OutputTokens += int64(record.OutputTokens)
	if record.CacheReadTokens != nil {
		row.row.Tokens.CacheReadTokens += int64(*record.CacheReadTokens)
	}
	if record.CacheWrite5mTokens != nil {
		row.row.Tokens.CacheWriteTokens += int64(*record.CacheWrite5mTokens)
	}
	if record.CacheWrite1hTokens != nil {
		row.row.Tokens.CacheWriteTokens += int64(*record.CacheWrite1hTokens)
	}
	if supplyUsageHasNormalizationGap(record.UsageNormalizationStatus) {
		row.row.Tokens.NormalizationGaps++
	}
	minute := record.CreatedAt.UTC().Truncate(time.Minute)
	row.tokenMinutes[minute] += inputTokens + int64(record.OutputTokens)
	row.activeHours[record.CreatedAt.UTC().Truncate(time.Hour)] = struct{}{}
	if record.ProcurementCostMicros != nil {
		currency := strings.ToUpper(strings.TrimSpace(record.ProcurementCostCurrency))
		if currency == "" {
			currency = "UNKNOWN"
		}
		cost := row.costs[currency]
		if cost == nil {
			cost = &supplyCostBuilder{}
			row.costs[currency] = cost
		}
		cost.costMicros += *record.ProcurementCostMicros
		cost.pricedRequests++
	} else {
		row.row.UnpricedRequests++
	}
}

func supplyUsageHasNormalizationGap(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return status == "" || status == "unknown" || strings.Contains(status, "missing") || strings.Contains(status, "invalid") || strings.Contains(status, "partial")
}

func (p *supplyProjection) report(traces []GatewayTrace, usage []UsageRecord, truncated bool, now time.Time) SupplyUtilizationReport {
	rows := make([]SupplyUtilizationRow, 0, len(p.rows))
	for _, builder := range p.rows {
		p.finalizeRow(builder, truncated)
		rows = append(rows, builder.row)
	}
	sort.Slice(rows, func(i, j int) bool {
		left, right := supplyDimensionOrder(rows[i].Dimension), supplyDimensionOrder(rows[j].Dimension)
		if left != right {
			return left < right
		}
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	byDimension := map[string]int{}
	for _, row := range rows {
		byDimension[row.Dimension]++
	}
	return SupplyUtilizationReport{
		Window:    SupplyWindow{From: p.query.From, To: p.query.To, DurationSeconds: int64(p.query.To.Sub(p.query.From).Seconds()), TraceCount: len(traces), UsageRecordCount: len(usage), Truncated: truncated},
		Freshness: SupplyEvidenceFreshness{TraceAsOf: latestSupplyTraceAt(traces), UsageAsOf: latestSupplyUsageAt(usage), HealthAsOf: latestSupplyHealthAt(p.health), CapacityAsOf: now},
		Rows:      rows, ByDimension: byDimension,
	}
}

func (p *supplyProjection) finalizeRow(builder *supplyRowBuilder, truncated bool) {
	row := &builder.row
	if row.Demand.Requests > 0 {
		row.Demand.SuccessRate = supplyRatio(row.Demand.SuccessfulRequests, row.Demand.Requests)
		row.Demand.FallbackRate = supplyRatio(row.Demand.FallbackRequests, row.Demand.Requests)
	}
	row.Concurrency = supplyConcurrency(builder.intervals)
	if builder.account != nil {
		store := p.currentCapacityStore()
		if store != nil {
			if snapshot, err := store.Snapshot(p.ctx, builder.account.ID); err == nil {
				builder.capacitySnapshot, builder.capacitySnapshotOK = snapshot, true
				row.Concurrency.Current = snapshot.CapacityUnits
			}
		}
		row.Watermarks = supplyAccountWatermarks(*builder.account, builder.capacitySnapshot, builder.capacitySnapshotOK, builder.requestMinutes, builder.tokenMinutes, row.Concurrency.Peak)
	} else {
		row.Watermarks.RPM.Peak = int64(supplyPeakMinute(builder.requestMinutes))
		row.Watermarks.TPM.Peak = supplyPeakTokenMinute(builder.tokenMinutes)
		row.Watermarks.Concurrency.Peak = int64(row.Concurrency.Peak)
	}
	row.Period = supplyPeriodMetrics(builder, p.query, p.now)
	row.Costs = supplyCosts(builder.costs)
	row.Evidence.Sources = supplyEvidenceSources(builder)
	row.Evidence.Complete = !truncated
	row.Evidence.Filter = SupplyEvidenceFilter{
		APIKeyID: row.APIKeyID, GatewayPrincipalID: row.GatewayPrincipalID, Model: firstSupplySetValue(builder.associatedModels), ProviderAccountID: supplyEvidenceAccountID(row, builder),
		RouteGroup: row.RouteGroup, GatewayModelID: row.GatewayModelID,
	}
	p.finalizeCapacityStatus(builder)
}

func (p *supplyProjection) currentCapacityStore() ProviderCapacityStore {
	return p.capacityStore
}

func (p *supplyProjection) finalizeCapacityStatus(builder *supplyRowBuilder) {
	row := &builder.row
	strandedReasons := p.strandedReasons(builder)
	row.StrandedReasons = strandedReasons
	row.StrandedCapacity = len(strandedReasons) > 0
	row.UnknownCapacity = builder.account == nil || (builder.account.Concurrency <= 0 && builder.account.RPMLimit <= 0 && builder.account.TPMLimit <= 0)
	peakConstraint, peakRatio := supplyPrimaryConstraint(row.Watermarks)
	row.PrimaryConstraint = peakConstraint
	switch {
	case row.StrandedCapacity:
		row.CapacityStatus = SupplyCapacityStranded
		row.PrimaryConstraint = CapacityConstraintRouting
	case row.Demand.CapacityRejected > 0 || peakRatio >= 0.9:
		row.CapacityStatus = SupplyCapacitySaturated
	case row.Demand.HTTP429Requests > 0 || row.Demand.HTTP5xxRequests > 0 || row.Demand.AccountErrors > 0 || (row.Period.HealthCoverage > 0 && row.Period.HealthCoverage < 1):
		row.CapacityStatus = SupplyCapacityDegraded
	case row.UnknownCapacity:
		row.CapacityStatus = SupplyCapacityUnknown
		row.PrimaryConstraint = CapacityConstraintUnknown
	case row.Demand.Requests == 0:
		row.CapacityStatus = SupplyCapacityIdle
	default:
		row.CapacityStatus = SupplyCapacityAvailable
	}
}

func (p *supplyProjection) strandedReasons(builder *supplyRowBuilder) []string {
	if builder.account != nil {
		return p.accountStrandedReasons(*builder.account)
	}
	if builder.row.Dimension != SupplyDimensionPublishedModel && builder.row.Dimension != SupplyDimensionRouteGroup {
		return []string{}
	}
	if len(builder.associatedAccounts) == 0 {
		return []string{"no_configured_route"}
	}
	if len(builder.activeAccounts) == 0 {
		return []string{"no_active_route"}
	}
	usable := false
	for accountID := range builder.activeAccounts {
		account, found := p.accounts[accountID]
		if !found || len(p.accountStrandedReasons(account)) == 0 {
			usable = true
			break
		}
	}
	if usable {
		return []string{}
	}
	return []string{"all_route_capacity_stranded"}
}

func (p *supplyProjection) accountStrandedReasons(account ProviderAccount) []string {
	reasons := make([]string, 0, 5)
	if account.Status != AccountStatusActive {
		reasons = append(reasons, "account_inactive")
	}
	if !account.Schedulable {
		reasons = append(reasons, "account_unschedulable")
	}
	if account.CooldownUntil != nil && account.CooldownUntil.After(p.now) {
		reasons = append(reasons, "cooldown_active")
	}
	if account.CircuitState == CircuitStateOpen || (account.CircuitOpenedUntil != nil && account.CircuitOpenedUntil.After(p.now)) {
		reasons = append(reasons, "circuit_open")
	}
	activeRoute := false
	for _, route := range p.routes {
		if route.ProviderAccountID == account.ID && route.Status == ModelRouteStatusActive {
			activeRoute = true
			break
		}
	}
	if !activeRoute {
		reasons = append(reasons, "no_active_route")
	}
	return reasons
}

func supplyAccountWatermarks(account ProviderAccount, snapshot ProviderCapacitySnapshot, snapshotOK bool, requestMinutes map[time.Time]int, tokenMinutes map[time.Time]int64, concurrencyPeak int) SupplyWatermarks {
	currentRPM, currentTPM, currentConcurrency := int64(0), int64(0), int64(0)
	if snapshotOK {
		currentRPM, currentTPM, currentConcurrency = int64(snapshot.Requests), int64(snapshot.Tokens), int64(snapshot.CapacityUnits)
	}
	return SupplyWatermarks{
		RPM:         supplyKnownWatermark(int64(account.RPMLimit), currentRPM, int64(supplyPeakMinute(requestMinutes)), snapshotOK, "provider_account_config"),
		TPM:         supplyKnownWatermark(int64(account.TPMLimit), currentTPM, supplyPeakTokenMinute(tokenMinutes), snapshotOK, "provider_account_config"),
		Concurrency: supplyKnownWatermark(int64(account.Concurrency), currentConcurrency, int64(concurrencyPeak), snapshotOK, "provider_account_config"),
	}
}

func supplyKnownWatermark(limit, current, peak int64, snapshotOK bool, source string) SupplyWatermark {
	if limit <= 0 {
		return SupplyWatermark{Status: SupplyEvidenceUnknown, Source: source, Current: current, Peak: peak}
	}
	watermark := SupplyWatermark{Status: SupplyEvidenceKnown, Source: source, Limit: limit, Current: current, Peak: peak}
	if snapshotOK {
		watermark.CurrentRatio = float64(current) / float64(limit)
	}
	watermark.PeakRatio = float64(peak) / float64(limit)
	return watermark
}

func supplyConcurrency(intervals []supplyInterval) SupplyConcurrencyMetrics {
	if len(intervals) == 0 {
		return SupplyConcurrencyMetrics{}
	}
	type event struct {
		at    time.Time
		delta int
	}
	events := make([]event, 0, len(intervals)*2)
	for _, interval := range intervals {
		end := interval.end
		if !end.After(interval.start) {
			end = interval.start.Add(time.Nanosecond)
		}
		events = append(events, event{at: interval.start, delta: 1}, event{at: end, delta: -1})
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].at.Equal(events[j].at) {
			return events[i].delta < events[j].delta
		}
		return events[i].at.Before(events[j].at)
	})
	values := make([]int, 0, len(intervals))
	current, peak := 0, 0
	for _, currentEvent := range events {
		current += currentEvent.delta
		if currentEvent.delta > 0 {
			values = append(values, current)
			if current > peak {
				peak = current
			}
		}
	}
	sort.Ints(values)
	return SupplyConcurrencyMetrics{P50: supplyNearestRank(values, 0.50), P95: supplyNearestRank(values, 0.95), P99: supplyNearestRank(values, 0.99), Peak: peak}
}

func supplyNearestRank(values []int, ratio float64) int {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values))*ratio+0.999999) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func supplyPeakMinute(values map[time.Time]int) int {
	peak := 0
	for _, value := range values {
		if value > peak {
			peak = value
		}
	}
	return peak
}

func supplyPeakTokenMinute(values map[time.Time]int64) int64 {
	var peak int64
	for _, value := range values {
		if value > peak {
			peak = value
		}
	}
	return peak
}

func supplyPeriodMetrics(builder *supplyRowBuilder, query SupplyUtilizationQuery, now time.Time) SupplyPeriodMetrics {
	var peakMinute *time.Time
	peakCalls := 0
	for minute, calls := range builder.requestMinutes {
		if calls > peakCalls || (calls == peakCalls && (peakMinute == nil || minute.Before(*peakMinute))) {
			copy := minute
			peakMinute, peakCalls = &copy, calls
		}
	}
	windowHours := int(query.To.Sub(query.From).Hours())
	if windowHours < 1 {
		windowHours = 1
	}
	idleHours := windowHours - len(builder.activeHours)
	if idleHours < 0 {
		idleHours = 0
	}
	cooldownSeconds := int64(0)
	if builder.account != nil && builder.account.CooldownUntil != nil && builder.account.CooldownUntil.After(now) {
		cooldownSeconds = int64(builder.account.CooldownUntil.Sub(now).Seconds())
	}
	healthCoverage := float64(0)
	if len(builder.expectedHealth) > 0 {
		healthCoverage = float64(len(builder.freshHealth)) / float64(len(builder.expectedHealth))
	}
	return SupplyPeriodMetrics{PeakMinute: peakMinute, PeakMinuteCalls: peakCalls, IdleHours: idleHours, CooldownSeconds: cooldownSeconds, HealthCoverage: healthCoverage}
}

func supplyCosts(values map[string]*supplyCostBuilder) []SupplyCostTotal {
	out := make([]SupplyCostTotal, 0, len(values))
	for currency, value := range values {
		out = append(out, SupplyCostTotal{Currency: currency, CostMicros: value.costMicros, PricedRequests: value.pricedRequests})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })
	return out
}

func supplyEvidenceSources(builder *supplyRowBuilder) []string {
	sources := []string{"provider_account_config", "model_route_config"}
	if builder.row.Evidence.TraceCount > 0 {
		sources = append(sources, "gateway_trace")
	}
	if builder.row.Evidence.UsageRecordCount > 0 {
		sources = append(sources, "usage_record")
	}
	if len(builder.expectedHealth) > 0 {
		sources = append(sources, "provider_health")
	}
	if builder.account != nil {
		sources = append(sources, "runtime_capacity_snapshot")
	}
	return sources
}

func supplyEvidenceAccountID(row *SupplyUtilizationRow, builder *supplyRowBuilder) string {
	if row.Dimension == SupplyDimensionProviderAccount {
		return row.ID
	}
	if len(builder.associatedAccounts) == 1 {
		return firstSupplySetValue(builder.associatedAccounts)
	}
	return ""
}

func firstSupplySetValue(values map[string]struct{}) string {
	if len(values) != 1 {
		return ""
	}
	for value := range values {
		return value
	}
	return ""
}

func supplyPrimaryConstraint(watermarks SupplyWatermarks) (string, float64) {
	constraint, ratio := CapacityConstraintUnknown, float64(0)
	for _, candidate := range []struct {
		name      string
		watermark SupplyWatermark
	}{{CapacityConstraintConcurrency, watermarks.Concurrency}, {CapacityConstraintRPM, watermarks.RPM}, {CapacityConstraintTPM, watermarks.TPM}} {
		if candidate.watermark.Status == SupplyEvidenceKnown && candidate.watermark.PeakRatio >= ratio {
			constraint, ratio = candidate.name, candidate.watermark.PeakRatio
		}
	}
	return constraint, ratio
}

func supplyRatio(numerator, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func latestSupplyTraceAt(values []GatewayTrace) *time.Time {
	var latest *time.Time
	for _, value := range values {
		latest = laterSupplyTime(latest, value.CreatedAt)
	}
	return latest
}

func latestSupplyUsageAt(values []UsageRecord) *time.Time {
	var latest *time.Time
	for _, value := range values {
		latest = laterSupplyTime(latest, value.CreatedAt)
	}
	return latest
}

func latestSupplyHealthAt(values map[string]ProviderAccountHealthCheck) *time.Time {
	var latest *time.Time
	for _, value := range values {
		latest = laterSupplyTime(latest, value.CheckedAt)
	}
	return latest
}

func laterSupplyTime(current *time.Time, candidate time.Time) *time.Time {
	if candidate.IsZero() || (current != nil && !candidate.After(*current)) {
		return current
	}
	copy := candidate
	return &copy
}

func supplyDimensionOrder(value string) int {
	switch value {
	case SupplyDimensionProviderAccount:
		return 0
	case SupplyDimensionRouteGroup:
		return 1
	case SupplyDimensionPublishedModel:
		return 2
	case SupplyDimensionApplication:
		return 3
	default:
		return 4
	}
}

func supplyRecommendationID(kind string, row SupplyUtilizationRow) string {
	return fmt.Sprintf("%s:%s:%s", kind, row.Dimension, row.ID)
}
