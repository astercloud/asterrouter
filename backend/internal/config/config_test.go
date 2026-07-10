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
