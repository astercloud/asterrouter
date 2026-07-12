package settings

import "time"

const (
	KeySiteName                = "site_name"
	KeySiteSubtitle            = "site_subtitle"
	KeyPublicBaseURL           = "public_base_url"
	KeyDefaultLocale           = "default_locale"
	KeyEnabledLocales          = "enabled_locales"
	KeyDefaultProfile          = "default_profile"
	KeyEnabledProfiles         = "enabled_profiles"
	KeySetupCompleted          = "setup_completed"
	KeyGatewayBasePath         = "gateway_base_path"
	KeyOIDCEnabled             = "oidc_enabled"
	KeyOIDCProviderName        = "oidc_provider_name"
	KeyOIDCIssuerURL           = "oidc_issuer_url"
	KeyOIDCClientID            = "oidc_client_id"
	KeyFeishuEnabled           = "feishu_enabled"
	KeyFeishuRegion            = "feishu_region"
	KeyFeishuAppID             = "feishu_app_id"
	KeyFeishuAppSecret         = "feishu_app_secret"
	KeyRegistrationEnabled     = "registration_enabled"
	KeyEmailVerifyEnabled      = "email_verify_enabled"
	KeyAllowedEmailDomains     = "allowed_email_domains"
	KeyInvitationRequired      = "invitation_required"
	KeyInvitationCodes         = "invitation_codes"
	KeyTOTPEnabled             = "totp_enabled"
	KeyTrustedProxyHeaders     = "trusted_proxy_headers"
	KeyTurnstileEnabled        = "turnstile_enabled"
	KeyTurnstileSiteKey        = "turnstile_site_key"
	KeyTurnstileSecretKey      = "turnstile_secret_key"
	KeyDefaultBalanceCents     = "default_balance_cents"
	KeyDefaultConcurrency      = "default_concurrency"
	KeyDefaultRPM              = "default_rpm"
	KeySMTPHost                = "smtp_host"
	KeySMTPPort                = "smtp_port"
	KeySMTPUsername            = "smtp_username"
	KeySMTPPassword            = "smtp_password"
	KeySMTPFrom                = "smtp_from"
	KeyLoginAgreementEnabled   = "login_agreement_enabled"
	KeyLoginAgreementTitle     = "login_agreement_title"
	KeyLoginAgreementContent   = "login_agreement_content"
	KeyBackendMode             = "backend_mode"
	KeyDefaultPageSize         = "default_page_size"
	KeyPageSizeOptions         = "page_size_options"
	KeySupportContact          = "support_contact"
	KeyDocumentationURL        = "documentation_url"
	KeyHomeContent             = "home_content"
	KeyHideImportButton        = "hide_import_button"
	KeyLoginAgreementMode      = "login_agreement_mode"
	KeyLoginAgreementUpdatedAt = "login_agreement_updated_at"
	KeyLegalDocuments          = "legal_documents"
	KeyDataRetentionDays       = "data_retention_days"
	KeyPromptLoggingMode       = "prompt_logging_mode"
	KeyUpdateChannel           = "update_channel"
	KeyServiceCenterMode       = "service_center_mode"
)

type Entry struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PublicSettings struct {
	SiteName                string          `json:"site_name"`
	SiteSubtitle            string          `json:"site_subtitle"`
	PublicBaseURL           string          `json:"public_base_url"`
	APIBaseURL              string          `json:"api_base_url"`
	GatewayBasePath         string          `json:"gateway_base_path"`
	DefaultProfile          string          `json:"default_profile"`
	EnabledProfiles         []string        `json:"enabled_profiles"`
	SetupCompleted          bool            `json:"setup_completed"`
	DefaultLocale           string          `json:"default_locale"`
	EnabledLocales          []string        `json:"enabled_locales"`
	OIDCEnabled             bool            `json:"oidc_enabled"`
	OIDCProviderName        string          `json:"oidc_provider_name"`
	FeishuEnabled           bool            `json:"feishu_enabled"`
	FeishuRegion            string          `json:"feishu_region"`
	RegistrationEnabled     bool            `json:"registration_enabled"`
	EmailVerifyEnabled      bool            `json:"email_verify_enabled"`
	TOTPEnabled             bool            `json:"totp_enabled"`
	TurnstileEnabled        bool            `json:"turnstile_enabled"`
	TurnstileSiteKey        string          `json:"turnstile_site_key"`
	InvitationRequired      bool            `json:"invitation_required"`
	LoginAgreementEnabled   bool            `json:"login_agreement_enabled"`
	LoginAgreementMode      string          `json:"login_agreement_mode"`
	LoginAgreementUpdatedAt string          `json:"login_agreement_updated_at"`
	LegalDocuments          []LegalDocument `json:"legal_documents"`
	BackendMode             bool            `json:"backend_mode"`
	SupportContact          string          `json:"support_contact"`
	DocumentationURL        string          `json:"documentation_url"`
	ServiceCenterMode       string          `json:"service_center_mode"`
	Version                 string          `json:"version"`
	ServerTimezone          string          `json:"server_timezone"`
	ServerUTCOffset         string          `json:"server_utc_offset"`
	StorageMode             string          `json:"storage_mode"`
	DemoMode                bool            `json:"demo_mode"`
}

type AdminSettings struct {
	PublicSettings
	OIDCIssuerURL         string   `json:"oidc_issuer_url"`
	OIDCClientID          string   `json:"oidc_client_id"`
	FeishuAppID           string   `json:"feishu_app_id"`
	FeishuAppSecret       string   `json:"feishu_app_secret,omitempty"`
	FeishuConfigured      bool     `json:"feishu_configured"`
	AllowedEmailDomains   []string `json:"allowed_email_domains"`
	InvitationCodes       []string `json:"invitation_codes"`
	TrustedProxyHeaders   bool     `json:"trusted_proxy_headers"`
	TurnstileSecretKey    string   `json:"turnstile_secret_key,omitempty"`
	TurnstileConfigured   bool     `json:"turnstile_configured"`
	DefaultBalanceCents   int      `json:"default_balance_cents"`
	DefaultConcurrency    int      `json:"default_concurrency"`
	DefaultRPM            int      `json:"default_rpm"`
	SMTPHost              string   `json:"smtp_host"`
	SMTPPort              int      `json:"smtp_port"`
	SMTPUsername          string   `json:"smtp_username"`
	SMTPPassword          string   `json:"smtp_password,omitempty"`
	SMTPFrom              string   `json:"smtp_from"`
	SMTPConfigured        bool     `json:"smtp_configured"`
	LoginAgreementTitle   string   `json:"login_agreement_title"`
	LoginAgreementContent string   `json:"login_agreement_content"`
	DefaultPageSize       int      `json:"default_page_size"`
	PageSizeOptions       []int    `json:"page_size_options"`
	HomeContent           string   `json:"home_content"`
	HideImportButton      bool     `json:"hide_import_button"`
	DataRetentionDays     int      `json:"data_retention_days"`
	PromptLoggingMode     string   `json:"prompt_logging_mode"`
	UpdateChannel         string   `json:"update_channel"`
}

type LegalDocument struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Content string `json:"content"`
}

type LocaleInfo struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Native string `json:"native"`
}

var SupportedLocales = []LocaleInfo{
	{Code: "en-US", Name: "English", Native: "English"},
	{Code: "zh-CN", Name: "Simplified Chinese", Native: "简体中文"},
}
