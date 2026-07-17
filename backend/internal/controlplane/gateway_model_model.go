package controlplane

import "time"

const (
	GatewayModelStatusActive   = "active"
	GatewayModelStatusDisabled = "disabled"

	ModelRouteStatusActive   = "active"
	ModelRouteStatusDisabled = "disabled"

	DefaultModelRouteGroup = "default"

	UpstreamFormatOpenAIChat      = "openai_chat"
	UpstreamFormatOpenAIResponses = "openai_responses"
	UpstreamFormatAnthropic       = "anthropic_messages"
	UpstreamFormatGemini          = "gemini_generate_content"
	UpstreamFormatBedrockConverse = "bedrock_converse"
	UpstreamFormatNativeMedia     = "native_media"
)

type GatewayModel struct {
	ID                string    `json:"id"`
	ModelID           string    `json:"model_id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	Modality          string    `json:"modality"`
	DefaultRouteGroup string    `json:"default_route_group"`
	StickyEnabled     bool      `json:"sticky_enabled"`
	StickyTTLSeconds  int       `json:"sticky_ttl_seconds"`
	Status            string    `json:"status"`
	RouteCount        int       `json:"route_count"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type GatewayModelRequest struct {
	ModelID           string `json:"model_id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	Modality          string `json:"modality"`
	DefaultRouteGroup string `json:"default_route_group"`
	StickyEnabled     bool   `json:"sticky_enabled"`
	StickyTTLSeconds  int    `json:"sticky_ttl_seconds"`
	Status            string `json:"status"`
}

type ModelRoute struct {
	ID                string    `json:"id"`
	GatewayModelID    string    `json:"gateway_model_id"`
	RouteGroup        string    `json:"route_group"`
	ProviderAccountID string    `json:"provider_account_id"`
	UpstreamModel     string    `json:"upstream_model"`
	UpstreamFormat    string    `json:"upstream_format"`
	DisabledReason    string    `json:"disabled_reason"`
	Priority          int       `json:"priority"`
	Weight            int       `json:"weight"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type ModelRouteRequest struct {
	GatewayModelID    string `json:"gateway_model_id"`
	RouteGroup        string `json:"route_group"`
	ProviderAccountID string `json:"provider_account_id"`
	UpstreamModel     string `json:"upstream_model"`
	UpstreamFormat    string `json:"upstream_format"`
	Priority          int    `json:"priority"`
	Weight            int    `json:"weight"`
	Status            string `json:"status"`
}

type ModelRouteBulkCreateRequest struct {
	Routes []ModelRouteRequest `json:"routes"`
}

type ModelRouteBulkCreateResult struct {
	Routes []ModelRoute `json:"routes"`
}

type ResolvedGatewayModel struct {
	GatewayModel GatewayModel `json:"gateway_model"`
	RequestedID  string       `json:"requested_id"`
	RouteGroup   string       `json:"route_group"`
}
