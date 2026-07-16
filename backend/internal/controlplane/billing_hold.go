package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

var (
	ErrBillingHoldBudgetExceeded      = errors.New("billing hold exceeds available monthly budget")
	ErrBillingHoldEstimateUnavailable = errors.New("billing hold cost estimate is unavailable")
	ErrBillingHoldImageQuotaExceeded  = errors.New("billing hold exceeds the monthly image quota")
	ErrBillingHoldVideoQuotaExceeded  = errors.New("billing hold exceeds the monthly video quota")
	ErrBillingHoldAudioQuotaExceeded  = errors.New("billing hold exceeds the monthly audio quota")
	ErrBillingHoldUsageEstimate       = errors.New("billing hold requires a media usage estimate")
	ErrBillingHoldStateConflict       = errors.New("billing hold state changed concurrently")
)

const (
	BillingHoldStatusReserved  = "reserved"
	BillingHoldStatusCommitted = "committed"
	BillingHoldStatusSettled   = "settled"
	BillingHoldStatusReleased  = "released"
	BillingHoldStatusDisputed  = "disputed"

	BillingHoldDefaultTTL             = AIJobDefaultTTL
	billingHoldDefaultMaxOutputTokens = 4096
)

type BillingHold struct {
	ID                       string          `json:"id"`
	OperationID              string          `json:"operation_id"`
	ProfileScope             string          `json:"profile_scope"`
	TenantID                 string          `json:"tenant_id"`
	CredentialID             string          `json:"credential_id"`
	CredentialSource         string          `json:"credential_source"`
	IntegrationID            string          `json:"integration_id"`
	PrincipalType            string          `json:"principal_type"`
	PrincipalID              string          `json:"principal_id"`
	ExternalSubjectReference string          `json:"external_subject_reference"`
	RequestFingerprint       string          `json:"request_fingerprint"`
	Status                   string          `json:"status"`
	Version                  int             `json:"version"`
	ReservedAmountCents      int             `json:"reserved_amount_cents"`
	ReservedUsageDimensions  UsageDimensions `json:"reserved_usage_dimensions"`
	SettledAmountCents       int             `json:"settled_amount_cents"`
	Currency                 string          `json:"currency"`
	PriceSnapshotID          string          `json:"price_snapshot_id,omitempty"`
	EstimateSource           string          `json:"estimate_source"`
	Reason                   string          `json:"reason,omitempty"`
	BudgetPeriodStart        time.Time       `json:"budget_period_start"`
	ExpiresAt                time.Time       `json:"expires_at"`
	CreatedAt                time.Time       `json:"created_at"`
	UpdatedAt                time.Time       `json:"updated_at"`
	SettledAt                *time.Time      `json:"settled_at,omitempty"`
	ReleasedAt               *time.Time      `json:"released_at,omitempty"`
}

type BillingHoldAdmission struct {
	Hold                     BillingHold
	MonthlyBudgetCents       int
	MonthlyImageLimit        int
	MonthlyVideoSecondsLimit int
	MonthlyAudioSecondsLimit int
}

func (s *Service) newBillingHoldAdmission(ctx context.Context, operation AIOperation, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) (BillingHoldAdmission, error) {
	reserved, currency, priceID, source, err := s.estimateBillingHold(ctx, request)
	if err != nil {
		return BillingHoldAdmission{}, err
	}
	if auth.Limits.MonthlyBudgetCents > 0 && reserved == 0 && source == "unpriced" {
		return BillingHoldAdmission{}, ErrBillingHoldEstimateUnavailable
	}
	if auth.Limits.MonthlyBudgetCents > 0 && currency != "USD" {
		return BillingHoldAdmission{}, ErrBillingHoldEstimateUnavailable
	}
	reservedUsage, err := usageReservationForCanonicalRequest(request)
	if err != nil {
		return BillingHoldAdmission{}, err
	}
	if auth.Limits.MonthlyVideoSecondsLimit > 0 && request.Modality == "video" && request.VideoDurationMS <= 0 {
		return BillingHoldAdmission{}, ErrBillingHoldUsageEstimate
	}
	if auth.Limits.MonthlyAudioSecondsLimit > 0 && request.Modality == "audio" {
		durationMS := request.AudioDurationMS
		if request.Operation == GatewayOperationAudioTranscription || request.Operation == GatewayOperationAudioTranslation {
			durationMS = request.InputAudioDurationMS
		}
		if durationMS <= 0 {
			return BillingHoldAdmission{}, ErrBillingHoldUsageEstimate
		}
	}
	now := operation.CreatedAt.UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	hold := BillingHold{
		ID: "hold_" + randomID(12), OperationID: operation.ID, ProfileScope: operation.ProfileScope, TenantID: operation.TenantID,
		CredentialID: operation.CredentialID, CredentialSource: operation.CredentialSource, IntegrationID: operation.IntegrationID,
		PrincipalType: operation.PrincipalType, PrincipalID: operation.PrincipalID, ExternalSubjectReference: operation.ExternalSubjectReference,
		RequestFingerprint: operation.RequestFingerprint, Status: BillingHoldStatusReserved, Version: 1,
		ReservedAmountCents: reserved, ReservedUsageDimensions: reservedUsage,
		Currency: currency, PriceSnapshotID: priceID, EstimateSource: source,
		BudgetPeriodStart: periodStart, ExpiresAt: now.Add(BillingHoldDefaultTTL), CreatedAt: now, UpdatedAt: now,
	}
	return BillingHoldAdmission{
		Hold: hold, MonthlyBudgetCents: nonNegative(auth.Limits.MonthlyBudgetCents),
		MonthlyImageLimit:        nonNegative(auth.Limits.MonthlyImageLimit),
		MonthlyVideoSecondsLimit: nonNegative(auth.Limits.MonthlyVideoSecondsLimit),
		MonthlyAudioSecondsLimit: nonNegative(auth.Limits.MonthlyAudioSecondsLimit),
	}, nil
}

func usageReservationForCanonicalRequest(request gatewaycore.CanonicalRequest) (UsageDimensions, error) {
	reserved := UsageDimensions{}
	switch request.Modality {
	case GatewayModalityImage:
		count := request.OutputCount
		if count <= 0 && request.Operation == GatewayOperationImageGeneration {
			count = 1
		}
		if count > 0 {
			reserved[UsageDimensionOutputImages] = UsageDimension{Quantity: int64(count), Unit: UsageUnitCount, Source: "request", Confidence: UsageConfidenceEstimated}
		}
	case "video":
		if request.VideoDurationMS > 0 {
			reserved[UsageDimensionOutputVideoMilliseconds] = UsageDimension{Quantity: request.VideoDurationMS, Unit: UsageUnitMillisecond, Source: "request", Confidence: UsageConfidenceEstimated}
		}
	case "audio":
		if request.InputAudioDurationMS > 0 {
			reserved[UsageDimensionInputAudioMilliseconds] = UsageDimension{Quantity: request.InputAudioDurationMS, Unit: UsageUnitMillisecond, Source: "request", Confidence: UsageConfidenceEstimated}
		}
		if request.AudioDurationMS > 0 {
			reserved[UsageDimensionOutputAudioMilliseconds] = UsageDimension{Quantity: request.AudioDurationMS, Unit: UsageUnitMillisecond, Source: "request", Confidence: UsageConfidenceEstimated}
		}
	}
	return NormalizeUsageDimensions(reserved)
}

func (s *Service) estimateBillingHold(ctx context.Context, request gatewaycore.CanonicalRequest) (int, string, string, string, error) {
	var limits struct {
		MaxTokens           int `json:"max_tokens"`
		MaxCompletionTokens int `json:"max_completion_tokens"`
		MaxCostCents        int `json:"max_cost_cents"`
	}
	if len(request.Payload) > 0 {
		if err := json.Unmarshal(request.Payload, &limits); err != nil {
			return 0, "", "", "", err
		}
	}
	if limits.MaxTokens < 0 || limits.MaxCompletionTokens < 0 || limits.MaxCostCents < 0 {
		return 0, "", "", "", errors.New("billing hold request limits must be non-negative")
	}
	reserved := limits.MaxCostCents
	currency := "USD"
	priceID := ""
	source := "unpriced"
	pricing, found, err := s.modelPricingForModel(ctx, request.Model)
	if err != nil {
		return 0, "", "", "", err
	}
	if found {
		outputTokens := max(limits.MaxTokens, limits.MaxCompletionTokens)
		if outputTokens == 0 && request.Modality == GatewayModalityText {
			outputTokens = billingHoldDefaultMaxOutputTokens
		}
		inputTokens := max(1, len(request.Payload)/4)
		reserved = max(reserved, estimateCostCents(pricing, inputTokens, outputTokens))
		currency = pricing.Currency
		priceID = pricing.ID
		source = "model_pricing"
	} else if reserved > 0 {
		source = "request_max_cost"
	}
	return reserved, strings.ToUpper(strings.TrimSpace(currency)), priceID, source, nil
}

func validateBillingHoldAdmission(operation AIOperation, admission BillingHoldAdmission) error {
	hold := admission.Hold
	if _, err := NormalizeUsageDimensions(hold.ReservedUsageDimensions); err != nil {
		return err
	}
	if strings.TrimSpace(hold.ID) == "" || hold.OperationID != operation.ID || hold.CredentialID != operation.CredentialID ||
		hold.ProfileScope != operation.ProfileScope || hold.TenantID != operation.TenantID || hold.CredentialSource != operation.CredentialSource ||
		hold.IntegrationID != operation.IntegrationID || hold.PrincipalType != operation.PrincipalType || hold.PrincipalID != operation.PrincipalID ||
		hold.ExternalSubjectReference != operation.ExternalSubjectReference ||
		hold.RequestFingerprint == "" || hold.RequestFingerprint != operation.RequestFingerprint || hold.Status != BillingHoldStatusReserved ||
		hold.Version != 1 || hold.ReservedAmountCents < 0 || hold.SettledAmountCents != 0 || len(strings.TrimSpace(hold.Currency)) != 3 ||
		hold.BudgetPeriodStart.IsZero() || hold.CreatedAt.IsZero() || !hold.ExpiresAt.After(hold.CreatedAt) ||
		admission.MonthlyBudgetCents < 0 || admission.MonthlyImageLimit < 0 || admission.MonthlyVideoSecondsLimit < 0 || admission.MonthlyAudioSecondsLimit < 0 {
		return errors.New("invalid billing hold admission")
	}
	return nil
}

func billingHoldCountsAgainstBudget(status string) bool {
	return oneOf(status, BillingHoldStatusReserved, BillingHoldStatusCommitted, BillingHoldStatusDisputed)
}

func billingHoldTransitionAllowed(fromStatus, toStatus string) bool {
	switch fromStatus {
	case BillingHoldStatusReserved:
		return oneOf(toStatus, BillingHoldStatusCommitted, BillingHoldStatusSettled, BillingHoldStatusReleased, BillingHoldStatusDisputed)
	case BillingHoldStatusCommitted:
		return oneOf(toStatus, BillingHoldStatusSettled, BillingHoldStatusDisputed)
	case BillingHoldStatusDisputed:
		return oneOf(toStatus, BillingHoldStatusSettled, BillingHoldStatusReleased)
	default:
		return false
	}
}

func prepareBillingHoldTransition(hold BillingHold, toStatus string, settledAmount int, reason string, at time.Time) (BillingHold, error) {
	toStatus = strings.TrimSpace(toStatus)
	if hold.Status == toStatus {
		return hold, nil
	}
	if !billingHoldTransitionAllowed(hold.Status, toStatus) || settledAmount < 0 {
		return BillingHold{}, fmt.Errorf("invalid billing hold transition %s -> %s", hold.Status, toStatus)
	}
	hold.Status = toStatus
	hold.Version++
	hold.Reason = strings.TrimSpace(reason)
	hold.UpdatedAt = at.UTC()
	if toStatus == BillingHoldStatusSettled {
		hold.SettledAmountCents = settledAmount
		hold.SettledAt = timePointer(at.UTC())
	}
	if toStatus == BillingHoldStatusReleased {
		hold.ReleasedAt = timePointer(at.UTC())
	}
	return hold, nil
}

func (s *Service) BillingHoldForOperation(ctx context.Context, operationID string) (BillingHold, bool, error) {
	return s.repo.FindBillingHoldByOperationID(ctx, strings.TrimSpace(operationID))
}

func (s *Service) CommitBillingHold(ctx context.Context, operationID, reason string) error {
	return s.transitionBillingHold(ctx, operationID, BillingHoldStatusCommitted, 0, reason)
}

func (s *Service) DisputeBillingHold(ctx context.Context, operationID, reason string) error {
	return s.transitionBillingHold(ctx, operationID, BillingHoldStatusDisputed, 0, reason)
}

func (s *Service) ReleaseBillingHold(ctx context.Context, operationID, reason string) error {
	return s.transitionBillingHold(ctx, operationID, BillingHoldStatusReleased, 0, reason)
}

func (s *Service) transitionBillingHold(ctx context.Context, operationID, status string, settledAmount int, reason string) error {
	hold, found, err := s.repo.FindBillingHoldByOperationID(ctx, strings.TrimSpace(operationID))
	if err != nil || !found {
		return err
	}
	if hold.Status == status || oneOf(hold.Status, BillingHoldStatusSettled, BillingHoldStatusReleased) ||
		(hold.Status == BillingHoldStatusDisputed && status == BillingHoldStatusCommitted) {
		return nil
	}
	_, updated, err := s.repo.TransitionBillingHold(ctx, hold.OperationID, hold.Version, status, settledAmount, strings.TrimSpace(reason), s.nowUTC())
	if err != nil {
		return err
	}
	if !updated {
		return ErrBillingHoldStateConflict
	}
	return nil
}
