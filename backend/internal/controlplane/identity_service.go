package controlplane

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

type TOTPSetup struct {
	Secret          string    `json:"secret"`
	ProvisioningURI string    `json:"provisioning_uri"`
	ExpiresAt       time.Time `json:"expires_at"`
}

type AccountAuthenticationState struct {
	UserID      string
	Role        string
	TOTPEnabled bool
}

const totpSetupTTL = 10 * time.Minute

type totpSecretEnvelope struct {
	Secret         string     `json:"secret"`
	SetupExpiresAt *time.Time `json:"setup_expires_at,omitempty"`
}

const (
	maxAvatarDataURLBytes = 256 * 1024
	// MinPasswordLength 是所有本地密码入口（注册、重置、改密）的统一下限。
	MinPasswordLength = 10
	// MaxPasswordBytes 来自 bcrypt 的输入上限。显式校验可避免不同入口返回不同错误。
	MaxPasswordBytes                = 72
	emailVerificationTokenLifetime  = 30 * time.Minute
	passwordResetTokenLifetime      = 30 * time.Minute
	emailVerificationResendCooldown = time.Minute
	passwordResetRequestCooldown    = time.Minute
	dummyWorkspaceUserPasswordHash  = "$2a$10$wmA3Ghc6aFB2Rxw5TFl.RuY69eTsen./ma./DUx2BUj.yf/unXtKO"
)

var ErrConfigManagedAccount = errors.New("account is managed by deployment configuration")

var ErrUserEmailExists = errors.New("user email already exists")

var ErrInvalidWorkspaceEmail = errors.New("valid email is required")

// ErrPasswordTooWeak 表示密码不满足强度要求。该原因可以安全地回给调用方——
// 它描述的是用户自己提交的输入，不泄漏任何账号是否存在的信息。
var ErrPasswordTooWeak = fmt.Errorf("password must contain at least %d characters", MinPasswordLength)

var ErrPasswordTooLong = fmt.Errorf("password must not exceed %d bytes", MaxPasswordBytes)

// ErrResetTokenInvalid 表示密码重置凭据不可用（不存在、已过期或已使用）。
// 三种情况共用一个错误，避免调用方据此探测邮箱是否已注册。
var ErrResetTokenInvalid = errors.New("password reset token is invalid or expired")

// ErrVerificationTokenInvalid 表示邮箱验证凭据不可用，同样合并了不存在/过期/已使用。
var ErrVerificationTokenInvalid = errors.New("email verification token is invalid or expired")

// ErrEmailVerificationUnavailable intentionally merges unknown, already
// verified, and cooldown cases so public resend endpoints cannot enumerate users.
var ErrEmailVerificationUnavailable = errors.New("email verification is not available")

// ErrPasswordResetUnavailable intentionally merges unknown, inactive, and
// cooldown cases so public reset-request endpoints can return a uniform result.
var ErrPasswordResetUnavailable = errors.New("password reset is not available")

// ErrInvalidWorkspaceCredentials intentionally merges unknown users, disabled
// users, unverified email addresses, and invalid passwords for local login.
var ErrInvalidWorkspaceCredentials = errors.New("invalid email or password")

var (
	ErrAccountDisplayNameRequired = errors.New("display name is required")
	ErrAccountDisplayNameTooLong  = errors.New("display name must contain at most 80 characters")
	ErrAccountAvatarTooLarge      = errors.New("avatar must not exceed 256 KiB")
	ErrAccountAvatarFormatInvalid = errors.New("avatar must be a PNG, JPEG, WebP, or GIF data URL")
	ErrAccountAvatarBase64Invalid = errors.New("avatar contains invalid base64 data")
	ErrAccountAvatarTypeInvalid   = errors.New("avatar content does not match a supported image type")

	ErrPasswordRecoveryRequired = errors.New("use password recovery to set a local password")
	ErrCurrentPasswordIncorrect = errors.New("current password is incorrect")
	ErrPasswordUnchanged        = errors.New("new password must be different from the current password")

	ErrWorkspaceUserDisabled    = errors.New("workspace user is disabled")
	ErrTOTPAlreadyEnabled       = errors.New("TOTP is already enabled")
	ErrTOTPEnrollmentNotStarted = errors.New("TOTP enrollment has not been started")
	ErrTOTPEnrollmentExpired    = errors.New("TOTP enrollment has expired; start again")
	ErrTOTPInvalidCode          = errors.New("invalid TOTP code")
	ErrTOTPNotEnabled           = errors.New("TOTP is not enabled")

	ErrAuthIdentityNotBound = errors.New("authentication identity is not bound")
	ErrLastLoginMethod      = errors.New("cannot remove the last available login method")
)

func validatePasswordStrength(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return ErrPasswordTooWeak
	}
	if len([]byte(password)) > MaxPasswordBytes {
		return ErrPasswordTooLong
	}
	return nil
}

// ValidatePasswordStrength 供 HTTP 边界在消费邀请码等有副作用的操作前复用同一密码策略。
func ValidatePasswordStrength(password string) error {
	return validatePasswordStrength(password)
}

func normalizeWorkspaceUserEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	if email == "" || len(email) > 254 || strings.Count(email, "@") != 1 {
		return "", ErrInvalidWorkspaceEmail
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return "", ErrInvalidWorkspaceEmail
	}
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" || len(local) > 64 {
		return "", ErrInvalidWorkspaceEmail
	}
	return email, nil
}

func (s *Service) EnsureLocalAdmin(ctx context.Context, username, password string, defaults ...WorkspaceUserDefaults) (WorkspaceUser, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return WorkspaceUser{}, err
	}
	for _, user := range users {
		if user.ID != username {
			continue
		}
		return s.ensureLocalAdminState(ctx, user, username, password)
	}

	email := strings.ToLower(username)
	if !strings.Contains(email, "@") {
		email += "@local.invalid"
	}
	for _, user := range users {
		if strings.EqualFold(user.Email, email) {
			return WorkspaceUser{}, fmt.Errorf("local administrator email %s already belongs to another user", email)
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return WorkspaceUser{}, err
	}
	now := time.Now().UTC()
	user := WorkspaceUser{
		ID: username, Email: email, DisplayName: username,
		Status: WorkspaceUserStatusActive, Role: RoleSuperAdmin,
		PasswordHash: string(hash), EmailVerified: true,
		CreatedAt: now, UpdatedAt: now,
	}
	applyWorkspaceUserDefaults(&user, defaults)
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		// Multiple HA instances can observe an empty table and bootstrap the
		// same account concurrently. The losing insert may report the email
		// unique constraint before the id conflict is visible to PostgreSQL.
		// Accept only the matching account created by the competing instance;
		// a different account must remain a hard conflict.
		if errors.Is(err, ErrUserEmailExists) {
			current, found, findErr := s.repo.FindWorkspaceUserByID(ctx, username)
			if findErr == nil && found && strings.EqualFold(current.Email, email) {
				return s.ensureLocalAdminState(ctx, current, username, password)
			}
		}
		return WorkspaceUser{}, err
	}
	if err := s.audit(ctx, systemActor, "bootstrap", "workspace_user", user.ID, "Provisioned local administrator account"); err != nil {
		return WorkspaceUser{}, err
	}
	return user, nil
}

func (s *Service) ensureLocalAdminState(ctx context.Context, user WorkspaceUser, username, password string) (WorkspaceUser, error) {
	changed := false
	if user.Status != WorkspaceUserStatusActive {
		user.Status = WorkspaceUserStatusActive
		changed = true
	}
	if user.Role != RoleSuperAdmin {
		user.Role = RoleSuperAdmin
		changed = true
	}
	if user.DisplayName == "" {
		user.DisplayName = username
		changed = true
	}
	if user.PasswordHash == "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return WorkspaceUser{}, err
		}
		user.PasswordHash = string(hash)
		changed = true
	}
	if changed {
		user.UpdatedAt = time.Now().UTC()
		if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
			return WorkspaceUser{}, err
		}
	}
	return user, nil
}

func (s *Service) CurrentAccountProfile(ctx context.Context, actor string) (AccountProfile, error) {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		return AccountProfile{}, err
	}
	profile := accountProfileFromUser(user)
	profile.AuthIdentities, err = s.repo.ListAuthIdentities(ctx, user.ID)
	return profile, err
}

func (s *Service) CurrentAccountAuthenticationState(ctx context.Context, actor string) (AccountAuthenticationState, error) {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		return AccountAuthenticationState{}, err
	}
	return AccountAuthenticationState{UserID: user.ID, Role: user.Role, TOTPEnabled: user.TOTPEnabled}, nil
}

func (s *Service) UpdateCurrentAccountProfile(ctx context.Context, actor string, req AccountProfileUpdateRequest) (AccountProfile, error) {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		return AccountProfile{}, err
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		return AccountProfile{}, ErrAccountDisplayNameRequired
	}
	if len([]rune(displayName)) > 80 {
		return AccountProfile{}, ErrAccountDisplayNameTooLong
	}
	if err := validateAvatarDataURL(req.AvatarDataURL); err != nil {
		return AccountProfile{}, err
	}
	user.DisplayName = displayName
	user.AvatarDataURL = strings.TrimSpace(req.AvatarDataURL)
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return AccountProfile{}, err
	}
	if err := s.audit(ctx, actor, "account_profile_updated", "workspace_user", user.ID, "Updated organization account profile"); err != nil {
		return AccountProfile{}, err
	}
	return accountProfileFromUser(user), nil
}

func (s *Service) ChangeCurrentAccountPassword(ctx context.Context, actor string, req AccountPasswordUpdateRequest) error {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		return err
	}
	if err := validatePasswordStrength(req.NewPassword); err != nil {
		return err
	}
	if user.PasswordHash == "" {
		return ErrPasswordRecoveryRequired
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)) != nil {
		return ErrCurrentPasswordIncorrect
	}
	if req.CurrentPassword == req.NewPassword {
		return ErrPasswordUnchanged
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(passwordHash)
	user.PasswordResetHash = ""
	user.PasswordResetExpiresAt = nil
	user.SessionVersion++
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return err
	}
	action, summary := "account_password_changed", "Changed organization account password"
	if err := s.audit(ctx, actor, action, "workspace_user", user.ID, summary); err != nil {
		return err
	}
	return nil
}

func (s *Service) CurrentAccountPasswordHash(ctx context.Context, actor string) (string, error) {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		return "", err
	}
	if user.PasswordHash == "" {
		return "", errors.New("password login is not enabled for this account")
	}
	return user.PasswordHash, nil
}

func accountProfileFromUser(user WorkspaceUser) AccountProfile {
	return AccountProfile{
		ID: user.ID, Email: user.Email, DisplayName: user.DisplayName, AvatarDataURL: user.AvatarDataURL,
		Status: user.Status, Role: user.Role,
		ConcurrencyLimit: user.ConcurrencyLimit, RPMLimit: user.RPMLimit,
		ExternalIssuer: user.ExternalIssuer, EmailVerified: user.EmailVerified,
		PasswordEnabled: user.PasswordHash != "", TOTPEnabled: user.TOTPEnabled,
		CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}
}

func validateAvatarDataURL(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if len(value) > maxAvatarDataURLBytes {
		return ErrAccountAvatarTooLarge
	}
	header, payload, ok := strings.Cut(value, ",")
	if !ok || !strings.HasSuffix(header, ";base64") || !oneOf(strings.TrimSuffix(header, ";base64"), "data:image/png", "data:image/jpeg", "data:image/webp", "data:image/gif") {
		return ErrAccountAvatarFormatInvalid
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ErrAccountAvatarBase64Invalid
	}
	detected := http.DetectContentType(decoded)
	if !oneOf(detected, "image/png", "image/jpeg", "image/webp", "image/gif") {
		return ErrAccountAvatarTypeInvalid
	}
	return nil
}

func (s *Service) RegisterWorkspaceUser(ctx context.Context, email, password, displayName string, requireVerification bool, defaults ...WorkspaceUserDefaults) (WorkspaceUser, string, error) {
	var err error
	email, err = normalizeWorkspaceUserEmail(email)
	if err != nil {
		return WorkspaceUser{}, "", err
	}
	if err := validatePasswordStrength(password); err != nil {
		return WorkspaceUser{}, "", err
	}
	if err := s.ensureUniqueUserEmail(ctx, "", email); err != nil {
		return WorkspaceUser{}, "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return WorkspaceUser{}, "", err
	}
	now := s.nowUTC()
	user := WorkspaceUser{ID: "usr_" + randomID(10), Email: email, DisplayName: strings.TrimSpace(displayName), Status: WorkspaceUserStatusActive, Role: RoleDeveloper, PasswordHash: string(hash), EmailVerified: !requireVerification, CreatedAt: now, UpdatedAt: now}
	applyWorkspaceUserDefaults(&user, defaults)
	verificationToken := ""
	if requireVerification {
		verificationToken, err = auth.RandomToken(32)
		if err != nil {
			return WorkspaceUser{}, "", err
		}
		user.EmailVerifyHash = recoveryCodeHash(verificationToken)
		expires := now.Add(emailVerificationTokenLifetime)
		user.EmailVerifyExpiresAt = &expires
		user.EmailVerifySentAt = &now
	}
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return WorkspaceUser{}, "", err
	}
	_ = s.audit(ctx, email, "register", "workspace_user", user.ID, "Registered workspace user")
	return user, verificationToken, nil
}

func (s *Service) VerifyWorkspaceUserEmail(ctx context.Context, token string) error {
	hash := recoveryCodeHash(token)
	now := s.nowUTC()
	user, consumed, err := s.repo.ConsumeWorkspaceUserEmailVerification(ctx, hash, now)
	if err != nil {
		return err
	}
	if !consumed {
		return ErrVerificationTokenInvalid
	}
	_ = s.audit(ctx, user.Email, "email_verified", "workspace_user", user.ID, "Verified workspace user email")
	return nil
}

func (s *Service) RenewEmailVerification(ctx context.Context, email string) (WorkspaceUser, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	user, found, err := s.findWorkspaceUserByLoginEmail(ctx, email)
	if err != nil {
		return WorkspaceUser{}, "", err
	}
	if !found {
		return WorkspaceUser{}, "", ErrEmailVerificationUnavailable
	}
	token, err := auth.RandomToken(32)
	if err != nil {
		return WorkspaceUser{}, "", err
	}
	now := s.nowUTC()
	expires := now.Add(emailVerificationTokenLifetime)
	user, issued, err := s.repo.IssueWorkspaceUserEmailVerification(ctx, user.Email, recoveryCodeHash(token), now, expires, emailVerificationResendCooldown)
	if err != nil {
		return WorkspaceUser{}, "", err
	}
	if !issued {
		return WorkspaceUser{}, "", ErrEmailVerificationUnavailable
	}
	return user, token, nil
}

func (s *Service) CancelEmailVerificationIssue(ctx context.Context, userID, token string) error {
	_, err := s.repo.CancelWorkspaceUserEmailVerification(ctx, userID, recoveryCodeHash(token), s.nowUTC())
	return err
}

func (s *Service) AuthenticateWorkspaceUser(ctx context.Context, email, password string, requireVerified bool) (WorkspaceUser, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	user, found, err := s.findWorkspaceUserByLoginEmail(ctx, email)
	if err != nil {
		return WorkspaceUser{}, err
	}
	passwordHash := dummyWorkspaceUserPasswordHash
	if found && user.PasswordHash != "" {
		passwordHash = user.PasswordHash
	}
	passwordMatches := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) == nil
	if !found || user.PasswordHash == "" || user.Status != WorkspaceUserStatusActive || (requireVerified && !user.EmailVerified) || !passwordMatches {
		return WorkspaceUser{}, ErrInvalidWorkspaceCredentials
	}
	return user, nil
}

func (s *Service) BeginPasswordReset(ctx context.Context, email string) (WorkspaceUser, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	user, found, err := s.findWorkspaceUserByLoginEmail(ctx, email)
	if err != nil {
		return WorkspaceUser{}, "", err
	}
	if !found {
		return WorkspaceUser{}, "", ErrPasswordResetUnavailable
	}
	token, err := auth.RandomToken(32)
	if err != nil {
		return WorkspaceUser{}, "", err
	}
	now := s.nowUTC()
	expires := now.Add(passwordResetTokenLifetime)
	user, issued, err := s.repo.IssueWorkspaceUserPasswordReset(ctx, user.Email, recoveryCodeHash(token), now, expires, passwordResetRequestCooldown)
	if err != nil {
		return WorkspaceUser{}, "", err
	}
	if !issued {
		return WorkspaceUser{}, "", ErrPasswordResetUnavailable
	}
	_ = s.audit(ctx, email, "password_reset_requested", "workspace_user", user.ID, "Requested password reset")
	return user, token, nil
}

func (s *Service) CancelPasswordResetIssue(ctx context.Context, userID, token string) error {
	_, err := s.repo.CancelWorkspaceUserPasswordReset(ctx, userID, recoveryCodeHash(token), s.nowUTC())
	return err
}

func (s *Service) CompletePasswordReset(ctx context.Context, token, password string) (WorkspaceUser, error) {
	if err := validatePasswordStrength(password); err != nil {
		return WorkspaceUser{}, err
	}
	hashToken := recoveryCodeHash(token)
	current, found, err := s.repo.FindWorkspaceUserByPasswordResetHash(ctx, hashToken)
	if err != nil {
		return WorkspaceUser{}, err
	}
	now := s.nowUTC()
	if !found || current.PasswordResetExpiresAt == nil || !now.Before(*current.PasswordResetExpiresAt) {
		return WorkspaceUser{}, ErrResetTokenInvalid
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return WorkspaceUser{}, err
	}
	user, consumed, err := s.repo.ConsumeWorkspaceUserPasswordReset(ctx, hashToken, string(passwordHash), now)
	if err != nil {
		return WorkspaceUser{}, err
	}
	if !consumed {
		return WorkspaceUser{}, ErrResetTokenInvalid
	}
	_ = s.audit(ctx, user.Email, "password_reset_completed", "workspace_user", user.ID, "Completed password reset")
	return user, nil
}

func (s *Service) BeginTOTPSetup(ctx context.Context, actor, currentPassword string) (TOTPSetup, error) {
	user, err := s.workspaceUserByID(ctx, actor)
	if err != nil {
		return TOTPSetup{}, err
	}
	if user.Status != WorkspaceUserStatusActive {
		return TOTPSetup{}, ErrWorkspaceUserDisabled
	}
	if user.TOTPEnabled {
		return TOTPSetup{}, ErrTOTPAlreadyEnabled
	}
	if user.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)) != nil {
		return TOTPSetup{}, ErrCurrentPasswordIncorrect
	}
	now := s.nowUTC()
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		return TOTPSetup{}, err
	}
	expiresAt := now.Add(totpSetupTTL)
	ciphertext, err := encryptTOTPSecret(s.secretKey, secret, &expiresAt)
	if err != nil {
		return TOTPSetup{}, err
	}
	user.TOTPEnabled = false
	user.TOTPSecretCiphertext = ciphertext
	user.UpdatedAt = now
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return TOTPSetup{}, err
	}
	if err := s.audit(ctx, actor, "totp_setup_started", "workspace_user", user.ID, "Started TOTP enrollment"); err != nil {
		return TOTPSetup{}, err
	}
	return TOTPSetup{Secret: secret, ProvisioningURI: auth.TOTPProvisioningURI("AsterRouter", user.Email, secret), ExpiresAt: expiresAt}, nil
}

func (s *Service) ConfirmTOTP(ctx context.Context, actor, code string) error {
	_, err := s.confirmTOTP(ctx, actor, code, false)
	return err
}

func (s *Service) ConfirmTOTPWithRecoveryCodes(ctx context.Context, actor, code string) ([]string, error) {
	return s.confirmTOTP(ctx, actor, code, true)
}

func (s *Service) confirmTOTP(ctx context.Context, actor, code string, includeRecoveryCodes bool) ([]string, error) {
	user, err := s.workspaceUserByID(ctx, actor)
	if err != nil {
		return nil, err
	}
	if user.TOTPEnabled {
		return nil, ErrTOTPAlreadyEnabled
	}
	if strings.TrimSpace(user.TOTPSecretCiphertext) == "" {
		return nil, ErrTOTPEnrollmentNotStarted
	}
	secret, setupExpiresAt, err := decryptTOTPSecret(s.secretKey, user.TOTPSecretCiphertext)
	if err != nil {
		return nil, err
	}
	now := s.nowUTC()
	if setupExpiresAt == nil || !now.Before(*setupExpiresAt) {
		return nil, ErrTOTPEnrollmentExpired
	}
	if !auth.ValidateTOTP(secret, code, now) {
		return nil, ErrTOTPInvalidCode
	}
	var codes []string
	if includeRecoveryCodes {
		var hashes []string
		codes, hashes, err = newTOTPRecoveryCodes()
		if err != nil {
			return nil, err
		}
		user.TOTPRecoveryHashes = hashes
	}
	ciphertext, err := encryptTOTPSecret(s.secretKey, secret, nil)
	if err != nil {
		return nil, err
	}
	user.TOTPEnabled = true
	user.TOTPSecretCiphertext = ciphertext
	user.SessionVersion++
	user.UpdatedAt = now
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, actor, "totp_enabled", "workspace_user", user.ID, "Enabled TOTP authentication"); err != nil {
		return nil, err
	}
	return codes, nil
}

func (s *Service) DisableTOTP(ctx context.Context, actor, code string) error {
	user, err := s.VerifyUserTOTP(ctx, actor, code)
	if err != nil {
		return err
	}
	user.TOTPEnabled = false
	user.TOTPSecretCiphertext = ""
	user.TOTPRecoveryHashes = nil
	user.SessionVersion++
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return err
	}
	if err := s.audit(ctx, actor, "totp_disabled", "workspace_user", user.ID, "Disabled TOTP authentication"); err != nil {
		return err
	}
	return nil
}

func (s *Service) VerifyUserTOTP(ctx context.Context, userID, code string) (WorkspaceUser, error) {
	user, err := s.workspaceUserByID(ctx, userID)
	if err != nil {
		return WorkspaceUser{}, err
	}
	if user.Status != WorkspaceUserStatusActive || !user.TOTPEnabled {
		return WorkspaceUser{}, ErrTOTPNotEnabled
	}
	secret, _, err := decryptTOTPSecret(s.secretKey, user.TOTPSecretCiphertext)
	if err == nil && auth.ValidateTOTP(secret, code, time.Now().UTC()) {
		return user, nil
	}
	hash := recoveryCodeHash(code)
	user, consumed, err := s.repo.ConsumeWorkspaceUserTOTPRecoveryCode(ctx, userID, hash, time.Now().UTC())
	if err != nil {
		return WorkspaceUser{}, err
	}
	if consumed {
		_ = s.audit(ctx, userID, "totp_recovery_used", "workspace_user", user.ID, "Used a TOTP recovery code")
		return user, nil
	}
	return WorkspaceUser{}, ErrTOTPInvalidCode
}

func (s *Service) GenerateTOTPRecoveryCodes(ctx context.Context, actor, code string) ([]string, error) {
	user, err := s.VerifyUserTOTP(ctx, actor, code)
	if err != nil {
		return nil, err
	}
	codes, hashes, err := newTOTPRecoveryCodes()
	if err != nil {
		return nil, err
	}
	user.TOTPRecoveryHashes = hashes
	user.SessionVersion++
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, actor, "totp_recovery_regenerated", "workspace_user", user.ID, "Regenerated TOTP recovery codes"); err != nil {
		return nil, err
	}
	return codes, nil
}

func newTOTPRecoveryCodes() ([]string, []string, error) {
	codes := make([]string, 10)
	hashes := make([]string, 10)
	for i := range codes {
		token, err := auth.GenerateRecoveryCode()
		if err != nil {
			return nil, nil, err
		}
		codes[i], hashes[i] = token, recoveryCodeHash(token)
	}
	return codes, hashes, nil
}

func recoveryCodeHash(code string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}

func encryptTOTPSecret(secretKey, secret string, setupExpiresAt *time.Time) (string, error) {
	payload, err := json.Marshal(totpSecretEnvelope{Secret: secret, SetupExpiresAt: setupExpiresAt})
	if err != nil {
		return "", err
	}
	return encryptSecret(secretKey, string(payload))
}

func decryptTOTPSecret(secretKey, ciphertext string) (string, *time.Time, error) {
	plaintext, err := decryptSecret(secretKey, ciphertext)
	if err != nil {
		return "", nil, err
	}
	var envelope totpSecretEnvelope
	if json.Unmarshal([]byte(plaintext), &envelope) == nil && strings.TrimSpace(envelope.Secret) != "" {
		return envelope.Secret, envelope.SetupExpiresAt, nil
	}
	if strings.TrimSpace(plaintext) == "" {
		return "", nil, errors.New("TOTP secret is empty")
	}
	return plaintext, nil, nil
}

// constantTimeHashEqual 以常量时间比较两个 hex 编码的哈希值，防时序攻击。
func constantTimeHashEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (s *Service) ListWorkspaceUsers(ctx context.Context) ([]WorkspaceUser, error) {
	return s.repo.ListWorkspaceUsers(ctx)
}

func (s *Service) ExternalIdentityExists(ctx context.Context, issuer, subject string) (bool, error) {
	issuer = strings.TrimSpace(issuer)
	subject = strings.TrimSpace(subject)
	if issuer == "" || subject == "" {
		return false, nil
	}
	_, found, err := s.repo.FindAuthIdentity(ctx, issuer, subject)
	if err != nil || found {
		return found, err
	}
	_, found, err = s.repo.FindWorkspaceUserByExternalIdentity(ctx, issuer, subject)
	return found, err
}

func (s *Service) UnbindCurrentAuthIdentity(ctx context.Context, actor, provider string) error {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		return err
	}
	identities, err := s.repo.ListAuthIdentities(ctx, user.ID)
	if err != nil {
		return err
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	index := -1
	for i, identity := range identities {
		issuer := strings.ToLower(identity.Issuer)
		matches := issuer == provider || (provider == "feishu" && strings.HasPrefix(issuer, "feishu:"))
		if provider == "oidc" {
			matches = issuer != "github" && issuer != "google" && issuer != "dingtalk" && !strings.HasPrefix(issuer, "feishu:")
		}
		if matches {
			index = i
			break
		}
	}
	if index < 0 {
		return ErrAuthIdentityNotBound
	}
	if user.PasswordHash == "" && len(identities) <= 1 {
		return ErrLastLoginMethod
	}
	identity := identities[index]
	if err := s.repo.DeleteAuthIdentity(ctx, identity.ID); err != nil {
		return err
	}
	if user.ExternalIssuer == identity.Issuer && user.ExternalSubject == identity.Subject {
		user.ExternalIssuer, user.ExternalSubject = "", ""
		user.UpdatedAt = time.Now().UTC()
		if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
			return err
		}
	}
	return s.audit(ctx, actor, "auth_identity_unbound", "workspace_user", user.ID, "Unbound "+identity.Issuer+" authentication identity")
}

func (s *Service) BindCurrentAuthIdentity(ctx context.Context, actor, issuer, subject, email string, emailVerified bool) error {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		return err
	}
	if user.Status != WorkspaceUserStatusActive {
		return errors.New("workspace user is disabled")
	}
	issuer, subject = strings.TrimSpace(issuer), strings.TrimSpace(subject)
	if issuer == "" || subject == "" {
		return errors.New("authentication identity is incomplete")
	}
	if existing, found, err := s.repo.FindAuthIdentity(ctx, issuer, subject); err != nil {
		return err
	} else if found {
		if existing.UserID == user.ID {
			return errors.New("authentication identity is already bound")
		}
		return errors.New("authentication identity is already bound to another user")
	}
	now := time.Now().UTC()
	identity := AuthIdentity{ID: "aid_" + randomID(10), UserID: user.ID, Issuer: issuer, Subject: subject, Email: strings.ToLower(strings.TrimSpace(email)), CreatedAt: now, UpdatedAt: now}
	verifyEmail := emailVerified && !user.EmailVerified && strings.EqualFold(user.Email, identity.Email)
	if err := s.repo.BindAuthIdentity(ctx, identity, verifyEmail); err != nil {
		return err
	}
	return s.audit(ctx, actor, "auth_identity_bound", "workspace_user", user.ID, "Bound "+issuer+" authentication identity")
}

func (s *Service) SessionVersion(ctx context.Context, actor string) (int64, bool, error) {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		if errors.Is(err, ErrConfigManagedAccount) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if user.Status != WorkspaceUserStatusActive {
		return user.SessionVersion + 1, true, nil
	}
	return user.SessionVersion, true, nil
}

func (s *Service) RevokeAccountSessions(ctx context.Context, actor string) error {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		return err
	}
	user.SessionVersion++
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return err
	}
	return s.audit(ctx, actor, "account_sessions_revoked", "workspace_user", user.ID, "Revoked all account sessions")
}

func (s *Service) ProvisionOIDCUser(ctx context.Context, issuer, subject, email, displayName, departmentCode string, emailVerified bool, defaults ...WorkspaceUserDefaults) (WorkspaceUser, error) {
	issuer = strings.TrimSpace(issuer)
	subject = strings.TrimSpace(subject)
	var err error
	email, err = normalizeWorkspaceUserEmail(email)
	if issuer == "" || subject == "" {
		return WorkspaceUser{}, errors.New("oidc issuer and subject are required")
	}
	if err != nil {
		return WorkspaceUser{}, errors.New("oidc email claim is required")
	}
	identity, found, err := s.repo.FindAuthIdentity(ctx, issuer, subject)
	if err != nil {
		return WorkspaceUser{}, err
	}
	if found {
		user, err := s.workspaceUserByID(ctx, identity.UserID)
		if err != nil {
			return WorkspaceUser{}, err
		}
		if user.Status != WorkspaceUserStatusActive {
			return WorkspaceUser{}, errors.New("workspace user is disabled")
		}
		if emailVerified && !user.EmailVerified && user.Email == email {
			user.EmailVerified = true
			user.EmailVerifyHash = ""
			user.EmailVerifyExpiresAt = nil
			user.EmailVerifySentAt = nil
			user.UpdatedAt = time.Now().UTC()
			if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
				return WorkspaceUser{}, err
			}
		}
		return user, nil
	}
	legacyUser, legacyFound, err := s.repo.FindWorkspaceUserByExternalIdentity(ctx, issuer, subject)
	if err != nil {
		return WorkspaceUser{}, err
	}
	if legacyFound {
		if legacyUser.Status != WorkspaceUserStatusActive {
			return WorkspaceUser{}, errors.New("workspace user is disabled")
		}
		now := time.Now().UTC()
		verifyEmail := emailVerified && !legacyUser.EmailVerified && legacyUser.Email == email
		identity := AuthIdentity{ID: "aid_" + randomID(10), UserID: legacyUser.ID, Issuer: issuer, Subject: subject, Email: legacyUser.Email, CreatedAt: now, UpdatedAt: now}
		if err := s.repo.BindAuthIdentity(ctx, identity, verifyEmail); err != nil {
			return WorkspaceUser{}, err
		}
		if verifyEmail {
			legacyUser.EmailVerified = true
			legacyUser.EmailVerifyHash = ""
			legacyUser.EmailVerifyExpiresAt = nil
			legacyUser.EmailVerifySentAt = nil
			legacyUser.UpdatedAt = now
		}
		return legacyUser, nil
	}
	if existingEmail, emailFound, err := s.repo.FindWorkspaceUserByEmail(ctx, email); err != nil {
		return WorkspaceUser{}, err
	} else if emailFound && (existingEmail.ExternalIssuer != "" || existingEmail.ExternalSubject != "") {
		return WorkspaceUser{}, errors.New("email is already bound to another external identity")
	}
	departmentID := ""
	if code := strings.TrimSpace(departmentCode); code != "" {
		departments, err := s.repo.ListDepartments(ctx)
		if err != nil {
			return WorkspaceUser{}, err
		}
		for _, department := range departments {
			if strings.EqualFold(department.Code, code) && department.Status == DepartmentStatusActive {
				departmentID = department.ID
				break
			}
		}
	}
	now := time.Now().UTC()
	user := WorkspaceUser{ID: "usr_" + randomID(10), Email: email, DisplayName: strings.TrimSpace(displayName), Status: WorkspaceUserStatusActive, Role: RoleDeveloper, ExternalIssuer: issuer, ExternalSubject: subject, DepartmentID: departmentID, EmailVerified: emailVerified, CreatedAt: now, UpdatedAt: now}
	applyWorkspaceUserDefaults(&user, defaults)
	if err := s.ensureUniqueUserEmail(ctx, "", email); err != nil {
		return WorkspaceUser{}, err
	}
	identity = AuthIdentity{ID: "aid_" + randomID(10), UserID: user.ID, Issuer: issuer, Subject: subject, Email: email, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.SaveProvisionedWorkspaceUser(ctx, user, identity); err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.audit(ctx, email, "oidc_provision", "workspace_user", user.ID, fmt.Sprintf("Provisioned workspace user %s through OIDC", email)); err != nil {
		return WorkspaceUser{}, err
	}
	return user, nil
}

func applyWorkspaceUserDefaults(user *WorkspaceUser, values []WorkspaceUserDefaults) {
	if len(values) == 0 {
		return
	}
	user.ConcurrencyLimit = max(values[0].ConcurrencyLimit, 0)
	user.RPMLimit = max(values[0].RPMLimit, 0)
}

func (s *Service) CreateWorkspaceUser(ctx context.Context, actor string, req WorkspaceUserRequest) (WorkspaceUser, error) {
	now := time.Now().UTC()
	user, err := workspaceUserFromRequest(req, now)
	if err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.validateWorkspaceUserDepartment(ctx, user.DepartmentID); err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.ensureUniqueUserEmail(ctx, "", user.Email); err != nil {
		return WorkspaceUser{}, err
	}
	user.ID = "usr_" + randomID(10)
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.audit(ctx, actor, "create", "workspace_user", user.ID, fmt.Sprintf("Created workspace user %s", user.Email)); err != nil {
		return WorkspaceUser{}, err
	}
	return user, nil
}

func (s *Service) UpdateWorkspaceUser(ctx context.Context, actor string, id string, req WorkspaceUserRequest) (WorkspaceUser, error) {
	existing, err := s.workspaceUserByID(ctx, id)
	if err != nil {
		return WorkspaceUser{}, err
	}
	user, err := workspaceUserFromRequest(req, existing.CreatedAt)
	if err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.ensureUniqueUserEmail(ctx, existing.ID, user.Email); err != nil {
		return WorkspaceUser{}, err
	}
	user.ID = existing.ID
	user.AvatarDataURL = existing.AvatarDataURL
	user.ExternalIssuer = existing.ExternalIssuer
	user.ExternalSubject = existing.ExternalSubject
	if req.DepartmentID == nil {
		user.DepartmentID = existing.DepartmentID
	}
	if err := s.validateWorkspaceUserDepartment(ctx, user.DepartmentID); err != nil {
		return WorkspaceUser{}, err
	}
	user.TOTPEnabled = existing.TOTPEnabled
	user.TOTPSecretCiphertext = existing.TOTPSecretCiphertext
	user.TOTPRecoveryHashes = existing.TOTPRecoveryHashes
	user.PasswordHash = existing.PasswordHash
	emailChanged := !strings.EqualFold(strings.TrimSpace(user.Email), strings.TrimSpace(existing.Email))
	if !emailChanged {
		user.EmailVerified = existing.EmailVerified
		user.EmailVerifyHash = existing.EmailVerifyHash
		user.EmailVerifyExpiresAt = existing.EmailVerifyExpiresAt
		user.EmailVerifySentAt = existing.EmailVerifySentAt
		user.PasswordResetHash = existing.PasswordResetHash
		user.PasswordResetExpiresAt = existing.PasswordResetExpiresAt
		user.PasswordResetSentAt = existing.PasswordResetSentAt
	}
	user.SessionVersion = existing.SessionVersion
	if emailChanged || user.Status != existing.Status || user.Role != existing.Role || user.DepartmentID != existing.DepartmentID {
		user.SessionVersion++
	}
	user.CreatedAt = existing.CreatedAt
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.audit(ctx, actor, "update", "workspace_user", user.ID, fmt.Sprintf("Updated workspace user %s", user.Email)); err != nil {
		return WorkspaceUser{}, err
	}
	return user, nil
}

func (s *Service) validateWorkspaceUserDepartment(ctx context.Context, departmentID string) error {
	departmentID = strings.TrimSpace(departmentID)
	if departmentID == "" {
		return nil
	}
	departments, err := s.repo.ListDepartments(ctx)
	if err != nil {
		return err
	}
	for _, department := range departments {
		if department.ID == departmentID && department.Status == DepartmentStatusActive {
			return nil
		}
	}
	return errors.New("active department not found")
}

func (s *Service) ListRoleBindings(ctx context.Context) ([]RoleBinding, error) {
	return s.repo.ListRoleBindings(ctx)
}

func (s *Service) CreateRoleBinding(ctx context.Context, actor string, req RoleBindingRequest) (RoleBinding, error) {
	now := time.Now().UTC()
	binding, err := s.roleBindingFromRequest(ctx, req, now)
	if err != nil {
		return RoleBinding{}, err
	}
	if err := s.ensureUniqueRoleBinding(ctx, binding); err != nil {
		return RoleBinding{}, err
	}
	binding.ID = "rb_" + randomID(10)
	if err := s.repo.SaveRoleBinding(ctx, binding); err != nil {
		return RoleBinding{}, err
	}
	if err := s.audit(ctx, actor, "grant_role", "role_binding", binding.ID, fmt.Sprintf("Granted %s on %s:%s to %s", binding.Role, binding.ScopeType, binding.ScopeID, binding.UserID)); err != nil {
		return RoleBinding{}, err
	}
	return binding, nil
}

func (s *Service) DeleteRoleBinding(ctx context.Context, actor string, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("role binding id is required")
	}
	binding, err := s.roleBindingByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteRoleBinding(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("role binding %s not found", id)
		}
		return err
	}
	return s.audit(ctx, actor, "revoke_role", "role_binding", binding.ID, fmt.Sprintf("Revoked %s on %s:%s from %s", binding.Role, binding.ScopeType, binding.ScopeID, binding.UserID))
}

func workspaceUserFromRequest(req WorkspaceUserRequest, createdAt time.Time) (WorkspaceUser, error) {
	now := time.Now().UTC()
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		return WorkspaceUser{}, errors.New("valid user email is required")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = WorkspaceUserStatusActive
	}
	if status != WorkspaceUserStatusActive && status != WorkspaceUserStatusDisabled {
		return WorkspaceUser{}, errors.New("invalid user status")
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = RoleDeveloper
	}
	if !validRole(role) {
		return WorkspaceUser{}, errors.New("invalid user role")
	}
	if createdAt.IsZero() {
		createdAt = now
	}
	departmentID := ""
	if req.DepartmentID != nil {
		departmentID = strings.TrimSpace(*req.DepartmentID)
	}
	return WorkspaceUser{
		Email:        email,
		DisplayName:  strings.TrimSpace(req.DisplayName),
		Status:       status,
		Role:         role,
		DepartmentID: departmentID,
		CreatedAt:    createdAt,
		UpdatedAt:    now,
	}, nil
}

func (s *Service) roleBindingFromRequest(ctx context.Context, req RoleBindingRequest, createdAt time.Time) (RoleBinding, error) {
	now := time.Now().UTC()
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		return RoleBinding{}, errors.New("user id is required")
	}
	if _, err := s.workspaceUserByID(ctx, userID); err != nil {
		return RoleBinding{}, err
	}
	role := strings.TrimSpace(req.Role)
	if !validRole(role) {
		return RoleBinding{}, errors.New("invalid role")
	}
	scopeType := strings.TrimSpace(req.ScopeType)
	if scopeType == "" {
		scopeType = RoleScopeOrganization
	}
	if !oneOf(scopeType, RoleScopeOrganization, RoleScopeDepartment, RoleScopeGroup, RoleScopeApplication, RoleScopeResource) {
		return RoleBinding{}, errors.New("invalid role scope")
	}
	scopeID := strings.TrimSpace(req.ScopeID)
	if scopeType == RoleScopeOrganization {
		scopeID = ""
	} else if scopeID == "" {
		return RoleBinding{}, errors.New("scope_id is required for scoped role bindings")
	} else if scopeType == RoleScopeResource && !validRBACResource(scopeID) {
		return RoleBinding{}, errors.New("invalid RBAC resource scope")
	} else if scopeType == RoleScopeDepartment {
		if _, err := s.departmentByID(ctx, scopeID); err != nil {
			return RoleBinding{}, errors.New("department scope does not exist")
		}
	} else if scopeType == RoleScopeGroup {
		groups, err := s.repo.ListOrganizationGroups(ctx)
		if err != nil {
			return RoleBinding{}, err
		}
		found := false
		for _, group := range groups {
			if group.ID == scopeID {
				found = true
				break
			}
		}
		if !found {
			return RoleBinding{}, errors.New("group scope does not exist")
		}
	} else if scopeType == RoleScopeApplication {
		applications, err := s.repo.ListApplications(ctx)
		if err != nil {
			return RoleBinding{}, err
		}
		found := false
		for _, application := range applications {
			if application.ID == scopeID {
				found = true
				break
			}
		}
		if !found {
			return RoleBinding{}, errors.New("application scope does not exist")
		}
	}
	if createdAt.IsZero() {
		createdAt = now
	}
	return RoleBinding{
		UserID:    userID,
		Role:      role,
		ScopeType: scopeType,
		ScopeID:   scopeID,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}, nil
}

func validRBACResource(resource string) bool {
	return oneOf(resource,
		RBACResourceDashboard, RBACResourceRouting, RBACResourceProviders, RBACResourceAPIKeys,
		RBACResourceUsage, RBACResourceTraces, RBACResourceAIJobs, RBACResourceArtifacts, RBACResourceAlerts, RBACResourceIdentity,
		RBACResourcePolicies, RBACResourceAudit, RBACResourceExports, RBACResourcePlugins,
		RBACResourceApplications, RBACResourceSettings, RBACResourceSystem,
	)
}

func validRole(role string) bool {
	switch role {
	case RoleSuperAdmin, RolePlatformAdmin, RoleKeyManager, RoleReadOnlyAuditor, RoleDeveloper:
		return true
	default:
		return false
	}
}

func (s *Service) workspaceUserByID(ctx context.Context, id string) (WorkspaceUser, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return WorkspaceUser{}, errors.New("user id is required")
	}
	user, found, err := s.repo.FindWorkspaceUserByID(ctx, id)
	if err != nil {
		return WorkspaceUser{}, err
	}
	if found {
		return user, nil
	}
	return WorkspaceUser{}, fmt.Errorf("user %s not found", id)
}

func (s *Service) workspaceUserForActor(ctx context.Context, actor string) (WorkspaceUser, error) {
	actor = strings.TrimSpace(actor)
	user, found, err := s.repo.FindWorkspaceUserByID(ctx, actor)
	if err != nil || found {
		return user, err
	}
	user, found, err = s.findWorkspaceUserByLoginEmail(ctx, actor)
	if err != nil {
		return WorkspaceUser{}, err
	}
	if found {
		return user, nil
	}
	return WorkspaceUser{}, ErrConfigManagedAccount
}

func (s *Service) roleBindingByID(ctx context.Context, id string) (RoleBinding, error) {
	bindings, err := s.repo.ListRoleBindings(ctx)
	if err != nil {
		return RoleBinding{}, err
	}
	for _, binding := range bindings {
		if binding.ID == id {
			return binding, nil
		}
	}
	return RoleBinding{}, fmt.Errorf("role binding %s not found", id)
}

// ensureUniqueUserEmail 拒绝与已有账号冲突的邮箱：既比对邮箱原值，也比对归一化后的
// “收件箱标识”，防止同一收件箱借 +别名 / Gmail 点号 / FQDN 根点派生多个账号
// （见 NormalizeEmailForAliasDedup）。冲突时对外只说明邮箱已被占用，不透露命中的是
// 哪个变体，避免泄漏他人已注册的具体地址。
func (s *Service) ensureUniqueUserEmail(ctx context.Context, currentID string, email string) error {
	user, found, err := s.repo.FindWorkspaceUserByEmail(ctx, email)
	if err != nil {
		return err
	}
	if found && user.ID != currentID {
		return fmt.Errorf("%w: %s", ErrUserEmailExists, email)
	}
	normalized := NormalizeEmailForAliasDedup(email)
	if normalized == "" {
		return nil
	}
	user, found, err = s.repo.FindWorkspaceUserByEmailNormalized(ctx, normalized)
	if err != nil {
		return err
	}
	if found && user.ID != currentID {
		return fmt.Errorf("%w: %s", ErrUserEmailExists, email)
	}
	return nil
}

// findWorkspaceUserByLoginEmail keeps local authentication compatible with
// historical mixed-case addresses while preserving the rule that provider
// aliases are only used for registration deduplication, not as login names.
func (s *Service) findWorkspaceUserByLoginEmail(ctx context.Context, email string) (WorkspaceUser, bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	user, found, err := s.repo.FindWorkspaceUserByEmail(ctx, email)
	if err != nil || found {
		return user, found, err
	}
	normalized := NormalizeEmailForAliasDedup(email)
	if normalized == "" {
		return WorkspaceUser{}, false, nil
	}
	user, found, err = s.repo.FindWorkspaceUserByEmailNormalized(ctx, normalized)
	if err != nil || !found {
		return user, found, err
	}
	if !strings.EqualFold(strings.TrimSpace(user.Email), email) {
		return WorkspaceUser{}, false, nil
	}
	return user, true, nil
}

func (s *Service) ensureUniqueRoleBinding(ctx context.Context, next RoleBinding) error {
	bindings, err := s.repo.ListRoleBindings(ctx)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if binding.UserID == next.UserID && binding.Role == next.Role && binding.ScopeType == next.ScopeType && binding.ScopeID == next.ScopeID {
			return errors.New("role binding already exists")
		}
	}
	return nil
}
