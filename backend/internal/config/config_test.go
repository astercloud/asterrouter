package config

import "testing"

func TestValidateRuntimeAllowsSourceBuildWithoutDatabase(t *testing.T) {
	cfg := Config{BuildType: "source"}

	if err := ValidateRuntime(cfg); err != nil {
		t.Fatalf("ValidateRuntime() = %v, want nil", err)
	}
}

func TestValidateRuntimeRequiresReleaseDatabase(t *testing.T) {
	cfg := Config{
		BuildType:     "release",
		SecretKey:     "stable-secret",
		AdminPassword: "change-me",
	}

	if err := ValidateRuntime(cfg); err == nil {
		t.Fatalf("ValidateRuntime() = nil, want error")
	}
}

func TestValidateRuntimeAllowsDemoModeAsReleaseAuthMechanism(t *testing.T) {
	cfg := Config{
		BuildType:   "release",
		DatabaseURL: "postgres://asterrouter:pass@localhost:5432/asterrouter?sslmode=disable",
		SecretKey:   "stable-secret",
		DemoMode:    true,
	}

	if err := ValidateRuntime(cfg); err != nil {
		t.Fatalf("ValidateRuntime() = %v, want nil", err)
	}
}

func TestValidateRuntimeRequiresProductionSecret(t *testing.T) {
	cfg := Config{
		BuildType:     "release",
		DatabaseURL:   "postgres://asterrouter:pass@localhost:5432/asterrouter?sslmode=disable",
		SecretKey:     localDevelopmentSecret,
		AdminPassword: "change-me",
	}

	if err := ValidateRuntime(cfg); err == nil {
		t.Fatalf("ValidateRuntime() = nil, want error")
	}
}

func TestValidateRuntimeRequiresCatalogTrustMaterialWhenOnline(t *testing.T) {
	cfg := Config{
		BuildType:     "release",
		DatabaseURL:   "postgres://asterrouter:pass@localhost:5432/asterrouter?sslmode=disable",
		SecretKey:     "stable-secret",
		AdminPassword: "change-me",
		CatalogMode:   "online",
		CatalogURL:    "https://catalog.example.test/official/v1/catalog/index",
	}

	if err := ValidateRuntime(cfg); err == nil {
		t.Fatalf("ValidateRuntime() = nil, want error")
	}
}

func TestValidateRuntimeRequiresCatalogTrustMaterialWhenPrivateMirror(t *testing.T) {
	cfg := Config{
		BuildType:     "release",
		DatabaseURL:   "postgres://asterrouter:pass@localhost:5432/asterrouter?sslmode=disable",
		SecretKey:     "stable-secret",
		AdminPassword: "change-me",
		CatalogMode:   "private_mirror",
		CatalogURL:    "https://mirror.example.test/official/v1/catalog/index",
	}

	if err := ValidateRuntime(cfg); err == nil {
		t.Fatalf("ValidateRuntime() = nil, want error")
	}
}

func TestValidateRuntimeRequiresCatalogTrustMaterialWhenOffline(t *testing.T) {
	cfg := Config{
		BuildType:     "release",
		DatabaseURL:   "postgres://asterrouter:pass@localhost:5432/asterrouter?sslmode=disable",
		SecretKey:     "stable-secret",
		AdminPassword: "change-me",
		CatalogMode:   "offline",
	}

	if err := ValidateRuntime(cfg); err == nil {
		t.Fatalf("ValidateRuntime() = nil, want error")
	}
}

func TestDefaultPluginHostURLUsesLoopbackForWildcardListenAddress(t *testing.T) {
	if got := defaultPluginHostURL(":8080"); got != "http://127.0.0.1:8080/api/v1/plugin-host" {
		t.Fatalf("defaultPluginHostURL(:8080) = %q", got)
	}
	if got := defaultPluginHostURL("0.0.0.0:18090"); got != "http://127.0.0.1:18090/api/v1/plugin-host" {
		t.Fatalf("defaultPluginHostURL(0.0.0.0:18090) = %q", got)
	}
}
