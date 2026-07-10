package system

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func TestCheckUpdateWithoutManifestReturnsManualWarning(t *testing.T) {
	svc := NewService(Config{Version: "0.1.0", BuildType: "source"})

	info, err := svc.CheckUpdate(context.Background(), false, "stable")
	if err != nil {
		t.Fatalf("CheckUpdate(): %v", err)
	}

	if info.CurrentVersion != "0.1.0" || info.LatestVersion != "0.1.0" {
		t.Fatalf("unexpected versions: %+v", info)
	}
	if info.ManifestConfigured {
		t.Fatalf("manifest should not be configured: %+v", info)
	}
	if info.UpdateSupported || info.Warning == "" {
		t.Fatalf("expected manual warning without update support: %+v", info)
	}
}

func TestCheckUpdateSelectsCompatibleManifestAsset(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := manifestFile{
			Releases: []manifestRelease{
				{
					Version: "0.2.0",
					Channel: "stable",
					Name:    "0.2.0",
					Assets: []Asset{
						{
							Name:   "asterrouter",
							URL:    serverURL + "/asterrouter",
							OS:     runtime.GOOS,
							Arch:   runtime.GOARCH,
							SHA256: "abc",
							Size:   123,
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()
	serverURL = srv.URL

	svc := NewService(Config{Version: "0.1.0", BuildType: "release", ManifestURL: srv.URL})

	info, err := svc.CheckUpdate(context.Background(), true, "stable")
	if err != nil {
		t.Fatalf("CheckUpdate(): %v", err)
	}

	if !info.HasUpdate || !info.UpdateSupported {
		t.Fatalf("expected supported update: %+v", info)
	}
	if info.ReleaseInfo == nil || info.ReleaseInfo.Asset == nil || info.ReleaseInfo.Asset.URL == "" {
		t.Fatalf("compatible asset not selected: %+v", info)
	}
}

func TestPerformUpdateSourceBuildRequiresManualUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(manifestFile{
			Version: "0.2.0",
			Assets: []Asset{
				{OS: runtime.GOOS, Arch: runtime.GOARCH, URL: "https://example.com/asterrouter", SHA256: "abc"},
			},
		})
	}))
	defer srv.Close()

	svc := NewService(Config{Version: "0.1.0", BuildType: "source", ManifestURL: srv.URL})

	result, err := svc.PerformUpdate(context.Background(), "stable", "op1")
	if !errors.Is(err, ErrUpdateUnsupported) {
		t.Fatalf("err = %v, want ErrUpdateUnsupported", err)
	}
	if result.ManualAction == "" || result.OperationID != "op1" {
		t.Fatalf("manual result incomplete: %+v", result)
	}
}
