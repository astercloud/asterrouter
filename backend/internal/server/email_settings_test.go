package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/settings"
)

func TestEmailTemplateRoutesExposeLowercaseCatalogAndIndependentUpdates(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{AdminToken: "secret"})
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	defaults := request(http.MethodGet, "/api/v1/console/settings/email-templates/defaults", "")
	if defaults.Code != http.StatusOK || !strings.Contains(defaults.Body.String(), `"event":"email_verification"`) || strings.Contains(defaults.Body.String(), `"Event"`) {
		t.Fatalf("defaults status=%d body=%s", defaults.Code, defaults.Body.String())
	}

	catalogResponse := request(http.MethodGet, "/api/v1/console/settings/email-templates", "")
	if catalogResponse.Code != http.StatusOK {
		t.Fatalf("catalog status=%d body=%s", catalogResponse.Code, catalogResponse.Body.String())
	}
	var catalogEnvelope struct {
		Data settings.EmailTemplateCatalog `json:"data"`
	}
	if err := json.Unmarshal(catalogResponse.Body.Bytes(), &catalogEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(catalogEnvelope.Data.Events) != 3 || len(catalogEnvelope.Data.Locales) != 2 {
		t.Fatalf("catalog = %+v", catalogEnvelope.Data)
	}

	updated := request(http.MethodPut, "/api/v1/console/settings/email-templates/quota_limit/en-US", `{"subject":"Custom {{.SiteName}}","html":"<p>{{.Limit}}</p>"}`)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"customized":true`) {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	loaded := request(http.MethodGet, "/api/v1/console/settings/email-templates/quota_limit/en-US", "")
	if loaded.Code != http.StatusOK || !strings.Contains(loaded.Body.String(), "Custom") {
		t.Fatalf("get status=%d body=%s", loaded.Code, loaded.Body.String())
	}
	restored := request(http.MethodPost, "/api/v1/console/settings/email-templates/quota_limit/en-US/restore", "")
	if restored.Code != http.StatusOK || !strings.Contains(restored.Body.String(), `"customized":false`) {
		t.Fatalf("restore status=%d body=%s", restored.Code, restored.Body.String())
	}
	invalid := request(http.MethodPut, "/api/v1/console/settings/email-templates/quota_limit/en-US", `{"subject":"{{.Unknown}}","html":"<p>body</p>"}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestEmailTemplateRoutesRequireAuthentication(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{AdminToken: "secret"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/settings/email-templates", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSMTPConnectionRouteUsesRequestConfiguration(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{AdminToken: "secret"})
	body := bytes.NewBufferString(`{"smtp_host":"127.0.0.1","smtp_port":1,"smtp_username":"unsaved","smtp_password":"unsaved-secret","smtp_use_tls":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/console/settings/smtp/test-connection", body)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("connection test status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response httpx.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != 1502 {
		t.Fatalf("connection test response=%+v", response)
	}
}
