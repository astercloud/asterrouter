package plugins

import (
	"encoding/json"
	"time"
)

const (
	StatusEnabled  = "enabled"
	StatusDisabled = "disabled"
	StatusLocked   = "locked"

	DeliveryStatusSucceeded = "succeeded"
	DeliveryStatusFailed    = "failed"
	DeliveryStatusSkipped   = "skipped"

	PackageCacheStatusCached  = "cached"
	PackageCacheStatusFailed  = "failed"
	PackageInstallInstalled   = "installed"
	PackageInstallUninstalled = "uninstalled"

	LicenseStatusActive  = "active"
	LicenseStatusExpired = "expired"
	LicenseStatusInvalid = "invalid"

	TierCore          = "core"
	TierFreeCore      = "free_core"
	TierProfileBundle = "profile_bundle"
	TierPaidAddon     = "paid_addon"

	EntitlementIncluded = "included"
	EntitlementFree     = "free"
	EntitlementMissing  = "missing"

	CatalogModeDisabled      = "disabled"
	CatalogModeOnline        = "online"
	CatalogModePrivateMirror = "private_mirror"
	CatalogModeOffline       = "offline"
)

type Plugin struct {
	ID                string    `json:"id"`
	PluginID          string    `json:"plugin_id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	Category          string    `json:"category"`
	Type              string    `json:"type"`
	Tier              string    `json:"tier"`
	Version           string    `json:"version"`
	Vendor            string    `json:"vendor"`
	Status            string    `json:"status"`
	EntitlementStatus string    `json:"entitlement_status"`
	Surfaces          []string  `json:"surfaces"`
	EntryPoint        string    `json:"entry_point"`
	Configurable      bool      `json:"configurable"`
	Packages          []Package `json:"packages,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Package struct {
	PluginID            string     `json:"plugin_id"`
	PackageID           string     `json:"package_id"`
	Version             string     `json:"version"`
	Channel             string     `json:"channel"`
	OS                  string     `json:"os"`
	Arch                string     `json:"arch"`
	SHA256              string     `json:"sha256"`
	SizeBytes           int64      `json:"size_bytes"`
	RequiredEntitlement bool       `json:"required_entitlement"`
	Revoked             bool       `json:"revoked"`
	RevokedByAdvisory   bool       `json:"revoked_by_advisory"`
	AdvisoryID          string     `json:"advisory_id,omitempty"`
	AdvisoryTitle       string     `json:"advisory_title,omitempty"`
	AdvisorySeverity    string     `json:"advisory_severity,omitempty"`
	Compatible          bool       `json:"compatible"`
	CompatibilityError  string     `json:"compatibility_error,omitempty"`
	CacheStatus         string     `json:"cache_status,omitempty"`
	CachePath           string     `json:"cache_path,omitempty"`
	CachedAt            *time.Time `json:"cached_at,omitempty"`
	InstallStatus       string     `json:"install_status,omitempty"`
	InstalledAt         *time.Time `json:"installed_at,omitempty"`
}

type PackageDownloadRequest struct {
	LicenseID        string `json:"license_id"`
	ActivationSecret string `json:"activation_secret"`
	InstanceID       string `json:"instance_id"`
}

type PackageImportRequest struct {
	ContentBase64 string          `json:"content_base64"`
	FileJSON      json.RawMessage `json:"file_json"`
}

type PackageDownloadResult struct {
	Package   Package   `json:"package"`
	CachePath string    `json:"cache_path"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"size_bytes"`
	CachedAt  time.Time `json:"cached_at"`
}

type PackageInstallation struct {
	PluginID    string    `json:"plugin_id"`
	PackageID   string    `json:"package_id"`
	Version     string    `json:"version"`
	OS          string    `json:"os"`
	Arch        string    `json:"arch"`
	CachePath   string    `json:"cache_path"`
	Status      string    `json:"status"`
	InstalledAt time.Time `json:"installed_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Config struct {
	PluginID    string            `json:"plugin_id"`
	Settings    map[string]string `json:"settings"`
	SecretHints map[string]string `json:"secret_hints"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type ConfigRequest struct {
	Settings map[string]string `json:"settings"`
	Secrets  map[string]string `json:"secrets"`
}

type DeliveryAttempt struct {
	ID            string    `json:"id"`
	PluginID      string    `json:"plugin_id"`
	AlertID       string    `json:"alert_id"`
	AlertType     string    `json:"alert_type"`
	AlertSeverity string    `json:"alert_severity"`
	Status        string    `json:"status"`
	Target        string    `json:"target"`
	HTTPStatus    int       `json:"http_status"`
	Error         string    `json:"error"`
	CreatedAt     time.Time `json:"created_at"`
}

type DeliveryQuery struct {
	PluginID string
	AlertID  string
	Status   string
	Limit    int
	Offset   int
}

type configRecord struct {
	PluginID          string
	Settings          map[string]string
	SecretCiphertexts map[string]string
	SecretHints       map[string]string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Summary struct {
	Total        int `json:"total"`
	Enabled      int `json:"enabled"`
	Free         int `json:"free"`
	PaidLocked   int `json:"paid_locked"`
	Configurable int `json:"configurable"`
}

type Catalog struct {
	Summary Summary  `json:"summary"`
	Plugins []Plugin `json:"plugins"`
}

type OfficialCatalogConfig struct {
	Mode            string
	BootstrapURL    string
	URL             string
	LicenseURL      string
	PublicKeyID     string
	PublicKeyBase64 string
}

type OfficialLicenseConfig struct {
	URL             string
	PublicKeyID     string
	PublicKeyBase64 string
	InstanceID      string
	Fingerprint     string
	DisplayName     string
}

type OfficialCatalogStatus struct {
	Mode            string    `json:"mode"`
	BootstrapURL    string    `json:"bootstrap_url,omitempty"`
	SourceURL       string    `json:"source_url"`
	LicenseURL      string    `json:"license_url,omitempty"`
	TrustConfigured bool      `json:"trust_configured"`
	CatalogVersion  int64     `json:"catalog_version"`
	PayloadSHA256   string    `json:"payload_sha256"`
	KeyID           string    `json:"key_id"`
	PluginCount     int       `json:"plugin_count"`
	AdvisoryCount   int       `json:"advisory_count"`
	Status          string    `json:"status"`
	Error           string    `json:"error,omitempty"`
	SyncedAt        time.Time `json:"synced_at,omitempty"`
}

type LicenseStatus struct {
	Configured      bool          `json:"configured"`
	LicenseID       string        `json:"license_id,omitempty"`
	CustomerID      string        `json:"customer_id,omitempty"`
	InstanceID      string        `json:"instance_id,omitempty"`
	SnapshotVersion int64         `json:"snapshot_version,omitempty"`
	Status          string        `json:"status"`
	Edition         string        `json:"edition,omitempty"`
	KeyID           string        `json:"key_id,omitempty"`
	EnvelopeSHA256  string        `json:"envelope_sha256,omitempty"`
	Entitlements    []Entitlement `json:"entitlements,omitempty"`
	IssuedAt        time.Time     `json:"issued_at,omitempty"`
	ExpiresAt       time.Time     `json:"expires_at,omitempty"`
	ImportedAt      time.Time     `json:"imported_at,omitempty"`
	Error           string        `json:"error,omitempty"`
}

type Entitlement struct {
	PublicID    string     `json:"public_id"`
	Type        string     `json:"type"`
	ResourceKey string     `json:"resource_key"`
	Status      string     `json:"status"`
	StartsAt    time.Time  `json:"starts_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type LicenseActivateRequest struct {
	LicenseID        string `json:"license_id"`
	ActivationSecret string `json:"activation_secret"`
	InstanceID       string `json:"instance_id"`
	Fingerprint      string `json:"instance_fingerprint"`
	DisplayName      string `json:"display_name"`
}

type LicenseImportRequest struct {
	Envelope         json.RawMessage `json:"envelope"`
	FileJSON         json.RawMessage `json:"file_json"`
	ActivationSecret string          `json:"activation_secret"`
}

type catalogSnapshotRecord struct {
	ID             string
	Mode           string
	SourceURL      string
	CatalogVersion int64
	PayloadSHA256  string
	KeyID          string
	Signature      string
	PluginCount    int
	AdvisoryCount  int
	Status         string
	Error          string
	PayloadJSON    string
	SyncedAt       time.Time
}

type advisoryRecord struct {
	PublicID      string
	AdvisoryID    string
	Severity      string
	Title         string
	Summary       string
	PublishedAt   time.Time
	SignatureJSON string
	Affected      []affectedVersionRecord
	SyncedAt      time.Time
}

type affectedVersionRecord struct {
	PublicID         string
	AdvisoryPublicID string
	AdvisoryID       string
	AdvisorySeverity string
	AdvisoryTitle    string
	PluginID         string
	PluginSlug       string
	VersionRange     string
	FixedVersion     string
	Revoked          bool
	CreatedAt        time.Time
}

type packageRecord struct {
	PluginID            string
	PluginSlug          string
	PluginPublicID      string
	VersionPublicID     string
	Version             string
	Channel             string
	RequiredEntitlement bool
	MinCoreVersion      string
	MaxCoreVersion      string
	PackageID           string
	PackageURI          string
	OS                  string
	Arch                string
	SHA256              string
	SizeBytes           int64
	SignatureJSON       string
	Revoked             bool
	CompatibilityJSON   string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type packageCacheRecord struct {
	PackageID string
	PluginID  string
	Version   string
	OS        string
	Arch      string
	SHA256    string
	SizeBytes int64
	CachePath string
	Status    string
	Error     string
	CachedAt  time.Time
	UpdatedAt time.Time
}

type packageInstallationRecord struct {
	PluginID    string
	PackageID   string
	Version     string
	OS          string
	Arch        string
	CachePath   string
	Status      string
	InstalledAt time.Time
	UpdatedAt   time.Time
}

type licenseRecord struct {
	LicenseID                  string
	CustomerID                 string
	InstanceID                 string
	SnapshotVersion            int64
	Status                     string
	Edition                    string
	KeyID                      string
	EnvelopeSHA256             string
	EnvelopeJSON               string
	ActivationSecretCiphertext string
	ActivationSecretHint       string
	EntitlementsJSON           string
	IssuedAt                   time.Time
	ExpiresAt                  time.Time
	ImportedAt                 time.Time
	UpdatedAt                  time.Time
	Error                      string
}
