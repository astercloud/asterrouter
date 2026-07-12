package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestAdminRBACAllowsGlobalAuditorReadAndBlocksWrites(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{AdminToken: "secret"})
	user, err := control.CreateWorkspaceUser(context.Background(), "tester", controlplane.WorkspaceUserRequest{
		Email:  "auditor@example.com",
		Status: controlplane.WorkspaceUserStatusActive,
		Role:   controlplane.RoleReadOnlyAuditor,
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceUser(): %v", err)
	}
	if _, err := control.CreateRoleBinding(context.Background(), "tester", controlplane.RoleBindingRequest{
		UserID:    user.ID,
		Role:      controlplane.RoleReadOnlyAuditor,
		ScopeType: controlplane.RoleScopeGlobal,
	}); err != nil {
		t.Fatalf("CreateRoleBinding(): %v", err)
	}

	readReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs", nil)
	readReq.Header.Set("Authorization", "Bearer secret")
	readReq.Header.Set("X-Actor", "auditor@example.com")
	readRec := httptest.NewRecorder()
	handler.ServeHTTP(readRec, readReq)
	if readRec.Code != http.StatusOK {
		t.Fatalf("auditor read status=%d body=%s", readRec.Code, readRec.Body.String())
	}

	writeReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewBufferString(`{"site_name":"Blocked"}`))
	writeReq.Header.Set("Authorization", "Bearer secret")
	writeReq.Header.Set("X-Actor", "auditor@example.com")
	writeReq.Header.Set("Content-Type", "application/json")
	writeRec := httptest.NewRecorder()
	handler.ServeHTTP(writeRec, writeReq)
	if writeRec.Code != http.StatusForbidden {
		t.Fatalf("auditor write should be forbidden status=%d body=%s", writeRec.Code, writeRec.Body.String())
	}
}

func TestAdminRBACBlocksDeveloperButPortalStillWorks(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{AdminToken: "secret"})
	user, err := control.CreateWorkspaceUser(context.Background(), "tester", controlplane.WorkspaceUserRequest{
		Email:  "dev@example.com",
		Status: controlplane.WorkspaceUserStatusActive,
		Role:   controlplane.RoleDeveloper,
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceUser(): %v", err)
	}
	if _, err := control.CreateRoleBinding(context.Background(), "tester", controlplane.RoleBindingRequest{
		UserID:    user.ID,
		Role:      controlplane.RoleDeveloper,
		ScopeType: controlplane.RoleScopeGlobal,
	}); err != nil {
		t.Fatalf("CreateRoleBinding(): %v", err)
	}

	adminReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard", nil)
	adminReq.Header.Set("Authorization", "Bearer secret")
	adminReq.Header.Set("X-Actor", "dev@example.com")
	adminRec := httptest.NewRecorder()
	handler.ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusForbidden {
		t.Fatalf("developer admin should be forbidden status=%d body=%s", adminRec.Code, adminRec.Body.String())
	}

	portalReq := httptest.NewRequest(http.MethodGet, "/api/v1/portal/workspace", nil)
	portalReq.Header.Set("Authorization", "Bearer secret")
	portalReq.Header.Set("X-Actor", "dev@example.com")
	portalRec := httptest.NewRecorder()
	handler.ServeHTTP(portalRec, portalReq)
	if portalRec.Code != http.StatusOK {
		t.Fatalf("developer portal should work status=%d body=%s", portalRec.Code, portalRec.Body.String())
	}
}

func TestAdminRBACProtectsPluginAndSystemWrites(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{AdminToken: "secret"})
	user, err := control.CreateWorkspaceUser(context.Background(), "tester", controlplane.WorkspaceUserRequest{
		Email:  "auditor@example.com",
		Status: controlplane.WorkspaceUserStatusActive,
		Role:   controlplane.RoleReadOnlyAuditor,
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceUser(): %v", err)
	}
	if _, err := control.CreateRoleBinding(context.Background(), "tester", controlplane.RoleBindingRequest{
		UserID:    user.ID,
		Role:      controlplane.RoleReadOnlyAuditor,
		ScopeType: controlplane.RoleScopeGlobal,
	}); err != nil {
		t.Fatalf("CreateRoleBinding(): %v", err)
	}

	for _, target := range []string{"/api/v1/admin/plugins/catalog-sync", "/api/v1/admin/system/update"} {
		req := httptest.NewRequest(http.MethodPost, target, nil)
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("X-Actor", "auditor@example.com")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s should be forbidden status=%d body=%s", target, rec.Code, rec.Body.String())
		}
	}
}
