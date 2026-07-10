package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Service) ListWorkspaceUsers(ctx context.Context) ([]WorkspaceUser, error) {
	return s.repo.ListWorkspaceUsers(ctx)
}

func (s *Service) CreateWorkspaceUser(ctx context.Context, actor string, req WorkspaceUserRequest) (WorkspaceUser, error) {
	now := time.Now().UTC()
	user, err := workspaceUserFromRequest(req, now)
	if err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.ensureUniqueUserEmail(ctx, "", user.Email); err != nil {
		return WorkspaceUser{}, err
	}
	user.ID = "usr_" + randomID(10)
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.audit(ctx, actor, "create", "workspace_user", user.ID, fmt.Sprintf("Created workspace user %s", user.Email)); err != nil {
		return WorkspaceUser{}, err
	}
	return user, nil
}

func (s *Service) UpdateWorkspaceUser(ctx context.Context, actor string, id string, req WorkspaceUserRequest) (WorkspaceUser, error) {
	existing, err := s.workspaceUserByID(ctx, id)
	if err != nil {
		return WorkspaceUser{}, err
	}
	user, err := workspaceUserFromRequest(req, existing.CreatedAt)
	if err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.ensureUniqueUserEmail(ctx, existing.ID, user.Email); err != nil {
		return WorkspaceUser{}, err
	}
	user.ID = existing.ID
	user.CreatedAt = existing.CreatedAt
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.audit(ctx, actor, "update", "workspace_user", user.ID, fmt.Sprintf("Updated workspace user %s", user.Email)); err != nil {
		return WorkspaceUser{}, err
	}
	return user, nil
}

func (s *Service) ListRoleBindings(ctx context.Context) ([]RoleBinding, error) {
	return s.repo.ListRoleBindings(ctx)
}

func (s *Service) CreateRoleBinding(ctx context.Context, actor string, req RoleBindingRequest) (RoleBinding, error) {
	now := time.Now().UTC()
	binding, err := s.roleBindingFromRequest(ctx, req, now)
	if err != nil {
		return RoleBinding{}, err
	}
	if err := s.ensureUniqueRoleBinding(ctx, binding); err != nil {
		return RoleBinding{}, err
	}
	binding.ID = "rb_" + randomID(10)
	if err := s.repo.SaveRoleBinding(ctx, binding); err != nil {
		return RoleBinding{}, err
	}
	if err := s.audit(ctx, actor, "grant_role", "role_binding", binding.ID, fmt.Sprintf("Granted %s on %s:%s to %s", binding.Role, binding.ScopeType, binding.ScopeID, binding.UserID)); err != nil {
		return RoleBinding{}, err
	}
	return binding, nil
}

func (s *Service) DeleteRoleBinding(ctx context.Context, actor string, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("role binding id is required")
	}
	binding, err := s.roleBindingByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteRoleBinding(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("role binding %s not found", id)
		}
		return err
	}
	return s.audit(ctx, actor, "revoke_role", "role_binding", binding.ID, fmt.Sprintf("Revoked %s on %s:%s from %s", binding.Role, binding.ScopeType, binding.ScopeID, binding.UserID))
}

func workspaceUserFromRequest(req WorkspaceUserRequest, createdAt time.Time) (WorkspaceUser, error) {
	now := time.Now().UTC()
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		return WorkspaceUser{}, errors.New("valid user email is required")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = WorkspaceUserStatusActive
	}
	if status != WorkspaceUserStatusActive && status != WorkspaceUserStatusDisabled {
		return WorkspaceUser{}, errors.New("invalid user status")
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = RoleDeveloper
	}
	if !validRole(role) {
		return WorkspaceUser{}, errors.New("invalid user role")
	}
	if createdAt.IsZero() {
		createdAt = now
	}
	return WorkspaceUser{
		Email:       email,
		DisplayName: strings.TrimSpace(req.DisplayName),
		Status:      status,
		Role:        role,
		CreatedAt:   createdAt,
		UpdatedAt:   now,
	}, nil
}

func (s *Service) roleBindingFromRequest(ctx context.Context, req RoleBindingRequest, createdAt time.Time) (RoleBinding, error) {
	now := time.Now().UTC()
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		return RoleBinding{}, errors.New("user id is required")
	}
	if _, err := s.workspaceUserByID(ctx, userID); err != nil {
		return RoleBinding{}, err
	}
	role := strings.TrimSpace(req.Role)
	if !validRole(role) {
		return RoleBinding{}, errors.New("invalid role")
	}
	scopeType := strings.TrimSpace(req.ScopeType)
	if scopeType == "" {
		scopeType = RoleScopeProject
	}
	scopeID := strings.TrimSpace(req.ScopeID)
	switch scopeType {
	case RoleScopeGlobal:
		scopeID = ""
	case RoleScopeProject:
		if scopeID == "" {
			return RoleBinding{}, errors.New("project scope id is required")
		}
		if _, err := s.projectByID(ctx, scopeID); err != nil {
			return RoleBinding{}, err
		}
	default:
		return RoleBinding{}, errors.New("invalid role scope")
	}
	if createdAt.IsZero() {
		createdAt = now
	}
	return RoleBinding{
		UserID:    userID,
		Role:      role,
		ScopeType: scopeType,
		ScopeID:   scopeID,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}, nil
}

func validRole(role string) bool {
	switch role {
	case RoleSuperAdmin, RolePlatformAdmin, RoleProjectAdmin, RoleReadOnlyAuditor, RoleDeveloper:
		return true
	default:
		return false
	}
}

func (s *Service) workspaceUserByID(ctx context.Context, id string) (WorkspaceUser, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return WorkspaceUser{}, errors.New("user id is required")
	}
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return WorkspaceUser{}, err
	}
	for _, user := range users {
		if user.ID == id {
			return user, nil
		}
	}
	return WorkspaceUser{}, fmt.Errorf("user %s not found", id)
}

func (s *Service) roleBindingByID(ctx context.Context, id string) (RoleBinding, error) {
	bindings, err := s.repo.ListRoleBindings(ctx)
	if err != nil {
		return RoleBinding{}, err
	}
	for _, binding := range bindings {
		if binding.ID == id {
			return binding, nil
		}
	}
	return RoleBinding{}, fmt.Errorf("role binding %s not found", id)
}

func (s *Service) ensureUniqueUserEmail(ctx context.Context, currentID string, email string) error {
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return err
	}
	for _, user := range users {
		if user.Email == email && user.ID != currentID {
			return fmt.Errorf("user email %s already exists", email)
		}
	}
	return nil
}

func (s *Service) ensureUniqueRoleBinding(ctx context.Context, next RoleBinding) error {
	bindings, err := s.repo.ListRoleBindings(ctx)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if binding.UserID == next.UserID && binding.Role == next.Role && binding.ScopeType == next.ScopeType && binding.ScopeID == next.ScopeID {
			return errors.New("role binding already exists")
		}
	}
	return nil
}
