package controlplane

import "time"

const (
	ProviderAccountModelSourceDiscovered = "discovered"
	ProviderAccountModelSourceManual     = "manual"

	ProviderAccountModelAvailabilityAvailable  = "available"
	ProviderAccountModelAvailabilityMissing    = "missing"
	ProviderAccountModelAvailabilityUnverified = "unverified"

	ProviderAccountModelChangeAdded     = "added"
	ProviderAccountModelChangeMissing   = "missing"
	ProviderAccountModelChangeUnchanged = "unchanged"
)

// ProviderAccountModel records one upstream model observed or configured for
// a provider account. ProviderAccount.Models remains the authoritative enabled
// list used by routing; this record adds provenance and discovery state.
type ProviderAccountModel struct {
	ProviderAccountID string     `json:"provider_account_id"`
	ModelID           string     `json:"model_id"`
	Source            string     `json:"source"`
	Enabled           bool       `json:"enabled"`
	Availability      string     `json:"availability"`
	Change            string     `json:"change,omitempty"`
	RouteCount        int        `json:"route_count"`
	FirstSeenAt       time.Time  `json:"first_seen_at"`
	LastSeenAt        *time.Time `json:"last_seen_at,omitempty"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type ProviderAccountModelInventory struct {
	AccountID           string                 `json:"account_id"`
	AutoEnableNewModels bool                   `json:"auto_enable_new_models"`
	LastDiscoveredAt    *time.Time             `json:"last_discovered_at,omitempty"`
	Models              []ProviderAccountModel `json:"models"`
}

type ProviderAccountModelDiscovery struct {
	AccountID        string                 `json:"account_id"`
	DiscoveredAt     time.Time              `json:"discovered_at"`
	Models           []ProviderAccountModel `json:"models"`
	AddedModels      []string               `json:"added_models"`
	MissingModels    []string               `json:"missing_models"`
	UnchangedModels  []string               `json:"unchanged_models"`
	AffectedRouteIDs []string               `json:"affected_route_ids"`
}

type ProviderAccountModelSyncRequest struct {
	EnabledModels       []string `json:"enabled_models"`
	AutoEnableNewModels bool     `json:"auto_enable_new_models"`
}

type ProviderAccountModelSyncResult struct {
	Account   ProviderAccount               `json:"account"`
	Inventory ProviderAccountModelInventory `json:"inventory"`
	Discovery ProviderAccountModelDiscovery `json:"discovery"`
}
