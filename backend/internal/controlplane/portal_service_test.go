package controlplane

import (
	"context"
	"testing"
)

func TestPortalWorkspaceUsesWorkspaceLevelAccess(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	if err := svc.EnsureSeedData(ctx); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	if _, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name:           "Shared Workspace Key",
		ModelAllowlist: []string{"gpt-4o-mini"},
	}); err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	user, err := svc.CreateWorkspaceUser(ctx, "tester", WorkspaceUserRequest{
		Email:       "dev@example.com",
		DisplayName: "Developer",
		Status:      WorkspaceUserStatusActive,
		Role:        RoleDeveloper,
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceUser(): %v", err)
	}
	workspace, err := svc.PortalWorkspace(ctx, user.Email)
	if err != nil {
		t.Fatalf("PortalWorkspace(): %v", err)
	}
	if workspace.CanManageKeys {
		t.Fatal("developer must not manage shared workspace keys")
	}
	if len(workspace.APIKeys) != 1 || workspace.APIKeys[0].Name != "Shared Workspace Key" {
		t.Fatalf("unexpected portal keys: %+v", workspace.APIKeys)
	}
}

func TestPortalKeyManagementRequiresKeyManagerRole(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	user, err := svc.CreateWorkspaceUser(ctx, "tester", WorkspaceUserRequest{
		Email:       "keys@example.com",
		DisplayName: "Key Manager",
		Status:      WorkspaceUserStatusActive,
		Role:        RoleKeyManager,
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceUser(): %v", err)
	}
	created, err := svc.CreatePortalAPIKey(ctx, user.Email, APIKeyCreateRequest{
		Name:           "CLI Key",
		ModelAllowlist: []string{"gpt-4o-mini"},
	})
	if err != nil {
		t.Fatalf("CreatePortalAPIKey(): %v", err)
	}
	if created.Key == "" || created.Record.Name != "CLI Key" {
		t.Fatalf("unexpected created key: %+v", created)
	}
	if _, err := svc.RotatePortalAPIKey(ctx, user.Email, created.Record.ID); err != nil {
		t.Fatalf("RotatePortalAPIKey(): %v", err)
	}
	if err := svc.DisablePortalAPIKey(ctx, user.Email, created.Record.ID); err != nil {
		t.Fatalf("DisablePortalAPIKey(): %v", err)
	}
}
