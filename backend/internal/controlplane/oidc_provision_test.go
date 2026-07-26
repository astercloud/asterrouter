package controlplane

import (
	"context"
	"testing"
	"time"
)

func TestProvisionOIDCUserBindsStableIdentityAndDepartment(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	department, err := svc.CreateDepartment(context.Background(), "admin", DepartmentRequest{Name: "Engineering", Code: "eng", Status: DepartmentStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.ProvisionOIDCUser(context.Background(), "https://id.example.test", "subject-1", "User@Example.test", "User", "eng", false)
	if err != nil {
		t.Fatal(err)
	}
	if user.ExternalSubject != "subject-1" || user.Email != "user@example.test" || user.DepartmentID != department.ID || user.Role != RoleDeveloper {
		t.Fatalf("user = %+v", user)
	}
	profile, err := svc.CurrentAccountProfile(context.Background(), user.ID)
	if err != nil || len(profile.AuthIdentities) != 1 || profile.AuthIdentities[0].Subject != "subject-1" {
		t.Fatalf("profile identities=%+v err=%v", profile.AuthIdentities, err)
	}
	again, err := svc.ProvisionOIDCUser(context.Background(), "https://id.example.test", "subject-1", "changed@example.test", "Changed", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != user.ID || again.Email != user.Email || again.EmailVerified {
		t.Fatalf("stable identity was not reused: %+v", again)
	}
	verified, err := svc.ProvisionOIDCUser(context.Background(), "https://id.example.test", "subject-1", user.Email, "Changed", "", true)
	if err != nil || !verified.EmailVerified {
		t.Fatalf("matching verified provider email was not promoted: user=%+v err=%v", verified, err)
	}
}

func TestProvisionOIDCUserPreservesExplicitEmailTrust(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "secret")

	untrusted, err := svc.ProvisionOIDCUser(ctx, "https://id.example.test", "untrusted", "untrusted@example.test", "Untrusted", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if untrusted.EmailVerified {
		t.Fatal("untrusted external email was marked verified")
	}

	trusted, err := svc.ProvisionOIDCUser(ctx, "google", "trusted", "trusted@example.test", "Trusted", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !trusted.EmailVerified {
		t.Fatal("verified external email was not persisted")
	}
}

func TestProvisionOIDCUserLegacyIdentityUpgradeIsAtomic(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "secret")
	now := time.Now().UTC()
	legacy := WorkspaceUser{
		ID: "usr_legacy", Email: "legacy@example.test", Status: WorkspaceUserStatusActive, Role: RoleDeveloper,
		ExternalIssuer: "github", ExternalSubject: "legacy-subject", CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.SaveWorkspaceUser(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveAuthIdentity(ctx, AuthIdentity{ID: "aid_existing", UserID: legacy.ID, Issuer: "github", Subject: "different-subject", Email: legacy.Email, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ProvisionOIDCUser(ctx, "github", "legacy-subject", legacy.Email, "Legacy", "", true); err == nil {
		t.Fatal("legacy identity upgrade conflict must be rejected")
	}
	stored, err := svc.workspaceUserByID(ctx, legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.EmailVerified {
		t.Fatal("failed legacy identity upgrade changed email verification state")
	}
}

func TestSaveProvisionedWorkspaceUserIsAtomicInMemory(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	now := time.Now().UTC()
	if err := repo.SaveAuthIdentity(ctx, AuthIdentity{ID: "aid_existing", UserID: "usr_existing", Issuer: "github", Subject: "subject", Email: "existing@example.test", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	user := WorkspaceUser{ID: "usr_new", Email: "new@example.test", Status: WorkspaceUserStatusActive, Role: RoleDeveloper, CreatedAt: now, UpdatedAt: now}
	identity := AuthIdentity{ID: "aid_new", UserID: user.ID, Issuer: "github", Subject: "subject", Email: user.Email, CreatedAt: now, UpdatedAt: now}
	if err := repo.SaveProvisionedWorkspaceUser(ctx, user, identity); err == nil {
		t.Fatal("conflicting identity must reject the atomic provision")
	}
	users, err := repo.ListWorkspaceUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 0 {
		t.Fatalf("failed provision left a workspace user behind: %+v", users)
	}
}

func TestAuthIdentityRepositorySupportsMultipleProvidersButRejectsOwnershipConflicts(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	now := time.Now().UTC()
	first := AuthIdentity{ID: "aid_1", UserID: "user_1", Issuer: "github", Subject: "123", Email: "user@example.test", CreatedAt: now, UpdatedAt: now}
	second := AuthIdentity{ID: "aid_2", UserID: "user_1", Issuer: "google", Subject: "abc", Email: "user@example.test", CreatedAt: now, UpdatedAt: now}
	if err := repo.SaveAuthIdentity(ctx, first); err != nil {
		t.Fatalf("SaveAuthIdentity(first): %v", err)
	}
	if err := repo.SaveAuthIdentity(ctx, second); err != nil {
		t.Fatalf("SaveAuthIdentity(second): %v", err)
	}
	identities, err := repo.ListAuthIdentities(ctx, "user_1")
	if err != nil || len(identities) != 2 {
		t.Fatalf("identities=%+v err=%v", identities, err)
	}
	conflict := first
	conflict.ID = "aid_3"
	conflict.UserID = "user_2"
	if err := repo.SaveAuthIdentity(ctx, conflict); err == nil {
		t.Fatal("identity ownership conflict must be rejected")
	}
}

func TestUnbindCurrentAuthIdentityProtectsLastLoginMethod(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "secret")
	user, err := svc.ProvisionOIDCUser(ctx, "github", "subject-1", "external@example.test", "External", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.UnbindCurrentAuthIdentity(ctx, user.ID, "github"); err == nil {
		t.Fatal("last login method must not be removed")
	}
	now := time.Now().UTC()
	if err := repo.SaveAuthIdentity(ctx, AuthIdentity{ID: "aid_google", UserID: user.ID, Issuer: "google", Subject: "subject-2", Email: user.Email, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := svc.UnbindCurrentAuthIdentity(ctx, user.ID, "github"); err != nil {
		t.Fatalf("unbind with another login method: %v", err)
	}
	profile, err := svc.CurrentAccountProfile(ctx, user.ID)
	if err != nil || len(profile.AuthIdentities) != 1 || profile.AuthIdentities[0].Issuer != "google" {
		t.Fatalf("profile identities=%+v err=%v", profile.AuthIdentities, err)
	}
	logs, err := svc.ListAuditLogs(ctx, 20)
	if err != nil || len(logs) == 0 || logs[0].Action != "auth_identity_unbound" {
		t.Fatalf("audit logs=%+v err=%v", logs, err)
	}
}

func TestUnbindCurrentAuthIdentityWithPasswordDoesNotAffectOtherUsers(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "secret")
	user, _, err := svc.RegisterWorkspaceUser(ctx, "local@example.test", "long-password", "Local", false)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := svc.RegisterWorkspaceUser(ctx, "other@example.test", "long-password", "Other", false)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, identity := range []AuthIdentity{
		{ID: "aid_local", UserID: user.ID, Issuer: "github", Subject: "local-subject", CreatedAt: now, UpdatedAt: now},
		{ID: "aid_other", UserID: other.ID, Issuer: "github", Subject: "other-subject", CreatedAt: now, UpdatedAt: now},
	} {
		if err := repo.SaveAuthIdentity(ctx, identity); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.UnbindCurrentAuthIdentity(ctx, user.ID, "google"); err == nil {
		t.Fatal("unbound provider must be rejected")
	}
	if err := svc.UnbindCurrentAuthIdentity(ctx, user.ID, "github"); err != nil {
		t.Fatal(err)
	}
	identities, err := repo.ListAuthIdentities(ctx, other.ID)
	if err != nil || len(identities) != 1 || identities[0].ID != "aid_other" {
		t.Fatalf("other identities=%+v err=%v", identities, err)
	}
}

func TestBindCurrentAuthIdentityEnforcesOwnershipAndIssuerUniqueness(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "secret")
	first, _, err := svc.RegisterWorkspaceUser(ctx, "first@example.test", "long-password", "First", false)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := svc.RegisterWorkspaceUser(ctx, "second@example.test", "long-password", "Second", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.BindCurrentAuthIdentity(ctx, first.ID, "github", "github-1", first.Email, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.BindCurrentAuthIdentity(ctx, second.ID, "github", "github-1", second.Email, true); err == nil {
		t.Fatal("identity owned by another user must be rejected")
	}
	if err := svc.BindCurrentAuthIdentity(ctx, first.ID, "github", "github-2", first.Email, true); err == nil {
		t.Fatal("a second identity for the same issuer must be rejected")
	}
	profile, err := svc.CurrentAccountProfile(ctx, first.ID)
	if err != nil || len(profile.AuthIdentities) != 1 || profile.AuthIdentities[0].Subject != "github-1" {
		t.Fatalf("profile identities=%+v err=%v", profile.AuthIdentities, err)
	}
	logs, err := svc.ListAuditLogs(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, log := range logs {
		seen = seen || log.Action == "auth_identity_bound" && log.ResourceID == first.ID
	}
	if !seen {
		t.Fatalf("binding audit missing: %+v", logs)
	}
}

func TestBindCurrentAuthIdentityOnlyVerifiesMatchingTrustedEmail(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user, _, err := svc.RegisterWorkspaceUser(ctx, "local@example.test", "long-password", "Local", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.BindCurrentAuthIdentity(ctx, user.ID, "github", "different-email", "different@example.test", true); err != nil {
		t.Fatal(err)
	}
	profile, err := svc.CurrentAccountProfile(ctx, user.ID)
	if err != nil || profile.EmailVerified {
		t.Fatalf("different provider email changed verification state: profile=%+v err=%v", profile, err)
	}
	if err := svc.BindCurrentAuthIdentity(ctx, user.ID, "google", "matching-email", user.Email, true); err != nil {
		t.Fatal(err)
	}
	profile, err = svc.CurrentAccountProfile(ctx, user.ID)
	if err != nil || !profile.EmailVerified {
		t.Fatalf("matching verified provider email was not promoted: profile=%+v err=%v", profile, err)
	}
}

func TestBindCurrentAuthIdentityDoesNotVerifyEmailWhenIdentityBindingFails(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "secret")
	user, _, err := svc.RegisterWorkspaceUser(ctx, "local@example.test", "long-password", "Local", true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := repo.SaveAuthIdentity(ctx, AuthIdentity{ID: "aid_existing", UserID: user.ID, Issuer: "github", Subject: "existing", Email: user.Email, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := svc.BindCurrentAuthIdentity(ctx, user.ID, "github", "conflicting", user.Email, true); err == nil {
		t.Fatal("second identity for the same issuer must be rejected")
	}
	profile, err := svc.CurrentAccountProfile(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if profile.EmailVerified {
		t.Fatal("failed identity binding changed email verification state")
	}
}

func TestProvisionOIDCUserRejectsDisabledAndConflictingIdentity(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user, err := svc.ProvisionOIDCUser(context.Background(), "https://id.example.test", "subject-1", "user@example.test", "User", "", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.UpdateWorkspaceUser(context.Background(), "admin", user.ID, WorkspaceUserRequest{Email: user.Email, DisplayName: user.DisplayName, Status: WorkspaceUserStatusDisabled, Role: user.Role})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ProvisionOIDCUser(context.Background(), "https://id.example.test", "subject-1", user.Email, "User", "", false); err == nil {
		t.Fatal("disabled user should be rejected")
	}
	if _, err := svc.ProvisionOIDCUser(context.Background(), "https://other.example.test", "subject-2", user.Email, "User", "", false); err == nil {
		t.Fatal("conflicting identity should be rejected")
	}
}
