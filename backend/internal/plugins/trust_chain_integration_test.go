package plugins

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
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
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPluginTrustChainCatalogToSidecarFeed(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	pluginID := "com.astercloud.catalog.feed-sidecar"
	pluginSlug := "feed-sidecar"
	packageID := "pkg_feed_sidecar_" + runtime.GOOS + "_" + runtime.GOARCH
	version := "1.0.0"
	serviceKey := "provider-intelligence"
	licenseID := "lic_trust_chain"
	instanceID := "inst_trust_chain"
	packageBody := buildJ07SidecarPackage(t, pluginID, version, serviceKey)
	packageSum := sha256.Sum256(packageBody)
	packageSHA := hex.EncodeToString(packageSum[:])

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	packageSignature := signedPackageEnvelope(t, privateKey, "trust-key-v1", packageSignaturePayload{
		SchemaVersion: packagePayloadSchema,
		Plugin:        pluginSlug,
		Version:       version,
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		SHA256:        packageSHA,
		SizeBytes:     int64(len(packageBody)),
		URI:           "object://feed-sidecar/1.0.0/package.tar.gz",
	}, now)
	catalogEnvelope := signedCatalogEnvelope(t, privateKey, "trust-key-v1", remoteCatalogIndex{
		SchemaVersion:  catalogIndexSchema,
		CatalogVersion: 1,
		GeneratedAt:    now,
		Plugins: []remoteCatalogPlugin{{
			PublicID:   "plg_feed_sidecar",
			PluginID:   pluginID,
			Slug:       pluginSlug,
			Name:       "Feed Sidecar",
			Summary:    "Synthetic signed sidecar for the trust-chain integration test.",
			Category:   "official",
			VendorName: "AsterCloud",
			Tier:       "free",
			Versions: []remoteCatalogVersion{{
				PublicID:       "plgv_feed_sidecar_1",
				Version:        version,
				Channel:        "stable",
				Status:         "published",
				MinCoreVersion: "1.0.0",
				Compatibility: []remoteCompatibility{{
					CoreVersionRange: ">=1.0.0 <2.0.0",
					OS:               runtime.GOOS,
					Arch:             runtime.GOARCH,
					Result:           "compatible",
				}},
				Packages: []remoteCatalogPackage{{
					PublicID:  packageID,
					OS:        runtime.GOOS,
					Arch:      runtime.GOARCH,
					SHA256:    packageSHA,
					SizeBytes: int64(len(packageBody)),
					Signature: packageSignature,
				}},
			}},
		}},
	}, now)

	var catalogServerURL string
	catalogServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/official/v1/catalog/index":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(wrappedCatalogEnvelope(t, catalogEnvelope))
		case "/official/v1/packages/" + packageID + "/download":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": packageDownloadGrant{
				ID:              "grant_trust_chain",
				PublicID:        "grant_trust_chain",
				PackageID:       packageID,
				PackagePublicID: packageID,
				DownloadURL:     catalogServerURL + "/objects/sidecar.tar.gz",
				SHA256:          packageSHA,
				Signature:       packageSignature,
				ExpiresAt:       now.Add(time.Hour),
				CreatedAt:       now,
			}})
		case "/objects/sidecar.tar.gz":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(packageBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(catalogServer.Close)
	catalogServerURL = catalogServer.URL

	repo := NewMemoryRepository()
	entitlements, err := json.Marshal([]Entitlement{{
		PublicID: "ent_trust_feed", Type: "data_feed", ResourceKey: serviceKey, Status: LicenseStatusActive,
		StartsAt: now.Add(-time.Hour), ExpiresAt: timePointer(now.Add(24 * time.Hour)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveLicense(ctx, licenseRecord{
		LicenseID: licenseID, InstanceID: instanceID, Status: LicenseStatusActive,
		EntitlementsJSON: string(entitlements), IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), ImportedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	var svc *Service
	hostServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1/plugin-host/" + pluginID + "/feeds/" + serviceKey
		if r.URL.Path != wantPath {
			http.NotFound(w, r)
			return
		}
		payload, err := svc.SidecarFeedPayload(ctx, pluginID, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), serviceKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(hostServer.Close)

	root := t.TempDir()
	svc = NewServiceWithOptions(repo, ServiceOptions{
		SecretKey: "trust-chain-local-secret",
		OfficialCatalog: OfficialCatalogConfig{
			Mode:            CatalogModeOnline,
			URL:             catalogServer.URL + "/official/v1/catalog/index",
			PublicKeyID:     "trust-key-v1",
			PublicKeyBase64: base64.StdEncoding.EncodeToString(publicKey),
		},
		PackageCacheDir: root + "/cache",
		PluginActiveDir: root + "/active",
		PluginHostURL:   hostServer.URL,
		CoreVersion:     "1.2.0",
		TargetOS:        runtime.GOOS,
		TargetArch:      runtime.GOARCH,
		Now:             func() time.Time { return now },
	})
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Shutdown(shutdownCtx)
	})

	status, err := svc.SyncOfficialCatalog(ctx)
	if err != nil || status.Status != catalogSyncSucceeded || status.PluginCount != 1 {
		t.Fatalf("catalog sync status=%+v err=%v", status, err)
	}
	download, err := svc.DownloadPackage(ctx, pluginID, packageID, PackageDownloadRequest{})
	if err != nil || download.SHA256 != packageSHA || download.SizeBytes != int64(len(packageBody)) {
		t.Fatalf("package download=%+v err=%v", download, err)
	}
	installation, err := svc.InstallPackage(ctx, pluginID, packageID)
	if err != nil || installation.Status != PackageInstallInstalled {
		t.Fatalf("package installation=%+v err=%v", installation, err)
	}

	client, err := svc.OfficialFeedClientInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	feedEnvelope := signedEncryptedFeedEnvelope(t, privateKey, "trust-key-v1", client.EncryptionPublicKey, encryptedFeedFixture{
		ServiceKey: serviceKey, FeedID: "feed_trust_chain", FeedVersion: "1", DataSchemaVersion: "provider-intelligence.feed.v1",
		LicenseID: licenseID, InstanceID: instanceID, IssuedAt: now, ExpiresAt: now.Add(time.Hour),
		Plaintext: json.RawMessage(`{"provider":"synthetic","trusted":true}`),
	})
	rawFeed, err := json.Marshal(feedEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ImportOfficialFeed(ctx, OfficialFeedImportRequest{Envelope: rawFeed}); err != nil {
		t.Fatalf("feed import: %v", err)
	}

	plugin, err := svc.Enable(ctx, pluginID)
	if err != nil || plugin.Status != StatusEnabled {
		t.Fatalf("plugin enable=%+v err=%v", plugin, err)
	}
	apiToken, err := svc.CreatePluginAPIToken(ctx, PluginAPITokenCreateRequest{
		Name: "Trust chain action token", PluginID: pluginID,
		Scopes: []string{PluginAPIScopeAction, PluginAPIScopePluginRead}, Surfaces: []string{"enterprise"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthorizePluginAPIToken(ctx, apiToken.Secret, PluginAPIScopeAction, pluginID, "enterprise"); err != nil {
		t.Fatalf("authorize action token: %v", err)
	}
	if _, err := svc.AuthorizePluginAPIToken(ctx, apiToken.Secret, PluginAPIScopeAction, pluginID, "personal"); !errors.Is(err, ErrPluginAPITokenScope) {
		t.Fatalf("wrong surface error=%v", err)
	}

	source := httptest.NewRequest(http.MethodGet, "http://router.local/actions/feed", nil)
	source.Header.Set("Authorization", "Bearer caller-token-must-not-reach-sidecar")
	response, err := svc.ProxySidecarHTTP(ctx, pluginID, "/actions/feed", source)
	if err != nil {
		t.Fatalf("sidecar proxy: %v", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(responseBody) != `{"provider":"synthetic","trusted":true}` {
		t.Fatalf("sidecar response status=%d body=%s", response.StatusCode, responseBody)
	}

	svc.sidecarsMu.Lock()
	runtimeToken := svc.sidecars[pluginID].Token
	svc.sidecarsMu.Unlock()
	if runtimeToken == "" || runtimeToken == apiToken.Secret || runtimeToken == "caller-token-must-not-reach-sidecar" {
		t.Fatal("runtime token was empty or reused a caller-visible token")
	}
	if _, err := svc.SidecarFeedPayload(ctx, pluginID, "wrong-token", serviceKey); !errors.Is(err, ErrPluginHostUnauthorized) {
		t.Fatalf("wrong runtime token error=%v", err)
	}
	if _, err := svc.SidecarFeedPayload(ctx, pluginID, runtimeToken, "risk-intelligence"); !errors.Is(err, ErrPluginHostPermission) {
		t.Fatalf("undeclared feed error=%v", err)
	}

	if _, err := svc.RevokePluginAPIToken(ctx, apiToken.Token.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthorizePluginAPIToken(ctx, apiToken.Secret, PluginAPIScopeAction, pluginID, "enterprise"); !errors.Is(err, ErrPluginAPITokenInvalid) {
		t.Fatalf("revoked API token error=%v", err)
	}
	if _, err := svc.Disable(ctx, pluginID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SidecarFeedPayload(ctx, pluginID, runtimeToken, serviceKey); !errors.Is(err, ErrPluginHostUnauthorized) {
		t.Fatalf("disabled sidecar token error=%v", err)
	}
}

func TestJ07SidecarHelperProcess(t *testing.T) {
	if os.Getenv("ASTER_J07_SIDECAR") != "1" {
		t.Skip("helper process")
	}
	addr := os.Getenv("ASTER_PLUGIN_ADDR")
	pluginID := os.Getenv("ASTER_PLUGIN_ID")
	token := os.Getenv("ASTER_PLUGIN_TOKEN")
	hostURL := strings.TrimRight(os.Getenv("ASTER_PLUGIN_HOST_URL"), "/")
	if addr == "" || pluginID == "" || token == "" || hostURL == "" {
		t.Fatal("sidecar environment is incomplete")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/actions/feed", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "runtime token missing", http.StatusUnauthorized)
			return
		}
		req, err := http.NewRequest(http.MethodGet, hostURL+"/api/v1/plugin-host/"+pluginID+"/feeds/provider-intelligence", nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	})
	if err := http.ListenAndServe(addr, mux); err != nil {
		t.Fatal(err)
	}
}

func buildJ07SidecarPackage(t *testing.T, pluginID, version, serviceKey string) []byte {
	t.Helper()
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	escapedBinary := strings.ReplaceAll(testBinary, "'", "'\\''")
	entrypoint := []byte("#!/bin/sh\nexport ASTER_J07_SIDECAR=1\nexec '" + escapedBinary + "' -test.run='^TestJ07SidecarHelperProcess$'\n")
	manifest, err := json.Marshal(sidecarManifest{
		ID: pluginID, Version: version, Runtime: "sidecar",
		Entrypoint: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "sidecar.sh"},
		DataFeeds:  []string{serviceKey},
	})
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	gzipWriter := gzip.NewWriter(&body)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, file := range []struct {
		name    string
		mode    int64
		content []byte
	}{
		{name: "plugin.json", mode: 0600, content: manifest},
		{name: "sidecar.sh", mode: 0700, content: entrypoint},
	} {
		if err := tarWriter.WriteHeader(&tar.Header{Name: file.name, Mode: file.mode, Size: int64(len(file.content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(file.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}
