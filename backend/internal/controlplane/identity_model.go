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

	RoleScopeGlobal = "global"
)

type WorkspaceUser struct {
	ID                     string     `json:"id"`
	Email                  string     `json:"email"`
	DisplayName            string     `json:"display_name"`
	Status                 string     `json:"status"`
	Role                   string     `json:"role"`
	ExternalIssuer         string     `json:"external_issuer,omitempty"`
	ExternalSubject        string     `json:"external_subject,omitempty"`
	DepartmentID           string     `json:"department_id,omitempty"`
	TOTPEnabled            bool       `json:"totp_enabled"`
	TOTPSecretCiphertext   string     `json:"-"`
	TOTPRecoveryHashes     []string   `json:"-"`
	PasswordHash           string     `json:"-"`
	EmailVerified          bool       `json:"email_verified"`
	EmailVerifyHash        string     `json:"-"`
	EmailVerifyExpiresAt   *time.Time `json:"-"`
	PasswordResetHash      string     `json:"-"`
	PasswordResetExpiresAt *time.Time `json:"-"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type WorkspaceUserRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
	Role        string `json:"role"`
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
