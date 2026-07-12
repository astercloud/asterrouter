package operator

import "time"

const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

type CustomerGroup struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CustomerGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

type Customer struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	GroupID      string    `json:"group_id"`
	PlanID       string    `json:"plan_id"`
	Status       string    `json:"status"`
	BalanceCents int64     `json:"balance_cents"`
	CreditCents  int64     `json:"credit_cents"`
	Notes        string    `json:"notes"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CustomerRequest struct {
	Name        string `json:"name"`
	Email       string `json:"email"`
	GroupID     string `json:"group_id"`
	PlanID      string `json:"plan_id"`
	Status      string `json:"status"`
	Notes       string `json:"notes"`
	CreditCents int64  `json:"credit_cents"`
}

type Plan struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	MonthlyFeeCents   int64     `json:"monthly_fee_cents"`
	IncludedTokens    int64     `json:"included_tokens"`
	MonthlyLimitCents int64     `json:"monthly_limit_cents"`
	RateMultiplier    float64   `json:"rate_multiplier"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type PlanRequest struct {
	Name              string  `json:"name"`
	Description       string  `json:"description"`
	Status            string  `json:"status"`
	MonthlyFeeCents   int64   `json:"monthly_fee_cents"`
	IncludedTokens    int64   `json:"included_tokens"`
	MonthlyLimitCents int64   `json:"monthly_limit_cents"`
	RateMultiplier    float64 `json:"rate_multiplier"`
}

type PricingRule struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	PlanID         string    `json:"plan_id"`
	Model          string    `json:"model"`
	InputPrice     int64     `json:"input_price_cents_per_1m_tokens"`
	OutputPrice    int64     `json:"output_price_cents_per_1m_tokens"`
	RateMultiplier float64   `json:"rate_multiplier"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type PricingRuleRequest struct {
	Name           string  `json:"name"`
	PlanID         string  `json:"plan_id"`
	Model          string  `json:"model"`
	Status         string  `json:"status"`
	InputPrice     int64   `json:"input_price_cents_per_1m_tokens"`
	OutputPrice    int64   `json:"output_price_cents_per_1m_tokens"`
	RateMultiplier float64 `json:"rate_multiplier"`
}

type BalanceEntry struct {
	ID           string    `json:"id"`
	CustomerID   string    `json:"customer_id"`
	Kind         string    `json:"kind"`
	AmountCents  int64     `json:"amount_cents"`
	BalanceAfter int64     `json:"balance_after_cents"`
	Reference    string    `json:"reference"`
	Note         string    `json:"note"`
	Actor        string    `json:"actor"`
	CreatedAt    time.Time `json:"created_at"`
}

type BalanceEntryRequest struct {
	CustomerID  string `json:"customer_id"`
	Kind        string `json:"kind"`
	Reference   string `json:"reference"`
	Note        string `json:"note"`
	AmountCents int64  `json:"amount_cents"`
}

type RiskRule struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	RuleType    string    `json:"rule_type"`
	Threshold   float64   `json:"threshold"`
	WindowMins  int       `json:"window_minutes"`
	Action      string    `json:"action"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type RiskRuleRequest struct {
	Name        string  `json:"name"`
	RuleType    string  `json:"rule_type"`
	Action      string  `json:"action"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	Threshold   float64 `json:"threshold"`
	WindowMins  int     `json:"window_minutes"`
}

type Notice struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Content   string     `json:"content"`
	Audience  string     `json:"audience"`
	Status    string     `json:"status"`
	PublishAt *time.Time `json:"publish_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type NoticeRequest struct {
	Title     string `json:"title"`
	Content   string `json:"content"`
	Audience  string `json:"audience"`
	Status    string `json:"status"`
	PublishAt string `json:"publish_at"`
}

type Dashboard struct {
	Customers       int   `json:"customers"`
	ActiveCustomers int   `json:"active_customers"`
	Plans           int   `json:"plans"`
	BalanceCents    int64 `json:"balance_cents"`
	RiskRules       int   `json:"risk_rules"`
	PublishedNotice int   `json:"published_notices"`
}
