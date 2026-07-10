package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/buildinfo"
)

type ServiceOptions struct {
	Version     string
	ProfileHint string
	StorageMode string
}

type Service struct {
	repo        Repository
	version     string
	profileHint string
	storageMode string
}

func NewService(repo Repository, opts ServiceOptions) *Service {
	version := opts.Version
	if version == "" {
		version = buildinfo.Version
	}
	storageMode := opts.StorageMode
	if storageMode == "" {
		storageMode = "unknown"
	}
	return &Service{
		repo:        repo,
		version:     version,
		profileHint: opts.ProfileHint,
		storageMode: storageMode,
	}
}

func (s *Service) Public(ctx context.Context) (PublicSettings, error) {
	settings, err := s.Admin(ctx)
	if err != nil {
		return PublicSettings{}, err
	}
	return settings.PublicSettings, nil
}

func (s *Service) Admin(ctx context.Context) (AdminSettings, error) {
	raw, err := s.repo.GetAll(ctx)
	if err != nil {
		return AdminSettings{}, err
	}
	merged := defaults()
	for key, value := range raw {
		merged[key] = value
	}
	if s.profileHint != "" && raw[KeyProfile] == "" {
		merged[KeyProfile] = s.profileHint
		merged[KeySetupCompleted] = "true"
	}
	return s.parse(merged), nil
}

func (s *Service) Update(ctx context.Context, in AdminSettings) (AdminSettings, error) {
	values, err := valuesFromAdminSettings(in)
	if err != nil {
		return AdminSettings{}, err
	}
	if err := s.repo.SetMultiple(ctx, values); err != nil {
		return AdminSettings{}, err
	}
	return s.Admin(ctx)
}

func (s *Service) ApplyProfile(ctx context.Context, profile string) (AdminSettings, error) {
	if !isProfile(profile) {
		return AdminSettings{}, fmt.Errorf("unsupported profile %q", profile)
	}
	if err := s.repo.SetMultiple(ctx, map[string]string{
		KeyProfile:        profile,
		KeySetupCompleted: "true",
	}); err != nil {
		return AdminSettings{}, err
	}
	return s.Admin(ctx)
}

func (s *Service) Health(ctx context.Context) error {
	return s.repo.Health(ctx)
}

func (s *Service) parse(values map[string]string) AdminSettings {
	_, offset := time.Now().Zone()
	return AdminSettings{
		PublicSettings: PublicSettings{
			SiteName:          values[KeySiteName],
			SiteSubtitle:      values[KeySiteSubtitle],
			PublicBaseURL:     values[KeyPublicBaseURL],
			APIBaseURL:        "/api/v1",
			GatewayBasePath:   values[KeyGatewayBasePath],
			Profile:           values[KeyProfile],
			SetupCompleted:    parseBool(values[KeySetupCompleted]),
			DefaultLocale:     values[KeyDefaultLocale],
			EnabledLocales:    parseStringList(values[KeyEnabledLocales], []string{"en-US", "zh-CN"}),
			OIDCEnabled:       parseBool(values[KeyOIDCEnabled]),
			OIDCProviderName:  values[KeyOIDCProviderName],
			ServiceCenterMode: values[KeyServiceCenterMode],
			Version:           s.version,
			ServerTimezone:    timezoneName(),
			ServerUTCOffset:   formatUTCOffset(offset),
			StorageMode:       s.storageMode,
		},
		OIDCIssuerURL:     values[KeyOIDCIssuerURL],
		OIDCClientID:      values[KeyOIDCClientID],
		DataRetentionDays: parseInt(values[KeyDataRetentionDays], 30),
		PromptLoggingMode: values[KeyPromptLoggingMode],
		UpdateChannel:     values[KeyUpdateChannel],
	}
}

func defaults() map[string]string {
	return map[string]string{
		KeySiteName:          "AsterRouter",
		KeySiteSubtitle:      "AI Gateway Control Plane",
		KeyPublicBaseURL:     "",
		KeyDefaultLocale:     "en-US",
		KeyEnabledLocales:    `["en-US","zh-CN"]`,
		KeyProfile:           "",
		KeySetupCompleted:    "false",
		KeyGatewayBasePath:   "/v1",
		KeyOIDCEnabled:       "false",
		KeyOIDCProviderName:  "OIDC",
		KeyOIDCIssuerURL:     "",
		KeyOIDCClientID:      "",
		KeyDataRetentionDays: "30",
		KeyPromptLoggingMode: "metadata_only",
		KeyUpdateChannel:     "stable",
		KeyServiceCenterMode: "disabled",
	}
}

func valuesFromAdminSettings(in AdminSettings) (map[string]string, error) {
	if strings.TrimSpace(in.SiteName) == "" {
		return nil, errors.New("site_name is required")
	}
	if !isLocale(in.DefaultLocale) {
		return nil, errors.New("default_locale must be en-US or zh-CN")
	}
	if len(in.EnabledLocales) == 0 {
		return nil, errors.New("enabled_locales must not be empty")
	}
	for _, locale := range in.EnabledLocales {
		if !isLocale(locale) {
			return nil, fmt.Errorf("unsupported locale %q", locale)
		}
	}
	if in.Profile != "" && !isProfile(in.Profile) {
		return nil, fmt.Errorf("unsupported profile %q", in.Profile)
	}
	if in.GatewayBasePath == "" || !strings.HasPrefix(in.GatewayBasePath, "/") {
		return nil, errors.New("gateway_base_path must start with /")
	}
	if in.DataRetentionDays < 1 || in.DataRetentionDays > 3650 {
		return nil, errors.New("data_retention_days must be between 1 and 3650")
	}
	if !oneOf(in.PromptLoggingMode, "disabled", "metadata_only", "full") {
		return nil, errors.New("prompt_logging_mode must be disabled, metadata_only, or full")
	}
	if !oneOf(in.UpdateChannel, "stable", "beta", "manual") {
		return nil, errors.New("update_channel must be stable, beta, or manual")
	}
	if !oneOf(in.ServiceCenterMode, "disabled", "online", "private_mirror", "offline") {
		return nil, errors.New("service_center_mode must be disabled, online, private_mirror, or offline")
	}
	locales, _ := json.Marshal(in.EnabledLocales)
	return map[string]string{
		KeySiteName:          strings.TrimSpace(in.SiteName),
		KeySiteSubtitle:      strings.TrimSpace(in.SiteSubtitle),
		KeyPublicBaseURL:     strings.TrimSpace(in.PublicBaseURL),
		KeyDefaultLocale:     in.DefaultLocale,
		KeyEnabledLocales:    string(locales),
		KeyProfile:           in.Profile,
		KeySetupCompleted:    strconv.FormatBool(in.SetupCompleted),
		KeyGatewayBasePath:   in.GatewayBasePath,
		KeyOIDCEnabled:       strconv.FormatBool(in.OIDCEnabled),
		KeyOIDCProviderName:  strings.TrimSpace(in.OIDCProviderName),
		KeyOIDCIssuerURL:     strings.TrimSpace(in.OIDCIssuerURL),
		KeyOIDCClientID:      strings.TrimSpace(in.OIDCClientID),
		KeyDataRetentionDays: strconv.Itoa(in.DataRetentionDays),
		KeyPromptLoggingMode: in.PromptLoggingMode,
		KeyUpdateChannel:     in.UpdateChannel,
		KeyServiceCenterMode: in.ServiceCenterMode,
	}, nil
}

func parseBool(value string) bool {
	return strings.EqualFold(value, "true") || value == "1"
}

func parseInt(value string, fallback int) int {
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func parseStringList(value string, fallback []string) []string {
	var out []string
	if err := json.Unmarshal([]byte(value), &out); err != nil || len(out) == 0 {
		return fallback
	}
	return out
}

func isLocale(value string) bool {
	return value == "en-US" || value == "zh-CN"
}

func isProfile(value string) bool {
	return value == "personal" || value == "relay_operator" || value == "enterprise"
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func timezoneName() string {
	name, _ := time.Now().Zone()
	if name == "" {
		return "Local"
	}
	return name
}

func formatUTCOffset(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, hours, minutes)
}
