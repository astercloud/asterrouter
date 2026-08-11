package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestSupplyRoutesRequireAuthenticationAndValidateWindow(t *testing.T) {
	handler, _ := newTestRuntime(t, RuntimeConfig{AdminToken: "secret"})

	unauthorized := httptest.NewRequest(http.MethodGet, "/api/v1/supply/utilization", nil)
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorizedResponse.Code, unauthorizedResponse.Body.String())
	}

	invalid := httptest.NewRequest(http.MethodGet, "/api/v1/supply/utilization?from=not-a-time", nil)
	invalid.Header.Set("Authorization", "Bearer secret")
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid window status=%d body=%s", invalidResponse.Code, invalidResponse.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/supply/utilization?window_hours=24", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("utilization status=%d body=%s", response.Code, response.Body.String())
	}
	var utilization struct {
		Data controlplane.SupplyUtilizationReport `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &utilization); err != nil {
		t.Fatalf("decode utilization: %v", err)
	}
	if utilization.Data.Window.DurationSeconds < 23*60*60 || utilization.Data.Window.DurationSeconds > 25*60*60 {
		t.Fatalf("window=%+v", utilization.Data.Window)
	}

	recommendations := httptest.NewRequest(http.MethodGet, "/api/v1/supply/recommendations?window_hours=24", nil)
	recommendations.Header.Set("Authorization", "Bearer secret")
	recommendationsResponse := httptest.NewRecorder()
	handler.ServeHTTP(recommendationsResponse, recommendations)
	if recommendationsResponse.Code != http.StatusOK || !json.Valid(recommendationsResponse.Body.Bytes()) {
		t.Fatalf("recommendations status=%d body=%s", recommendationsResponse.Code, recommendationsResponse.Body.String())
	}
}

func TestSupplyRoutesEnforceUsageReadPermission(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{AdminToken: "secret"})
	developer, err := control.CreateWorkspaceUser(context.Background(), "tester", controlplane.WorkspaceUserRequest{
		Email: "supply-developer@example.test", Status: controlplane.WorkspaceUserStatusActive, Role: controlplane.RoleDeveloper,
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceUser(): %v", err)
	}
	if _, err := control.CreateRoleBinding(context.Background(), "tester", controlplane.RoleBindingRequest{
		UserID: developer.ID, Role: controlplane.RoleDeveloper, ScopeType: controlplane.RoleScopeOrganization,
	}); err != nil {
		t.Fatalf("CreateRoleBinding(): %v", err)
	}

	for _, path := range []string{"/api/v1/supply/utilization", "/api/v1/supply/recommendations"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer secret")
		request.Header.Set("X-Actor", developer.Email)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("developer path=%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}
