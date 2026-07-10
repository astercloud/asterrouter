package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const maxPackageBytes = 512 * 1024 * 1024

func (s *Service) requestPackageDownload(ctx context.Context, cfg OfficialCatalogConfig, record packageRecord, request PackageDownloadRequest) (packageDownloadGrant, error) {
	endpoint, err := packageDownloadURL(cfg.URL, record.PackageID)
	if err != nil {
		return packageDownloadGrant{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return packageDownloadGrant{}, err
	}
	httpReq.Header.Set("X-Aster-Core-Version", s.coreVersion)
	httpReq.Header.Set("X-Aster-OS", s.targetOS)
	httpReq.Header.Set("X-Aster-Arch", s.targetArch)
	if strings.TrimSpace(request.LicenseID) != "" {
		httpReq.Header.Set("X-Aster-License-ID", strings.TrimSpace(request.LicenseID))
	}
	if strings.TrimSpace(request.ActivationSecret) != "" {
		httpReq.Header.Set("X-Aster-Activation-Secret", strings.TrimSpace(request.ActivationSecret))
	}
	if strings.TrimSpace(request.InstanceID) != "" {
		httpReq.Header.Set("X-Aster-Instance-ID", strings.TrimSpace(request.InstanceID))
	}
	response, err := s.httpClient.Do(httpReq)
	if err != nil {
		return packageDownloadGrant{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return packageDownloadGrant{}, fmt.Errorf("official package download authorization returned status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxCatalogBytes+1))
	if err != nil {
		return packageDownloadGrant{}, err
	}
	if len(body) > maxCatalogBytes {
		return packageDownloadGrant{}, fmt.Errorf("official package download authorization exceeds maximum size")
	}
	return decodePackageDownloadGrant(body)
}

func (s *Service) downloadPackageObject(ctx context.Context, grant packageDownloadGrant, record packageRecord) (string, int64, error) {
	if !isHTTPURL(grant.DownloadURL) {
		return "", 0, fmt.Errorf("package download URL must be http or https")
	}
	packageDir := filepath.Join(s.packageCacheDir, sanitizePathSegment(record.PluginID), sanitizePathSegment(record.Version))
	if err := os.MkdirAll(packageDir, 0750); err != nil {
		return "", 0, fmt.Errorf("create package cache directory: %w", err)
	}
	targetPath := filepath.Join(packageDir, sanitizePathSegment(record.PackageID)+"-"+sanitizePathSegment(record.OS)+"-"+sanitizePathSegment(record.Arch)+".pkg")
	tempFile, err := os.CreateTemp(packageDir, ".download-*")
	if err != nil {
		return "", 0, fmt.Errorf("create package temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() { _ = os.Remove(tempPath) }()
	defer tempFile.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, grant.DownloadURL, nil)
	if err != nil {
		return "", 0, err
	}
	for key, value := range grant.Headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	response, err := s.httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", 0, fmt.Errorf("official package object returned status %d", response.StatusCode)
	}
	limit := record.SizeBytes
	if limit <= 0 || limit > maxPackageBytes {
		limit = maxPackageBytes
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(tempFile, hash), io.LimitReader(response.Body, limit+1))
	if err != nil {
		return "", 0, err
	}
	if written > limit {
		return "", 0, fmt.Errorf("official package exceeds expected size")
	}
	if record.SizeBytes > 0 && written != record.SizeBytes {
		return "", 0, fmt.Errorf("official package size mismatch")
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != record.SHA256 {
		return "", 0, ErrPackageChecksum
	}
	if err := tempFile.Close(); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		return "", 0, fmt.Errorf("store package cache: %w", err)
	}
	return targetPath, written, nil
}

func decodePackageDownloadGrant(body []byte) (packageDownloadGrant, error) {
	var direct packageDownloadGrant
	if err := json.Unmarshal(body, &direct); err == nil && direct.DownloadURL != "" {
		return direct, nil
	}
	var wrapped struct {
		Data packageDownloadGrant `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return packageDownloadGrant{}, err
	}
	if wrapped.Data.DownloadURL == "" {
		return packageDownloadGrant{}, fmt.Errorf("package download grant is missing")
	}
	return wrapped.Data, nil
}

func packageDownloadURL(catalogURL string, packageID string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(catalogURL))
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("catalog URL must be http or https")
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(basePath, "/catalog/index") {
		basePath = strings.TrimSuffix(basePath, "/catalog/index")
	}
	parsed.Path = strings.TrimRight(basePath, "/") + "/packages/" + url.PathEscape(strings.TrimSpace(packageID)) + "/download"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func isHTTPURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func sanitizePathSegment(value string) string {
	value = sanitizeCatalogSlug(value)
	if value == "" {
		return "unknown"
	}
	return value
}
