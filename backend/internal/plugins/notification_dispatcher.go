package plugins

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

type deliveryResult struct {
	Status     string
	Target     string
	HTTPStatus int
	Error      string
	Attempted  bool
}

func (s *Service) DispatchAlert(ctx context.Context, event controlplane.AlertEvent) error {
	if event.Status != controlplane.AlertStatusActive {
		return nil
	}
	plugins, err := s.repo.ListPlugins(ctx)
	if err != nil {
		return err
	}
	for _, plugin := range plugins {
		if plugin.Category != "notification" || plugin.Status != StatusEnabled {
			continue
		}
		if !hasNotificationWebhookAdapter(plugin.ID) {
			continue
		}
		result := s.dispatchWebhookNotification(ctx, plugin, event)
		if result.Attempted {
			_ = s.recordDeliveryAttempt(ctx, plugin, event, result)
		}
		if result.Error != "" {
			return errors.New(result.Error)
		}
	}
	return nil
}

func (s *Service) DeliveryAttempts(ctx context.Context, query DeliveryQuery) ([]DeliveryAttempt, error) {
	if strings.TrimSpace(query.PluginID) != "" {
		plugin, ok, err := s.repo.FindPlugin(ctx, strings.TrimSpace(query.PluginID))
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrPluginNotFound
		}
		query.PluginID = plugin.ID
	}
	return s.repo.QueryDeliveryAttempts(ctx, query)
}

func (s *Service) dispatchWebhookNotification(ctx context.Context, plugin Plugin, event controlplane.AlertEvent) deliveryResult {
	record, ok, err := s.repo.FindConfig(ctx, plugin.ID)
	if err != nil {
		return failedDelivery(plugin.ID, err)
	}
	if !ok {
		return failedDelivery(plugin.ID, errors.New("notification plugin is not configured"))
	}
	if !alertEventAllowed(record.Settings, event) {
		return deliveryResult{Status: DeliveryStatusSkipped, Target: plugin.ID, Attempted: false}
	}
	req, target, err := s.buildNotificationWebhookRequest(ctx, plugin, record, event)
	if err != nil {
		return failedDelivery(target, err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return failedDelivery(target, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return deliveryResult{Status: DeliveryStatusFailed, Target: target, HTTPStatus: resp.StatusCode, Error: fmt.Sprintf("notification delivery failed with HTTP %d", resp.StatusCode), Attempted: true}
	}
	return deliveryResult{Status: DeliveryStatusSucceeded, Target: target, HTTPStatus: resp.StatusCode, Attempted: true}
}

func (s *Service) recordDeliveryAttempt(ctx context.Context, plugin Plugin, event controlplane.AlertEvent, result deliveryResult) error {
	status := result.Status
	if status == "" {
		status = DeliveryStatusFailed
	}
	return s.repo.SaveDeliveryAttempt(ctx, DeliveryAttempt{
		ID:            "delivery_" + randomID(12),
		PluginID:      plugin.ID,
		AlertID:       event.ID,
		AlertType:     event.Type,
		AlertSeverity: event.Severity,
		Status:        status,
		Target:        result.Target,
		HTTPStatus:    result.HTTPStatus,
		Error:         trimForStorage(result.Error, 500),
		CreatedAt:     time.Now().UTC(),
	})
}

func failedDelivery(target string, err error) deliveryResult {
	message := ""
	if err != nil {
		message = err.Error()
	}
	return deliveryResult{Status: DeliveryStatusFailed, Target: target, Error: message, Attempted: true}
}

func alertEventAllowed(settings map[string]string, event controlplane.AlertEvent) bool {
	minSeverity := strings.TrimSpace(settings["min_severity"])
	if minSeverity == "" {
		minSeverity = controlplane.AlertSeverityWarning
	}
	if severityRank(event.Severity) < severityRank(minSeverity) {
		return false
	}
	types := cleanStringList(strings.Split(settings["alert_types"], ","))
	if len(types) == 0 {
		return true
	}
	for _, alertType := range types {
		if alertType == event.Type {
			return true
		}
	}
	return false
}

func severityRank(value string) int {
	switch strings.TrimSpace(value) {
	case controlplane.AlertSeverityInfo:
		return 1
	case controlplane.AlertSeverityWarning:
		return 2
	case controlplane.AlertSeverityCritical:
		return 3
	default:
		return 0
	}
}
