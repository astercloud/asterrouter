package plugins

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/hkdf"
)

func TestOfficialFeedImportDecryptsAndEncryptsLocalCache(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	serviceKey := "provider-intelligence"
	instanceID := "inst_feed_test"
	licenseID := "lic_feed_test"
	svc, repo, privateKey := newOfficialFeedTestService(t, now, licenseID, instanceID, serviceKey)
	client, err := svc.OfficialFeedClientInfo(context.Background())
	if err != nil {
		t.Fatalf("OfficialFeedClientInfo(): %v", err)
	}
	payload := json.RawMessage(`{"providers":[{"key":"provider-a","availability":0.999}],"generated_by":"fixture"}`)
	envelope := signedEncryptedFeedEnvelope(t, privateKey, "feed-key-v1", client.EncryptionPublicKey, encryptedFeedFixture{
		ServiceKey:        serviceKey,
		FeedID:            "feed_20260712_001",
		FeedVersion:       "2026.07.12.001",
		DataSchemaVersion: "provider-intelligence.feed.v1",
		LicenseID:         licenseID,
		InstanceID:        instanceID,
		IssuedAt:          now,
		ExpiresAt:         now.Add(24 * time.Hour),
		Plaintext:         payload,
	})
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	status, err := svc.ImportOfficialFeed(context.Background(), OfficialFeedImportRequest{Envelope: raw})
	if err != nil {
		t.Fatalf("ImportOfficialFeed(): %v", err)
	}
	if status.ServiceKey != serviceKey || status.FeedID != "feed_20260712_001" || !status.SignatureVerified {
		t.Fatalf("feed status mismatch: %+v", status)
	}
	record, ok, err := repo.LatestOfficialFeed(context.Background(), serviceKey)
	if err != nil || !ok {
		t.Fatalf("LatestOfficialFeed() ok=%v err=%v", ok, err)
	}
	if record.PayloadCiphertext == string(payload) || record.PayloadCiphertext == "" {
		t.Fatalf("decrypted payload was not encrypted at rest: %+v", record)
	}
	decrypted, err := svc.OfficialFeedPayload(context.Background(), serviceKey)
	if err != nil {
		t.Fatalf("OfficialFeedPayload(): %v", err)
	}
	if string(decrypted) != string(payload) {
		t.Fatalf("decrypted payload = %s, want %s", decrypted, payload)
	}
	if err := repo.SaveLicense(context.Background(), licenseRecord{
		LicenseID: licenseID, InstanceID: instanceID, Status: LicenseStatusExpired,
		EntitlementsJSON: `[]`, IssuedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour), ImportedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("SaveLicense(expired): %v", err)
	}
	if _, err := svc.OfficialFeedPayload(context.Background(), serviceKey); !errors.Is(err, ErrLicenseNotFound) {
		t.Fatalf("OfficialFeedPayload(expired license) error = %v, want ErrLicenseNotFound", err)
	}
}

func TestOfficialFeedImportRejectsWrongBindingAndEntitlement(t *testing.T) {
	now := time.Date(2026, 7, 12, 11, 0, 0, 0, time.UTC)
	svc, _, privateKey := newOfficialFeedTestService(t, now, "lic_feed", "inst_feed", "provider-intelligence")
	client, err := svc.OfficialFeedClientInfo(context.Background())
	if err != nil {
		t.Fatalf("OfficialFeedClientInfo(): %v", err)
	}
	wrongBinding := signedEncryptedFeedEnvelope(t, privateKey, "feed-key-v1", client.EncryptionPublicKey, encryptedFeedFixture{
		ServiceKey: "provider-intelligence", FeedID: "feed_wrong_instance", FeedVersion: "2", DataSchemaVersion: "provider-intelligence.feed.v1",
		LicenseID: "lic_feed", InstanceID: "inst_other", IssuedAt: now, ExpiresAt: now.Add(time.Hour), Plaintext: json.RawMessage(`{"ok":true}`),
	})
	rawWrongBinding, _ := json.Marshal(wrongBinding)
	if _, err := svc.ImportOfficialFeed(context.Background(), OfficialFeedImportRequest{Envelope: rawWrongBinding}); !errors.Is(err, ErrOfficialFeedBinding) {
		t.Fatalf("wrong binding error = %v, want ErrOfficialFeedBinding", err)
	}

	wrongEntitlement := signedEncryptedFeedEnvelope(t, privateKey, "feed-key-v1", client.EncryptionPublicKey, encryptedFeedFixture{
		ServiceKey: "model-authenticity", FeedID: "feed_wrong_entitlement", FeedVersion: "2", DataSchemaVersion: "model-authenticity.feed.v1",
		LicenseID: "lic_feed", InstanceID: "inst_feed", IssuedAt: now, ExpiresAt: now.Add(time.Hour), Plaintext: json.RawMessage(`{"ok":true}`),
	})
	rawWrongEntitlement, _ := json.Marshal(wrongEntitlement)
	if _, err := svc.ImportOfficialFeed(context.Background(), OfficialFeedImportRequest{Envelope: rawWrongEntitlement}); !errors.Is(err, ErrOfficialFeedEntitlement) {
		t.Fatalf("wrong entitlement error = %v, want ErrOfficialFeedEntitlement", err)
	}
}

func TestOfficialFeedImportRejectsTamperedCiphertextAndRollback(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	svc, _, privateKey := newOfficialFeedTestService(t, now, "lic_feed", "inst_feed", "provider-intelligence")
	client, err := svc.OfficialFeedClientInfo(context.Background())
	if err != nil {
		t.Fatalf("OfficialFeedClientInfo(): %v", err)
	}
	newer := encryptedFeedFixture{
		ServiceKey: "provider-intelligence", FeedID: "feed_newer", FeedVersion: "3", DataSchemaVersion: "provider-intelligence.feed.v1",
		LicenseID: "lic_feed", InstanceID: "inst_feed", IssuedAt: now, ExpiresAt: now.Add(24 * time.Hour), Plaintext: json.RawMessage(`{"version":3}`),
	}
	newerEnvelope := signedEncryptedFeedEnvelope(t, privateKey, "feed-key-v1", client.EncryptionPublicKey, newer)
	rawNewer, _ := json.Marshal(newerEnvelope)
	if _, err := svc.ImportOfficialFeed(context.Background(), OfficialFeedImportRequest{Envelope: rawNewer}); err != nil {
		t.Fatalf("ImportOfficialFeed(newer): %v", err)
	}

	tampered := newer
	tampered.FeedID = "feed_tampered"
	tampered.FeedVersion = "4"
	tampered.IssuedAt = now.Add(time.Minute)
	tamperedEnvelope := signedEncryptedFeedEnvelope(t, privateKey, "feed-key-v1", client.EncryptionPublicKey, tampered)
	var tamperedPayload encryptedFeedPackage
	if err := json.Unmarshal(tamperedEnvelope.Payload, &tamperedPayload); err != nil {
		t.Fatalf("decode tampered package: %v", err)
	}
	ciphertext, err := decodeBase64Value(tamperedPayload.Payload.Ciphertext)
	if err != nil {
		t.Fatalf("decode ciphertext: %v", err)
	}
	ciphertext[len(ciphertext)-1] ^= 0xff
	tamperedPayload.Payload.Ciphertext = base64.RawURLEncoding.EncodeToString(ciphertext)
	tamperedEnvelope = signFeedPackage(t, privateKey, "feed-key-v1", tamperedPayload, tampered.IssuedAt, tampered.ExpiresAt)
	rawTampered, _ := json.Marshal(tamperedEnvelope)
	if _, err := svc.ImportOfficialFeed(context.Background(), OfficialFeedImportRequest{Envelope: rawTampered}); !errors.Is(err, ErrOfficialFeedDecrypt) {
		t.Fatalf("tampered ciphertext error = %v, want ErrOfficialFeedDecrypt", err)
	}

	older := newer
	older.FeedID = "feed_older"
	older.FeedVersion = "2"
	older.IssuedAt = now.Add(-time.Hour)
	olderEnvelope := signedEncryptedFeedEnvelope(t, privateKey, "feed-key-v1", client.EncryptionPublicKey, older)
	rawOlder, _ := json.Marshal(olderEnvelope)
	if _, err := svc.ImportOfficialFeed(context.Background(), OfficialFeedImportRequest{Envelope: rawOlder}); !errors.Is(err, ErrOfficialFeedReplay) {
		t.Fatalf("rollback error = %v, want ErrOfficialFeedReplay", err)
	}
	payload, err := svc.OfficialFeedPayload(context.Background(), "provider-intelligence")
	if err != nil || string(payload) != `{"version":3}` {
		t.Fatalf("last usable payload was replaced after rejected imports: payload=%s err=%v", payload, err)
	}
}

func TestOfficialFeedSyncUsesSignedMetadataAndShortLivedDownloadGrant(t *testing.T) {
	now := time.Date(2026, 7, 12, 13, 0, 0, 0, time.UTC)
	serviceKey := "provider-intelligence"
	licenseID := "lic_feed_sync"
	instanceID := "inst_feed_sync"
	svc, repo, privateKey := newOfficialFeedTestService(t, now, licenseID, instanceID, serviceKey)
	activationSecret := "activation-secret"
	ciphertext, err := encryptSecret(svc.secretKey, activationSecret)
	if err != nil {
		t.Fatalf("encrypt activation secret: %v", err)
	}
	license, ok, err := repo.LatestLicense(context.Background())
	if err != nil || !ok {
		t.Fatalf("LatestLicense() ok=%v err=%v", ok, err)
	}
	license.ActivationSecretCiphertext = ciphertext
	license.ImportedAt = now.Add(time.Second)
	license.UpdatedAt = now.Add(time.Second)
	if err := repo.SaveLicense(context.Background(), license); err != nil {
		t.Fatalf("SaveLicense(): %v", err)
	}

	var server *httptest.Server
	var artifact []byte
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/objects/") {
			if r.Header.Get("X-Aster-License-ID") != licenseID || r.Header.Get("X-Aster-Instance-ID") != instanceID || r.Header.Get("X-Aster-Activation-Secret") != activationSecret {
				t.Errorf("authorization headers mismatch: %#v", r.Header)
			}
			if r.Header.Get("X-Aster-Feed-Public-Key") == "" || r.Header.Get("X-Aster-Request-ID") == "" {
				t.Errorf("feed binding headers are missing: %#v", r.Header)
			}
		}
		w.Header().Set("X-Aster-Request-ID", "cloud-request-123")
		switch r.URL.Path {
		case "/official/v1/services/" + serviceKey + "/feeds/latest":
			metadata := officialFeedRemoteMetadata{
				ServiceKey: serviceKey, FeedID: "feed_online_001", FeedVersion: "2026.07.12.001",
				DataSchemaVersion: "provider-intelligence.feed.v1", ExpiresAt: now.Add(time.Hour),
			}
			writeFeedTestJSON(t, w, signOfficialFeedTestEnvelope(t, privateKey, "feed-key-v1", officialFeedMetadataPurpose, metadata, now, now.Add(time.Hour)))
		case "/official/v1/services/" + serviceKey + "/feeds/feed_online_001/download":
			feedEnvelope := signedEncryptedFeedEnvelope(t, privateKey, "feed-key-v1", r.Header.Get("X-Aster-Feed-Public-Key"), encryptedFeedFixture{
				ServiceKey: serviceKey, FeedID: "feed_online_001", FeedVersion: "2026.07.12.001", DataSchemaVersion: "provider-intelligence.feed.v1",
				LicenseID: licenseID, InstanceID: instanceID, IssuedAt: now, ExpiresAt: now.Add(time.Hour), Plaintext: json.RawMessage(`{"source":"online"}`),
			})
			raw, marshalErr := json.Marshal(feedEnvelope)
			if marshalErr != nil {
				t.Fatalf("marshal feed envelope: %v", marshalErr)
			}
			artifact = raw
			sum := sha256.Sum256(artifact)
			grant := officialFeedDownloadGrant{
				ServiceKey: serviceKey, FeedID: "feed_online_001", DownloadURL: server.URL + "/objects/feed_online_001.json",
				RequestID: "cloud-request-123", SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(artifact)), ExpiresAt: now.Add(5 * time.Minute),
			}
			writeFeedTestJSON(t, w, signOfficialFeedTestEnvelope(t, privateKey, "feed-key-v1", officialFeedDownloadPurpose, grant, now, now.Add(5*time.Minute)))
		case "/objects/feed_online_001.json":
			if len(artifact) == 0 {
				t.Fatal("feed artifact was not prepared")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(artifact)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	svc.catalogConfig.Mode = CatalogModeOnline
	svc.catalogConfig.URL = server.URL + "/official/v1/catalog/index"
	svc.httpClient = server.Client()

	result, err := svc.SyncOfficialFeed(context.Background(), serviceKey)
	if err != nil {
		t.Fatalf("SyncOfficialFeed(): %v", err)
	}
	if result.Feed.FeedID != "feed_online_001" || result.Run.Status != "succeeded" || result.Run.RequestID != "cloud-request-123" {
		t.Fatalf("sync result mismatch: %+v", result)
	}
	payload, err := svc.OfficialFeedPayload(context.Background(), serviceKey)
	if err != nil || string(payload) != `{"source":"online"}` {
		t.Fatalf("OfficialFeedPayload() payload=%s err=%v", payload, err)
	}
	runs, err := svc.OfficialFeedSyncRuns(context.Background(), serviceKey, 10)
	if err != nil || len(runs) != 1 || runs[0].Status != "succeeded" {
		t.Fatalf("OfficialFeedSyncRuns() runs=%+v err=%v", runs, err)
	}
}

func TestOfficialFeedImportAppliesRevocationsAndFallsBackToUsableCache(t *testing.T) {
	now := time.Date(2026, 7, 12, 14, 0, 0, 0, time.UTC)
	current := now
	serviceKey := "provider-intelligence"
	svc, repo, privateKey := newOfficialFeedTestService(t, now, "lic_feed", "inst_feed", serviceKey)
	svc.now = func() time.Time { return current }
	client, err := svc.OfficialFeedClientInfo(context.Background())
	if err != nil {
		t.Fatalf("OfficialFeedClientInfo(): %v", err)
	}
	first := signedEncryptedFeedEnvelope(t, privateKey, "feed-key-v1", client.EncryptionPublicKey, encryptedFeedFixture{
		ServiceKey: serviceKey, FeedID: "feed_first", FeedVersion: "1", DataSchemaVersion: "provider-intelligence.feed.v1",
		LicenseID: "lic_feed", InstanceID: "inst_feed", IssuedAt: current, ExpiresAt: current.Add(24 * time.Hour), Plaintext: json.RawMessage(`{"version":1}`),
	})
	rawFirst, _ := json.Marshal(first)
	if _, err := svc.ImportOfficialFeed(context.Background(), OfficialFeedImportRequest{Envelope: rawFirst}); err != nil {
		t.Fatalf("ImportOfficialFeed(first): %v", err)
	}
	current = current.Add(time.Minute)
	second := signedEncryptedFeedEnvelope(t, privateKey, "feed-key-v1", client.EncryptionPublicKey, encryptedFeedFixture{
		ServiceKey: serviceKey, FeedID: "feed_second", FeedVersion: "2", DataSchemaVersion: "provider-intelligence.feed.v1",
		LicenseID: "lic_feed", InstanceID: "inst_feed", IssuedAt: current, ExpiresAt: current.Add(24 * time.Hour), Plaintext: json.RawMessage(`{"version":2}`),
	})
	rawSecond, _ := json.Marshal(second)
	if _, err := svc.ImportOfficialFeed(context.Background(), OfficialFeedImportRequest{Envelope: rawSecond}); err != nil {
		t.Fatalf("ImportOfficialFeed(second): %v", err)
	}
	current = current.Add(time.Minute)
	third := signedEncryptedFeedEnvelope(t, privateKey, "feed-key-v1", client.EncryptionPublicKey, encryptedFeedFixture{
		ServiceKey: serviceKey, FeedID: "feed_third", FeedVersion: "3", DataSchemaVersion: "provider-intelligence.feed.v1",
		LicenseID: "lic_feed", InstanceID: "inst_feed", IssuedAt: current, ExpiresAt: current.Add(24 * time.Hour), Plaintext: json.RawMessage(`{"version":3}`),
		Revocations: []feedRevocation{{FeedID: "feed_second", Reason: "withdrawn", RevokedAt: current}},
	})
	rawThird, _ := json.Marshal(third)
	if _, err := svc.ImportOfficialFeed(context.Background(), OfficialFeedImportRequest{Envelope: rawThird}); err != nil {
		t.Fatalf("ImportOfficialFeed(third): %v", err)
	}
	statuses, err := svc.OfficialFeedStatuses(context.Background(), serviceKey)
	if err != nil {
		t.Fatalf("OfficialFeedStatuses(): %v", err)
	}
	if feedStatusByID(statuses, "feed_first") != "active" || feedStatusByID(statuses, "feed_second") != "revoked" || feedStatusByID(statuses, "feed_third") != "active" {
		t.Fatalf("revocation statuses mismatch: %+v", statuses)
	}

	if err := repo.UpdateOfficialFeedStatus(context.Background(), serviceKey, "feed_third", "revoked", current.Add(time.Minute)); err != nil {
		t.Fatalf("UpdateOfficialFeedStatus(): %v", err)
	}
	payload, err := svc.OfficialFeedPayload(context.Background(), serviceKey)
	if err != nil || string(payload) != `{"version":1}` {
		t.Fatalf("payload fallback=%s err=%v", payload, err)
	}
}

func TestOfficialFeedSyncAppliesSignedRevocationBeforeFailedDownload(t *testing.T) {
	now := time.Date(2026, 7, 12, 15, 0, 0, 0, time.UTC)
	serviceKey := "provider-intelligence"
	svc, repo, privateKey := newOfficialFeedTestService(t, now, "lic_feed", "inst_feed", serviceKey)
	client, err := svc.OfficialFeedClientInfo(context.Background())
	if err != nil {
		t.Fatalf("OfficialFeedClientInfo(): %v", err)
	}
	active := signedEncryptedFeedEnvelope(t, privateKey, "feed-key-v1", client.EncryptionPublicKey, encryptedFeedFixture{
		ServiceKey: serviceKey, FeedID: "feed_recalled", FeedVersion: "1", DataSchemaVersion: "provider-intelligence.feed.v1",
		LicenseID: "lic_feed", InstanceID: "inst_feed", IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), Plaintext: json.RawMessage(`{"version":1}`),
	})
	rawActive, _ := json.Marshal(active)
	if _, err := svc.ImportOfficialFeed(context.Background(), OfficialFeedImportRequest{Envelope: rawActive}); err != nil {
		t.Fatalf("ImportOfficialFeed(active): %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/official/v1/services/" + serviceKey + "/feeds/latest":
			metadata := officialFeedRemoteMetadata{
				ServiceKey: serviceKey, FeedID: "feed_replacement", FeedVersion: "2", DataSchemaVersion: "provider-intelligence.feed.v1",
				ExpiresAt: now.Add(time.Hour), Revocations: []feedRevocation{{FeedID: "feed_recalled", Reason: "security recall", RevokedAt: now}},
			}
			writeFeedTestJSON(t, w, signOfficialFeedTestEnvelope(t, privateKey, "feed-key-v1", officialFeedMetadataPurpose, metadata, now, now.Add(time.Hour)))
		case "/official/v1/services/" + serviceKey + "/feeds/feed_replacement/download":
			http.Error(w, "object unavailable", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	svc.catalogConfig.Mode = CatalogModeOnline
	svc.catalogConfig.URL = server.URL + "/official/v1/catalog/index"
	svc.httpClient = server.Client()

	result, err := svc.SyncOfficialFeed(context.Background(), serviceKey)
	if !errors.Is(err, ErrOfficialFeedRemote) || result.Run.ErrorCode != "remote_request_failed" {
		t.Fatalf("SyncOfficialFeed() result=%+v err=%v", result, err)
	}
	statuses, err := svc.OfficialFeedStatuses(context.Background(), serviceKey)
	if err != nil || feedStatusByID(statuses, "feed_recalled") != "revoked" {
		t.Fatalf("signed revocation was not applied: statuses=%+v err=%v", statuses, err)
	}
	if _, ok, err := repo.LatestOfficialFeed(context.Background(), serviceKey); err != nil || !ok {
		t.Fatalf("revoked historical record disappeared: ok=%v err=%v", ok, err)
	}
	if _, err := svc.OfficialFeedPayload(context.Background(), serviceKey); !errors.Is(err, ErrOfficialFeedExpired) {
		t.Fatalf("revoked payload remains readable: %v", err)
	}
}

type encryptedFeedFixture struct {
	ServiceKey        string
	FeedID            string
	FeedVersion       string
	DataSchemaVersion string
	LicenseID         string
	InstanceID        string
	IssuedAt          time.Time
	ExpiresAt         time.Time
	Plaintext         json.RawMessage
	Revocations       []feedRevocation
}

func newOfficialFeedTestService(t *testing.T, now time.Time, licenseID string, instanceID string, serviceKey string) (*Service, *MemoryRepository, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(): %v", err)
	}
	repo := NewMemoryRepository()
	entitlements, err := json.Marshal([]Entitlement{{
		PublicID: "ent_feed", Type: "data_feed", ResourceKey: serviceKey, Status: LicenseStatusActive,
		StartsAt: now.Add(-time.Hour), ExpiresAt: timePointer(now.Add(48 * time.Hour)),
	}})
	if err != nil {
		t.Fatalf("marshal entitlements: %v", err)
	}
	if err := repo.SaveLicense(context.Background(), licenseRecord{
		LicenseID: licenseID, InstanceID: instanceID, Status: LicenseStatusActive,
		EntitlementsJSON: string(entitlements), IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(48 * time.Hour), ImportedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SaveLicense(): %v", err)
	}
	svc := NewServiceWithOptions(repo, ServiceOptions{
		SecretKey: "feed-test-local-secret",
		OfficialCatalog: OfficialCatalogConfig{
			Mode: CatalogModeOffline, PublicKeyID: "feed-key-v1", PublicKeyBase64: base64.StdEncoding.EncodeToString(publicKey),
		},
		Now: func() time.Time { return now },
	})
	return svc, repo, privateKey
}

func signedEncryptedFeedEnvelope(t *testing.T, signingKey ed25519.PrivateKey, keyID string, clientPublicKey string, fixture encryptedFeedFixture) catalogEnvelope {
	t.Helper()
	clientRaw, err := decodeBase64Value(clientPublicKey)
	if err != nil {
		t.Fatalf("decode client public key: %v", err)
	}
	clientKey, err := ecdh.X25519().NewPublicKey(clientRaw)
	if err != nil {
		t.Fatalf("NewPublicKey(): %v", err)
	}
	ephemeralKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(ephemeral): %v", err)
	}
	sharedSecret, err := ephemeralKey.ECDH(clientKey)
	if err != nil {
		t.Fatalf("ECDH(): %v", err)
	}
	keyEncryptionKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, sharedSecret, []byte(fixture.FeedID), []byte("astercloud:official-data-feed:key-wrap:v1")), keyEncryptionKey); err != nil {
		t.Fatalf("derive key encryption key: %v", err)
	}
	dataKey := sha256.Sum256([]byte("data-key|" + fixture.FeedID))
	keyNonce := []byte("keynonce1234")
	wrappedKey := sealFeedFixture(t, keyEncryptionKey, keyNonce, dataKey[:], []byte(fixture.FeedID+"|"+fixture.ServiceKey))
	payloadNonce := []byte("payloadnonce")
	ciphertext := sealFeedFixture(t, dataKey[:], payloadNonce, fixture.Plaintext, []byte(fixture.ServiceKey+"|"+fixture.FeedID+"|"+fixture.DataSchemaVersion))
	sum := sha256.Sum256(fixture.Plaintext)
	feed := encryptedFeedPackage{
		SchemaVersion: officialFeedPackageSchema, ServiceKey: fixture.ServiceKey, FeedID: fixture.FeedID,
		FeedVersion: fixture.FeedVersion, DataSchemaVersion: fixture.DataSchemaVersion,
		LicenseID: fixture.LicenseID, InstanceID: fixture.InstanceID, IssuedAt: fixture.IssuedAt, ExpiresAt: fixture.ExpiresAt,
		Revocations: fixture.Revocations,
		Payload: encryptedFeedPayload{
			Cipher: officialFeedCipher, KeyWrap: officialFeedKeyWrap,
			EphemeralPublicKey:    base64.RawURLEncoding.EncodeToString(ephemeralKey.PublicKey().Bytes()),
			EncryptedDataKeyNonce: base64.RawURLEncoding.EncodeToString(keyNonce),
			EncryptedDataKey:      base64.RawURLEncoding.EncodeToString(wrappedKey),
			Nonce:                 base64.RawURLEncoding.EncodeToString(payloadNonce), Ciphertext: base64.RawURLEncoding.EncodeToString(ciphertext),
			SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(fixture.Plaintext)),
		},
	}
	return signFeedPackage(t, signingKey, keyID, feed, fixture.IssuedAt, fixture.ExpiresAt)
}

func signOfficialFeedTestEnvelope(t *testing.T, privateKey ed25519.PrivateKey, keyID string, purpose string, payload any, issuedAt time.Time, expiresAt time.Time) catalogEnvelope {
	t.Helper()
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal signed payload: %v", err)
	}
	envelope := catalogEnvelope{
		SchemaVersion: catalogEnvelopeSchema, Purpose: purpose, KeyID: keyID, Algorithm: "Ed25519",
		IssuedAt: issuedAt.UTC().Format(time.RFC3339Nano), ExpiresAt: expiresAt.UTC().Format(time.RFC3339Nano), Payload: rawPayload,
	}
	message, err := catalogEnvelopeSigningMessage(envelope)
	if err != nil {
		t.Fatalf("catalogEnvelopeSigningMessage(): %v", err)
	}
	envelope.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, message))
	return envelope
}

func writeFeedTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func feedStatusByID(items []OfficialFeedStatus, feedID string) string {
	for _, item := range items {
		if strings.TrimSpace(item.FeedID) == feedID {
			return item.Status
		}
	}
	return ""
}

func signFeedPackage(t *testing.T, privateKey ed25519.PrivateKey, keyID string, feed encryptedFeedPackage, issuedAt time.Time, expiresAt time.Time) catalogEnvelope {
	t.Helper()
	payload, err := json.Marshal(feed)
	if err != nil {
		t.Fatalf("marshal feed package: %v", err)
	}
	envelope := catalogEnvelope{
		SchemaVersion: catalogEnvelopeSchema, Purpose: officialFeedEnvelopePurpose, KeyID: keyID, Algorithm: "Ed25519",
		IssuedAt: issuedAt.UTC().Format(time.RFC3339Nano), ExpiresAt: expiresAt.UTC().Format(time.RFC3339Nano), Payload: payload,
	}
	message, err := catalogEnvelopeSigningMessage(envelope)
	if err != nil {
		t.Fatalf("catalogEnvelopeSigningMessage(): %v", err)
	}
	envelope.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, message))
	return envelope
}

func sealFeedFixture(t *testing.T, key []byte, nonce []byte, plaintext []byte, additionalData []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher(): %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("NewGCM(): %v", err)
	}
	if len(nonce) != gcm.NonceSize() {
		t.Fatalf("nonce size = %d, want %d", len(nonce), gcm.NonceSize())
	}
	return gcm.Seal(nil, nonce, plaintext, additionalData)
}

func timePointer(value time.Time) *time.Time {
	return &value
}
