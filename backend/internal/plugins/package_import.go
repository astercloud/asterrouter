package plugins

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrPackageImport = errors.New("plugin package import failed")

type packageImportFile struct {
	PackageID     string `json:"package_id"`
	ContentBase64 string `json:"content_base64"`
	SHA256        string `json:"sha256"`
	SizeBytes     int64  `json:"size_bytes"`
}

func (s *Service) ImportPackage(ctx context.Context, pluginID string, packageID string, request PackageImportRequest) (PackageDownloadResult, error) {
	cfg := normalizeOfficialCatalogConfig(s.catalogConfig)
	if cfg.Mode == CatalogModeDisabled {
		return PackageDownloadResult{}, ErrCatalogSyncDisabled
	}
	if cfg.PublicKeyID == "" || cfg.PublicKeyBase64 == "" {
		return PackageDownloadResult{}, ErrCatalogNotConfigured
	}
	record, ok, err := s.repo.FindPackage(ctx, strings.TrimSpace(packageID))
	if err != nil {
		return PackageDownloadResult{}, err
	}
	if !ok || record.PluginID != strings.TrimSpace(pluginID) {
		return PackageDownloadResult{}, ErrPackageNotFound
	}
	view, err := s.packageView(ctx, record)
	if err != nil {
		return PackageDownloadResult{}, err
	}
	if view.Revoked {
		return PackageDownloadResult{}, ErrPackageRevoked
	}
	if !view.Compatible {
		return PackageDownloadResult{}, fmt.Errorf("%w: %s", ErrPackageIncompatible, view.CompatibilityError)
	}
	if record.RequiredEntitlement {
		if _, _, ok, err := s.localLicenseForPackage(ctx, record); err != nil {
			return PackageDownloadResult{}, err
		} else if !ok {
			return PackageDownloadResult{}, ErrPluginLocked
		}
	}
	if err := verifyPackageSignature(record.SignatureJSON, record, cfg, s.now().UTC()); err != nil {
		return PackageDownloadResult{}, err
	}
	importFile, content, err := decodePackageImportRequest(request)
	if err != nil {
		return PackageDownloadResult{}, err
	}
	if importFile.PackageID != "" && importFile.PackageID != record.PackageID {
		return PackageDownloadResult{}, fmt.Errorf("%w: package id mismatch", ErrPackageImport)
	}
	if importFile.SHA256 != "" && strings.ToLower(strings.TrimSpace(importFile.SHA256)) != record.SHA256 {
		return PackageDownloadResult{}, ErrPackageChecksum
	}
	if importFile.SizeBytes > 0 && importFile.SizeBytes != record.SizeBytes {
		return PackageDownloadResult{}, ErrPackageChecksum
	}
	cachePath, sizeBytes, err := s.storeImportedPackage(record, content)
	if err != nil {
		return PackageDownloadResult{}, err
	}
	return s.recordPackageCache(ctx, record, view, cachePath, sizeBytes)
}

func decodePackageImportRequest(request PackageImportRequest) (packageImportFile, []byte, error) {
	importFile := packageImportFile{ContentBase64: request.ContentBase64}
	if len(request.FileJSON) > 0 && string(request.FileJSON) != "null" {
		if err := json.Unmarshal(request.FileJSON, &importFile); err != nil {
			return packageImportFile{}, nil, fmt.Errorf("%w: decode import file", ErrPackageImport)
		}
	}
	contentBase64 := strings.TrimSpace(importFile.ContentBase64)
	if contentBase64 == "" {
		return packageImportFile{}, nil, fmt.Errorf("%w: content_base64 is required", ErrPackageImport)
	}
	content, err := decodePackageContent(contentBase64)
	if err != nil {
		return packageImportFile{}, nil, fmt.Errorf("%w: invalid content_base64", ErrPackageImport)
	}
	if len(content) == 0 || int64(len(content)) > maxPackageBytes {
		return packageImportFile{}, nil, fmt.Errorf("%w: package size is invalid", ErrPackageImport)
	}
	importFile.PackageID = strings.TrimSpace(importFile.PackageID)
	importFile.SHA256 = strings.ToLower(strings.TrimSpace(importFile.SHA256))
	return importFile, content, nil
}

func decodePackageContent(value string) ([]byte, error) {
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.RawURLEncoding.DecodeString(value)
}

func (s *Service) storeImportedPackage(record packageRecord, content []byte) (string, int64, error) {
	if record.SizeBytes > 0 && int64(len(content)) != record.SizeBytes {
		return "", 0, ErrPackageChecksum
	}
	sum := sha256.Sum256(content)
	if got := hex.EncodeToString(sum[:]); got != record.SHA256 {
		return "", 0, ErrPackageChecksum
	}
	packageDir := filepath.Join(s.packageCacheDir, sanitizePathSegment(record.PluginID), sanitizePathSegment(record.Version))
	if err := os.MkdirAll(packageDir, 0750); err != nil {
		return "", 0, fmt.Errorf("create package cache directory: %w", err)
	}
	targetPath := filepath.Join(packageDir, sanitizePathSegment(record.PackageID)+"-"+sanitizePathSegment(record.OS)+"-"+sanitizePathSegment(record.Arch)+".pkg")
	tempFile, err := os.CreateTemp(packageDir, ".import-*")
	if err != nil {
		return "", 0, fmt.Errorf("create package import temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := bytes.NewReader(content).WriteTo(tempFile); err != nil {
		_ = tempFile.Close()
		return "", 0, err
	}
	if err := tempFile.Close(); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		return "", 0, fmt.Errorf("store imported package cache: %w", err)
	}
	return targetPath, int64(len(content)), nil
}
