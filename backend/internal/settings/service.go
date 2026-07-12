package settings

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/buildinfo"
)

type ServiceOptions struct {
	Version         string
	EnabledProfiles []string
	DefaultProfile  string
	StorageMode     string
	DemoMode        bool
}

type Service struct {
	repo            Repository
	version         string
	enabledProfiles []string
	defaultProfile  string
	storageMode     string
	demoMode        bool
	inviteMu        sync.Mutex
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
		repo:            repo,
		version:         version,
		enabledProfiles: normalizeProfiles(opts.EnabledProfiles),
		defaultProfile:  strings.TrimSpace(opts.DefaultProfile),
		storageMode:     storageMode,
		demoMode:        opts.DemoMode,
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
	if len(s.enabledProfiles) > 0 && raw[KeyEnabledProfiles] == "" && raw[KeyDefaultProfile] == "" {
		defaultProfile := s.defaultProfile
		if defaultProfile == "" {
			defaultProfile = s.enabledProfiles[0]
		}
		if !containsString(s.enabledProfiles, defaultProfile) {
			defaultProfile = s.enabledProfiles[0]
		}
		encodedProfiles, _ := json.Marshal(s.enabledProfiles)
		merged[KeyEnabledProfiles] = string(encodedProfiles)
		merged[KeyDefaultProfile] = defaultProfile
		merged[KeySetupCompleted] = "true"
	}
	if s.demoMode && len(s.enabledProfiles) == 0 && raw[KeyEnabledProfiles] == "" && raw[KeyDefaultProfile] == "" {
		merged[KeyEnabledProfiles] = `["personal","relay_operator","enterprise"]`
		merged[KeyDefaultProfile] = "personal"
		merged[KeySetupCompleted] = "true"
	}
	return s.parse(merged), nil
}

func (s *Service) Update(ctx context.Context, in AdminSettings) (AdminSettings, error) {
	values, err := valuesFromAdminSettings(in)
	if err != nil {
		return AdminSettings{}, err
	}
	if strings.TrimSpace(in.FeishuAppSecret) == "" {
		if existing, getErr := s.repo.GetAll(ctx); getErr == nil && strings.TrimSpace(existing[KeyFeishuAppSecret]) != "" {
			values[KeyFeishuAppSecret] = existing[KeyFeishuAppSecret]
		}
	}
	if existing, getErr := s.repo.GetAll(ctx); getErr == nil {
		if strings.TrimSpace(in.TurnstileSecretKey) == "" && existing[KeyTurnstileSecretKey] != "" {
			values[KeyTurnstileSecretKey] = existing[KeyTurnstileSecretKey]
		}
		if strings.TrimSpace(in.SMTPPassword) == "" && existing[KeySMTPPassword] != "" {
			values[KeySMTPPassword] = existing[KeySMTPPassword]
		}
	}
	if err := s.repo.SetMultiple(ctx, values); err != nil {
		return AdminSettings{}, err
	}
	return s.Admin(ctx)
}

func (s *Service) ApplyProfiles(ctx context.Context, profiles []string, defaultProfile string) (AdminSettings, error) {
	enabledProfiles := normalizeProfiles(profiles)
	if len(enabledProfiles) == 0 {
		return AdminSettings{}, errors.New("at least one profile is required")
	}
	defaultProfile = strings.TrimSpace(defaultProfile)
	if defaultProfile == "" {
		defaultProfile = enabledProfiles[0]
	}
	if !containsString(enabledProfiles, defaultProfile) {
		return AdminSettings{}, fmt.Errorf("default profile %q is not enabled", defaultProfile)
	}
	encodedProfiles, _ := json.Marshal(enabledProfiles)
	if err := s.repo.SetMultiple(ctx, map[string]string{
		KeyDefaultProfile:  defaultProfile,
		KeyEnabledProfiles: string(encodedProfiles),
		KeySetupCompleted:  "true",
	}); err != nil {
		return AdminSettings{}, err
	}
	return s.Admin(ctx)
}

func (s *Service) Health(ctx context.Context) error {
	return s.repo.Health(ctx)
}

func (s *Service) FeishuSecret(ctx context.Context) (string, error) {
	values, err := s.repo.GetAll(ctx)
	if err != nil {
		return "", err
	}
	return values[KeyFeishuAppSecret], nil
}

type LoginSecuritySettings struct {
	TurnstileEnabled bool
	TurnstileSecret  string
}

type RegistrationPolicy struct {
	Enabled, EmailVerification, InvitationRequired bool
	AllowedDomains, InvitationCodes                []string
}

func (s *Service) SMTPConfig(ctx context.Context) (host string, port int, username, password, from string, err error) {
	values, err := s.repo.GetAll(ctx)
	if err != nil {
		return "", 0, "", "", "", err
	}
	return values[KeySMTPHost], parseInt(values[KeySMTPPort], 587), values[KeySMTPUsername], values[KeySMTPPassword], values[KeySMTPFrom], nil
}

func (s *Service) RegistrationPolicy(ctx context.Context) (RegistrationPolicy, error) {
	values, err := s.repo.GetAll(ctx)
	if err != nil {
		return RegistrationPolicy{}, err
	}
	return RegistrationPolicy{Enabled: parseBool(values[KeyRegistrationEnabled]), EmailVerification: parseBool(values[KeyEmailVerifyEnabled]), InvitationRequired: parseBool(values[KeyInvitationRequired]), AllowedDomains: parseStringList(values[KeyAllowedEmailDomains], []string{}), InvitationCodes: parseStringList(values[KeyInvitationCodes], []string{})}, nil
}

func (s *Service) ConsumeInvitationCode(ctx context.Context, code string) error {
	s.inviteMu.Lock()
	defer s.inviteMu.Unlock()
	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New("invitation code is required")
	}
	values, err := s.repo.GetAll(ctx)
	if err != nil {
		return err
	}
	codes := parseStringList(values[KeyInvitationCodes], []string{})
	for index, candidate := range codes {
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			codes = append(codes[:index], codes[index+1:]...)
			raw, _ := json.Marshal(codes)
			return s.repo.SetMultiple(ctx, map[string]string{KeyInvitationCodes: string(raw)})
		}
	}
	return errors.New("invitation code is invalid")
}

func (s *Service) LoginSecurity(ctx context.Context) (LoginSecuritySettings, error) {
	values, err := s.repo.GetAll(ctx)
	if err != nil {
		return LoginSecuritySettings{}, err
	}
	return LoginSecuritySettings{TurnstileEnabled: parseBool(values[KeyTurnstileEnabled]), TurnstileSecret: values[KeyTurnstileSecretKey]}, nil
}

func (s *Service) parse(values map[string]string) AdminSettings {
	_, offset := time.Now().Zone()
	enabledProfiles := parseProfileList(values[KeyEnabledProfiles])
	defaultProfile := strings.TrimSpace(values[KeyDefaultProfile])
	if defaultProfile == "" && len(enabledProfiles) > 0 {
		defaultProfile = enabledProfiles[0]
	}
	if defaultProfile != "" && !containsString(enabledProfiles, defaultProfile) {
		enabledProfiles = normalizeProfiles(append([]string{defaultProfile}, enabledProfiles...))
	}
	return AdminSettings{
		PublicSettings: PublicSettings{
			SiteName:              values[KeySiteName],
			SiteSubtitle:          values[KeySiteSubtitle],
			PublicBaseURL:         values[KeyPublicBaseURL],
			APIBaseURL:            "/api/v1",
			GatewayBasePath:       values[KeyGatewayBasePath],
			DefaultProfile:        defaultProfile,
			EnabledProfiles:       enabledProfiles,
			SetupCompleted:        parseBool(values[KeySetupCompleted]),
			DefaultLocale:         values[KeyDefaultLocale],
			EnabledLocales:        parseStringList(values[KeyEnabledLocales], []string{"en-US", "zh-CN"}),
			OIDCEnabled:           parseBool(values[KeyOIDCEnabled]),
			OIDCProviderName:      values[KeyOIDCProviderName],
			FeishuEnabled:         parseBool(values[KeyFeishuEnabled]),
			FeishuRegion:          values[KeyFeishuRegion],
			RegistrationEnabled:   parseBool(values[KeyRegistrationEnabled]),
			EmailVerifyEnabled:    parseBool(values[KeyEmailVerifyEnabled]),
			TOTPEnabled:           parseBool(values[KeyTOTPEnabled]),
			TurnstileEnabled:      parseBool(values[KeyTurnstileEnabled]),
			TurnstileSiteKey:      values[KeyTurnstileSiteKey],
			InvitationRequired:    parseBool(values[KeyInvitationRequired]),
			LoginAgreementEnabled: parseBool(values[KeyLoginAgreementEnabled]),
			LoginAgreementMode:    values[KeyLoginAgreementMode], LoginAgreementUpdatedAt: values[KeyLoginAgreementUpdatedAt], LegalDocuments: parseLegalDocuments(values[KeyLegalDocuments]), BackendMode: parseBool(values[KeyBackendMode]), SupportContact: values[KeySupportContact], DocumentationURL: values[KeyDocumentationURL],
			ServiceCenterMode: values[KeyServiceCenterMode],
			Version:           s.version,
			ServerTimezone:    timezoneName(),
			ServerUTCOffset:   formatUTCOffset(offset),
			StorageMode:       s.storageMode,
			DemoMode:          s.demoMode,
		},
		OIDCIssuerURL:       values[KeyOIDCIssuerURL],
		OIDCClientID:        values[KeyOIDCClientID],
		FeishuAppID:         values[KeyFeishuAppID],
		FeishuConfigured:    strings.TrimSpace(values[KeyFeishuAppSecret]) != "",
		AllowedEmailDomains: parseStringList(values[KeyAllowedEmailDomains], []string{}),
		InvitationCodes:     parseStringList(values[KeyInvitationCodes], []string{}),
		TrustedProxyHeaders: parseBool(values[KeyTrustedProxyHeaders]),
		TurnstileConfigured: strings.TrimSpace(values[KeyTurnstileSecretKey]) != "",
		DefaultBalanceCents: parseInt(values[KeyDefaultBalanceCents], 0),
		DefaultConcurrency:  parseInt(values[KeyDefaultConcurrency], 5),
		DefaultRPM:          parseInt(values[KeyDefaultRPM], 0),
		SMTPHost:            values[KeySMTPHost], SMTPPort: parseInt(values[KeySMTPPort], 587), SMTPUsername: values[KeySMTPUsername], SMTPFrom: values[KeySMTPFrom], SMTPConfigured: strings.TrimSpace(values[KeySMTPPassword]) != "",
		LoginAgreementTitle: values[KeyLoginAgreementTitle], LoginAgreementContent: values[KeyLoginAgreementContent],
		DefaultPageSize: parseInt(values[KeyDefaultPageSize], 20), PageSizeOptions: parseIntList(values[KeyPageSizeOptions], []int{10, 20, 50}), HomeContent: values[KeyHomeContent], HideImportButton: parseBool(values[KeyHideImportButton]),
		DataRetentionDays: parseInt(values[KeyDataRetentionDays], 30),
		PromptLoggingMode: values[KeyPromptLoggingMode],
		UpdateChannel:     values[KeyUpdateChannel],
	}
}

func defaults() map[string]string {
	return map[string]string{
		KeySiteName:            "AsterRouter",
		KeySiteSubtitle:        "AI Gateway Control Plane",
		KeyPublicBaseURL:       "",
		KeyDefaultLocale:       "en-US",
		KeyEnabledLocales:      `["en-US","zh-CN"]`,
		KeyDefaultProfile:      "",
		KeyEnabledProfiles:     "[]",
		KeySetupCompleted:      "false",
		KeyGatewayBasePath:     "/v1",
		KeyOIDCEnabled:         "false",
		KeyOIDCProviderName:    "OIDC",
		KeyOIDCIssuerURL:       "",
		KeyOIDCClientID:        "",
		KeyFeishuEnabled:       "false",
		KeyFeishuRegion:        "cn",
		KeyFeishuAppID:         "",
		KeyFeishuAppSecret:     "",
		KeyRegistrationEnabled: "false", KeyEmailVerifyEnabled: "false", KeyAllowedEmailDomains: "[]", KeyInvitationRequired: "false", KeyInvitationCodes: "[]", KeyTOTPEnabled: "false", KeyTrustedProxyHeaders: "false", KeyTurnstileEnabled: "false", KeyTurnstileSiteKey: "", KeyTurnstileSecretKey: "", KeyDefaultBalanceCents: "0", KeyDefaultConcurrency: "5", KeyDefaultRPM: "0", KeySMTPHost: "", KeySMTPPort: "587", KeySMTPUsername: "", KeySMTPPassword: "", KeySMTPFrom: "", KeyLoginAgreementEnabled: "false", KeyLoginAgreementTitle: "Terms of Service", KeyLoginAgreementContent: "",
		KeyBackendMode: "false", KeyDefaultPageSize: "20", KeyPageSizeOptions: "[10,20,50]", KeySupportContact: "", KeyDocumentationURL: "", KeyHomeContent: "", KeyHideImportButton: "false", KeyLoginAgreementMode: "modal", KeyLoginAgreementUpdatedAt: "", KeyLegalDocuments: "[]",
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
	enabledProfiles := normalizeProfiles(in.EnabledProfiles)
	defaultProfile := strings.TrimSpace(in.DefaultProfile)
	if defaultProfile == "" && len(enabledProfiles) > 0 {
		defaultProfile = enabledProfiles[0]
	}
	if defaultProfile != "" && !containsString(enabledProfiles, defaultProfile) {
		return nil, fmt.Errorf("default profile %q is not enabled", defaultProfile)
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
	if !oneOf(strings.TrimSpace(in.FeishuRegion), "cn", "global") {
		return nil, errors.New("feishu_region must be cn or global")
	}
	if in.FeishuEnabled && strings.TrimSpace(in.FeishuAppID) == "" {
		return nil, errors.New("feishu_app_id is required when feishu login is enabled")
	}
	if in.DefaultBalanceCents < 0 || in.DefaultConcurrency < 0 || in.DefaultRPM < 0 {
		return nil, errors.New("default user limits cannot be negative")
	}
	if in.SMTPPort < 1 || in.SMTPPort > 65535 {
		return nil, errors.New("smtp_port must be between 1 and 65535")
	}
	if in.DefaultPageSize < 5 || in.DefaultPageSize > 1000 {
		return nil, errors.New("default_page_size must be between 5 and 1000")
	}
	if len(in.PageSizeOptions) == 0 {
		return nil, errors.New("page_size_options must not be empty")
	}
	pageSizeSeen := make(map[int]struct{}, len(in.PageSizeOptions))
	for _, size := range in.PageSizeOptions {
		if size < 5 || size > 1000 {
			return nil, errors.New("page_size_options must be between 5 and 1000")
		}
		if _, exists := pageSizeSeen[size]; exists {
			return nil, errors.New("page_size_options must not contain duplicates")
		}
		pageSizeSeen[size] = struct{}{}
	}
	if _, exists := pageSizeSeen[in.DefaultPageSize]; !exists {
		return nil, errors.New("default_page_size must be included in page_size_options")
	}
	if err := validateOptionalHTTPURL("public_base_url", in.PublicBaseURL); err != nil {
		return nil, err
	}
	if err := validateOptionalHTTPURL("documentation_url", in.DocumentationURL); err != nil {
		return nil, err
	}
	if !oneOf(in.LoginAgreementMode, "modal", "checkbox") {
		return nil, errors.New("login_agreement_mode must be modal or checkbox")
	}
	if err := validateLegalDocuments(in.LegalDocuments, in.LoginAgreementEnabled); err != nil {
		return nil, err
	}
	for _, domain := range in.AllowedEmailDomains {
		if strings.TrimSpace(domain) == "" || strings.Contains(domain, "@") {
			return nil, errors.New("allowed_email_domains must contain domain names")
		}
	}
	locales, _ := json.Marshal(in.EnabledLocales)
	profiles, _ := json.Marshal(enabledProfiles)
	domains, _ := json.Marshal(in.AllowedEmailDomains)
	invitationCodes, _ := json.Marshal(in.InvitationCodes)
	pageSizes, _ := json.Marshal(in.PageSizeOptions)
	legalDocuments, _ := json.Marshal(in.LegalDocuments)
	return map[string]string{
		KeySiteName:            strings.TrimSpace(in.SiteName),
		KeySiteSubtitle:        strings.TrimSpace(in.SiteSubtitle),
		KeyPublicBaseURL:       strings.TrimSpace(in.PublicBaseURL),
		KeyDefaultLocale:       in.DefaultLocale,
		KeyEnabledLocales:      string(locales),
		KeyDefaultProfile:      defaultProfile,
		KeyEnabledProfiles:     string(profiles),
		KeySetupCompleted:      strconv.FormatBool(in.SetupCompleted),
		KeyGatewayBasePath:     in.GatewayBasePath,
		KeyOIDCEnabled:         strconv.FormatBool(in.OIDCEnabled),
		KeyOIDCProviderName:    strings.TrimSpace(in.OIDCProviderName),
		KeyOIDCIssuerURL:       strings.TrimSpace(in.OIDCIssuerURL),
		KeyOIDCClientID:        strings.TrimSpace(in.OIDCClientID),
		KeyFeishuEnabled:       strconv.FormatBool(in.FeishuEnabled),
		KeyFeishuRegion:        strings.TrimSpace(in.FeishuRegion),
		KeyFeishuAppID:         strings.TrimSpace(in.FeishuAppID),
		KeyFeishuAppSecret:     strings.TrimSpace(in.FeishuAppSecret),
		KeyRegistrationEnabled: strconv.FormatBool(in.RegistrationEnabled), KeyEmailVerifyEnabled: strconv.FormatBool(in.EmailVerifyEnabled), KeyAllowedEmailDomains: string(domains), KeyInvitationRequired: strconv.FormatBool(in.InvitationRequired), KeyInvitationCodes: string(invitationCodes), KeyTOTPEnabled: strconv.FormatBool(in.TOTPEnabled), KeyTrustedProxyHeaders: strconv.FormatBool(in.TrustedProxyHeaders), KeyTurnstileEnabled: strconv.FormatBool(in.TurnstileEnabled), KeyTurnstileSiteKey: strings.TrimSpace(in.TurnstileSiteKey), KeyTurnstileSecretKey: strings.TrimSpace(in.TurnstileSecretKey), KeyDefaultBalanceCents: strconv.Itoa(in.DefaultBalanceCents), KeyDefaultConcurrency: strconv.Itoa(in.DefaultConcurrency), KeyDefaultRPM: strconv.Itoa(in.DefaultRPM), KeySMTPHost: strings.TrimSpace(in.SMTPHost), KeySMTPPort: strconv.Itoa(in.SMTPPort), KeySMTPUsername: strings.TrimSpace(in.SMTPUsername), KeySMTPPassword: strings.TrimSpace(in.SMTPPassword), KeySMTPFrom: strings.TrimSpace(in.SMTPFrom), KeyLoginAgreementEnabled: strconv.FormatBool(in.LoginAgreementEnabled), KeyLoginAgreementTitle: strings.TrimSpace(in.LoginAgreementTitle), KeyLoginAgreementContent: strings.TrimSpace(in.LoginAgreementContent),
		KeyBackendMode: strconv.FormatBool(in.BackendMode), KeyDefaultPageSize: strconv.Itoa(in.DefaultPageSize), KeyPageSizeOptions: string(pageSizes), KeySupportContact: strings.TrimSpace(in.SupportContact), KeyDocumentationURL: strings.TrimSpace(in.DocumentationURL), KeyHomeContent: in.HomeContent, KeyHideImportButton: strconv.FormatBool(in.HideImportButton), KeyLoginAgreementMode: strings.TrimSpace(in.LoginAgreementMode), KeyLoginAgreementUpdatedAt: strings.TrimSpace(in.LoginAgreementUpdatedAt), KeyLegalDocuments: string(legalDocuments),
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

func parseIntList(value string, fallback []int) []int {
	var out []int
	if err := json.Unmarshal([]byte(value), &out); err != nil || len(out) == 0 {
		return fallback
	}
	return out
}

func parseLegalDocuments(value string) []LegalDocument {
	var out []LegalDocument
	if err := json.Unmarshal([]byte(value), &out); err != nil || out == nil {
		return []LegalDocument{}
	}
	return out
}

var legalSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func validateLegalDocuments(documents []LegalDocument, required bool) error {
	if required && len(documents) == 0 {
		return errors.New("legal_documents must not be empty when login agreement is enabled")
	}
	ids := make(map[string]struct{}, len(documents))
	slugs := make(map[string]struct{}, len(documents))
	for _, document := range documents {
		id := strings.TrimSpace(document.ID)
		name := strings.TrimSpace(document.Name)
		slug := strings.TrimSpace(document.Slug)
		if id == "" || name == "" || slug == "" || strings.TrimSpace(document.Content) == "" {
			return errors.New("legal document id, name, slug, and content are required")
		}
		if !legalSlugPattern.MatchString(slug) {
			return fmt.Errorf("legal document slug %q must contain lowercase letters, numbers, and hyphens only", slug)
		}
		if _, exists := ids[id]; exists {
			return fmt.Errorf("duplicate legal document id %q", id)
		}
		if _, exists := slugs[slug]; exists {
			return fmt.Errorf("duplicate legal document slug %q", slug)
		}
		ids[id] = struct{}{}
		slugs[slug] = struct{}{}
	}
	return nil
}

func validateOptionalHTTPURL(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("%s must be an http or https URL", field)
	}
	return nil
}

func parseProfileList(value string) []string {
	var out []string
	if err := json.Unmarshal([]byte(value), &out); err == nil {
		if normalized := normalizeProfiles(out); len(normalized) > 0 {
			return normalized
		}
	}
	return []string{}
}

func isLocale(value string) bool {
	return value == "en-US" || value == "zh-CN"
}

func isProfile(value string) bool {
	return value == "personal" || value == "relay_operator" || value == "enterprise"
}

func normalizeProfiles(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		profile := strings.TrimSpace(value)
		if !isProfile(profile) || seen[profile] {
			continue
		}
		seen[profile] = true
		out = append(out, profile)
	}
	return out
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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
