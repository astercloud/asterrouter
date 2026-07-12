package controlplane

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

type TOTPSetup struct {
	Secret          string `json:"secret"`
	ProvisioningURI string `json:"provisioning_uri"`
}

func (s *Service) RegisterWorkspaceUser(ctx context.Context, email, password, displayName string, requireVerification bool) (WorkspaceUser, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return WorkspaceUser{}, "", errors.New("valid email is required")
	}
	if len(password) < 10 {
		return WorkspaceUser{}, "", errors.New("password must contain at least 10 characters")
	}
	if err := s.ensureUniqueUserEmail(ctx, "", email); err != nil {
		return WorkspaceUser{}, "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return WorkspaceUser{}, "", err
	}
	now := time.Now().UTC()
	user := WorkspaceUser{ID: "usr_" + randomID(10), Email: email, DisplayName: strings.TrimSpace(displayName), Status: WorkspaceUserStatusActive, Role: RoleDeveloper, PasswordHash: string(hash), EmailVerified: !requireVerification, CreatedAt: now, UpdatedAt: now}
	verificationToken := ""
	if requireVerification {
		verificationToken, err = auth.RandomToken(32)
		if err != nil {
			return WorkspaceUser{}, "", err
		}
		user.EmailVerifyHash = recoveryCodeHash(verificationToken)
		expires := now.Add(30 * time.Minute)
		user.EmailVerifyExpiresAt = &expires
	}
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return WorkspaceUser{}, "", err
	}
	if err := s.audit(ctx, email, "register", "workspace_user", user.ID, "Registered workspace user"); err != nil {
		return WorkspaceUser{}, "", err
	}
	return user, verificationToken, nil
}

func (s *Service) VerifyWorkspaceUserEmail(ctx context.Context, token string) error {
	hash := recoveryCodeHash(token)
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, user := range users {
		if user.EmailVerifyHash == hash && user.EmailVerifyExpiresAt != nil && now.Before(*user.EmailVerifyExpiresAt) {
			user.EmailVerified = true
			user.EmailVerifyHash = ""
			user.EmailVerifyExpiresAt = nil
			user.UpdatedAt = now
			if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
				return err
			}
			return s.audit(ctx, user.Email, "email_verified", "workspace_user", user.ID, "Verified workspace user email")
		}
	}
	return errors.New("email verification token is invalid or expired")
}

func (s *Service) RenewEmailVerification(ctx context.Context, email string) (WorkspaceUser, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return WorkspaceUser{}, "", err
	}
	for _, user := range users {
		if user.Email == email && user.Status == WorkspaceUserStatusActive && !user.EmailVerified {
			token, err := auth.RandomToken(32)
			if err != nil {
				return WorkspaceUser{}, "", err
			}
			expires := time.Now().UTC().Add(30 * time.Minute)
			user.EmailVerifyHash = recoveryCodeHash(token)
			user.EmailVerifyExpiresAt = &expires
			user.UpdatedAt = time.Now().UTC()
			if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
				return WorkspaceUser{}, "", err
			}
			return user, token, nil
		}
	}
	return WorkspaceUser{}, "", errors.New("user is not awaiting email verification")
}

func (s *Service) AuthenticateWorkspaceUser(ctx context.Context, email, password string, requireVerified bool) (WorkspaceUser, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return WorkspaceUser{}, err
	}
	for _, user := range users {
		if user.Email == email && user.PasswordHash != "" {
			if user.Status != WorkspaceUserStatusActive || (requireVerified && !user.EmailVerified) || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
				break
			}
			return user, nil
		}
	}
	return WorkspaceUser{}, errors.New("invalid email or password")
}

func (s *Service) BeginPasswordReset(ctx context.Context, email string) (WorkspaceUser, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return WorkspaceUser{}, "", err
	}
	for _, user := range users {
		if user.Email == email && user.Status == WorkspaceUserStatusActive && user.PasswordHash != "" {
			token, err := auth.RandomToken(32)
			if err != nil {
				return WorkspaceUser{}, "", err
			}
			expires := time.Now().UTC().Add(30 * time.Minute)
			user.PasswordResetHash = recoveryCodeHash(token)
			user.PasswordResetExpiresAt = &expires
			user.UpdatedAt = time.Now().UTC()
			if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
				return WorkspaceUser{}, "", err
			}
			_ = s.audit(ctx, email, "password_reset_requested", "workspace_user", user.ID, "Requested password reset")
			return user, token, nil
		}
	}
	return WorkspaceUser{}, "", errors.New("user is not eligible for password reset")
}

func (s *Service) CompletePasswordReset(ctx context.Context, token, password string) error {
	if len(password) < 10 {
		return errors.New("password must contain at least 10 characters")
	}
	hashToken := recoveryCodeHash(token)
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, user := range users {
		if user.PasswordResetHash == hashToken && user.PasswordResetExpiresAt != nil && now.Before(*user.PasswordResetExpiresAt) {
			passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return err
			}
			user.PasswordHash = string(passwordHash)
			user.PasswordResetHash = ""
			user.PasswordResetExpiresAt = nil
			user.UpdatedAt = now
			if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
				return err
			}
			return s.audit(ctx, user.Email, "password_reset_completed", "workspace_user", user.ID, "Completed password reset")
		}
	}
	return errors.New("password reset token is invalid or expired")
}

func (s *Service) BeginTOTPSetup(ctx context.Context, actor string) (TOTPSetup, error) {
	user, err := s.workspaceUserByID(ctx, actor)
	if err != nil {
		return TOTPSetup{}, err
	}
	if user.Status != WorkspaceUserStatusActive {
		return TOTPSetup{}, errors.New("workspace user is disabled")
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		return TOTPSetup{}, err
	}
	ciphertext, err := encryptSecret(s.secretKey, secret)
	if err != nil {
		return TOTPSetup{}, err
	}
	user.TOTPEnabled = false
	user.TOTPSecretCiphertext = ciphertext
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return TOTPSetup{}, err
	}
	if err := s.audit(ctx, actor, "totp_setup_started", "workspace_user", user.ID, "Started TOTP enrollment"); err != nil {
		return TOTPSetup{}, err
	}
	return TOTPSetup{Secret: secret, ProvisioningURI: auth.TOTPProvisioningURI("AsterRouter", user.Email, secret)}, nil
}

func (s *Service) ConfirmTOTP(ctx context.Context, actor, code string) error {
	user, err := s.workspaceUserByID(ctx, actor)
	if err != nil {
		return err
	}
	secret, err := decryptSecret(s.secretKey, user.TOTPSecretCiphertext)
	if err != nil {
		return errors.New("TOTP enrollment has not been started")
	}
	if !auth.ValidateTOTP(secret, code, time.Now().UTC()) {
		return errors.New("invalid TOTP code")
	}
	user.TOTPEnabled = true
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return err
	}
	return s.audit(ctx, actor, "totp_enabled", "workspace_user", user.ID, "Enabled TOTP authentication")
}

func (s *Service) DisableTOTP(ctx context.Context, actor, code string) error {
	user, err := s.workspaceUserByID(ctx, actor)
	if err != nil {
		return err
	}
	secret, err := decryptSecret(s.secretKey, user.TOTPSecretCiphertext)
	if err != nil || !user.TOTPEnabled || !auth.ValidateTOTP(secret, code, time.Now().UTC()) {
		return errors.New("invalid TOTP code")
	}
	user.TOTPEnabled = false
	user.TOTPSecretCiphertext = ""
	user.TOTPRecoveryHashes = nil
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return err
	}
	return s.audit(ctx, actor, "totp_disabled", "workspace_user", user.ID, "Disabled TOTP authentication")
}

func (s *Service) VerifyUserTOTP(ctx context.Context, userID, code string) (WorkspaceUser, error) {
	user, err := s.workspaceUserByID(ctx, userID)
	if err != nil {
		return WorkspaceUser{}, err
	}
	if user.Status != WorkspaceUserStatusActive || !user.TOTPEnabled {
		return WorkspaceUser{}, errors.New("TOTP is not enabled")
	}
	secret, err := decryptSecret(s.secretKey, user.TOTPSecretCiphertext)
	if err == nil && auth.ValidateTOTP(secret, code, time.Now().UTC()) {
		return user, nil
	}
	hash := recoveryCodeHash(code)
	for index, stored := range user.TOTPRecoveryHashes {
		if stored == hash {
			user.TOTPRecoveryHashes = append(user.TOTPRecoveryHashes[:index], user.TOTPRecoveryHashes[index+1:]...)
			user.UpdatedAt = time.Now().UTC()
			if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
				return WorkspaceUser{}, err
			}
			_ = s.audit(ctx, userID, "totp_recovery_used", "workspace_user", user.ID, "Used a TOTP recovery code")
			return user, nil
		}
	}
	return WorkspaceUser{}, errors.New("invalid TOTP code")
}

func (s *Service) GenerateTOTPRecoveryCodes(ctx context.Context, actor string) ([]string, error) {
	user, err := s.workspaceUserByID(ctx, actor)
	if err != nil {
		return nil, err
	}
	if !user.TOTPEnabled {
		return nil, errors.New("TOTP is not enabled")
	}
	codes := make([]string, 10)
	hashes := make([]string, 10)
	for i := range codes {
		token, err := auth.GenerateRecoveryCode()
		if err != nil {
			return nil, err
		}
		codes[i], hashes[i] = token, recoveryCodeHash(token)
	}
	user.TOTPRecoveryHashes = hashes
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, actor, "totp_recovery_regenerated", "workspace_user", user.ID, "Regenerated TOTP recovery codes"); err != nil {
		return nil, err
	}
	return codes, nil
}

func recoveryCodeHash(code string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}

func (s *Service) ListWorkspaceUsers(ctx context.Context) ([]WorkspaceUser, error) {
	return s.repo.ListWorkspaceUsers(ctx)
}

func (s *Service) ProvisionOIDCUser(ctx context.Context, issuer, subject, email, displayName, departmentCode string) (WorkspaceUser, error) {
	issuer = strings.TrimSpace(issuer)
	subject = strings.TrimSpace(subject)
	email = strings.ToLower(strings.TrimSpace(email))
	if issuer == "" || subject == "" {
		return WorkspaceUser{}, errors.New("oidc issuer and subject are required")
	}
	if email == "" || !strings.Contains(email, "@") {
		return WorkspaceUser{}, errors.New("oidc email claim is required")
	}
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return WorkspaceUser{}, err
	}
	for _, user := range users {
		if user.ExternalIssuer == issuer && user.ExternalSubject == subject {
			if user.Status != WorkspaceUserStatusActive {
				return WorkspaceUser{}, errors.New("workspace user is disabled")
			}
			return user, nil
		}
		if user.Email == email && (user.ExternalIssuer != "" || user.ExternalSubject != "") {
			return WorkspaceUser{}, errors.New("email is already bound to another external identity")
		}
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
	user := WorkspaceUser{ID: "usr_" + randomID(10), Email: email, DisplayName: strings.TrimSpace(displayName), Status: WorkspaceUserStatusActive, Role: RoleDeveloper, ExternalIssuer: issuer, ExternalSubject: subject, DepartmentID: departmentID, CreatedAt: now, UpdatedAt: now}
	if err := s.ensureUniqueUserEmail(ctx, "", email); err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.audit(ctx, email, "oidc_provision", "workspace_user", user.ID, fmt.Sprintf("Provisioned workspace user %s through OIDC", email)); err != nil {
		return WorkspaceUser{}, err
	}
	return user, nil
}

func (s *Service) CreateWorkspaceUser(ctx context.Context, actor string, req WorkspaceUserRequest) (WorkspaceUser, error) {
	now := time.Now().UTC()
	user, err := workspaceUserFromRequest(req, now)
	if err != nil {
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
	user.ExternalIssuer = existing.ExternalIssuer
	user.ExternalSubject = existing.ExternalSubject
	user.DepartmentID = existing.DepartmentID
	user.TOTPEnabled = existing.TOTPEnabled
	user.TOTPSecretCiphertext = existing.TOTPSecretCiphertext
	user.TOTPRecoveryHashes = existing.TOTPRecoveryHashes
	user.PasswordHash = existing.PasswordHash
	user.EmailVerified = existing.EmailVerified
	user.EmailVerifyHash = existing.EmailVerifyHash
	user.EmailVerifyExpiresAt = existing.EmailVerifyExpiresAt
	user.PasswordResetHash = existing.PasswordResetHash
	user.PasswordResetExpiresAt = existing.PasswordResetExpiresAt
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
	return WorkspaceUser{
		Email:       email,
		DisplayName: strings.TrimSpace(req.DisplayName),
		Status:      status,
		Role:        role,
		CreatedAt:   createdAt,
		UpdatedAt:   now,
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
		scopeType = RoleScopeGlobal
	}
	if scopeType != RoleScopeGlobal {
		return RoleBinding{}, errors.New("invalid role scope")
	}
	scopeID := ""
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
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return WorkspaceUser{}, err
	}
	for _, user := range users {
		if user.ID == id {
			return user, nil
		}
	}
	return WorkspaceUser{}, fmt.Errorf("user %s not found", id)
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

func (s *Service) ensureUniqueUserEmail(ctx context.Context, currentID string, email string) error {
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return err
	}
	for _, user := range users {
		if user.Email == email && user.ID != currentID {
			return fmt.Errorf("user email %s already exists", email)
		}
	}
	return nil
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
