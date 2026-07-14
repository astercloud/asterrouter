package controlplane

import (
	"context"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
)

func TestTOTPEnrollmentAndDisable(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user, err := svc.CreateWorkspaceUser(context.Background(), "admin", WorkspaceUserRequest{Email: "user@example.test", Role: RoleDeveloper, Status: WorkspaceUserStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	setup, err := svc.BeginTOTPSetup(context.Background(), user.ID)
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
	recovery, err := svc.GenerateTOTPRecoveryCodes(context.Background(), user.ID)
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
