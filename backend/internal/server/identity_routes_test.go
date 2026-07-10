package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestAdminIdentityUserAndRoleBindingEndpoints(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})

	createBody := bytes.NewBufferString(`{"email":"dev@example.com","display_name":"Dev User","status":"active","role":"developer"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create user status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var createResp struct {
		Data controlplane.WorkspaceUser `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create user: %v", err)
	}
	if createResp.Data.ID == "" || createResp.Data.Email != "dev@example.com" || createResp.Data.Role != controlplane.RoleDeveloper {
		t.Fatalf("create user mismatch: %+v", createResp.Data)
	}

	updateBody := bytes.NewBufferString(`{"email":"dev@example.com","display_name":"Developer User","status":"active","role":"project_admin"}`)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/"+createResp.Data.ID, updateBody)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update user status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updateResp struct {
		Data controlplane.WorkspaceUser `json:"data"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updateResp); err != nil {
		t.Fatalf("decode update user: %v", err)
	}
	if updateResp.Data.DisplayName != "Developer User" || updateResp.Data.Role != controlplane.RoleProjectAdmin {
		t.Fatalf("update user mismatch: %+v", updateResp.Data)
	}

	bindingBody := bytes.NewBufferString(`{"user_id":"` + createResp.Data.ID + `","role":"project_admin","scope_type":"project","scope_id":"proj_platform"}`)
	bindingReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/role-bindings", bindingBody)
	bindingReq.Header.Set("Content-Type", "application/json")
	bindingRec := httptest.NewRecorder()
	handler.ServeHTTP(bindingRec, bindingReq)
	if bindingRec.Code != http.StatusOK {
		t.Fatalf("create role binding status = %d body=%s", bindingRec.Code, bindingRec.Body.String())
	}
	var bindingResp struct {
		Data controlplane.RoleBinding `json:"data"`
	}
	if err := json.Unmarshal(bindingRec.Body.Bytes(), &bindingResp); err != nil {
		t.Fatalf("decode role binding: %v", err)
	}
	if bindingResp.Data.UserID != createResp.Data.ID || bindingResp.Data.ScopeID != "proj_platform" {
		t.Fatalf("role binding mismatch: %+v", bindingResp.Data)
	}

	usersReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	usersRec := httptest.NewRecorder()
	handler.ServeHTTP(usersRec, usersReq)
	if usersRec.Code != http.StatusOK {
		t.Fatalf("list users status = %d body=%s", usersRec.Code, usersRec.Body.String())
	}
	var usersResp struct {
		Data []controlplane.WorkspaceUser `json:"data"`
	}
	if err := json.Unmarshal(usersRec.Body.Bytes(), &usersResp); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	if len(usersResp.Data) != 1 || usersResp.Data[0].ProjectCount != 1 {
		t.Fatalf("users list should include project count: %+v", usersResp.Data)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/role-bindings/"+bindingResp.Data.ID, nil)
	deleteRec := httptest.NewRecorder()
	handler.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete binding status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	audit, err := control.ListAuditLogs(context.Background(), 20)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	var seenCreateUser, seenGrant, seenRevoke bool
	for _, event := range audit {
		seenCreateUser = seenCreateUser || event.ResourceType == "workspace_user" && event.Action == "create"
		seenGrant = seenGrant || event.ResourceType == "role_binding" && event.Action == "grant_role"
		seenRevoke = seenRevoke || event.ResourceType == "role_binding" && event.Action == "revoke_role"
	}
	if !seenCreateUser || !seenGrant || !seenRevoke {
		t.Fatalf("identity audit events missing create=%v grant=%v revoke=%v audit=%+v", seenCreateUser, seenGrant, seenRevoke, audit)
	}

	duplicateReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewBufferString(`{"email":"dev@example.com","display_name":"Duplicate","status":"active","role":"developer"}`))
	duplicateReq.Header.Set("Content-Type", "application/json")
	duplicateRec := httptest.NewRecorder()
	handler.ServeHTTP(duplicateRec, duplicateReq)
	if duplicateRec.Code != http.StatusBadRequest || !strings.Contains(duplicateRec.Body.String(), "already exists") {
		t.Fatalf("duplicate user should be rejected status=%d body=%s", duplicateRec.Code, duplicateRec.Body.String())
	}
}
