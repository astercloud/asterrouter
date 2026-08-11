package plugins

import (
	"fmt"
	"time"
)

const OpenAICompatibleProviderPluginID = "com.asterrouter.provider.openai-compatible"

func builtinPlugins(now time.Time) []Plugin {
	return []Plugin{
		builtin("com.asterrouter.core.gateway", "Gateway Core", "OpenAI-compatible gateway, API key validation, provider forwarding, and audit hooks.", "core", "backend", TierCore, StatusEnabled, EntitlementIncluded, "", false, now),
		builtin("com.asterrouter.core.plugin-host", "Plugin Host", "Built-in plugin registry, contribution metadata, entitlement gates, and plugin audit events.", "core", "backend", TierCore, StatusEnabled, EntitlementIncluded, "/console/system/plugins", false, now),
		builtin("com.asterrouter.core.update-manager", "System Update Manager", "Version check, release manifest matching, checksum validation, rollback, and restart orchestration.", "operations", "backend", TierCore, StatusEnabled, EntitlementIncluded, "/console/system", true, now),
		builtin(OpenAICompatibleProviderPluginID, "OpenAI-compatible Provider", "Forward compatible text traffic and execute image generation; video and audio adapters can be installed as provider plugins.", "provider", "backend", TierFreeCore, StatusEnabled, EntitlementFree, "/console/model-services/providers", true, now),
		builtin(ArtifactS3SinkPluginID, "S3-compatible Artifact Delivery", "Deliver generated image, video, and audio artifacts to organization-owned S3, Cloudflare R2, or OSS storage.", "artifact_sink", "integration", TierFreeCore, StatusDisabled, EntitlementFree, "/console/system/plugins", true, now),
		builtin("com.asterrouter.notification.webhook", "Generic Webhook Notification", "Send budget, provider health, and policy alerts to a generic webhook endpoint.", "notification", "integration", TierFreeCore, StatusDisabled, EntitlementFree, "/console/system/plugins", true, now),
		builtin("com.asterrouter.notification.email", "Email Notification", "Deliver basic administrative notifications through SMTP or managed email configuration.", "notification", "integration", TierFreeCore, StatusDisabled, EntitlementFree, "/console/system/plugins", true, now),
		builtin("com.asterrouter.enterprise.audit-baseline", "Audit Baseline", "Core audit search and export-ready event structure for governance review.", "governance", "backend", TierEnterpriseBundle, StatusEnabled, EntitlementIncluded, "/console/system/audit", false, now),
		builtin("com.asterrouter.notification.slack", "Slack Notification", "Slack app and incoming webhook delivery for enterprise alert routing.", "notification", "integration", TierPaidAddon, StatusLocked, EntitlementMissing, "/console/system/plugins", true, now),
		builtin("com.asterrouter.notification.lark", "Feishu / Lark Notification", "Feishu and Lark bot delivery for alert routing and approval workflows.", "notification", "integration", TierPaidAddon, StatusLocked, EntitlementMissing, "/console/system/plugins", true, now),
		builtin("com.asterrouter.notification.wecom", "WeCom Notification", "Enterprise WeChat notification channel for private deployments.", "notification", "integration", TierPaidAddon, StatusLocked, EntitlementMissing, "/console/system/plugins", true, now),
		builtin("com.asterrouter.notification.dingtalk", "DingTalk Notification", "DingTalk robot delivery for operational and governance alerts.", "notification", "integration", TierPaidAddon, StatusLocked, EntitlementMissing, "/console/system/plugins", true, now),
		builtin("com.asterrouter.provider-trust.evidence", "Provider Trust Evidence", "Evidence collection foundation for model authenticity, dispute reports, and provider risk scoring.", "data_service", "backend", TierPaidAddon, StatusLocked, EntitlementMissing, "/console/system/plugins", true, now),
		builtin("com.asterrouter.finops.chargeback", "FinOps Chargeback", "Advanced allocation, chargeback, and budget anomaly reporting.", "finops", "backend", TierPaidAddon, StatusLocked, EntitlementMissing, "/console/usage/cost-allocation", true, now),
	}
}

func builtin(id, name, description, category, pluginType, tier, status, entitlement, entryPoint string, configurable bool, now time.Time) Plugin {
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
		EntryPoint:        entryPoint,
		Configurable:      configurable,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}
