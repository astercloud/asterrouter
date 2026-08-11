package controlplane

import (
	"errors"
	"time"
)

const (
	SupplyDimensionProviderAccount = "provider_account"
	SupplyDimensionRouteGroup      = "route_group"
	SupplyDimensionPublishedModel  = "published_model"
	SupplyDimensionApplication     = "application"

	SupplyCapacityAvailable  = "available"
	SupplyCapacityDegraded   = "degraded"
	SupplyCapacitySaturated  = "saturated"
	SupplyCapacityIdle       = "idle"
	SupplyCapacityStranded   = "stranded"
	SupplyCapacityUnknown    = "unknown"
	SupplyCapacityNoEvidence = "no_evidence"

	SupplyEvidenceKnown         = "known"
	SupplyEvidenceUnknown       = "unknown"
	SupplyEvidenceNotApplicable = "not_applicable"
	SupplyEvidenceNotComparable = "not_comparable"

	CapacityRecommendationObserveOnly  = "observe_only"
	CapacityRecommendationActionable   = "actionable"
	CapacityRecommendationInconclusive = "inconclusive"

	CapacityRecommendationIncrease       = "increase_capacity"
	CapacityRecommendationDefer          = "defer_expansion"
	CapacityRecommendationReviewStranded = "review_stranded_capacity"

	CapacityConstraintConcurrency = "concurrency"
	CapacityConstraintRPM         = "rpm"
	CapacityConstraintTPM         = "tpm"
	CapacityConstraintHealth      = "health"
	CapacityConstraintRouting     = "routing"
	CapacityConstraintUnknown     = "unknown"

	SupplyConfidenceHigh   = "high"
	SupplyConfidenceMedium = "medium"
	SupplyConfidenceLow    = "low"
)

var ErrSupplyWindowInvalid = errors.New("supply utilization window is invalid")

type SupplyUtilizationQuery struct {
	From      time.Time
	To        time.Time
	APIKeyIDs []string
}

type SupplyWindow struct {
	From             time.Time `json:"from"`
	To               time.Time `json:"to"`
	DurationSeconds  int64     `json:"duration_seconds"`
	TraceCount       int       `json:"trace_count"`
	UsageRecordCount int       `json:"usage_record_count"`
	Truncated        bool      `json:"truncated"`
}

type SupplyEvidenceFreshness struct {
	TraceAsOf    *time.Time `json:"trace_as_of,omitempty"`
	UsageAsOf    *time.Time `json:"usage_as_of,omitempty"`
	HealthAsOf   *time.Time `json:"health_as_of,omitempty"`
	CapacityAsOf time.Time  `json:"capacity_as_of"`
}

type SupplyEvidenceFilter struct {
	APIKeyID           string `json:"api_key_id,omitempty"`
	GatewayPrincipalID string `json:"gateway_principal_id,omitempty"`
	Model              string `json:"model,omitempty"`
	ProviderAccountID  string `json:"provider_account_id,omitempty"`
	RouteGroup         string `json:"route_group,omitempty"`
	GatewayModelID     string `json:"gateway_model_id,omitempty"`
}

type SupplyEvidenceSummary struct {
	TraceCount       int                  `json:"trace_count"`
	UsageRecordCount int                  `json:"usage_record_count"`
	AttemptCount     int                  `json:"attempt_count"`
	Sources          []string             `json:"sources"`
	Filter           SupplyEvidenceFilter `json:"filter"`
	Complete         bool                 `json:"complete"`
}

type SupplyDemandMetrics struct {
	Requests             int     `json:"requests"`
	SuccessfulRequests   int     `json:"successful_requests"`
	RejectedRequests     int     `json:"rejected_requests"`
	SuccessRate          float64 `json:"success_rate"`
	HTTP429Requests      int     `json:"http_429_requests"`
	HTTP5xxRequests      int     `json:"http_5xx_requests"`
	FallbackRequests     int     `json:"fallback_requests"`
	FallbackRate         float64 `json:"fallback_rate"`
	NoCandidateRequests  int     `json:"no_candidate_requests"`
	CapacityRejected     int     `json:"capacity_rejected_requests"`
	PolicyRejected       int     `json:"policy_rejected_requests"`
	AccountErrors        int     `json:"account_error_requests"`
	ProtocolIncompatible int     `json:"protocol_incompatible_requests"`
	UnclassifiedFailures int     `json:"unclassified_failure_requests"`
}

type SupplyTokenMetrics struct {
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	CacheReadTokens   int64  `json:"cache_read_tokens"`
	CacheWriteTokens  int64  `json:"cache_write_tokens"`
	ReasoningTokens   *int64 `json:"reasoning_tokens,omitempty"`
	ReasoningStatus   string `json:"reasoning_status"`
	NormalizationGaps int    `json:"normalization_gaps"`
}

type SupplyCostTotal struct {
	Currency       string `json:"currency"`
	CostMicros     int64  `json:"cost_micros"`
	PricedRequests int    `json:"priced_requests"`
}

type SupplyConcurrencyMetrics struct {
	Current int `json:"current"`
	P50     int `json:"p50"`
	P95     int `json:"p95"`
	P99     int `json:"p99"`
	Peak    int `json:"peak"`
}

type SupplyWatermark struct {
	Status       string  `json:"status"`
	Source       string  `json:"source"`
	Limit        int64   `json:"limit"`
	Current      int64   `json:"current"`
	Peak         int64   `json:"peak"`
	CurrentRatio float64 `json:"current_ratio"`
	PeakRatio    float64 `json:"peak_ratio"`
}

type SupplyWatermarks struct {
	RPM         SupplyWatermark `json:"rpm"`
	TPM         SupplyWatermark `json:"tpm"`
	Concurrency SupplyWatermark `json:"concurrency"`
}

type SupplyPeriodMetrics struct {
	PeakMinute      *time.Time `json:"peak_minute,omitempty"`
	PeakMinuteCalls int        `json:"peak_minute_calls"`
	IdleHours       int        `json:"idle_hours"`
	CooldownSeconds int64      `json:"cooldown_seconds"`
	HealthCoverage  float64    `json:"health_coverage"`
}

type SupplyUtilizationRow struct {
	Dimension          string                   `json:"dimension"`
	ID                 string                   `json:"id"`
	Name               string                   `json:"name"`
	ProviderID         string                   `json:"provider_id,omitempty"`
	GatewayModelID     string                   `json:"gateway_model_id,omitempty"`
	RouteGroup         string                   `json:"route_group,omitempty"`
	APIKeyID           string                   `json:"api_key_id,omitempty"`
	GatewayPrincipalID string                   `json:"gateway_principal_id,omitempty"`
	CapacityStatus     string                   `json:"capacity_status"`
	PrimaryConstraint  string                   `json:"primary_constraint"`
	UnknownCapacity    bool                     `json:"unknown_capacity"`
	StrandedCapacity   bool                     `json:"stranded_capacity"`
	StrandedReasons    []string                 `json:"stranded_reasons"`
	Demand             SupplyDemandMetrics      `json:"demand"`
	Tokens             SupplyTokenMetrics       `json:"tokens"`
	Costs              []SupplyCostTotal        `json:"costs"`
	UnpricedRequests   int                      `json:"unpriced_requests"`
	Concurrency        SupplyConcurrencyMetrics `json:"concurrency"`
	Watermarks         SupplyWatermarks         `json:"watermarks"`
	Period             SupplyPeriodMetrics      `json:"period"`
	Evidence           SupplyEvidenceSummary    `json:"evidence"`
}

type SupplyUtilizationReport struct {
	Window      SupplyWindow            `json:"window"`
	Freshness   SupplyEvidenceFreshness `json:"freshness"`
	Rows        []SupplyUtilizationRow  `json:"rows"`
	ByDimension map[string]int          `json:"by_dimension"`
}

type CapacityRecommendationEvidence struct {
	SampleCount              int       `json:"sample_count"`
	PeakWatermark            float64   `json:"peak_watermark"`
	CapacityRejectedRequests int       `json:"capacity_rejected_requests"`
	PolicyRejectedRequests   int       `json:"policy_rejected_requests"`
	UnclassifiedFailures     int       `json:"unclassified_failure_requests"`
	FallbackRate             float64   `json:"fallback_rate"`
	SuccessRate              float64   `json:"success_rate"`
	HealthCoverage           float64   `json:"health_coverage"`
	ObservedFrom             time.Time `json:"observed_from"`
	ObservedTo               time.Time `json:"observed_to"`
}

type CapacityRecommendation struct {
	ID                   string                         `json:"id"`
	Status               string                         `json:"status"`
	Type                 string                         `json:"type"`
	Target               SupplyEvidenceFilter           `json:"target"`
	TargetName           string                         `json:"target_name"`
	PrimaryConstraint    string                         `json:"primary_constraint"`
	Confidence           string                         `json:"confidence"`
	ReasonCodes          []string                       `json:"reason_codes"`
	CounterEvidence      []string                       `json:"counter_evidence"`
	MissingEvidence      []string                       `json:"missing_evidence"`
	AffectedApplications []string                       `json:"affected_applications"`
	AffectedModels       []string                       `json:"affected_models"`
	AffectedRouteGroups  []string                       `json:"affected_route_groups"`
	Evidence             CapacityRecommendationEvidence `json:"evidence"`
	Rollback             string                         `json:"rollback"`
}

type CapacityRecommendationSummary struct {
	Total        int `json:"total"`
	Actionable   int `json:"actionable"`
	Inconclusive int `json:"inconclusive"`
}

type CapacityRecommendationReport struct {
	Mode        string                        `json:"mode"`
	GeneratedAt time.Time                     `json:"generated_at"`
	Window      SupplyWindow                  `json:"window"`
	Summary     CapacityRecommendationSummary `json:"summary"`
	Items       []CapacityRecommendation      `json:"items"`
}
