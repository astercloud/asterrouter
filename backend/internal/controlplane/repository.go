package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

type Repository interface {
	ListProviders(ctx context.Context) ([]ProviderConnection, error)
	SaveProvider(ctx context.Context, provider ProviderConnection) error
	ListLatestProviderHealthChecks(ctx context.Context) ([]ProviderHealthCheck, error)
	SaveProviderHealthCheck(ctx context.Context, check ProviderHealthCheck) error
	ListProjects(ctx context.Context) ([]Project, error)
	SaveProject(ctx context.Context, project Project) error
	ListDepartments(ctx context.Context) ([]Department, error)
	SaveDepartment(ctx context.Context, department Department) error
	ListApplications(ctx context.Context, projectID string) ([]Application, error)
	SaveApplication(ctx context.Context, app Application) error
	ListWorkspaceUsers(ctx context.Context) ([]WorkspaceUser, error)
	SaveWorkspaceUser(ctx context.Context, user WorkspaceUser) error
	ListRoleBindings(ctx context.Context) ([]RoleBinding, error)
	SaveRoleBinding(ctx context.Context, binding RoleBinding) error
	DeleteRoleBinding(ctx context.Context, id string) error
	ListRoutingGroups(ctx context.Context) ([]RoutingGroup, error)
	SaveRoutingGroup(ctx context.Context, group RoutingGroup) error
	ListProviderAccounts(ctx context.Context) ([]ProviderAccount, error)
	SaveProviderAccount(ctx context.Context, account ProviderAccount) error
	ListLatestProviderAccountHealthChecks(ctx context.Context) ([]ProviderAccountHealthCheck, error)
	SaveProviderAccountHealthCheck(ctx context.Context, check ProviderAccountHealthCheck) error
	ListModelPricings(ctx context.Context) ([]ModelPricing, error)
	SaveModelPricing(ctx context.Context, pricing ModelPricing) error
	ListAPIKeys(ctx context.Context) ([]APIKeyRecord, error)
	FindAPIKeyByHash(ctx context.Context, hash string) (APIKeyRecord, bool, error)
	SaveAPIKey(ctx context.Context, key APIKeyRecord) error
	DisableAPIKey(ctx context.Context, id string, updatedAt time.Time) error
	UpdateAPIKeyLastUsed(ctx context.Context, id string, lastUsedAt time.Time) error
	SaveUsageRecord(ctx context.Context, record UsageRecord) error
	ListUsageRecords(ctx context.Context, limit int) ([]UsageRecord, error)
	QueryUsageRecords(ctx context.Context, query UsageQuery) ([]UsageRecord, error)
	SummarizeUsageRecords(ctx context.Context, query UsageQuery) (UsageAggregate, error)
	SummarizeCostAllocation(ctx context.Context, dimension string, query UsageQuery) ([]CostAllocationRollup, error)
	SumUsageTokensByAPIKeySince(ctx context.Context, apiKeyID string, since time.Time) (int, error)
	SumUsageCostCentsByProjectSince(ctx context.Context, projectID string, since time.Time) (int, error)
	SaveGatewayTrace(ctx context.Context, trace GatewayTrace) error
	ListGatewayTraces(ctx context.Context, limit int) ([]GatewayTrace, error)
	QueryGatewayTraces(ctx context.Context, query GatewayTraceQuery) ([]GatewayTrace, error)
	SummarizeGatewayTraces(ctx context.Context, query GatewayTraceQuery) (GatewayTraceSummary, error)
	ListAuditLogs(ctx context.Context, limit int) ([]AuditLog, error)
	QueryAuditLogs(ctx context.Context, query AuditLogQuery) ([]AuditLog, error)
	SummarizeAuditLogs(ctx context.Context, query AuditLogQuery) (AuditLogSummary, error)
	AddAuditLog(ctx context.Context, event AuditLog) error
	QueryAlertEvents(ctx context.Context, query AlertQuery) ([]AlertEvent, error)
	SummarizeAlertEvents(ctx context.Context, query AlertQuery) (AlertSummary, error)
	FindAlertEvent(ctx context.Context, id string) (AlertEvent, bool, error)
	FindAlertByDedupeKey(ctx context.Context, dedupeKey string) (AlertEvent, bool, error)
	SaveAlertEvent(ctx context.Context, event AlertEvent) error
	Health(ctx context.Context) error
	Close() error
}

func NewRepository(ctx context.Context, databaseURL string) (Repository, string, error) {
	if databaseURL == "" {
		return NewMemoryRepository(), "memory", nil
	}
	repo, err := NewPostgresRepository(ctx, databaseURL)
	if err != nil {
		return nil, "", err
	}
	return repo, "postgres", nil
}

type MemoryRepository struct {
	mu                  sync.RWMutex
	providers           map[string]ProviderConnection
	healthChecks        map[string]ProviderHealthCheck
	projects            map[string]Project
	departments         map[string]Department
	applications        map[string]Application
	workspaceUsers      map[string]WorkspaceUser
	roleBindings        map[string]RoleBinding
	groups              map[string]RoutingGroup
	accounts            map[string]ProviderAccount
	accountHealthChecks map[string]ProviderAccountHealthCheck
	modelPricings       map[string]ModelPricing
	apiKeys             map[string]APIKeyRecord
	usageRecords        map[string]UsageRecord
	gatewayTraces       map[string]GatewayTrace
	auditLogs           map[string]AuditLog
	alertEvents         map[string]AlertEvent
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		providers:           map[string]ProviderConnection{},
		healthChecks:        map[string]ProviderHealthCheck{},
		projects:            map[string]Project{},
		departments:         map[string]Department{},
		applications:        map[string]Application{},
		workspaceUsers:      map[string]WorkspaceUser{},
		roleBindings:        map[string]RoleBinding{},
		groups:              map[string]RoutingGroup{},
		accounts:            map[string]ProviderAccount{},
		accountHealthChecks: map[string]ProviderAccountHealthCheck{},
		modelPricings:       map[string]ModelPricing{},
		apiKeys:             map[string]APIKeyRecord{},
		usageRecords:        map[string]UsageRecord{},
		gatewayTraces:       map[string]GatewayTrace{},
		auditLogs:           map[string]AuditLog{},
		alertEvents:         map[string]AlertEvent{},
	}
}

func (r *MemoryRepository) ListProviders(context.Context) ([]ProviderConnection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderConnection, 0, len(r.providers))
	for _, provider := range r.providers {
		out = append(out, provider)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority == out[j].Priority {
			return out[i].Name < out[j].Name
		}
		return out[i].Priority < out[j].Priority
	})
	return out, nil
}

func (r *MemoryRepository) SaveProvider(_ context.Context, provider ProviderConnection) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[provider.ID] = provider
	return nil
}

func (r *MemoryRepository) ListLatestProviderHealthChecks(context.Context) ([]ProviderHealthCheck, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderHealthCheck, 0, len(r.healthChecks))
	for _, check := range r.healthChecks {
		out = append(out, check)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CheckedAt.After(out[j].CheckedAt) })
	return out, nil
}

func (r *MemoryRepository) SaveProviderHealthCheck(_ context.Context, check ProviderHealthCheck) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.healthChecks[check.ProviderID] = check
	return nil
}

func (r *MemoryRepository) ListProjects(context.Context) ([]Project, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Project, 0, len(r.projects))
	for _, project := range r.projects {
		out = append(out, project)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (r *MemoryRepository) SaveProject(_ context.Context, project Project) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projects[project.ID] = project
	return nil
}

func (r *MemoryRepository) ListApplications(_ context.Context, projectID string) ([]Application, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Application, 0, len(r.applications))
	for _, app := range r.applications {
		if projectID == "" || app.ProjectID == projectID {
			out = append(out, app)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (r *MemoryRepository) SaveApplication(_ context.Context, app Application) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applications[app.ID] = app
	return nil
}

func (r *MemoryRepository) ListRoutingGroups(context.Context) ([]RoutingGroup, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RoutingGroup, 0, len(r.groups))
	for _, group := range r.groups {
		group.AccountCount, group.ActiveAccounts = r.accountCountsForGroup(group.ID)
		out = append(out, group)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder == out[j].SortOrder {
			return out[i].Name < out[j].Name
		}
		return out[i].SortOrder < out[j].SortOrder
	})
	return out, nil
}

func (r *MemoryRepository) SaveRoutingGroup(_ context.Context, group RoutingGroup) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.groups[group.ID] = group
	return nil
}

func (r *MemoryRepository) ListProviderAccounts(context.Context) ([]ProviderAccount, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderAccount, 0, len(r.accounts))
	for _, account := range r.accounts {
		out = append(out, account)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority == out[j].Priority {
			return out[i].Name < out[j].Name
		}
		return out[i].Priority < out[j].Priority
	})
	return out, nil
}

func (r *MemoryRepository) SaveProviderAccount(_ context.Context, account ProviderAccount) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accounts[account.ID] = account
	return nil
}

func (r *MemoryRepository) ListLatestProviderAccountHealthChecks(context.Context) ([]ProviderAccountHealthCheck, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderAccountHealthCheck, 0, len(r.accountHealthChecks))
	for _, check := range r.accountHealthChecks {
		out = append(out, check)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CheckedAt.After(out[j].CheckedAt) })
	return out, nil
}

func (r *MemoryRepository) SaveProviderAccountHealthCheck(_ context.Context, check ProviderAccountHealthCheck) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accountHealthChecks[check.AccountID] = check
	return nil
}

func (r *MemoryRepository) ListModelPricings(context.Context) ([]ModelPricing, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ModelPricing, 0, len(r.modelPricings))
	for _, pricing := range r.modelPricings {
		out = append(out, pricing)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out, nil
}

func (r *MemoryRepository) SaveModelPricing(_ context.Context, pricing ModelPricing) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modelPricings[pricing.ID] = pricing
	return nil
}

func (r *MemoryRepository) accountCountsForGroup(groupID string) (int, int) {
	var total, active int
	for _, account := range r.accounts {
		if contains(account.GroupIDs, groupID) {
			total++
			if account.Status == AccountStatusActive && account.Schedulable {
				active++
			}
		}
	}
	return total, active
}

func (r *MemoryRepository) ListAPIKeys(context.Context) ([]APIKeyRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]APIKeyRecord, 0, len(r.apiKeys))
	for _, key := range r.apiKeys {
		out = append(out, key)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (r *MemoryRepository) FindAPIKeyByHash(_ context.Context, hash string) (APIKeyRecord, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, key := range r.apiKeys {
		if key.KeyHash == hash {
			return key, true, nil
		}
	}
	return APIKeyRecord{}, false, nil
}

func (r *MemoryRepository) SaveAPIKey(_ context.Context, key APIKeyRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.apiKeys[key.ID] = key
	return nil
}

func (r *MemoryRepository) UpdateAPIKeyLastUsed(_ context.Context, id string, lastUsedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.apiKeys[id]
	if !ok {
		return nil
	}
	key.LastUsedAt = &lastUsedAt
	key.UpdatedAt = lastUsedAt
	r.apiKeys[id] = key
	return nil
}

func (r *MemoryRepository) DisableAPIKey(_ context.Context, id string, updatedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.apiKeys[id]
	if !ok {
		return nil
	}
	key.Status = APIKeyStatusDisabled
	key.UpdatedAt = updatedAt
	r.apiKeys[id] = key
	return nil
}

func (r *MemoryRepository) SaveUsageRecord(_ context.Context, record UsageRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.usageRecords[record.ID] = record
	return nil
}

func (r *MemoryRepository) ListUsageRecords(_ context.Context, limit int) ([]UsageRecord, error) {
	return r.QueryUsageRecords(context.Background(), UsageQuery{Limit: limit})
}

func (r *MemoryRepository) QueryUsageRecords(_ context.Context, query UsageQuery) ([]UsageRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]UsageRecord, 0, len(r.usageRecords))
	for _, record := range r.usageRecords {
		if memoryUsageRecordMatches(record, query) {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 100, 500)
	if offset >= len(out) {
		return []UsageRecord{}, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (r *MemoryRepository) SummarizeUsageRecords(_ context.Context, query UsageQuery) (UsageAggregate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	records := make([]UsageRecord, 0, len(r.usageRecords))
	for _, record := range r.usageRecords {
		if memoryUsageRecordMatches(record, query) {
			records = append(records, record)
		}
	}
	return usageAggregateFromRecords(records), nil
}

func (r *MemoryRepository) SumUsageTokensByAPIKeySince(_ context.Context, apiKeyID string, since time.Time) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var total int
	for _, record := range r.usageRecords {
		if record.APIKeyID == apiKeyID && !record.CreatedAt.Before(since) {
			total += record.InputTokens + record.OutputTokens
		}
	}
	return total, nil
}

func (r *MemoryRepository) SumUsageCostCentsByProjectSince(_ context.Context, projectID string, since time.Time) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var total int
	for _, record := range r.usageRecords {
		if record.ProjectID == projectID && !record.CreatedAt.Before(since) {
			total += record.CostCents
		}
	}
	return total, nil
}

func (r *MemoryRepository) SaveGatewayTrace(_ context.Context, trace GatewayTrace) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gatewayTraces[trace.ID] = trace
	return nil
}

func (r *MemoryRepository) ListGatewayTraces(_ context.Context, limit int) ([]GatewayTrace, error) {
	return r.QueryGatewayTraces(context.Background(), GatewayTraceQuery{Limit: limit})
}

func (r *MemoryRepository) QueryGatewayTraces(_ context.Context, query GatewayTraceQuery) ([]GatewayTrace, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]GatewayTrace, 0, len(r.gatewayTraces))
	for _, trace := range r.gatewayTraces {
		if memoryGatewayTraceMatches(trace, query) {
			out = append(out, trace)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 100, 500)
	if offset >= len(out) {
		return []GatewayTrace{}, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (r *MemoryRepository) SummarizeGatewayTraces(_ context.Context, query GatewayTraceQuery) (GatewayTraceSummary, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var summary GatewayTraceSummary
	var latencyTotal int64
	for _, trace := range r.gatewayTraces {
		if !memoryGatewayTraceMatches(trace, query) {
			continue
		}
		summary.Total++
		if trace.ProviderID != "" || trace.ProviderAccountID != "" {
			summary.Routed++
		}
		if trace.Status == "upstream_error" || trace.Status == "error" || trace.ErrorType != "" {
			summary.Errors++
		}
		summary.Tokens += trace.InputTokens + trace.OutputTokens
		latencyTotal += trace.LatencyMS
	}
	if summary.Total > 0 {
		summary.AvgLatencyMS = latencyTotal / int64(summary.Total)
	}
	return summary, nil
}

func (r *MemoryRepository) ListAuditLogs(_ context.Context, limit int) ([]AuditLog, error) {
	return r.QueryAuditLogs(context.Background(), AuditLogQuery{Limit: limit})
}

func (r *MemoryRepository) QueryAuditLogs(_ context.Context, query AuditLogQuery) ([]AuditLog, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AuditLog, 0, len(r.auditLogs))
	for _, event := range r.auditLogs {
		if memoryAuditLogMatches(event, query) {
			out = append(out, event)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 50, 500)
	if offset >= len(out) {
		return []AuditLog{}, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (r *MemoryRepository) SummarizeAuditLogs(_ context.Context, query AuditLogQuery) (AuditLogSummary, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	actors := map[string]struct{}{}
	resources := map[string]struct{}{}
	actions := map[string]struct{}{}
	var summary AuditLogSummary
	for _, event := range r.auditLogs {
		if !memoryAuditLogMatches(event, query) {
			continue
		}
		summary.Total++
		if event.Actor != "" {
			actors[event.Actor] = struct{}{}
		}
		if event.ResourceType != "" {
			resources[event.ResourceType] = struct{}{}
		}
		if event.Action != "" {
			actions[event.Action] = struct{}{}
		}
	}
	summary.Actors = len(actors)
	summary.Resources = len(resources)
	summary.Actions = len(actions)
	return summary, nil
}

func (r *MemoryRepository) AddAuditLog(_ context.Context, event AuditLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.auditLogs[event.ID] = event
	return nil
}

func (r *MemoryRepository) Health(context.Context) error {
	return nil
}

func (r *MemoryRepository) Close() error {
	return nil
}

func memoryUsageRecordMatches(record UsageRecord, query UsageQuery) bool {
	if query.APIKeyID != "" && record.APIKeyID != query.APIKeyID {
		return false
	}
	if query.Model != "" && record.Model != query.Model {
		return false
	}
	if query.Status != "" && record.Status != query.Status {
		return false
	}
	if query.ProjectID != "" && record.ProjectID != query.ProjectID {
		return false
	}
	if query.ApplicationID != "" && record.ApplicationID != query.ApplicationID {
		return false
	}
	if !query.CreatedFrom.IsZero() && record.CreatedAt.Before(query.CreatedFrom) {
		return false
	}
	if !query.CreatedTo.IsZero() && record.CreatedAt.After(query.CreatedTo) {
		return false
	}
	keyword := strings.ToLower(strings.TrimSpace(query.Search))
	if keyword == "" {
		return true
	}
	return anyContains(keyword, record.Model, record.Status, record.ErrorType, record.ProviderID, record.ProviderAccountID, record.APIKeyID, record.APIFingerprint, record.ProjectID, record.ApplicationID)
}

func memoryGatewayTraceMatches(trace GatewayTrace, query GatewayTraceQuery) bool {
	if query.APIKeyID != "" && trace.APIKeyID != query.APIKeyID {
		return false
	}
	if query.Model != "" && trace.Model != query.Model {
		return false
	}
	if query.Status != "" && trace.Status != query.Status {
		return false
	}
	if query.ProjectID != "" && trace.ProjectID != query.ProjectID {
		return false
	}
	if query.ApplicationID != "" && trace.ApplicationID != query.ApplicationID {
		return false
	}
	if !query.CreatedFrom.IsZero() && trace.CreatedAt.Before(query.CreatedFrom) {
		return false
	}
	if !query.CreatedTo.IsZero() && trace.CreatedAt.After(query.CreatedTo) {
		return false
	}
	keyword := strings.ToLower(strings.TrimSpace(query.Search))
	if keyword == "" {
		return true
	}
	return anyContains(keyword, trace.Model, trace.Status, trace.ErrorType, trace.ProviderID, trace.ProviderAccountID, trace.RouteSource, trace.RouteReason, trace.APIKeyID, trace.APIFingerprint, trace.ProjectID, trace.ApplicationID, trace.RequestSummary, trace.ResponseSummary)
}

func memoryAuditLogMatches(event AuditLog, query AuditLogQuery) bool {
	if query.Action != "" && event.Action != query.Action {
		return false
	}
	if query.ResourceType != "" && event.ResourceType != query.ResourceType {
		return false
	}
	if !query.CreatedFrom.IsZero() && event.CreatedAt.Before(query.CreatedFrom) {
		return false
	}
	if !query.CreatedTo.IsZero() && event.CreatedAt.After(query.CreatedTo) {
		return false
	}
	keyword := strings.ToLower(strings.TrimSpace(query.Search))
	if keyword == "" {
		return true
	}
	return anyContains(keyword, event.Actor, event.Action, event.ResourceType, event.ResourceID, event.Summary)
}

func anyContains(keyword string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), keyword) {
			return true
		}
	}
	return false
}

func normalizeListWindow(limit int, offset int, fallback int, max int) (int, int) {
	if limit <= 0 {
		limit = fallback
	}
	if limit > max {
		limit = max
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func appendExactFilter(clauses *[]string, args *[]any, column string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	*args = append(*args, value)
	*clauses = append(*clauses, fmt.Sprintf("%s = $%d", column, len(*args)))
}

func appendTimeFilter(clauses *[]string, args *[]any, column string, operator string, value time.Time) {
	if value.IsZero() {
		return
	}
	*args = append(*args, value)
	*clauses = append(*clauses, fmt.Sprintf("%s %s $%d", column, operator, len(*args)))
}

func appendUsageRecordFilters(clauses *[]string, args *[]any, query UsageQuery) {
	appendExactFilter(clauses, args, "api_key_id", query.APIKeyID)
	appendExactFilter(clauses, args, "model", query.Model)
	appendExactFilter(clauses, args, "status", query.Status)
	appendExactFilter(clauses, args, "project_id", query.ProjectID)
	appendExactFilter(clauses, args, "application_id", query.ApplicationID)
	appendTimeFilter(clauses, args, "created_at", ">=", query.CreatedFrom)
	appendTimeFilter(clauses, args, "created_at", "<=", query.CreatedTo)
	appendSearchFilter(clauses, args, query.Search, []string{"model", "status", "error_type", "provider_id", "provider_account_id", "api_key_id", "api_fingerprint", "project_id", "application_id"})
}

func appendGatewayTraceFilters(clauses *[]string, args *[]any, query GatewayTraceQuery) {
	appendExactFilter(clauses, args, "api_key_id", query.APIKeyID)
	appendExactFilter(clauses, args, "model", query.Model)
	appendExactFilter(clauses, args, "status", query.Status)
	appendExactFilter(clauses, args, "project_id", query.ProjectID)
	appendExactFilter(clauses, args, "application_id", query.ApplicationID)
	appendTimeFilter(clauses, args, "created_at", ">=", query.CreatedFrom)
	appendTimeFilter(clauses, args, "created_at", "<=", query.CreatedTo)
	appendSearchFilter(clauses, args, query.Search, []string{"model", "status", "error_type", "provider_id", "provider_account_id", "route_source", "route_reason", "api_key_id", "api_fingerprint", "project_id", "application_id", "request_summary", "response_summary"})
}

func appendAuditLogFilters(clauses *[]string, args *[]any, query AuditLogQuery) {
	appendExactFilter(clauses, args, "action", query.Action)
	appendExactFilter(clauses, args, "resource_type", query.ResourceType)
	appendTimeFilter(clauses, args, "created_at", ">=", query.CreatedFrom)
	appendTimeFilter(clauses, args, "created_at", "<=", query.CreatedTo)
	appendSearchFilter(clauses, args, query.Search, []string{"actor", "action", "resource_type", "resource_id", "summary"})
}

func appendSearchFilter(clauses *[]string, args *[]any, value string, columns []string) {
	value = strings.TrimSpace(value)
	if value == "" || len(columns) == 0 {
		return
	}
	*args = append(*args, "%"+value+"%")
	placeholder := fmt.Sprintf("$%d", len(*args))
	parts := make([]string, 0, len(columns))
	for _, column := range columns {
		parts = append(parts, column+" ILIKE "+placeholder)
	}
	*clauses = append(*clauses, "("+strings.Join(parts, " OR ")+")")
}

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(ctx context.Context, databaseURL string) (*PostgresRepository, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	repo := &PostgresRepository{db: db}
	if err := repo.Health(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := repo.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return repo, nil
}

func (r *PostgresRepository) migrate(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS provider_connections (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  base_url TEXT NOT NULL,
  status TEXT NOT NULL,
  models TEXT NOT NULL DEFAULT '[]',
  priority INTEGER NOT NULL DEFAULT 100,
  secret_configured BOOLEAN NOT NULL DEFAULT false,
  secret_hint TEXT NOT NULL DEFAULT '',
  secret_ciphertext TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE provider_connections ADD COLUMN IF NOT EXISTS secret_ciphertext TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS provider_health_checks (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  latency_ms BIGINT NOT NULL DEFAULT 0,
  message TEXT NOT NULL DEFAULT '',
  models TEXT NOT NULL DEFAULT '[]',
  checked_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS provider_health_checks_provider_checked_idx
  ON provider_health_checks(provider_id, checked_at DESC);

CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  cost_center TEXT NOT NULL DEFAULT '',
  monthly_budget_cents INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS departments (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  code TEXT NOT NULL UNIQUE,
  parent_id TEXT NOT NULL DEFAULT '',
  cost_center TEXT NOT NULL DEFAULT '',
  monthly_budget_cents INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS departments_parent_idx
  ON departments(parent_id);

CREATE INDEX IF NOT EXISTS departments_cost_center_idx
  ON departments(cost_center);

CREATE TABLE IF NOT EXISTS applications (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  environment TEXT NOT NULL DEFAULT 'dev',
  owner TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS workspace_users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  role TEXT NOT NULL DEFAULT 'developer',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS role_bindings (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS role_bindings_unique_scope_idx
  ON role_bindings(user_id, role, scope_type, scope_id);

CREATE INDEX IF NOT EXISTS role_bindings_scope_idx
  ON role_bindings(scope_type, scope_id);

CREATE TABLE IF NOT EXISTS routing_groups (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  platform TEXT NOT NULL,
  rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1,
  status TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS provider_accounts (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  platform TEXT NOT NULL,
  auth_type TEXT NOT NULL,
  status TEXT NOT NULL,
  schedulable BOOLEAN NOT NULL DEFAULT true,
  priority INTEGER NOT NULL DEFAULT 50,
  concurrency INTEGER NOT NULL DEFAULT 3,
  rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1,
  models TEXT NOT NULL DEFAULT '[]',
  group_ids TEXT NOT NULL DEFAULT '[]',
  secret_configured BOOLEAN NOT NULL DEFAULT false,
  secret_hint TEXT NOT NULL DEFAULT '',
  secret_ciphertext TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  last_used_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS provider_id TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS provider_account_health_checks (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
  provider_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  latency_ms BIGINT NOT NULL DEFAULT 0,
  message TEXT NOT NULL DEFAULT '',
  models TEXT NOT NULL DEFAULT '[]',
  checked_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS provider_account_health_checks_account_checked_idx
  ON provider_account_health_checks(account_id, checked_at DESC);

CREATE TABLE IF NOT EXISTS model_pricings (
  id TEXT PRIMARY KEY,
  model TEXT NOT NULL UNIQUE,
  currency TEXT NOT NULL DEFAULT 'USD',
  input_price_cents_per_1m_tokens INTEGER NOT NULL DEFAULT 0,
  output_price_cents_per_1m_tokens INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS api_keys (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  key_hash TEXT NOT NULL UNIQUE,
  fingerprint TEXT NOT NULL,
  prefix TEXT NOT NULL,
  status TEXT NOT NULL,
  model_allowlist TEXT NOT NULL DEFAULT '[]',
  qps_limit INTEGER NOT NULL DEFAULT 0,
  monthly_token_limit INTEGER NOT NULL DEFAULT 0,
  expires_at TIMESTAMPTZ,
  last_used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id TEXT PRIMARY KEY,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  summary TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS usage_records (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  application_id TEXT NOT NULL,
  api_key_id TEXT NOT NULL,
  api_fingerprint TEXT NOT NULL,
  model TEXT NOT NULL,
  provider_id TEXT NOT NULL DEFAULT '',
  provider_account_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  error_type TEXT NOT NULL DEFAULT '',
  latency_ms BIGINT NOT NULL DEFAULT 0,
  input_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  cost_cents INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS provider_account_id TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS gateway_traces (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  application_id TEXT NOT NULL,
  api_key_id TEXT NOT NULL,
  api_fingerprint TEXT NOT NULL,
  model TEXT NOT NULL,
  stream BOOLEAN NOT NULL DEFAULT false,
  message_count INTEGER NOT NULL DEFAULT 0,
  provider_id TEXT NOT NULL DEFAULT '',
  provider_account_id TEXT NOT NULL DEFAULT '',
  route_source TEXT NOT NULL DEFAULT '',
  route_reason TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  http_status INTEGER NOT NULL DEFAULT 0,
  error_type TEXT NOT NULL DEFAULT '',
  latency_ms BIGINT NOT NULL DEFAULT 0,
  input_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  request_summary TEXT NOT NULL DEFAULT '',
  response_summary TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS gateway_traces_created_idx
  ON gateway_traces(created_at DESC);

CREATE INDEX IF NOT EXISTS gateway_traces_route_idx
  ON gateway_traces(provider_id, provider_account_id, created_at DESC);

CREATE TABLE IF NOT EXISTS alert_events (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  severity TEXT NOT NULL,
  status TEXT NOT NULL,
  title TEXT NOT NULL,
  summary TEXT NOT NULL,
  resource_type TEXT NOT NULL DEFAULT '',
  resource_id TEXT NOT NULL DEFAULT '',
  project_id TEXT NOT NULL DEFAULT '',
  dedupe_key TEXT NOT NULL UNIQUE,
  metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  first_seen_at TIMESTAMPTZ NOT NULL,
  last_seen_at TIMESTAMPTZ NOT NULL,
  acknowledged_at TIMESTAMPTZ,
  acknowledged_by TEXT NOT NULL DEFAULT '',
  resolved_at TIMESTAMPTZ,
  resolved_by TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS alert_events_status_last_seen_idx
  ON alert_events(status, last_seen_at DESC);

CREATE INDEX IF NOT EXISTS alert_events_resource_idx
  ON alert_events(resource_type, resource_id, last_seen_at DESC);

CREATE INDEX IF NOT EXISTS alert_events_project_idx
  ON alert_events(project_id, last_seen_at DESC);
`)
	return err
}

func (r *PostgresRepository) ListProviders(ctx context.Context) ([]ProviderConnection, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, name, type, base_url, status, models, priority, secret_configured, secret_hint, secret_ciphertext, created_at, updated_at
FROM provider_connections
ORDER BY priority ASC, name ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderConnection
	for rows.Next() {
		var provider ProviderConnection
		var models string
		if err := rows.Scan(&provider.ID, &provider.Name, &provider.Type, &provider.BaseURL, &provider.Status, &models, &provider.Priority, &provider.SecretConfigured, &provider.SecretHint, &provider.SecretCiphertext, &provider.CreatedAt, &provider.UpdatedAt); err != nil {
			return nil, err
		}
		provider.Models = parseStringList(models)
		out = append(out, provider)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveProvider(ctx context.Context, provider ProviderConnection) error {
	models := marshalStringList(provider.Models)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO provider_connections(id, name, type, base_url, status, models, priority, secret_configured, secret_hint, secret_ciphertext, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT(id) DO UPDATE SET
  name = EXCLUDED.name,
  type = EXCLUDED.type,
  base_url = EXCLUDED.base_url,
  status = EXCLUDED.status,
  models = EXCLUDED.models,
  priority = EXCLUDED.priority,
  secret_configured = EXCLUDED.secret_configured,
  secret_hint = EXCLUDED.secret_hint,
  secret_ciphertext = EXCLUDED.secret_ciphertext,
  updated_at = EXCLUDED.updated_at
`, provider.ID, provider.Name, provider.Type, provider.BaseURL, provider.Status, models, provider.Priority, provider.SecretConfigured, provider.SecretHint, provider.SecretCiphertext, provider.CreatedAt, provider.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListLatestProviderHealthChecks(ctx context.Context) ([]ProviderHealthCheck, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT DISTINCT ON (provider_id) id, provider_id, status, latency_ms, message, models, checked_at
FROM provider_health_checks
ORDER BY provider_id, checked_at DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderHealthCheck
	for rows.Next() {
		var check ProviderHealthCheck
		var models string
		if err := rows.Scan(&check.ID, &check.ProviderID, &check.Status, &check.LatencyMS, &check.Message, &models, &check.CheckedAt); err != nil {
			return nil, err
		}
		check.Models = parseStringList(models)
		out = append(out, check)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CheckedAt.After(out[j].CheckedAt) })
	return out, rows.Err()
}

func (r *PostgresRepository) SaveProviderHealthCheck(ctx context.Context, check ProviderHealthCheck) error {
	models := marshalStringList(check.Models)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO provider_health_checks(id, provider_id, status, latency_ms, message, models, checked_at)
VALUES($1,$2,$3,$4,$5,$6,$7)
`, check.ID, check.ProviderID, check.Status, check.LatencyMS, check.Message, models, check.CheckedAt)
	return err
}

func (r *PostgresRepository) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, name, description, cost_center, monthly_budget_cents, status, created_at, updated_at
FROM projects
ORDER BY created_at ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var project Project
		if err := rows.Scan(&project.ID, &project.Name, &project.Description, &project.CostCenter, &project.MonthlyBudgetCents, &project.Status, &project.CreatedAt, &project.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, project)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveProject(ctx context.Context, project Project) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO projects(id, name, description, cost_center, monthly_budget_cents, status, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(id) DO UPDATE SET
  name = EXCLUDED.name,
  description = EXCLUDED.description,
  cost_center = EXCLUDED.cost_center,
  monthly_budget_cents = EXCLUDED.monthly_budget_cents,
  status = EXCLUDED.status,
  updated_at = EXCLUDED.updated_at
`, project.ID, project.Name, project.Description, project.CostCenter, project.MonthlyBudgetCents, project.Status, project.CreatedAt, project.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListApplications(ctx context.Context, projectID string) ([]Application, error) {
	query := `
SELECT id, project_id, name, environment, owner, status, created_at, updated_at
FROM applications`
	args := []any{}
	if projectID != "" {
		query += ` WHERE project_id = $1`
		args = append(args, projectID)
	}
	query += ` ORDER BY created_at ASC`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Application
	for rows.Next() {
		var app Application
		if err := rows.Scan(&app.ID, &app.ProjectID, &app.Name, &app.Environment, &app.Owner, &app.Status, &app.CreatedAt, &app.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, app)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveApplication(ctx context.Context, app Application) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO applications(id, project_id, name, environment, owner, status, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(id) DO UPDATE SET
  project_id = EXCLUDED.project_id,
  name = EXCLUDED.name,
  environment = EXCLUDED.environment,
  owner = EXCLUDED.owner,
  status = EXCLUDED.status,
  updated_at = EXCLUDED.updated_at
`, app.ID, app.ProjectID, app.Name, app.Environment, app.Owner, app.Status, app.CreatedAt, app.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListRoutingGroups(ctx context.Context) ([]RoutingGroup, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, name, description, platform, rate_multiplier, status, sort_order, created_at, updated_at
FROM routing_groups
ORDER BY sort_order ASC, name ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RoutingGroup
	for rows.Next() {
		var group RoutingGroup
		if err := rows.Scan(&group.ID, &group.Name, &group.Description, &group.Platform, &group.RateMultiplier, &group.Status, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	counts, activeCounts, err := r.routingGroupAccountCounts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].AccountCount = counts[out[i].ID]
		out[i].ActiveAccounts = activeCounts[out[i].ID]
	}
	return out, nil
}

func (r *PostgresRepository) SaveRoutingGroup(ctx context.Context, group RoutingGroup) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO routing_groups(id, name, description, platform, rate_multiplier, status, sort_order, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(id) DO UPDATE SET
  name = EXCLUDED.name,
  description = EXCLUDED.description,
  platform = EXCLUDED.platform,
  rate_multiplier = EXCLUDED.rate_multiplier,
  status = EXCLUDED.status,
  sort_order = EXCLUDED.sort_order,
  updated_at = EXCLUDED.updated_at
`, group.ID, group.Name, group.Description, group.Platform, group.RateMultiplier, group.Status, group.SortOrder, group.CreatedAt, group.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListProviderAccounts(ctx context.Context) ([]ProviderAccount, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, provider_id, name, platform, auth_type, status, schedulable, priority, concurrency, rate_multiplier, models, group_ids, secret_configured, secret_hint, secret_ciphertext, error_message, last_used_at, expires_at, created_at, updated_at
FROM provider_accounts
ORDER BY priority ASC, name ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProviderAccount
	for rows.Next() {
		var account ProviderAccount
		var models, groupIDs string
		var lastUsedAt, expiresAt sql.NullTime
		if err := rows.Scan(&account.ID, &account.ProviderID, &account.Name, &account.Platform, &account.AuthType, &account.Status, &account.Schedulable, &account.Priority, &account.Concurrency, &account.RateMultiplier, &models, &groupIDs, &account.SecretConfigured, &account.SecretHint, &account.SecretCiphertext, &account.ErrorMessage, &lastUsedAt, &expiresAt, &account.CreatedAt, &account.UpdatedAt); err != nil {
			return nil, err
		}
		account.Models = parseStringList(models)
		account.GroupIDs = parseStringList(groupIDs)
		if lastUsedAt.Valid {
			account.LastUsedAt = &lastUsedAt.Time
		}
		if expiresAt.Valid {
			account.ExpiresAt = &expiresAt.Time
		}
		out = append(out, account)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveProviderAccount(ctx context.Context, account ProviderAccount) error {
	models := marshalStringList(account.Models)
	groupIDs := marshalStringList(account.GroupIDs)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO provider_accounts(id, provider_id, name, platform, auth_type, status, schedulable, priority, concurrency, rate_multiplier, models, group_ids, secret_configured, secret_hint, secret_ciphertext, error_message, last_used_at, expires_at, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
ON CONFLICT(id) DO UPDATE SET
  provider_id = EXCLUDED.provider_id,
  name = EXCLUDED.name,
  platform = EXCLUDED.platform,
  auth_type = EXCLUDED.auth_type,
  status = EXCLUDED.status,
  schedulable = EXCLUDED.schedulable,
  priority = EXCLUDED.priority,
  concurrency = EXCLUDED.concurrency,
  rate_multiplier = EXCLUDED.rate_multiplier,
  models = EXCLUDED.models,
  group_ids = EXCLUDED.group_ids,
  secret_configured = EXCLUDED.secret_configured,
  secret_hint = EXCLUDED.secret_hint,
  secret_ciphertext = EXCLUDED.secret_ciphertext,
  error_message = EXCLUDED.error_message,
  last_used_at = EXCLUDED.last_used_at,
  expires_at = EXCLUDED.expires_at,
  updated_at = EXCLUDED.updated_at
`, account.ID, account.ProviderID, account.Name, account.Platform, account.AuthType, account.Status, account.Schedulable, account.Priority, account.Concurrency, account.RateMultiplier, models, groupIDs, account.SecretConfigured, account.SecretHint, account.SecretCiphertext, account.ErrorMessage, account.LastUsedAt, account.ExpiresAt, account.CreatedAt, account.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListLatestProviderAccountHealthChecks(ctx context.Context) ([]ProviderAccountHealthCheck, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT DISTINCT ON (account_id) id, account_id, provider_id, status, latency_ms, message, models, checked_at
FROM provider_account_health_checks
ORDER BY account_id, checked_at DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderAccountHealthCheck
	for rows.Next() {
		var check ProviderAccountHealthCheck
		var models string
		if err := rows.Scan(&check.ID, &check.AccountID, &check.ProviderID, &check.Status, &check.LatencyMS, &check.Message, &models, &check.CheckedAt); err != nil {
			return nil, err
		}
		check.Models = parseStringList(models)
		out = append(out, check)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CheckedAt.After(out[j].CheckedAt) })
	return out, rows.Err()
}

func (r *PostgresRepository) SaveProviderAccountHealthCheck(ctx context.Context, check ProviderAccountHealthCheck) error {
	models := marshalStringList(check.Models)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO provider_account_health_checks(id, account_id, provider_id, status, latency_ms, message, models, checked_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)
`, check.ID, check.AccountID, check.ProviderID, check.Status, check.LatencyMS, check.Message, models, check.CheckedAt)
	return err
}

func (r *PostgresRepository) ListModelPricings(ctx context.Context) ([]ModelPricing, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, model, currency, input_price_cents_per_1m_tokens, output_price_cents_per_1m_tokens, status, created_at, updated_at
FROM model_pricings
ORDER BY model ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelPricing
	for rows.Next() {
		var pricing ModelPricing
		if err := rows.Scan(&pricing.ID, &pricing.Model, &pricing.Currency, &pricing.InputPriceCentsPer1MTokens, &pricing.OutputPriceCentsPer1MTokens, &pricing.Status, &pricing.CreatedAt, &pricing.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, pricing)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveModelPricing(ctx context.Context, pricing ModelPricing) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO model_pricings(id, model, currency, input_price_cents_per_1m_tokens, output_price_cents_per_1m_tokens, status, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(id) DO UPDATE SET
  model = EXCLUDED.model,
  currency = EXCLUDED.currency,
  input_price_cents_per_1m_tokens = EXCLUDED.input_price_cents_per_1m_tokens,
  output_price_cents_per_1m_tokens = EXCLUDED.output_price_cents_per_1m_tokens,
  status = EXCLUDED.status,
  updated_at = EXCLUDED.updated_at
`, pricing.ID, pricing.Model, pricing.Currency, pricing.InputPriceCentsPer1MTokens, pricing.OutputPriceCentsPer1MTokens, pricing.Status, pricing.CreatedAt, pricing.UpdatedAt)
	return err
}

func (r *PostgresRepository) routingGroupAccountCounts(ctx context.Context) (map[string]int, map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT status, schedulable, group_ids
FROM provider_accounts
`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	counts := map[string]int{}
	activeCounts := map[string]int{}
	for rows.Next() {
		var status, groupIDs string
		var schedulable bool
		if err := rows.Scan(&status, &schedulable, &groupIDs); err != nil {
			return nil, nil, err
		}
		for _, groupID := range parseStringList(groupIDs) {
			counts[groupID]++
			if status == AccountStatusActive && schedulable {
				activeCounts[groupID]++
			}
		}
	}
	return counts, activeCounts, rows.Err()
}

func (r *PostgresRepository) ListAPIKeys(ctx context.Context) ([]APIKeyRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, project_id, application_id, name, key_hash, fingerprint, prefix, status, model_allowlist, qps_limit, monthly_token_limit, expires_at, last_used_at, created_at, updated_at
FROM api_keys
ORDER BY created_at DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKeyRecord
	for rows.Next() {
		var key APIKeyRecord
		var allowlist string
		var expiresAt, lastUsedAt sql.NullTime
		if err := rows.Scan(&key.ID, &key.ProjectID, &key.ApplicationID, &key.Name, &key.KeyHash, &key.Fingerprint, &key.Prefix, &key.Status, &allowlist, &key.QPSLimit, &key.MonthlyTokenLimit, &expiresAt, &lastUsedAt, &key.CreatedAt, &key.UpdatedAt); err != nil {
			return nil, err
		}
		key.ModelAllowlist = parseStringList(allowlist)
		if expiresAt.Valid {
			key.ExpiresAt = &expiresAt.Time
		}
		if lastUsedAt.Valid {
			key.LastUsedAt = &lastUsedAt.Time
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) FindAPIKeyByHash(ctx context.Context, hash string) (APIKeyRecord, bool, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, project_id, application_id, name, key_hash, fingerprint, prefix, status, model_allowlist, qps_limit, monthly_token_limit, expires_at, last_used_at, created_at, updated_at
FROM api_keys
WHERE key_hash = $1
`, hash)
	var key APIKeyRecord
	var allowlist string
	var expiresAt, lastUsedAt sql.NullTime
	if err := row.Scan(&key.ID, &key.ProjectID, &key.ApplicationID, &key.Name, &key.KeyHash, &key.Fingerprint, &key.Prefix, &key.Status, &allowlist, &key.QPSLimit, &key.MonthlyTokenLimit, &expiresAt, &lastUsedAt, &key.CreatedAt, &key.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return APIKeyRecord{}, false, nil
		}
		return APIKeyRecord{}, false, err
	}
	key.ModelAllowlist = parseStringList(allowlist)
	if expiresAt.Valid {
		key.ExpiresAt = &expiresAt.Time
	}
	if lastUsedAt.Valid {
		key.LastUsedAt = &lastUsedAt.Time
	}
	return key, true, nil
}

func (r *PostgresRepository) SaveAPIKey(ctx context.Context, key APIKeyRecord) error {
	allowlist := marshalStringList(key.ModelAllowlist)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO api_keys(id, project_id, application_id, name, key_hash, fingerprint, prefix, status, model_allowlist, qps_limit, monthly_token_limit, expires_at, last_used_at, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
ON CONFLICT(id) DO UPDATE SET
  name = EXCLUDED.name,
  key_hash = EXCLUDED.key_hash,
  fingerprint = EXCLUDED.fingerprint,
  prefix = EXCLUDED.prefix,
  status = EXCLUDED.status,
  model_allowlist = EXCLUDED.model_allowlist,
  qps_limit = EXCLUDED.qps_limit,
  monthly_token_limit = EXCLUDED.monthly_token_limit,
  expires_at = EXCLUDED.expires_at,
  last_used_at = EXCLUDED.last_used_at,
  updated_at = EXCLUDED.updated_at
`, key.ID, key.ProjectID, key.ApplicationID, key.Name, key.KeyHash, key.Fingerprint, key.Prefix, key.Status, allowlist, key.QPSLimit, key.MonthlyTokenLimit, key.ExpiresAt, key.LastUsedAt, key.CreatedAt, key.UpdatedAt)
	return err
}

func (r *PostgresRepository) UpdateAPIKeyLastUsed(ctx context.Context, id string, lastUsedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = $1, updated_at = $1 WHERE id = $2`, lastUsedAt, id)
	return err
}

func (r *PostgresRepository) DisableAPIKey(ctx context.Context, id string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE api_keys SET status = $1, updated_at = $2 WHERE id = $3`, APIKeyStatusDisabled, updatedAt, id)
	return err
}

func (r *PostgresRepository) SaveUsageRecord(ctx context.Context, record UsageRecord) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO usage_records(id, project_id, application_id, api_key_id, api_fingerprint, model, provider_id, provider_account_id, status, error_type, latency_ms, input_tokens, output_tokens, cost_cents, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
`, record.ID, record.ProjectID, record.ApplicationID, record.APIKeyID, record.APIFingerprint, record.Model, record.ProviderID, record.ProviderAccountID, record.Status, record.ErrorType, record.LatencyMS, record.InputTokens, record.OutputTokens, record.CostCents, record.CreatedAt)
	return err
}

func (r *PostgresRepository) ListUsageRecords(ctx context.Context, limit int) ([]UsageRecord, error) {
	return r.QueryUsageRecords(ctx, UsageQuery{Limit: limit})
}

func (r *PostgresRepository) QueryUsageRecords(ctx context.Context, query UsageQuery) ([]UsageRecord, error) {
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 100, 500)
	clauses := []string{}
	args := []any{}
	appendUsageRecordFilters(&clauses, &args, query)
	sqlText := `
SELECT id, project_id, application_id, api_key_id, api_fingerprint, model, provider_id, provider_account_id, status, error_type, latency_ms, input_tokens, output_tokens, cost_cents, created_at
FROM usage_records`
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit, offset)
	sqlText += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageRecord
	for rows.Next() {
		var record UsageRecord
		if err := rows.Scan(&record.ID, &record.ProjectID, &record.ApplicationID, &record.APIKeyID, &record.APIFingerprint, &record.Model, &record.ProviderID, &record.ProviderAccountID, &record.Status, &record.ErrorType, &record.LatencyMS, &record.InputTokens, &record.OutputTokens, &record.CostCents, &record.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SummarizeUsageRecords(ctx context.Context, query UsageQuery) (UsageAggregate, error) {
	clauses := []string{}
	args := []any{}
	appendUsageRecordFilters(&clauses, &args, query)
	sqlText := `
SELECT model,
       COUNT(*),
       COALESCE(SUM(CASE WHEN status IN ('upstream_error', 'error') OR error_type <> '' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(input_tokens + output_tokens), 0),
       COALESCE(SUM(cost_cents), 0),
       COALESCE(SUM(latency_ms), 0)
FROM usage_records`
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	sqlText += " GROUP BY model ORDER BY model ASC"
	rows, err := r.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return UsageAggregate{}, err
	}
	defer rows.Close()
	var aggregate UsageAggregate
	var latencyTotal int64
	for rows.Next() {
		var model string
		var requests int64
		var errors int64
		var tokens int64
		var costCents int64
		var modelLatencyTotal int64
		if err := rows.Scan(&model, &requests, &errors, &tokens, &costCents, &modelLatencyTotal); err != nil {
			return UsageAggregate{}, err
		}
		avgLatency := int64(0)
		if requests > 0 {
			avgLatency = modelLatencyTotal / requests
		}
		aggregate.ByModel = append(aggregate.ByModel, UsageModelSummary{
			Model:      model,
			Requests:   int(requests),
			Errors:     int(errors),
			Tokens:     int(tokens),
			CostCents:  int(costCents),
			AvgLatency: avgLatency,
		})
		aggregate.TotalRequests += int(requests)
		aggregate.ErrorRequests += int(errors)
		aggregate.TotalTokens += int(tokens)
		aggregate.TotalCostCents += int(costCents)
		latencyTotal += modelLatencyTotal
	}
	if err := rows.Err(); err != nil {
		return UsageAggregate{}, err
	}
	if aggregate.TotalRequests > 0 {
		aggregate.AvgLatencyMS = latencyTotal / int64(aggregate.TotalRequests)
	}
	return aggregate, nil
}

func (r *PostgresRepository) SumUsageTokensByAPIKeySince(ctx context.Context, apiKeyID string, since time.Time) (int, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(input_tokens + output_tokens), 0)
FROM usage_records
WHERE api_key_id = $1 AND created_at >= $2
`, apiKeyID, since)
	var total int
	if err := row.Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (r *PostgresRepository) SumUsageCostCentsByProjectSince(ctx context.Context, projectID string, since time.Time) (int, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(cost_cents), 0)
FROM usage_records
WHERE project_id = $1 AND created_at >= $2
`, projectID, since)
	var total int
	if err := row.Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (r *PostgresRepository) SaveGatewayTrace(ctx context.Context, trace GatewayTrace) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO gateway_traces(id, project_id, application_id, api_key_id, api_fingerprint, model, stream, message_count, provider_id, provider_account_id, route_source, route_reason, status, http_status, error_type, latency_ms, input_tokens, output_tokens, request_summary, response_summary, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
`, trace.ID, trace.ProjectID, trace.ApplicationID, trace.APIKeyID, trace.APIFingerprint, trace.Model, trace.Stream, trace.MessageCount, trace.ProviderID, trace.ProviderAccountID, trace.RouteSource, trace.RouteReason, trace.Status, trace.HTTPStatus, trace.ErrorType, trace.LatencyMS, trace.InputTokens, trace.OutputTokens, trace.RequestSummary, trace.ResponseSummary, trace.CreatedAt)
	return err
}

func (r *PostgresRepository) ListGatewayTraces(ctx context.Context, limit int) ([]GatewayTrace, error) {
	return r.QueryGatewayTraces(ctx, GatewayTraceQuery{Limit: limit})
}

func (r *PostgresRepository) QueryGatewayTraces(ctx context.Context, query GatewayTraceQuery) ([]GatewayTrace, error) {
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 100, 500)
	clauses := []string{}
	args := []any{}
	appendGatewayTraceFilters(&clauses, &args, query)
	sqlText := `
SELECT id, project_id, application_id, api_key_id, api_fingerprint, model, stream, message_count, provider_id, provider_account_id, route_source, route_reason, status, http_status, error_type, latency_ms, input_tokens, output_tokens, request_summary, response_summary, created_at
FROM gateway_traces`
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit, offset)
	sqlText += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GatewayTrace
	for rows.Next() {
		var trace GatewayTrace
		if err := rows.Scan(&trace.ID, &trace.ProjectID, &trace.ApplicationID, &trace.APIKeyID, &trace.APIFingerprint, &trace.Model, &trace.Stream, &trace.MessageCount, &trace.ProviderID, &trace.ProviderAccountID, &trace.RouteSource, &trace.RouteReason, &trace.Status, &trace.HTTPStatus, &trace.ErrorType, &trace.LatencyMS, &trace.InputTokens, &trace.OutputTokens, &trace.RequestSummary, &trace.ResponseSummary, &trace.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, trace)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SummarizeGatewayTraces(ctx context.Context, query GatewayTraceQuery) (GatewayTraceSummary, error) {
	clauses := []string{}
	args := []any{}
	appendGatewayTraceFilters(&clauses, &args, query)
	sqlText := `
SELECT COUNT(*),
       COALESCE(SUM(CASE WHEN provider_id <> '' OR provider_account_id <> '' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN status IN ('upstream_error', 'error') OR error_type <> '' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(input_tokens + output_tokens), 0),
       COALESCE(SUM(latency_ms), 0)
FROM gateway_traces`
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	var total int64
	var routed int64
	var errors int64
	var tokens int64
	var latencyTotal int64
	if err := r.db.QueryRowContext(ctx, sqlText, args...).Scan(&total, &routed, &errors, &tokens, &latencyTotal); err != nil {
		return GatewayTraceSummary{}, err
	}
	summary := GatewayTraceSummary{Total: int(total), Routed: int(routed), Errors: int(errors), Tokens: int(tokens)}
	if total > 0 {
		summary.AvgLatencyMS = latencyTotal / total
	}
	return summary, nil
}

func (r *PostgresRepository) ListAuditLogs(ctx context.Context, limit int) ([]AuditLog, error) {
	return r.QueryAuditLogs(ctx, AuditLogQuery{Limit: limit})
}

func (r *PostgresRepository) QueryAuditLogs(ctx context.Context, query AuditLogQuery) ([]AuditLog, error) {
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 50, 500)
	clauses := []string{}
	args := []any{}
	appendAuditLogFilters(&clauses, &args, query)
	sqlText := `
SELECT id, actor, action, resource_type, resource_id, summary, created_at
FROM audit_logs`
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit, offset)
	sqlText += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditLog
	for rows.Next() {
		var event AuditLog
		if err := rows.Scan(&event.ID, &event.Actor, &event.Action, &event.ResourceType, &event.ResourceID, &event.Summary, &event.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SummarizeAuditLogs(ctx context.Context, query AuditLogQuery) (AuditLogSummary, error) {
	clauses := []string{}
	args := []any{}
	appendAuditLogFilters(&clauses, &args, query)
	sqlText := `
SELECT COUNT(*),
       COUNT(DISTINCT NULLIF(actor, '')),
       COUNT(DISTINCT NULLIF(resource_type, '')),
       COUNT(DISTINCT NULLIF(action, ''))
FROM audit_logs`
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	var total int64
	var actors int64
	var resources int64
	var actions int64
	if err := r.db.QueryRowContext(ctx, sqlText, args...).Scan(&total, &actors, &resources, &actions); err != nil {
		return AuditLogSummary{}, err
	}
	return AuditLogSummary{Total: int(total), Actors: int(actors), Resources: int(resources), Actions: int(actions)}, nil
}

func (r *PostgresRepository) AddAuditLog(ctx context.Context, event AuditLog) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO audit_logs(id, actor, action, resource_type, resource_id, summary, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7)
`, event.ID, event.Actor, event.Action, event.ResourceType, event.ResourceID, event.Summary, event.CreatedAt)
	return err
}

func (r *PostgresRepository) Health(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

func (r *PostgresRepository) Close() error {
	return r.db.Close()
}

func marshalStringList(values []string) string {
	raw, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func parseStringList(value string) []string {
	var out []string
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return []string{}
	}
	return out
}
