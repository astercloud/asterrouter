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
	Actor            string   `json:"actor"`
	Role             string   `json:"role"`
	OrganizationWide bool     `json:"organization_wide"`
	Permissions      []string `json:"permissions"`
	ResolvedFrom     string   `json:"resolved_from"`
	Resource         string   `json:"resource,omitempty"`
	DepartmentIDs    []string `json:"department_ids,omitempty"`
	GroupIDs         []string `json:"group_ids,omitempty"`
	ApplicationIDs   []string `json:"application_ids,omitempty"`
}

func (s *Service) PrincipalAccess(ctx context.Context, actor string) (PrincipalAccess, error) {
	return s.principalAccessForResource(ctx, actor, "")
}

func (s *Service) principalAccessForResource(ctx context.Context, actor string, resource string) (PrincipalAccess, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "local-admin"
	}
	if isLocalAdminActor(actor) {
		return PrincipalAccess{
			Actor:            actor,
			Role:             RoleSuperAdmin,
			OrganizationWide: true,
			Permissions:      permissionsForRole(RoleSuperAdmin, resource),
			ResolvedFrom:     "local_admin",
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
		Actor:            actor,
		Role:             user.Role,
		OrganizationWide: user.Role != RoleDeveloper,
		Permissions:      permissionsForRole(user.Role, resource),
		ResolvedFrom:     "workspace_user",
		Resource:         resource,
	}
	bindings, err := s.repo.ListRoleBindings(ctx)
	if err != nil {
		return PrincipalAccess{}, err
	}
	for _, binding := range bindings {
		if binding.UserID != user.ID {
			continue
		}
		if binding.ScopeType != RoleScopeOrganization && binding.ScopeType != RoleScopeDepartment && binding.ScopeType != RoleScopeGroup && binding.ScopeType != RoleScopeApplication && (binding.ScopeType != RoleScopeResource || binding.ScopeID != resource) {
			continue
		}
		access.Permissions = mergePermissions(access.Permissions, permissionsForRole(binding.Role, resource))
		if roleRank(binding.Role) > roleRank(access.Role) {
			access.Role = binding.Role
		}
		if binding.ScopeType == RoleScopeOrganization {
			access.OrganizationWide = true
		} else if binding.ScopeType == RoleScopeDepartment && !contains(access.DepartmentIDs, binding.ScopeID) {
			access.DepartmentIDs = append(access.DepartmentIDs, binding.ScopeID)
		} else if binding.ScopeType == RoleScopeGroup && !contains(access.GroupIDs, binding.ScopeID) {
			access.GroupIDs = append(access.GroupIDs, binding.ScopeID)
		} else if binding.ScopeType == RoleScopeApplication && !contains(access.ApplicationIDs, binding.ScopeID) {
			access.ApplicationIDs = append(access.ApplicationIDs, binding.ScopeID)
		}
	}
	return access, nil
}

func (s *Service) ActorCan(ctx context.Context, actor string, permission string) (bool, PrincipalAccess, error) {
	return s.ActorCanResource(ctx, actor, permission, "")
}

// ActorIsSystemAdministrator identifies the narrow installation-level authority.
func (s *Service) ActorIsSystemAdministrator(ctx context.Context, actor string) (bool, error) {
	access, err := s.PrincipalAccess(ctx, actor)
	if err != nil {
		return false, err
	}
	return access.OrganizationWide && access.Role == RoleSuperAdmin, nil
}

func (s *Service) ActorCanResource(ctx context.Context, actor string, permission string, resource string) (bool, PrincipalAccess, error) {
	access, err := s.principalAccessForResource(ctx, actor, resource)
	if err != nil {
		return false, PrincipalAccess{}, err
	}
	return contains(access.Permissions, permission), access, nil
}

func permissionsForRole(role string, resource string) []string {
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
		switch resource {
		case RBACResourceAPIKeys:
			return []string{PermissionAdminRead, PermissionAdminWrite}
		case RBACResourceUsage, RBACResourceTraces:
			return []string{PermissionAdminRead}
		default:
			return []string{}
		}
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
