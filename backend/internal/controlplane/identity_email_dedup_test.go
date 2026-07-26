package controlplane

import (
	"context"
	"strings"
	"testing"
)

func TestRegisterWorkspaceUserRejectsEmailAliases(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	if _, _, err := svc.RegisterWorkspaceUser(ctx, "alias.user@gmail.com", "sufficiently-long-password", "First", false); err != nil {
		t.Fatalf("RegisterWorkspaceUser(): %v", err)
	}

	aliases := []string{
		"aliasuser@gmail.com",
		"alias.user+promo@gmail.com",
		"aliasuser@googlemail.com",
		"ALIAS.USER@GMAIL.COM",
		"a.l.i.a.s.u.s.e.r@gmail.com",
	}
	for _, alias := range aliases {
		_, _, err := svc.RegisterWorkspaceUser(ctx, alias, "sufficiently-long-password", "Dup", false)
		if err == nil {
			t.Fatalf("RegisterWorkspaceUser(%q) succeeded, want duplicate rejection", alias)
		}
		if !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("RegisterWorkspaceUser(%q) error = %v, want duplicate rejection", alias, err)
		}
	}
}

func TestRegisterWorkspaceUserAllowsDistinctInboxes(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	if _, _, err := svc.RegisterWorkspaceUser(ctx, "team.member@example.test", "sufficiently-long-password", "First", false); err != nil {
		t.Fatalf("RegisterWorkspaceUser(): %v", err)
	}
	// 非 Gmail 域名的点号是有意义的，去点后不得与已有账号冲突。
	if _, _, err := svc.RegisterWorkspaceUser(ctx, "teammember@example.test", "sufficiently-long-password", "Second", false); err != nil {
		t.Fatalf("RegisterWorkspaceUser() on distinct inbox: %v", err)
	}
	if _, _, err := svc.RegisterWorkspaceUser(ctx, "other.user@gmail.com", "sufficiently-long-password", "Third", false); err != nil {
		t.Fatalf("RegisterWorkspaceUser() on distinct gmail inbox: %v", err)
	}
}

func TestSaveWorkspaceUserPopulatesNormalizedEmail(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	if err := repo.SaveWorkspaceUser(ctx, WorkspaceUser{ID: "usr_1", Email: "Mixed.Case+tag@GoogleMail.com"}); err != nil {
		t.Fatalf("SaveWorkspaceUser(): %v", err)
	}
	users, err := repo.ListWorkspaceUsers(ctx)
	if err != nil {
		t.Fatalf("ListWorkspaceUsers(): %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("len(users) = %d, want 1", len(users))
	}
	if users[0].EmailNormalized != "mixedcase@gmail.com" {
		t.Fatalf("EmailNormalized = %q, want %q", users[0].EmailNormalized, "mixedcase@gmail.com")
	}
	if users[0].Email != "Mixed.Case+tag@GoogleMail.com" {
		t.Fatalf("Email was rewritten to %q, want the original value preserved", users[0].Email)
	}
}
