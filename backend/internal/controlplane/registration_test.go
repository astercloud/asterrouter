package controlplane

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestUnknownWorkspaceLoginUsesValidDummyPasswordHash(t *testing.T) {
	if _, err := bcrypt.Cost([]byte(dummyWorkspaceUserPasswordHash)); err != nil {
		t.Fatalf("dummy password hash is invalid: %v", err)
	}
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	if _, err := svc.AuthenticateWorkspaceUser(t.Context(), "missing@example.test", "long-password", true); err == nil {
		t.Fatal("unknown workspace user authenticated")
	}
}

func TestWorkspaceRegistrationVerificationAndAuthentication(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user, token, err := svc.RegisterWorkspaceUser(context.Background(), "User@Example.test", "long-password", "User", true)
	if err != nil {
		t.Fatal(err)
	}
	if user.EmailVerified || token == "" {
		t.Fatalf("user=%+v token=%q", user, token)
	}
	if _, err := svc.AuthenticateWorkspaceUser(context.Background(), user.Email, "long-password", true); err == nil {
		t.Fatal("unverified user must be rejected")
	}
	if err := svc.VerifyWorkspaceUserEmail(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyWorkspaceUserEmail(context.Background(), token); !errors.Is(err, ErrVerificationTokenInvalid) {
		t.Fatalf("reused verification token error = %v", err)
	}
	if _, err := svc.AuthenticateWorkspaceUser(context.Background(), user.Email, "long-password", true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateWorkspaceUser(context.Background(), user.Email, "wrong-password", true); err == nil {
		t.Fatal("wrong password must be rejected")
	}
	_, resetToken, err := svc.BeginPasswordReset(context.Background(), user.Email)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompletePasswordReset(context.Background(), resetToken, "new-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompletePasswordReset(context.Background(), resetToken, "another-password"); !errors.Is(err, ErrResetTokenInvalid) {
		t.Fatalf("reused reset token error = %v", err)
	}
	if _, err := svc.AuthenticateWorkspaceUser(context.Background(), user.Email, "long-password", true); err == nil {
		t.Fatal("old password must be invalid")
	}
	if _, err := svc.AuthenticateWorkspaceUser(context.Background(), user.Email, "new-long-password", true); err != nil {
		t.Fatal(err)
	}
}

func TestInitialEmailVerificationStartsResendCooldown(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user, token, err := svc.RegisterWorkspaceUser(t.Context(), "cooldown@example.test", "long-password", "Cooldown", true)
	if err != nil {
		t.Fatal(err)
	}
	if user.EmailVerifySentAt == nil {
		t.Fatal("initial verification issue did not record its send time")
	}
	if _, _, err := svc.RenewEmailVerification(t.Context(), user.Email); !errors.Is(err, ErrEmailVerificationUnavailable) {
		t.Fatalf("immediate resend error = %v, want %v", err, ErrEmailVerificationUnavailable)
	}
	if err := svc.VerifyWorkspaceUserEmail(t.Context(), token); err != nil {
		t.Fatalf("initial verification token was invalidated by rejected resend: %v", err)
	}
}

func TestWorkspaceUserUpdatePreservesPendingCredentialsForSameEmail(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user, verificationToken, err := svc.RegisterWorkspaceUser(t.Context(), "pending@example.test", "long-password", "Pending", true)
	if err != nil {
		t.Fatal(err)
	}
	_, resetToken, err := svc.BeginPasswordReset(t.Context(), user.Email)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.UpdateWorkspaceUser(t.Context(), "admin", user.ID, WorkspaceUserRequest{
		Email: user.Email, DisplayName: "Updated", Status: WorkspaceUserStatusActive, Role: RoleDeveloper,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.EmailVerifySentAt == nil || updated.PasswordResetSentAt == nil {
		t.Fatalf("same-email update cleared credential cooldowns: %+v", updated)
	}
	if _, _, err := svc.RenewEmailVerification(t.Context(), user.Email); !errors.Is(err, ErrEmailVerificationUnavailable) {
		t.Fatalf("verification cooldown error = %v, want %v", err, ErrEmailVerificationUnavailable)
	}
	if _, _, err := svc.BeginPasswordReset(t.Context(), user.Email); !errors.Is(err, ErrPasswordResetUnavailable) {
		t.Fatalf("password reset cooldown error = %v, want %v", err, ErrPasswordResetUnavailable)
	}
	if err := svc.VerifyWorkspaceUserEmail(t.Context(), verificationToken); err != nil {
		t.Fatalf("same-email update invalidated verification token: %v", err)
	}
	if _, err := svc.CompletePasswordReset(t.Context(), resetToken, "another-long-password"); err != nil {
		t.Fatalf("same-email update invalidated reset token: %v", err)
	}
}

func TestWorkspaceUserEmailChangeRevokesOldEmailCredentials(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user, verificationToken, err := svc.RegisterWorkspaceUser(t.Context(), "old-address@example.test", "long-password", "Old", true)
	if err != nil {
		t.Fatal(err)
	}
	_, resetToken, err := svc.BeginPasswordReset(t.Context(), user.Email)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.UpdateWorkspaceUser(t.Context(), "admin", user.ID, WorkspaceUserRequest{
		Email: "new-address@example.test", DisplayName: user.DisplayName, Status: WorkspaceUserStatusActive, Role: RoleDeveloper,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.EmailVerified || updated.EmailVerifyHash != "" || updated.EmailVerifyExpiresAt != nil || updated.EmailVerifySentAt != nil ||
		updated.PasswordResetHash != "" || updated.PasswordResetExpiresAt != nil || updated.PasswordResetSentAt != nil {
		t.Fatalf("email change retained old-email credentials: %+v", updated)
	}
	if updated.SessionVersion != user.SessionVersion+1 {
		t.Fatalf("session version = %d, want %d", updated.SessionVersion, user.SessionVersion+1)
	}
	if err := svc.VerifyWorkspaceUserEmail(t.Context(), verificationToken); !errors.Is(err, ErrVerificationTokenInvalid) {
		t.Fatalf("old verification token error = %v, want %v", err, ErrVerificationTokenInvalid)
	}
	if _, err := svc.CompletePasswordReset(t.Context(), resetToken, "another-long-password"); !errors.Is(err, ErrResetTokenInvalid) {
		t.Fatalf("old reset token error = %v, want %v", err, ErrResetTokenInvalid)
	}
	if _, err := svc.AuthenticateWorkspaceUser(t.Context(), updated.Email, "long-password", true); !errors.Is(err, ErrInvalidWorkspaceCredentials) {
		t.Fatalf("unverified changed email login error = %v, want %v", err, ErrInvalidWorkspaceCredentials)
	}
}

func TestHistoricalMixedCaseEmailSupportsAuthenticationAndEmailWorkflows(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "secret")
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("long-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	stored := WorkspaceUser{
		ID: "usr_legacy_case", Email: "Legacy.User@Gmail.com", DisplayName: "Legacy",
		Status: WorkspaceUserStatusActive, Role: RoleDeveloper, PasswordHash: string(passwordHash),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.SaveWorkspaceUser(t.Context(), stored); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateWorkspaceUser(t.Context(), "legacy.user@gmail.com", "long-password", false); err != nil {
		t.Fatalf("AuthenticateWorkspaceUser(mixed-case legacy email): %v", err)
	}
	if _, err := svc.AuthenticateWorkspaceUser(t.Context(), "legacyuser@gmail.com", "long-password", false); err == nil {
		t.Fatal("Gmail dot alias unexpectedly became a login identifier")
	}

	_, verificationToken, err := svc.RenewEmailVerification(t.Context(), "legacy.user@gmail.com")
	if err != nil {
		t.Fatalf("RenewEmailVerification(mixed-case legacy email): %v", err)
	}
	if err := svc.VerifyWorkspaceUserEmail(t.Context(), verificationToken); err != nil {
		t.Fatalf("VerifyWorkspaceUserEmail(): %v", err)
	}
	_, resetToken, err := svc.BeginPasswordReset(t.Context(), "legacy.user@gmail.com")
	if err != nil {
		t.Fatalf("BeginPasswordReset(mixed-case legacy email): %v", err)
	}
	if _, err := svc.CompletePasswordReset(t.Context(), resetToken, "another-long-password"); err != nil {
		t.Fatalf("CompletePasswordReset(): %v", err)
	}
}

func TestAuthenticationTokensAreConsumedAtomically(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user, verificationToken, err := svc.RegisterWorkspaceUser(t.Context(), "atomic@example.test", "long-password", "Atomic", true)
	if err != nil {
		t.Fatal(err)
	}

	var verified atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if svc.VerifyWorkspaceUserEmail(t.Context(), verificationToken) == nil {
				verified.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := verified.Load(); got != 1 {
		t.Fatalf("verification successes = %d, want 1", got)
	}

	_, resetToken, err := svc.BeginPasswordReset(t.Context(), user.Email)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.BeginPasswordReset(t.Context(), user.Email); !errors.Is(err, ErrPasswordResetUnavailable) {
		t.Fatalf("password reset cooldown error = %v", err)
	}

	var reset atomic.Int32
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.CompletePasswordReset(t.Context(), resetToken, "new-long-password"); err == nil {
				reset.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := reset.Load(); got != 1 {
		t.Fatalf("password reset successes = %d, want 1", got)
	}
}

func TestWorkspaceRegistrationValidatesEmailAndBcryptLimit(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	for _, email := range []string{"not-an-email", "Name <user@example.test>", "user@@example.test", "@example.test"} {
		if _, _, err := svc.RegisterWorkspaceUser(t.Context(), email, "long-password", "Invalid", false); err == nil {
			t.Fatalf("RegisterWorkspaceUser(%q) accepted an invalid email", email)
		}
	}
	if _, _, err := svc.RegisterWorkspaceUser(t.Context(), "long-password@example.test", strings.Repeat("密", 25), "Invalid", false); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("RegisterWorkspaceUser() error = %v, want ErrPasswordTooLong", err)
	}
}

func TestConcurrentAliasRegistrationCreatesOneAccount(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	start := make(chan struct{})
	var successes atomic.Int32
	var wg sync.WaitGroup
	for _, email := range []string{"alias.user@gmail.com", "aliasuser+other@googlemail.com"} {
		wg.Add(1)
		go func(email string) {
			defer wg.Done()
			<-start
			if _, _, err := svc.RegisterWorkspaceUser(t.Context(), email, "sufficiently-long-password", "Alias", false); err == nil {
				successes.Add(1)
			}
		}(email)
	}
	close(start)
	wg.Wait()
	if got := successes.Load(); got != 1 {
		t.Fatalf("registration successes = %d, want 1", got)
	}
}

func TestRegisterWorkspaceUserAppliesDefaults(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	user, _, err := svc.RegisterWorkspaceUser(context.Background(), "defaults@example.test", "long-password", "Defaults", false, WorkspaceUserDefaults{BalanceMicros: 1200, ConcurrencyLimit: 8, RPMLimit: 90})
	if err != nil {
		t.Fatalf("RegisterWorkspaceUser() error = %v", err)
	}
	if user.BalanceMicros != 1200 || user.ConcurrencyLimit != 8 || user.RPMLimit != 90 {
		t.Fatalf("user defaults not applied: %+v", user)
	}
}
