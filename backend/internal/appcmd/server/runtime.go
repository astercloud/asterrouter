package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/buildinfo"
	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	httpserver "github.com/astercloud/asterrouter/backend/internal/server"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
)

type runtime struct {
	storageMode string
	handler     http.Handler

	settingsRepo settings.Repository
	controlRepo  controlplane.Repository
	pluginRepo   plugins.Repository
	exportStore  httpserver.CSVExportJobStore

	settingsService *settings.Service
	controlService  *controlplane.Service
	pluginService   *plugins.Service
	systemService   *system.Service
	durableJobs     *controlplane.DurableAIJobRuntime

	closeInfrastructure func()
	cancel              context.CancelFunc
	backgroundErrors    chan error
	waitGroup           sync.WaitGroup
	closeOnce           sync.Once
	closeErr            error
}

// applyE2EOIDCSettings is opt-in and exists only for the isolated browser
// journey. It configures the real runtime before identity services initialize.
func applyE2EOIDCSettings(ctx context.Context, service *settings.Service, current *settings.AdminSettings, databaseURL string) error {
	if strings.TrimSpace(os.Getenv("ASTER_E2E_OIDC_ENABLED")) != "1" {
		return nil
	}
	if !e2eOIDCStorageAllowed(databaseURL) {
		return fmt.Errorf("ASTER_E2E_OIDC_ENABLED requires isolated memory or an explicitly available PostgreSQL database with an e2e or test name token")
	}
	port := strings.TrimSpace(os.Getenv("ASTER_E2E_OIDC_PORT"))
	if port == "" {
		return fmt.Errorf("ASTER_E2E_OIDC_PORT is required when ASTER_E2E_OIDC_ENABLED=1")
	}
	next := *current
	next.PublicBaseURL = "https://127.0.0.1:" + port
	next.OIDCEnabled = true
	next.OIDCProviderName = "Fake OIDC"
	next.OIDCIssuerURL = next.PublicBaseURL + "/oidc"
	next.OIDCClientID = envOr("ASTER_E2E_OIDC_CLIENT_ID", "asterrouter-e2e")
	next.OIDCClientSecret = envOr("ASTER_E2E_OIDC_CLIENT_SECRET", "asterrouter-e2e-secret")
	next.OIDCRequireVerifiedEmail = true
	updated, err := service.Update(ctx, next)
	if err != nil {
		return fmt.Errorf("configure E2E OIDC settings: %w", err)
	}
	*current = updated
	return nil
}

var e2eDatabaseTokenSeparator = regexp.MustCompile(`[^a-z0-9]+`)

func e2eOIDCStorageAllowed(databaseURL string) bool {
	if strings.TrimSpace(os.Getenv("ASTER_DEV_ISOLATED_MEMORY")) == "1" {
		return true
	}
	if strings.TrimSpace(os.Getenv("ASTER_E2E_POSTGRES_AVAILABLE")) != "1" {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(databaseURL))
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || strings.TrimSpace(parsed.Hostname()) == "" {
		return false
	}
	databaseName, err := url.PathUnescape(strings.TrimPrefix(parsed.Path, "/"))
	if err != nil || strings.Contains(databaseName, "/") {
		return false
	}
	for _, token := range e2eDatabaseTokenSeparator.Split(strings.ToLower(databaseName), -1) {
		if token == "e2e" || token == "test" {
			return true
		}
	}
	return false
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func newRuntime(ctx context.Context, cfg *config.Server) (_ *runtime, err error) {
	rt := &runtime{closeInfrastructure: func() {}, backgroundErrors: make(chan error, 1)}
	defer func() {
		if err != nil {
			_ = rt.Close(context.Background())
		}
	}()

	if rt.settingsRepo, rt.storageMode, err = settings.NewRepository(ctx, cfg.Storage.DatabaseURL); err != nil {
		return nil, fmt.Errorf("initialize settings repository: %w", err)
	}
	if rt.controlRepo, _, err = controlplane.NewRepository(ctx, cfg.Storage.DatabaseURL); err != nil {
		return nil, fmt.Errorf("initialize control plane repository: %w", err)
	}
	if rt.pluginRepo, _, err = plugins.NewRepository(ctx, cfg.Storage.DatabaseURL); err != nil {
		return nil, fmt.Errorf("initialize plugin repository: %w", err)
	}
	if rt.exportStore, err = httpserver.NewCSVExportJobStore(ctx, cfg.Storage.DatabaseURL); err != nil {
		return nil, fmt.Errorf("initialize export job store: %w", err)
	}

	rt.settingsService = settings.NewService(rt.settingsRepo, settings.ServiceOptions{
		Version: buildinfo.Version, StorageMode: rt.storageMode, DemoMode: cfg.Bootstrap.DemoMode, SecretKey: cfg.Security.SecretKey,
	})
	if err = rt.settingsService.MigrateLegacySensitiveSettings(ctx); err != nil {
		return nil, fmt.Errorf("migrate sensitive settings: %w", err)
	}
	adminSettings, err := rt.settingsService.Admin(ctx)
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	if err := applyE2EOIDCSettings(ctx, rt.settingsService, &adminSettings, cfg.Storage.DatabaseURL); err != nil {
		return nil, err
	}

	oidcService, feishuService, githubOAuthService, googleOAuthService, dingTalkService, err := newIdentityServices(ctx, rt.settingsService, adminSettings)
	if err != nil {
		return nil, err
	}

	rt.controlService = controlplane.NewService(rt.controlRepo, "/v1", cfg.Security.SecretKey)
	if err = configureArtifactStore(ctx, cfg.Artifacts, rt.controlService); err != nil {
		return nil, err
	}
	if err = rt.controlService.SetAIJobAdmissionLimits(controlplane.AIJobAdmissionLimits{
		Organization: cfg.Jobs.Queue.Limits.Organization, Application: cfg.Jobs.Queue.Limits.Application, Principal: cfg.Jobs.Queue.Limits.Principal,
	}); err != nil {
		return nil, fmt.Errorf("configure durable AI job admission: %w", err)
	}
	var deliveryQueue controlplane.AIJobDeliveryQueue
	deliveryQueue, rt.closeInfrastructure, err = configureAIJobInfrastructure(ctx, cfg.Jobs, cfg.Storage.Redis, rt.controlService)
	if err != nil {
		return nil, fmt.Errorf("initialize durable AI job infrastructure: %w", err)
	}
	if err = rt.controlService.EnsureSeedData(ctx); err != nil {
		return nil, fmt.Errorf("seed control plane repository: %w", err)
	}

	authService := auth.NewService(auth.Config{
		Username: cfg.Security.Admin.Username, Password: cfg.Security.Admin.Password,
		LegacyAdminToken: cfg.Security.Admin.Token, SecretKey: cfg.Security.SecretKey, DemoMode: cfg.Bootstrap.DemoMode,
	})
	localAdminUsername, localAdminPassword := authService.BootstrapIdentity()
	localAdminDefaults := controlplane.WorkspaceUserDefaults{
		ConcurrencyLimit: adminSettings.DefaultConcurrency, RPMLimit: adminSettings.DefaultRPM,
	}
	localAdmin, err := rt.controlService.EnsureLocalAdmin(ctx, localAdminUsername, localAdminPassword, localAdminDefaults)
	if err != nil {
		return nil, fmt.Errorf("initialize local administrator account: %w", err)
	}
	authService.SetPasswordHash(localAdmin.PasswordHash)

	rt.pluginService = plugins.NewServiceWithOptions(rt.pluginRepo, plugins.ServiceOptions{
		SecretKey: cfg.Security.SecretKey,
		OfficialCatalog: plugins.OfficialCatalogConfig{
			Mode: cfg.Official.Catalog.Mode, BootstrapURL: cfg.Official.Catalog.BootstrapURL,
			URL: cfg.Official.Catalog.URL, ServicesURL: cfg.Official.Catalog.ServicesURL,
			LicenseURL: cfg.Official.License.URL, RedeemURL: cfg.Official.License.RedeemURL,
			PublicKeyID: cfg.Official.Catalog.KeyID, PublicKeyBase64: cfg.Official.Catalog.PublicKey,
		},
		OfficialLicense: plugins.OfficialLicenseConfig{
			URL: cfg.Official.License.URL, RedeemURL: cfg.Official.License.RedeemURL,
			PublicKeyID: cfg.Official.License.KeyID, PublicKeyBase64: cfg.Official.License.PublicKey,
			InstanceID: cfg.Official.Instance.ID, Fingerprint: cfg.Official.Instance.Fingerprint,
			DisplayName: cfg.Official.Instance.DisplayName,
		},
		PackageCacheDir: cfg.Plugins.CacheDir, PluginActiveDir: cfg.Plugins.ActiveDir, PluginDataDir: cfg.Plugins.DataDir,
		PluginHostURL: cfg.Plugins.HostURL, CoreVersion: buildinfo.Version, ArtifactSinkRegistry: rt.controlService,
	})
	if err = rt.pluginService.EnsureSeedData(ctx); err != nil {
		return nil, fmt.Errorf("seed plugin repository: %w", err)
	}
	if err = rt.pluginService.StartEnabledSidecars(ctx); err != nil {
		return nil, fmt.Errorf("start enabled plugin sidecars: %w", err)
	}
	if err = rt.pluginService.StartEnabledArtifactSinks(ctx); err != nil {
		return nil, fmt.Errorf("start enabled artifact sinks: %w", err)
	}
	if rt.durableJobs, err = controlplane.NewDurableAIJobRuntime(rt.controlService, deliveryQueue, rt.pluginService, controlplane.DurableAIJobRuntimeConfig{}); err != nil {
		return nil, fmt.Errorf("initialize durable AI job runtime: %w", err)
	}

	officialCatalogURL, officialCatalogKeyID, officialCatalogPublicKey := systemCatalog(cfg.Official.Catalog)
	rt.systemService = system.NewService(system.Config{
		Version: buildinfo.Version, BuildType: buildinfo.BuildType, ManifestURL: cfg.Official.UpdateManifestURL,
		OfficialCatalogURL: officialCatalogURL, OfficialKeyID: officialCatalogKeyID, OfficialPublicKey: officialCatalogPublicKey,
		AllowRestart: cfg.Maintenance.AllowRestart, DatabaseURL: cfg.Storage.DatabaseURL,
		PluginCacheDir: cfg.Plugins.CacheDir, PluginActiveDir: cfg.Plugins.ActiveDir,
		BackupDir: cfg.Maintenance.BackupDir, DiagnosticDir: cfg.Maintenance.DiagnosticDir,
		MaxArchiveBytes: cfg.Maintenance.MaxArchiveBytes,
	})

	rt.handler = httpserver.New(httpserver.Options{
		Runtime: httpserver.RuntimeConfig{
			AdminToken: cfg.Security.Admin.Token, MetricsToken: cfg.Observability.MetricsToken,
			DemoMode: cfg.Bootstrap.DemoMode, FrontendDir: cfg.HTTP.FrontendDir,
		},
		AuthService: authService, OIDCService: oidcService, FeishuService: feishuService,
		GitHubOAuthService: githubOAuthService, GoogleOAuthService: googleOAuthService, DingTalkService: dingTalkService,
		SettingsService: rt.settingsService, ControlService: rt.controlService,
		PluginService: rt.pluginService, SystemService: rt.systemService, ExportJobStore: rt.exportStore,
		DurableAIJobs: rt.durableJobs, AIJobRuntime: rt.durableJobs,
	})
	return rt, nil
}

func (rt *runtime) Start(ctx context.Context) {
	backgroundCtx, cancel := context.WithCancel(ctx)
	rt.cancel = cancel
	rt.startBackground(backgroundCtx)
}

func (rt *runtime) Errors() <-chan error {
	return rt.backgroundErrors
}

func (rt *runtime) Close(ctx context.Context) error {
	if rt == nil {
		return nil
	}
	rt.closeOnce.Do(func() {
		if rt.cancel != nil {
			rt.cancel()
		}
		waitDone := make(chan struct{})
		go func() {
			rt.waitGroup.Wait()
			close(waitDone)
		}()
		select {
		case <-waitDone:
		case <-ctx.Done():
			rt.closeErr = errors.Join(rt.closeErr, ctx.Err())
		}
		if rt.pluginService != nil {
			rt.closeErr = errors.Join(rt.closeErr, rt.pluginService.Shutdown(context.WithoutCancel(ctx)))
		}
		if rt.closeInfrastructure != nil {
			rt.closeInfrastructure()
		}
		for _, closeResource := range []func() error{
			closeCSVExportStore(rt.exportStore), closePluginRepository(rt.pluginRepo),
			closeControlRepository(rt.controlRepo), closeSettingsRepository(rt.settingsRepo),
		} {
			rt.closeErr = errors.Join(rt.closeErr, closeResource())
		}
	})
	return rt.closeErr
}

func (rt *runtime) startBackground(ctx context.Context) {
	rt.launch(func() {
		rt.controlService.RunChannelMonitor(ctx, func(ctx context.Context) (controlplane.ChannelMonitorConfig, error) {
			current, err := rt.settingsService.Admin(ctx)
			if err != nil {
				return controlplane.ChannelMonitorConfig{}, err
			}
			return controlplane.ChannelMonitorConfig{
				Enabled: current.ChannelMonitorEnabled, Interval: time.Duration(current.ChannelMonitorIntervalSeconds) * time.Second,
			}, nil
		}, func(operation string, err error) {
			slog.Error("channel monitor failed", "operation", operation, "error", err)
		})
	})
	rt.launch(func() {
		rt.controlService.RunArtifactLifecycleScheduler(ctx, 30*time.Second, 100, logBackgroundError("artifact lifecycle scheduler"))
	})
	rt.launch(func() {
		rt.controlService.RunEffectivePricingDecisionMonitor(ctx, time.Minute, logBackgroundError("effective pricing decision monitor"))
	})
	rt.launch(func() {
		rt.controlService.RunProviderBillingSyncScheduler(ctx, time.Minute, logBackgroundError("provider billing sync scheduler"))
	})
	rt.launch(func() {
		httpserver.RunBackupScheduler(ctx, rt.systemService, rt.settingsService, rt.controlService, logBackgroundError("backup scheduler"))
	})
	rt.launch(func() {
		if err := rt.durableJobs.Run(ctx, func(component string, err error) {
			slog.Error("durable AI job runtime component failed", "component", component, "error", err)
		}); err != nil && !errors.Is(err, context.Canceled) {
			select {
			case rt.backgroundErrors <- err:
			default:
			}
		}
	})
}

func (rt *runtime) launch(run func()) {
	rt.waitGroup.Add(1)
	go func() {
		defer rt.waitGroup.Done()
		run()
	}()
}

func logBackgroundError(component string) func(error) {
	return func(err error) { slog.Error("background component failed", "component", component, "error", err) }
}

func systemCatalog(cfg config.OfficialCatalog) (string, string, string) {
	if cfg.Mode != "online" {
		return "", "", ""
	}
	url := cfg.URL
	if url == "" {
		url = cfg.BootstrapURL
	}
	return url, cfg.KeyID, cfg.PublicKey
}

func closeCSVExportStore(store httpserver.CSVExportJobStore) func() error {
	return func() error {
		if store == nil {
			return nil
		}
		return store.Close()
	}
}

func closePluginRepository(repo plugins.Repository) func() error {
	return func() error {
		if repo == nil {
			return nil
		}
		return repo.Close()
	}
}

func closeControlRepository(repo controlplane.Repository) func() error {
	return func() error {
		if repo == nil {
			return nil
		}
		return repo.Close()
	}
}

func closeSettingsRepository(repo settings.Repository) func() error {
	return func() error {
		if repo == nil {
			return nil
		}
		return repo.Close()
	}
}
