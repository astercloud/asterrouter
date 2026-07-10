package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"

	"github.com/gowebpki/jcs"
)

func TestAdminPluginsCatalogEndpoint(t *testing.T) {
	handler := newTestHandler(t, config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int             `json:"code"`
		Data plugins.Catalog `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Summary.Total == 0 || resp.Data.Summary.PaidLocked == 0 {
		t.Fatalf("unexpected plugin summary: %+v", resp.Data.Summary)
	}
}

func TestAdminPluginsCatalogSyncEndpoint(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(): %v", err)
	}
	now := time.Date(2026, 7, 11, 2, 0, 0, 0, time.UTC)
	envelope := signedServerCatalogEnvelope(t, privateKey, "catalog-key-v1", map[string]any{
		"schema_version":  "astercloud.catalog-index.v1",
		"catalog_version": 3,
		"generated_at":    now.Format(time.RFC3339),
		"plugins": []map[string]any{
			{
				"public_id":   "plg_router_sync",
				"slug":        "router-sync",
				"name":        "Router Sync",
				"summary":     "Catalog synchronized plugin.",
				"category":    "official",
				"vendor_name": "AsterCloud",
				"tier":        "free",
				"versions": []map[string]any{
					{"public_id": "plgv_router_sync", "version": "1.0.0", "status": "published", "required_entitlement": false},
				},
			},
		},
	}, now)
	catalogServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": envelope})
	}))
	defer catalogServer.Close()

	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	pluginService := plugins.NewServiceWithOptions(plugins.NewMemoryRepository(), plugins.ServiceOptions{
		OfficialCatalog: plugins.OfficialCatalogConfig{
			Mode:            plugins.CatalogModeOnline,
			URL:             catalogServer.URL,
			PublicKeyID:     "catalog-key-v1",
			PublicKeyBase64: base64.StdEncoding.EncodeToString(publicKey),
		},
		Now: func() time.Time { return now },
	})
	handler := New(Options{Config: config.Config{}, SettingsService: settingsService, ControlService: controlService, PluginService: pluginService, SystemService: system.NewService(system.Config{Version: "test", BuildType: "source"})})

	syncReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins/catalog-sync", nil)
	syncRec := httptest.NewRecorder()
	handler.ServeHTTP(syncRec, syncReq)
	if syncRec.Code != http.StatusOK {
		t.Fatalf("sync status = %d body=%s", syncRec.Code, syncRec.Body.String())
	}
	var syncResp struct {
		Data plugins.OfficialCatalogStatus `json:"data"`
	}
	if err := json.Unmarshal(syncRec.Body.Bytes(), &syncResp); err != nil {
		t.Fatalf("decode sync: %v", err)
	}
	if syncResp.Data.CatalogVersion != 3 || syncResp.Data.PluginCount != 1 || syncResp.Data.Status != "succeeded" {
		t.Fatalf("sync response mismatch: %+v", syncResp.Data)
	}

	catalogReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins", nil)
	catalogRec := httptest.NewRecorder()
	handler.ServeHTTP(catalogRec, catalogReq)
	if catalogRec.Code != http.StatusOK {
		t.Fatalf("catalog status = %d body=%s", catalogRec.Code, catalogRec.Body.String())
	}
	var catalogResp struct {
		Data plugins.Catalog `json:"data"`
	}
	if err := json.Unmarshal(catalogRec.Body.Bytes(), &catalogResp); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	for _, plugin := range catalogResp.Data.Plugins {
		if plugin.ID == "com.astercloud.catalog.router-sync" && plugin.Version == "1.0.0" {
			return
		}
	}
	t.Fatalf("synced plugin not found: %+v", catalogResp.Data.Plugins)
}

func TestAdminPluginPackageDownloadEndpoint(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(): %v", err)
	}
	now := time.Date(2026, 7, 11, 2, 45, 0, 0, time.UTC)
	content := []byte("router package content")
	checksumBytes := sha256.Sum256(content)
	checksum := hex.EncodeToString(checksumBytes[:])
	packageID := "pkg_router_darwin_arm64"
	packageSignature := signedServerPackageEnvelope(t, privateKey, "catalog-key-v1", map[string]any{
		"schema_version": "astercloud.plugin-package.v1",
		"plugin":         "router-sync",
		"version":        "1.0.0",
		"os":             "darwin",
		"arch":           "arm64",
		"sha256":         checksum,
		"size_bytes":     len(content),
		"uri":            "object://router-sync/1.0.0/darwin-arm64.pkg",
	}, now)
	catalogEnvelope := signedServerCatalogEnvelope(t, privateKey, "catalog-key-v1", map[string]any{
		"schema_version":  "astercloud.catalog-index.v1",
		"catalog_version": 4,
		"generated_at":    now.Format(time.RFC3339),
		"plugins": []map[string]any{
			{
				"public_id":   "plg_router_sync",
				"slug":        "router-sync",
				"name":        "Router Sync",
				"summary":     "Catalog synchronized plugin.",
				"category":    "official",
				"vendor_name": "AsterCloud",
				"tier":        "free",
				"versions": []map[string]any{
					{
						"public_id":            "plgv_router_sync",
						"version":              "1.0.0",
						"channel":              "stable",
						"status":               "published",
						"min_core_version":     "1.0.0",
						"required_entitlement": false,
						"compatibility": []map[string]any{
							{"core_version_range": ">=1.0.0 <2.0.0", "os": "darwin", "arch": "arm64", "result": "compatible"},
						},
						"packages": []map[string]any{
							{"public_id": packageID, "os": "darwin", "arch": "arm64", "sha256": checksum, "size_bytes": len(content), "signature": packageSignature},
						},
					},
				},
			},
		},
	}, now)
	var catalogServerURL string
	catalogServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/official/v1/catalog/index":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": catalogEnvelope})
		case "/official/v1/packages/" + packageID + "/download":
			if r.Header.Get("X-Aster-Core-Version") != "1.2.0" || r.Header.Get("X-Aster-OS") != "darwin" || r.Header.Get("X-Aster-Arch") != "arm64" {
				t.Fatalf("missing package compatibility headers: %+v", r.Header)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"id":                "dgr_test",
					"public_id":         "dgr_test_public",
					"package_id":        "internal-package-id",
					"package_public_id": packageID,
					"download_url":      catalogServerURL + "/objects/router.pkg",
					"headers":           map[string]string{"X-Test-Download": "ok"},
					"sha256":            checksum,
					"signature":         packageSignature,
					"expires_at":        now.Add(10 * time.Minute).Format(time.RFC3339),
					"created_at":        now.Format(time.RFC3339),
				},
			})
		case "/objects/router.pkg":
			if r.Header.Get("X-Test-Download") != "ok" {
				t.Fatalf("download grant headers were not forwarded")
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
		default:
			http.NotFound(w, r)
		}
	}))
	defer catalogServer.Close()
	catalogServerURL = catalogServer.URL

	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	pluginService := plugins.NewServiceWithOptions(plugins.NewMemoryRepository(), plugins.ServiceOptions{
		OfficialCatalog: plugins.OfficialCatalogConfig{
			Mode:            plugins.CatalogModeOnline,
			URL:             catalogServer.URL + "/official/v1/catalog/index",
			PublicKeyID:     "catalog-key-v1",
			PublicKeyBase64: base64.StdEncoding.EncodeToString(publicKey),
		},
		PackageCacheDir: t.TempDir(),
		CoreVersion:     "1.2.0",
		TargetOS:        "darwin",
		TargetArch:      "arm64",
		Now:             func() time.Time { return now },
	})
	handler := New(Options{Config: config.Config{}, SettingsService: settingsService, ControlService: controlService, PluginService: pluginService, SystemService: system.NewService(system.Config{Version: "test", BuildType: "source"})})

	syncReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins/catalog-sync", nil)
	syncRec := httptest.NewRecorder()
	handler.ServeHTTP(syncRec, syncReq)
	if syncRec.Code != http.StatusOK {
		t.Fatalf("sync status = %d body=%s", syncRec.Code, syncRec.Body.String())
	}

	downloadReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins/com.astercloud.catalog.router-sync/packages/"+packageID+"/download", nil)
	downloadRec := httptest.NewRecorder()
	handler.ServeHTTP(downloadRec, downloadReq)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("download status = %d body=%s", downloadRec.Code, downloadRec.Body.String())
	}
	var downloadResp struct {
		Data plugins.PackageDownloadResult `json:"data"`
	}
	if err := json.Unmarshal(downloadRec.Body.Bytes(), &downloadResp); err != nil {
		t.Fatalf("decode download: %v", err)
	}
	cached, err := os.ReadFile(downloadResp.Data.CachePath)
	if err != nil {
		t.Fatalf("ReadFile(cache): %v", err)
	}
	if string(cached) != string(content) || downloadResp.Data.Package.CacheStatus != plugins.PackageCacheStatusCached {
		t.Fatalf("download response mismatch: %+v content=%q", downloadResp.Data, cached)
	}

	installReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins/com.astercloud.catalog.router-sync/packages/"+packageID+"/install", nil)
	installRec := httptest.NewRecorder()
	handler.ServeHTTP(installRec, installReq)
	if installRec.Code != http.StatusOK {
		t.Fatalf("install status = %d body=%s", installRec.Code, installRec.Body.String())
	}
	var installResp struct {
		Data plugins.PackageInstallation `json:"data"`
	}
	if err := json.Unmarshal(installRec.Body.Bytes(), &installResp); err != nil {
		t.Fatalf("decode install: %v", err)
	}
	if installResp.Data.Status != plugins.PackageInstallInstalled || installResp.Data.PackageID != packageID {
		t.Fatalf("install response mismatch: %+v", installResp.Data)
	}

	uninstallReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins/com.astercloud.catalog.router-sync/packages/"+packageID+"/uninstall", nil)
	uninstallRec := httptest.NewRecorder()
	handler.ServeHTTP(uninstallRec, uninstallReq)
	if uninstallRec.Code != http.StatusOK {
		t.Fatalf("uninstall status = %d body=%s", uninstallRec.Code, uninstallRec.Body.String())
	}
	var uninstallResp struct {
		Data plugins.PackageInstallation `json:"data"`
	}
	if err := json.Unmarshal(uninstallRec.Body.Bytes(), &uninstallResp); err != nil {
		t.Fatalf("decode uninstall: %v", err)
	}
	if uninstallResp.Data.Status != plugins.PackageInstallUninstalled || uninstallResp.Data.PackageID != packageID {
		t.Fatalf("uninstall response mismatch: %+v", uninstallResp.Data)
	}

	audit, err := controlService.ListAuditLogs(context.Background(), 20)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	seenInstall := false
	seenUninstall := false
	for _, event := range audit {
		if event.ResourceType != "plugin" {
			continue
		}
		seenInstall = seenInstall || event.Action == "package_install"
		seenUninstall = seenUninstall || event.Action == "package_uninstall"
	}
	if !seenInstall || !seenUninstall {
		t.Fatalf("package install audit missing install=%v uninstall=%v audit=%+v", seenInstall, seenUninstall, audit)
	}
}

func TestAdminPluginsEnableFreePluginAudits(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins/com.asterrouter.notification.webhook/enable", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int            `json:"code"`
		Data plugins.Plugin `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Status != plugins.StatusEnabled {
		t.Fatalf("status = %q", resp.Data.Status)
	}
	audit, err := control.ListAuditLogs(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	for _, event := range audit {
		if event.ResourceType == "plugin" && event.Action == "enable" {
			return
		}
	}
	t.Fatalf("plugin audit event not found: %+v", audit)
}

func TestAdminPluginConfigEndpointsAuditAndMaskSecrets(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})

	body := bytes.NewBufferString(`{"settings":{"min_severity":"critical","alert_types":"project_budget"},"secrets":{"webhook_url":"https://example.com/hook","bearer_token":"secret-token"}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/plugins/com.asterrouter.notification.webhook/config", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int            `json:"code"`
		Data plugins.Config `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Settings["min_severity"] != "critical" || resp.Data.SecretHints["webhook_url"] == "" || strings.Contains(resp.Data.SecretHints["webhook_url"], "example.com/hook") {
		t.Fatalf("config response mismatch: %+v", resp.Data)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins/com.asterrouter.notification.webhook/config", nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", getRec.Code, getRec.Body.String())
	}

	audit, err := control.ListAuditLogs(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	for _, event := range audit {
		if event.ResourceType == "plugin" && event.Action == "configure" {
			return
		}
	}
	t.Fatalf("plugin configure audit event not found: %+v", audit)
}

func TestAdminPluginDeliveriesEndpoint(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer webhook.Close()

	enableReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins/com.asterrouter.notification.webhook/enable", nil)
	enableRec := httptest.NewRecorder()
	handler.ServeHTTP(enableRec, enableReq)
	if enableRec.Code != http.StatusOK {
		t.Fatalf("enable status = %d body=%s", enableRec.Code, enableRec.Body.String())
	}
	configBody := bytes.NewBufferString(`{"settings":{"min_severity":"warning","alert_types":"project_budget"},"secrets":{"webhook_url":"` + webhook.URL + `"}}`)
	configReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/plugins/com.asterrouter.notification.webhook/config", configBody)
	configReq.Header.Set("Content-Type", "application/json")
	configRec := httptest.NewRecorder()
	handler.ServeHTTP(configRec, configReq)
	if configRec.Code != http.StatusOK {
		t.Fatalf("config status = %d body=%s", configRec.Code, configRec.Body.String())
	}

	project, err := control.CreateProject(context.Background(), "tester", controlplane.ProjectRequest{
		Name:               "Delivery Budget Project",
		CostCenter:         "OPS",
		MonthlyBudgetCents: 100,
		Status:             controlplane.ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProject(): %v", err)
	}
	app, err := control.CreateApplication(context.Background(), "tester", controlplane.ApplicationRequest{
		ProjectID:   project.ID,
		Name:        "Delivery App",
		Environment: "prod",
		Owner:       "ops",
		Status:      controlplane.ApplicationStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateApplication(): %v", err)
	}
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		ProjectID:         project.ID,
		ApplicationID:     app.ID,
		Name:              "delivery key",
		ModelAllowlist:    []string{"gpt-delivery"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := control.AuthorizeGatewayModel(context.Background(), created.Key, "gpt-delivery")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := control.RecordGatewayUsage(context.Background(), auth, controlplane.GatewayUsageInput{
		Model:     "gpt-delivery",
		Status:    "forwarded",
		CostCents: 100,
	}); err != nil {
		t.Fatalf("RecordGatewayUsage(): %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins/com.asterrouter.notification.webhook/deliveries", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("deliveries status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []plugins.DeliveryAttempt `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode deliveries: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Status != plugins.DeliveryStatusSucceeded || resp.Data[0].HTTPStatus != http.StatusAccepted {
		t.Fatalf("deliveries mismatch: %+v", resp.Data)
	}
}

func TestAdminPluginLicenseImportEndpointAuditsAndUpdatesStatus(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(): %v", err)
	}
	now := time.Date(2026, 7, 11, 4, 0, 0, 0, time.UTC)
	expiresAt := now.Add(24 * time.Hour)
	envelope := signedServerEnvelope(t, privateKey, "license-key-v1", "license_snapshot", map[string]any{
		"schema_version":   "astercloud.license-snapshot.v1",
		"snapshot_id":      "lss_route_import",
		"snapshot_version": 1,
		"license": map[string]any{
			"public_id":  "lic_route_import",
			"edition":    "enterprise",
			"status":     plugins.LicenseStatusActive,
			"seats":      10,
			"starts_at":  now.Add(-time.Hour).Format(time.RFC3339),
			"expires_at": expiresAt.Format(time.RFC3339),
		},
		"customer": map[string]any{"public_id": "cus_route_import"},
		"sku": map[string]any{
			"public_id": "sku_enterprise",
			"code":      "ASTER-ENT",
			"features":  map[string]any{},
			"limits":    map[string]any{},
		},
		"instance": map[string]any{
			"public_id":          "inst_route_import",
			"fingerprint":        "sha256:00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
			"display_name":       "router-test",
			"first_activated_at": now.Format(time.RFC3339),
		},
		"entitlements": []map[string]any{
			{
				"public_id":    "ent_notification_slack",
				"type":         "plugin",
				"resource_key": "com.asterrouter.notification.slack",
				"status":       plugins.LicenseStatusActive,
				"starts_at":    now.Add(-time.Hour).Format(time.RFC3339),
				"expires_at":   expiresAt.Format(time.RFC3339),
			},
		},
		"issued_at":  now.Format(time.RFC3339),
		"expires_at": expiresAt.Format(time.RFC3339),
	}, now)

	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	pluginService := plugins.NewServiceWithOptions(plugins.NewMemoryRepository(), plugins.ServiceOptions{
		SecretKey: "test-secret",
		OfficialCatalog: plugins.OfficialCatalogConfig{
			Mode:            plugins.CatalogModeOnline,
			URL:             "https://official.example/official/v1/catalog/index",
			PublicKeyID:     "license-key-v1",
			PublicKeyBase64: base64.StdEncoding.EncodeToString(publicKey),
		},
		Now: func() time.Time { return now },
	})
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("Plugin EnsureSeedData(): %v", err)
	}
	handler := New(Options{Config: config.Config{}, SettingsService: settingsService, ControlService: controlService, PluginService: pluginService, SystemService: system.NewService(system.Config{Version: "test", BuildType: "source"})})

	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins/license/status", nil)
	statusRec := httptest.NewRecorder()
	handler.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("initial status = %d body=%s", statusRec.Code, statusRec.Body.String())
	}
	var initialStatus struct {
		Data plugins.LicenseStatus `json:"data"`
	}
	if err := json.Unmarshal(statusRec.Body.Bytes(), &initialStatus); err != nil {
		t.Fatalf("decode initial status: %v", err)
	}
	if initialStatus.Data.Status != "not_imported" || !initialStatus.Data.Configured {
		t.Fatalf("initial license status mismatch: %+v", initialStatus.Data)
	}

	body, err := json.Marshal(map[string]any{
		"envelope":          envelope,
		"activation_secret": "activation-secret",
	})
	if err != nil {
		t.Fatalf("marshal license import: %v", err)
	}
	importReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins/license/import", bytes.NewReader(body))
	importReq.Header.Set("Content-Type", "application/json")
	importRec := httptest.NewRecorder()
	handler.ServeHTTP(importRec, importReq)
	if importRec.Code != http.StatusOK {
		t.Fatalf("import status = %d body=%s", importRec.Code, importRec.Body.String())
	}
	var importResp struct {
		Data plugins.LicenseStatus `json:"data"`
	}
	if err := json.Unmarshal(importRec.Body.Bytes(), &importResp); err != nil {
		t.Fatalf("decode import: %v", err)
	}
	if importResp.Data.LicenseID != "lic_route_import" || importResp.Data.Status != plugins.LicenseStatusActive || len(importResp.Data.Entitlements) != 1 {
		t.Fatalf("import response mismatch: %+v", importResp.Data)
	}

	catalog, err := pluginService.Catalog(context.Background())
	if err != nil {
		t.Fatalf("Catalog(): %v", err)
	}
	slack := findServerPlugin(catalog.Plugins, "com.asterrouter.notification.slack")
	if slack.Status != plugins.StatusDisabled || slack.EntitlementStatus != plugins.EntitlementIncluded {
		t.Fatalf("license should unlock slack plugin: %+v", slack)
	}
	audit, err := controlService.ListAuditLogs(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	for _, event := range audit {
		if event.ResourceType == "plugin" && event.Action == "license_import" && event.ResourceID == "lic_route_import" {
			return
		}
	}
	t.Fatalf("license import audit event not found: %+v", audit)
}

func TestAdminPluginsRejectLockedPaidPlugin(t *testing.T) {
	handler := newTestHandler(t, config.Config{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins/com.asterrouter.notification.slack/enable", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func findServerPlugin(items []plugins.Plugin, id string) plugins.Plugin {
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	return plugins.Plugin{}
}

type serverCatalogEnvelope struct {
	SchemaVersion string          `json:"schema_version"`
	Purpose       string          `json:"purpose"`
	KeyID         string          `json:"key_id"`
	Algorithm     string          `json:"algorithm"`
	IssuedAt      string          `json:"issued_at"`
	ExpiresAt     string          `json:"expires_at,omitempty"`
	Payload       json.RawMessage `json:"payload"`
	Signature     string          `json:"signature"`
}

func signedServerCatalogEnvelope(t *testing.T, privateKey ed25519.PrivateKey, keyID string, payload any, issuedAt time.Time) serverCatalogEnvelope {
	t.Helper()
	return signedServerEnvelope(t, privateKey, keyID, "catalog_index", payload, issuedAt)
}

func signedServerPackageEnvelope(t *testing.T, privateKey ed25519.PrivateKey, keyID string, payload any, issuedAt time.Time) serverCatalogEnvelope {
	t.Helper()
	return signedServerEnvelope(t, privateKey, keyID, "plugin_package", payload, issuedAt)
}

func signedServerEnvelope(t *testing.T, privateKey ed25519.PrivateKey, keyID string, purpose string, payload any, issuedAt time.Time) serverCatalogEnvelope {
	t.Helper()
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal signed payload: %v", err)
	}
	envelope := serverCatalogEnvelope{
		SchemaVersion: "astercloud.signed-envelope.v1",
		Purpose:       purpose,
		KeyID:         keyID,
		Algorithm:     "Ed25519",
		IssuedAt:      issuedAt.UTC().Format(time.RFC3339Nano),
		Payload:       payloadJSON,
	}
	unsigned := struct {
		SchemaVersion string          `json:"schema_version"`
		Purpose       string          `json:"purpose"`
		KeyID         string          `json:"key_id"`
		Algorithm     string          `json:"algorithm"`
		IssuedAt      string          `json:"issued_at"`
		ExpiresAt     string          `json:"expires_at,omitempty"`
		Payload       json.RawMessage `json:"payload"`
	}{
		SchemaVersion: envelope.SchemaVersion,
		Purpose:       envelope.Purpose,
		KeyID:         envelope.KeyID,
		Algorithm:     envelope.Algorithm,
		IssuedAt:      envelope.IssuedAt,
		ExpiresAt:     envelope.ExpiresAt,
		Payload:       envelope.Payload,
	}
	raw, err := json.Marshal(unsigned)
	if err != nil {
		t.Fatalf("marshal unsigned envelope: %v", err)
	}
	canonical, err := jcs.Transform(raw)
	if err != nil {
		t.Fatalf("canonicalize envelope: %v", err)
	}
	envelope.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, canonical))
	return envelope
}
