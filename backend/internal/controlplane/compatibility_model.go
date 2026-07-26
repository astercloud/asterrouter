package controlplane

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	CompatibilitySchemaVersion = "asterrouter.compatibility.v1"

	CompatibilityReleaseCurrent  = "current"
	CompatibilityReleasePrevious = "previous"

	CompatibilityEvidenceSDKRuntime   = "sdk_runtime"
	CompatibilityEvidenceProtocolMock = "protocol_mock"
	CompatibilityEvidencePending      = "pending"

	CompatibilityResultPassed  = "passed"
	CompatibilityResultFailed  = "failed"
	CompatibilityResultPending = "pending"

	CompatibilityStatusVerified            = "verified"
	CompatibilityStatusPendingConfirmation = "pending_confirmation"
)

type CompatibilityManifest struct {
	SchemaVersion     string                `json:"schema_version"`
	Revision          string                `json:"revision"`
	RouterVersion     string                `json:"router_version"`
	GeneratedAt       time.Time             `json:"generated_at"`
	SupportWindowDays int                   `json:"support_window_days"`
	Records           []CompatibilityRecord `json:"records"`
}

type CompatibilityRecord struct {
	ID                 string    `json:"id"`
	Client             string    `json:"client"`
	Language           string    `json:"language"`
	Version            string    `json:"version"`
	ReleaseLine        string    `json:"release_line"`
	RouterVersion      string    `json:"router_version"`
	ProtocolVersion    string    `json:"protocol_version"`
	Suite              string    `json:"suite"`
	Result             string    `json:"result"`
	VerificationStatus string    `json:"verification_status"`
	Capabilities       []string  `json:"capabilities"`
	KnownLimitations   []string  `json:"known_limitations"`
	EvidenceLevel      string    `json:"evidence_level"`
	EvidenceReference  string    `json:"evidence_reference"`
	VersionSource      string    `json:"version_source"`
	TestedAt           time.Time `json:"tested_at,omitempty"`
	ExpiresAt          time.Time `json:"expires_at,omitempty"`
}

//go:embed compatibility_manifest.json
var compatibilityManifestJSON []byte

var compatibilityManifestSource = mustLoadCompatibilityManifest()

func mustLoadCompatibilityManifest() CompatibilityManifest {
	var manifest CompatibilityManifest
	if err := json.Unmarshal(compatibilityManifestJSON, &manifest); err != nil {
		panic(fmt.Sprintf("decode compatibility manifest: %v", err))
	}
	if err := validateCompatibilityManifest(manifest); err != nil {
		panic(fmt.Sprintf("validate compatibility manifest: %v", err))
	}
	return manifest
}

func validateCompatibilityManifest(manifest CompatibilityManifest) error {
	if manifest.SchemaVersion != CompatibilitySchemaVersion {
		return fmt.Errorf("schema_version must be %q", CompatibilitySchemaVersion)
	}
	if strings.TrimSpace(manifest.Revision) == "" || manifest.GeneratedAt.IsZero() {
		return fmt.Errorf("revision and generated_at are required")
	}
	if manifest.SupportWindowDays < 1 || manifest.SupportWindowDays > 365 {
		return fmt.Errorf("support_window_days must be between 1 and 365")
	}
	if len(manifest.Records) == 0 {
		return fmt.Errorf("records are required")
	}
	seen := make(map[string]struct{}, len(manifest.Records))
	for _, record := range manifest.Records {
		if strings.TrimSpace(record.ID) == "" {
			return fmt.Errorf("record id is required")
		}
		if _, found := seen[record.ID]; found {
			return fmt.Errorf("record id %q is duplicated", record.ID)
		}
		seen[record.ID] = struct{}{}
		if !validOnboardingClient(record.Client) {
			return fmt.Errorf("record %q has unsupported client %q", record.ID, record.Client)
		}
		if !oneOf(record.Language, "cli", "javascript", "python") {
			return fmt.Errorf("record %q has unsupported language %q", record.ID, record.Language)
		}
		if !validCompatibilityVersion(record.Version) {
			return fmt.Errorf("record %q has invalid version %q", record.ID, record.Version)
		}
		if !oneOf(record.ReleaseLine, CompatibilityReleaseCurrent, CompatibilityReleasePrevious) {
			return fmt.Errorf("record %q has unsupported release_line %q", record.ID, record.ReleaseLine)
		}
		if strings.TrimSpace(record.ProtocolVersion) == "" || strings.TrimSpace(record.Suite) == "" || len(record.Capabilities) == 0 {
			return fmt.Errorf("record %q is missing protocol, suite, or capabilities", record.ID)
		}
		if !oneOf(record.Result, CompatibilityResultPassed, CompatibilityResultFailed, CompatibilityResultPending) {
			return fmt.Errorf("record %q has unsupported result %q", record.ID, record.Result)
		}
		if !oneOf(record.EvidenceLevel, CompatibilityEvidenceSDKRuntime, CompatibilityEvidenceProtocolMock, CompatibilityEvidencePending) {
			return fmt.Errorf("record %q has unsupported evidence_level %q", record.ID, record.EvidenceLevel)
		}
		if strings.TrimSpace(record.VersionSource) == "" {
			return fmt.Errorf("record %q is missing version_source", record.ID)
		}
		if record.EvidenceLevel == CompatibilityEvidencePending {
			if record.Result != CompatibilityResultPending || !record.TestedAt.IsZero() {
				return fmt.Errorf("record %q has inconsistent pending evidence", record.ID)
			}
			continue
		}
		if record.Result == CompatibilityResultPending || record.TestedAt.IsZero() || strings.TrimSpace(record.EvidenceReference) == "" {
			return fmt.Errorf("record %q is missing executed evidence", record.ID)
		}
	}
	return nil
}

func validCompatibilityVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		if _, err := strconv.ParseUint(part, 10, 64); err != nil {
			return false
		}
	}
	return true
}

func (s *Service) CompatibilityManifest(routerVersion string) CompatibilityManifest {
	manifest := compatibilityManifestSource
	manifest.RouterVersion = strings.TrimSpace(routerVersion)
	if manifest.RouterVersion == "" {
		manifest.RouterVersion = "unknown"
	}
	manifest.Records = make([]CompatibilityRecord, len(compatibilityManifestSource.Records))
	now := s.nowUTC()
	window := time.Duration(manifest.SupportWindowDays) * 24 * time.Hour
	for index, source := range compatibilityManifestSource.Records {
		record := source
		record.RouterVersion = manifest.RouterVersion
		record.Capabilities = append([]string(nil), source.Capabilities...)
		record.KnownLimitations = append([]string(nil), source.KnownLimitations...)
		if !record.TestedAt.IsZero() {
			record.ExpiresAt = record.TestedAt.Add(window)
		}
		record.VerificationStatus = CompatibilityStatusPendingConfirmation
		executed := oneOf(record.EvidenceLevel, CompatibilityEvidenceSDKRuntime, CompatibilityEvidenceProtocolMock)
		if executed && record.Result == CompatibilityResultPassed && !record.ExpiresAt.IsZero() && now.Before(record.ExpiresAt) {
			record.VerificationStatus = CompatibilityStatusVerified
		}
		manifest.Records[index] = record
	}
	return manifest
}
