package plugins

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrPackageNotFound     = errors.New("plugin package not found")
	ErrPackageIncompatible = errors.New("plugin package is incompatible with this router")
	ErrPackageRevoked      = errors.New("plugin package has been revoked")
	ErrPackageSignature    = errors.New("plugin package signature verification failed")
	ErrPackageChecksum     = errors.New("plugin package checksum verification failed")
	ErrPackageNotCached    = errors.New("plugin package is not cached")
	ErrPackageNotInstalled = errors.New("plugin package is not installed")
)

func (s *Service) Packages(ctx context.Context, pluginID string) ([]Package, error) {
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return []Package{}, nil
	}
	if _, ok, err := s.repo.FindPlugin(ctx, pluginID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrPluginNotFound
	}
	records, err := s.repo.ListPackages(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	out := make([]Package, 0, len(records))
	for _, record := range records {
		item, err := s.packageView(ctx, record)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Service) DownloadPackage(ctx context.Context, pluginID string, packageID string, request PackageDownloadRequest) (PackageDownloadResult, error) {
	cfg, err := s.effectiveCatalogConfig(ctx)
	if err != nil {
		return PackageDownloadResult{}, err
	}
	if cfg.Mode == CatalogModeDisabled {
		return PackageDownloadResult{}, ErrCatalogSyncDisabled
	}
	if cfg.Mode != CatalogModeOnline && cfg.Mode != CatalogModePrivateMirror {
		return PackageDownloadResult{}, ErrCatalogNotConfigured
	}
	if cfg.PublicKeyID == "" || cfg.PublicKeyBase64 == "" || cfg.Mode == CatalogModeOnline && cfg.URL == "" {
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
	if record.RequiredEntitlement && (strings.TrimSpace(request.LicenseID) == "" || strings.TrimSpace(request.ActivationSecret) == "") {
		license, secret, ok, err := s.localLicenseForPackage(ctx, record)
		if err != nil {
			return PackageDownloadResult{}, err
		}
		if !ok || secret == "" {
			return PackageDownloadResult{}, ErrPluginLocked
		}
		request.LicenseID = license.LicenseID
		request.ActivationSecret = secret
		request.InstanceID = license.InstanceID
	}
	if err := verifyPackageSignature(record.SignatureJSON, record, cfg, s.now().UTC()); err != nil {
		return PackageDownloadResult{}, err
	}
	var cachePath string
	var sizeBytes int64
	if cfg.Mode == CatalogModePrivateMirror {
		cachePath, sizeBytes, err = s.downloadPackageObject(ctx, packageDownloadGrant{DownloadURL: record.PackageURI}, record)
	} else {
		var grant packageDownloadGrant
		grant, err = s.requestPackageDownload(ctx, cfg, record, request)
		if err != nil {
			return PackageDownloadResult{}, err
		}
		if err := verifyPackageDownloadGrant(grant, record, cfg, s.now().UTC()); err != nil {
			return PackageDownloadResult{}, err
		}
		cachePath, sizeBytes, err = s.downloadPackageObject(ctx, grant, record)
	}
	if err != nil {
		return PackageDownloadResult{}, err
	}
	result, err := s.recordPackageCache(ctx, record, view, cachePath, sizeBytes)
	if err != nil {
		return PackageDownloadResult{}, err
	}
	return result, nil
}

func (s *Service) packageView(ctx context.Context, record packageRecord) (Package, error) {
	revocation, err := s.packageRevocation(ctx, record)
	if err != nil {
		return Package{}, err
	}
	compatible, compatibilityError := s.packageCompatible(record, revocation)
	item := Package{
		PluginID:            record.PluginID,
		PackageID:           record.PackageID,
		Version:             record.Version,
		Channel:             record.Channel,
		OS:                  record.OS,
		Arch:                record.Arch,
		SHA256:              record.SHA256,
		SizeBytes:           record.SizeBytes,
		RequiredEntitlement: record.RequiredEntitlement,
		Revoked:             revocation.Revoked,
		RevokedByAdvisory:   revocation.RevokedByAdvisory,
		AdvisoryID:          revocation.AdvisoryID,
		AdvisoryTitle:       revocation.AdvisoryTitle,
		AdvisorySeverity:    revocation.AdvisorySeverity,
		Compatible:          compatible,
		CompatibilityError:  compatibilityError,
	}
	if cache, ok, err := s.repo.FindPackageCache(ctx, record.PackageID); err != nil {
		return Package{}, err
	} else if ok {
		item.CacheStatus = cache.Status
		item.CachePath = cache.CachePath
		if !cache.CachedAt.IsZero() {
			item.CachedAt = &cache.CachedAt
		}
	}
	if installation, ok, err := s.repo.FindPackageInstallation(ctx, record.PluginID); err != nil {
		return Package{}, err
	} else if ok && installation.PackageID == record.PackageID {
		item.InstallStatus = installation.Status
		if !installation.InstalledAt.IsZero() {
			item.InstalledAt = &installation.InstalledAt
		}
	}
	return item, nil
}

func (s *Service) recordPackageCache(ctx context.Context, record packageRecord, view Package, cachePath string, sizeBytes int64) (PackageDownloadResult, error) {
	now := s.now().UTC()
	cache := packageCacheRecord{
		PackageID: record.PackageID,
		PluginID:  record.PluginID,
		Version:   record.Version,
		OS:        record.OS,
		Arch:      record.Arch,
		SHA256:    record.SHA256,
		SizeBytes: sizeBytes,
		CachePath: cachePath,
		Status:    PackageCacheStatusCached,
		CachedAt:  now,
		UpdatedAt: now,
	}
	if err := s.repo.SavePackageCache(ctx, cache); err != nil {
		return PackageDownloadResult{}, err
	}
	view.CacheStatus = cache.Status
	view.CachePath = cache.CachePath
	view.CachedAt = &cache.CachedAt
	view.SizeBytes = sizeBytes
	return PackageDownloadResult{
		Package:   view,
		CachePath: cachePath,
		SHA256:    record.SHA256,
		SizeBytes: sizeBytes,
		CachedAt:  now,
	}, nil
}
