package plugins

import "time"

const (
	StatusEnabled  = "enabled"
	StatusDisabled = "disabled"
	StatusLocked   = "locked"

	TierCore          = "core"
	TierFreeCore      = "free_core"
	TierProfileBundle = "profile_bundle"
	TierPaidAddon     = "paid_addon"

	EntitlementIncluded = "included"
	EntitlementFree     = "free"
	EntitlementMissing  = "missing"
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
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
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
