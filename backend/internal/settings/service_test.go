package settings

import (
	"context"
	"testing"
)

func TestServiceDefaults(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
	got, err := svc.Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin() error = %v", err)
	}
	if got.SiteName != "AsterRouter" {
		t.Fatalf("SiteName = %q", got.SiteName)
	}
	if got.DefaultLocale != "en-US" {
		t.Fatalf("DefaultLocale = %q", got.DefaultLocale)
	}
	if got.GatewayBasePath != "/v1" {
		t.Fatalf("GatewayBasePath = %q", got.GatewayBasePath)
	}
}

func TestApplyProfiles(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
	got, err := svc.ApplyProfiles(context.Background(), []string{"enterprise", "personal"}, "personal")
	if err != nil {
		t.Fatalf("ApplyProfiles() error = %v", err)
	}
	if !got.SetupCompleted || got.DefaultProfile != "personal" || len(got.EnabledProfiles) != 2 {
		t.Fatalf("profiles not applied: %+v", got)
	}
}

func TestDemoModeCompletesSetupWithAllProfiles(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory", DemoMode: true})
	got, err := svc.Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin() error = %v", err)
	}
	if !got.SetupCompleted || !got.DemoMode || got.DefaultProfile != "personal" {
		t.Fatalf("demo settings not applied: %+v", got.PublicSettings)
	}
	if len(got.EnabledProfiles) != 3 {
		t.Fatalf("EnabledProfiles = %+v", got.EnabledProfiles)
	}
}

func TestDemoModeDoesNotOverrideConfiguredProfiles(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{
		Version:         "test",
		StorageMode:     "memory",
		DemoMode:        true,
		EnabledProfiles: []string{"enterprise"},
		DefaultProfile:  "enterprise",
	})
	got, err := svc.Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin() error = %v", err)
	}
	if got.DefaultProfile != "enterprise" || len(got.EnabledProfiles) != 1 || got.EnabledProfiles[0] != "enterprise" {
		t.Fatalf("configured profiles overridden: %+v", got.PublicSettings)
	}
}

func TestUpdateValidatesLocale(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
	_, err := svc.Update(context.Background(), AdminSettings{
		PublicSettings: PublicSettings{
			SiteName:          "AsterRouter",
			DefaultLocale:     "ja-JP",
			EnabledLocales:    []string{"en-US"},
			GatewayBasePath:   "/v1",
			ServiceCenterMode: "disabled",
		},
		DataRetentionDays: 30,
		PromptLoggingMode: "metadata_only",
		UpdateChannel:     "stable",
	})
	if err == nil {
		t.Fatal("Update() error = nil, want validation error")
	}
}

func TestValidateLegalDocumentsRejectsDuplicateSlug(t *testing.T) {
	err := validateLegalDocuments([]LegalDocument{
		{ID: "terms", Name: "Terms", Slug: "terms", Content: "one"},
		{ID: "privacy", Name: "Privacy", Slug: "terms", Content: "two"},
	}, true)
	if err == nil {
		t.Fatal("validateLegalDocuments() error = nil, want duplicate slug error")
	}
}

func TestParseIntListFallsBackOnInvalidJSON(t *testing.T) {
	fallback := []int{10, 20, 50}
	got := parseIntList("invalid", fallback)
	if len(got) != len(fallback) || got[1] != 20 {
		t.Fatalf("parseIntList() = %v, want %v", got, fallback)
	}
}
