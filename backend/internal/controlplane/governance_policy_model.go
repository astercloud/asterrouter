package controlplane

import "time"

const (
	GovernancePolicyStatusActive   = "active"
	GovernancePolicyStatusDisabled = "disabled"

	GovernancePolicyScopeGlobal     = "global"
	GovernancePolicyScopeDepartment = "department"
	GovernancePolicyScopeProject    = "project"
	GovernancePolicyScopeAPIKey     = "api_key"

	GovernancePolicyOverageBlock    = "block"
	GovernancePolicyOverageWarn     = "warn"
	GovernancePolicyOverageFallback = "fallback"

	GovernancePolicyPromptLoggingDisabled     = "disabled"
	GovernancePolicyPromptLoggingMetadataOnly = "metadata_only"
	GovernancePolicyPromptLoggingRedacted     = "redacted"

	GatewayPolicySourceAPIKeyExplicit  = "api_key_explicit"
	GatewayPolicySourceAPIKeyScope     = "api_key_scope"
	GatewayPolicySourceProjectExplicit = "project_explicit"
	GatewayPolicySourceProjectScope    = "project_scope"
	GatewayPolicySourceGlobalScope     = "global_scope"
)

type GovernancePolicy struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	ScopeType          string    `json:"scope_type"`
	ScopeID            string    `json:"scope_id"`
	ModelAllowlist     []string  `json:"model_allowlist"`
	ModelDenylist      []string  `json:"model_denylist"`
	QPSLimit           int       `json:"qps_limit"`
	MonthlyTokenLimit  int       `json:"monthly_token_limit"`
	MonthlyBudgetCents int       `json:"monthly_budget_cents"`
	OverageAction      string    `json:"overage_action"`
	PromptLoggingMode  string    `json:"prompt_logging_mode"`
	RetentionDays      int       `json:"retention_days"`
	ToolCallAllowed    bool      `json:"tool_call_allowed"`
	ImageInputAllowed  bool      `json:"image_input_allowed"`
	WebAccessAllowed   bool      `json:"web_access_allowed"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type GovernancePolicyRequest struct {
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	ScopeType          string   `json:"scope_type"`
	ScopeID            string   `json:"scope_id"`
	ModelAllowlist     []string `json:"model_allowlist"`
	ModelDenylist      []string `json:"model_denylist"`
	QPSLimit           int      `json:"qps_limit"`
	MonthlyTokenLimit  int      `json:"monthly_token_limit"`
	MonthlyBudgetCents int      `json:"monthly_budget_cents"`
	OverageAction      string   `json:"overage_action"`
	PromptLoggingMode  string   `json:"prompt_logging_mode"`
	RetentionDays      int      `json:"retention_days"`
	ToolCallAllowed    bool     `json:"tool_call_allowed"`
	ImageInputAllowed  bool     `json:"image_input_allowed"`
	WebAccessAllowed   bool     `json:"web_access_allowed"`
	Status             string   `json:"status"`
}
