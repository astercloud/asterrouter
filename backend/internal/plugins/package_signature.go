package plugins

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	packageEnvelopePurpose = "plugin_package"
	packagePayloadSchema   = "astercloud.plugin-package.v1"
)

type packageSignaturePayload struct {
	SchemaVersion string `json:"schema_version"`
	Plugin        string `json:"plugin"`
	Version       string `json:"version"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	SHA256        string `json:"sha256"`
	SizeBytes     int64  `json:"size_bytes"`
	URI           string `json:"uri"`
}

type packageDownloadGrant struct {
	ID              string            `json:"id"`
	PublicID        string            `json:"public_id"`
	PackageID       string            `json:"package_id"`
	PackagePublicID string            `json:"package_public_id"`
	DownloadURL     string            `json:"download_url"`
	Headers         map[string]string `json:"headers"`
	SHA256          string            `json:"sha256"`
	Signature       catalogEnvelope   `json:"signature"`
	ExpiresAt       time.Time         `json:"expires_at"`
	CreatedAt       time.Time         `json:"created_at"`
}

func verifyPackageDownloadGrant(grant packageDownloadGrant, record packageRecord, cfg OfficialCatalogConfig, now time.Time) error {
	if grant.PackagePublicID != "" && grant.PackagePublicID != record.PackageID {
		return ErrPackageSignature
	}
	if strings.ToLower(strings.TrimSpace(grant.SHA256)) != record.SHA256 {
		return ErrPackageSignature
	}
	if !grant.ExpiresAt.IsZero() && !grant.ExpiresAt.After(now) {
		return ErrPackageSignature
	}
	return verifyPackageEnvelope(grant.Signature, record, cfg, now)
}

func verifyPackageSignature(signatureJSON string, record packageRecord, cfg OfficialCatalogConfig, now time.Time) error {
	var envelope catalogEnvelope
	if err := json.Unmarshal([]byte(signatureJSON), &envelope); err != nil {
		return ErrPackageSignature
	}
	return verifyPackageEnvelope(envelope, record, cfg, now)
}

func verifyPackageEnvelope(envelope catalogEnvelope, record packageRecord, cfg OfficialCatalogConfig, now time.Time) error {
	if err := verifySignedEnvelope(envelope, cfg, packageEnvelopePurpose, now); err != nil {
		return ErrPackageSignature
	}
	var payload packageSignaturePayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return ErrPackageSignature
	}
	if payload.SchemaVersion != packagePayloadSchema ||
		payload.Plugin != record.PluginSlug ||
		payload.Version != record.Version ||
		strings.ToLower(payload.OS) != record.OS ||
		strings.ToLower(payload.Arch) != record.Arch ||
		strings.ToLower(payload.SHA256) != record.SHA256 ||
		payload.SizeBytes != record.SizeBytes ||
		strings.TrimSpace(payload.URI) == "" ||
		strings.TrimSpace(record.PackageURI) != "" && strings.TrimSpace(payload.URI) != strings.TrimSpace(record.PackageURI) {
		return ErrPackageSignature
	}
	return nil
}
