package operator

import (
	"context"
	"database/sql"
	"sync"

	_ "github.com/lib/pq"
)

type Repository interface {
	ListGroups(context.Context) ([]CustomerGroup, error)
	SaveGroup(context.Context, CustomerGroup) error
	DeleteGroup(context.Context, string) error
	ListCustomers(context.Context) ([]Customer, error)
	SaveCustomer(context.Context, Customer) error
	DeleteCustomer(context.Context, string) error
	ListPlans(context.Context) ([]Plan, error)
	SavePlan(context.Context, Plan) error
	DeletePlan(context.Context, string) error
	ListPricingRules(context.Context) ([]PricingRule, error)
	SavePricingRule(context.Context, PricingRule) error
	DeletePricingRule(context.Context, string) error
	ListBalanceEntries(context.Context) ([]BalanceEntry, error)
	ApplyBalanceEntry(context.Context, BalanceEntry) (BalanceEntry, error)
	ListRiskRules(context.Context) ([]RiskRule, error)
	SaveRiskRule(context.Context, RiskRule) error
	DeleteRiskRule(context.Context, string) error
	ListNotices(context.Context) ([]Notice, error)
	SaveNotice(context.Context, Notice) error
	DeleteNotice(context.Context, string) error
	Health(context.Context) error
	Close() error
}

type MemoryRepository struct {
	mu        sync.RWMutex
	groups    map[string]CustomerGroup
	customers map[string]Customer
	plans     map[string]Plan
	pricing   map[string]PricingRule
	balances  map[string]BalanceEntry
	risks     map[string]RiskRule
	notices   map[string]Notice
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{groups: map[string]CustomerGroup{}, customers: map[string]Customer{}, plans: map[string]Plan{}, pricing: map[string]PricingRule{}, balances: map[string]BalanceEntry{}, risks: map[string]RiskRule{}, notices: map[string]Notice{}}
}

func (r *MemoryRepository) ListGroups(context.Context) ([]CustomerGroup, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return mapValues(r.groups), nil
}
func (r *MemoryRepository) SaveGroup(_ context.Context, v CustomerGroup) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.groups[v.ID] = v
	return nil
}
func (r *MemoryRepository) DeleteGroup(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.groups, id)
	return nil
}
func (r *MemoryRepository) ListCustomers(context.Context) ([]Customer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return mapValues(r.customers), nil
}
func (r *MemoryRepository) SaveCustomer(_ context.Context, v Customer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.customers[v.ID] = v
	return nil
}
func (r *MemoryRepository) DeleteCustomer(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.customers, id)
	return nil
}
func (r *MemoryRepository) ListPlans(context.Context) ([]Plan, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return mapValues(r.plans), nil
}
func (r *MemoryRepository) SavePlan(_ context.Context, v Plan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plans[v.ID] = v
	return nil
}
func (r *MemoryRepository) DeletePlan(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.plans, id)
	return nil
}
func (r *MemoryRepository) ListPricingRules(context.Context) ([]PricingRule, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return mapValues(r.pricing), nil
}
func (r *MemoryRepository) SavePricingRule(_ context.Context, v PricingRule) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pricing[v.ID] = v
	return nil
}
func (r *MemoryRepository) DeletePricingRule(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pricing, id)
	return nil
}
func (r *MemoryRepository) ListBalanceEntries(context.Context) ([]BalanceEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return mapValues(r.balances), nil
}
func (r *MemoryRepository) ApplyBalanceEntry(_ context.Context, v BalanceEntry) (BalanceEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v.Reference != "" {
		for _, existing := range r.balances {
			if existing.CustomerID == v.CustomerID && existing.Reference == v.Reference {
				return existing, nil
			}
		}
	}
	customer := r.customers[v.CustomerID]
	v.BalanceAfter = customer.BalanceCents + v.AmountCents
	customer.BalanceCents = v.BalanceAfter
	customer.UpdatedAt = v.CreatedAt
	r.customers[v.CustomerID] = customer
	r.balances[v.ID] = v
	return v, nil
}
func (r *MemoryRepository) ListRiskRules(context.Context) ([]RiskRule, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return mapValues(r.risks), nil
}
func (r *MemoryRepository) SaveRiskRule(_ context.Context, v RiskRule) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.risks[v.ID] = v
	return nil
}
func (r *MemoryRepository) DeleteRiskRule(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.risks, id)
	return nil
}
func (r *MemoryRepository) ListNotices(context.Context) ([]Notice, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return mapValues(r.notices), nil
}
func (r *MemoryRepository) SaveNotice(_ context.Context, v Notice) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notices[v.ID] = v
	return nil
}
func (r *MemoryRepository) DeleteNotice(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.notices, id)
	return nil
}
func (r *MemoryRepository) Health(context.Context) error { return nil }
func (r *MemoryRepository) Close() error                 { return nil }

func mapValues[T any](values map[string]T) []T {
	out := make([]T, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

type PostgresRepository struct{ db *sql.DB }

func NewRepository(ctx context.Context, databaseURL string) (Repository, error) {
	if databaseURL == "" {
		return NewMemoryRepository(), nil
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	r := &PostgresRepository{db: db}
	if err := r.Health(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := r.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return r, nil
}

func (r *PostgresRepository) Health(ctx context.Context) error { return r.db.PingContext(ctx) }
func (r *PostgresRepository) Close() error                     { return r.db.Close() }

func (r *PostgresRepository) migrate(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, operatorSchema)
	return err
}

const operatorSchema = `
CREATE TABLE IF NOT EXISTS operator_customer_groups (id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL);
CREATE TABLE IF NOT EXISTS operator_plans (id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', monthly_fee_cents BIGINT NOT NULL DEFAULT 0, included_tokens BIGINT NOT NULL DEFAULT 0, monthly_limit_cents BIGINT NOT NULL DEFAULT 0, rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1, status TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL);
CREATE TABLE IF NOT EXISTS operator_customers (id TEXT PRIMARY KEY, name TEXT NOT NULL, email TEXT NOT NULL DEFAULT '', group_id TEXT NOT NULL DEFAULT '', plan_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, balance_cents BIGINT NOT NULL DEFAULT 0, credit_cents BIGINT NOT NULL DEFAULT 0, notes TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL);
CREATE TABLE IF NOT EXISTS operator_pricing_rules (id TEXT PRIMARY KEY, name TEXT NOT NULL, plan_id TEXT NOT NULL DEFAULT '', model TEXT NOT NULL, input_price_cents_per_1m_tokens BIGINT NOT NULL DEFAULT 0, output_price_cents_per_1m_tokens BIGINT NOT NULL DEFAULT 0, rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1, status TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL);
CREATE TABLE IF NOT EXISTS operator_balance_entries (id TEXT PRIMARY KEY, customer_id TEXT NOT NULL REFERENCES operator_customers(id) ON DELETE CASCADE, kind TEXT NOT NULL, amount_cents BIGINT NOT NULL, balance_after_cents BIGINT NOT NULL, reference TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '', actor TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL);
CREATE TABLE IF NOT EXISTS operator_risk_rules (id TEXT PRIMARY KEY, name TEXT NOT NULL, rule_type TEXT NOT NULL, threshold DOUBLE PRECISION NOT NULL DEFAULT 0, window_minutes INTEGER NOT NULL DEFAULT 60, action TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL);
CREATE TABLE IF NOT EXISTS operator_notices (id TEXT PRIMARY KEY, title TEXT NOT NULL, content TEXT NOT NULL, audience TEXT NOT NULL DEFAULT 'all', status TEXT NOT NULL, publish_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL);
CREATE INDEX IF NOT EXISTS operator_customers_group_idx ON operator_customers(group_id, status);
CREATE INDEX IF NOT EXISTS operator_customers_plan_idx ON operator_customers(plan_id, status);
CREATE INDEX IF NOT EXISTS operator_balance_customer_created_idx ON operator_balance_entries(customer_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS operator_balance_reference_idx ON operator_balance_entries(customer_id, reference) WHERE reference <> '';
`
