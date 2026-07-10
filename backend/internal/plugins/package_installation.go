package plugins

import (
	"context"
	"fmt"
	"strings"
)

func (s *Service) InstallPackage(ctx context.Context, pluginID string, packageID string) (PackageInstallation, error) {
	pluginID = strings.TrimSpace(pluginID)
	packageID = strings.TrimSpace(packageID)
	record, ok, err := s.repo.FindPackage(ctx, packageID)
	if err != nil {
		return PackageInstallation{}, err
	}
	if !ok || record.PluginID != pluginID {
		return PackageInstallation{}, ErrPackageNotFound
	}
	view, err := s.packageView(ctx, record)
	if err != nil {
		return PackageInstallation{}, err
	}
	if view.Revoked {
		return PackageInstallation{}, ErrPackageRevoked
	}
	if !view.Compatible {
		return PackageInstallation{}, fmt.Errorf("%w: %s", ErrPackageIncompatible, view.CompatibilityError)
	}
	if record.RequiredEntitlement {
		if _, _, ok, err := s.localLicenseForPackage(ctx, record); err != nil {
			return PackageInstallation{}, err
		} else if !ok {
			return PackageInstallation{}, ErrPluginLocked
		}
	}
	cache, ok, err := s.repo.FindPackageCache(ctx, record.PackageID)
	if err != nil {
		return PackageInstallation{}, err
	}
	if !ok || cache.Status != PackageCacheStatusCached || strings.TrimSpace(cache.CachePath) == "" {
		return PackageInstallation{}, ErrPackageNotCached
	}
	now := s.now().UTC()
	installation := packageInstallationRecord{
		PluginID:    record.PluginID,
		PackageID:   record.PackageID,
		Version:     record.Version,
		OS:          record.OS,
		Arch:        record.Arch,
		CachePath:   cache.CachePath,
		Status:      PackageInstallInstalled,
		InstalledAt: now,
		UpdatedAt:   now,
	}
	if err := s.repo.SavePackageInstallation(ctx, installation); err != nil {
		return PackageInstallation{}, err
	}
	return packageInstallationFromRecord(installation), nil
}

func (s *Service) UninstallPackage(ctx context.Context, pluginID string, packageID string) (PackageInstallation, error) {
	pluginID = strings.TrimSpace(pluginID)
	packageID = strings.TrimSpace(packageID)
	installation, ok, err := s.repo.FindPackageInstallation(ctx, pluginID)
	if err != nil {
		return PackageInstallation{}, err
	}
	if !ok || installation.Status != PackageInstallInstalled || installation.PackageID != packageID {
		return PackageInstallation{}, ErrPackageNotInstalled
	}
	installation.Status = PackageInstallUninstalled
	installation.UpdatedAt = s.now().UTC()
	if err := s.repo.SavePackageInstallation(ctx, installation); err != nil {
		return PackageInstallation{}, err
	}
	return packageInstallationFromRecord(installation), nil
}

func packageInstallationFromRecord(record packageInstallationRecord) PackageInstallation {
	return PackageInstallation{
		PluginID:    record.PluginID,
		PackageID:   record.PackageID,
		Version:     record.Version,
		OS:          record.OS,
		Arch:        record.Arch,
		CachePath:   record.CachePath,
		Status:      record.Status,
		InstalledAt: record.InstalledAt,
		UpdatedAt:   record.UpdatedAt,
	}
}
