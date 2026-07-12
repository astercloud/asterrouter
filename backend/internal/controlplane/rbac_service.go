package controlplane

import (
	"context"
	"strings"
)

const (
	PermissionAdminRead      = "admin:read"
	PermissionAdminWrite     = "admin:write"
	PermissionAdminAudit     = "admin:audit"
	PermissionPluginManage   = "plugins:manage"
	PermissionExportManage   = "exports:manage"
	PermissionSystemManage   = "system:manage"
	PermissionSettingsManage = "settings:manage"
)

type PrincipalAccess struct {
	Actor        string   `json:"actor"`
	Role         string   `json:"role"`
	Global       bool     `json:"global"`
	Permissions  []string `json:"permissions"`
	ResolvedFrom string   `json:"resolved_from"`
}

func (s *Service) PrincipalAccess(ctx context.Context, actor string) (PrincipalAccess, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "local-admin"
	}
	if isLocalAdminActor(actor) {
		return PrincipalAccess{
			Actor:        actor,
			Role:         RoleSuperAdmin,
			Global:       true,
			Permissions:  permissionsForRole(RoleSuperAdmin),
			ResolvedFrom: "local_admin",
		}, nil
	}
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return PrincipalAccess{}, err
	}
	user, ok := workspaceUserByActor(users, actor)
	if !ok || user.Status != WorkspaceUserStatusActive {
		return PrincipalAccess{Actor: actor, Role: RoleDeveloper, ResolvedFrom: "unmatched"}, nil
	}
	access := PrincipalAccess{
		Actor:        actor,
		Role:         user.Role,
		Global:       true,
		Permissions:  permissionsForRole(user.Role),
		ResolvedFrom: "workspace_user",
	}
	bindings, err := s.repo.ListRoleBindings(ctx)
	if err != nil {
		return PrincipalAccess{}, err
	}
	for _, binding := range bindings {
		if binding.UserID != user.ID {
			continue
		}
		access.Permissions = mergePermissions(access.Permissions, permissionsForRole(binding.Role))
		if roleRank(binding.Role) > roleRank(access.Role) {
			access.Role = binding.Role
		}
		if binding.ScopeType == RoleScopeGlobal {
			access.Global = true
		}
	}
	return access, nil
}

func (s *Service) ActorCan(ctx context.Context, actor string, permission string) (bool, PrincipalAccess, error) {
	access, err := s.PrincipalAccess(ctx, actor)
	if err != nil {
		return false, PrincipalAccess{}, err
	}
	if !access.Global {
		return false, access, nil
	}
	return contains(access.Permissions, permission), access, nil
}

func permissionsForRole(role string) []string {
	switch role {
	case RoleSuperAdmin:
		return []string{
			PermissionAdminRead,
			PermissionAdminWrite,
			PermissionAdminAudit,
			PermissionPluginManage,
			PermissionExportManage,
			PermissionSystemManage,
			PermissionSettingsManage,
		}
	case RolePlatformAdmin:
		return []string{
			PermissionAdminRead,
			PermissionAdminWrite,
			PermissionAdminAudit,
			PermissionPluginManage,
			PermissionExportManage,
			PermissionSettingsManage,
		}
	case RoleKeyManager:
		return []string{PermissionAdminRead, PermissionExportManage}
	case RoleReadOnlyAuditor:
		return []string{PermissionAdminRead, PermissionAdminAudit, PermissionExportManage}
	case RoleDeveloper:
		return []string{}
	default:
		return []string{}
	}
}

func mergePermissions(current []string, next []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(current)+len(next))
	for _, permission := range append(current, next...) {
		if _, ok := seen[permission]; ok {
			continue
		}
		seen[permission] = struct{}{}
		out = append(out, permission)
	}
	return out
}

func roleRank(role string) int {
	switch role {
	case RoleSuperAdmin:
		return 5
	case RolePlatformAdmin:
		return 4
	case RoleKeyManager:
		return 3
	case RoleReadOnlyAuditor:
		return 2
	case RoleDeveloper:
		return 1
	default:
		return 0
	}
}
