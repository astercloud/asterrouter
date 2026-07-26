package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestDemoLoginRequiresDemoMode(t *testing.T) {
	svc := NewService(Config{Username: "admin", Password: "secret", SecretKey: "test-secret"})

	if _, err := svc.Login(context.Background(), "demo", "demo"); err == nil {
		t.Fatal("Login() error = nil, want invalid credentials")
	}
}

func TestLocalLoginUsesPersistedPasswordHash(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("persisted-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword(): %v", err)
	}
	svc := NewService(Config{Username: "admin", Password: "bootstrap-password", PasswordHash: string(hash), SecretKey: "test-secret"})
	if _, err := svc.Login(context.Background(), "admin", "bootstrap-password"); err == nil {
		t.Fatal("bootstrap password remained valid after persisted hash was loaded")
	}
	if _, err := svc.Login(context.Background(), "admin", "persisted-password"); err != nil {
		t.Fatalf("Login(persisted password): %v", err)
	}
}

func TestDemoLoginIssuesDemoPrincipal(t *testing.T) {
	svc := NewService(Config{Username: "admin", Password: "secret", SecretKey: "test-secret", DemoMode: true})

	result, err := svc.Login(context.Background(), "demo", "demo")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.User.Username != "demo" || result.User.Role != "demo_admin" {
		t.Fatalf("demo user = %+v", result.User)
	}
	principal, ok := svc.Verify(result.AccessToken)
	if !ok {
		t.Fatal("Verify() ok = false")
	}
	if principal.Subject != "demo" || principal.Role != "demo_admin" {
		t.Fatalf("principal = %+v", principal)
	}
}

func TestMFAChallengeIsInvalidatedAfterFiveFailures(t *testing.T) {
	svc := NewService(Config{Username: "admin", Password: "secret", SecretKey: "test-secret"})
	challenge, _, err := svc.BeginMFA("user-1", "developer")
	if err != nil {
		t.Fatalf("BeginMFA(): %v", err)
	}
	for attempt := 1; attempt < maxMFAChallengeAttempts; attempt++ {
		if exhausted := svc.RecordMFAFailure(challenge); exhausted {
			t.Fatalf("challenge exhausted after %d failures", attempt)
		}
		if _, _, ok := svc.InspectMFA(challenge); !ok {
			t.Fatalf("challenge disappeared after %d failures", attempt)
		}
	}
	if exhausted := svc.RecordMFAFailure(challenge); !exhausted {
		t.Fatal("challenge remained valid after the maximum failure count")
	}
	if _, _, ok := svc.InspectMFA(challenge); ok {
		t.Fatal("exhausted challenge can still be inspected")
	}
}

func TestMFAChallengesAreCapacityBoundAndExpiredEntriesAreReclaimed(t *testing.T) {
	svc := NewService(Config{Username: "admin", Password: "secret", SecretKey: "test-secret"})
	svc.maxMFAChallenges = 2

	first, _, err := svc.BeginMFA("user-1", "developer")
	if err != nil {
		t.Fatalf("BeginMFA(first): %v", err)
	}
	if _, _, err := svc.BeginMFA("user-2", "developer"); err != nil {
		t.Fatalf("BeginMFA(second): %v", err)
	}
	if _, _, err := svc.BeginMFA("user-3", "developer"); !errors.Is(err, ErrMFAChallengeCapacity) {
		t.Fatalf("BeginMFA(over capacity) error = %v, want %v", err, ErrMFAChallengeCapacity)
	}

	if _, _, ok := svc.ConsumeMFA(first); !ok {
		t.Fatal("ConsumeMFA(first) failed")
	}
	if _, _, err := svc.BeginMFA("user-3", "developer"); err != nil {
		t.Fatalf("BeginMFA(after consume): %v", err)
	}

	svc.mfaMu.Lock()
	for key, challenge := range svc.mfaChallenges {
		challenge.ExpiresAt = time.Now().UTC().Add(-time.Second)
		svc.mfaChallenges[key] = challenge
	}
	svc.mfaMu.Unlock()
	if _, _, err := svc.BeginMFA("user-4", "developer"); err != nil {
		t.Fatalf("BeginMFA(after expiry): %v", err)
	}
}

func TestSessionVersionResolverFailsClosed(t *testing.T) {
	service := NewService(Config{Username: "admin", Password: "secret", SecretKey: "test-secret"})
	available := true
	service.SetSessionVersionResolver(func(subject string) (int64, bool, error) {
		return 7, available && subject == "user-1", nil
	})

	result, err := service.LoginOIDC("user-1", "developer")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := service.Verify(result.AccessToken); !ok {
		t.Fatal("session with a confirmed version was rejected")
	}
	available = false
	if _, ok := service.Verify(result.AccessToken); ok {
		t.Fatal("session remained valid when its version could not be resolved")
	}
	if _, err := service.LoginOIDC("user-1", "developer"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("session issued while resolver was unavailable: %v", err)
	}
	service.SetSessionVersionResolver(func(string) (int64, bool, error) {
		return 0, false, errors.New("database unavailable")
	})
	if _, err := service.LoginOIDC("user-1", "developer"); !errors.Is(err, ErrSessionStateUnavailable) {
		t.Fatalf("resolver failure error = %v", err)
	}
}
