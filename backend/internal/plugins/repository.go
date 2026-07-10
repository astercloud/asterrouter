package plugins

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

type Repository interface {
	ListPlugins(ctx context.Context) ([]Plugin, error)
	SavePlugin(ctx context.Context, plugin Plugin) error
	FindPlugin(ctx context.Context, id string) (Plugin, bool, error)
	UpdateStatus(ctx context.Context, id string, status string, updatedAt time.Time) error
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
	mu      sync.RWMutex
	plugins map[string]Plugin
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{plugins: map[string]Plugin{}}
}

func (r *MemoryRepository) ListPlugins(context.Context) ([]Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Plugin, 0, len(r.plugins))
	for _, plugin := range r.plugins {
		out = append(out, plugin)
	}
	sortPlugins(out)
	return out, nil
}

func (r *MemoryRepository) SavePlugin(_ context.Context, plugin Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[plugin.ID] = plugin
	return nil
}

func (r *MemoryRepository) FindPlugin(_ context.Context, id string) (Plugin, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	plugin, ok := r.plugins[id]
	return plugin, ok, nil
}

func (r *MemoryRepository) UpdateStatus(_ context.Context, id string, status string, updatedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	plugin, ok := r.plugins[id]
	if !ok {
		return nil
	}
	plugin.Status = status
	plugin.UpdatedAt = updatedAt
	r.plugins[id] = plugin
	return nil
}

func (r *MemoryRepository) Health(context.Context) error {
	return nil
}

func (r *MemoryRepository) Close() error {
	return nil
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
CREATE TABLE IF NOT EXISTS plugins (
  id TEXT PRIMARY KEY,
  plugin_id TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL,
  type TEXT NOT NULL,
  tier TEXT NOT NULL,
  version TEXT NOT NULL,
  vendor TEXT NOT NULL,
  status TEXT NOT NULL,
  entitlement_status TEXT NOT NULL,
  surfaces TEXT NOT NULL DEFAULT '[]',
  entry_point TEXT NOT NULL DEFAULT '',
  configurable BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
`)
	return err
}

func (r *PostgresRepository) ListPlugins(ctx context.Context) ([]Plugin, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, plugin_id, name, description, category, type, tier, version, vendor, status, entitlement_status, surfaces, entry_point, configurable, created_at, updated_at
FROM plugins
ORDER BY category ASC, tier ASC, name ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Plugin
	for rows.Next() {
		plugin, err := scanPlugin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, plugin)
	}
	sortPlugins(out)
	return out, rows.Err()
}

func (r *PostgresRepository) SavePlugin(ctx context.Context, plugin Plugin) error {
	surfaces := marshalStringList(plugin.Surfaces)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO plugins(id, plugin_id, name, description, category, type, tier, version, vendor, status, entitlement_status, surfaces, entry_point, configurable, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
ON CONFLICT(id) DO UPDATE SET
  plugin_id = EXCLUDED.plugin_id,
  name = EXCLUDED.name,
  description = EXCLUDED.description,
  category = EXCLUDED.category,
  type = EXCLUDED.type,
  tier = EXCLUDED.tier,
  version = EXCLUDED.version,
  vendor = EXCLUDED.vendor,
  entitlement_status = EXCLUDED.entitlement_status,
  surfaces = EXCLUDED.surfaces,
  entry_point = EXCLUDED.entry_point,
  configurable = EXCLUDED.configurable,
  updated_at = EXCLUDED.updated_at
`, plugin.ID, plugin.PluginID, plugin.Name, plugin.Description, plugin.Category, plugin.Type, plugin.Tier, plugin.Version, plugin.Vendor, plugin.Status, plugin.EntitlementStatus, surfaces, plugin.EntryPoint, plugin.Configurable, plugin.CreatedAt, plugin.UpdatedAt)
	return err
}

func (r *PostgresRepository) FindPlugin(ctx context.Context, id string) (Plugin, bool, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, plugin_id, name, description, category, type, tier, version, vendor, status, entitlement_status, surfaces, entry_point, configurable, created_at, updated_at
FROM plugins
WHERE id = $1 OR plugin_id = $1
`, id)
	plugin, err := scanPlugin(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return Plugin{}, false, nil
		}
		return Plugin{}, false, err
	}
	return plugin, true, nil
}

func (r *PostgresRepository) UpdateStatus(ctx context.Context, id string, status string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE plugins SET status = $1, updated_at = $2 WHERE id = $3 OR plugin_id = $3`, status, updatedAt, id)
	return err
}

func (r *PostgresRepository) Health(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

func (r *PostgresRepository) Close() error {
	return r.db.Close()
}

type pluginScanner interface {
	Scan(dest ...any) error
}

func scanPlugin(scanner pluginScanner) (Plugin, error) {
	var plugin Plugin
	var surfaces string
	if err := scanner.Scan(&plugin.ID, &plugin.PluginID, &plugin.Name, &plugin.Description, &plugin.Category, &plugin.Type, &plugin.Tier, &plugin.Version, &plugin.Vendor, &plugin.Status, &plugin.EntitlementStatus, &surfaces, &plugin.EntryPoint, &plugin.Configurable, &plugin.CreatedAt, &plugin.UpdatedAt); err != nil {
		return Plugin{}, err
	}
	plugin.Surfaces = parseStringList(surfaces)
	return plugin, nil
}

func sortPlugins(plugins []Plugin) {
	sort.Slice(plugins, func(i, j int) bool {
		if plugins[i].Category == plugins[j].Category {
			if plugins[i].Tier == plugins[j].Tier {
				return plugins[i].Name < plugins[j].Name
			}
			return tierRank(plugins[i].Tier) < tierRank(plugins[j].Tier)
		}
		return plugins[i].Category < plugins[j].Category
	})
}

func tierRank(tier string) int {
	switch tier {
	case TierCore:
		return 0
	case TierFreeCore:
		return 1
	case TierProfileBundle:
		return 2
	case TierPaidAddon:
		return 3
	default:
		return 9
	}
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
