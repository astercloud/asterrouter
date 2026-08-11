package settings

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/buildinfo"
)

type ServiceOptions struct {
	Version     string
	StorageMode string
	DemoMode    bool
	SecretKey   string
}

type Service struct {
	repo        Repository
	version     string
	storageMode string
	demoMode    bool
	secretKey   string
}

var (
	ErrInvitationCodeRequired   = errors.New("invitation code is required")
	ErrInvitationCodeInvalid    = errors.New("invitation code is invalid")
	ErrOrganizationNameRequired = errors.New("organization_name is required")
)

func NewService(repo Repository, opts ServiceOptions) *Service {
	version := opts.Version
	if version == "" {
		version = buildinfo.Version
	}
	storageMode := opts.StorageMode
	if storageMode == "" {
		storageMode = "unknown"
	}
	secretKey := strings.TrimSpace(opts.SecretKey)
	if secretKey == "" {
		secretKey = "asterrouter-local-development-secret"
	}
	return &Service{
		repo:        repo,
		version:     version,
		storageMode: storageMode,
		demoMode:    opts.DemoMode,
		secretKey:   secretKey,
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
	raw, err := s.readValues(ctx)
	if err != nil {
		return AdminSettings{}, err
	}
	merged := defaults()
	for key, value := range raw {
		merged[key] = value
	}
	if s.demoMode && raw[KeySetupCompleted] == "" {
		merged[KeySetupCompleted] = "true"
	}
	return s.parse(merged), nil
}

func (s *Service) Update(ctx context.Context, in AdminSettings) (AdminSettings, error) {
	current, err := s.Admin(ctx)
	if err != nil {
		return AdminSettings{}, err
	}
	in.EmailTemplates = current.EmailTemplates
	values, err := valuesFromAdminSettings(in)
	if err != nil {
		return AdminSettings{}, err
	}
	existing, err := s.readValues(ctx)
	if err != nil {
		return AdminSettings{}, err
	}
	if strings.TrimSpace(in.FeishuAppSecret) == "" && existing[KeyFeishuAppSecret] != "" {
		values[KeyFeishuAppSecret] = existing[KeyFeishuAppSecret]
	}
	if strings.TrimSpace(in.OIDCClientSecret) == "" && existing[KeyOIDCClientSecret] != "" {
		values[KeyOIDCClientSecret] = existing[KeyOIDCClientSecret]
	}
	if strings.TrimSpace(in.TurnstileSecretKey) == "" && existing[KeyTurnstileSecretKey] != "" {
		values[KeyTurnstileSecretKey] = existing[KeyTurnstileSecretKey]
	}
	if strings.TrimSpace(in.SMTPPassword) == "" && existing[KeySMTPPassword] != "" {
		values[KeySMTPPassword] = existing[KeySMTPPassword]
	}
	if strings.TrimSpace(in.GitHubOAuthClientSecret) == "" && existing[KeyGitHubOAuthSecret] != "" {
		values[KeyGitHubOAuthSecret] = existing[KeyGitHubOAuthSecret]
	}
	if strings.TrimSpace(in.GoogleOAuthClientSecret) == "" && existing[KeyGoogleOAuthSecret] != "" {
		values[KeyGoogleOAuthSecret] = existing[KeyGoogleOAuthSecret]
	}
	if strings.TrimSpace(in.DingTalkClientSecret) == "" && existing[KeyDingTalkClientSecret] != "" {
		values[KeyDingTalkClientSecret] = existing[KeyDingTalkClientSecret]
	}
	if strings.TrimSpace(in.BackupS3SecretKey) == "" && existing[KeyBackupS3SecretKey] != "" {
		values[KeyBackupS3SecretKey] = existing[KeyBackupS3SecretKey]
	}
	// Templates have a dedicated compare-and-swap API. Excluding them from the
	// broad settings write prevents a stale form from losing another edit.
	delete(values, KeyEmailTemplates)
	values, err = s.encryptValues(values)
	if err != nil {
		return AdminSettings{}, err
	}
	if err := s.repo.SetMultiple(ctx, values); err != nil {
		return AdminSettings{}, err
	}
	return s.Admin(ctx)
}

func (s *Service) CompleteSetup(ctx context.Context, organizationName string) (AdminSettings, error) {
	organizationName = strings.TrimSpace(organizationName)
	if organizationName == "" {
		return AdminSettings{}, ErrOrganizationNameRequired
	}
	if err := s.repo.CompleteSetup(ctx, organizationName); err != nil {
		return AdminSettings{}, err
	}
	return s.Admin(ctx)
}

func (s *Service) Health(ctx context.Context) error {
	return s.repo.Health(ctx)
}

func (s *Service) FeishuSecret(ctx context.Context) (string, error) {
	values, err := s.readValues(ctx)
	if err != nil {
		return "", err
	}
	return values[KeyFeishuAppSecret], nil
}

func (s *Service) OIDCSecret(ctx context.Context) (string, error) {
	values, err := s.readValues(ctx)
	if err != nil {
		return "", err
	}
	return values[KeyOIDCClientSecret], nil
}

func (s *Service) SocialOAuthSecrets(ctx context.Context) (github, google string, err error) {
	values, err := s.readValues(ctx)
	if err != nil {
		return "", "", err
	}
	return values[KeyGitHubOAuthSecret], values[KeyGoogleOAuthSecret], nil
}

func (s *Service) DingTalkSecret(ctx context.Context) (string, error) {
	values, err := s.readValues(ctx)
	if err != nil {
		return "", err
	}
	return values[KeyDingTalkClientSecret], nil
}

type LoginSecuritySettings struct {
	TurnstileEnabled bool
	TurnstileSecret  string
}

type RegistrationPolicy struct {
	Enabled, EmailVerification, PasswordReset, InvitationRequired bool
	AllowedDomains, InvitationCodes                               []string
}

type SMTPSettings struct {
	Host, Username, Password, From, FromName string
	Port                                     int
	UseTLS                                   bool
}

func (s *Service) SMTPConfig(ctx context.Context) (SMTPSettings, error) {
	values, err := s.readValues(ctx)
	if err != nil {
		return SMTPSettings{}, err
	}
	return SMTPSettings{
		Host: values[KeySMTPHost], Port: parseInt(values[KeySMTPPort], 587),
		Username: values[KeySMTPUsername], Password: values[KeySMTPPassword],
		From: values[KeySMTPFrom], FromName: values[KeySMTPFromName], UseTLS: parseBool(values[KeySMTPUseTLS]),
	}, nil
}

func (s *Service) ResolveSMTPConfig(ctx context.Context, candidate SMTPSettings) (SMTPSettings, error) {
	saved, err := s.SMTPConfig(ctx)
	if err != nil {
		return SMTPSettings{}, err
	}
	candidate.Host = strings.TrimSpace(candidate.Host)
	candidate.Username = strings.TrimSpace(candidate.Username)
	candidate.Password = strings.TrimSpace(candidate.Password)
	candidate.From = strings.TrimSpace(candidate.From)
	candidate.FromName = strings.TrimSpace(candidate.FromName)
	if candidate.Host == "" {
		candidate.Host = saved.Host
	}
	if candidate.Port <= 0 {
		candidate.Port = saved.Port
	}
	if candidate.Username == "" {
		candidate.Username = saved.Username
	}
	if candidate.Password == "" {
		candidate.Password = saved.Password
	}
	if candidate.From == "" {
		candidate.From = saved.From
	}
	if candidate.FromName == "" {
		candidate.FromName = saved.FromName
	}
	if candidate.Port <= 0 {
		candidate.Port = 587
	}
	if candidate.Host == "" {
		return SMTPSettings{}, errors.New("smtp_host is required")
	}
	if candidate.Port > 65535 {
		return SMTPSettings{}, errors.New("smtp_port must be between 1 and 65535")
	}
	return candidate, nil
}

var emailTemplatePlaceholders = map[string][]string{
	"email_verification": {"{{.SiteName}}", "{{.UserName}}", "{{.ActionURL}}"},
	"password_reset":     {"{{.SiteName}}", "{{.ActionURL}}"},
	"quota_limit":        {"{{.SiteName}}", "{{.Period}}", "{{.Limit}}"},
}

func (s *Service) EmailTemplateCatalog(ctx context.Context) (EmailTemplateCatalog, error) {
	custom, _, _, err := s.storedEmailTemplates(ctx)
	if err != nil {
		return EmailTemplateCatalog{}, err
	}
	customized := make(map[string]struct{}, len(custom))
	for _, item := range custom {
		customized[emailTemplateKey(item.Event, item.Locale)] = struct{}{}
	}
	events := make([]EmailTemplateEventInfo, 0, len(emailTemplatePlaceholders))
	seenEvents := make(map[string]struct{}, len(emailTemplatePlaceholders))
	summaries := make([]EmailTemplateSummary, 0, len(auth.DefaultEmailTemplates()))
	placeholderSet := make(map[string]struct{})
	placeholders := make([]string, 0, 8)
	for _, item := range auth.DefaultEmailTemplates() {
		if _, exists := seenEvents[item.Event]; !exists {
			eventPlaceholders := append([]string(nil), emailTemplatePlaceholders[item.Event]...)
			events = append(events, EmailTemplateEventInfo{Event: item.Event, Placeholders: eventPlaceholders})
			seenEvents[item.Event] = struct{}{}
			for _, placeholder := range eventPlaceholders {
				if _, exists := placeholderSet[placeholder]; exists {
					continue
				}
				placeholderSet[placeholder] = struct{}{}
				placeholders = append(placeholders, placeholder)
			}
		}
		_, isCustomized := customized[emailTemplateKey(item.Event, item.Locale)]
		summaries = append(summaries, EmailTemplateSummary{Event: item.Event, Locale: item.Locale, Customized: isCustomized})
	}
	return EmailTemplateCatalog{Events: events, Locales: []string{"zh-CN", "en-US"}, Templates: summaries, Placeholders: placeholders}, nil
}

func (s *Service) EmailTemplate(ctx context.Context, event, locale string) (EmailTemplateDetail, error) {
	official, err := officialEmailTemplate(event, locale)
	if err != nil {
		return EmailTemplateDetail{}, err
	}
	custom, _, _, err := s.storedEmailTemplates(ctx)
	if err != nil {
		return EmailTemplateDetail{}, err
	}
	for _, item := range custom {
		if item.Event == official.Event && item.Locale == official.Locale {
			return emailTemplateDetail(item, true), nil
		}
	}
	return emailTemplateDetail(official, false), nil
}

func (s *Service) UpdateEmailTemplate(ctx context.Context, event, locale, subject, htmlBody string) (EmailTemplateDetail, error) {
	official, err := officialEmailTemplate(event, locale)
	if err != nil {
		return EmailTemplateDetail{}, err
	}
	nextTemplate := EmailTemplate{Event: official.Event, Locale: official.Locale, Subject: strings.TrimSpace(subject), HTML: strings.TrimSpace(htmlBody)}
	if _, err := validateAndMarshalEmailTemplates([]EmailTemplate{nextTemplate}); err != nil {
		return EmailTemplateDetail{}, err
	}
	current, raw, exists, err := s.storedEmailTemplates(ctx)
	if err != nil {
		return EmailTemplateDetail{}, err
	}
	if nextTemplate.Subject == official.Subject && nextTemplate.HTML == official.HTML {
		next := removeEmailTemplate(current, official.Event, official.Locale)
		if exists {
			if err := s.replaceEmailTemplates(ctx, raw, true, next); err != nil {
				return EmailTemplateDetail{}, err
			}
		}
		return emailTemplateDetail(official, false), nil
	}
	replaced := false
	for index := range current {
		if current[index].Event == official.Event && current[index].Locale == official.Locale {
			current[index] = nextTemplate
			replaced = true
			break
		}
	}
	if !replaced {
		current = append(current, nextTemplate)
	}
	if err := s.replaceEmailTemplates(ctx, raw, exists, current); err != nil {
		return EmailTemplateDetail{}, err
	}
	return emailTemplateDetail(nextTemplate, true), nil
}

func (s *Service) RestoreEmailTemplate(ctx context.Context, event, locale string) (EmailTemplateDetail, error) {
	official, err := officialEmailTemplate(event, locale)
	if err != nil {
		return EmailTemplateDetail{}, err
	}
	current, raw, exists, err := s.storedEmailTemplates(ctx)
	if err != nil {
		return EmailTemplateDetail{}, err
	}
	if !exists {
		return emailTemplateDetail(official, false), nil
	}
	next := removeEmailTemplate(current, official.Event, official.Locale)
	if err := s.replaceEmailTemplates(ctx, raw, true, next); err != nil {
		return EmailTemplateDetail{}, err
	}
	return emailTemplateDetail(official, false), nil
}

func (s *Service) storedEmailTemplates(ctx context.Context) ([]EmailTemplate, string, bool, error) {
	values, err := s.repo.GetAll(ctx)
	if err != nil {
		return nil, "", false, err
	}
	raw, exists := values[KeyEmailTemplates]
	if !exists || strings.TrimSpace(raw) == "" {
		return []EmailTemplate{}, raw, exists, nil
	}
	var templates []EmailTemplate
	if err := json.Unmarshal([]byte(raw), &templates); err != nil {
		return nil, raw, true, fmt.Errorf("decode email templates: %w", err)
	}
	if _, err := validateAndMarshalEmailTemplates(templates); err != nil {
		return nil, raw, true, err
	}
	overrides := make([]EmailTemplate, 0, len(templates))
	for _, item := range templates {
		official, err := officialEmailTemplate(item.Event, item.Locale)
		if err != nil || item.Subject != official.Subject || item.HTML != official.HTML {
			overrides = append(overrides, item)
		}
	}
	return overrides, raw, true, nil
}

func (s *Service) replaceEmailTemplates(ctx context.Context, expected string, exists bool, templates []EmailTemplate) error {
	encoded, err := validateAndMarshalEmailTemplates(templates)
	if err != nil {
		return err
	}
	if !exists {
		inserted, err := s.repo.SetIfAbsent(ctx, KeyEmailTemplates, string(encoded))
		if err != nil {
			return err
		}
		if !inserted {
			return ErrSettingsChanged
		}
		return nil
	}
	return s.repo.ReplaceIfUnchanged(ctx, map[string]ValueReplacement{KeyEmailTemplates: {Expected: expected, Value: string(encoded)}})
}

func officialEmailTemplate(event, locale string) (EmailTemplate, error) {
	event = strings.TrimSpace(event)
	locale = strings.TrimSpace(locale)
	for _, item := range auth.DefaultEmailTemplates() {
		if item.Event == event && item.Locale == locale {
			return EmailTemplate{Event: item.Event, Locale: item.Locale, Subject: item.Subject, HTML: item.HTML}, nil
		}
	}
	return EmailTemplate{}, fmt.Errorf("unsupported email template %q", emailTemplateKey(event, locale))
}

func emailTemplateDetail(template EmailTemplate, customized bool) EmailTemplateDetail {
	return EmailTemplateDetail{EmailTemplate: template, Customized: customized, Placeholders: append([]string(nil), emailTemplatePlaceholders[template.Event]...)}
}

func emailTemplateKey(event, locale string) string {
	return event + ":" + locale
}

func removeEmailTemplate(templates []EmailTemplate, event, locale string) []EmailTemplate {
	next := make([]EmailTemplate, 0, len(templates))
	for _, item := range templates {
		if item.Event != event || item.Locale != locale {
			next = append(next, item)
		}
	}
	return next
}

func (s *Service) RegistrationPolicy(ctx context.Context) (RegistrationPolicy, error) {
	values, err := s.readValues(ctx)
	if err != nil {
		return RegistrationPolicy{}, err
	}
	emailVerification := parseBool(values[KeyEmailVerifyEnabled])
	return RegistrationPolicy{Enabled: parseBool(values[KeyRegistrationEnabled]), EmailVerification: emailVerification, PasswordReset: parseBool(values[KeyPasswordResetEnabled]), InvitationRequired: parseBool(values[KeyInvitationRequired]), AllowedDomains: parseStringList(values[KeyAllowedEmailDomains], []string{}), InvitationCodes: parseStringList(values[KeyInvitationCodes], []string{})}, nil
}

// ValidateInvitationCode 校验邀请码是否可用但不消费它。
// 注册流程先校验、注册成功后再消费，避免因为后续步骤失败（邮箱重复、
// 邮件发送失败等）把用户手里的邀请码白白烧掉。
func (s *Service) ValidateInvitationCode(ctx context.Context, code string) error {
	_, err := s.findInvitationCode(ctx, code)
	return err
}

func (s *Service) ConsumeInvitationCode(ctx context.Context, code string) error {
	return s.repo.ConsumeInvitationCode(ctx, code)
}

func (s *Service) RestoreInvitationCode(ctx context.Context, code string) error {
	return s.repo.RestoreInvitationCode(ctx, code)
}

func (s *Service) findInvitationCode(ctx context.Context, code string) ([]string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, ErrInvitationCodeRequired
	}
	values, err := s.readValues(ctx)
	if err != nil {
		return nil, err
	}
	codes := parseStringList(values[KeyInvitationCodes], []string{})
	for index, candidate := range codes {
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			remaining := make([]string, 0, len(codes)-1)
			remaining = append(remaining, codes[:index]...)
			remaining = append(remaining, codes[index+1:]...)
			return remaining, nil
		}
	}
	return nil, ErrInvitationCodeInvalid
}

func (s *Service) LoginSecurity(ctx context.Context) (LoginSecuritySettings, error) {
	values, err := s.readValues(ctx)
	if err != nil {
		return LoginSecuritySettings{}, err
	}
	return LoginSecuritySettings{TurnstileEnabled: parseBool(values[KeyTurnstileEnabled]), TurnstileSecret: values[KeyTurnstileSecretKey]}, nil
}

func (s *Service) parse(values map[string]string) AdminSettings {
	_, offset := time.Now().Zone()
	return AdminSettings{
		PublicSettings: PublicSettings{
			SiteName:                 values[KeySiteName],
			SiteSubtitle:             values[KeySiteSubtitle],
			SiteLogo:                 values[KeySiteLogo],
			PublicBaseURL:            values[KeyPublicBaseURL],
			APIBaseURL:               "/api/v1",
			GatewayBasePath:          values[KeyGatewayBasePath],
			SetupCompleted:           parseBool(values[KeySetupCompleted]),
			DefaultLocale:            values[KeyDefaultLocale],
			EnabledLocales:           parseStringList(values[KeyEnabledLocales], []string{"en-US", "zh-CN"}),
			OIDCEnabled:              parseBool(values[KeyOIDCEnabled]),
			OIDCProviderName:         values[KeyOIDCProviderName],
			OIDCRequireVerifiedEmail: parseBool(values[KeyOIDCRequireVerifiedEmail]),
			FeishuEnabled:            parseBool(values[KeyFeishuEnabled]),
			FeishuRegion:             values[KeyFeishuRegion],
			GitHubOAuthEnabled:       parseBool(values[KeyGitHubOAuthEnabled]), GoogleOAuthEnabled: parseBool(values[KeyGoogleOAuthEnabled]),
			DingTalkEnabled:       parseBool(values[KeyDingTalkEnabled]),
			RegistrationEnabled:   parseBool(values[KeyRegistrationEnabled]),
			EmailVerifyEnabled:    parseBool(values[KeyEmailVerifyEnabled]),
			PasswordResetEnabled:  parseBool(values[KeyPasswordResetEnabled]),
			AllowedEmailDomains:   parseStringList(values[KeyAllowedEmailDomains], []string{}),
			TOTPEnabled:           parseBool(values[KeyTOTPEnabled]),
			TurnstileEnabled:      parseBool(values[KeyTurnstileEnabled]),
			TurnstileSiteKey:      values[KeyTurnstileSiteKey],
			InvitationRequired:    parseBool(values[KeyInvitationRequired]),
			LoginAgreementEnabled: parseBool(values[KeyLoginAgreementEnabled]),
			LoginAgreementMode:    values[KeyLoginAgreementMode], LoginAgreementUpdatedAt: values[KeyLoginAgreementUpdatedAt], LegalDocuments: parseLegalDocuments(values[KeyLegalDocuments]), BackendMode: parseBool(values[KeyBackendMode]), SupportContact: values[KeySupportContact], DocumentationURL: values[KeyDocumentationURL],
			CustomEndpoints: parseCustomEndpoints(values[KeyCustomEndpoints]), CustomMenuItems: parseCustomMenuItems(values[KeyCustomMenuItems]),
			ServiceCenterMode:     values[KeyServiceCenterMode],
			ChannelMonitorEnabled: parseBool(values[KeyChannelMonitorEnabled]), AvailableChannelsEnabled: parseBool(values[KeyAvailableChannels]), RiskControlEnabled: parseBool(values[KeyRiskControlEnabled]), CyberSessionBlockEnabled: parseBool(values[KeyCyberSessionBlock]),
			BackupS3Enabled: parseBool(values[KeyBackupS3Enabled]),
			Version:         s.version,
			ServerTimezone:  timezoneName(),
			ServerUTCOffset: formatUTCOffset(offset),
			StorageMode:     s.storageMode,
			DemoMode:        s.demoMode,
		},
		OIDCIssuerURL:              values[KeyOIDCIssuerURL],
		OIDCClientID:               values[KeyOIDCClientID],
		OIDCClientSecretConfigured: strings.TrimSpace(values[KeyOIDCClientSecret]) != "",
		FeishuAppID:                values[KeyFeishuAppID],
		FeishuConfigured:           strings.TrimSpace(values[KeyFeishuAppSecret]) != "",
		GitHubOAuthClientID:        values[KeyGitHubOAuthClientID], GitHubOAuthConfigured: strings.TrimSpace(values[KeyGitHubOAuthSecret]) != "", GoogleOAuthClientID: values[KeyGoogleOAuthClientID], GoogleOAuthConfigured: strings.TrimSpace(values[KeyGoogleOAuthSecret]) != "",
		DingTalkClientID: values[KeyDingTalkClientID], DingTalkConfigured: strings.TrimSpace(values[KeyDingTalkClientSecret]) != "",
		InvitationCodes:     parseStringList(values[KeyInvitationCodes], []string{}),
		TrustedProxyHeaders: parseBool(values[KeyTrustedProxyHeaders]),
		TrustedProxyCIDRs:   parseStringList(values[KeyTrustedProxyCIDRs], []string{}),
		TurnstileConfigured: strings.TrimSpace(values[KeyTurnstileSecretKey]) != "",
		DefaultConcurrency:  parseInt(values[KeyDefaultConcurrency], 5),
		DefaultRPM:          parseInt(values[KeyDefaultRPM], 0),
		AuthSourceDefaults:  parseAuthSourceDefaults(values[KeyAuthSourceDefaults]),
		SMTPHost:            values[KeySMTPHost], SMTPPort: parseInt(values[KeySMTPPort], 587), SMTPUsername: values[KeySMTPUsername], SMTPFrom: values[KeySMTPFrom], SMTPFromName: values[KeySMTPFromName], SMTPUseTLS: parseBool(values[KeySMTPUseTLS]), SMTPConfigured: strings.TrimSpace(values[KeySMTPHost]) != "" && strings.TrimSpace(values[KeySMTPFrom]) != "",
		EmailTemplates:      parseEmailTemplates(values[KeyEmailTemplates]),
		LoginAgreementTitle: values[KeyLoginAgreementTitle], LoginAgreementContent: values[KeyLoginAgreementContent],
		DefaultPageSize: parseInt(values[KeyDefaultPageSize], 20), PageSizeOptions: parseIntList(values[KeyPageSizeOptions], []int{10, 20, 50}), HomeContent: values[KeyHomeContent], HideImportButton: parseBool(values[KeyHideImportButton]),
		ChannelMonitorIntervalSeconds: parseInt(values[KeyChannelMonitorInterval], 300), CyberSessionBlockTTLSeconds: parseInt(values[KeyCyberSessionBlockTTL], 3600),
		BackupS3Endpoint: values[KeyBackupS3Endpoint], BackupS3Region: values[KeyBackupS3Region], BackupS3Bucket: values[KeyBackupS3Bucket], BackupS3Prefix: values[KeyBackupS3Prefix], BackupS3AccessKey: values[KeyBackupS3AccessKey], BackupS3Configured: strings.TrimSpace(values[KeyBackupS3SecretKey]) != "", BackupS3PathStyle: parseBool(values[KeyBackupS3PathStyle]), BackupRetentionDays: parseInt(values[KeyBackupRetentionDays], 30), BackupMaxRetained: parseInt(values[KeyBackupMaxRetained], 10), BackupScheduleEnabled: parseBool(values[KeyBackupScheduleEnabled]), BackupIntervalHours: parseInt(values[KeyBackupIntervalHours], 24),
		DataRetentionDays: parseInt(values[KeyDataRetentionDays], 30),
		PromptLoggingMode: values[KeyPromptLoggingMode],
		UpdateChannel:     values[KeyUpdateChannel],
	}
}

type BackupS3Config struct {
	Enabled, PathStyle               bool
	Endpoint, Region, Bucket, Prefix string
	AccessKey, SecretKey             string
	RetentionDays, MaxRetained       int
}

func (s *Service) BackupS3Config(ctx context.Context) (BackupS3Config, error) {
	values, err := s.readValues(ctx)
	if err != nil {
		return BackupS3Config{}, err
	}
	return BackupS3Config{Enabled: parseBool(values[KeyBackupS3Enabled]), PathStyle: parseBool(values[KeyBackupS3PathStyle]), Endpoint: values[KeyBackupS3Endpoint], Region: values[KeyBackupS3Region], Bucket: values[KeyBackupS3Bucket], Prefix: values[KeyBackupS3Prefix], AccessKey: values[KeyBackupS3AccessKey], SecretKey: values[KeyBackupS3SecretKey], RetentionDays: parseInt(values[KeyBackupRetentionDays], 30), MaxRetained: parseInt(values[KeyBackupMaxRetained], 10)}, nil
}

func defaults() map[string]string {
	return map[string]string{
		KeySiteName:                 "AsterRouter",
		KeySiteSubtitle:             "AI Gateway Control Plane",
		KeySiteLogo:                 "",
		KeyPublicBaseURL:            "",
		KeyDefaultLocale:            "en-US",
		KeyEnabledLocales:           `["en-US","zh-CN"]`,
		KeySetupCompleted:           "false",
		KeyGatewayBasePath:          "/v1",
		KeyOIDCEnabled:              "false",
		KeyOIDCProviderName:         "OIDC",
		KeyOIDCIssuerURL:            "",
		KeyOIDCClientID:             "",
		KeyOIDCClientSecret:         "",
		KeyOIDCRequireVerifiedEmail: "true",
		KeyFeishuEnabled:            "false",
		KeyFeishuRegion:             "cn",
		KeyFeishuAppID:              "",
		KeyFeishuAppSecret:          "",
		KeyGitHubOAuthEnabled:       "false", KeyGitHubOAuthClientID: "", KeyGitHubOAuthSecret: "", KeyGoogleOAuthEnabled: "false", KeyGoogleOAuthClientID: "", KeyGoogleOAuthSecret: "",
		KeyDingTalkEnabled: "false", KeyDingTalkClientID: "", KeyDingTalkClientSecret: "",
		KeyRegistrationEnabled: "false", KeyEmailVerifyEnabled: "false", KeyPasswordResetEnabled: "false", KeyAllowedEmailDomains: "[]", KeyInvitationRequired: "false", KeyInvitationCodes: "[]", KeyTOTPEnabled: "false", KeyTrustedProxyHeaders: "false", KeyTrustedProxyCIDRs: "[]", KeyTurnstileEnabled: "false", KeyTurnstileSiteKey: "", KeyTurnstileSecretKey: "", KeyDefaultConcurrency: "5", KeyDefaultRPM: "0", KeySMTPHost: "", KeySMTPPort: "587", KeySMTPUsername: "", KeySMTPPassword: "", KeySMTPFrom: "", KeySMTPFromName: "", KeySMTPUseTLS: "false", KeyLoginAgreementEnabled: "false", KeyLoginAgreementTitle: "Terms of Service", KeyLoginAgreementContent: "",
		KeyAuthSourceDefaults: "{}",
		KeyEmailTemplates:     "[]",
		KeyBackendMode:        "false", KeyDefaultPageSize: "20", KeyPageSizeOptions: "[10,20,50]", KeySupportContact: "", KeyDocumentationURL: "", KeyHomeContent: "", KeyHideImportButton: "false", KeyLoginAgreementMode: "modal", KeyLoginAgreementUpdatedAt: "", KeyLegalDocuments: "[]",
		KeyCustomEndpoints: "[]", KeyCustomMenuItems: "[]",
		KeyChannelMonitorEnabled: "true", KeyChannelMonitorInterval: "300", KeyAvailableChannels: "true", KeyRiskControlEnabled: "true", KeyCyberSessionBlock: "true", KeyCyberSessionBlockTTL: "3600",
		KeyBackupS3Enabled: "false", KeyBackupS3Endpoint: "", KeyBackupS3Region: "auto", KeyBackupS3Bucket: "", KeyBackupS3Prefix: "asterrouter", KeyBackupS3AccessKey: "", KeyBackupS3SecretKey: "", KeyBackupS3PathStyle: "false", KeyBackupRetentionDays: "30", KeyBackupMaxRetained: "10", KeyBackupScheduleEnabled: "false", KeyBackupIntervalHours: "24",
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
	if in.OIDCEnabled {
		if strings.TrimSpace(in.OIDCClientID) == "" {
			return nil, errors.New("oidc_client_id is required when OIDC login is enabled")
		}
		issuer, err := url.Parse(strings.TrimSpace(in.OIDCIssuerURL))
		if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil {
			return nil, errors.New("oidc_issuer_url must be an https URL when OIDC login is enabled")
		}
	}
	if in.FeishuEnabled && strings.TrimSpace(in.FeishuAppID) == "" {
		return nil, errors.New("feishu_app_id is required when feishu login is enabled")
	}
	if in.FeishuEnabled && !in.FeishuConfigured && strings.TrimSpace(in.FeishuAppSecret) == "" {
		return nil, errors.New("feishu_app_secret is required when feishu login is enabled")
	}
	if in.GitHubOAuthEnabled && strings.TrimSpace(in.GitHubOAuthClientID) == "" {
		return nil, errors.New("github_oauth_client_id is required")
	}
	if in.GitHubOAuthEnabled && !in.GitHubOAuthConfigured && strings.TrimSpace(in.GitHubOAuthClientSecret) == "" {
		return nil, errors.New("github_oauth_client_secret is required")
	}
	if in.GoogleOAuthEnabled && strings.TrimSpace(in.GoogleOAuthClientID) == "" {
		return nil, errors.New("google_oauth_client_id is required")
	}
	if in.GoogleOAuthEnabled && !in.GoogleOAuthConfigured && strings.TrimSpace(in.GoogleOAuthClientSecret) == "" {
		return nil, errors.New("google_oauth_client_secret is required")
	}
	if in.DingTalkEnabled && strings.TrimSpace(in.DingTalkClientID) == "" {
		return nil, errors.New("dingtalk_client_id is required")
	}
	if in.DingTalkEnabled && !in.DingTalkConfigured && strings.TrimSpace(in.DingTalkClientSecret) == "" {
		return nil, errors.New("dingtalk_client_secret is required")
	}
	if in.DefaultConcurrency < 0 || in.DefaultRPM < 0 {
		return nil, errors.New("default user limits cannot be negative")
	}
	if err := validateAuthSourceDefaults(in.AuthSourceDefaults); err != nil {
		return nil, err
	}
	if in.SMTPPort < 1 || in.SMTPPort > 65535 {
		return nil, errors.New("smtp_port must be between 1 and 65535")
	}
	if in.EmailVerifyEnabled || in.PasswordResetEnabled {
		if strings.TrimSpace(in.PublicBaseURL) == "" {
			return nil, errors.New("public_base_url is required when authentication email is enabled")
		}
		if strings.TrimSpace(in.SMTPHost) == "" || strings.TrimSpace(in.SMTPFrom) == "" {
			return nil, errors.New("SMTP host and sender are required when authentication email is enabled")
		}
	}
	if in.EmailVerifyEnabled || in.PasswordResetEnabled || in.OIDCEnabled || in.FeishuEnabled || in.GitHubOAuthEnabled || in.GoogleOAuthEnabled || in.DingTalkEnabled {
		if err := validateSecureAuthenticationBaseURL(in.PublicBaseURL); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(in.SMTPFrom) != "" {
		address, err := mail.ParseAddress(strings.TrimSpace(in.SMTPFrom))
		if err != nil || address.Address != strings.TrimSpace(in.SMTPFrom) {
			return nil, errors.New("smtp_from must be a valid email address")
		}
	}
	if in.TurnstileEnabled && (strings.TrimSpace(in.TurnstileSiteKey) == "" || (!in.TurnstileConfigured && strings.TrimSpace(in.TurnstileSecretKey) == "")) {
		return nil, errors.New("Turnstile site key and secret are required when Turnstile is enabled")
	}
	trustedProxyCIDRs, err := normalizeTrustedProxyCIDRs(in.TrustedProxyCIDRs)
	if err != nil {
		return nil, err
	}
	if in.TrustedProxyHeaders && len(trustedProxyCIDRs) == 0 {
		return nil, errors.New("trusted_proxy_cidrs is required when proxy headers are trusted")
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
	if err := validateSiteLogo(in.SiteLogo); err != nil {
		return nil, err
	}
	if err := validateCustomNavigation(in.CustomEndpoints, in.CustomMenuItems); err != nil {
		return nil, err
	}
	if in.ChannelMonitorIntervalSeconds < 30 || in.ChannelMonitorIntervalSeconds > 86400 {
		return nil, errors.New("channel_monitor_interval_seconds must be between 30 and 86400")
	}
	if in.CyberSessionBlockTTLSeconds < 60 || in.CyberSessionBlockTTLSeconds > 2592000 {
		return nil, errors.New("cyber_session_block_ttl_seconds must be between 60 and 2592000")
	}
	if in.BackupS3Enabled && (strings.TrimSpace(in.BackupS3Bucket) == "" || strings.TrimSpace(in.BackupS3AccessKey) == "" || (!in.BackupS3Configured && strings.TrimSpace(in.BackupS3SecretKey) == "")) {
		return nil, errors.New("S3 bucket, access key, and secret key are required when S3 backup is enabled")
	}
	if err := validateOptionalHTTPURL("backup_s3_endpoint", in.BackupS3Endpoint); err != nil {
		return nil, err
	}
	if in.BackupRetentionDays < 1 || in.BackupRetentionDays > 3650 || in.BackupMaxRetained < 1 || in.BackupMaxRetained > 1000 {
		return nil, errors.New("backup retention settings are out of range")
	}
	if in.BackupIntervalHours < 1 || in.BackupIntervalHours > 24*30 {
		return nil, errors.New("backup interval must be between 1 and 720 hours")
	}
	normalizedDomains, err := normalizeAllowedEmailDomains(in.AllowedEmailDomains)
	if err != nil {
		return nil, err
	}
	locales, _ := json.Marshal(in.EnabledLocales)
	domains, _ := json.Marshal(normalizedDomains)
	proxyCIDRs, _ := json.Marshal(trustedProxyCIDRs)
	invitationCodes, _ := json.Marshal(in.InvitationCodes)
	pageSizes, _ := json.Marshal(in.PageSizeOptions)
	legalDocuments, _ := json.Marshal(in.LegalDocuments)
	customEndpoints, _ := json.Marshal(in.CustomEndpoints)
	customMenuItems, _ := json.Marshal(in.CustomMenuItems)
	emailTemplates, err := validateAndMarshalEmailTemplates(in.EmailTemplates)
	if err != nil {
		return nil, err
	}
	authSourceDefaults, _ := json.Marshal(in.AuthSourceDefaults)
	return map[string]string{
		KeySiteName:                 strings.TrimSpace(in.SiteName),
		KeySiteSubtitle:             strings.TrimSpace(in.SiteSubtitle),
		KeySiteLogo:                 strings.TrimSpace(in.SiteLogo),
		KeyPublicBaseURL:            strings.TrimSpace(in.PublicBaseURL),
		KeyDefaultLocale:            in.DefaultLocale,
		KeyEnabledLocales:           string(locales),
		KeyGatewayBasePath:          in.GatewayBasePath,
		KeyOIDCEnabled:              strconv.FormatBool(in.OIDCEnabled),
		KeyOIDCProviderName:         strings.TrimSpace(in.OIDCProviderName),
		KeyOIDCIssuerURL:            strings.TrimSpace(in.OIDCIssuerURL),
		KeyOIDCClientID:             strings.TrimSpace(in.OIDCClientID),
		KeyOIDCClientSecret:         strings.TrimSpace(in.OIDCClientSecret),
		KeyOIDCRequireVerifiedEmail: strconv.FormatBool(in.OIDCRequireVerifiedEmail),
		KeyFeishuEnabled:            strconv.FormatBool(in.FeishuEnabled),
		KeyFeishuRegion:             strings.TrimSpace(in.FeishuRegion),
		KeyFeishuAppID:              strings.TrimSpace(in.FeishuAppID),
		KeyFeishuAppSecret:          strings.TrimSpace(in.FeishuAppSecret),
		KeyGitHubOAuthEnabled:       strconv.FormatBool(in.GitHubOAuthEnabled), KeyGitHubOAuthClientID: strings.TrimSpace(in.GitHubOAuthClientID), KeyGitHubOAuthSecret: strings.TrimSpace(in.GitHubOAuthClientSecret), KeyGoogleOAuthEnabled: strconv.FormatBool(in.GoogleOAuthEnabled), KeyGoogleOAuthClientID: strings.TrimSpace(in.GoogleOAuthClientID), KeyGoogleOAuthSecret: strings.TrimSpace(in.GoogleOAuthClientSecret),
		KeyDingTalkEnabled: strconv.FormatBool(in.DingTalkEnabled), KeyDingTalkClientID: strings.TrimSpace(in.DingTalkClientID), KeyDingTalkClientSecret: strings.TrimSpace(in.DingTalkClientSecret),
		KeyRegistrationEnabled: strconv.FormatBool(in.RegistrationEnabled), KeyEmailVerifyEnabled: strconv.FormatBool(in.EmailVerifyEnabled), KeyPasswordResetEnabled: strconv.FormatBool(in.PasswordResetEnabled), KeyAllowedEmailDomains: string(domains), KeyInvitationRequired: strconv.FormatBool(in.InvitationRequired), KeyInvitationCodes: string(invitationCodes), KeyTOTPEnabled: strconv.FormatBool(in.TOTPEnabled), KeyTrustedProxyHeaders: strconv.FormatBool(in.TrustedProxyHeaders), KeyTrustedProxyCIDRs: string(proxyCIDRs), KeyTurnstileEnabled: strconv.FormatBool(in.TurnstileEnabled), KeyTurnstileSiteKey: strings.TrimSpace(in.TurnstileSiteKey), KeyTurnstileSecretKey: strings.TrimSpace(in.TurnstileSecretKey), KeyDefaultConcurrency: strconv.Itoa(in.DefaultConcurrency), KeyDefaultRPM: strconv.Itoa(in.DefaultRPM), KeySMTPHost: strings.TrimSpace(in.SMTPHost), KeySMTPPort: strconv.Itoa(in.SMTPPort), KeySMTPUsername: strings.TrimSpace(in.SMTPUsername), KeySMTPPassword: strings.TrimSpace(in.SMTPPassword), KeySMTPFrom: strings.TrimSpace(in.SMTPFrom), KeySMTPFromName: strings.TrimSpace(in.SMTPFromName), KeySMTPUseTLS: strconv.FormatBool(in.SMTPUseTLS), KeyLoginAgreementEnabled: strconv.FormatBool(in.LoginAgreementEnabled), KeyLoginAgreementTitle: strings.TrimSpace(in.LoginAgreementTitle), KeyLoginAgreementContent: strings.TrimSpace(in.LoginAgreementContent),
		KeyEmailTemplates:     string(emailTemplates),
		KeyAuthSourceDefaults: string(authSourceDefaults),
		KeyBackendMode:        strconv.FormatBool(in.BackendMode), KeyDefaultPageSize: strconv.Itoa(in.DefaultPageSize), KeyPageSizeOptions: string(pageSizes), KeySupportContact: strings.TrimSpace(in.SupportContact), KeyDocumentationURL: strings.TrimSpace(in.DocumentationURL), KeyHomeContent: in.HomeContent, KeyHideImportButton: strconv.FormatBool(in.HideImportButton), KeyLoginAgreementMode: strings.TrimSpace(in.LoginAgreementMode), KeyLoginAgreementUpdatedAt: strings.TrimSpace(in.LoginAgreementUpdatedAt), KeyLegalDocuments: string(legalDocuments),
		KeyCustomEndpoints: string(customEndpoints), KeyCustomMenuItems: string(customMenuItems),
		KeyChannelMonitorEnabled: strconv.FormatBool(in.ChannelMonitorEnabled), KeyChannelMonitorInterval: strconv.Itoa(in.ChannelMonitorIntervalSeconds), KeyAvailableChannels: strconv.FormatBool(in.AvailableChannelsEnabled), KeyRiskControlEnabled: strconv.FormatBool(in.RiskControlEnabled), KeyCyberSessionBlock: strconv.FormatBool(in.CyberSessionBlockEnabled), KeyCyberSessionBlockTTL: strconv.Itoa(in.CyberSessionBlockTTLSeconds),
		KeyBackupS3Enabled: strconv.FormatBool(in.BackupS3Enabled), KeyBackupS3Endpoint: strings.TrimSpace(in.BackupS3Endpoint), KeyBackupS3Region: strings.TrimSpace(in.BackupS3Region), KeyBackupS3Bucket: strings.TrimSpace(in.BackupS3Bucket), KeyBackupS3Prefix: strings.Trim(strings.TrimSpace(in.BackupS3Prefix), "/"), KeyBackupS3AccessKey: strings.TrimSpace(in.BackupS3AccessKey), KeyBackupS3SecretKey: strings.TrimSpace(in.BackupS3SecretKey), KeyBackupS3PathStyle: strconv.FormatBool(in.BackupS3PathStyle), KeyBackupRetentionDays: strconv.Itoa(in.BackupRetentionDays), KeyBackupMaxRetained: strconv.Itoa(in.BackupMaxRetained), KeyBackupScheduleEnabled: strconv.FormatBool(in.BackupScheduleEnabled), KeyBackupIntervalHours: strconv.Itoa(in.BackupIntervalHours),
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

func parseInt64(value string, fallback int64) int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
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

func parseEmailTemplates(value string) []EmailTemplate {
	var templates []EmailTemplate
	if json.Unmarshal([]byte(value), &templates) != nil || templates == nil {
		return []EmailTemplate{}
	}
	return templates
}

func parseCustomEndpoints(value string) []CustomEndpoint {
	var out []CustomEndpoint
	if json.Unmarshal([]byte(value), &out) != nil || out == nil {
		return []CustomEndpoint{}
	}
	return out
}
func parseCustomMenuItems(value string) []CustomMenuItem {
	var out []CustomMenuItem
	if json.Unmarshal([]byte(value), &out) != nil || out == nil {
		return []CustomMenuItem{}
	}
	return out
}

func parseAuthSourceDefaults(value string) map[string]AuthSourceDefault {
	out := map[string]AuthSourceDefault{}
	if json.Unmarshal([]byte(value), &out) != nil {
		return map[string]AuthSourceDefault{}
	}
	return out
}

func validateAuthSourceDefaults(values map[string]AuthSourceDefault) error {
	allowed := map[string]bool{"local": true, "oidc": true, "feishu": true, "dingtalk": true, "github": true, "google": true}
	for source, value := range values {
		if !allowed[source] {
			return fmt.Errorf("unsupported auth source default %q", source)
		}
		if value.Concurrency < 0 || value.RPM < 0 {
			return fmt.Errorf("auth source default %q cannot be negative", source)
		}
	}
	return nil
}

func validateSiteLogo(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 || (!strings.HasPrefix(parts[0], "data:image/png;base64") && !strings.HasPrefix(parts[0], "data:image/jpeg;base64") && !strings.HasPrefix(parts[0], "data:image/webp;base64")) {
		return errors.New("site_logo must be a PNG, JPEG, or WebP data URL")
	}
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil || len(raw) > 1024*1024 {
		return errors.New("site_logo must be valid and no larger than 1 MiB")
	}
	return nil
}

func validateCustomNavigation(endpoints []CustomEndpoint, items []CustomMenuItem) error {
	names := map[string]bool{}
	for _, endpoint := range endpoints {
		name := strings.TrimSpace(endpoint.Name)
		if name == "" || strings.TrimSpace(endpoint.Endpoint) == "" {
			return errors.New("custom endpoints require name and endpoint")
		}
		if names[name] {
			return errors.New("custom endpoint names must be unique")
		}
		names[name] = true
	}
	ids := map[string]bool{}
	for _, item := range items {
		if item.ID == "" || strings.TrimSpace(item.Label) == "" || strings.TrimSpace(item.URL) == "" {
			return errors.New("custom menu items require id, label, and URL")
		}
		if ids[item.ID] {
			return errors.New("custom menu item ids must be unique")
		}
		ids[item.ID] = true
		if !strings.HasPrefix(item.URL, "/") {
			if err := validateOptionalHTTPURL("custom menu URL", item.URL); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateAndMarshalEmailTemplates(templates []EmailTemplate) ([]byte, error) {
	allowedEvents := map[string]bool{"email_verification": true, "password_reset": true, "quota_limit": true}
	seen := map[string]bool{}
	for _, item := range templates {
		key := item.Event + ":" + item.Locale
		if !allowedEvents[item.Event] || !isLocale(item.Locale) || strings.TrimSpace(item.Subject) == "" || strings.TrimSpace(item.HTML) == "" {
			return nil, fmt.Errorf("invalid email template %q", key)
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate email template %q", key)
		}
		if _, _, err := auth.RenderEmailTemplate(item.Subject, item.HTML, auth.EmailTemplateData{}); err != nil {
			return nil, fmt.Errorf("invalid email template %q: %w", key, err)
		}
		seen[key] = true
	}
	return json.Marshal(templates)
}

var (
	legalSlugPattern   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	emailDomainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)
)

func normalizeAllowedEmailDomains(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		value = strings.TrimPrefix(value, "@")
		wildcard := strings.HasPrefix(value, "*.")
		domain := strings.TrimPrefix(value, "*.")
		if !emailDomainPattern.MatchString(domain) {
			return nil, fmt.Errorf("invalid allowed email domain %q", raw)
		}
		if wildcard {
			value = "*." + domain
		} else {
			value = domain
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func normalizeTrustedProxyCIDRs(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			if ip.To4() != nil {
				value = ip.String() + "/32"
			} else {
				value = ip.String() + "/128"
			}
		} else {
			_, network, err := net.ParseCIDR(value)
			if err != nil {
				return nil, fmt.Errorf("invalid trusted proxy CIDR %q", raw)
			}
			value = network.String()
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

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

func validateSecureAuthenticationBaseURL(value string) error {
	value = strings.TrimSpace(value)
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawFragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return errors.New("public_base_url must be an origin URL without a path, query, fragment, or user credentials")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	hostname := strings.TrimSpace(parsed.Hostname())
	if parsed.Scheme == "http" && (strings.EqualFold(hostname, "localhost") || isLoopbackHost(hostname)) {
		return nil
	}
	return errors.New("public_base_url must use https when authentication email or external login is enabled")
}

// ValidateSecureAuthenticationBaseURL applies the same trust-boundary check
// when authentication links are generated from persisted settings.
func ValidateSecureAuthenticationBaseURL(value string) error {
	return validateSecureAuthenticationBaseURL(value)
}

func isLoopbackHost(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isLocale(value string) bool {
	return value == "en-US" || value == "zh-CN"
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
