package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSidecarFeedPayloadRequiresRuntimeTokenAndManifestPermission(t *testing.T) {
	now := time.Date(2026, 7, 12, 16, 0, 0, 0, time.UTC)
	serviceKey := "provider-intelligence"
	pluginID := "com.asterrouter.test.feed-reader"
	version := "1.0.0"
	token := "runtime-token"
	svc, repo, privateKey := newOfficialFeedTestService(t, now, "lic_feed", "inst_feed", serviceKey)
	svc.packageActiveDir = t.TempDir()

	if err := repo.SavePlugin(context.Background(), Plugin{
		ID: pluginID, PluginID: pluginID, Name: "Feed reader", Status: StatusEnabled,
		Tier: TierFreeCore, EntitlementStatus: EntitlementFree, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SavePlugin(): %v", err)
	}
	if err := repo.SavePackageInstallation(context.Background(), packageInstallationRecord{
		PluginID: pluginID, PackageID: "pkg_feed_reader", Version: version, Status: PackageInstallInstalled, InstalledAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SavePackageInstallation(): %v", err)
	}
	activeDir, err := svc.activePackageDir(pluginID, version)
	if err != nil {
		t.Fatalf("activePackageDir(): %v", err)
	}
	if err := os.MkdirAll(activeDir, 0750); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	manifest, err := json.Marshal(sidecarManifest{ID: pluginID, Version: version, Runtime: "sidecar", DataFeeds: []string{serviceKey}})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(activeDir, "plugin.json"), manifest, 0600); err != nil {
		t.Fatalf("WriteFile(manifest): %v", err)
	}

	client, err := svc.OfficialFeedClientInfo(context.Background())
	if err != nil {
		t.Fatalf("OfficialFeedClientInfo(): %v", err)
	}
	envelope := signedEncryptedFeedEnvelope(t, privateKey, "feed-key-v1", client.EncryptionPublicKey, encryptedFeedFixture{
		ServiceKey: serviceKey, FeedID: "feed_sidecar", FeedVersion: "1", DataSchemaVersion: "provider-intelligence.feed.v1",
		LicenseID: "lic_feed", InstanceID: "inst_feed", IssuedAt: now, ExpiresAt: now.Add(time.Hour), Plaintext: json.RawMessage(`{"allowed":true}`),
	})
	rawEnvelope, _ := json.Marshal(envelope)
	if _, err := svc.ImportOfficialFeed(context.Background(), OfficialFeedImportRequest{Envelope: rawEnvelope}); err != nil {
		t.Fatalf("ImportOfficialFeed(): %v", err)
	}

	done := make(chan struct{})
	svc.sidecars[pluginID] = &sidecarProcess{
		PluginID: pluginID, Version: version, Token: token,
		Command: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}, done: done,
	}

	if _, err := svc.SidecarFeedPayload(context.Background(), pluginID, "wrong-token", serviceKey); !errors.Is(err, ErrPluginHostUnauthorized) {
		t.Fatalf("wrong token error = %v, want ErrPluginHostUnauthorized", err)
	}
	if _, err := svc.SidecarFeedPayload(context.Background(), pluginID, token, "risk-intelligence"); !errors.Is(err, ErrPluginHostPermission) {
		t.Fatalf("undeclared service error = %v, want ErrPluginHostPermission", err)
	}
	payload, err := svc.SidecarFeedPayload(context.Background(), pluginID, token, serviceKey)
	if err != nil || string(payload) != `{"allowed":true}` {
		t.Fatalf("SidecarFeedPayload() payload=%s err=%v", payload, err)
	}

	close(done)
	if _, err := svc.SidecarFeedPayload(context.Background(), pluginID, token, serviceKey); !errors.Is(err, ErrPluginHostUnauthorized) {
		t.Fatalf("stopped process error = %v, want ErrPluginHostUnauthorized", err)
	}
}

func TestReadSidecarManifestRejectsWildcardDataFeedPermission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plugin.json")
	if err := os.WriteFile(path, []byte(`{"id":"com.asterrouter.test","version":"1.0.0","runtime":"sidecar","data_feeds":["*"]}`), 0600); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	if _, err := readSidecarManifest(path); err == nil {
		t.Fatal("wildcard data feed permission was accepted")
	}
}

func TestSidecarConfigReturnsOnlyBoundPluginConfig(t *testing.T) {
	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	pluginID := "com.asterrouter.test.config-reader"
	version := "1.0.0"
	token := "runtime-token"
	repo := NewMemoryRepository()
	svc := NewServiceWithOptions(repo, ServiceOptions{
		SecretKey: "test-secret", PluginActiveDir: t.TempDir(), PluginDataDir: t.TempDir(),
	})
	if err := repo.SavePlugin(context.Background(), Plugin{
		ID: pluginID, PluginID: pluginID, Name: "Config reader", Status: StatusEnabled,
		Tier: TierFreeCore, EntitlementStatus: EntitlementFree, Configurable: true,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePackageInstallation(context.Background(), packageInstallationRecord{
		PluginID: pluginID, PackageID: "pkg_config_reader", Version: version, Status: PackageInstallInstalled,
		InstalledAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	activeDir, err := svc.activePackageDir(pluginID, version)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(activeDir, 0750); err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(sidecarManifest{ID: pluginID, Version: version, Runtime: "sidecar"})
	if err := os.WriteFile(filepath.Join(activeDir, "plugin.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	ciphertext, err := encryptSecret(svc.secretKey, "postgres://monitorprice:secret@db.example/sub2api")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveConfig(context.Background(), configRecord{
		PluginID:          pluginID,
		Settings:          map[string]string{"base_currency": "CNY"},
		SecretCiphertexts: map[string]string{"sub2api_database_url": ciphertext},
		SecretHints:       map[string]string{"sub2api_database_url": "postgr...2api"},
		CreatedAt:         now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	svc.sidecars[pluginID] = &sidecarProcess{
		PluginID: pluginID, Version: version, Token: token,
		Command: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}, done: done,
	}
	defer close(done)

	if _, err := svc.SidecarConfig(context.Background(), pluginID, "wrong-token"); !errors.Is(err, ErrPluginHostUnauthorized) {
		t.Fatalf("wrong token error = %v", err)
	}
	config, err := svc.SidecarConfig(context.Background(), pluginID, token)
	if err != nil {
		t.Fatal(err)
	}
	if config.Settings["base_currency"] != "CNY" || config.Secrets["sub2api_database_url"] != "postgres://monitorprice:secret@db.example/sub2api" {
		t.Fatalf("unexpected sidecar config: %#v", config)
	}
	if len(config.Secrets) != 1 || config.PluginID != pluginID {
		t.Fatalf("sidecar config escaped plugin boundary: %#v", config)
	}
}

func TestSidecarBaseEnvironmentDropsHostSecrets(t *testing.T) {
	got := sidecarBaseEnvironment([]string{
		"PATH=/usr/bin", "HOME=/tmp/home", "LANG=zh_CN.UTF-8", "DATABASE_URL=postgres://core-secret",
		"ASTERROUTER_SERVER_SECURITY_SECRET_KEY=core-secret", "MONITORPRICE_SUB2API_DATABASE_URL=plugin-secret",
	})
	want := map[string]bool{"PATH=/usr/bin": true, "HOME=/tmp/home": true, "LANG=zh_CN.UTF-8": true}
	if len(got) != len(want) {
		t.Fatalf("environment = %#v", got)
	}
	for _, entry := range got {
		if !want[entry] {
			t.Fatalf("unexpected inherited environment %q", entry)
		}
	}
}

func TestSidecarDataDirIsStableAndProtected(t *testing.T) {
	root := t.TempDir()
	svc := NewServiceWithOptions(NewMemoryRepository(), ServiceOptions{PluginDataDir: root})
	dir, err := svc.sidecarDataDir("com.asterrouter.monitorprice.workbench")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "com.asterrouter.monitorprice.workbench")
	if dir != want {
		t.Fatalf("data dir = %q, want %q", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("data dir mode = %o", info.Mode().Perm())
	}
}
