package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestAdminAlertEndpoints(t *testing.T) {
	ctx := context.Background()
	handler, control := newTestRuntime(t, RuntimeConfig{})
	created, err := control.CreateAPIKey(ctx, "tester", controlplane.APIKeyCreateRequest{
		Name:              "HTTP alert key",
		ModelAllowlist:    []string{"gpt-alert"},
		QPSLimit:          0,
		MonthlyTokenLimit: 100,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := control.AuthorizeGatewayModel(ctx, created.Key, "gpt-alert")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := control.RecordGatewayUsage(ctx, auth, controlplane.GatewayUsageInput{Model: "gpt-alert", Status: "forwarded", InputTokens: 100}); err != nil {
		t.Fatalf("RecordGatewayUsage(): %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/alerts?type=api_key_quota&status=active", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Data []controlplane.AlertEvent `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Data) != 1 || listResp.Data[0].Severity != controlplane.AlertSeverityCritical {
		t.Fatalf("alert list mismatch: %+v", listResp.Data)
	}
	alertID := listResp.Data[0].ID

	summaryReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/alerts/summary?type=api_key_quota&status=active", nil)
	summaryRec := httptest.NewRecorder()
	handler.ServeHTTP(summaryRec, summaryReq)
	if summaryRec.Code != http.StatusOK {
		t.Fatalf("summary status = %d body=%s", summaryRec.Code, summaryRec.Body.String())
	}
	var summaryResp struct {
		Data controlplane.AlertSummary `json:"data"`
	}
	if err := json.Unmarshal(summaryRec.Body.Bytes(), &summaryResp); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summaryResp.Data.Total != 1 || summaryResp.Data.Critical != 1 {
		t.Fatalf("summary mismatch: %+v", summaryResp.Data)
	}

	ackReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/alerts/"+alertID+"/acknowledge", nil)
	ackRec := httptest.NewRecorder()
	handler.ServeHTTP(ackRec, ackReq)
	if ackRec.Code != http.StatusOK {
		t.Fatalf("ack status = %d body=%s", ackRec.Code, ackRec.Body.String())
	}
	var ackResp struct {
		Data controlplane.AlertEvent `json:"data"`
	}
	if err := json.Unmarshal(ackRec.Body.Bytes(), &ackResp); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if ackResp.Data.Status != controlplane.AlertStatusAcknowledged {
		t.Fatalf("ack mismatch: %+v", ackResp.Data)
	}

	repeatAckReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/alerts/"+alertID+"/acknowledge", nil)
	repeatAckRec := httptest.NewRecorder()
	handler.ServeHTTP(repeatAckRec, repeatAckReq)
	if repeatAckRec.Code != http.StatusOK || !strings.Contains(repeatAckRec.Body.String(), `"status":"acknowledged"`) {
		t.Fatalf("repeat ack status = %d body=%s", repeatAckRec.Code, repeatAckRec.Body.String())
	}

	resolveReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/alerts/"+alertID+"/resolve", nil)
	resolveRec := httptest.NewRecorder()
	handler.ServeHTTP(resolveRec, resolveReq)
	if resolveRec.Code != http.StatusOK {
		t.Fatalf("resolve status = %d body=%s", resolveRec.Code, resolveRec.Body.String())
	}
	var resolveResp struct {
		Data controlplane.AlertEvent `json:"data"`
	}
	if err := json.Unmarshal(resolveRec.Body.Bytes(), &resolveResp); err != nil {
		t.Fatalf("decode resolve: %v", err)
	}
	if resolveResp.Data.Status != controlplane.AlertStatusResolved {
		t.Fatalf("resolve mismatch: %+v", resolveResp.Data)
	}

	repeatResolveReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/alerts/"+alertID+"/resolve", nil)
	repeatResolveRec := httptest.NewRecorder()
	handler.ServeHTTP(repeatResolveRec, repeatResolveReq)
	if repeatResolveRec.Code != http.StatusOK || !strings.Contains(repeatResolveRec.Body.String(), `"status":"resolved"`) {
		t.Fatalf("repeat resolve status = %d body=%s", repeatResolveRec.Code, repeatResolveRec.Body.String())
	}

	resolvedAckReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/alerts/"+alertID+"/acknowledge", nil)
	resolvedAckRec := httptest.NewRecorder()
	handler.ServeHTTP(resolvedAckRec, resolvedAckReq)
	if resolvedAckRec.Code != http.StatusBadRequest || !strings.Contains(resolvedAckRec.Body.String(), `"code":1520`) || !strings.Contains(resolvedAckRec.Body.String(), "resolved alert cannot be acknowledged") {
		t.Fatalf("resolved alert ack status = %d body=%s", resolvedAckRec.Code, resolvedAckRec.Body.String())
	}

	missingResolveReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/alerts/alert_missing/resolve", nil)
	missingResolveRec := httptest.NewRecorder()
	handler.ServeHTTP(missingResolveRec, missingResolveReq)
	if missingResolveRec.Code != http.StatusBadRequest || !strings.Contains(missingResolveRec.Body.String(), `"code":1521`) || !strings.Contains(missingResolveRec.Body.String(), `alert \"alert_missing\" not found`) {
		t.Fatalf("missing alert resolve status = %d body=%s", missingResolveRec.Code, missingResolveRec.Body.String())
	}
}
