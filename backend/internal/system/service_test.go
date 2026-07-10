package system

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCheckUpdateWithoutManifestReturnsManualWarning(t *testing.T) {
	svc := NewService(Config{Version: "0.1.0", BuildType: "source"})

	info, err := svc.CheckUpdate(context.Background(), false, "stable")
	if err != nil {
		t.Fatalf("CheckUpdate(): %v", err)
	}

	if info.CurrentVersion != "0.1.0" || info.LatestVersion != "0.1.0" {
		t.Fatalf("unexpected versions: %+v", info)
	}
	if info.ManifestConfigured {
		t.Fatalf("manifest should not be configured: %+v", info)
	}
	if info.UpdateSupported || info.Warning == "" {
		t.Fatalf("expected manual warning without update support: %+v", info)
	}
}

func TestCheckUpdateSelectsCompatibleManifestAsset(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := manifestFile{
			Releases: []manifestRelease{
				{
					Version: "0.2.0",
					Channel: "stable",
					Name:    "0.2.0",
					Assets: []Asset{
						{
							Name:   "asterrouter",
							URL:    serverURL + "/asterrouter",
							OS:     runtime.GOOS,
							Arch:   runtime.GOARCH,
							SHA256: "abc",
							Size:   123,
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()
	serverURL = srv.URL

	svc := NewService(Config{Version: "0.1.0", BuildType: "release", ManifestURL: srv.URL})

	info, err := svc.CheckUpdate(context.Background(), true, "stable")
	if err != nil {
		t.Fatalf("CheckUpdate(): %v", err)
	}

	if !info.HasUpdate || !info.UpdateSupported {
		t.Fatalf("expected supported update: %+v", info)
	}
	if info.ReleaseInfo == nil || info.ReleaseInfo.Asset == nil || info.ReleaseInfo.Asset.URL == "" {
		t.Fatalf("compatible asset not selected: %+v", info)
	}
}

func TestCheckUpdateSelectsSignedOfficialCoreRelease(t *testing.T) {
	keyID, publicKey, privateKey := newOfficialTestKey(t)
	now := time.Now().UTC()
	checksum := officialTestChecksum("asterrouter 0.2.0")
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corePayload := officialCoreReleasePayload{
			SchemaVersion:       officialCoreReleaseSchema,
			Version:             "0.2.0",
			Channel:             "stable",
			SHA256:              checksum,
			SizeBytes:           123,
			URI:                 serverURL + "/core/0.2.0/asterrouter",
			MinSupportedVersion: "0.1.0",
		}
		coreEnvelope := signOfficialTestEnvelope(t, keyID, privateKey, officialCoreReleasePurpose, corePayload, now, nil)
		publishedAt := now
		index := officialCatalogIndex{
			SchemaVersion:  officialCatalogIndexSchema,
			CatalogVersion: 1,
			GeneratedAt:    now,
			CoreReleases: []officialCoreReleaseIndex{
				{
					PublicID:            "core_020",
					Version:             corePayload.Version,
					Channel:             corePayload.Channel,
					SHA256:              corePayload.SHA256,
					SizeBytes:           corePayload.SizeBytes,
					MinSupportedVersion: corePayload.MinSupportedVersion,
					PublishedAt:         &publishedAt,
					Signature:           coreEnvelope,
				},
			},
		}
		catalogEnvelope := signOfficialTestEnvelope(t, keyID, privateKey, officialCatalogPurpose, index, now, ptrTime(now.Add(time.Hour)))
		_ = json.NewEncoder(w).Encode(catalogEnvelope)
	}))
	defer srv.Close()
	serverURL = srv.URL

	svc := NewService(Config{
		Version:            "0.1.0",
		BuildType:          "release",
		OfficialCatalogURL: srv.URL,
		OfficialKeyID:      keyID,
		OfficialPublicKey:  publicKey,
	})

	info, err := svc.CheckUpdate(context.Background(), true, "stable")
	if err != nil {
		t.Fatalf("CheckUpdate(): %v", err)
	}

	if !info.HasUpdate || !info.UpdateSupported || !info.SignedMetadata || info.Source != updateSourceOfficialCatalog {
		t.Fatalf("expected signed official update: %+v", info)
	}
	if info.ReleaseInfo == nil || info.ReleaseInfo.Asset == nil {
		t.Fatalf("expected selected signed asset: %+v", info)
	}
	if info.ReleaseInfo.Asset.URL != serverURL+"/core/0.2.0/asterrouter" || info.ReleaseInfo.Asset.SHA256 != checksum || info.ReleaseInfo.Asset.Size != 123 {
		t.Fatalf("unexpected signed asset: %+v", info.ReleaseInfo.Asset)
	}
}

func TestCheckUpdateRejectsTamperedOfficialCoreRelease(t *testing.T) {
	keyID, publicKey, privateKey := newOfficialTestKey(t)
	now := time.Now().UTC()
	checksum := officialTestChecksum("asterrouter 0.2.0")
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corePayload := officialCoreReleasePayload{
			SchemaVersion: officialCoreReleaseSchema,
			Version:       "0.2.0",
			Channel:       "stable",
			SHA256:        checksum,
			SizeBytes:     123,
			URI:           serverURL + "/core/0.2.0/asterrouter",
		}
		coreEnvelope := signOfficialTestEnvelope(t, keyID, privateKey, officialCoreReleasePurpose, corePayload, now, nil)
		tamperedPayload := corePayload
		tamperedPayload.SHA256 = strings.Repeat("b", 64)
		rawPayload, err := json.Marshal(tamperedPayload)
		if err != nil {
			t.Fatalf("marshal tampered payload: %v", err)
		}
		coreEnvelope.Payload = rawPayload
		index := officialCatalogIndex{
			SchemaVersion:  officialCatalogIndexSchema,
			CatalogVersion: 1,
			GeneratedAt:    now,
			CoreReleases: []officialCoreReleaseIndex{
				{
					PublicID:  "core_020",
					Version:   tamperedPayload.Version,
					Channel:   tamperedPayload.Channel,
					SHA256:    tamperedPayload.SHA256,
					SizeBytes: tamperedPayload.SizeBytes,
					Signature: coreEnvelope,
				},
			},
		}
		catalogEnvelope := signOfficialTestEnvelope(t, keyID, privateKey, officialCatalogPurpose, index, now, ptrTime(now.Add(time.Hour)))
		_ = json.NewEncoder(w).Encode(catalogEnvelope)
	}))
	defer srv.Close()
	serverURL = srv.URL

	svc := NewService(Config{
		Version:            "0.1.0",
		BuildType:          "release",
		OfficialCatalogURL: srv.URL,
		OfficialKeyID:      keyID,
		OfficialPublicKey:  publicKey,
	})

	info, err := svc.CheckUpdate(context.Background(), true, "stable")
	if err != nil {
		t.Fatalf("CheckUpdate(): %v", err)
	}
	if info.HasUpdate || !info.SignedMetadata || !strings.Contains(info.Warning, ErrUpdateSignature.Error()) {
		t.Fatalf("expected signature warning without update: %+v", info)
	}
	if _, err := svc.PerformUpdate(context.Background(), "stable", "op1"); !errors.Is(err, ErrUpdateSignature) {
		t.Fatalf("PerformUpdate() err = %v, want ErrUpdateSignature", err)
	}
}

func TestCheckUpdateSkipsOfficialCoreReleaseAboveMinimumVersion(t *testing.T) {
	keyID, publicKey, privateKey := newOfficialTestKey(t)
	now := time.Now().UTC()
	checksum := officialTestChecksum("asterrouter 1.0.0")
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corePayload := officialCoreReleasePayload{
			SchemaVersion:       officialCoreReleaseSchema,
			Version:             "1.0.0",
			Channel:             "stable",
			SHA256:              checksum,
			SizeBytes:           456,
			URI:                 serverURL + "/core/1.0.0/asterrouter",
			MinSupportedVersion: "0.9.0",
		}
		coreEnvelope := signOfficialTestEnvelope(t, keyID, privateKey, officialCoreReleasePurpose, corePayload, now, nil)
		index := officialCatalogIndex{
			SchemaVersion:  officialCatalogIndexSchema,
			CatalogVersion: 1,
			GeneratedAt:    now,
			CoreReleases: []officialCoreReleaseIndex{
				{
					PublicID:            "core_100",
					Version:             corePayload.Version,
					Channel:             corePayload.Channel,
					SHA256:              corePayload.SHA256,
					SizeBytes:           corePayload.SizeBytes,
					MinSupportedVersion: corePayload.MinSupportedVersion,
					Signature:           coreEnvelope,
				},
			},
		}
		catalogEnvelope := signOfficialTestEnvelope(t, keyID, privateKey, officialCatalogPurpose, index, now, ptrTime(now.Add(time.Hour)))
		_ = json.NewEncoder(w).Encode(catalogEnvelope)
	}))
	defer srv.Close()
	serverURL = srv.URL

	svc := NewService(Config{
		Version:            "0.1.0",
		BuildType:          "release",
		OfficialCatalogURL: srv.URL,
		OfficialKeyID:      keyID,
		OfficialPublicKey:  publicKey,
	})

	info, err := svc.CheckUpdate(context.Background(), true, "stable")
	if err != nil {
		t.Fatalf("CheckUpdate(): %v", err)
	}

	if info.HasUpdate || info.LatestVersion != "0.1.0" || !info.SignedMetadata {
		t.Fatalf("expected no compatible signed release: %+v", info)
	}
}

func TestPerformUpdateSourceBuildRequiresManualUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(manifestFile{
			Version: "0.2.0",
			Assets: []Asset{
				{OS: runtime.GOOS, Arch: runtime.GOARCH, URL: "https://example.com/asterrouter", SHA256: "abc"},
			},
		})
	}))
	defer srv.Close()

	svc := NewService(Config{Version: "0.1.0", BuildType: "source", ManifestURL: srv.URL})

	result, err := svc.PerformUpdate(context.Background(), "stable", "op1")
	if !errors.Is(err, ErrUpdateUnsupported) {
		t.Fatalf("err = %v, want ErrUpdateUnsupported", err)
	}
	if result.ManualAction == "" || result.OperationID != "op1" {
		t.Fatalf("manual result incomplete: %+v", result)
	}
}

func newOfficialTestKey(t *testing.T) (string, string, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return "test-key", base64.RawURLEncoding.EncodeToString(publicKey), privateKey
}

func signOfficialTestEnvelope(t *testing.T, keyID string, privateKey ed25519.PrivateKey, purpose string, payload any, issuedAt time.Time, expiresAt *time.Time) officialEnvelope {
	t.Helper()
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	envelope := officialEnvelope{
		SchemaVersion: officialEnvelopeSchema,
		Purpose:       purpose,
		KeyID:         keyID,
		Algorithm:     "Ed25519",
		IssuedAt:      issuedAt.Format(time.RFC3339Nano),
		Payload:       rawPayload,
	}
	if expiresAt != nil {
		envelope.ExpiresAt = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	message, err := officialEnvelopeSigningMessage(envelope)
	if err != nil {
		t.Fatalf("signing message: %v", err)
	}
	envelope.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, message))
	return envelope
}

func officialTestChecksum(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func ptrTime(value time.Time) *time.Time {
	return &value
}
