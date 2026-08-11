package plugins

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMonitorPricePackageInstallProxyRestartAndFrontend(t *testing.T) {
	packagePath := os.Getenv("ASTER_TEST_MONITORPRICE_PACKAGE")
	if packagePath == "" {
		t.Skip("ASTER_TEST_MONITORPRICE_PACKAGE is not configured")
	}
	raw, err := os.ReadFile(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	pluginID := "com.asterrouter.monitorprice.workbench"
	packageID := "pkg_monitorprice_e2e"
	version := strings.TrimSpace(os.Getenv("ASTER_TEST_MONITORPRICE_VERSION"))
	if version == "" {
		version = "0.1.1"
	}
	now := time.Now().UTC()
	digest := sha256.Sum256(raw)
	root := t.TempDir()
	cachePath := filepath.Join(root, "monitorprice.pkg")
	if err := os.WriteFile(cachePath, raw, 0600); err != nil {
		t.Fatal(err)
	}

	repo := NewMemoryRepository()
	if err := repo.SavePlugin(context.Background(), Plugin{
		ID: pluginID, PluginID: pluginID, Name: "MonitorPrice 采购比价", Description: "ChatGPT 账号与 API 采购比价",
		Category: "finops", Type: "remote", Tier: TierFreeCore, Version: version, Vendor: "AsterCloud",
		Status: StatusEnabled, EntitlementStatus: EntitlementFree,
		Configurable: true, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	compatibility := fmt.Sprintf(`[{"core_version_range":">=0.16.0","os":%q,"arch":%q,"result":"compatible"}]`, runtime.GOOS, runtime.GOARCH)
	if err := repo.SavePackage(context.Background(), packageRecord{
		PluginID: pluginID, PluginSlug: "monitorprice", PackageID: packageID, Version: version, Channel: "stable",
		OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: hex.EncodeToString(digest[:]), SizeBytes: int64(len(raw)),
		CompatibilityJSON: compatibility, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePackageCache(context.Background(), packageCacheRecord{
		PackageID: packageID, PluginID: pluginID, Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH,
		SHA256: hex.EncodeToString(digest[:]), SizeBytes: int64(len(raw)), CachePath: cachePath,
		Status: PackageCacheStatusCached, CachedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	dataRoot := filepath.Join(root, "data")
	svc := NewServiceWithOptions(repo, ServiceOptions{
		PackageCacheDir: filepath.Join(root, "cache"), PluginActiveDir: filepath.Join(root, "active"), PluginDataDir: dataRoot,
		CoreVersion: "0.18.0", TargetOS: runtime.GOOS, TargetArch: runtime.GOARCH,
	})
	defer svc.Shutdown(context.Background())

	installation, err := svc.InstallPackage(context.Background(), pluginID, packageID)
	if err != nil {
		t.Fatal(err)
	}
	if installation.Status != PackageInstallInstalled {
		t.Fatalf("installation = %#v", installation)
	}
	workbench, err := svc.PluginWorkbench(context.Background(), pluginID)
	if err != nil || !bytes.Contains(workbench, []byte(`"workbench"`)) {
		t.Fatalf("frontend workbench=%s err=%v", workbench, err)
	}
	catalog, err := svc.Catalog(context.Background())
	if err != nil || len(catalog.Plugins) != 1 || !catalog.Plugins[0].FrontendAvailable {
		t.Fatalf("catalog=%#v err=%v", catalog, err)
	}

	dashboard := proxyMonitorPrice(t, svc, pluginID, http.MethodGet, "/actions/bootstrap", nil)
	var initial struct {
		Comparison struct {
			Candidates []struct {
				Best       bool    `json:"best"`
				MarketTier string  `json:"market_tier"`
				TotalCost  float64 `json:"total_cost_cny"`
			} `json:"candidates"`
			Recommendation struct {
				Status   string `json:"status"`
				WinnerID string `json:"winner_id"`
			} `json:"recommendation"`
		} `json:"comparison"`
	}
	if err := json.Unmarshal(dashboard, &initial); err != nil {
		t.Fatal(err)
	}
	bestCount := 0
	for _, candidate := range initial.Comparison.Candidates {
		if candidate.Best {
			bestCount++
		}
	}
	if bestCount != 0 || initial.Comparison.Recommendation.WinnerID != "" || initial.Comparison.Recommendation.Status != "insufficient_evidence" {
		t.Fatalf("comparison=%#v", initial.Comparison)
	}

	proxyMonitorPrice(t, svc, pluginID, http.MethodPost, "/actions/quotes", []byte(`{"kind":"account","supplier":"NVTokens","market_tier":"Plus","price_cny":5.4,"service_fee_rate":0}`))
	proxyMonitorPrice(t, svc, pluginID, http.MethodPost, "/actions/quotes", []byte(`{"kind":"api","supplier":"第三方供应商","market_tier":"API","model":"test-model","price_cny":0.08,"service_fee_rate":0}`))
	proxyMonitorPrice(t, svc, pluginID, http.MethodPost, "/actions/capacities", []byte(`{"market_tier":"Plus","standard_tokens_p10":80000000,"standard_tokens_p50":100000000,"standard_tokens_p90":120000000,"sample_accounts":1,"sample_windows":1}`))
	proxyMonitorPrice(t, svc, pluginID, http.MethodPost, "/actions/comparisons/run", []byte(`{}`))
	dashboard = proxyMonitorPrice(t, svc, pluginID, http.MethodGet, "/actions/bootstrap", nil)
	var measured struct {
		Comparison struct {
			Candidates []struct {
				Best       bool    `json:"best"`
				MarketTier string  `json:"market_tier"`
				TotalCost  float64 `json:"total_cost_cny"`
			} `json:"candidates"`
			Recommendation struct {
				WinnerID string  `json:"winner_id"`
				Savings  float64 `json:"savings_vs_api_percent"`
			} `json:"recommendation"`
		} `json:"comparison"`
	}
	if err := json.Unmarshal(dashboard, &measured); err != nil {
		t.Fatal(err)
	}
	bestCount = 0
	for _, candidate := range measured.Comparison.Candidates {
		if candidate.Best {
			bestCount++
			if candidate.MarketTier != "Plus" || candidate.TotalCost != 162 {
				t.Fatalf("best candidate=%#v", candidate)
			}
		}
	}
	if bestCount != 1 || measured.Comparison.Recommendation.WinnerID != "account-plus" || measured.Comparison.Recommendation.Savings < 15.62 {
		t.Fatalf("comparison=%#v", measured.Comparison)
	}

	scenario := []byte(`{"id":"default","name":"持久化场景","daily_standard_tokens":90000000,"days":30,"reliability":"p50"}`)
	proxyMonitorPrice(t, svc, pluginID, http.MethodPut, "/actions/scenario", scenario)
	if _, err := svc.Disable(context.Background(), pluginID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Enable(context.Background(), pluginID); err != nil {
		t.Fatal(err)
	}
	dashboard = proxyMonitorPrice(t, svc, pluginID, http.MethodGet, "/actions/bootstrap", nil)
	var restarted struct {
		Scenario struct {
			Name  string  `json:"name"`
			Daily float64 `json:"daily_standard_tokens"`
		} `json:"scenario"`
	}
	if err := json.Unmarshal(dashboard, &restarted); err != nil {
		t.Fatal(err)
	}
	if restarted.Scenario.Name != "持久化场景" || restarted.Scenario.Daily != 90_000_000 {
		t.Fatalf("scenario did not persist: %#v", restarted.Scenario)
	}
	dataPath := filepath.Join(dataRoot, pluginID, "monitorprice.db")
	if info, err := os.Stat(dataPath); err != nil || info.Size() == 0 {
		t.Fatalf("plugin data path=%s info=%v err=%v", dataPath, info, err)
	}
}

func proxyMonitorPrice(t *testing.T, svc *Service, pluginID, method, path string, body []byte) []byte {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := svc.ProxySidecarHTTP(context.Background(), pluginID, path, req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("proxy %s %s status=%d body=%s", method, path, response.StatusCode, raw)
	}
	return raw
}
