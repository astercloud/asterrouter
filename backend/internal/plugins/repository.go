package plugins

import (
	"context"
	"time"
)

type Repository interface {
	ListPlugins(ctx context.Context) ([]Plugin, error)
	SavePlugin(ctx context.Context, plugin Plugin) error
	FindPlugin(ctx context.Context, id string) (Plugin, bool, error)
	UpdateStatus(ctx context.Context, id string, status string, updatedAt time.Time) error
	FindConfig(ctx context.Context, pluginID string) (configRecord, bool, error)
	SaveConfig(ctx context.Context, record configRecord) error
	QueryDeliveryAttempts(ctx context.Context, query DeliveryQuery) ([]DeliveryAttempt, error)
	SaveDeliveryAttempt(ctx context.Context, attempt DeliveryAttempt) error
	SaveCatalogSnapshot(ctx context.Context, record catalogSnapshotRecord) error
	LatestCatalogSnapshot(ctx context.Context) (catalogSnapshotRecord, bool, error)
	SavePackage(ctx context.Context, record packageRecord) error
	ListPackages(ctx context.Context, pluginID string) ([]packageRecord, error)
	FindPackage(ctx context.Context, packageID string) (packageRecord, bool, error)
	SaveAdvisory(ctx context.Context, record advisoryRecord) error
	ListRevokedAffectedVersions(ctx context.Context, pluginID string) ([]affectedVersionRecord, error)
	SavePackageCache(ctx context.Context, record packageCacheRecord) error
	FindPackageCache(ctx context.Context, packageID string) (packageCacheRecord, bool, error)
	SavePackageInstallation(ctx context.Context, record packageInstallationRecord) error
	FindPackageInstallation(ctx context.Context, pluginID string) (packageInstallationRecord, bool, error)
	SaveLicense(ctx context.Context, record licenseRecord) error
	LatestLicense(ctx context.Context) (licenseRecord, bool, error)
	Health(ctx context.Context) error
	Close() error
}

func NewRepository(ctx context.Context, databaseURL string) (Repository, string, error) {
	if databaseURL == "" {
		return NewMemoryRepository(), "memory", nil
	}
	repo, err := NewPostgresRepository(ctx, databaseURL)
	if err != nil {
		return nil, "", err
	}
	return repo, "postgres", nil
}
