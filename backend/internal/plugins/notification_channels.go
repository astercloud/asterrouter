package plugins

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

const (
	notificationWebhookPluginID  = "com.asterrouter.notification.webhook"
	notificationSlackPluginID    = "com.asterrouter.notification.slack"
	notificationLarkPluginID     = "com.asterrouter.notification.lark"
	notificationWeComPluginID    = "com.asterrouter.notification.wecom"
	notificationDingTalkPluginID = "com.asterrouter.notification.dingtalk"
)

func hasNotificationWebhookAdapter(pluginID string) bool {
	switch pluginID {
	case notificationWebhookPluginID, notificationSlackPluginID, notificationLarkPluginID, notificationWeComPluginID, notificationDingTalkPluginID:
		return true
	default:
		return false
	}
}

func (s *Service) buildNotificationWebhookRequest(ctx context.Context, plugin Plugin, record configRecord, event controlplane.AlertEvent) (*http.Request, string, error) {
	endpoint, target, err := s.notificationEndpoint(record)
	if err != nil {
		return nil, plugin.ID, err
	}
	now := time.Now().UTC()
	var payload any
	switch plugin.ID {
	case notificationWebhookPluginID:
		payload = map[string]any{
			"plugin_id":    plugin.ID,
			"delivered_at": now,
			"event":        event,
		}
	case notificationSlackPluginID:
		payload = slackAlertPayload(event)
	case notificationLarkPluginID:
		payload, err = s.larkAlertPayload(record, event, now)
	case notificationWeComPluginID:
		payload = weComAlertPayload(event)
	case notificationDingTalkPluginID:
		endpoint, err = s.signedDingTalkEndpoint(record, endpoint, now)
		payload = dingTalkAlertPayload(event)
	default:
		err = fmt.Errorf("%w: unsupported notification plugin %s", ErrPluginConfigInvalid, plugin.ID)
	}
	if err != nil {
		return nil, target, err
	}
	req, err := newJSONWebhookRequest(ctx, endpoint.String(), payload)
	if err != nil {
		return nil, target, err
	}
	if plugin.ID == notificationWebhookPluginID {
		token, err := s.decryptConfigSecret(record, "bearer_token")
		if err != nil {
			return nil, target, err
		}
		if strings.TrimSpace(token) != "" {
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
		}
	}
	return req, target, nil
}

func (s *Service) notificationEndpoint(record configRecord) (*url.URL, string, error) {
	webhookURL, err := s.decryptConfigSecret(record, "webhook_url")
	if err != nil {
		return nil, "webhook", err
	}
	if strings.TrimSpace(webhookURL) == "" {
		return nil, "webhook", errorsWithConfig("webhook_url is not configured")
	}
	endpoint, err := url.Parse(strings.TrimSpace(webhookURL))
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, "webhook", errorsWithConfig("webhook_url must be an HTTP or HTTPS URL")
	}
	return endpoint, endpoint.Scheme + "://" + endpoint.Host, nil
}

func newJSONWebhookRequest(ctx context.Context, endpoint string, payload any) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AsterRouter-AlertDispatcher/0.1")
	return req, nil
}

func slackAlertPayload(event controlplane.AlertEvent) map[string]any {
	return map[string]any{
		"text": alertPlainText(event),
		"blocks": []map[string]any{
			{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": alertMarkdown(event),
				},
			},
		},
	}
}

func (s *Service) larkAlertPayload(record configRecord, event controlplane.AlertEvent, now time.Time) (map[string]any, error) {
	payload := map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": alertPlainText(event)},
	}
	secret, err := s.decryptConfigSecret(record, "signing_secret")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(secret) != "" {
		timestamp := strconv.FormatInt(now.Unix(), 10)
		payload["timestamp"] = timestamp
		payload["sign"] = larkSignature(timestamp, secret)
	}
	return payload, nil
}

func weComAlertPayload(event controlplane.AlertEvent) map[string]any {
	return map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]string{"content": alertMarkdown(event)},
	}
}

func dingTalkAlertPayload(event controlplane.AlertEvent) map[string]any {
	return map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": alertTitle(event),
			"text":  alertMarkdown(event),
		},
		"at": map[string]any{"isAtAll": false},
	}
}

func (s *Service) signedDingTalkEndpoint(record configRecord, endpoint *url.URL, now time.Time) (*url.URL, error) {
	secret, err := s.decryptConfigSecret(record, "signing_secret")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(secret) == "" {
		return endpoint, nil
	}
	signed := *endpoint
	query := signed.Query()
	timestamp := strconv.FormatInt(now.UnixMilli(), 10)
	query.Set("timestamp", timestamp)
	query.Set("sign", dingTalkSignature(timestamp, secret))
	signed.RawQuery = query.Encode()
	return &signed, nil
}

func alertTitle(event controlplane.AlertEvent) string {
	if strings.TrimSpace(event.Title) != "" {
		return strings.TrimSpace(event.Title)
	}
	return event.Type
}

func alertPlainText(event controlplane.AlertEvent) string {
	parts := []string{
		fmt.Sprintf("[%s] %s", strings.ToUpper(event.Severity), alertTitle(event)),
		strings.TrimSpace(event.Summary),
		fmt.Sprintf("Type: %s", event.Type),
		fmt.Sprintf("Resource: %s/%s", event.ResourceType, event.ResourceID),
		fmt.Sprintf("Alert ID: %s", event.ID),
	}
	return strings.Join(cleanStringList(parts), "\n")
}

func alertMarkdown(event controlplane.AlertEvent) string {
	parts := []string{
		fmt.Sprintf("**[%s] %s**", strings.ToUpper(event.Severity), alertTitle(event)),
		strings.TrimSpace(event.Summary),
		fmt.Sprintf("- Type: `%s`", event.Type),
		fmt.Sprintf("- Resource: `%s/%s`", event.ResourceType, event.ResourceID),
		fmt.Sprintf("- Alert ID: `%s`", event.ID),
	}
	return strings.Join(cleanStringList(parts), "\n")
}

func larkSignature(timestamp string, secret string) string {
	stringToSign := timestamp + "\n" + strings.TrimSpace(secret)
	mac := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func dingTalkSignature(timestamp string, secret string) string {
	secret = strings.TrimSpace(secret)
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func errorsWithConfig(message string) error {
	return fmt.Errorf("%w: %s", ErrPluginConfigInvalid, message)
}
