package controlplane

import (
	"context"
	"errors"
	"strings"
)

type portalScope struct {
	Actor         string
	CanView       bool
	CanManageKeys bool
}

func (s *Service) PortalWorkspace(ctx context.Context, actor string) (PortalWorkspace, error) {
	scope, err := s.portalScopeForActor(ctx, actor)
	if err != nil {
		return PortalWorkspace{}, err
	}
	if !scope.CanView {
		return PortalWorkspace{}, errors.New("portal principal is not an active workspace user")
	}
	keys, err := s.repo.ListAPIKeys(ctx)
	if err != nil {
		return PortalWorkspace{}, err
	}
	usage, err := s.UsageReportQuery(ctx, UsageQuery{Limit: 20})
	if err != nil {
		return PortalWorkspace{}, err
	}
	traces, err := s.ListGatewayTracesQuery(ctx, GatewayTraceQuery{Limit: 12})
	if err != nil {
		return PortalWorkspace{}, err
	}
	alerts, err := s.ListAlertEventsQuery(ctx, AlertQuery{Limit: 12, Status: AlertStatusActive})
	if err != nil {
		return PortalWorkspace{}, err
	}
	models, err := s.GatewayModels(ctx)
	if err != nil {
		return PortalWorkspace{}, err
	}
	return PortalWorkspace{
		APIKeys:       keys,
		Usage:         usage,
		RecentTraces:  traces,
		Alerts:        alerts,
		Models:        models,
		GatewayPath:   s.gatewayPath,
		CanManageKeys: scope.CanManageKeys,
		Principal:     scope.Actor,
	}, nil
}

func (s *Service) CreatePortalAPIKey(ctx context.Context, actor string, req APIKeyCreateRequest) (APIKeyCreateResponse, error) {
	scope, err := s.portalScopeForActor(ctx, actor)
	if err != nil {
		return APIKeyCreateResponse{}, err
	}
	if !scope.CanManageKeys {
		return APIKeyCreateResponse{}, errors.New("portal principal cannot manage workspace keys")
	}
	return s.CreateAPIKey(ctx, portalActor(scope.Actor), req)
}

func (s *Service) RotatePortalAPIKey(ctx context.Context, actor string, id string) (APIKeyCreateResponse, error) {
	scope, key, err := s.portalAPIKeyAccess(ctx, actor, id)
	if err != nil {
		return APIKeyCreateResponse{}, err
	}
	if !scope.CanManageKeys {
		return APIKeyCreateResponse{}, errors.New("portal principal cannot manage workspace keys")
	}
	return s.RotateAPIKey(ctx, portalActor(scope.Actor), key.ID)
}

func (s *Service) DisablePortalAPIKey(ctx context.Context, actor string, id string) error {
	scope, key, err := s.portalAPIKeyAccess(ctx, actor, id)
	if err != nil {
		return err
	}
	if !scope.CanManageKeys {
		return errors.New("portal principal cannot manage workspace keys")
	}
	return s.DisableAPIKey(ctx, portalActor(scope.Actor), key.ID)
}

func (s *Service) portalAPIKeyAccess(ctx context.Context, actor string, id string) (portalScope, APIKeyRecord, error) {
	scope, err := s.portalScopeForActor(ctx, actor)
	if err != nil {
		return portalScope{}, APIKeyRecord{}, err
	}
	if !scope.CanView {
		return portalScope{}, APIKeyRecord{}, errors.New("portal principal is not an active workspace user")
	}
	key, err := s.apiKeyByID(ctx, id)
	if err != nil {
		return portalScope{}, APIKeyRecord{}, err
	}
	return scope, key, nil
}

func (s *Service) portalScopeForActor(ctx context.Context, actor string) (portalScope, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "local-admin"
	}
	if isLocalAdminActor(actor) {
		return portalScope{Actor: actor, CanView: true, CanManageKeys: true}, nil
	}
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return portalScope{}, err
	}
	user, ok := workspaceUserByActor(users, actor)
	if !ok || user.Status != WorkspaceUserStatusActive {
		return portalScope{Actor: actor}, nil
	}
	scope := portalScope{Actor: actor, CanView: true, CanManageKeys: roleCanManageKeys(user.Role)}
	bindings, err := s.repo.ListRoleBindings(ctx)
	if err != nil {
		return portalScope{}, err
	}
	for _, binding := range bindings {
		if binding.UserID == user.ID && binding.ScopeType == RoleScopeGlobal && roleCanManageKeys(binding.Role) {
			scope.CanManageKeys = true
		}
	}
	return scope, nil
}

func workspaceUserByActor(users []WorkspaceUser, actor string) (WorkspaceUser, bool) {
	actor = strings.ToLower(strings.TrimSpace(actor))
	for _, user := range users {
		if strings.ToLower(user.ID) == actor || strings.ToLower(user.Email) == actor {
			return user, true
		}
	}
	return WorkspaceUser{}, false
}

func roleCanManageKeys(role string) bool {
	switch role {
	case RoleSuperAdmin, RolePlatformAdmin, RoleKeyManager:
		return true
	default:
		return false
	}
}

func isLocalAdminActor(actor string) bool {
	actor = strings.TrimSpace(actor)
	return actor == "" || actor == "local-admin" || actor == "admin" || actor == "demo"
}

func portalActor(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "local-admin"
	}
	return "portal:" + actor
}
