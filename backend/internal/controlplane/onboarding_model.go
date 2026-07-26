package controlplane

import "time"

const (
	OnboardingStatusInProgress = "in_progress"
	OnboardingStatusFailed     = "failed"
	OnboardingStatusCompleted  = "completed"

	OnboardingStepStarted        = "started"
	OnboardingStepModelSource    = "model_source"
	OnboardingStepPublishedModel = "published_model"
	OnboardingStepAPIKey         = "api_key"
	OnboardingStepVerification   = "verification"

	ClientCodex        = "codex"
	ClientClaudeCode   = "claude_code"
	ClientOpenAISDK    = "openai_sdk"
	ClientAnthropicSDK = "anthropic_sdk"
)

type OnboardingSession struct {
	ID                         string    `json:"id"`
	Actor                      string    `json:"actor"`
	IdempotencyKey             string    `json:"-"`
	Status                     string    `json:"status"`
	CurrentStep                string    `json:"current_step"`
	ProviderID                 string    `json:"provider_id,omitempty"`
	ProviderAccountID          string    `json:"provider_account_id,omitempty"`
	ProviderHealthCheckID      string    `json:"provider_health_check_id,omitempty"`
	GatewayModelID             string    `json:"gateway_model_id,omitempty"`
	ModelRouteID               string    `json:"model_route_id,omitempty"`
	APIKeyID                   string    `json:"api_key_id,omitempty"`
	VerificationClient         string    `json:"verification_client,omitempty"`
	VerificationModel          string    `json:"verification_model,omitempty"`
	VerificationOperationID    string    `json:"verification_operation_id,omitempty"`
	VerificationTraceID        string    `json:"verification_trace_id,omitempty"`
	VerificationHTTPStatus     int       `json:"verification_http_status,omitempty"`
	VerificationErrorCode      string    `json:"verification_error_code,omitempty"`
	VerificationRecoveryAction string    `json:"verification_recovery_action,omitempty"`
	FailureStage               string    `json:"failure_stage,omitempty"`
	FailureCode                string    `json:"failure_code,omitempty"`
	RecoveryHint               string    `json:"recovery_hint,omitempty"`
	Version                    int64     `json:"version"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
	ExpiresAt                  time.Time `json:"expires_at"`
	CompletedSteps             []string  `json:"completed_steps"`
	PendingSteps               []string  `json:"pending_steps"`
}

type OnboardingModelSourceRequest struct {
	ProviderName  string            `json:"provider_name"`
	ProviderType  string            `json:"provider_type"`
	BaseURL       string            `json:"base_url"`
	AccountName   string            `json:"account_name"`
	AuthType      string            `json:"auth_type"`
	Secret        string            `json:"secret"`
	AdapterConfig map[string]string `json:"adapter_config"`
	UpstreamModel string            `json:"upstream_model"`
	Concurrency   int               `json:"concurrency"`
	RPMLimit      int               `json:"rpm_limit"`
	TPMLimit      int               `json:"tpm_limit"`
}

type OnboardingModelSourceResult struct {
	Session  OnboardingSession          `json:"session"`
	Provider ProviderConnection         `json:"provider"`
	Account  ProviderAccount            `json:"account"`
	Health   ProviderAccountHealthCheck `json:"health"`
}

type OnboardingPublishedModelRequest struct {
	ModelID        string `json:"model_id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Modality       string `json:"modality"`
	RouteGroup     string `json:"route_group"`
	UpstreamModel  string `json:"upstream_model"`
	UpstreamFormat string `json:"upstream_format"`
}

type OnboardingPublishedModelResult struct {
	Session OnboardingSession `json:"session"`
	Model   GatewayModel      `json:"published_model"`
	Route   ModelRoute        `json:"route"`
}

type OnboardingAPIKeyRequest struct {
	Name                string `json:"name"`
	QPSLimit            int    `json:"qps_limit"`
	RPMLimit            int    `json:"rpm_limit"`
	TPMLimit            int    `json:"tpm_limit"`
	ConcurrencyLimit    int    `json:"concurrency_limit"`
	MonthlyTokenLimit   int    `json:"monthly_token_limit"`
	MonthlyBudgetMicros int64  `json:"monthly_budget_micros"`
}

type OnboardingAPIKeyResult struct {
	Session    OnboardingSession `json:"session"`
	APIKey     APIKeyRecord      `json:"api_key"`
	Credential string            `json:"credential"`
}

type APIKeyClientConfig struct {
	APIKeyID             string            `json:"api_key_id"`
	Client               string            `json:"client"`
	Model                string            `json:"model"`
	GatewayURL           string            `json:"gateway_url"`
	CredentialEnv        string            `json:"credential_env"`
	Format               string            `json:"format"`
	FilePath             string            `json:"file_path,omitempty"`
	Content              string            `json:"content"`
	Environment          map[string]string `json:"environment"`
	VerificationPath     string            `json:"verification_path"`
	RecoveryInstructions []string          `json:"recovery_instructions"`
	ContainsSecret       bool              `json:"contains_secret"`
}

type ClientVerificationResult struct {
	Status         string `json:"status"`
	Client         string `json:"client"`
	APIKeyID       string `json:"api_key_id"`
	Model          string `json:"model"`
	HTTPStatus     int    `json:"http_status"`
	OperationID    string `json:"operation_id,omitempty"`
	TraceID        string `json:"trace_id,omitempty"`
	ErrorCode      string `json:"error_code,omitempty"`
	RecoveryAction string `json:"recovery_action,omitempty"`
}

type OnboardingVerificationResult struct {
	Session      OnboardingSession        `json:"session"`
	Verification ClientVerificationResult `json:"verification"`
}
