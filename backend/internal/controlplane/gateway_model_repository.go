package controlplane

import (
	"context"
	"database/sql"
	"sort"
)

func (r *MemoryRepository) ListGatewayModels(context.Context) ([]GatewayModel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]GatewayModel, 0, len(r.gatewayModels))
	for _, model := range r.gatewayModels {
		model.RouteCount = r.modelRouteCount(model.ID)
		out = append(out, model)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModelID < out[j].ModelID })
	return out, nil
}

func (r *MemoryRepository) SaveGatewayModel(_ context.Context, model GatewayModel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gatewayModels[model.ID] = model
	return nil
}

func (r *MemoryRepository) DeleteGatewayModel(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.gatewayModels, id)
	for routeID, route := range r.modelRoutes {
		if route.GatewayModelID == id {
			delete(r.modelRoutes, routeID)
		}
	}
	return nil
}

func (r *MemoryRepository) ListModelRoutes(context.Context) ([]ModelRoute, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ModelRoute, 0, len(r.modelRoutes))
	for _, route := range r.modelRoutes {
		out = append(out, route)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].GatewayModelID != out[j].GatewayModelID {
			return out[i].GatewayModelID < out[j].GatewayModelID
		}
		if out[i].RouteGroup != out[j].RouteGroup {
			return out[i].RouteGroup < out[j].RouteGroup
		}
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (r *MemoryRepository) SaveModelRoute(_ context.Context, route ModelRoute) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modelRoutes[route.ID] = route
	return nil
}

func (r *MemoryRepository) SaveModelRoutes(_ context.Context, routes []ModelRoute) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, route := range routes {
		r.modelRoutes[route.ID] = route
	}
	return nil
}

func (r *MemoryRepository) DeleteModelRoute(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.modelRoutes, id)
	return nil
}

func (r *MemoryRepository) modelRouteCount(gatewayModelID string) int {
	count := 0
	for _, route := range r.modelRoutes {
		if route.GatewayModelID == gatewayModelID {
			count++
		}
	}
	return count
}

func (r *PostgresRepository) ListGatewayModels(ctx context.Context) ([]GatewayModel, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT m.id, m.model_id, m.name, m.description, m.modality, m.default_route_group, m.sticky_enabled, m.sticky_ttl_seconds, m.status,
  COUNT(r.id), m.created_at, m.updated_at
FROM gateway_models m
LEFT JOIN model_routes r ON r.gateway_model_id = m.id
GROUP BY m.id
ORDER BY m.model_id ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]GatewayModel, 0)
	for rows.Next() {
		var model GatewayModel
		if err := rows.Scan(&model.ID, &model.ModelID, &model.Name, &model.Description, &model.Modality, &model.DefaultRouteGroup, &model.StickyEnabled, &model.StickyTTLSeconds, &model.Status, &model.RouteCount, &model.CreatedAt, &model.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, model)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveGatewayModel(ctx context.Context, model GatewayModel) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO gateway_models(id, model_id, name, description, modality, default_route_group, sticky_enabled, sticky_ttl_seconds, status, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT(id) DO UPDATE SET
  model_id = EXCLUDED.model_id,
  name = EXCLUDED.name,
  description = EXCLUDED.description,
  modality = EXCLUDED.modality,
  default_route_group = EXCLUDED.default_route_group,
  sticky_enabled = EXCLUDED.sticky_enabled,
  sticky_ttl_seconds = EXCLUDED.sticky_ttl_seconds,
  status = EXCLUDED.status,
  updated_at = EXCLUDED.updated_at
`, model.ID, model.ModelID, model.Name, model.Description, model.Modality, model.DefaultRouteGroup, model.StickyEnabled, model.StickyTTLSeconds, model.Status, model.CreatedAt, model.UpdatedAt)
	return err
}

func (r *PostgresRepository) DeleteGatewayModel(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM gateway_models WHERE id = $1`, id)
	return err
}

func (r *PostgresRepository) ListModelRoutes(ctx context.Context) ([]ModelRoute, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, gateway_model_id, route_group, provider_account_id, upstream_model, priority, weight, status, created_at, updated_at
FROM model_routes
ORDER BY gateway_model_id ASC, route_group ASC, priority ASC, id ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ModelRoute, 0)
	for rows.Next() {
		var route ModelRoute
		if err := rows.Scan(&route.ID, &route.GatewayModelID, &route.RouteGroup, &route.ProviderAccountID, &route.UpstreamModel, &route.Priority, &route.Weight, &route.Status, &route.CreatedAt, &route.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, route)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveModelRoute(ctx context.Context, route ModelRoute) error {
	return saveModelRoute(ctx, r.db, route)
}

type modelRouteExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func saveModelRoute(ctx context.Context, executor modelRouteExecutor, route ModelRoute) error {
	_, err := executor.ExecContext(ctx, `
INSERT INTO model_routes(id, gateway_model_id, route_group, provider_account_id, upstream_model, priority, weight, status, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT(id) DO UPDATE SET
  gateway_model_id = EXCLUDED.gateway_model_id,
  route_group = EXCLUDED.route_group,
  provider_account_id = EXCLUDED.provider_account_id,
  upstream_model = EXCLUDED.upstream_model,
  priority = EXCLUDED.priority,
  weight = EXCLUDED.weight,
  status = EXCLUDED.status,
  updated_at = EXCLUDED.updated_at
`, route.ID, route.GatewayModelID, route.RouteGroup, route.ProviderAccountID, route.UpstreamModel, route.Priority, route.Weight, route.Status, route.CreatedAt, route.UpdatedAt)
	return err
}

func (r *PostgresRepository) SaveModelRoutes(ctx context.Context, routes []ModelRoute) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, route := range routes {
		if err := saveModelRoute(ctx, tx, route); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *PostgresRepository) DeleteModelRoute(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM model_routes WHERE id = $1`, id)
	return err
}
