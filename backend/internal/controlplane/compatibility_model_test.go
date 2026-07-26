package controlplane

import (
	"testing"
	"time"
)

func TestCompatibilityManifestSchemaAndSupportWindows(t *testing.T) {
	service := NewService(NewMemoryRepository(), "/v1")
	service.now = func() time.Time { return time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC) }

	manifest := service.CompatibilityManifest("0.18.0-test")
	if err := validateCompatibilityManifest(compatibilityManifestSource); err != nil {
		t.Fatalf("validateCompatibilityManifest(): %v", err)
	}
	if manifest.SchemaVersion != CompatibilitySchemaVersion || manifest.Revision != "2026-07-26.1" || manifest.RouterVersion != "0.18.0-test" {
		t.Fatalf("manifest identity=%+v", manifest)
	}
	if manifest.SupportWindowDays != 30 || len(manifest.Records) != 12 {
		t.Fatalf("support_window_days=%d records=%d", manifest.SupportWindowDays, len(manifest.Records))
	}
	for _, record := range manifest.Records {
		if record.RouterVersion != manifest.RouterVersion {
			t.Fatalf("record %s router_version=%q", record.ID, record.RouterVersion)
		}
		if record.VerificationStatus != CompatibilityStatusVerified {
			t.Fatalf("record %s verification_status=%q", record.ID, record.VerificationStatus)
		}
		if record.EvidenceLevel != CompatibilityEvidenceProtocolMock || record.Result != CompatibilityResultPassed {
			t.Fatalf("record %s evidence=%q result=%q", record.ID, record.EvidenceLevel, record.Result)
		}
		if got := record.ExpiresAt.Sub(record.TestedAt); got != 30*24*time.Hour {
			t.Fatalf("record %s support window=%s", record.ID, got)
		}
		if len(record.KnownLimitations) == 0 || record.KnownLimitations[0] != "official_client_runtime_not_executed" {
			t.Fatalf("record %s must disclose runtime limitation: %+v", record.ID, record.KnownLimitations)
		}
	}
}

func TestCompatibilityManifestCoversCurrentAndPreviousReleaseLines(t *testing.T) {
	expected := map[string]map[string]string{
		"codex/cli":                {CompatibilityReleaseCurrent: "0.145.0", CompatibilityReleasePrevious: "0.144.6"},
		"claude_code/cli":          {CompatibilityReleaseCurrent: "2.1.220", CompatibilityReleasePrevious: "1.0.128"},
		"openai_sdk/javascript":    {CompatibilityReleaseCurrent: "6.49.0", CompatibilityReleasePrevious: "5.23.2"},
		"openai_sdk/python":        {CompatibilityReleaseCurrent: "2.48.0", CompatibilityReleasePrevious: "1.109.1"},
		"anthropic_sdk/javascript": {CompatibilityReleaseCurrent: "0.115.0", CompatibilityReleasePrevious: "0.114.0"},
		"anthropic_sdk/python":     {CompatibilityReleaseCurrent: "0.120.0", CompatibilityReleasePrevious: "0.119.0"},
	}
	actual := make(map[string]map[string]string)
	for _, record := range compatibilityManifestSource.Records {
		key := record.Client + "/" + record.Language
		if actual[key] == nil {
			actual[key] = make(map[string]string)
		}
		if actual[key][record.ReleaseLine] != "" {
			t.Fatalf("duplicate release line %s/%s", key, record.ReleaseLine)
		}
		actual[key][record.ReleaseLine] = record.Version
	}
	if len(actual) != len(expected) {
		t.Fatalf("client/language groups=%d want=%d: %+v", len(actual), len(expected), actual)
	}
	for key, releases := range expected {
		for line, version := range releases {
			if actual[key][line] != version {
				t.Fatalf("%s %s version=%q want=%q", key, line, actual[key][line], version)
			}
		}
	}
}

func TestCompatibilityManifestExpiresEvidenceAndReturnsIndependentCopies(t *testing.T) {
	service := NewService(NewMemoryRepository(), "/v1")
	service.now = func() time.Time { return time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC) }

	first := service.CompatibilityManifest("")
	if first.RouterVersion != "unknown" {
		t.Fatalf("router_version=%q", first.RouterVersion)
	}
	for _, record := range first.Records {
		if record.VerificationStatus != CompatibilityStatusPendingConfirmation {
			t.Fatalf("expired record %s verification_status=%q", record.ID, record.VerificationStatus)
		}
	}
	first.Records[0].Capabilities[0] = "mutated"
	first.Records[0].KnownLimitations[0] = "mutated"
	second := service.CompatibilityManifest("test")
	if second.Records[0].Capabilities[0] == "mutated" || second.Records[0].KnownLimitations[0] == "mutated" {
		t.Fatal("CompatibilityManifest returned mutable embedded data")
	}
}
