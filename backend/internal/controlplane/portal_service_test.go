package controlplane

import (
	"context"
	"strings"
	"testing"
)

func TestPortalWorkspaceFiltersByProjectRoleBinding(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	if err := svc.EnsureSeedData(ctx); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	project, err := svc.CreateProject(ctx, "tester", ProjectRequest{
		Name:       "Scoped Project",
		CostCenter: "DEV",
		Status:     ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProject(): %v", err)
	}
	app, err := svc.CreateApplication(ctx, "tester", ApplicationRequest{
		ProjectID:   project.ID,
		Name:        "Scoped App",
		Environment: "dev",
		Owner:       "dev@example.com",
		Status:      ApplicationStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateApplication(): %v", err)
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
	if _, err := svc.CreateRoleBinding(ctx, "tester", RoleBindingRequest{
		UserID:    user.ID,
		Role:      RoleDeveloper,
		ScopeType: RoleScopeProject,
		ScopeID:   project.ID,
	}); err != nil {
		t.Fatalf("CreateRoleBinding(): %v", err)
	}
	if _, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "Platform key",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		MonthlyTokenLimit: 0,
	}); err != nil {
		t.Fatalf("CreateAPIKey platform: %v", err)
	}
	created, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		ProjectID:         project.ID,
		ApplicationID:     app.ID,
		Name:              "Scoped key",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey scoped: %v", err)
	}
	auth, err := svc.AuthorizeGatewayModel(ctx, created.Key, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "gpt-4o-mini", Status: "forwarded", InputTokens: 3, OutputTokens: 2, CostCents: 7}); err != nil {
		t.Fatalf("RecordGatewayUsage(): %v", err)
	}
	if err := svc.RecordGatewayTrace(ctx, auth, GatewayTraceInput{Model: "gpt-4o-mini", Status: "forwarded", ResponseSummary: "ok"}); err != nil {
		t.Fatalf("RecordGatewayTrace(): %v", err)
	}

	workspace, err := svc.PortalWorkspace(ctx, "dev@example.com")
	if err != nil {
		t.Fatalf("PortalWorkspace(): %v", err)
	}
	if len(workspace.Projects) != 1 || workspace.Projects[0].ID != project.ID {
		t.Fatalf("portal projects not scoped: %+v", workspace.Projects)
	}
	if len(workspace.Applications) != 1 || workspace.Applications[0].ID != app.ID {
		t.Fatalf("portal apps not scoped: %+v", workspace.Applications)
	}
	if len(workspace.APIKeys) != 1 || workspace.APIKeys[0].ProjectID != project.ID {
		t.Fatalf("portal keys not scoped: %+v", workspace.APIKeys)
	}
	if workspace.Usage.TotalRequests != 1 || len(workspace.RecentTraces) != 1 || !workspace.CanManageKeys {
		t.Fatalf("portal usage or permission mismatch: %+v", workspace)
	}
}

func TestPortalAPIKeyManagementRequiresProjectAccess(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	if err := svc.EnsureSeedData(ctx); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	project, err := svc.CreateProject(ctx, "tester", ProjectRequest{Name: "Portal Project", Status: ProjectStatusActive})
	if err != nil {
		t.Fatalf("CreateProject(): %v", err)
	}
	app, err := svc.CreateApplication(ctx, "tester", ApplicationRequest{ProjectID: project.ID, Name: "Portal App", Environment: "dev", Owner: "dev@example.com", Status: ApplicationStatusActive})
	if err != nil {
		t.Fatalf("CreateApplication(): %v", err)
	}
	user, err := svc.CreateWorkspaceUser(ctx, "tester", WorkspaceUserRequest{Email: "dev@example.com", Status: WorkspaceUserStatusActive, Role: RoleDeveloper})
	if err != nil {
		t.Fatalf("CreateWorkspaceUser(): %v", err)
	}
	if _, err := svc.CreateRoleBinding(ctx, "tester", RoleBindingRequest{UserID: user.ID, Role: RoleDeveloper, ScopeType: RoleScopeProject, ScopeID: project.ID}); err != nil {
		t.Fatalf("CreateRoleBinding(): %v", err)
	}

	if _, err := svc.CreatePortalAPIKey(ctx, "dev@example.com", APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "Bad key",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		MonthlyTokenLimit: 0,
	}); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected project access error, got %v", err)
	}
	created, err := svc.CreatePortalAPIKey(ctx, "dev@example.com", APIKeyCreateRequest{
		ProjectID:         project.ID,
		ApplicationID:     app.ID,
		Name:              "Good key",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreatePortalAPIKey(): %v", err)
	}
	if created.Record.ProjectID != project.ID || created.Key == "" {
		t.Fatalf("portal key mismatch: %+v", created)
	}
	if _, err := svc.RotatePortalAPIKey(ctx, "dev@example.com", created.Record.ID); err != nil {
		t.Fatalf("RotatePortalAPIKey(): %v", err)
	}
	if err := svc.DisablePortalAPIKey(ctx, "dev@example.com", created.Record.ID); err != nil {
		t.Fatalf("DisablePortalAPIKey(): %v", err)
	}
}
