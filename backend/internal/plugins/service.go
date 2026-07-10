package plugins

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrPluginNotFound     = errors.New("plugin not found")
	ErrPluginLocked       = errors.New("plugin entitlement is missing")
	ErrPluginCoreRequired = errors.New("core plugin cannot be disabled")
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) EnsureSeedData(ctx context.Context) error {
	existing, err := s.repo.ListPlugins(ctx)
	if err != nil {
		return err
	}
	existingByID := map[string]Plugin{}
	for _, plugin := range existing {
		existingByID[plugin.ID] = plugin
	}
	now := time.Now().UTC()
	for _, plugin := range builtinPlugins(now) {
		targetStatus := plugin.Status
		if current, ok := existingByID[plugin.ID]; ok {
			plugin.Status = current.Status
			if plugin.Tier == TierPaidAddon && plugin.EntitlementStatus == EntitlementMissing {
				plugin.Status = StatusLocked
			}
			targetStatus = plugin.Status
			plugin.CreatedAt = current.CreatedAt
		}
		if err := s.repo.SavePlugin(ctx, plugin); err != nil {
			return err
		}
		if err := s.repo.UpdateStatus(ctx, plugin.ID, targetStatus, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Catalog(ctx context.Context) (Catalog, error) {
	plugins, err := s.repo.ListPlugins(ctx)
	if err != nil {
		return Catalog{}, err
	}
	return Catalog{Summary: summarize(plugins), Plugins: plugins}, nil
}

func (s *Service) Enable(ctx context.Context, id string) (Plugin, error) {
	plugin, ok, err := s.repo.FindPlugin(ctx, strings.TrimSpace(id))
	if err != nil {
		return Plugin{}, err
	}
	if !ok {
		return Plugin{}, ErrPluginNotFound
	}
	if plugin.Tier == TierPaidAddon && plugin.EntitlementStatus == EntitlementMissing {
		return Plugin{}, ErrPluginLocked
	}
	now := time.Now().UTC()
	if err := s.repo.UpdateStatus(ctx, plugin.ID, StatusEnabled, now); err != nil {
		return Plugin{}, err
	}
	plugin.Status = StatusEnabled
	plugin.UpdatedAt = now
	return plugin, nil
}

func (s *Service) Disable(ctx context.Context, id string) (Plugin, error) {
	plugin, ok, err := s.repo.FindPlugin(ctx, strings.TrimSpace(id))
	if err != nil {
		return Plugin{}, err
	}
	if !ok {
		return Plugin{}, ErrPluginNotFound
	}
	if plugin.Tier == TierCore {
		return Plugin{}, ErrPluginCoreRequired
	}
	now := time.Now().UTC()
	if err := s.repo.UpdateStatus(ctx, plugin.ID, StatusDisabled, now); err != nil {
		return Plugin{}, err
	}
	plugin.Status = StatusDisabled
	plugin.UpdatedAt = now
	return plugin, nil
}

func (s *Service) Health(ctx context.Context) error {
	return s.repo.Health(ctx)
}

func summarize(plugins []Plugin) Summary {
	var out Summary
	out.Total = len(plugins)
	for _, plugin := range plugins {
		if plugin.Status == StatusEnabled {
			out.Enabled++
		}
		if plugin.Tier == TierCore || plugin.Tier == TierFreeCore {
			out.Free++
		}
		if plugin.Tier == TierPaidAddon && plugin.Status == StatusLocked {
			out.PaidLocked++
		}
		if plugin.Configurable {
			out.Configurable++
		}
	}
	return out
}

func builtinPlugins(now time.Time) []Plugin {
	return []Plugin{
		builtin("com.asterrouter.core.gateway", "Gateway Core", "OpenAI-compatible gateway, API key validation, provider forwarding, and audit hooks.", "core", "backend", TierCore, StatusEnabled, EntitlementIncluded, []string{"admin", "portal"}, "", false, now),
		builtin("com.asterrouter.core.plugin-host", "Plugin Host", "Built-in plugin registry, contribution metadata, entitlement gates, and plugin audit events.", "core", "backend", TierCore, StatusEnabled, EntitlementIncluded, []string{"admin"}, "/admin/plugins", false, now),
		builtin("com.asterrouter.core.update-manager", "System Update Manager", "Version check, release manifest matching, checksum validation, rollback, and restart orchestration.", "operations", "backend", TierCore, StatusEnabled, EntitlementIncluded, []string{"admin"}, "/admin/settings", true, now),
		builtin("com.asterrouter.provider.openai-compatible", "OpenAI-compatible Provider", "Register compatible provider connections and forward chat completion traffic.", "provider", "backend", TierFreeCore, StatusEnabled, EntitlementFree, []string{"admin"}, "/admin/providers", true, now),
		builtin("com.asterrouter.notification.webhook", "Generic Webhook Notification", "Send budget, provider health, and policy alerts to a generic webhook endpoint.", "notification", "integration", TierFreeCore, StatusDisabled, EntitlementFree, []string{"admin"}, "/admin/plugins", true, now),
		builtin("com.asterrouter.notification.email", "Email Notification", "Deliver basic administrative notifications through SMTP or managed email configuration.", "notification", "integration", TierFreeCore, StatusDisabled, EntitlementFree, []string{"admin"}, "/admin/plugins", true, now),
		builtin("com.asterrouter.enterprise.audit-baseline", "Audit Baseline", "Core audit search and export-ready event structure for governance review.", "governance", "backend", TierProfileBundle, StatusEnabled, EntitlementIncluded, []string{"admin"}, "/admin/audit", false, now),
		builtin("com.asterrouter.notification.slack", "Slack Notification", "Slack app and incoming webhook delivery for enterprise alert routing.", "notification", "integration", TierPaidAddon, StatusLocked, EntitlementMissing, []string{"admin"}, "/admin/plugins", true, now),
		builtin("com.asterrouter.notification.lark", "Feishu / Lark Notification", "Feishu and Lark bot delivery for alert routing and approval workflows.", "notification", "integration", TierPaidAddon, StatusLocked, EntitlementMissing, []string{"admin"}, "/admin/plugins", true, now),
		builtin("com.asterrouter.notification.wecom", "WeCom Notification", "Enterprise WeChat notification channel for private deployments.", "notification", "integration", TierPaidAddon, StatusLocked, EntitlementMissing, []string{"admin"}, "/admin/plugins", true, now),
		builtin("com.asterrouter.notification.dingtalk", "DingTalk Notification", "DingTalk robot delivery for operational and governance alerts.", "notification", "integration", TierPaidAddon, StatusLocked, EntitlementMissing, []string{"admin"}, "/admin/plugins", true, now),
		builtin("com.asterrouter.provider-trust.evidence", "Provider Trust Evidence", "Evidence collection foundation for model authenticity, dispute reports, and provider risk scoring.", "data_service", "backend", TierPaidAddon, StatusLocked, EntitlementMissing, []string{"admin"}, "/admin/plugins", true, now),
		builtin("com.asterrouter.finops.chargeback", "FinOps Chargeback", "Advanced allocation, chargeback, and budget anomaly reporting.", "finops", "backend", TierPaidAddon, StatusLocked, EntitlementMissing, []string{"admin"}, "/admin/plugins", true, now),
	}
}

func builtin(id, name, description, category, pluginType, tier, status, entitlement string, surfaces []string, entryPoint string, configurable bool, now time.Time) Plugin {
	if id == "" {
		panic(fmt.Sprintf("builtin plugin %q has empty id", name))
	}
	return Plugin{
		ID:                id,
		PluginID:          id,
		Name:              name,
		Description:       description,
		Category:          category,
		Type:              pluginType,
		Tier:              tier,
		Version:           "0.1.0",
		Vendor:            "AsterRouter",
		Status:            status,
		EntitlementStatus: entitlement,
		Surfaces:          surfaces,
		EntryPoint:        entryPoint,
		Configurable:      configurable,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}
