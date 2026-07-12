package auth

import (
	"context"
	"testing"
)

func TestDemoLoginRequiresDemoMode(t *testing.T) {
	svc := NewService(Config{Username: "admin", Password: "secret", SecretKey: "test-secret"})

	if _, err := svc.Login(context.Background(), "demo", "demo"); err == nil {
		t.Fatal("Login() error = nil, want invalid credentials")
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
