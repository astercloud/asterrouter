package plugins

import (
	"fmt"
	"time"
)

func builtinPlugins(now time.Time) []Plugin {
	return []Plugin{
		builtin("com.asterrouter.core.gateway", "Gateway Core", "OpenAI-compatible gateway, API key validation, provider forwarding, and audit hooks.", "core", "backend", TierCore, StatusEnabled, EntitlementIncluded, []string{"personal", "relay_operator", "enterprise"}, "", false, now),
		builtin("com.asterrouter.core.plugin-host", "Plugin Host", "Built-in plugin registry, contribution metadata, entitlement gates, and plugin audit events.", "core", "backend", TierCore, StatusEnabled, EntitlementIncluded, []string{"personal", "relay_operator", "enterprise"}, "/plugins", false, now),
		builtin("com.asterrouter.core.update-manager", "System Update Manager", "Version check, release manifest matching, checksum validation, rollback, and restart orchestration.", "operations", "backend", TierCore, StatusEnabled, EntitlementIncluded, []string{"personal", "relay_operator", "enterprise"}, "/settings", true, now),
		builtin("com.asterrouter.provider.openai-compatible", "OpenAI-compatible Provider", "Register compatible provider connections and forward chat completion traffic.", "provider", "backend", TierFreeCore, StatusEnabled, EntitlementFree, []string{"personal", "relay_operator", "enterprise"}, "/providers", true, now),
		builtin("com.asterrouter.notification.webhook", "Generic Webhook Notification", "Send budget, provider health, and policy alerts to a generic webhook endpoint.", "notification", "integration", TierFreeCore, StatusDisabled, EntitlementFree, []string{"personal", "relay_operator", "enterprise"}, "/plugins", true, now),
		builtin("com.asterrouter.notification.email", "Email Notification", "Deliver basic administrative notifications through SMTP or managed email configuration.", "notification", "integration", TierFreeCore, StatusDisabled, EntitlementFree, []string{"personal", "relay_operator", "enterprise"}, "/plugins", true, now),
		builtin("com.asterrouter.enterprise.audit-baseline", "Audit Baseline", "Core audit search and export-ready event structure for governance review.", "governance", "backend", TierProfileBundle, StatusEnabled, EntitlementIncluded, []string{"enterprise"}, "/audit", false, now),
		builtin("com.asterrouter.notification.slack", "Slack Notification", "Slack app and incoming webhook delivery for enterprise alert routing.", "notification", "integration", TierPaidAddon, StatusLocked, EntitlementMissing, []string{"personal", "relay_operator", "enterprise"}, "/plugins", true, now),
		builtin("com.asterrouter.notification.lark", "Feishu / Lark Notification", "Feishu and Lark bot delivery for alert routing and approval workflows.", "notification", "integration", TierPaidAddon, StatusLocked, EntitlementMissing, []string{"personal", "relay_operator", "enterprise"}, "/plugins", true, now),
		builtin("com.asterrouter.notification.wecom", "WeCom Notification", "Enterprise WeChat notification channel for private deployments.", "notification", "integration", TierPaidAddon, StatusLocked, EntitlementMissing, []string{"personal", "relay_operator", "enterprise"}, "/plugins", true, now),
		builtin("com.asterrouter.notification.dingtalk", "DingTalk Notification", "DingTalk robot delivery for operational and governance alerts.", "notification", "integration", TierPaidAddon, StatusLocked, EntitlementMissing, []string{"personal", "relay_operator", "enterprise"}, "/plugins", true, now),
		builtin("com.asterrouter.provider-trust.evidence", "Provider Trust Evidence", "Evidence collection foundation for model authenticity, dispute reports, and provider risk scoring.", "data_service", "backend", TierPaidAddon, StatusLocked, EntitlementMissing, []string{"personal", "relay_operator", "enterprise"}, "/plugins", true, now),
		builtin("com.asterrouter.finops.chargeback", "FinOps Chargeback", "Advanced allocation, chargeback, and budget anomaly reporting.", "finops", "backend", TierPaidAddon, StatusLocked, EntitlementMissing, []string{"enterprise"}, "/plugins", true, now),
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
