package controlplane

import "time"

const (
	WorkspaceUserStatusActive   = "active"
	WorkspaceUserStatusDisabled = "disabled"

	RoleSuperAdmin      = "super_admin"
	RolePlatformAdmin   = "platform_admin"
	RoleKeyManager      = "key_manager"
	RoleReadOnlyAuditor = "read_only_auditor"
	RoleDeveloper       = "developer"

	RoleScopeOrganization = "organization"
	RoleScopeDepartment   = "department"
	RoleScopeGroup        = "group"
	RoleScopeApplication  = "application"
	RoleScopeResource     = "resource"

	RBACResourceDashboard    = "dashboard"
	RBACResourceRouting      = "routing"
	RBACResourceProviders    = "providers"
	RBACResourceAPIKeys      = "api_keys"
	RBACResourceUsage        = "usage"
	RBACResourceTraces       = "traces"
	RBACResourceAIJobs       = "ai_jobs"
	RBACResourceArtifacts    = "artifacts"
	RBACResourceAlerts       = "alerts"
	RBACResourceIdentity     = "identity"
	RBACResourceApplications = "applications"
	RBACResourcePolicies     = "policies"
	RBACResourceAudit        = "audit"
	RBACResourceExports      = "exports"
	RBACResourcePlugins      = "plugins"
	RBACResourceSettings     = "settings"
	RBACResourceSystem       = "system"
)

type WorkspaceUser struct {
	ID string `json:"id"`
	// Email 是账号邮箱原值，用于展示、登录与邮件投递。
	Email string `json:"email"`
	// EmailNormalized 是 Email 的“收件箱标识”（见 NormalizeEmailForAliasDedup），
	// 仅用于注册查重，防止同一收件箱借 +别名 / Gmail 点号派生多个账号。
	EmailNormalized      string     `json:"-"`
	DisplayName          string     `json:"display_name"`
	AvatarDataURL        string     `json:"avatar_data_url,omitempty"`
	Status               string     `json:"status"`
	Role                 string     `json:"role"`
	ConcurrencyLimit     int        `json:"concurrency_limit"`
	RPMLimit             int        `json:"rpm_limit"`
	ExternalIssuer       string     `json:"external_issuer,omitempty"`
	ExternalSubject      string     `json:"external_subject,omitempty"`
	DepartmentID         string     `json:"department_id,omitempty"`
	TOTPEnabled          bool       `json:"totp_enabled"`
	TOTPSecretCiphertext string     `json:"-"`
	TOTPRecoveryHashes   []string   `json:"-"`
	PasswordHash         string     `json:"-"`
	EmailVerified        bool       `json:"email_verified"`
	EmailVerifyHash      string     `json:"-"`
	EmailVerifyExpiresAt *time.Time `json:"-"`
	// EmailVerifySentAt 记录上一次发送验证邮件的时间，用于重发冷却。
	EmailVerifySentAt      *time.Time `json:"-"`
	PasswordResetHash      string     `json:"-"`
	PasswordResetExpiresAt *time.Time `json:"-"`
	// PasswordResetSentAt 记录上一次签发密码重置邮件的时间，用于跨实例冷却。
	PasswordResetSentAt *time.Time `json:"-"`
	SessionVersion      int64      `json:"-"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type AuthIdentity struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Issuer    string    `json:"issuer"`
	Subject   string    `json:"subject"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AccountProfile struct {
	ID               string         `json:"id"`
	Email            string         `json:"email"`
	DisplayName      string         `json:"display_name"`
	AvatarDataURL    string         `json:"avatar_data_url,omitempty"`
	Status           string         `json:"status"`
	Role             string         `json:"role"`
	ConcurrencyLimit int            `json:"concurrency_limit"`
	RPMLimit         int            `json:"rpm_limit"`
	ExternalIssuer   string         `json:"external_issuer,omitempty"`
	AuthIdentities   []AuthIdentity `json:"auth_identities"`
	EmailVerified    bool           `json:"email_verified"`
	PasswordEnabled  bool           `json:"password_enabled"`
	TOTPEnabled      bool           `json:"totp_enabled"`
	ManagedByConfig  bool           `json:"managed_by_config"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type AccountProfileUpdateRequest struct {
	DisplayName   string `json:"display_name"`
	AvatarDataURL string `json:"avatar_data_url"`
}

type AccountPasswordUpdateRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type WorkspaceUserDefaults struct {
	ConcurrencyLimit int
	RPMLimit         int
}

type WorkspaceUserRequest struct {
	Email        string  `json:"email"`
	DisplayName  string  `json:"display_name"`
	Status       string  `json:"status"`
	Role         string  `json:"role"`
	DepartmentID *string `json:"department_id"`
}

type RoleBinding struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	ScopeType string    `json:"scope_type"`
	ScopeID   string    `json:"scope_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type RoleBindingRequest struct {
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
}
