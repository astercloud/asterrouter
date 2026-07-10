package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	CostAllocationByProject     = "project"
	CostAllocationByApplication = "application"
	CostAllocationByAPIKey      = "api_key"
	CostAllocationByModel       = "model"
)

var ErrInvalidCostAllocationDimension = errors.New("invalid cost allocation dimension")

func (s *Service) CostAllocationReportQuery(ctx context.Context, dimension string, query UsageQuery) (CostAllocationReport, error) {
	dimension, err := normalizeCostAllocationDimension(dimension)
	if err != nil {
		return CostAllocationReport{}, err
	}
	rollups, err := s.repo.SummarizeCostAllocation(ctx, dimension, query)
	if err != nil {
		return CostAllocationReport{}, err
	}
	aggregate, err := s.repo.SummarizeUsageRecords(ctx, query)
	if err != nil {
		return CostAllocationReport{}, err
	}
	projects, err := s.repo.ListProjects(ctx)
	if err != nil {
		return CostAllocationReport{}, err
	}
	applications, err := s.repo.ListApplications(ctx, "")
	if err != nil {
		return CostAllocationReport{}, err
	}
	apiKeys, err := s.repo.ListAPIKeys(ctx)
	if err != nil {
		return CostAllocationReport{}, err
	}

	projectByID := make(map[string]Project, len(projects))
	for _, project := range projects {
		projectByID[project.ID] = project
	}
	appByID := make(map[string]Application, len(applications))
	for _, app := range applications {
		appByID[app.ID] = app
	}
	keyByID := make(map[string]APIKeyRecord, len(apiKeys))
	for _, key := range apiKeys {
		keyByID[key.ID] = key
	}

	rows := make([]CostAllocationRow, 0, len(rollups))
	for _, rollup := range rollups {
		rows = append(rows, costAllocationRow(dimension, rollup, aggregate.TotalCostCents, projectByID, appByID, keyByID))
	}
	return CostAllocationReport{
		Dimension:      dimension,
		TotalRequests:  aggregate.TotalRequests,
		ErrorRequests:  aggregate.ErrorRequests,
		TotalTokens:    aggregate.TotalTokens,
		TotalCostCents: aggregate.TotalCostCents,
		AvgLatencyMS:   aggregate.AvgLatencyMS,
		Rows:           rows,
	}, nil
}

func normalizeCostAllocationDimension(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return CostAllocationByProject, nil
	}
	switch value {
	case CostAllocationByProject, CostAllocationByApplication, CostAllocationByAPIKey, CostAllocationByModel:
		return value, nil
	default:
		return "", ErrInvalidCostAllocationDimension
	}
}

func costAllocationRow(dimension string, rollup CostAllocationRollup, totalCostCents int, projects map[string]Project, applications map[string]Application, apiKeys map[string]APIKeyRecord) CostAllocationRow {
	row := CostAllocationRow{
		Dimension:      dimension,
		ProjectID:      rollup.ProjectID,
		ApplicationID:  rollup.ApplicationID,
		APIKeyID:       rollup.APIKeyID,
		APIFingerprint: rollup.APIFingerprint,
		Model:          rollup.Model,
		Requests:       rollup.Requests,
		ErrorRequests:  rollup.ErrorRequests,
		TotalTokens:    rollup.TotalTokens,
		TotalCostCents: rollup.TotalCostCents,
		AvgLatencyMS:   rollup.AvgLatencyMS,
	}

	if key, ok := apiKeys[row.APIKeyID]; ok {
		row.APIKeyName = key.Name
		if row.APIFingerprint == "" {
			row.APIFingerprint = key.Fingerprint
		}
		if row.ProjectID == "" {
			row.ProjectID = key.ProjectID
		}
		if row.ApplicationID == "" {
			row.ApplicationID = key.ApplicationID
		}
	}
	if app, ok := applications[row.ApplicationID]; ok {
		row.ApplicationName = app.Name
		if row.ProjectID == "" {
			row.ProjectID = app.ProjectID
		}
	}
	if project, ok := projects[row.ProjectID]; ok {
		row.ProjectName = project.Name
		row.CostCenter = project.CostCenter
		row.BudgetCents = project.MonthlyBudgetCents
		if row.BudgetCents > 0 {
			row.BudgetUsedPercent = percent(row.TotalCostCents, row.BudgetCents)
		}
	}
	if totalCostCents > 0 {
		row.CostSharePercent = percent(row.TotalCostCents, totalCostCents)
	}

	row.ResourceID, row.ResourceName = costAllocationResource(dimension, row)
	return row
}

func costAllocationResource(dimension string, row CostAllocationRow) (string, string) {
	switch dimension {
	case CostAllocationByProject:
		return firstNonEmpty(row.ProjectID, "unassigned"), firstNonEmpty(row.ProjectName, row.ProjectID, "Unassigned project")
	case CostAllocationByApplication:
		return firstNonEmpty(row.ApplicationID, "unassigned"), firstNonEmpty(row.ApplicationName, row.ApplicationID, "Unassigned application")
	case CostAllocationByAPIKey:
		return firstNonEmpty(row.APIKeyID, "unassigned"), firstNonEmpty(row.APIKeyName, row.APIFingerprint, row.APIKeyID, "Unassigned API key")
	case CostAllocationByModel:
		return firstNonEmpty(row.Model, "unknown_model"), firstNonEmpty(row.Model, "Unknown model")
	default:
		return "unknown", "Unknown"
	}
}

func percent(part int, total int) float64 {
	return float64(part) * 100 / float64(total)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (r *MemoryRepository) SummarizeCostAllocation(_ context.Context, dimension string, query UsageQuery) ([]CostAllocationRollup, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := map[string]*CostAllocationRollup{}
	for _, record := range r.usageRecords {
		if !memoryUsageRecordMatches(record, query) {
			continue
		}
		key := costAllocationKey(dimension, record)
		rollup := values[key]
		if rollup == nil {
			rollup = &CostAllocationRollup{
				ProjectID:      record.ProjectID,
				ApplicationID:  record.ApplicationID,
				APIKeyID:       record.APIKeyID,
				APIFingerprint: record.APIFingerprint,
				Model:          record.Model,
			}
			if dimension == CostAllocationByModel {
				rollup.ProjectID = ""
				rollup.ApplicationID = ""
				rollup.APIKeyID = ""
				rollup.APIFingerprint = ""
			}
			values[key] = rollup
		}
		rollup.Requests++
		if record.Status == "upstream_error" || record.Status == "error" || record.ErrorType != "" {
			rollup.ErrorRequests++
		}
		rollup.TotalTokens += record.InputTokens + record.OutputTokens
		rollup.TotalCostCents += record.CostCents
		rollup.LatencyTotal += record.LatencyMS
	}

	out := make([]CostAllocationRollup, 0, len(values))
	for _, rollup := range values {
		if rollup.Requests > 0 {
			rollup.AvgLatencyMS = rollup.LatencyTotal / int64(rollup.Requests)
		}
		out = append(out, *rollup)
	}
	sortCostAllocationRollups(out)
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 100, 500)
	if offset >= len(out) {
		return []CostAllocationRollup{}, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func costAllocationKey(dimension string, record UsageRecord) string {
	switch dimension {
	case CostAllocationByProject:
		return record.ProjectID
	case CostAllocationByApplication:
		return record.ProjectID + "\x00" + record.ApplicationID
	case CostAllocationByAPIKey:
		return record.APIKeyID
	case CostAllocationByModel:
		return record.Model
	default:
		return record.ProjectID
	}
}

func (r *PostgresRepository) SummarizeCostAllocation(ctx context.Context, dimension string, query UsageQuery) ([]CostAllocationRollup, error) {
	selectFields, groupBy, err := costAllocationSQLGrouping(dimension)
	if err != nil {
		return nil, err
	}
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 100, 500)
	clauses := []string{}
	args := []any{}
	appendUsageRecordFilters(&clauses, &args, query)
	sqlText := fmt.Sprintf(`
SELECT %s,
       COUNT(*),
       COALESCE(SUM(CASE WHEN status IN ('upstream_error', 'error') OR error_type <> '' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(input_tokens + output_tokens), 0),
       COALESCE(SUM(cost_cents), 0),
       COALESCE(SUM(latency_ms), 0)
FROM usage_records`, selectFields)
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	sqlText += " GROUP BY " + groupBy
	args = append(args, limit, offset)
	sqlText += fmt.Sprintf(" ORDER BY COALESCE(SUM(cost_cents), 0) DESC, COUNT(*) DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CostAllocationRollup{}
	for rows.Next() {
		var rollup CostAllocationRollup
		var requests, errorsCount, tokens, costCents, latencyTotal int64
		if err := rows.Scan(&rollup.ProjectID, &rollup.ApplicationID, &rollup.APIKeyID, &rollup.APIFingerprint, &rollup.Model, &requests, &errorsCount, &tokens, &costCents, &latencyTotal); err != nil {
			return nil, err
		}
		rollup.Requests = int(requests)
		rollup.ErrorRequests = int(errorsCount)
		rollup.TotalTokens = int(tokens)
		rollup.TotalCostCents = int(costCents)
		rollup.LatencyTotal = latencyTotal
		if requests > 0 {
			rollup.AvgLatencyMS = latencyTotal / requests
		}
		out = append(out, rollup)
	}
	return out, rows.Err()
}

func costAllocationSQLGrouping(dimension string) (string, string, error) {
	switch dimension {
	case CostAllocationByProject:
		return "project_id, '' AS application_id, '' AS api_key_id, '' AS api_fingerprint, '' AS model", "project_id", nil
	case CostAllocationByApplication:
		return "project_id, application_id, '' AS api_key_id, '' AS api_fingerprint, '' AS model", "project_id, application_id", nil
	case CostAllocationByAPIKey:
		return "MAX(project_id) AS project_id, MAX(application_id) AS application_id, api_key_id, MAX(api_fingerprint) AS api_fingerprint, '' AS model", "api_key_id", nil
	case CostAllocationByModel:
		return "'' AS project_id, '' AS application_id, '' AS api_key_id, '' AS api_fingerprint, model", "model", nil
	default:
		return "", "", ErrInvalidCostAllocationDimension
	}
}

func sortCostAllocationRollups(rows []CostAllocationRollup) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TotalCostCents == rows[j].TotalCostCents {
			if rows[i].Requests == rows[j].Requests {
				return costAllocationSortName(rows[i]) < costAllocationSortName(rows[j])
			}
			return rows[i].Requests > rows[j].Requests
		}
		return rows[i].TotalCostCents > rows[j].TotalCostCents
	})
}

func costAllocationSortName(row CostAllocationRollup) string {
	return firstNonEmpty(row.ProjectID, row.ApplicationID, row.APIKeyID, row.Model)
}
