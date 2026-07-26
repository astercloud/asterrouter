package controlplane

import (
	"context"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestWorkspaceUserLookupByEmail(t *testing.T) {
	ctx := context.Background()
	repos := []struct {
		name string
		repo Repository
	}{
		{"memory", NewMemoryRepository()},
	}
	for _, tc := range repos {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			user1 := WorkspaceUser{ID: "usr_1", Email: "found@example.test", CreatedAt: now, UpdatedAt: now}
			user2 := WorkspaceUser{ID: "usr_2", Email: "other@example.test", CreatedAt: now, UpdatedAt: now}
			if err := tc.repo.SaveWorkspaceUser(ctx, user1); err != nil {
				t.Fatalf("SaveWorkspaceUser(user1): %v", err)
			}
			if err := tc.repo.SaveWorkspaceUser(ctx, user2); err != nil {
				t.Fatalf("SaveWorkspaceUser(user2): %v", err)
			}
			found, ok, err := tc.repo.FindWorkspaceUserByEmail(ctx, "found@example.test")
			if err != nil {
				t.Fatalf("FindWorkspaceUserByEmail(): %v", err)
			}
			if !ok {
				t.Fatal("FindWorkspaceUserByEmail() returned ok=false, want found")
			}
			if found.ID != "usr_1" {
				t.Fatalf("found.ID = %q, want %q", found.ID, "usr_1")
			}
			found, ok, err = tc.repo.FindWorkspaceUserByEmailNormalized(ctx, NormalizeEmailForAliasDedup("found+tag@example.test"))
			if err != nil || !ok || found.ID != "usr_1" {
				t.Fatalf("normalized lookup user=%+v ok=%t err=%v", found, ok, err)
			}
			_, ok, err = tc.repo.FindWorkspaceUserByEmail(ctx, "missing@example.test")
			if err != nil {
				t.Fatalf("FindWorkspaceUserByEmail(missing): %v", err)
			}
			if ok {
				t.Fatal("FindWorkspaceUserByEmail(missing) returned ok=true, want not found")
			}
		})
	}
}

func TestWorkspaceUserLookupByTokenHash(t *testing.T) {
	ctx := context.Background()
	repos := []struct {
		name string
		repo Repository
	}{
		{"memory", NewMemoryRepository()},
	}
	for _, tc := range repos {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			expires := now.Add(30 * time.Minute)
			user1 := WorkspaceUser{ID: "usr_1", Email: "verify@example.test", EmailVerifyHash: "hash_verify", EmailVerifyExpiresAt: &expires, CreatedAt: now, UpdatedAt: now}
			user2 := WorkspaceUser{ID: "usr_2", Email: "reset@example.test", PasswordResetHash: "hash_reset", PasswordResetExpiresAt: &expires, CreatedAt: now, UpdatedAt: now}
			if err := tc.repo.SaveWorkspaceUser(ctx, user1); err != nil {
				t.Fatalf("SaveWorkspaceUser(user1): %v", err)
			}
			if err := tc.repo.SaveWorkspaceUser(ctx, user2); err != nil {
				t.Fatalf("SaveWorkspaceUser(user2): %v", err)
			}
			found, ok, err := tc.repo.FindWorkspaceUserByEmailVerifyHash(ctx, "hash_verify")
			if err != nil {
				t.Fatalf("FindWorkspaceUserByEmailVerifyHash(): %v", err)
			}
			if !ok {
				t.Fatal("FindWorkspaceUserByEmailVerifyHash() returned ok=false, want found")
			}
			if found.ID != "usr_1" {
				t.Fatalf("found.ID = %q, want %q", found.ID, "usr_1")
			}
			found, ok, err = tc.repo.FindWorkspaceUserByPasswordResetHash(ctx, "hash_reset")
			if err != nil {
				t.Fatalf("FindWorkspaceUserByPasswordResetHash(): %v", err)
			}
			if !ok {
				t.Fatal("FindWorkspaceUserByPasswordResetHash() returned ok=false, want found")
			}
			if found.ID != "usr_2" {
				t.Fatalf("found.ID = %q, want %q", found.ID, "usr_2")
			}
			_, ok, err = tc.repo.FindWorkspaceUserByEmailVerifyHash(ctx, "missing")
			if err != nil {
				t.Fatalf("FindWorkspaceUserByEmailVerifyHash(missing): %v", err)
			}
			if ok {
				t.Fatal("FindWorkspaceUserByEmailVerifyHash(missing) returned ok=true, want not found")
			}
		})
	}
}

func TestPostgresWorkspaceUserLookups(t *testing.T) {
	ctx := context.Background()
	schema := testutil.NewPostgresSchema(t)
	repo, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("NewPostgresRepository(): %v", err)
	}
	defer repo.Close()
	now := time.Now().UTC()
	expires := now.Add(30 * time.Minute)
	user := WorkspaceUser{
		ID:                     "usr_postgres_lookup",
		Email:                  "lookup@example.test",
		EmailVerifyHash:        "hash_verify_postgres",
		EmailVerifyExpiresAt:   &expires,
		PasswordResetHash:      "hash_reset_postgres",
		PasswordResetExpiresAt: &expires,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if err := repo.SaveWorkspaceUser(ctx, user); err != nil {
		t.Fatalf("SaveWorkspaceUser(): %v", err)
	}
	for name, lookup := range map[string]func() (WorkspaceUser, bool, error){
		"email": func() (WorkspaceUser, bool, error) {
			return repo.FindWorkspaceUserByEmail(ctx, user.Email)
		},
		"normalized email": func() (WorkspaceUser, bool, error) {
			return repo.FindWorkspaceUserByEmailNormalized(ctx, NormalizeEmailForAliasDedup(user.Email))
		},
		"email verification hash": func() (WorkspaceUser, bool, error) {
			return repo.FindWorkspaceUserByEmailVerifyHash(ctx, user.EmailVerifyHash)
		},
		"password reset hash": func() (WorkspaceUser, bool, error) {
			return repo.FindWorkspaceUserByPasswordResetHash(ctx, user.PasswordResetHash)
		},
	} {
		t.Run(name, func(t *testing.T) {
			found, ok, err := lookup()
			if err != nil || !ok || found.ID != user.ID {
				t.Fatalf("lookup user=%+v ok=%t err=%v", found, ok, err)
			}
		})
	}
	if _, ok, err := repo.FindWorkspaceUserByEmail(ctx, "missing@example.test"); err != nil || ok {
		t.Fatalf("missing email lookup ok=%t err=%v", ok, err)
	}
}
