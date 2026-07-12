package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	catalogBootstrapSchema = "astercloud.plugin-client-bootstrap.v1"
	maxBootstrapBytes      = 1024 * 1024
)

type catalogBootstrap struct {
	SchemaVersion string                `json:"schema_version"`
	CatalogURL    string                `json:"catalog_url"`
	ServicesURL   string                `json:"services_url"`
	LicenseURL    string                `json:"license_url"`
	RedeemURL     string                `json:"redeem_url"`
	SigningKeys   []catalogBootstrapKey `json:"signing_keys"`
	GeneratedAt   time.Time             `json:"generated_at"`
}

type catalogBootstrapKey struct {
	KeyID     string    `json:"key_id"`
	Purpose   string    `json:"purpose"`
	Algorithm string    `json:"algorithm"`
	Status    string    `json:"status"`
	PublicKey string    `json:"public_key"`
	Encoding  string    `json:"encoding"`
	NotBefore time.Time `json:"not_before"`
}

func (s *Service) effectiveCatalogConfig(ctx context.Context) (OfficialCatalogConfig, error) {
	cfg := normalizeOfficialCatalogConfig(s.catalogConfig)
	if cfg.Mode != CatalogModeOnline && cfg.Mode != CatalogModePrivateMirror {
		return cfg, nil
	}
	if cfg.URL != "" && cfg.PublicKeyID != "" && cfg.PublicKeyBase64 != "" {
		return cfg, nil
	}
	if cfg.BootstrapURL == "" {
		return cfg, nil
	}
	bootstrap, err := s.fetchCatalogBootstrap(ctx, cfg.BootstrapURL)
	if err != nil {
		return cfg, err
	}
	applyCatalogBootstrap(&cfg, bootstrap)
	return normalizeOfficialCatalogConfig(cfg), nil
}

func (s *Service) effectiveLicenseConfig(ctx context.Context) (OfficialLicenseConfig, error) {
	catalogCfg, err := s.effectiveCatalogConfig(ctx)
	if err != nil {
		return OfficialLicenseConfig{}, err
	}
	licenseCfg := normalizeOfficialLicenseConfig(s.licenseConfig, catalogCfg)
	return licenseCfg, nil
}

func (s *Service) fetchCatalogBootstrap(ctx context.Context, sourceURL string) (catalogBootstrap, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(sourceURL), nil)
	if err != nil {
		return catalogBootstrap{}, err
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return catalogBootstrap{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return catalogBootstrap{}, fmt.Errorf("official catalog bootstrap returned status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBootstrapBytes+1))
	if err != nil {
		return catalogBootstrap{}, err
	}
	if len(body) > maxBootstrapBytes {
		return catalogBootstrap{}, fmt.Errorf("official catalog bootstrap response exceeds maximum size")
	}
	bootstrap, err := decodeCatalogBootstrap(body)
	if err != nil {
		return catalogBootstrap{}, err
	}
	if err := validateCatalogBootstrap(bootstrap); err != nil {
		return catalogBootstrap{}, err
	}
	return bootstrap, nil
}

func decodeCatalogBootstrap(body []byte) (catalogBootstrap, error) {
	var direct catalogBootstrap
	if err := json.Unmarshal(body, &direct); err == nil && direct.SchemaVersion != "" {
		return direct, nil
	}
	var wrapped struct {
		Data catalogBootstrap `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return catalogBootstrap{}, err
	}
	if wrapped.Data.SchemaVersion == "" {
		return catalogBootstrap{}, fmt.Errorf("official catalog bootstrap is missing")
	}
	return wrapped.Data, nil
}

func validateCatalogBootstrap(bootstrap catalogBootstrap) error {
	if bootstrap.SchemaVersion != catalogBootstrapSchema || !isHTTPURL(bootstrap.CatalogURL) || len(bootstrap.SigningKeys) == 0 {
		return fmt.Errorf("invalid official catalog bootstrap")
	}
	if bootstrap.LicenseURL != "" && !isHTTPURL(bootstrap.LicenseURL) {
		return fmt.Errorf("invalid official license URL in bootstrap")
	}
	if bootstrap.ServicesURL != "" && !isHTTPURL(bootstrap.ServicesURL) {
		return fmt.Errorf("invalid official services URL in bootstrap")
	}
	if bootstrap.RedeemURL != "" && !isHTTPURL(bootstrap.RedeemURL) {
		return fmt.Errorf("invalid official redeem URL in bootstrap")
	}
	for _, key := range bootstrap.SigningKeys {
		if usableBootstrapKey(key) {
			return nil
		}
	}
	return fmt.Errorf("official catalog bootstrap has no usable signing key")
}

func applyCatalogBootstrap(cfg *OfficialCatalogConfig, bootstrap catalogBootstrap) {
	if cfg.URL == "" {
		cfg.URL = strings.TrimSpace(bootstrap.CatalogURL)
	}
	if cfg.LicenseURL == "" {
		cfg.LicenseURL = strings.TrimSpace(bootstrap.LicenseURL)
	}
	if cfg.ServicesURL == "" {
		cfg.ServicesURL = strings.TrimSpace(bootstrap.ServicesURL)
	}
	if cfg.RedeemURL == "" {
		cfg.RedeemURL = strings.TrimSpace(bootstrap.RedeemURL)
	}
	if cfg.PublicKeyID != "" && cfg.PublicKeyBase64 != "" {
		return
	}
	for _, key := range bootstrap.SigningKeys {
		if !usableBootstrapKey(key) {
			continue
		}
		if cfg.PublicKeyID == "" {
			cfg.PublicKeyID = strings.TrimSpace(key.KeyID)
		}
		if cfg.PublicKeyBase64 == "" {
			cfg.PublicKeyBase64 = strings.TrimSpace(key.PublicKey)
		}
		break
	}
}

func usableBootstrapKey(key catalogBootstrapKey) bool {
	status := strings.TrimSpace(key.Status)
	return strings.TrimSpace(key.KeyID) != "" &&
		strings.TrimSpace(key.PublicKey) != "" &&
		strings.TrimSpace(key.Algorithm) == "Ed25519" &&
		(status == "" || status == "active" || status == "retired")
}

func normalizeBootstrapURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimSpace(value)
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
