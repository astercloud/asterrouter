package system

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/buildinfo"
)

const (
	cacheTTL                 = 20 * time.Minute
	defaultMaxDownloadBytes  = 500 << 20
	manifestResponseMaxBytes = 4 << 20
)

var (
	ErrNoUpdateAvailable   = errors.New("no update available")
	ErrUpdateNotConfigured = errors.New("update manifest is not configured")
	ErrUpdateUnsupported   = errors.New("one-click update is not supported for this build")
	ErrNoCompatibleAsset   = errors.New("no compatible update asset found")
	ErrChecksumRequired    = errors.New("update asset sha256 checksum is required")
	ErrRestartUnsupported  = errors.New("service restart is not enabled")
)

type Config struct {
	Version          string
	BuildType        string
	ManifestURL      string
	AllowRestart     bool
	MaxDownloadBytes int64
	HTTPClient       *http.Client
}

type Service struct {
	version          string
	buildType        string
	manifestURL      string
	allowRestart     bool
	maxDownloadBytes int64
	client           *http.Client

	mu            sync.Mutex
	cached        *UpdateInfo
	cachedAt      time.Time
	cachedChannel string
}

type UpdateInfo struct {
	CurrentVersion     string       `json:"current_version"`
	LatestVersion      string       `json:"latest_version"`
	HasUpdate          bool         `json:"has_update"`
	ReleaseInfo        *ReleaseInfo `json:"release_info,omitempty"`
	Cached             bool         `json:"cached"`
	Warning            string       `json:"warning,omitempty"`
	BuildType          string       `json:"build_type"`
	UpdateSupported    bool         `json:"update_supported"`
	ManifestConfigured bool         `json:"manifest_configured"`
	RestartSupported   bool         `json:"restart_supported"`
	Channel            string       `json:"channel"`
	Platform           string       `json:"platform"`
}

type ReleaseInfo struct {
	Version     string  `json:"version"`
	Name        string  `json:"name"`
	Notes       string  `json:"notes"`
	PublishedAt string  `json:"published_at"`
	HTMLURL     string  `json:"html_url"`
	Asset       *Asset  `json:"asset,omitempty"`
	Assets      []Asset `json:"assets,omitempty"`
}

type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type ApplyResult struct {
	Message         string `json:"message"`
	OperationID     string `json:"operation_id"`
	NeedRestart     bool   `json:"need_restart"`
	AlreadyUpToDate bool   `json:"already_up_to_date"`
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	ManualAction    string `json:"manual_action,omitempty"`
}

type manifestFile struct {
	Version     string            `json:"version"`
	Channel     string            `json:"channel"`
	Name        string            `json:"name"`
	Notes       string            `json:"notes"`
	PublishedAt string            `json:"published_at"`
	HTMLURL     string            `json:"html_url"`
	Assets      []Asset           `json:"assets"`
	Releases    []manifestRelease `json:"releases"`
}

type manifestRelease struct {
	Version     string  `json:"version"`
	Channel     string  `json:"channel"`
	Name        string  `json:"name"`
	Notes       string  `json:"notes"`
	PublishedAt string  `json:"published_at"`
	HTMLURL     string  `json:"html_url"`
	Assets      []Asset `json:"assets"`
}

func NewService(cfg Config) *Service {
	version := strings.TrimSpace(cfg.Version)
	if version == "" {
		version = buildinfo.Version
	}
	buildType := strings.TrimSpace(cfg.BuildType)
	if buildType == "" {
		buildType = "source"
	}
	maxBytes := cfg.MaxDownloadBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxDownloadBytes
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Service{
		version:          version,
		buildType:        buildType,
		manifestURL:      strings.TrimSpace(cfg.ManifestURL),
		allowRestart:     cfg.AllowRestart,
		maxDownloadBytes: maxBytes,
		client:           client,
	}
}

func (s *Service) CheckUpdate(ctx context.Context, force bool, channel string) (UpdateInfo, error) {
	channel = normalizeChannel(channel)
	if s.manifestURL == "" {
		info := s.baseInfo(channel)
		info.Warning = "Update manifest is not configured. Use manual update or configure ASTER_UPDATE_MANIFEST_URL."
		return info, nil
	}
	if channel == "manual" {
		info := s.baseInfo(channel)
		info.ManifestConfigured = true
		info.Warning = "Update channel is manual. Automatic checks are disabled."
		return info, nil
	}

	if !force {
		if cached, ok := s.cachedInfo(channel); ok {
			return cached, nil
		}
	}

	manifest, err := s.fetchManifest(ctx)
	if err != nil {
		if cached, ok := s.cachedInfo(channel); ok {
			cached.Warning = "Using cached update data: " + err.Error()
			return cached, nil
		}
		info := s.baseInfo(channel)
		info.ManifestConfigured = true
		info.Warning = err.Error()
		return info, nil
	}

	release, ok := selectRelease(manifest, channel)
	if !ok {
		info := s.baseInfo(channel)
		info.ManifestConfigured = true
		info.Warning = "No release is available for the selected channel."
		s.storeCache(channel, info)
		return info, nil
	}

	asset := selectAsset(release.Assets)
	latest := strings.TrimPrefix(strings.TrimSpace(release.Version), "v")
	info := s.baseInfo(channel)
	info.LatestVersion = latest
	info.HasUpdate = compareVersions(info.CurrentVersion, latest) < 0
	info.ManifestConfigured = true
	info.ReleaseInfo = &ReleaseInfo{
		Version:     latest,
		Name:        release.Name,
		Notes:       release.Notes,
		PublishedAt: release.PublishedAt,
		HTMLURL:     release.HTMLURL,
		Asset:       asset,
		Assets:      release.Assets,
	}
	info.UpdateSupported = info.HasUpdate && s.buildType == "release" && asset != nil
	if info.HasUpdate && s.buildType != "release" {
		info.Warning = "This build was not produced as a release artifact. Use manual update for source builds."
	}
	if info.HasUpdate && s.buildType == "release" && asset == nil {
		info.Warning = "No compatible update asset was found for this platform."
	}
	s.storeCache(channel, info)
	return info, nil
}

func (s *Service) PerformUpdate(ctx context.Context, channel string, operationID string) (ApplyResult, error) {
	info, err := s.CheckUpdate(ctx, true, channel)
	if err != nil {
		return ApplyResult{}, err
	}
	if !info.ManifestConfigured {
		return manualUpdateResult(info, operationID), ErrUpdateNotConfigured
	}
	if normalizeChannel(channel) == "manual" {
		return manualUpdateResult(info, operationID), ErrUpdateUnsupported
	}
	if !info.HasUpdate {
		return ApplyResult{
			Message:         "Already up to date",
			OperationID:     operationID,
			AlreadyUpToDate: true,
			CurrentVersion:  info.CurrentVersion,
			LatestVersion:   info.LatestVersion,
		}, nil
	}
	if s.buildType != "release" {
		return manualUpdateResult(info, operationID), ErrUpdateUnsupported
	}
	if info.ReleaseInfo == nil || info.ReleaseInfo.Asset == nil {
		return manualUpdateResult(info, operationID), ErrNoCompatibleAsset
	}
	asset := *info.ReleaseInfo.Asset
	if strings.TrimSpace(asset.SHA256) == "" {
		return manualUpdateResult(info, operationID), ErrChecksumRequired
	}
	if err := s.applyAsset(ctx, asset); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{
		Message:        "Update completed. Restart the service to run the new version.",
		OperationID:    operationID,
		NeedRestart:    true,
		CurrentVersion: info.CurrentVersion,
		LatestVersion:  info.LatestVersion,
	}, nil
}

func (s *Service) Rollback(operationID string) (ApplyResult, error) {
	exePath, err := executablePath()
	if err != nil {
		return ApplyResult{}, err
	}
	backupPath := exePath + ".backup"
	if _, err := os.Stat(backupPath); err != nil {
		if os.IsNotExist(err) {
			return ApplyResult{}, fmt.Errorf("no rollback backup found")
		}
		return ApplyResult{}, err
	}
	currentBackup := exePath + ".rollback-current"
	_ = os.Remove(currentBackup)
	if err := os.Rename(exePath, currentBackup); err != nil {
		return ApplyResult{}, fmt.Errorf("prepare rollback: %w", err)
	}
	if err := os.Rename(backupPath, exePath); err != nil {
		_ = os.Rename(currentBackup, exePath)
		return ApplyResult{}, fmt.Errorf("restore backup: %w", err)
	}
	_ = os.Remove(currentBackup)
	return ApplyResult{
		Message:     "Rollback completed. Restart the service to run the restored version.",
		OperationID: operationID,
		NeedRestart: true,
	}, nil
}

func (s *Service) Restart(operationID string, delay time.Duration) (ApplyResult, error) {
	if !s.allowRestart {
		return ApplyResult{
			Message:      "Automatic restart is disabled.",
			OperationID:  operationID,
			ManualAction: "Restart the service manually, or set ASTER_ALLOW_RESTART=true for managed deployments.",
		}, ErrRestartUnsupported
	}
	if delay <= 0 {
		delay = 500 * time.Millisecond
	}
	go func() {
		time.Sleep(delay)
		os.Exit(0)
	}()
	return ApplyResult{
		Message:     "Service restart initiated.",
		OperationID: operationID,
	}, nil
}

func (s *Service) baseInfo(channel string) UpdateInfo {
	return UpdateInfo{
		CurrentVersion:     s.version,
		LatestVersion:      s.version,
		BuildType:          s.buildType,
		UpdateSupported:    false,
		ManifestConfigured: s.manifestURL != "",
		RestartSupported:   s.allowRestart,
		Channel:            channel,
		Platform:           runtime.GOOS + "/" + runtime.GOARCH,
	}
}

func (s *Service) cachedInfo(channel string) (UpdateInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached == nil || s.cachedChannel != channel || time.Since(s.cachedAt) > cacheTTL {
		return UpdateInfo{}, false
	}
	out := *s.cached
	out.Cached = true
	return out, true
}

func (s *Service) storeCache(channel string, info UpdateInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copyInfo := info
	copyInfo.Cached = false
	s.cached = &copyInfo
	s.cachedAt = time.Now()
	s.cachedChannel = channel
}

func (s *Service) fetchManifest(ctx context.Context) (manifestFile, error) {
	if !isHTTPURL(s.manifestURL) {
		return manifestFile{}, fmt.Errorf("update manifest URL must be http or https")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.manifestURL, nil)
	if err != nil {
		return manifestFile{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return manifestFile{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return manifestFile{}, fmt.Errorf("manifest request failed with status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, manifestResponseMaxBytes+1))
	if err != nil {
		return manifestFile{}, err
	}
	if len(body) > manifestResponseMaxBytes {
		return manifestFile{}, fmt.Errorf("manifest response exceeds %d bytes", manifestResponseMaxBytes)
	}
	var manifest manifestFile
	if err := json.Unmarshal(body, &manifest); err != nil {
		return manifestFile{}, err
	}
	return manifest, nil
}

func selectRelease(manifest manifestFile, channel string) (manifestRelease, bool) {
	releases := manifest.Releases
	if len(releases) == 0 && manifest.Version != "" {
		releases = []manifestRelease{{
			Version:     manifest.Version,
			Channel:     manifest.Channel,
			Name:        manifest.Name,
			Notes:       manifest.Notes,
			PublishedAt: manifest.PublishedAt,
			HTMLURL:     manifest.HTMLURL,
			Assets:      manifest.Assets,
		}}
	}
	var selected manifestRelease
	found := false
	for _, release := range releases {
		if !releaseMatchesChannel(release.Channel, channel) {
			continue
		}
		if !found || compareVersions(selected.Version, release.Version) < 0 {
			selected = release
			found = true
		}
	}
	return selected, found
}

func releaseMatchesChannel(releaseChannel string, channel string) bool {
	releaseChannel = normalizeChannel(releaseChannel)
	channel = normalizeChannel(channel)
	return releaseChannel == channel || releaseChannel == "stable" && channel == ""
}

func selectAsset(assets []Asset) *Asset {
	for _, asset := range assets {
		if platformPartMatches(asset.OS, runtime.GOOS) && platformPartMatches(asset.Arch, runtime.GOARCH) {
			copyAsset := asset
			return &copyAsset
		}
	}
	return nil
}

func platformPartMatches(value string, current string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == current
}

func (s *Service) applyAsset(ctx context.Context, asset Asset) error {
	if !isHTTPURL(asset.URL) {
		return fmt.Errorf("asset URL must be http or https")
	}
	exePath, err := executablePath()
	if err != nil {
		return err
	}
	exeDir := filepath.Dir(exePath)
	tempDir, err := os.MkdirTemp(exeDir, ".asterrouter-update-*")
	if err != nil {
		return fmt.Errorf("create update temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	newBinaryPath := filepath.Join(tempDir, "asterrouter.new")
	if err := s.downloadFile(ctx, asset, newBinaryPath); err != nil {
		return err
	}
	if err := verifySHA256(newBinaryPath, asset.SHA256); err != nil {
		return err
	}
	if err := os.Chmod(newBinaryPath, 0755); err != nil {
		return fmt.Errorf("chmod update asset: %w", err)
	}

	backupPath := exePath + ".backup"
	_ = os.Remove(backupPath)
	if err := os.Rename(exePath, backupPath); err != nil {
		return fmt.Errorf("backup current executable: %w", err)
	}
	if err := os.Rename(newBinaryPath, exePath); err != nil {
		if restoreErr := os.Rename(backupPath, exePath); restoreErr != nil {
			return fmt.Errorf("replace executable failed: %w; restore failed: %v", err, restoreErr)
		}
		return fmt.Errorf("replace executable failed and backup was restored: %w", err)
	}
	return nil
}

func (s *Service) downloadFile(ctx context.Context, asset Asset, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}
	if resp.ContentLength > s.maxDownloadBytes {
		return fmt.Errorf("download exceeds %d bytes", s.maxDownloadBytes)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	written, err := io.Copy(out, io.LimitReader(resp.Body, s.maxDownloadBytes+1))
	if err != nil {
		return err
	}
	if written > s.maxDownloadBytes {
		return fmt.Errorf("download exceeds %d bytes", s.maxDownloadBytes)
	}
	if asset.Size > 0 && written != asset.Size {
		return fmt.Errorf("download size mismatch: expected %d, got %d", asset.Size, written)
	}
	return nil
}

func verifySHA256(path string, want string) error {
	want = strings.ToLower(strings.TrimSpace(want))
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return err
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != want {
		return fmt.Errorf("sha256 mismatch: expected %s, got %s", want, got)
	}
	return nil
}

func executablePath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate executable: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	return exePath, nil
}

func manualUpdateResult(info UpdateInfo, operationID string) ApplyResult {
	return ApplyResult{
		Message:        "Manual update is required for this build or platform.",
		OperationID:    operationID,
		CurrentVersion: info.CurrentVersion,
		LatestVersion:  info.LatestVersion,
		ManualAction:   "Download the matching release artifact, verify its checksum, replace the binary, and restart the service.",
	}
}

func normalizeChannel(channel string) string {
	switch strings.TrimSpace(channel) {
	case "beta":
		return "beta"
	case "manual":
		return "manual"
	default:
		return "stable"
	}
}

func isHTTPURL(value string) bool {
	return strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://")
}

func compareVersions(current, latest string) int {
	currentParts := parseVersion(current)
	latestParts := parseVersion(latest)
	for i := 0; i < 3; i++ {
		if currentParts[i] < latestParts[i] {
			return -1
		}
		if currentParts[i] > latestParts[i] {
			return 1
		}
	}
	return 0
}

func parseVersion(value string) [3]int {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.Split(value, ".")
	out := [3]int{}
	for i := 0; i < len(parts) && i < len(out); i++ {
		out[i] = parseVersionPart(parts[i])
	}
	return out
}

func parseVersionPart(value string) int {
	var b strings.Builder
	for _, r := range value {
		if r < '0' || r > '9' {
			break
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return 0
	}
	n, err := strconv.Atoi(b.String())
	if err != nil {
		return 0
	}
	return n
}
