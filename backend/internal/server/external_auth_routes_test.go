package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/settings"
)

func TestGatewayAcceptsSignedExternalAuthContextAndRejectsItFromControlPlane(t *testing.T) {
	handler, control := newApplicationExternalAuthHandler(t)
	ctx := context.Background()
	if _, err := control.CreateGatewayModel(ctx, "tester", controlplane.GatewayModelRequest{ModelID: "model-a", Name: "Model A", Status: controlplane.GatewayModelStatusActive}); err != nil {
		t.Fatal(err)
	}
	application, err := control.CreateApplication(ctx, "operator", controlplane.ApplicationRequest{Name: "Gateway product", Slug: "gateway-product"})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := control.CreateGatewayPrincipal(ctx, "operator", controlplane.GatewayPrincipalRequest{ApplicationID: application.ID, Name: "Gateway backend", PrincipalType: controlplane.GatewayPrincipalTypeIntegration})
	if err != nil {
		t.Fatal(err)
	}
	integration, err := control.CreateExternalAuthIntegration(ctx, "operator", controlplane.ExternalAuthIntegrationRequest{
		ApplicationID: application.ID, GatewayPrincipalID: principal.ID, Name: "Gateway integration", KeyID: "gateway-v1", Audience: "https://gateway.example/v1",
		ModelAllowlist: []string{"model-a"}, QPSLimit: 5, MonthlyTokenLimit: 500, MaxTTLSeconds: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	contextToken := signedGatewayContext(t, controlplane.ExternalAuthContextClaims{
		Version: 1, IntegrationID: integration.Record.ID, KeyID: integration.Record.KeyID, ApplicationID: application.ID,
		SubjectReference: "opaque-subject", Audience: integration.Record.Audience,
		IssuedAt: time.Now().Add(-time.Minute).Unix(), ExpiresAt: time.Now().Add(time.Minute).Unix(),
		ModelAllowlist: []string{"model-a"}, QPSLimit: 2, MonthlyTokenLimit: 100,
	}, integration.Secret)

	modelsReq := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	modelsReq.Header.Set("Authorization", "Aster-Context "+contextToken)
	modelsRec := httptest.NewRecorder()
	handler.ServeHTTP(modelsRec, modelsReq)
	if modelsRec.Code != http.StatusOK || !bytes.Contains(modelsRec.Body.Bytes(), []byte("model-a")) {
		t.Fatalf("models status=%d body=%s", modelsRec.Code, modelsRec.Body.String())
	}

	chatReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"model-a","messages":[{"role":"user","content":"ping"}]}`))
	chatReq.Header.Set("Content-Type", "application/json")
	chatReq.Header.Set("Authorization", "Aster-Context "+contextToken)
	chatRec := httptest.NewRecorder()
	handler.ServeHTTP(chatRec, chatReq)
	if chatRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("chat status=%d body=%s", chatRec.Code, chatRec.Body.String())
	}
	usage, err := control.UsageReportQuery(ctx, controlplane.UsageQuery{ExternalAuthIntegrationID: integration.Record.ID})
	if err != nil || len(usage.Recent) != 1 || usage.Recent[0].ExternalSubjectReference != "opaque-subject" {
		t.Fatalf("gateway evidence usage=%+v err=%v", usage, err)
	}

	controlReq := httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil)
	controlReq.Header.Set("Authorization", "Aster-Context "+contextToken)
	controlRec := httptest.NewRecorder()
	handler.ServeHTTP(controlRec, controlReq)
	if controlRec.Code != http.StatusUnauthorized {
		t.Fatalf("delegated context reached control plane status=%d body=%s", controlRec.Code, controlRec.Body.String())
	}
}

func TestApplicationExternalIntegrationRoutesReturnSecretOnlyOnce(t *testing.T) {
	handler, control := newApplicationExternalAuthHandler(t)
	ctx := context.Background()
	application, err := control.CreateApplication(ctx, "operator", controlplane.ApplicationRequest{Name: "Route product", Slug: "route-product"})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := control.CreateGatewayPrincipal(ctx, "operator", controlplane.GatewayPrincipalRequest{ApplicationID: application.ID, Name: "Route backend", PrincipalType: controlplane.GatewayPrincipalTypeIntegration})
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"principal_id":"` + principal.ID + `","name":"Route integration","key_id":"route-v1","audience":"https://gateway.example/v1","model_allowlist":["model-a"],"qps_limit":1,"monthly_token_limit":10,"max_ttl_seconds":300}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/applications/"+application.ID+"/external-integrations", bytes.NewBufferString(payload))
	createReq.Header.Set("Authorization", "Bearer secret")
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var createResponse struct {
		Data controlplane.ExternalAuthIntegrationCreateResponse `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResponse); err != nil || createResponse.Data.Secret == "" || createResponse.Data.Record.SecretCiphertext != "" {
		t.Fatalf("create response=%+v err=%v", createResponse, err)
	}
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/applications/"+application.ID+"/external-integrations", nil)
	listReq.Header.Set("Authorization", "Bearer secret")
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || bytes.Contains(listRec.Body.Bytes(), []byte(createResponse.Data.Secret)) {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	rotateReq := httptest.NewRequest(http.MethodPost, "/api/v1/applications/"+application.ID+"/external-integrations/"+createResponse.Data.Record.ID+"/rotate-secret", nil)
	rotateReq.Header.Set("Authorization", "Bearer secret")
	rotateRec := httptest.NewRecorder()
	handler.ServeHTTP(rotateRec, rotateReq)
	var rotateResponse struct {
		Data controlplane.ExternalAuthIntegrationCreateResponse `json:"data"`
	}
	if rotateRec.Code != http.StatusOK || json.Unmarshal(rotateRec.Body.Bytes(), &rotateResponse) != nil || rotateResponse.Data.Secret == "" || rotateResponse.Data.Secret == createResponse.Data.Secret {
		t.Fatalf("rotate status=%d body=%s", rotateRec.Code, rotateRec.Body.String())
	}
}

func newApplicationExternalAuthHandler(t *testing.T) (http.Handler, *controlplane.Service) {
	t.Helper()
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{
		Version: "test", StorageMode: "memory",
	})
	control := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	return New(Options{Runtime: RuntimeConfig{AdminToken: "secret"}, SettingsService: settingsService, ControlService: control}), control
}

func signedGatewayContext(t testing.TB, claims controlplane.ExternalAuthContextClaims, secret string) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
