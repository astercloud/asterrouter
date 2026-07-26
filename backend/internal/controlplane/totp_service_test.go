package controlplane

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
)

const totpTestPassword = "current-password"

func registerTOTPTestUser(t *testing.T, svc *Service, email string) WorkspaceUser {
	t.Helper()
	user, _, err := svc.RegisterWorkspaceUser(t.Context(), email, totpTestPassword, "TOTP User", false)
	if err != nil {
		t.Fatalf("RegisterWorkspaceUser(): %v", err)
	}
	return user
}

func TestTOTPEnrollmentAndDisable(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user := registerTOTPTestUser(t, svc, "user@example.test")
	if _, err := svc.BeginTOTPSetup(context.Background(), user.ID, "wrong-password"); err == nil {
		t.Fatal("TOTP setup accepted an incorrect current password")
	}
	setup, err := svc.BeginTOTPSetup(context.Background(), user.ID, totpTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	code := auth.GenerateTOTPCode(setup.Secret, time.Now().UTC())
	if err := svc.ConfirmTOTP(context.Background(), user.ID, code); err != nil {
		t.Fatal(err)
	}
	stored, _ := svc.workspaceUserByID(context.Background(), user.ID)
	if stored.SessionVersion != 1 {
		t.Fatalf("session version after TOTP enable = %d, want 1", stored.SessionVersion)
	}
	if _, err := svc.GenerateTOTPRecoveryCodes(context.Background(), user.ID, "000000"); err == nil {
		t.Fatal("recovery-code regeneration accepted an invalid TOTP code")
	}
	recovery, err := svc.GenerateTOTPRecoveryCodes(context.Background(), user.ID, code)
	if err != nil || len(recovery) != 10 {
		t.Fatalf("recovery=%v err=%v", recovery, err)
	}
	if _, err := svc.VerifyUserTOTP(context.Background(), user.ID, recovery[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VerifyUserTOTP(context.Background(), user.ID, recovery[0]); err == nil {
		t.Fatal("recovery code must be single use")
	}
	stored, _ = svc.workspaceUserByID(context.Background(), user.ID)
	if !stored.TOTPEnabled || stored.TOTPSecretCiphertext == "" {
		t.Fatalf("stored = %+v", stored)
	}
	if stored.SessionVersion != 2 {
		t.Fatalf("session version after recovery code regeneration = %d, want 2", stored.SessionVersion)
	}
	if err := svc.DisableTOTP(context.Background(), user.ID, code); err != nil {
		t.Fatal(err)
	}
	stored, _ = svc.workspaceUserByID(context.Background(), user.ID)
	if stored.TOTPEnabled || stored.TOTPSecretCiphertext != "" {
		t.Fatalf("stored after disable = %+v", stored)
	}
	if stored.SessionVersion != 3 {
		t.Fatalf("session version after TOTP disable = %d, want 3", stored.SessionVersion)
	}
}

func TestConfirmTOTPWithRecoveryCodesPersistsEnrollmentAtomically(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user := registerTOTPTestUser(t, svc, "atomic@example.test")
	setup, err := svc.BeginTOTPSetup(t.Context(), user.ID, totpTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	codes, err := svc.ConfirmTOTPWithRecoveryCodes(t.Context(), user.ID, auth.GenerateTOTPCode(setup.Secret, time.Now().UTC()))
	if err != nil || len(codes) != 10 {
		t.Fatalf("codes=%v err=%v", codes, err)
	}
	stored, err := svc.workspaceUserByID(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.TOTPEnabled || len(stored.TOTPRecoveryHashes) != 10 || stored.SessionVersion != 1 {
		t.Fatalf("stored enrollment = %+v", stored)
	}
}

func TestBeginTOTPSetupDoesNotDisableExistingEnrollment(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user := registerTOTPTestUser(t, svc, "enabled@example.test")
	setup, err := svc.BeginTOTPSetup(t.Context(), user.ID, totpTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmTOTP(t.Context(), user.ID, auth.GenerateTOTPCode(setup.Secret, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BeginTOTPSetup(t.Context(), user.ID, totpTestPassword); err == nil {
		t.Fatal("an existing TOTP enrollment must not be replaced by starting setup")
	}
	stored, err := svc.workspaceUserByID(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.TOTPEnabled || stored.TOTPSecretCiphertext == "" {
		t.Fatalf("existing TOTP enrollment was modified: %+v", stored)
	}
}

func TestTOTPSetupExpiresAndCanBeRestarted(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	now := time.Date(2026, time.July, 26, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	user := registerTOTPTestUser(t, svc, "expires@example.test")
	setup, err := svc.BeginTOTPSetup(t.Context(), user.ID, totpTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(totpSetupTTL); !setup.ExpiresAt.Equal(want) {
		t.Fatalf("setup expiry = %v, want %v", setup.ExpiresAt, want)
	}

	now = setup.ExpiresAt
	if err := svc.ConfirmTOTP(t.Context(), user.ID, auth.GenerateTOTPCode(setup.Secret, now)); err == nil {
		t.Fatal("expired TOTP setup was confirmed")
	}
	stored, err := svc.workspaceUserByID(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.TOTPEnabled {
		t.Fatal("expired TOTP setup enabled the account")
	}

	restarted, err := svc.BeginTOTPSetup(t.Context(), user.ID, totpTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmTOTP(t.Context(), user.ID, auth.GenerateTOTPCode(restarted.Secret, now)); err != nil {
		t.Fatalf("restarted TOTP setup could not be confirmed: %v", err)
	}
}

func TestTOTPVerificationReadsLegacyEnabledCiphertext(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user := registerTOTPTestUser(t, svc, "legacy-totp@example.test")
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	user.TOTPSecretCiphertext, err = encryptSecret(svc.secretKey, secret)
	if err != nil {
		t.Fatal(err)
	}
	user.TOTPEnabled = true
	if err := svc.repo.SaveWorkspaceUser(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VerifyUserTOTP(t.Context(), user.ID, auth.GenerateTOTPCode(secret, time.Now().UTC())); err != nil {
		t.Fatalf("legacy TOTP ciphertext was not accepted: %v", err)
	}
}

func TestTOTPRecoveryCodeIsConsumedAtomically(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user := registerTOTPTestUser(t, svc, "recovery@example.test")
	setup, err := svc.BeginTOTPSetup(t.Context(), user.ID, totpTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	codes, err := svc.ConfirmTOTPWithRecoveryCodes(t.Context(), user.ID, auth.GenerateTOTPCode(setup.Secret, time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}

	var successes atomic.Int32
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := svc.VerifyUserTOTP(t.Context(), user.ID, codes[0]); err == nil {
				successes.Add(1)
			}
		}()
	}
	wait.Wait()
	if got := successes.Load(); got != 1 {
		t.Fatalf("recovery code successes = %d, want 1", got)
	}
}
