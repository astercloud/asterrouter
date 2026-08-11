package plugins

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMapRemoteCatalogPluginsUsesEnterpriseMetadata(t *testing.T) {
	manifestPayload, err := json.Marshal(map[string]any{
		"schema_version": "astercloud.plugin-manifest.v1",
		"plugin":         "imagegen-workbench",
		"version":        "0.3.2",
		"manifest":       map[string]any{"configurable": true},
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
	if !plugins[0].Configurable {
		t.Fatal("manifest configurable flag was not preserved")
	}
}
