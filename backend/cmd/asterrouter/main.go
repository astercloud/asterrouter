package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/buildinfo"
	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	operatorcore "github.com/astercloud/asterrouter/backend/internal/operator"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/astercloud/asterrouter/backend/internal/server"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
	"github.com/redis/go-redis/v9"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Printf("asterrouter %s\ncommit: %s\nbuilt: %s\nbuild_type: %s\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date, buildinfo.BuildType)
		return
	}

	cfg := config.Load()
	if err := config.ValidateRuntime(cfg); err != nil {
		log.Fatalf("invalid runtime configuration: %v", err)
	}
	repo, storageMode, err := settings.NewRepository(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("initialize settings repository: %v", err)
	}
	defer repo.Close()

	controlRepo, _, err := controlplane.NewRepository(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("initialize control plane repository: %v", err)
	}
	defer controlRepo.Close()
	operatorRepo, err := operatorcore.NewRepository(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("initialize operator repository: %v", err)
	}
	defer operatorRepo.Close()
	pluginRepo, _, err := plugins.NewRepository(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("initialize plugin repository: %v", err)
	}
	defer pluginRepo.Close()
	exportJobStore, err := server.NewCSVExportJobStore(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("initialize export job store: %v", err)
	}
	defer exportJobStore.Close()

	settingsService := settings.NewService(repo, settings.ServiceOptions{
		Version:         cfg.Version,
		EnabledProfiles: cfg.Profiles,
		DefaultProfile:  cfg.DefaultProfile,
		StorageMode:     storageMode,
		DemoMode:        cfg.DemoMode,
	})
	if err := settingsService.BootstrapProfile(context.Background()); err != nil {
		log.Fatalf("bootstrap profile: %v", err)
	}
	adminSettings, err := settingsService.Admin(context.Background())
	if err != nil {
		log.Fatalf("load settings: %v", err)
	}
	oidcService, err := auth.NewOIDCService(auth.OIDCConfig{
		Enabled:              adminSettings.OIDCEnabled,
		RequireVerifiedEmail: adminSettings.OIDCRequireVerifiedEmail,
		IssuerURL:            adminSettings.OIDCIssuerURL,
		ClientID:             adminSettings.OIDCClientID,
		RedirectURL:          strings.TrimRight(adminSettings.PublicBaseURL, "/") + "/api/v1/auth/oidc/callback",
	})
	if err != nil {
		log.Fatalf("initialize oidc: %v", err)
	}
	if adminSettings.OIDCEnabled {
		if err := oidcService.Initialize(context.Background()); err != nil {
			log.Fatalf("initialize oidc provider: %v", err)
		}
	}
	feishuSecret, err := settingsService.FeishuSecret(context.Background())
	if err != nil {
		log.Fatalf("load feishu secret: %v", err)
	}
	feishuService, err := auth.NewFeishuService(auth.FeishuConfig{Enabled: adminSettings.FeishuEnabled, Region: adminSettings.FeishuRegion, AppID: adminSettings.FeishuAppID, AppSecret: feishuSecret, RedirectURL: strings.TrimRight(adminSettings.PublicBaseURL, "/") + "/api/v1/auth/feishu/callback"})
	if err != nil {
		log.Fatalf("initialize feishu login: %v", err)
	}
	githubSecret, googleSecret, err := settingsService.SocialOAuthSecrets(context.Background())
	if err != nil {
		log.Fatalf("load social OAuth secrets: %v", err)
	}
	githubOAuthService, err := auth.NewSocialOAuthService(auth.SocialOAuthConfig{Provider: "github", Enabled: adminSettings.GitHubOAuthEnabled, ClientID: adminSettings.GitHubOAuthClientID, ClientSecret: githubSecret, RedirectURL: strings.TrimRight(adminSettings.PublicBaseURL, "/") + "/api/v1/auth/oauth/github/callback"})
	if err != nil {
		log.Fatalf("initialize GitHub OAuth: %v", err)
	}
	googleOAuthService, err := auth.NewSocialOAuthService(auth.SocialOAuthConfig{Provider: "google", Enabled: adminSettings.GoogleOAuthEnabled, ClientID: adminSettings.GoogleOAuthClientID, ClientSecret: googleSecret, RedirectURL: strings.TrimRight(adminSettings.PublicBaseURL, "/") + "/api/v1/auth/oauth/google/callback"})
	if err != nil {
		log.Fatalf("initialize Google OAuth: %v", err)
	}
	dingTalkSecret, err := settingsService.DingTalkSecret(context.Background())
	if err != nil {
		log.Fatalf("load DingTalk secret: %v", err)
	}
	dingTalkService, err := auth.NewDingTalkService(auth.DingTalkConfig{Enabled: adminSettings.DingTalkEnabled, ClientID: adminSettings.DingTalkClientID, ClientSecret: dingTalkSecret, RedirectURL: strings.TrimRight(adminSettings.PublicBaseURL, "/") + "/api/v1/auth/dingtalk/callback"})
	if err != nil {
		log.Fatalf("initialize DingTalk login: %v", err)
	}
	controlService := controlplane.NewService(controlRepo, "/v1", cfg.SecretKey)
	switch strings.TrimSpace(cfg.ArtifactStoreDriver) {
	case "local":
		artifactStore, err := controlplane.NewLocalArtifactStore(cfg.ArtifactLocalRoot)
		if err != nil {
			log.Fatalf("initialize local artifact store: %v", err)
		}
		if err := controlService.SetArtifactStore(artifactStore); err != nil {
			log.Fatalf("configure local artifact store: %v", err)
		}
	case "s3":
		artifactStore, err := controlplane.NewS3ArtifactStore(context.Background(), controlplane.S3ArtifactStoreConfig{
			Endpoint: cfg.ArtifactS3Endpoint, Region: cfg.ArtifactS3Region, Bucket: cfg.ArtifactS3Bucket,
			Prefix: cfg.ArtifactS3Prefix, AccessKey: cfg.ArtifactS3AccessKey, SecretKey: cfg.ArtifactS3SecretKey,
			PathStyle: cfg.ArtifactS3PathStyle,
		})
		if err != nil {
			log.Fatalf("initialize S3 artifact store: %v", err)
		}
		if err := controlService.SetArtifactStore(artifactStore); err != nil {
			log.Fatalf("configure S3 artifact store: %v", err)
		}
	}
	if err := controlService.SetAIJobAdmissionLimits(controlplane.AIJobAdmissionLimits{
		Profile: cfg.AIJobQueueProfileLimit, Tenant: cfg.AIJobQueueTenantLimit, Principal: cfg.AIJobQueuePrincipalLimit,
	}); err != nil {
		log.Fatalf("configure durable ai job admission: %v", err)
	}
	deliveryQueue, closeAIJobInfrastructure, err := configureAIJobInfrastructure(context.Background(), cfg, controlService)
	if err != nil {
		log.Fatalf("initialize durable ai job infrastructure: %v", err)
	}
	defer closeAIJobInfrastructure()
	if err := controlService.EnsureSeedData(context.Background()); err != nil {
		log.Fatalf("seed control plane repository: %v", err)
	}
	if adminSettings.DefaultProfile == controlplane.ProfileScopePlatform {
		if err := controlService.EnsurePlatformBootstrap(context.Background()); err != nil {
			log.Fatalf("initialize platform domain: %v", err)
		}
	}
	authService := auth.NewService(auth.Config{
		Username:         cfg.AdminUsername,
		Password:         cfg.AdminPassword,
		LegacyAdminToken: cfg.AdminToken,
		SecretKey:        cfg.SecretKey,
		DemoMode:         cfg.DemoMode,
	})
	localAdminUsername, localAdminPassword := authService.BootstrapIdentity()
	localAdminDefaults := controlplane.WorkspaceUserDefaults{BalanceCents: adminSettings.DefaultBalanceCents, ConcurrencyLimit: adminSettings.DefaultConcurrency, RPMLimit: adminSettings.DefaultRPM}
	localAdmin, err := controlService.EnsureLocalAdmin(context.Background(), localAdminUsername, localAdminPassword, localAdminDefaults)
	if err != nil {
		log.Fatalf("initialize local administrator account: %v", err)
	}
	authService.SetPasswordHash(localAdmin.PasswordHash)
	operatorService := operatorcore.NewService(operatorRepo, controlService)
	operatorService.SetRiskConfigProvider(func(ctx context.Context) (operatorcore.RiskRuntimeConfig, error) {
		current, err := settingsService.Admin(ctx)
		if err != nil {
			return operatorcore.RiskRuntimeConfig{}, err
		}
		return operatorcore.RiskRuntimeConfig{
			Enabled:      current.RiskControlEnabled,
			AutoBlock:    current.CyberSessionBlockEnabled,
			BlockTimeout: time.Duration(current.CyberSessionBlockTTLSeconds) * time.Second,
		}, nil
	})
	controlService.SetUsageObserver(operatorService)
	monitorCtx, stopChannelMonitor := context.WithCancel(context.Background())
	defer stopChannelMonitor()
	go controlService.RunChannelMonitor(monitorCtx, func(ctx context.Context) (controlplane.ChannelMonitorConfig, error) {
		current, err := settingsService.Admin(ctx)
		if err != nil {
			return controlplane.ChannelMonitorConfig{}, err
		}
		return controlplane.ChannelMonitorConfig{
			Enabled:  current.ChannelMonitorEnabled,
			Interval: time.Duration(current.ChannelMonitorIntervalSeconds) * time.Second,
		}, nil
	}, func(operation string, err error) {
		log.Printf("channel monitor: %s: %v", operation, err)
	})
	go controlService.RunCustomerNotificationScheduler(monitorCtx, func(err error) {
		log.Printf("customer notification scheduler: %v", err)
	})
	go controlService.RunPlatformUsageDeliveryScheduler(monitorCtx, func(err error) {
		log.Printf("platform usage delivery scheduler: %v", err)
	})
	go controlService.RunArtifactLifecycleScheduler(monitorCtx, 30*time.Second, 100, func(err error) {
		log.Printf("artifact lifecycle scheduler: %v", err)
	})
	pluginService := plugins.NewServiceWithOptions(pluginRepo, plugins.ServiceOptions{
		SecretKey: cfg.SecretKey,
		OfficialCatalog: plugins.OfficialCatalogConfig{
			Mode:            cfg.CatalogMode,
			BootstrapURL:    cfg.CatalogBootstrapURL,
			URL:             cfg.CatalogURL,
			ServicesURL:     cfg.OfficialServicesURL,
			LicenseURL:      cfg.LicenseURL,
			RedeemURL:       cfg.RedeemURL,
			PublicKeyID:     cfg.CatalogKeyID,
			PublicKeyBase64: cfg.CatalogPublicKey,
		},
		OfficialLicense: plugins.OfficialLicenseConfig{
			URL:             cfg.LicenseURL,
			RedeemURL:       cfg.RedeemURL,
			PublicKeyID:     cfg.LicenseKeyID,
			PublicKeyBase64: cfg.LicensePublicKey,
			InstanceID:      cfg.InstanceID,
			Fingerprint:     cfg.InstanceFingerprint,
			DisplayName:     cfg.InstanceDisplayName,
		},
		PackageCacheDir:      cfg.PluginCacheDir,
		PluginActiveDir:      cfg.PluginActiveDir,
		PluginHostURL:        cfg.PluginHostURL,
		CoreVersion:          cfg.Version,
		ArtifactSinkRegistry: controlService,
	})
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		log.Fatalf("seed plugin repository: %v", err)
	}
	if err := pluginService.StartEnabledSidecars(context.Background()); err != nil {
		log.Fatalf("start enabled plugin sidecars: %v", err)
	}
	if err := pluginService.StartEnabledArtifactSinks(context.Background()); err != nil {
		log.Fatalf("start enabled artifact sink plugins: %v", err)
	}
	durableJobRuntime, err := controlplane.NewDurableAIJobRuntime(controlService, deliveryQueue, pluginService, controlplane.DurableAIJobRuntimeConfig{})
	if err != nil {
		log.Fatalf("initialize durable ai job runtime: %v", err)
	}
	durableJobRuntimeDone := make(chan error, 1)
	go func() {
		durableJobRuntimeDone <- durableJobRuntime.Run(monitorCtx, func(component string, err error) {
			log.Printf("durable ai job runtime: %s: %v", component, err)
		})
	}()
	officialCatalogURL := ""
	officialCatalogKeyID := ""
	officialCatalogPublicKey := ""
	if cfg.CatalogMode == "online" {
		officialCatalogURL = cfg.CatalogURL
		if officialCatalogURL == "" {
			officialCatalogURL = cfg.CatalogBootstrapURL
		}
		officialCatalogKeyID = cfg.CatalogKeyID
		officialCatalogPublicKey = cfg.CatalogPublicKey
	}
	systemService := system.NewService(system.Config{
		Version:            cfg.Version,
		BuildType:          cfg.BuildType,
		ManifestURL:        cfg.UpdateManifestURL,
		OfficialCatalogURL: officialCatalogURL,
		OfficialKeyID:      officialCatalogKeyID,
		OfficialPublicKey:  officialCatalogPublicKey,
		AllowRestart:       cfg.AllowRestart,
		DatabaseURL:        cfg.DatabaseURL,
		PluginCacheDir:     cfg.PluginCacheDir,
		PluginActiveDir:    cfg.PluginActiveDir,
		BackupDir:          cfg.BackupDir,
		DiagnosticDir:      cfg.DiagnosticDir,
		MaxArchiveBytes:    cfg.MaxArchiveBytes,
	})
	go server.RunBackupScheduler(monitorCtx, systemService, settingsService, controlService, func(err error) {
		log.Printf("backup scheduler: %v", err)
	})

	router := server.New(server.Options{
		Config:             cfg,
		AuthService:        authService,
		OIDCService:        oidcService,
		FeishuService:      feishuService,
		GitHubOAuthService: githubOAuthService,
		GoogleOAuthService: googleOAuthService,
		DingTalkService:    dingTalkService,
		SettingsService:    settingsService,
		ControlService:     controlService,
		OperatorService:    operatorService,
		PluginService:      pluginService,
		SystemService:      systemService,
		ExportJobStore:     exportJobStore,
		DurableAIJobs:      durableJobRuntime,
		AIJobRuntime:       durableJobRuntime,
	})

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("AsterRouter listening on %s (storage=%s)", cfg.Addr, storageMode)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	stopChannelMonitor()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("server shutdown failed: %v", err)
	}
	select {
	case err := <-durableJobRuntimeDone:
		if err != nil {
			log.Printf("durable ai job runtime shutdown failed: %v", err)
		}
	case <-ctx.Done():
		log.Printf("durable ai job runtime shutdown timed out: %v", ctx.Err())
	}
	if err := pluginService.Shutdown(context.Background()); err != nil {
		log.Printf("plugin shutdown failed: %v", err)
	}
}

func configureAIJobInfrastructure(ctx context.Context, cfg config.Config, service *controlplane.Service) (controlplane.AIJobDeliveryQueue, func(), error) {
	queueDriver := strings.TrimSpace(cfg.AIJobQueueDriver)
	if queueDriver == "" {
		queueDriver = "memory"
	}
	affinityDriver := strings.TrimSpace(cfg.RoutingAffinityDriver)
	if affinityDriver == "" {
		affinityDriver = "repository"
	}
	if queueDriver != "memory" && queueDriver != "redis" {
		return nil, func() {}, fmt.Errorf("unsupported durable ai job queue driver %q", queueDriver)
	}
	if affinityDriver != "repository" && affinityDriver != "redis" {
		return nil, func() {}, fmt.Errorf("unsupported routing affinity driver %q", affinityDriver)
	}
	if queueDriver != "redis" && affinityDriver != "redis" {
		queue, err := controlplane.NewMemoryAIJobDeliveryQueue(30 * time.Second)
		if err != nil {
			return nil, func() {}, err
		}
		service.SetAIJobReadyIndex(controlplane.NewMemoryAIJobReadyIndex())
		return queue, func() {}, nil
	}
	options, err := redis.ParseURL(strings.TrimSpace(cfg.RedisURL))
	if err != nil {
		return nil, func() {}, fmt.Errorf("parse Redis URL: %w", err)
	}
	client := redis.NewClient(options)
	closeClient := func() { _ = client.Close() }
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		closeClient()
		return nil, func() {}, fmt.Errorf("connect to Redis: %w", err)
	}
	namespace := strings.TrimSpace(cfg.RedisNamespace)
	if namespace == "" {
		namespace = "asterrouter"
	}
	var queue controlplane.AIJobDeliveryQueue
	if queueDriver == "redis" {
		queue, err = controlplane.NewRedisAIJobDeliveryQueue(client, controlplane.RedisAIJobDeliveryQueueConfig{Namespace: namespace})
		if err != nil {
			closeClient()
			return nil, func() {}, err
		}
		readyIndex, readyErr := controlplane.NewRedisAIJobReadyIndex(client, controlplane.RedisAIJobReadyIndexConfig{Namespace: namespace})
		if readyErr != nil {
			closeClient()
			return nil, func() {}, readyErr
		}
		capacityStore, capacityErr := controlplane.NewRedisProviderCapacityStore(client, controlplane.RedisProviderCapacityStoreConfig{Namespace: namespace})
		if capacityErr != nil {
			closeClient()
			return nil, func() {}, capacityErr
		}
		service.SetAIJobReadyIndex(readyIndex)
		service.SetProviderCapacityStore(capacityStore)
	} else {
		queue, err = controlplane.NewMemoryAIJobDeliveryQueue(30 * time.Second)
		if err != nil {
			closeClient()
			return nil, func() {}, err
		}
		service.SetAIJobReadyIndex(controlplane.NewMemoryAIJobReadyIndex())
	}
	if affinityDriver == "redis" {
		coordinator, coordinatorErr := controlplane.NewRedisRoutingAffinityCoordinator(client, controlplane.RedisRoutingAffinityCoordinatorConfig{Namespace: namespace})
		if coordinatorErr != nil {
			closeClient()
			return nil, func() {}, coordinatorErr
		}
		service.SetRoutingAffinityCoordinator(coordinator)
	}
	return queue, closeClient, nil
}
