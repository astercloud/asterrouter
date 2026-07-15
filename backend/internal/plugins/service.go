package plugins

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

var (
	ErrPluginNotFound                  = errors.New("plugin not found")
	ErrPluginSurface                   = errors.New("plugin is not available on this surface")
	ErrPluginLocked                    = errors.New("plugin entitlement is missing")
	ErrPluginCoreRequired              = errors.New("core plugin cannot be disabled")
	ErrPluginNotConfigurable           = errors.New("plugin is not configurable")
	ErrPluginConfigInvalid             = errors.New("plugin configuration is invalid")
	ErrArtifactSinkRegistryRequired    = errors.New("artifact sink registry is required")
	ErrArtifactSinkDestinationNotFound = errors.New("artifact sink destination not found")
)

type Service struct {
	repo                      Repository
	secretKey                 string
	httpClient                *http.Client
	providerAdapterHTTPClient *http.Client
	catalogConfig             OfficialCatalogConfig
	licenseConfig             OfficialLicenseConfig
	licenseInstanceID         string
	licenseFingerprint        string
	packageCacheDir           string
	packageActiveDir          string
	pluginHostURL             string
	coreVersion               string
	targetOS                  string
	targetArch                string
	now                       func() time.Time
	sidecarsMu                sync.Mutex
	packageMu                 sync.Mutex
	sidecars                  map[string]*sidecarProcess
	supervisors               map[string]*sidecarSupervisor
	artifactSinkRegistry      ArtifactSinkRegistry
	artifactSinkStoreFactory  ArtifactSinkStoreFactory
	artifactSinkMu            sync.Mutex
	registeredArtifactSinks   map[string]controlplane.ArtifactSink
	openAIImageCacheMu        sync.Mutex
	openAIImageTasks          map[string]openAIImageTaskCache
	openAIImageCacheBytes     int64
}

type ServiceOptions struct {
	SecretKey                 string
	HTTPClient                *http.Client
	ProviderAdapterHTTPClient *http.Client
	OfficialCatalog           OfficialCatalogConfig
	OfficialLicense           OfficialLicenseConfig
	PackageCacheDir           string
	PluginActiveDir           string
	PluginHostURL             string
	CoreVersion               string
	TargetOS                  string
	TargetArch                string
	Now                       func() time.Time
	ArtifactSinkRegistry      ArtifactSinkRegistry
	ArtifactSinkStoreFactory  ArtifactSinkStoreFactory
}

func NewService(repo Repository, secretKey ...string) *Service {
	key := "asterrouter-local-development-secret"
	if len(secretKey) > 0 && strings.TrimSpace(secretKey[0]) != "" {
		key = strings.TrimSpace(secretKey[0])
	}
	return NewServiceWithOptions(repo, ServiceOptions{SecretKey: key})
}

func NewServiceWithOptions(repo Repository, options ServiceOptions) *Service {
	key := "asterrouter-local-development-secret"
	if strings.TrimSpace(options.SecretKey) != "" {
		key = strings.TrimSpace(options.SecretKey)
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	providerAdapterClient := options.ProviderAdapterHTTPClient
	if providerAdapterClient == nil {
		providerAdapterTransport := http.DefaultTransport.(*http.Transport).Clone()
		providerAdapterTransport.ResponseHeaderTimeout = 5 * time.Minute
		providerAdapterClient = &http.Client{
			Transport: providerAdapterTransport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("provider adapter redirects are not allowed")
			},
		}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	cacheDir := defaultString(strings.TrimSpace(options.PackageCacheDir), defaultPackageCacheDir())
	activeDir := defaultString(strings.TrimSpace(options.PluginActiveDir), defaultPackageActiveDir(cacheDir))
	return &Service{
		repo:                      repo,
		secretKey:                 key,
		httpClient:                client,
		providerAdapterHTTPClient: providerAdapterClient,
		catalogConfig:             normalizeOfficialCatalogConfig(options.OfficialCatalog),
		licenseConfig:             normalizeOfficialLicenseConfig(options.OfficialLicense, options.OfficialCatalog),
		licenseInstanceID:         strings.TrimSpace(options.OfficialLicense.InstanceID),
		licenseFingerprint:        strings.TrimSpace(options.OfficialLicense.Fingerprint),
		packageCacheDir:           cacheDir,
		packageActiveDir:          activeDir,
		pluginHostURL:             strings.TrimRight(strings.TrimSpace(options.PluginHostURL), "/"),
		coreVersion:               defaultString(strings.TrimSpace(options.CoreVersion), "0.1.0-dev"),
		targetOS:                  defaultString(strings.ToLower(strings.TrimSpace(options.TargetOS)), runtime.GOOS),
		targetArch:                defaultString(strings.ToLower(strings.TrimSpace(options.TargetArch)), runtime.GOARCH),
		now:                       now,
		sidecars:                  map[string]*sidecarProcess{},
		supervisors:               map[string]*sidecarSupervisor{},
		artifactSinkRegistry:      options.ArtifactSinkRegistry,
		artifactSinkStoreFactory:  options.ArtifactSinkStoreFactory,
		registeredArtifactSinks:   map[string]controlplane.ArtifactSink{},
		openAIImageTasks:          map[string]openAIImageTaskCache{},
	}
}

func (s *Service) EnsureSeedData(ctx context.Context) error {
	existing, err := s.repo.ListPlugins(ctx)
	if err != nil {
		return err
	}
	existingByID := map[string]Plugin{}
	for _, plugin := range existing {
		existingByID[plugin.ID] = plugin
	}
	now := time.Now().UTC()
	for _, plugin := range builtinPlugins(now) {
		targetStatus := plugin.Status
		if current, ok := existingByID[plugin.ID]; ok {
			plugin.Status = current.Status
			if plugin.Tier == TierPaidAddon && plugin.EntitlementStatus == EntitlementMissing {
				plugin.Status = StatusLocked
			}
			targetStatus = plugin.Status
			plugin.CreatedAt = current.CreatedAt
		}
		if err := s.repo.SavePlugin(ctx, plugin); err != nil {
			return err
		}
		if err := s.repo.UpdateStatus(ctx, plugin.ID, targetStatus, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Catalog(ctx context.Context) (Catalog, error) {
	return s.catalog(ctx, "")
}

func (s *Service) CatalogForSurface(ctx context.Context, surface string) (Catalog, error) {
	return s.catalog(ctx, strings.TrimSpace(surface))
}

func (s *Service) catalog(ctx context.Context, surface string) (Catalog, error) {
	plugins, err := s.repo.ListPlugins(ctx)
	if err != nil {
		return Catalog{}, err
	}
	for index := range plugins {
		if surface != "" && !pluginSurfaceAllowed(plugins[index], surface) {
			continue
		}
		plugin, err := s.applyLocalEntitlement(ctx, plugins[index])
		if err != nil {
			return Catalog{}, err
		}
		plugins[index] = plugin
		packages, err := s.Packages(ctx, plugins[index].ID)
		if err != nil {
			return Catalog{}, err
		}
		plugins[index].Packages = packages
	}
	filtered := make([]Plugin, 0, len(plugins))
	for _, plugin := range plugins {
		if surface == "" || pluginSurfaceAllowed(plugin, surface) {
			filtered = append(filtered, plugin)
		}
	}
	return Catalog{Summary: summarize(filtered), Plugins: filtered}, nil
}

func (s *Service) RequireSurface(ctx context.Context, id string, surface string) error {
	plugin, ok, err := s.repo.FindPlugin(ctx, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if !ok {
		return ErrPluginNotFound
	}
	if !pluginSurfaceAllowed(plugin, strings.TrimSpace(surface)) {
		return ErrPluginSurface
	}
	return nil
}

func pluginSurfaceAllowed(plugin Plugin, surface string) bool {
	if surface == "" {
		return true
	}
	for _, item := range plugin.Surfaces {
		if strings.TrimSpace(item) == surface {
			return true
		}
		// Keep old catalog records readable while they are refreshed.
		if surface == "enterprise" && (item == "admin" || item == "portal") {
			return true
		}
	}
	return false
}

func (s *Service) Enable(ctx context.Context, id string) (Plugin, error) {
	plugin, ok, err := s.repo.FindPlugin(ctx, strings.TrimSpace(id))
	if err != nil {
		return Plugin{}, err
	}
	if !ok {
		return Plugin{}, ErrPluginNotFound
	}
	plugin, err = s.applyLocalEntitlement(ctx, plugin)
	if err != nil {
		return Plugin{}, err
	}
	if plugin.Tier == TierPaidAddon && plugin.EntitlementStatus == EntitlementMissing {
		return Plugin{}, ErrPluginLocked
	}
	now := time.Now().UTC()
	if err := s.repo.UpdateStatus(ctx, plugin.ID, StatusEnabled, now); err != nil {
		return Plugin{}, err
	}
	plugin.Status = StatusEnabled
	plugin.UpdatedAt = now
	if plugin.ID == ArtifactS3SinkPluginID {
		if err := s.enableArtifactSinkPlugin(ctx, plugin); err != nil {
			statusRollbackErr := s.repo.UpdateStatus(ctx, plugin.ID, StatusDisabled, now)
			plugin.Status = StatusDisabled
			runtimeRollbackErr := s.syncArtifactSinkPlugin(ctx, plugin)
			return Plugin{}, errors.Join(err, statusRollbackErr, runtimeRollbackErr)
		}
	}
	if _, ok, err := s.sidecarTarget(ctx, plugin.ID); err != nil {
		return Plugin{}, err
	} else if ok {
		if err := s.ensureSidecarSupervisor(ctx, plugin.ID); err != nil {
			return Plugin{}, err
		}
	}
	return plugin, nil
}

func (s *Service) Disable(ctx context.Context, id string) (Plugin, error) {
	plugin, ok, err := s.repo.FindPlugin(ctx, strings.TrimSpace(id))
	if err != nil {
		return Plugin{}, err
	}
	if !ok {
		return Plugin{}, ErrPluginNotFound
	}
	if plugin.Tier == TierCore {
		return Plugin{}, ErrPluginCoreRequired
	}
	now := time.Now().UTC()
	if err := s.repo.UpdateStatus(ctx, plugin.ID, StatusDisabled, now); err != nil {
		return Plugin{}, err
	}
	plugin.Status = StatusDisabled
	plugin.UpdatedAt = now
	if plugin.ID == ArtifactS3SinkPluginID {
		if err := s.syncArtifactSinkPlugin(ctx, plugin); err != nil {
			return Plugin{}, err
		}
	}
	_ = s.stopSidecarSupervisor(ctx, plugin.ID)
	return plugin, nil
}

func (s *Service) Health(ctx context.Context) error {
	return s.repo.Health(ctx)
}

func (s *Service) Shutdown(ctx context.Context) error {
	s.stopArtifactSinkPlugins()
	s.clearOpenAIImageTaskCache()
	s.sidecarsMu.Lock()
	supervisors := make([]*sidecarSupervisor, 0, len(s.supervisors))
	for key, supervisor := range s.supervisors {
		supervisors = append(supervisors, supervisor)
		delete(s.supervisors, key)
	}
	sidecars := make([]*sidecarProcess, 0, len(s.sidecars))
	for key, proc := range s.sidecars {
		sidecars = append(sidecars, proc)
		delete(s.sidecars, key)
	}
	s.sidecarsMu.Unlock()
	for _, supervisor := range supervisors {
		_ = supervisor.stop(ctx)
	}
	for _, proc := range sidecars {
		_ = proc.stop(ctx)
	}
	return nil
}

func summarize(plugins []Plugin) Summary {
	var out Summary
	out.Total = len(plugins)
	for _, plugin := range plugins {
		if plugin.Status == StatusEnabled {
			out.Enabled++
		}
		if plugin.Tier == TierCore || plugin.Tier == TierFreeCore {
			out.Free++
		}
		if plugin.Tier == TierPaidAddon && plugin.Status == StatusLocked {
			out.PaidLocked++
		}
		if plugin.Configurable {
			out.Configurable++
		}
	}
	return out
}

func defaultPackageCacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(base) == "" {
		return filepath.Join(".", "data", "plugin-cache")
	}
	return filepath.Join(base, "asterrouter", "plugins")
}

func defaultPackageActiveDir(cacheDir string) string {
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		return filepath.Join(".", "data", "plugin-active")
	}
	parent := filepath.Dir(cacheDir)
	return filepath.Join(parent, "plugin-active")
}
