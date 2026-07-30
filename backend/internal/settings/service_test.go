package settings

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/auth"
)

func TestServiceDefaults(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
	got, err := svc.Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin() error = %v", err)
	}
	if got.SiteName != "AsterRouter" {
		t.Fatalf("SiteName = %q", got.SiteName)
	}
	if got.DefaultLocale != "en-US" {
		t.Fatalf("DefaultLocale = %q", got.DefaultLocale)
	}
	if got.GatewayBasePath != "/v1" {
		t.Fatalf("GatewayBasePath = %q", got.GatewayBasePath)
	}
}

func TestEmailTemplatesListUpdateAndRestore(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
	catalog, err := svc.EmailTemplateCatalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Events) != 6 || len(catalog.Locales) != 2 || len(catalog.Templates) != 12 {
		t.Fatalf("EmailTemplateCatalog() = %+v", catalog)
	}
	detail, err := svc.EmailTemplate(t.Context(), "password_reset", "en-US")
	if err != nil || detail.Event != "password_reset" || detail.Customized {
		t.Fatalf("EmailTemplate() = %+v, %v", detail, err)
	}
	detail, err = svc.UpdateEmailTemplate(t.Context(), "password_reset", "en-US", "Custom {{.SiteName}}", "<p>{{.ActionURL}}</p>")
	if err != nil || !detail.Customized || detail.Subject != "Custom {{.SiteName}}" {
		t.Fatalf("UpdateEmailTemplate() = %+v, %v", detail, err)
	}
	admin, err := svc.Admin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	admin.EmailTemplates = nil
	admin.SiteSubtitle = "Updated without a template snapshot"
	if _, err := svc.Update(t.Context(), admin); err != nil {
		t.Fatalf("Update() independent settings: %v", err)
	}
	detail, err = svc.EmailTemplate(t.Context(), "password_reset", "en-US")
	if err != nil || detail.Subject != "Custom {{.SiteName}}" {
		t.Fatalf("broad settings update replaced template: %+v, %v", detail, err)
	}
	detail, err = svc.RestoreEmailTemplate(t.Context(), "password_reset", "en-US")
	if err != nil || detail.Customized || detail.Subject == "Custom {{.SiteName}}" {
		t.Fatalf("RestoreEmailTemplate() = %+v, %v", detail, err)
	}
	if _, err := svc.UpdateEmailTemplate(t.Context(), "password_reset", "en-US", "{{.UnknownField}}", "<p>body</p>"); err == nil || !strings.Contains(err.Error(), "invalid email template") {
		t.Fatalf("UpdateEmailTemplate(invalid syntax) error = %v", err)
	}
	if _, err := svc.EmailTemplate(t.Context(), "unknown", "en-US"); err == nil {
		t.Fatal("EmailTemplate() accepted an unsupported event")
	}
}

func TestEmailTemplateUpdateDoesNotOverwriteConcurrentSettings(t *testing.T) {
	repo := &conflictOnceSettingsRepository{MemoryRepository: NewMemoryRepository()}
	if err := repo.SetMultiple(t.Context(), map[string]string{KeyEmailTemplates: "[]"}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory"})
	_, err := svc.UpdateEmailTemplate(t.Context(), "balance_low", "en-US", "Custom {{.SiteName}}", "<p>{{.Amount}}</p>")
	if !errors.Is(err, ErrSettingsChanged) {
		t.Fatalf("UpdateEmailTemplate() error = %v", err)
	}
	values, err := repo.GetAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if values[KeyEmailTemplates] != "[]" {
		t.Fatalf("conflicting template update changed stored value: %s", values[KeyEmailTemplates])
	}
}

func TestLegacyDefaultTemplatesAreNotReportedAsCustomized(t *testing.T) {
	repo := NewMemoryRepository()
	legacy, err := json.Marshal(auth.DefaultEmailTemplates())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMultiple(t.Context(), map[string]string{KeyEmailTemplates: string(legacy)}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory"})
	catalog, err := svc.EmailTemplateCatalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range catalog.Templates {
		if item.Customized {
			t.Fatalf("legacy default template was reported as customized: %+v", item)
		}
	}
	detail, err := svc.EmailTemplate(t.Context(), "email_verification", "zh-CN")
	if err != nil || detail.Customized {
		t.Fatalf("EmailTemplate() = %+v, %v", detail, err)
	}
}

func TestResolveSMTPConfigUsesUnsavedValuesAndStoredPassword(t *testing.T) {
	repo := NewMemoryRepository()
	if err := repo.SetMultiple(t.Context(), map[string]string{
		KeySMTPHost: "saved.example.test", KeySMTPPort: "587", KeySMTPUsername: "saved-user",
		KeySMTPPassword: "saved-secret", KeySMTPFrom: "saved@example.test", KeySMTPFromName: "Saved sender",
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory"})
	resolved, err := svc.ResolveSMTPConfig(t.Context(), SMTPSettings{
		Host: "new.example.test", Port: 465, Username: "new-user", From: "new@example.test", FromName: "New sender", UseTLS: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Host != "new.example.test" || resolved.Port != 465 || resolved.Username != "new-user" || resolved.Password != "saved-secret" || resolved.From != "new@example.test" || !resolved.UseTLS {
		t.Fatalf("ResolveSMTPConfig() = %+v", resolved)
	}
}

func TestAuthenticationSettingsRoundTrip(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory", SecretKey: "settings-test-secret"})
	current, err := svc.Admin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	current.PublicBaseURL = "https://router.example.test"
	current.PasswordResetEnabled = true
	current.EmailVerifyEnabled = false
	current.AllowedEmailDomains = []string{"EXAMPLE.COM", "*.corp.example.com"}
	current.TrustedProxyHeaders = true
	current.TrustedProxyCIDRs = []string{"10.0.0.0/8", "192.168.1.5"}
	current.TurnstileEnabled = true
	current.TurnstileSiteKey = "site-key"
	current.TurnstileSecretKey = "secret-key"
	current.SMTPHost = "smtp.example.test"
	current.SMTPUsername = "mailer"
	current.SMTPPassword = "smtp-secret"
	current.SMTPFrom = "noreply@example.test"
	current.SMTPFromName = "AsterRouter Security"
	current.SMTPUseTLS = true

	updated, err := svc.Update(t.Context(), current)
	if err != nil {
		t.Fatalf("Update(): %v", err)
	}
	if !updated.PasswordResetEnabled || updated.EmailVerifyEnabled || !updated.SMTPUseTLS || updated.SMTPFromName != "AsterRouter Security" {
		t.Fatalf("authentication settings not preserved: %+v", updated)
	}
	if len(updated.AllowedEmailDomains) != 2 || updated.AllowedEmailDomains[0] != "example.com" {
		t.Fatalf("allowed domains = %v", updated.AllowedEmailDomains)
	}
	if len(updated.TrustedProxyCIDRs) != 2 || updated.TrustedProxyCIDRs[1] != "192.168.1.5/32" {
		t.Fatalf("trusted proxy CIDRs = %v", updated.TrustedProxyCIDRs)
	}
	raw, err := repo.GetAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for key, plaintext := range map[string]string{KeyTurnstileSecretKey: "secret-key", KeySMTPPassword: "smtp-secret"} {
		if raw[key] == plaintext || !strings.HasPrefix(raw[key], settingCiphertextPrefix) {
			t.Fatalf("setting %s was stored without encryption", key)
		}
	}
	policy, err := svc.RegistrationPolicy(t.Context())
	if err != nil || !policy.PasswordReset || policy.EmailVerification {
		t.Fatalf("RegistrationPolicy() = %+v, %v", policy, err)
	}

	updated.TurnstileSecretKey = ""
	updated.SMTPPassword = ""
	if _, err := svc.Update(t.Context(), updated); err != nil {
		t.Fatalf("Update() with redacted secrets: %v", err)
	}
	security, err := svc.LoginSecurity(t.Context())
	if err != nil || security.TurnstileSecret != "secret-key" {
		t.Fatalf("LoginSecurity() = %+v, %v", security, err)
	}
	smtp, err := svc.SMTPConfig(t.Context())
	if err != nil || smtp.Password != "smtp-secret" {
		t.Fatalf("SMTPConfig() = %+v, %v", smtp, err)
	}
}

func TestAuthenticationSettingsRequireSecurePublicBaseURL(t *testing.T) {
	for _, test := range []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{name: "https", baseURL: "https://router.example.test"},
		{name: "localhost", baseURL: "http://localhost:5173"},
		{name: "ipv4 loopback", baseURL: "http://127.0.0.1:5173"},
		{name: "ipv6 loopback", baseURL: "http://[::1]:5173"},
		{name: "remote http", baseURL: "http://router.example.test", wantErr: true},
		{name: "userinfo", baseURL: "https://user:password@router.example.test", wantErr: true},
		{name: "path", baseURL: "https://router.example.test/asterrouter", wantErr: true},
		{name: "query", baseURL: "https://router.example.test?tenant=one", wantErr: true},
		{name: "fragment", baseURL: "https://router.example.test#auth", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
			current, err := svc.Admin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			current.PublicBaseURL = test.baseURL
			current.PasswordResetEnabled = true
			current.SMTPHost = "smtp.example.test"
			current.SMTPFrom = "noreply@example.test"
			_, err = svc.Update(t.Context(), current)
			if (err != nil) != test.wantErr {
				t.Fatalf("Update() error = %v, wantErr=%v", err, test.wantErr)
			}
		})
	}
}

func TestAuthenticationSettingsRejectIncompleteIdentityProviders(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*AdminSettings)
	}{
		{name: "OIDC client id", edit: func(in *AdminSettings) {
			in.OIDCEnabled = true
			in.OIDCIssuerURL = "https://id.example.test"
		}},
		{name: "OIDC issuer", edit: func(in *AdminSettings) {
			in.OIDCEnabled = true
			in.OIDCClientID = "client"
		}},
		{name: "OIDC secure issuer", edit: func(in *AdminSettings) {
			in.OIDCEnabled = true
			in.OIDCClientID = "client"
			in.OIDCIssuerURL = "http://id.example.test"
		}},
		{name: "Feishu secret", edit: func(in *AdminSettings) {
			in.FeishuEnabled = true
			in.FeishuAppID = "app"
		}},
		{name: "DingTalk secret", edit: func(in *AdminSettings) {
			in.DingTalkEnabled = true
			in.DingTalkClientID = "app"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
			current, err := svc.Admin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			current.PublicBaseURL = "https://router.example.test"
			test.edit(&current)
			if _, err := svc.Update(t.Context(), current); err == nil {
				t.Fatal("incomplete identity provider configuration was accepted")
			}
		})
	}
}

func TestAuthenticationSettingsPreserveConfiguredProviderSecrets(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory", SecretKey: "settings-test-secret"})
	current, err := svc.Admin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	current.PublicBaseURL = "https://router.example.test"
	current.OIDCEnabled = true
	current.OIDCIssuerURL = "https://id.example.test"
	current.OIDCClientID = "oidc-client"
	current.OIDCClientSecret = "oidc-secret"
	current.FeishuEnabled = true
	current.FeishuAppID = "feishu-app"
	current.FeishuAppSecret = "feishu-secret"
	current.DingTalkEnabled = true
	current.DingTalkClientID = "dingtalk-app"
	current.DingTalkClientSecret = "dingtalk-secret"
	configured, err := svc.Update(t.Context(), current)
	if err != nil {
		t.Fatal(err)
	}
	configured.FeishuAppSecret = ""
	configured.DingTalkClientSecret = ""
	configured.OIDCClientSecret = ""
	if _, err := svc.Update(t.Context(), configured); err != nil {
		t.Fatalf("redacted configured secrets were rejected: %v", err)
	}
	feishuSecret, err := svc.FeishuSecret(t.Context())
	if err != nil || feishuSecret != "feishu-secret" {
		t.Fatalf("FeishuSecret() = %q, %v", feishuSecret, err)
	}
	dingTalkSecret, err := svc.DingTalkSecret(t.Context())
	if err != nil || dingTalkSecret != "dingtalk-secret" {
		t.Fatalf("DingTalkSecret() = %q, %v", dingTalkSecret, err)
	}
	oidcSecret, err := svc.OIDCSecret(t.Context())
	if err != nil || oidcSecret != "oidc-secret" {
		t.Fatalf("OIDCSecret() = %q, %v", oidcSecret, err)
	}
	admin, err := svc.Admin(t.Context())
	if err != nil || !admin.OIDCClientSecretConfigured || admin.OIDCClientSecret != "" {
		t.Fatalf("Admin() exposed or lost OIDC secret state: configured=%v secret=%q err=%v", admin.OIDCClientSecretConfigured, admin.OIDCClientSecret, err)
	}
}

func TestSensitiveSettingsUpgradeLegacyPlaintextOnUpdate(t *testing.T) {
	repo := NewMemoryRepository()
	legacy := legacySensitiveSettingValues()
	if err := repo.SetMultiple(t.Context(), legacy); err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory", SecretKey: "settings-test-secret"})

	if value, err := svc.FeishuSecret(t.Context()); err != nil || value != legacy[KeyFeishuAppSecret] {
		t.Fatalf("FeishuSecret() = %q, %v", value, err)
	}
	if value, err := svc.OIDCSecret(t.Context()); err != nil || value != legacy[KeyOIDCClientSecret] {
		t.Fatalf("OIDCSecret() = %q, %v", value, err)
	}
	github, google, err := svc.SocialOAuthSecrets(t.Context())
	if err != nil || github != legacy[KeyGitHubOAuthSecret] || google != legacy[KeyGoogleOAuthSecret] {
		t.Fatalf("SocialOAuthSecrets() = %q, %q, %v", github, google, err)
	}
	if value, err := svc.DingTalkSecret(t.Context()); err != nil || value != legacy[KeyDingTalkClientSecret] {
		t.Fatalf("DingTalkSecret() = %q, %v", value, err)
	}
	if security, err := svc.LoginSecurity(t.Context()); err != nil || security.TurnstileSecret != legacy[KeyTurnstileSecretKey] {
		t.Fatalf("LoginSecurity() = %+v, %v", security, err)
	}
	if smtp, err := svc.SMTPConfig(t.Context()); err != nil || smtp.Password != legacy[KeySMTPPassword] {
		t.Fatalf("SMTPConfig() = %+v, %v", smtp, err)
	}
	if backup, err := svc.BackupS3Config(t.Context()); err != nil || backup.SecretKey != legacy[KeyBackupS3SecretKey] {
		t.Fatalf("BackupS3Config() = %+v, %v", backup, err)
	}

	current, err := svc.Admin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(t.Context(), current); err != nil {
		t.Fatalf("Update(): %v", err)
	}
	raw, err := repo.GetAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for key, plaintext := range legacy {
		if raw[key] == plaintext || !strings.HasPrefix(raw[key], settingCiphertextPrefix) {
			t.Fatalf("legacy setting %s was not upgraded to ciphertext", key)
		}
	}
}

func TestMigrateLegacySensitiveSettingsEncryptsAllValuesAndIsIdempotent(t *testing.T) {
	repo := NewMemoryRepository()
	legacy := legacySensitiveSettingValues()
	if err := repo.SetMultiple(t.Context(), legacy); err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory", SecretKey: "settings-test-secret"})
	if err := svc.MigrateLegacySensitiveSettings(t.Context()); err != nil {
		t.Fatalf("MigrateLegacySensitiveSettings(): %v", err)
	}
	first, err := repo.GetAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for key, plaintext := range legacy {
		if first[key] == plaintext || !strings.HasPrefix(first[key], settingCiphertextPrefix) {
			t.Fatalf("legacy setting %s was not encrypted", key)
		}
		decrypted, err := decryptSettingValue("settings-test-secret", key, first[key])
		if err != nil || decrypted != plaintext {
			t.Fatalf("decrypt migrated setting %s = %q, %v", key, decrypted, err)
		}
	}
	if err := svc.MigrateLegacySensitiveSettings(t.Context()); err != nil {
		t.Fatalf("second MigrateLegacySensitiveSettings(): %v", err)
	}
	second, err := repo.GetAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for key := range legacy {
		if second[key] != first[key] {
			t.Fatalf("idempotent migration rewrote setting %s", key)
		}
	}
}

func TestMigrateLegacySensitiveSettingsFailsBeforeWritingWithWrongKey(t *testing.T) {
	repo := NewMemoryRepository()
	ciphertext, err := encryptSettingValue("original-secret", KeyTurnstileSecretKey, "turnstile-secret")
	if err != nil {
		t.Fatal(err)
	}
	before := map[string]string{
		KeyTurnstileSecretKey: ciphertext,
		KeySMTPPassword:       "legacy-smtp-secret",
	}
	if err := repo.SetMultiple(t.Context(), before); err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory", SecretKey: "wrong-secret"})
	if err := svc.MigrateLegacySensitiveSettings(t.Context()); err == nil {
		t.Fatal("migration accepted ciphertext encrypted with another key")
	}
	after, err := repo.GetAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range before {
		if after[key] != value {
			t.Fatalf("failed migration changed setting %s", key)
		}
	}
}

type conflictOnceSettingsRepository struct {
	*MemoryRepository
	attempts atomic.Int32
}

func (r *conflictOnceSettingsRepository) ReplaceIfUnchanged(ctx context.Context, replacements map[string]ValueReplacement) error {
	if r.attempts.Add(1) == 1 {
		return ErrSettingsChanged
	}
	return r.MemoryRepository.ReplaceIfUnchanged(ctx, replacements)
}

func TestMigrateLegacySensitiveSettingsRetriesConcurrentChange(t *testing.T) {
	repo := &conflictOnceSettingsRepository{MemoryRepository: NewMemoryRepository()}
	if err := repo.SetMultiple(t.Context(), map[string]string{KeySMTPPassword: "legacy-smtp-secret"}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory", SecretKey: "settings-test-secret"})
	if err := svc.MigrateLegacySensitiveSettings(t.Context()); err != nil {
		t.Fatalf("MigrateLegacySensitiveSettings(): %v", err)
	}
	if repo.attempts.Load() != 2 {
		t.Fatalf("migration attempts = %d, want 2", repo.attempts.Load())
	}
}

func TestMemoryReplaceIfUnchangedIsAtomic(t *testing.T) {
	repo := NewMemoryRepository()
	if err := repo.SetMultiple(t.Context(), map[string]string{"first": "old-first", "second": "old-second"}); err != nil {
		t.Fatal(err)
	}
	err := repo.ReplaceIfUnchanged(t.Context(), map[string]ValueReplacement{
		"first":  {Expected: "old-first", Value: "new-first"},
		"second": {Expected: "stale-second", Value: "new-second"},
	})
	if !errors.Is(err, ErrSettingsChanged) {
		t.Fatalf("ReplaceIfUnchanged() error = %v", err)
	}
	values, err := repo.GetAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if values["first"] != "old-first" || values["second"] != "old-second" {
		t.Fatalf("failed replacement partially changed settings: %#v", values)
	}
	if err := repo.ReplaceIfUnchanged(t.Context(), map[string]ValueReplacement{
		"missing": {Expected: "", Value: "new-value"},
	}); !errors.Is(err, ErrSettingsChanged) {
		t.Fatalf("ReplaceIfUnchanged(missing) error = %v", err)
	}
}

func TestMemorySetIfAbsentDoesNotOverwrite(t *testing.T) {
	repo := NewMemoryRepository()
	inserted, err := repo.SetIfAbsent(t.Context(), "key", "first")
	if err != nil || !inserted {
		t.Fatalf("SetIfAbsent(first) = %v, %v", inserted, err)
	}
	inserted, err = repo.SetIfAbsent(t.Context(), "key", "second")
	if err != nil || inserted {
		t.Fatalf("SetIfAbsent(second) = %v, %v", inserted, err)
	}
	values, err := repo.GetAll(t.Context())
	if err != nil || values["key"] != "first" {
		t.Fatalf("GetAll() = %#v, %v", values, err)
	}
}

func legacySensitiveSettingValues() map[string]string {
	return map[string]string{
		KeyOIDCClientSecret:     "oidc-secret",
		KeyFeishuAppSecret:      "feishu-secret",
		KeyGitHubOAuthSecret:    "github-secret",
		KeyGoogleOAuthSecret:    "google-secret",
		KeyDingTalkClientSecret: "dingtalk-secret",
		KeyTurnstileSecretKey:   "turnstile-secret",
		KeySMTPPassword:         "smtp-secret",
		KeyBackupS3SecretKey:    "backup-secret",
	}
}

func TestSensitiveSettingsFailClosedWithWrongKey(t *testing.T) {
	repo := NewMemoryRepository()
	ciphertext, err := encryptSettingValue("original-secret", KeyTurnstileSecretKey, "turnstile-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMultiple(t.Context(), map[string]string{KeyTurnstileSecretKey: ciphertext}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory", SecretKey: "wrong-secret"})
	if _, err := svc.LoginSecurity(t.Context()); err == nil {
		t.Fatal("LoginSecurity() accepted ciphertext encrypted with another key")
	}
}

func TestSensitiveSettingsCiphertextIsBoundToSettingKey(t *testing.T) {
	ciphertext, err := encryptSettingValue("settings-test-secret", KeyTurnstileSecretKey, "turnstile-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decryptSettingValue("settings-test-secret", KeySMTPPassword, ciphertext); err == nil {
		t.Fatal("ciphertext encrypted for one setting was accepted for another setting")
	}
}

type failSecondSettingsReadRepository struct {
	*MemoryRepository
	reads atomic.Int32
}

func (r *failSecondSettingsReadRepository) GetAll(ctx context.Context) (map[string]string, error) {
	if r.reads.Add(1) == 2 {
		return nil, errors.New("synthetic settings read failure")
	}
	return r.MemoryRepository.GetAll(ctx)
}

func TestSettingsUpdateFailsClosedWhenExistingSecretsCannotBeRead(t *testing.T) {
	repo := &failSecondSettingsReadRepository{MemoryRepository: NewMemoryRepository()}
	svc := NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory", SecretKey: "settings-test-secret"})
	current, err := svc.Admin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	repo.reads.Store(0)
	if _, err := svc.Update(t.Context(), current); err == nil || err.Error() != "synthetic settings read failure" {
		t.Fatalf("Update() error = %v", err)
	}
}

func TestMemoryInvitationCodeConsumptionIsAtomic(t *testing.T) {
	testInvitationCodeConsumptionIsAtomic(t, NewMemoryRepository())
}

func testInvitationCodeConsumptionIsAtomic(t *testing.T, repo Repository) {
	t.Helper()
	if err := repo.SetMultiple(t.Context(), map[string]string{KeyInvitationCodes: `["single-use"]`}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- repo.ConsumeInvitationCode(t.Context(), "single-use")
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, ErrInvitationCodeInvalid) {
			t.Fatalf("unexpected consume error: %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("successful invitation consumptions = %d, want 1", succeeded)
	}
	if err := repo.RestoreInvitationCode(t.Context(), "single-use"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConsumeInvitationCode(t.Context(), "single-use"); err != nil {
		t.Fatalf("restored invitation code could not be consumed: %v", err)
	}
}

func TestApplyProfilesAllowsSwitchingOneActiveDeploymentProfile(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
	got, err := svc.ApplyProfiles(context.Background(), []string{"enterprise"}, "enterprise")
	if err != nil {
		t.Fatalf("ApplyProfiles() error = %v", err)
	}
	if !got.SetupCompleted || got.DefaultProfile != "enterprise" || len(got.EnabledProfiles) != 1 || got.EnabledProfiles[0] != "enterprise" {
		t.Fatalf("profiles not applied: %+v", got)
	}
	if _, err := svc.ApplyProfiles(context.Background(), []string{"enterprise"}, "enterprise"); err != nil {
		t.Fatalf("ApplyProfiles() same-profile retry error = %v", err)
	}
	if _, err := svc.ApplyProfiles(context.Background(), []string{"enterprise", "personal"}, "enterprise"); err == nil {
		t.Fatal("ApplyProfiles() accepted more than one deployment profile")
	}
	got, err = svc.ApplyProfiles(context.Background(), []string{"platform"}, "platform")
	if err != nil {
		t.Fatalf("ApplyProfiles() switch error = %v", err)
	}
	if got.DefaultProfile != "platform" || len(got.EnabledProfiles) != 1 || got.EnabledProfiles[0] != "platform" {
		t.Fatalf("profiles not switched: %+v", got.PublicSettings)
	}
}

func TestApplyInitialProfileRequiresOneFreshProfile(t *testing.T) {
	for _, profile := range []string{"personal", "relay_operator", "enterprise", "platform"} {
		t.Run(profile, func(t *testing.T) {
			svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
			got, err := svc.ApplyInitialProfile(context.Background(), profile)
			if err != nil {
				t.Fatalf("ApplyInitialProfile() error = %v", err)
			}
			if !got.SetupCompleted || got.DefaultProfile != profile || len(got.EnabledProfiles) != 1 || got.EnabledProfiles[0] != profile {
				t.Fatalf("initial deployment profile not applied: %+v", got)
			}
			if _, err := svc.ApplyInitialProfile(context.Background(), "platform"); err == nil {
				t.Fatal("ApplyInitialProfile() after setup = nil, want error")
			}
		})
	}
	if _, err := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"}).ApplyInitialProfile(context.Background(), "unknown"); err == nil {
		t.Fatal("ApplyInitialProfile() accepted an unknown profile")
	}
}

type coordinatedInitialProfileRepository struct {
	*MemoryRepository
	reads        atomic.Int32
	initialReads chan struct{}
	applyReads   chan struct{}
}

func newCoordinatedInitialProfileRepository() *coordinatedInitialProfileRepository {
	return &coordinatedInitialProfileRepository{
		MemoryRepository: NewMemoryRepository(),
		initialReads:     make(chan struct{}),
		applyReads:       make(chan struct{}),
	}
}

func (r *coordinatedInitialProfileRepository) GetAll(ctx context.Context) (map[string]string, error) {
	values, err := r.MemoryRepository.GetAll(ctx)
	read := r.reads.Add(1)
	var barrier chan struct{}
	switch read {
	case 1, 2:
		barrier = r.initialReads
	case 3, 4:
		barrier = r.applyReads
	}
	if barrier != nil {
		if read == 2 || read == 4 {
			close(barrier)
		}
		<-barrier
	}
	return values, err
}

func (r *coordinatedInitialProfileRepository) InitializeDeploymentProfile(ctx context.Context, profile string) error {
	// The atomic path bypasses both coordinated read phases used to reproduce the legacy race.
	r.reads.Store(4)
	return r.MemoryRepository.InitializeDeploymentProfile(ctx, profile)
}

func TestApplyInitialProfileSerializesConflictingConcurrentInstalls(t *testing.T) {
	repo := newCoordinatedInitialProfileRepository()
	services := []*Service{
		NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory"}),
		NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory"}),
	}
	profiles := []string{"enterprise", "platform"}
	start := make(chan struct{})
	results := make(chan error, len(services))
	for index, service := range services {
		go func(svc *Service, profile string) {
			<-start
			_, err := svc.ApplyInitialProfile(context.Background(), profile)
			results <- err
		}(service, profiles[index])
	}
	close(start)

	succeeded := 0
	conflicted := 0
	for range services {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrDeploymentProfileInitialized):
			conflicted++
		default:
			t.Fatalf("ApplyInitialProfile() unexpected error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent install results: succeeded=%d conflicted=%d", succeeded, conflicted)
	}

	current, err := services[0].Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin(): %v", err)
	}
	if !current.SetupCompleted || len(current.EnabledProfiles) != 1 || current.DefaultProfile != current.EnabledProfiles[0] {
		t.Fatalf("persisted deployment profile is inconsistent: %+v", current.PublicSettings)
	}
}

func TestApplyProfilesSerializesConflictingConcurrentInstalls(t *testing.T) {
	repo := newCoordinatedInitialProfileRepository()
	services := []*Service{
		NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory"}),
		NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory"}),
	}
	profiles := []string{"enterprise", "platform"}
	start := make(chan struct{})
	results := make(chan error, len(services))
	for index, service := range services {
		go func(svc *Service, profile string) {
			<-start
			_, err := svc.ApplyProfiles(context.Background(), []string{profile}, profile)
			results <- err
		}(service, profiles[index])
	}
	close(start)

	succeeded := 0
	conflicted := 0
	for range services {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrDeploymentProfileInitialized):
			conflicted++
		default:
			t.Fatalf("ApplyProfiles() unexpected error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent profile results: succeeded=%d conflicted=%d", succeeded, conflicted)
	}

	current, err := services[0].Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin(): %v", err)
	}
	if !current.SetupCompleted || len(current.EnabledProfiles) != 1 || current.DefaultProfile != current.EnabledProfiles[0] {
		t.Fatalf("persisted deployment profile is inconsistent: %+v", current.PublicSettings)
	}
}

func TestBootstrapProfilePersistsSingleConfiguredProfile(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, ServiceOptions{
		Version: "test", StorageMode: "memory", EnabledProfiles: []string{"platform"}, DefaultProfile: "platform",
	})
	if err := svc.BootstrapProfile(context.Background()); err != nil {
		t.Fatalf("BootstrapProfile() error = %v", err)
	}
	raw, err := repo.GetAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if raw[KeyEnabledProfiles] != `["platform"]` || raw[KeyDefaultProfile] != "platform" || raw[KeySetupCompleted] != "true" {
		t.Fatalf("bootstrap settings = %v", raw)
	}
	if err := svc.BootstrapProfile(context.Background()); err != nil {
		t.Fatalf("repeat BootstrapProfile() error = %v", err)
	}
}

func TestBootstrapProfileDoesNotOverrideRuntimeProfileSwitch(t *testing.T) {
	repo := NewMemoryRepository()
	installed := NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory"})
	if _, err := installed.ApplyInitialProfile(context.Background(), "enterprise"); err != nil {
		t.Fatalf("ApplyInitialProfile() error = %v", err)
	}
	if _, err := installed.ApplyProfiles(context.Background(), []string{"platform"}, "platform"); err != nil {
		t.Fatalf("ApplyProfiles() error = %v", err)
	}

	configuredAsEnterprise := NewService(repo, ServiceOptions{
		Version: "test", StorageMode: "memory", EnabledProfiles: []string{"enterprise"}, DefaultProfile: "enterprise",
	})
	if err := configuredAsEnterprise.BootstrapProfile(context.Background()); err != nil {
		t.Fatalf("BootstrapProfile() after setup error = %v", err)
	}

	stored, err := installed.Admin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored.DefaultProfile != "platform" || len(stored.EnabledProfiles) != 1 || stored.EnabledProfiles[0] != "platform" {
		t.Fatalf("persisted deployment profile changed: %+v", stored.PublicSettings)
	}
}

func TestBootstrapProfileRejectsUnsupportedConfiguredRole(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, ServiceOptions{
		Version: "test", StorageMode: "memory", EnabledProfiles: []string{"unknown"}, DefaultProfile: "unknown",
	})
	if err := svc.BootstrapProfile(context.Background()); err == nil {
		t.Fatal("BootstrapProfile() accepted an unsupported configured role")
	}
	values, err := repo.GetAll(context.Background())
	if err != nil {
		t.Fatalf("GetAll(): %v", err)
	}
	if len(values) != 0 {
		t.Fatalf("unsupported bootstrap role mutated settings: %#v", values)
	}
}

func TestMemoryRepositoryRejectsUnsupportedDeploymentProfile(t *testing.T) {
	repo := NewMemoryRepository()
	if err := repo.InitializeDeploymentProfile(context.Background(), "unknown"); err == nil {
		t.Fatal("InitializeDeploymentProfile() accepted an unsupported role")
	}
	values, err := repo.GetAll(context.Background())
	if err != nil {
		t.Fatalf("GetAll(): %v", err)
	}
	if len(values) != 0 {
		t.Fatalf("unsupported role mutated settings: %#v", values)
	}
}

func TestDemoModeCompletesSetupWithAllProfiles(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory", DemoMode: true})
	got, err := svc.Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin() error = %v", err)
	}
	if !got.SetupCompleted || !got.DemoMode || got.DefaultProfile != "personal" {
		t.Fatalf("demo settings not applied: %+v", got.PublicSettings)
	}
	if len(got.EnabledProfiles) != 4 {
		t.Fatalf("EnabledProfiles = %+v", got.EnabledProfiles)
	}
}

func TestDemoModeDoesNotOverrideConfiguredProfiles(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{
		Version:         "test",
		StorageMode:     "memory",
		DemoMode:        true,
		EnabledProfiles: []string{"enterprise"},
		DefaultProfile:  "enterprise",
	})
	got, err := svc.Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin() error = %v", err)
	}
	if got.DefaultProfile != "enterprise" || len(got.EnabledProfiles) != 1 || got.EnabledProfiles[0] != "enterprise" {
		t.Fatalf("configured profiles overridden: %+v", got.PublicSettings)
	}
}

func TestUpdateCannotBypassDeploymentProfileInvariant(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
	current, err := svc.ApplyInitialProfile(context.Background(), "platform")
	if err != nil {
		t.Fatal(err)
	}
	current.EnabledProfiles = []string{"platform", "enterprise"}
	if _, err := svc.Update(context.Background(), current); err == nil {
		t.Fatal("Update() accepted multiple deployment profiles")
	}
	current.EnabledProfiles = []string{"enterprise"}
	current.DefaultProfile = "enterprise"
	if _, err := svc.Update(context.Background(), current); err == nil {
		t.Fatal("Update() changed the installed deployment profile")
	}
	stored, err := svc.Admin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored.DefaultProfile != "platform" || len(stored.EnabledProfiles) != 1 || stored.EnabledProfiles[0] != "platform" {
		t.Fatalf("stored deployment profile = %+v", stored.PublicSettings)
	}
}

func TestUpdateValidatesLocale(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
	_, err := svc.Update(context.Background(), AdminSettings{
		PublicSettings: PublicSettings{
			SiteName:          "AsterRouter",
			DefaultLocale:     "ja-JP",
			EnabledLocales:    []string{"en-US"},
			GatewayBasePath:   "/v1",
			ServiceCenterMode: "disabled",
		},
		DataRetentionDays: 30,
		PromptLoggingMode: "metadata_only",
		UpdateChannel:     "stable",
	})
	if err == nil {
		t.Fatal("Update() error = nil, want validation error")
	}
}

func TestValidateLegalDocumentsRejectsDuplicateSlug(t *testing.T) {
	err := validateLegalDocuments([]LegalDocument{
		{ID: "terms", Name: "Terms", Slug: "terms", Content: "one"},
		{ID: "privacy", Name: "Privacy", Slug: "terms", Content: "two"},
	}, true)
	if err == nil {
		t.Fatal("validateLegalDocuments() error = nil, want duplicate slug error")
	}
}

func TestParseIntListFallsBackOnInvalidJSON(t *testing.T) {
	fallback := []int{10, 20, 50}
	got := parseIntList("invalid", fallback)
	if len(got) != len(fallback) || got[1] != 20 {
		t.Fatalf("parseIntList() = %v, want %v", got, fallback)
	}
}
