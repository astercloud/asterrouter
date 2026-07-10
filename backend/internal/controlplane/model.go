package controlplane

import "time"

const (
	ProviderStatusActive      = "active"
	ProviderStatusDisabled    = "disabled"
	ProviderStatusNeedsSecret = "needs_secret"

	ProjectStatusActive   = "active"
	ProjectStatusArchived = "archived"

	ApplicationStatusActive   = "active"
	ApplicationStatusDisabled = "disabled"

	APIKeyStatusActive   = "active"
	APIKeyStatusDisabled = "disabled"

	AccountStatusActive   = "active"
	AccountStatusError    = "error"
	AccountStatusDisabled = "disabled"

	RoutingGroupStatusActive   = "active"
	RoutingGroupStatusDisabled = "disabled"

	ModelPricingStatusActive   = "active"
	ModelPricingStatusDisabled = "disabled"
)

type ProviderConnection struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Type             string    `json:"type"`
	BaseURL          string    `json:"base_url"`
	Status           string    `json:"status"`
	Models           []string  `json:"models"`
	Priority         int       `json:"priority"`
	SecretConfigured bool      `json:"secret_configured"`
	SecretHint       string    `json:"secret_hint"`
	SecretCiphertext string    `json:"-"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ProviderHealthCheck struct {
	ID         string    `json:"id"`
	ProviderID string    `json:"provider_id"`
	Status     string    `json:"status"`
	LatencyMS  int64     `json:"latency_ms"`
	Message    string    `json:"message"`
	Models     []string  `json:"models"`
	CheckedAt  time.Time `json:"checked_at"`
}

type ProviderRequest struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	BaseURL  string   `json:"base_url"`
	Status   string   `json:"status"`
	Models   []string `json:"models"`
	Priority int      `json:"priority"`
	APIKey   string   `json:"api_key"`
}

type Project struct {
	ID                    string    `json:"id"`
	Name                  string    `json:"name"`
	Description           string    `json:"description"`
	CostCenter            string    `json:"cost_center"`
	MonthlyBudgetCents    int       `json:"monthly_budget_cents"`
	CurrentMonthCostCents int       `json:"current_month_cost_cents"`
	BudgetRemainingCents  int       `json:"budget_remaining_cents"`
	BudgetUsedPercent     float64   `json:"budget_used_percent"`
	BudgetStatus          string    `json:"budget_status"`
	Status                string    `json:"status"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type ProjectRequest struct {
	Name               string `json:"name"`
	Description        string `json:"description"`
	CostCenter         string `json:"cost_center"`
	MonthlyBudgetCents int    `json:"monthly_budget_cents"`
	Status             string `json:"status"`
}

type Application struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Name        string    `json:"name"`
	Environment string    `json:"environment"`
	Owner       string    `json:"owner"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ApplicationRequest struct {
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Environment string `json:"environment"`
	Owner       string `json:"owner"`
	Status      string `json:"status"`
}

type RoutingGroup struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Platform       string    `json:"platform"`
	RateMultiplier float64   `json:"rate_multiplier"`
	Status         string    `json:"status"`
	SortOrder      int       `json:"sort_order"`
	AccountCount   int       `json:"account_count"`
	ActiveAccounts int       `json:"active_account_count"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type RoutingGroupRequest struct {
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	Platform       string  `json:"platform"`
	RateMultiplier float64 `json:"rate_multiplier"`
	Status         string  `json:"status"`
	SortOrder      int     `json:"sort_order"`
}

type ProviderAccount struct {
	ID               string     `json:"id"`
	ProviderID       string     `json:"provider_id"`
	Name             string     `json:"name"`
	Platform         string     `json:"platform"`
	AuthType         string     `json:"auth_type"`
	Status           string     `json:"status"`
	Schedulable      bool       `json:"schedulable"`
	Priority         int        `json:"priority"`
	Concurrency      int        `json:"concurrency"`
	RateMultiplier   float64    `json:"rate_multiplier"`
	Models           []string   `json:"models"`
	GroupIDs         []string   `json:"group_ids"`
	SecretConfigured bool       `json:"secret_configured"`
	SecretHint       string     `json:"secret_hint"`
	SecretCiphertext string     `json:"-"`
	ErrorMessage     string     `json:"error_message"`
	LastUsedAt       *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type ProviderAccountRequest struct {
	ProviderID     string   `json:"provider_id"`
	Name           string   `json:"name"`
	Platform       string   `json:"platform"`
	AuthType       string   `json:"auth_type"`
	Status         string   `json:"status"`
	Schedulable    *bool    `json:"schedulable"`
	Priority       int      `json:"priority"`
	Concurrency    int      `json:"concurrency"`
	RateMultiplier float64  `json:"rate_multiplier"`
	Models         []string `json:"models"`
	GroupIDs       []string `json:"group_ids"`
	Secret         string   `json:"secret"`
	ExpiresAt      string   `json:"expires_at"`
}

type ProviderAccountHealthCheck struct {
	ID         string    `json:"id"`
	AccountID  string    `json:"account_id"`
	ProviderID string    `json:"provider_id"`
	Status     string    `json:"status"`
	LatencyMS  int64     `json:"latency_ms"`
	Message    string    `json:"message"`
	Models     []string  `json:"models"`
	CheckedAt  time.Time `json:"checked_at"`
}

type ModelPricing struct {
	ID                          string    `json:"id"`
	Model                       string    `json:"model"`
	Currency                    string    `json:"currency"`
	InputPriceCentsPer1MTokens  int       `json:"input_price_cents_per_1m_tokens"`
	OutputPriceCentsPer1MTokens int       `json:"output_price_cents_per_1m_tokens"`
	Status                      string    `json:"status"`
	CreatedAt                   time.Time `json:"created_at"`
	UpdatedAt                   time.Time `json:"updated_at"`
}

type ModelPricingRequest struct {
	Model                       string `json:"model"`
	Currency                    string `json:"currency"`
	InputPriceCentsPer1MTokens  int    `json:"input_price_cents_per_1m_tokens"`
	OutputPriceCentsPer1MTokens int    `json:"output_price_cents_per_1m_tokens"`
	Status                      string `json:"status"`
}

type APIKeyRecord struct {
	ID                string     `json:"id"`
	ProjectID         string     `json:"project_id"`
	ApplicationID     string     `json:"application_id"`
	Name              string     `json:"name"`
	KeyHash           string     `json:"-"`
	Fingerprint       string     `json:"fingerprint"`
	Prefix            string     `json:"prefix"`
	Status            string     `json:"status"`
	ModelAllowlist    []string   `json:"model_allowlist"`
	QPSLimit          int        `json:"qps_limit"`
	MonthlyTokenLimit int        `json:"monthly_token_limit"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type APIKeyCreateRequest struct {
	ProjectID         string   `json:"project_id"`
	ApplicationID     string   `json:"application_id"`
	Name              string   `json:"name"`
	ModelAllowlist    []string `json:"model_allowlist"`
	QPSLimit          int      `json:"qps_limit"`
	MonthlyTokenLimit int      `json:"monthly_token_limit"`
	ExpiresAt         string   `json:"expires_at"`
}

type APIKeyUpdateRequest struct {
	Name              string   `json:"name"`
	ModelAllowlist    []string `json:"model_allowlist"`
	QPSLimit          int      `json:"qps_limit"`
	MonthlyTokenLimit int      `json:"monthly_token_limit"`
	ExpiresAt         string   `json:"expires_at"`
	Status            string   `json:"status"`
}

type APIKeyCreateResponse struct {
	Record APIKeyRecord `json:"record"`
	Key    string       `json:"key"`
}

type AuditLog struct {
	ID           string    `json:"id"`
	Actor        string    `json:"actor"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	Summary      string    `json:"summary"`
	CreatedAt    time.Time `json:"created_at"`
}

type Dashboard struct {
	ProviderCount       int        `json:"provider_count"`
	ActiveProviderCount int        `json:"active_provider_count"`
	ProjectCount        int        `json:"project_count"`
	ApplicationCount    int        `json:"application_count"`
	APIKeyCount         int        `json:"api_key_count"`
	ActiveAPIKeyCount   int        `json:"active_api_key_count"`
	Models              []string   `json:"models"`
	RecentAudit         []AuditLog `json:"recent_audit"`
}

type UsageRecord struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"project_id"`
	ApplicationID     string    `json:"application_id"`
	APIKeyID          string    `json:"api_key_id"`
	APIFingerprint    string    `json:"api_fingerprint"`
	Model             string    `json:"model"`
	ProviderID        string    `json:"provider_id"`
	ProviderAccountID string    `json:"provider_account_id"`
	Status            string    `json:"status"`
	ErrorType         string    `json:"error_type"`
	LatencyMS         int64     `json:"latency_ms"`
	InputTokens       int       `json:"input_tokens"`
	OutputTokens      int       `json:"output_tokens"`
	CostCents         int       `json:"cost_cents"`
	CreatedAt         time.Time `json:"created_at"`
}

type UsageModelSummary struct {
	Model      string `json:"model"`
	Requests   int    `json:"requests"`
	Errors     int    `json:"errors"`
	Tokens     int    `json:"tokens"`
	CostCents  int    `json:"cost_cents"`
	AvgLatency int64  `json:"avg_latency_ms"`
}

type UsageReport struct {
	TotalRequests  int                 `json:"total_requests"`
	ErrorRequests  int                 `json:"error_requests"`
	TotalTokens    int                 `json:"total_tokens"`
	TotalCostCents int                 `json:"total_cost_cents"`
	AvgLatencyMS   int64               `json:"avg_latency_ms"`
	ByModel        []UsageModelSummary `json:"by_model"`
	Recent         []UsageRecord       `json:"recent"`
}

type UsageAggregate struct {
	TotalRequests  int
	ErrorRequests  int
	TotalTokens    int
	TotalCostCents int
	AvgLatencyMS   int64
	ByModel        []UsageModelSummary
}

type CostAllocationRollup struct {
	ProjectID      string
	ApplicationID  string
	APIKeyID       string
	APIFingerprint string
	Model          string
	Requests       int
	ErrorRequests  int
	TotalTokens    int
	TotalCostCents int
	AvgLatencyMS   int64
	LatencyTotal   int64
}

type CostAllocationRow struct {
	Dimension         string  `json:"dimension"`
	ResourceID        string  `json:"resource_id"`
	ResourceName      string  `json:"resource_name"`
	ProjectID         string  `json:"project_id"`
	ProjectName       string  `json:"project_name"`
	CostCenter        string  `json:"cost_center"`
	ApplicationID     string  `json:"application_id"`
	ApplicationName   string  `json:"application_name"`
	APIKeyID          string  `json:"api_key_id"`
	APIKeyName        string  `json:"api_key_name"`
	APIFingerprint    string  `json:"api_fingerprint"`
	Model             string  `json:"model"`
	Requests          int     `json:"requests"`
	ErrorRequests     int     `json:"error_requests"`
	TotalTokens       int     `json:"total_tokens"`
	TotalCostCents    int     `json:"total_cost_cents"`
	AvgLatencyMS      int64   `json:"avg_latency_ms"`
	BudgetCents       int     `json:"budget_cents"`
	BudgetUsedPercent float64 `json:"budget_used_percent"`
	CostSharePercent  float64 `json:"cost_share_percent"`
}

type CostAllocationReport struct {
	Dimension      string              `json:"dimension"`
	TotalRequests  int                 `json:"total_requests"`
	ErrorRequests  int                 `json:"error_requests"`
	TotalTokens    int                 `json:"total_tokens"`
	TotalCostCents int                 `json:"total_cost_cents"`
	AvgLatencyMS   int64               `json:"avg_latency_ms"`
	Rows           []CostAllocationRow `json:"rows"`
}

type UsageQuery struct {
	Limit         int
	Offset        int
	Search        string
	APIKeyID      string
	Model         string
	Status        string
	ProjectID     string
	ApplicationID string
	CreatedFrom   time.Time
	CreatedTo     time.Time
}

type GatewayTrace struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"project_id"`
	ApplicationID     string    `json:"application_id"`
	APIKeyID          string    `json:"api_key_id"`
	APIFingerprint    string    `json:"api_fingerprint"`
	Model             string    `json:"model"`
	Stream            bool      `json:"stream"`
	MessageCount      int       `json:"message_count"`
	ProviderID        string    `json:"provider_id"`
	ProviderAccountID string    `json:"provider_account_id"`
	RouteSource       string    `json:"route_source"`
	RouteReason       string    `json:"route_reason"`
	Status            string    `json:"status"`
	HTTPStatus        int       `json:"http_status"`
	ErrorType         string    `json:"error_type"`
	LatencyMS         int64     `json:"latency_ms"`
	InputTokens       int       `json:"input_tokens"`
	OutputTokens      int       `json:"output_tokens"`
	RequestSummary    string    `json:"request_summary"`
	ResponseSummary   string    `json:"response_summary"`
	CreatedAt         time.Time `json:"created_at"`
}

type GatewayTraceQuery struct {
	Limit         int
	Offset        int
	Search        string
	APIKeyID      string
	Model         string
	Status        string
	ProjectID     string
	ApplicationID string
	CreatedFrom   time.Time
	CreatedTo     time.Time
}

type GatewayTraceSummary struct {
	Total        int   `json:"total"`
	Routed       int   `json:"routed"`
	Errors       int   `json:"errors"`
	Tokens       int   `json:"tokens"`
	AvgLatencyMS int64 `json:"avg_latency_ms"`
}

type AuditLogQuery struct {
	Limit        int
	Offset       int
	Search       string
	Action       string
	ResourceType string
	CreatedFrom  time.Time
	CreatedTo    time.Time
}

type AuditLogSummary struct {
	Total     int `json:"total"`
	Actors    int `json:"actors"`
	Resources int `json:"resources"`
	Actions   int `json:"actions"`
}

type PortalWorkspace struct {
	Projects     []Project      `json:"projects"`
	Applications []Application  `json:"applications"`
	APIKeys      []APIKeyRecord `json:"api_keys"`
	Models       []string       `json:"models"`
	GatewayPath  string         `json:"gateway_path"`
}

type GatewayAuthContext struct {
	APIKey      APIKeyRecord `json:"api_key"`
	Project     Project      `json:"project"`
	Application Application  `json:"application"`
}

type GatewayProvider struct {
	ID              string
	Name            string
	BaseURL         string
	APIKey          string
	AccountID       string
	AccountName     string
	Source          string
	SelectionReason string
}
