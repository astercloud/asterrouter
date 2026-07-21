package plugins

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMapRemoteCatalogPluginsUsesManifestSurfaces(t *testing.T) {
	manifestPayload, err := json.Marshal(map[string]any{
		"schema_version": "astercloud.plugin-manifest.v1",
		"plugin":         "imagegen-workbench",
		"version":        "0.3.2",
		"manifest": map[string]any{
			"surfaces": []string{"personal", "enterprise", "personal"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	plugins := mapRemoteCatalogPlugins(remoteCatalogIndex{
		Plugins: []remoteCatalogPlugin{{
			PluginID: "com.asterrouter.imagegen.workbench",
			Slug:     "imagegen-workbench",
			Name:     "图片生成工作台",
			Tier:     "free",
			Versions: []remoteCatalogVersion{{
				Version:           "0.3.2",
				Status:            "published",
				ManifestSignature: catalogEnvelope{Payload: manifestPayload},
			}},
		}},
	}, time.Now())
	if len(plugins) != 1 {
		t.Fatalf("mapped plugins = %d, want 1", len(plugins))
	}
	if got, want := plugins[0].Surfaces, []string{"personal", "enterprise"}; !equalStringSlices(got, want) {
		t.Fatalf("surfaces = %#v, want %#v", got, want)
	}
}

func TestMapRemoteCatalogPluginsKeepsLegacyAdminSurface(t *testing.T) {
	plugins := mapRemoteCatalogPlugins(remoteCatalogIndex{
		Plugins: []remoteCatalogPlugin{{
			PluginID: "com.astercloud.catalog.legacy",
			Slug:     "legacy",
			Name:     "Legacy",
			Tier:     "free",
			Versions: []remoteCatalogVersion{{Version: "1.0.0", Status: "published"}},
		}},
	}, time.Now())
	if len(plugins) != 1 || !equalStringSlices(plugins[0].Surfaces, []string{"admin"}) {
		t.Fatalf("legacy surfaces = %#v, want [admin]", plugins[0].Surfaces)
	}
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
