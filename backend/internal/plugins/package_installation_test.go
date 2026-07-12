package plugins

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

var errInstallationWrite = errors.New("installation write failed")

type failingInstallationRepository struct {
	*MemoryRepository
	failInstallation bool
}

func (r *failingInstallationRepository) SavePackageInstallation(ctx context.Context, record packageInstallationRecord) error {
	if r.failInstallation {
		return errInstallationWrite
	}
	return r.MemoryRepository.SavePackageInstallation(ctx, record)
}

func TestInstallPackageRollsBackActiveDirectoryWhenRepositoryWriteFails(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	cachePath := filepath.Join(root, "plugin.pkg")
	pluginID := "com.asterrouter.test.frontend"
	packageID := "pkg_test_frontend"
	version := "1.0.0"
	writeTestPluginArchive(t, cachePath, pluginID, version)

	memory := NewMemoryRepository()
	repo := &failingInstallationRepository{MemoryRepository: memory}
	now := time.Now().UTC()
	if err := repo.SavePlugin(ctx, Plugin{ID: pluginID, PluginID: pluginID, Name: "Test plugin", Status: StatusEnabled, Tier: TierFreeCore, EntitlementStatus: EntitlementFree, Surfaces: []string{"personal"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	compatibility := fmt.Sprintf(`[{"core_version_range":">=0.1.0","os":%q,"arch":%q,"result":"compatible"}]`, runtime.GOOS, runtime.GOARCH)
	record := packageRecord{PluginID: pluginID, PluginSlug: "test-frontend", PackageID: packageID, Version: version, Channel: "stable", OS: runtime.GOOS, Arch: runtime.GOARCH, CompatibilityJSON: compatibility, CreatedAt: now, UpdatedAt: now}
	if err := repo.SavePackage(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePackageCache(ctx, packageCacheRecord{PackageID: packageID, PluginID: pluginID, Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH, CachePath: cachePath, Status: PackageCacheStatusCached, CachedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	svc := NewServiceWithOptions(repo, ServiceOptions{PluginActiveDir: filepath.Join(root, "active"), PackageCacheDir: filepath.Join(root, "cache"), CoreVersion: "0.1.0"})
	repo.failInstallation = true
	if _, err := svc.InstallPackage(ctx, pluginID, packageID); !errors.Is(err, errInstallationWrite) {
		t.Fatalf("InstallPackage() error = %v, want errInstallationWrite", err)
	}
	if _, err := os.Stat(svc.activePackageDir(pluginID, version)); !os.IsNotExist(err) {
		t.Fatalf("failed installation left active directory, stat error=%v", err)
	}
	if _, ok, err := memory.FindPackageInstallation(ctx, pluginID); err != nil || ok {
		t.Fatalf("failed installation persisted record: ok=%v err=%v", ok, err)
	}
}

func writeTestPluginArchive(t *testing.T, target, pluginID, version string) {
	t.Helper()
	file, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	manifest := []byte(fmt.Sprintf(`{"id":%q,"version":%q,"runtime":"frontend","entrypoint":{}}`, pluginID, version))
	if err := tarWriter.WriteHeader(&tar.Header{Name: "plugin.json", Mode: 0600, Size: int64(len(manifest)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(manifest); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
