package controlplane

import "time"

const (
	WorkspaceUserStatusActive   = "active"
	WorkspaceUserStatusDisabled = "disabled"

	RoleSuperAdmin      = "super_admin"
	RolePlatformAdmin   = "platform_admin"
	RoleProjectAdmin    = "project_admin"
	RoleReadOnlyAuditor = "read_only_auditor"
	RoleDeveloper       = "developer"

	RoleScopeGlobal  = "global"
	RoleScopeProject = "project"
)

type WorkspaceUser struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"display_name"`
	Status       string    `json:"status"`
	Role         string    `json:"role"`
	ProjectCount int       `json:"project_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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
