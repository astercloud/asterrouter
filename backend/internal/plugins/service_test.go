package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestServiceSeedsBuiltinPluginCatalog(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}

	catalog, err := svc.Catalog(context.Background())
	if err != nil {
		t.Fatalf("Catalog(): %v", err)
	}

	if catalog.Summary.Total < 10 {
		t.Fatalf("expected built-in plugin catalog, got %+v", catalog.Summary)
	}
	if catalog.Summary.Enabled == 0 || catalog.Summary.PaidLocked == 0 {
		t.Fatalf("summary does not reflect enabled and locked plugins: %+v", catalog.Summary)
	}
}

func TestServiceFiltersCatalogAndActionsBySurface(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository())
	if err := svc.EnsureSeedData(ctx); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	personal, err := svc.CatalogForSurface(ctx, "personal")
	if err != nil {
		t.Fatalf("CatalogForSurface(personal): %v", err)
	}
	for _, plugin := range personal.Plugins {
		if plugin.ID == "com.asterrouter.enterprise.audit-baseline" {
			t.Fatal("enterprise-only plugin leaked into personal catalog")
		}
	}
	if err := svc.RequireSurface(ctx, "com.asterrouter.enterprise.audit-baseline", "personal"); !errors.Is(err, ErrPluginSurface) {
		t.Fatalf("RequireSurface(personal) = %v, want ErrPluginSurface", err)
	}
	if err := svc.RequireSurface(ctx, "com.asterrouter.enterprise.audit-baseline", "enterprise"); err != nil {
		t.Fatalf("RequireSurface(enterprise): %v", err)
	}
}

func TestServiceEnablesFreeCorePlugin(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}

	plugin, err := svc.Enable(context.Background(), "com.asterrouter.notification.webhook")
	if err != nil {
		t.Fatalf("Enable(): %v", err)
	}
	if plugin.Status != StatusEnabled {
		t.Fatalf("status = %q", plugin.Status)
	}
}

func TestServiceRejectsLockedPaidPlugin(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}

	_, err := svc.Enable(context.Background(), "com.asterrouter.notification.slack")
	if !errors.Is(err, ErrPluginLocked) {
		t.Fatalf("err = %v, want ErrPluginLocked", err)
	}
}

func TestServiceRejectsDisablingCorePlugin(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}

	_, err := svc.Disable(context.Background(), "com.asterrouter.core.gateway")
	if !errors.Is(err, ErrPluginCoreRequired) {
		t.Fatalf("err = %v, want ErrPluginCoreRequired", err)
	}
}

func TestServiceRejectsConfiguringLockedPaidPlugin(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}

	_, err := svc.UpdateConfig(context.Background(), "com.asterrouter.notification.slack", ConfigRequest{
		Secrets: map[string]string{"webhook_url": "https://example.com/slack"},
	})
	if !errors.Is(err, ErrPluginLocked) {
		t.Fatalf("err = %v, want ErrPluginLocked", err)
	}
}

func TestServiceConfigEncryptsSecretsAndDispatchesWebhookAlert(t *testing.T) {
	ctx := context.Background()
	var delivered struct {
		PluginID string                  `json:"plugin_id"`
		Event    controlplane.AlertEvent `json:"event"`
	}
	var authHeader string
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		authHeader = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&delivered); err != nil {
			t.Fatalf("decode webhook: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer webhook.Close()

	repo := NewMemoryRepository()
	svc := NewService(repo, "test-secret")
	if err := svc.EnsureSeedData(ctx); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	if _, err := svc.Enable(ctx, "com.asterrouter.notification.webhook"); err != nil {
		t.Fatalf("Enable(): %v", err)
	}
	config, err := svc.UpdateConfig(ctx, "com.asterrouter.notification.webhook", ConfigRequest{
		Settings: map[string]string{
			"min_severity": "warning",
			"alert_types":  "api_key_quota,api_key_quota",
		},
		Secrets: map[string]string{
			"webhook_url":  webhook.URL,
			"bearer_token": "secret-token",
		},
	})
	if err != nil {
		t.Fatalf("UpdateConfig(): %v", err)
	}
	if config.SecretHints["webhook_url"] == "" || config.SecretHints["webhook_url"] == webhook.URL {
		t.Fatalf("webhook secret hint not masked: %+v", config.SecretHints)
	}
	record, ok, err := repo.FindConfig(ctx, "com.asterrouter.notification.webhook")
	if err != nil || !ok {
		t.Fatalf("FindConfig() ok=%v err=%v", ok, err)
	}
	if record.SecretCiphertexts["webhook_url"] == webhook.URL {
		t.Fatalf("webhook URL stored in plaintext")
	}

	event := controlplane.AlertEvent{
		ID:           "alert_test",
		Type:         controlplane.AlertTypeAPIKeyQuota,
		Severity:     controlplane.AlertSeverityWarning,
		Status:       controlplane.AlertStatusActive,
		Title:        "Budget warning",
		Summary:      "Budget warning summary",
		ResourceType: "project",
		ResourceID:   "proj_test",
		DedupeKey:    "api_key_quota:proj_test:2026-07",
		FirstSeenAt:  time.Now().UTC(),
		LastSeenAt:   time.Now().UTC(),
	}
	if err := svc.DispatchAlert(ctx, event); err != nil {
		t.Fatalf("DispatchAlert(): %v", err)
	}
	if delivered.PluginID != "com.asterrouter.notification.webhook" || delivered.Event.ID != event.ID {
		t.Fatalf("webhook payload mismatch: %+v", delivered)
	}
	if authHeader != "Bearer secret-token" {
		t.Fatalf("authorization header = %q", authHeader)
	}
	deliveries, err := svc.DeliveryAttempts(ctx, DeliveryQuery{PluginID: "com.asterrouter.notification.webhook"})
	if err != nil {
		t.Fatalf("DeliveryAttempts(): %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].Status != DeliveryStatusSucceeded || deliveries[0].HTTPStatus != http.StatusAccepted || deliveries[0].AlertID != event.ID {
		t.Fatalf("delivery record mismatch: %+v", deliveries)
	}
}

func TestServiceDispatchesChannelSpecificWebhookPayloads(t *testing.T) {
	cases := []struct {
		name     string
		pluginID string
		secrets  map[string]string
		assert   func(t *testing.T, payload map[string]any, query url.Values)
	}{
		{
			name:     "slack",
			pluginID: notificationSlackPluginID,
			assert: func(t *testing.T, payload map[string]any, query url.Values) {
				t.Helper()
				if text := stringPayloadValue(t, payload, "text"); !strings.Contains(text, "Budget warning") {
					t.Fatalf("slack text = %q", text)
				}
				if _, ok := payload["blocks"].([]any); !ok {
					t.Fatalf("slack blocks missing: %+v", payload)
				}
			},
		},
		{
			name:     "lark",
			pluginID: notificationLarkPluginID,
			secrets:  map[string]string{"signing_secret": "lark-secret"},
			assert: func(t *testing.T, payload map[string]any, query url.Values) {
				t.Helper()
				if msgType := stringPayloadValue(t, payload, "msg_type"); msgType != "text" {
					t.Fatalf("lark msg_type = %q", msgType)
				}
				if timestamp := stringPayloadValue(t, payload, "timestamp"); timestamp == "" {
					t.Fatalf("lark timestamp missing: %+v", payload)
				}
				if sign := stringPayloadValue(t, payload, "sign"); sign == "" {
					t.Fatalf("lark sign missing: %+v", payload)
				}
				content, ok := payload["content"].(map[string]any)
				if !ok || !strings.Contains(stringPayloadValue(t, content, "text"), "Budget warning") {
					t.Fatalf("lark content mismatch: %+v", payload)
				}
			},
		},
		{
			name:     "wecom",
			pluginID: notificationWeComPluginID,
			assert: func(t *testing.T, payload map[string]any, query url.Values) {
				t.Helper()
				if msgType := stringPayloadValue(t, payload, "msgtype"); msgType != "markdown" {
					t.Fatalf("wecom msgtype = %q", msgType)
				}
				markdown, ok := payload["markdown"].(map[string]any)
				if !ok || !strings.Contains(stringPayloadValue(t, markdown, "content"), "Budget warning") {
					t.Fatalf("wecom markdown mismatch: %+v", payload)
				}
			},
		},
		{
			name:     "dingtalk",
			pluginID: notificationDingTalkPluginID,
			secrets:  map[string]string{"signing_secret": "dingtalk-secret"},
			assert: func(t *testing.T, payload map[string]any, query url.Values) {
				t.Helper()
				if msgType := stringPayloadValue(t, payload, "msgtype"); msgType != "markdown" {
					t.Fatalf("dingtalk msgtype = %q", msgType)
				}
				if query.Get("timestamp") == "" || query.Get("sign") == "" {
					t.Fatalf("dingtalk signature query missing: %s", query.Encode())
				}
				markdown, ok := payload["markdown"].(map[string]any)
				if !ok || !strings.Contains(stringPayloadValue(t, markdown, "text"), "Budget warning") {
					t.Fatalf("dingtalk markdown mismatch: %+v", payload)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			var delivered map[string]any
			var deliveredQuery url.Values
			webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Fatalf("method = %s", r.Method)
				}
				if contentType := r.Header.Get("Content-Type"); !strings.Contains(contentType, "application/json") {
					t.Fatalf("content-type = %q", contentType)
				}
				deliveredQuery = r.URL.Query()
				if err := json.NewDecoder(r.Body).Decode(&delivered); err != nil {
					t.Fatalf("decode webhook: %v", err)
				}
				w.WriteHeader(http.StatusAccepted)
			}))
			defer webhook.Close()

			repo := NewMemoryRepository()
			svc := NewService(repo, "test-secret")
			if err := svc.EnsureSeedData(ctx); err != nil {
				t.Fatalf("EnsureSeedData(): %v", err)
			}
			enableSeededNotificationPlugin(t, ctx, repo, tc.pluginID)

			secrets := map[string]string{"webhook_url": webhook.URL}
			for key, value := range tc.secrets {
				secrets[key] = value
			}
			if _, err := svc.UpdateConfig(ctx, tc.pluginID, ConfigRequest{
				Settings: map[string]string{"min_severity": "info"},
				Secrets:  secrets,
			}); err != nil {
				t.Fatalf("UpdateConfig(): %v", err)
			}

			event := testAlertEvent()
			if err := svc.DispatchAlert(ctx, event); err != nil {
				t.Fatalf("DispatchAlert(): %v", err)
			}
			tc.assert(t, delivered, deliveredQuery)

			deliveries, err := svc.DeliveryAttempts(ctx, DeliveryQuery{PluginID: tc.pluginID})
			if err != nil {
				t.Fatalf("DeliveryAttempts(): %v", err)
			}
			if len(deliveries) != 1 || deliveries[0].Status != DeliveryStatusSucceeded || deliveries[0].AlertID != event.ID {
				t.Fatalf("delivery record mismatch: %+v", deliveries)
			}
		})
	}
}

func enableSeededNotificationPlugin(t *testing.T, ctx context.Context, repo *MemoryRepository, pluginID string) {
	t.Helper()
	plugin, ok, err := repo.FindPlugin(ctx, pluginID)
	if err != nil || !ok {
		t.Fatalf("FindPlugin(%s) ok=%v err=%v", pluginID, ok, err)
	}
	plugin.Status = StatusEnabled
	plugin.EntitlementStatus = EntitlementIncluded
	plugin.UpdatedAt = time.Now().UTC()
	if err := repo.SavePlugin(ctx, plugin); err != nil {
		t.Fatalf("SavePlugin(%s): %v", pluginID, err)
	}
}

func testAlertEvent() controlplane.AlertEvent {
	now := time.Now().UTC()
	return controlplane.AlertEvent{
		ID:           "alert_test",
		Type:         controlplane.AlertTypeAPIKeyQuota,
		Severity:     controlplane.AlertSeverityWarning,
		Status:       controlplane.AlertStatusActive,
		Title:        "Budget warning",
		Summary:      "Budget warning summary",
		ResourceType: "project",
		ResourceID:   "proj_test",
		DedupeKey:    "api_key_quota:proj_test:2026-07",
		FirstSeenAt:  now,
		LastSeenAt:   now,
	}
}

func stringPayloadValue(t *testing.T, payload map[string]any, key string) string {
	t.Helper()
	value, ok := payload[key].(string)
	if !ok {
		t.Fatalf("payload[%s] is not string: %+v", key, payload)
	}
	return value
}
