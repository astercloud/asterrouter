package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/textproto"
	"strings"

	"golang.org/x/net/http/httpguts"
)

// ProviderAccountModelMapping describes an account-local model alias. The
// gateway model route remains the source of truth for route selection; this
// mapping only changes the model sent to the selected upstream account.
type ProviderAccountModelMapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ProviderAccountRuntimeSettings contains UI-level account policies stored in
// adapter_config[ProviderAccountAdapterConfigSettings]. Provider-specific
// adapter credentials stay in the outer adapter_config map.
type ProviderAccountRuntimeSettings struct {
	BaseURL               string                        `json:"base_url"`
	ModelRestrictionMode  string                        `json:"model_restriction_mode"`
	ModelMappings         []ProviderAccountModelMapping `json:"model_mappings"`
	HeaderOverrideEnabled bool                          `json:"header_override_enabled"`
	HeaderOverrideJSON    string                        `json:"header_override_json"`
	AutoPauseOnExpired    bool                          `json:"auto_pause_on_expired"`
}

// ParseProviderAccountRuntimeSettings decodes the optional UI settings. A
// malformed settings document is ignored here so account listing and route
// inspection remain available; request construction validates enabled header
// overrides and returns an actionable error before dispatch.
func ParseProviderAccountRuntimeSettings(adapterConfig map[string]string) ProviderAccountRuntimeSettings {
	var settings ProviderAccountRuntimeSettings
	if adapterConfig == nil {
		return settings
	}
	raw := strings.TrimSpace(adapterConfig[ProviderAccountAdapterConfigSettings])
	if raw == "" || json.Unmarshal([]byte(raw), &settings) != nil {
		return ProviderAccountRuntimeSettings{}
	}
	for index := range settings.ModelMappings {
		settings.ModelMappings[index].From = strings.TrimSpace(settings.ModelMappings[index].From)
		settings.ModelMappings[index].To = strings.TrimSpace(settings.ModelMappings[index].To)
	}
	settings.BaseURL = strings.TrimSpace(settings.BaseURL)
	settings.ModelRestrictionMode = strings.TrimSpace(settings.ModelRestrictionMode)
	return settings
}

// EffectiveProviderAccountBaseURL returns the account override when present,
// otherwise the provider connection URL. Validation is deliberately left to
// the caller so an invalid non-empty override cannot silently route elsewhere.
func EffectiveProviderAccountBaseURL(account ProviderAccount, provider ProviderConnection) string {
	settings := ParseProviderAccountRuntimeSettings(account.AdapterConfig)
	if settings.BaseURL != "" {
		return settings.BaseURL
	}
	return strings.TrimSpace(provider.BaseURL)
}

// ProviderAccountMappedModel applies the account's model mapping to a model
// identifier. Empty or incomplete rows are ignored. Mappings are honored when
// the UI is in mapping mode; older records without that mode remain compatible
// by applying any complete mapping rows.
func ProviderAccountMappedModel(account ProviderAccount, model string) string {
	mapped, _ := providerAccountMappedModel(account, model)
	return mapped
}

func providerAccountMappedModel(account ProviderAccount, model string) (string, bool) {
	model = strings.TrimSpace(model)
	if model == "" {
		return model, false
	}
	settings := ParseProviderAccountRuntimeSettings(account.AdapterConfig)
	if settings.ModelRestrictionMode != "" && settings.ModelRestrictionMode != "mapping" {
		return model, false
	}
	for _, mapping := range settings.ModelMappings {
		if mapping.From != "" && mapping.To != "" && mapping.From == model {
			return mapping.To, true
		}
	}
	return model, false
}

// ProviderAccountDispatchModel applies a route-model mapping first and then a
// requested-model mapping when the route row itself has no mapping. This keeps
// model-route eligibility tied to the persisted route declaration while still
// allowing an account to translate either identifier at dispatch time.
func ProviderAccountDispatchModel(account ProviderAccount, routeModel, requestedModel string) string {
	routeModel = strings.TrimSpace(routeModel)
	if mapped, matched := providerAccountMappedModel(account, routeModel); matched {
		return mapped
	}
	if mapped, matched := providerAccountMappedModel(account, requestedModel); matched {
		return mapped
	}
	return routeModel
}

// ApplyProviderAccountHeaderOverrides applies validated account-local headers
// after the adapter has installed authentication headers. Hop-by-hop headers
// are rejected because they are controlled by net/http and must not be
// user-configurable. Authorization remains overridable for explicit provider
// integrations that require a non-standard credential header.
func ApplyProviderAccountHeaderOverrides(req *http.Request, adapterConfig map[string]string) error {
	if req == nil {
		return fmt.Errorf("provider account request is nil")
	}
	raw, err := providerAccountHeaderOverrides(adapterConfig)
	if err != nil {
		return err
	}
	for name, value := range raw {
		req.Header.Set(name, value)
	}
	return nil
}

// ValidateProviderAccountRuntimeSettings validates the serialized UI settings
// before they are persisted. It intentionally does not validate provider
// adapter credentials, which remain the responsibility of the adapter matrix.
func ValidateProviderAccountRuntimeSettings(adapterConfig map[string]string) error {
	raw := strings.TrimSpace(adapterConfig[ProviderAccountAdapterConfigSettings])
	if raw == "" {
		return nil
	}
	var settings ProviderAccountRuntimeSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return fmt.Errorf("adapter_config.%s must be valid JSON: %w", ProviderAccountAdapterConfigSettings, err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &document); err != nil || document == nil {
		if err == nil {
			err = fmt.Errorf("settings must be a JSON object")
		}
		return fmt.Errorf("adapter_config.%s must be a JSON object: %w", ProviderAccountAdapterConfigSettings, err)
	}
	if settings.BaseURL = strings.TrimSpace(settings.BaseURL); settings.BaseURL != "" && !validHTTPURL(settings.BaseURL) {
		return fmt.Errorf("adapter_config.%s.base_url must be an absolute http or https URL", ProviderAccountAdapterConfigSettings)
	}
	if settings.ModelRestrictionMode != "" && settings.ModelRestrictionMode != "whitelist" && settings.ModelRestrictionMode != "mapping" {
		return fmt.Errorf("adapter_config.%s.model_restriction_mode must be whitelist or mapping", ProviderAccountAdapterConfigSettings)
	}
	for index, mapping := range settings.ModelMappings {
		from, to := strings.TrimSpace(mapping.From), strings.TrimSpace(mapping.To)
		if (from == "") != (to == "") {
			return fmt.Errorf("adapter_config.%s.model_mappings[%d] requires both from and to", ProviderAccountAdapterConfigSettings, index)
		}
	}
	_, err := providerAccountHeaderOverrides(adapterConfig)
	return err
}

func providerAccountHeaderOverrides(adapterConfig map[string]string) (map[string]string, error) {
	settings := ParseProviderAccountRuntimeSettings(adapterConfig)
	if !settings.HeaderOverrideEnabled || strings.TrimSpace(settings.HeaderOverrideJSON) == "" {
		return nil, nil
	}
	var raw map[string]string
	if err := json.Unmarshal([]byte(settings.HeaderOverrideJSON), &raw); err != nil || raw == nil {
		if err == nil {
			err = fmt.Errorf("header override must be a JSON object")
		}
		return nil, fmt.Errorf("provider account header overrides are invalid; value must be a JSON object: %w", err)
	}
	validated := make(map[string]string, len(raw))
	canonicalNames := make(map[string]string, len(raw))
	for name, value := range raw {
		name = strings.TrimSpace(name)
		canonical := textproto.CanonicalMIMEHeaderKey(name)
		if name == "" || canonical == "" || !httpguts.ValidHeaderFieldName(name) {
			return nil, fmt.Errorf("provider account header name is invalid")
		}
		if !httpguts.ValidHeaderFieldValue(value) {
			return nil, fmt.Errorf("provider account header %q contains an invalid control character", name)
		}
		switch strings.ToLower(name) {
		case "connection", "content-length", "host", "proxy-connection", "transfer-encoding", "upgrade":
			return nil, fmt.Errorf("provider account header %q is not overrideable", name)
		}
		if previous, exists := canonicalNames[canonical]; exists {
			return nil, fmt.Errorf("provider account header names %q and %q are duplicates", previous, name)
		}
		canonicalNames[canonical] = name
		validated[canonical] = value
	}
	return validated, nil
}
