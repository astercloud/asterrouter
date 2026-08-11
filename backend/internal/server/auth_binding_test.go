package server

import (
	"errors"
	"testing"
	"time"
)

func TestAuthBindingStoreIsSingleUseProviderBoundAndExpiring(t *testing.T) {
	now := time.Now().UTC()
	store := newAuthBindingStore()
	if err := store.Save("state-1", "user-1", "github", "/console/account", now); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Consume("state-1", "google", now); ok {
		t.Fatal("transaction must be bound to its provider")
	}
	if _, ok := store.Consume("state-1", "github", now); ok {
		t.Fatal("failed provider match must still consume the transaction")
	}
	if err := store.Save("state-2", "user-2", "oidc", "/portal/account", now); err != nil {
		t.Fatal(err)
	}
	transaction, ok := store.Consume("state-2", "oidc", now.Add(time.Minute))
	if !ok || transaction.UserID != "user-2" || transaction.ReturnPath != "/portal/account" {
		t.Fatalf("transaction=%+v ok=%v", transaction, ok)
	}
	if _, ok := store.Consume("state-2", "oidc", now.Add(time.Minute)); ok {
		t.Fatal("transaction must be single use")
	}
	if err := store.Save("state-3", "user-3", "oidc", "https://evil.example/account", now); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Consume("state-3", "oidc", now.Add(11*time.Minute)); ok {
		t.Fatal("expired transaction must be rejected")
	}
	if got := authBindingRedirect(authBindingTransaction{ReturnPath: "//evil.example"}, "error", "", "failed"); got != "/console/account?binding=error&message=failed" {
		t.Fatalf("unsafe redirect = %q", got)
	}
}

func TestAuthBindingStoreIsCapacityBoundAndReclaimsEntries(t *testing.T) {
	now := time.Now().UTC()
	store := newAuthBindingStore()
	store.maxEntries = 2

	if err := store.Save("state-1", "user-1", "github", "", now); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("state-2", "user-2", "github", "", now); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("state-3", "user-3", "github", "", now); !errors.Is(err, errAuthBindingCapacity) {
		t.Fatalf("Save(over capacity) error = %v, want %v", err, errAuthBindingCapacity)
	}

	if _, ok := store.Consume("state-1", "github", now); !ok {
		t.Fatal("Consume(state-1) failed")
	}
	if err := store.Save("state-3", "user-3", "github", "", now); err != nil {
		t.Fatalf("Save(after consume): %v", err)
	}
	if err := store.Save("state-4", "user-4", "github", "", now.Add(11*time.Minute)); err != nil {
		t.Fatalf("Save(after expiry): %v", err)
	}
}
