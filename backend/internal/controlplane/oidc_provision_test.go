package controlplane

import (
	"context"
	"testing"
)

func TestProvisionOIDCUserBindsStableIdentityAndDepartment(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	department, err := svc.CreateDepartment(context.Background(), "admin", DepartmentRequest{Name: "Engineering", Code: "eng", Status: DepartmentStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.ProvisionOIDCUser(context.Background(), "https://id.example.test", "subject-1", "User@Example.test", "User", "eng")
	if err != nil {
		t.Fatal(err)
	}
	if user.ExternalSubject != "subject-1" || user.Email != "user@example.test" || user.DepartmentID != department.ID || user.Role != RoleDeveloper {
		t.Fatalf("user = %+v", user)
	}
	again, err := svc.ProvisionOIDCUser(context.Background(), "https://id.example.test", "subject-1", "changed@example.test", "Changed", "")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != user.ID || again.Email != user.Email {
		t.Fatalf("stable identity was not reused: %+v", again)
	}
}

func TestProvisionOIDCUserRejectsDisabledAndConflictingIdentity(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user, err := svc.ProvisionOIDCUser(context.Background(), "https://id.example.test", "subject-1", "user@example.test", "User", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.UpdateWorkspaceUser(context.Background(), "admin", user.ID, WorkspaceUserRequest{Email: user.Email, DisplayName: user.DisplayName, Status: WorkspaceUserStatusDisabled, Role: user.Role})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ProvisionOIDCUser(context.Background(), "https://id.example.test", "subject-1", user.Email, "User", ""); err == nil {
		t.Fatal("disabled user should be rejected")
	}
	if _, err := svc.ProvisionOIDCUser(context.Background(), "https://other.example.test", "subject-2", user.Email, "User", ""); err == nil {
		t.Fatal("conflicting identity should be rejected")
	}
}
