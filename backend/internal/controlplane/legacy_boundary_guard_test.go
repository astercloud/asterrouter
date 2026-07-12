package controlplane

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProjectApplicationBoundaryCannotBeRestored(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve guard location")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../.."))
	forbidden := []string{
		"project_id",
		"application_id",
		"project_budget",
		"project_admin",
		"RoleProjectAdmin",
		"RoleScopeProject",
		"CostAllocationByProject",
		"CostAllocationByApplication",
		"/projects",
		"/applications",
	}
	for _, relativeRoot := range []string{"backend", "frontend/src", "README.md"} {
		root := filepath.Join(repositoryRoot, relativeRoot)
		info, err := os.Stat(root)
		if err != nil {
			t.Fatalf("stat %s: %v", root, err)
		}
		if !info.IsDir() {
			assertFileExcludesLegacyBoundary(t, root, forbidden)
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "node_modules" || entry.Name() == "dist" {
					return filepath.SkipDir
				}
				return nil
			}
			if path == currentFile {
				return nil
			}
			switch filepath.Ext(path) {
			case ".go", ".sql", ".ts", ".vue", ".md":
				assertFileExcludesLegacyBoundary(t, path, forbidden)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", root, err)
		}
	}
}

func assertFileExcludesLegacyBoundary(t *testing.T, path string, forbidden []string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	content := string(raw)
	for _, token := range forbidden {
		if strings.Contains(content, token) {
			t.Errorf("legacy boundary token %q found in %s", token, path)
		}
	}
}
