package config

import (
	"errors"
	"os"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/buildinfo"
)

const localDevelopmentSecret = "asterrouter-local-development-secret"

type Config struct {
	Addr              string
	AdminToken        string
	AdminUsername     string
	AdminPassword     string
	DatabaseURL       string
	FrontendDir       string
	Profile           string
	PublicBase        string
	SecretKey         string
	Version           string
	BuildType         string
	UpdateManifestURL string
	AllowRestart      bool
}

func Load() Config {
	return Config{
		Addr:              getEnv("ASTER_ADDR", ":8080"),
		AdminToken:        strings.TrimSpace(os.Getenv("ASTER_ADMIN_TOKEN")),
		AdminUsername:     getEnv("ASTER_ADMIN_USERNAME", "admin"),
		AdminPassword:     strings.TrimSpace(os.Getenv("ASTER_ADMIN_PASSWORD")),
		DatabaseURL:       strings.TrimSpace(os.Getenv("DATABASE_URL")),
		FrontendDir:       getEnv("ASTER_FRONTEND_DIR", "../frontend/dist"),
		Profile:           normalizeProfile(os.Getenv("ASTER_PROFILE")),
		PublicBase:        strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")),
		SecretKey:         getEnv("ASTER_SECRET_KEY", localDevelopmentSecret),
		Version:           getEnv("ASTER_VERSION", buildinfo.Version),
		BuildType:         getEnv("ASTER_BUILD_TYPE", buildinfo.BuildType),
		UpdateManifestURL: strings.TrimSpace(os.Getenv("ASTER_UPDATE_MANIFEST_URL")),
		AllowRestart:      getBoolEnv("ASTER_ALLOW_RESTART"),
	}
}

func ValidateRuntime(cfg Config) error {
	if cfg.BuildType != "release" {
		return nil
	}
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return errors.New("DATABASE_URL is required for release deployments")
	}
	if strings.TrimSpace(cfg.SecretKey) == localDevelopmentSecret {
		return errors.New("ASTER_SECRET_KEY must be set to a stable production secret")
	}
	if strings.TrimSpace(cfg.AdminPassword) == "" && strings.TrimSpace(cfg.AdminToken) == "" {
		return errors.New("ASTER_ADMIN_PASSWORD or ASTER_ADMIN_TOKEN is required for release deployments")
	}
	return nil
}

func getEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func normalizeProfile(value string) string {
	switch strings.TrimSpace(value) {
	case "personal", "relay_operator", "enterprise":
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func getBoolEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
