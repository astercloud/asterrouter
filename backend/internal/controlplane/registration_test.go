package controlplane

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

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
	if err := svc.VerifyWorkspaceUserEmail(context.Background(), token); err == nil {
		t.Fatal("verification token must be single use")
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
	if err := svc.CompletePasswordReset(context.Background(), resetToken, "new-long-password"); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompletePasswordReset(context.Background(), resetToken, "another-password"); err == nil {
		t.Fatal("reset token must be single use")
	}
	if _, err := svc.AuthenticateWorkspaceUser(context.Background(), user.Email, "long-password", true); err == nil {
		t.Fatal("old password must be invalid")
	}
	if _, err := svc.AuthenticateWorkspaceUser(context.Background(), user.Email, "new-long-password", true); err != nil {
		t.Fatal(err)
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
	if _, _, err := svc.BeginPasswordReset(t.Context(), user.Email); err == nil {
		t.Fatal("password reset cooldown was not enforced")
	}

	var reset atomic.Int32
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if svc.CompletePasswordReset(t.Context(), resetToken, "new-long-password") == nil {
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
