package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

var ErrProviderBillingObservationInvalid = errors.New("provider billing observation is invalid")

func (s *Service) recordAIOperationUsage(ctx context.Context, operation AIOperation, attempt AIAttempt, input GatewayUsageInput) error {
	auth, err := s.gatewayAuthForOperationUsage(ctx, operation)
	if err != nil {
		return err
	}
	input.OperationID = operation.ID
	input.AttemptID = attempt.ID
	input.RequestFingerprint = operation.RequestFingerprint
	input.Model = operation.Model
	input.UpstreamModel = attempt.UpstreamModel
	input.Protocol = operation.Protocol
	input.ProviderID = attempt.ProviderID
	input.ProviderAccountID = attempt.ProviderAccountID
	if input.UpstreamRequestID == "" {
		input.UpstreamRequestID = attempt.ProviderRequestID
	}
	if input.UsageVersion <= 0 {
		input.UsageVersion = 1
	}
	return s.RecordGatewayUsage(ctx, auth, input)
}

func (s *Service) RecordDirectAIProviderUsage(ctx context.Context, operation AIOperation, attempt AIAttempt, result ProviderDispatchResult, input GatewayUsageInput) error {
	if strings.TrimSpace(operation.ID) == "" || attempt.OperationID != operation.ID {
		return errors.New("direct ai usage attempt does not belong to the operation")
	}
	billing, err := normalizeProviderSuccessBilling(result.Billing, input.UsageDimensions)
	if err != nil {
		return err
	}
	if input.UsageSource == "" {
		input.UsageSource = "provider_final"
	}
	if input.UpstreamRequestID == "" {
		input.UpstreamRequestID = result.Task.ProviderRequestID
	}
	applyProviderBillingUsageFields(&input, billing)
	return s.recordAIOperationUsage(ctx, operation, attempt, input)
}

func (s *Service) FinalizeDirectAIProviderTerminalBilling(ctx context.Context, operation AIOperation, attempt AIAttempt, terminalStatus string, result ProviderDispatchResult) (bool, error) {
	return s.finalizeAIOperationTerminalBilling(ctx, operation, attempt, terminalStatus, result, 2)
}

func (s *Service) finalizeAIOperationTerminalBilling(ctx context.Context, operation AIOperation, attempt AIAttempt, terminalStatus string, result ProviderDispatchResult, usageVersion int) (bool, error) {
	billing, dimensions, err := normalizeProviderTerminalBilling(result.Billing, result.UsageDimensions)
	if err != nil {
		return false, err
	}
	if billing.Status == ProviderBillingStatusPending || billing.Status == ProviderBillingStatusUnknown {
		return false, nil
	}
	usageStatus := "upstream_error"
	errorType := "provider_failed"
	if terminalStatus == AIJobStatusCanceled {
		usageStatus = "canceled"
		errorType = "provider_canceled"
	}
	input := GatewayUsageInput{
		UsageVersion: usageVersion, UsageSource: "provider_final", Status: usageStatus, ErrorType: errorType,
		UsageDimensions: dimensions, UsageNormalizationStatus: "normalized_provider_terminal",
		UpstreamRequestID: result.Task.ProviderRequestID,
	}
	applyProviderBillingUsageFields(&input, billing)
	if err := s.recordAIOperationUsage(ctx, operation, attempt, input); err != nil {
		return false, err
	}
	return true, nil
}

func normalizeProviderSuccessBilling(billing ProviderBillingObservation, dimensions UsageDimensions) (ProviderBillingObservation, error) {
	if strings.ToLower(strings.TrimSpace(billing.Status)) == ProviderBillingStatusFinal {
		normalized, _, err := normalizeProviderTerminalBilling(billing, dimensions)
		return normalized, err
	}
	normalized, _, err := normalizeProviderTerminalBilling(billing, nil)
	return normalized, err
}

func applyProviderBillingUsageFields(input *GatewayUsageInput, billing ProviderBillingObservation) {
	input.SkipProcurementCostEstimate = true
	input.ProcurementCostMicros = billing.ProcurementCostMicros
	input.ProcurementCostCurrency = billing.Currency
	input.ProcurementCostSource = billing.Source
	input.ProcurementCostConfidence = billing.Confidence
	input.ProcurementPriceID = billing.PriceID
	input.ProviderBillingLineID = billing.ProviderBillingLineID
	if billing.Status == ProviderBillingStatusNotCharged {
		zero := int64(0)
		input.ProcurementCostMicros = &zero
		input.ProcurementCostSource = "provider_not_charged"
		input.ProcurementCostConfidence = ProcurementCostConfidenceExact
	}
}

func normalizeProviderTerminalBilling(billing ProviderBillingObservation, dimensions UsageDimensions) (ProviderBillingObservation, UsageDimensions, error) {
	billing.Status = strings.ToLower(strings.TrimSpace(billing.Status))
	if billing.Status == "" {
		billing.Status = ProviderBillingStatusUnknown
	}
	billing.Currency = strings.ToUpper(strings.TrimSpace(billing.Currency))
	billing.Source = strings.ToLower(strings.TrimSpace(billing.Source))
	billing.Confidence = strings.ToLower(strings.TrimSpace(billing.Confidence))
	billing.PriceID = strings.TrimSpace(billing.PriceID)
	billing.ProviderBillingLineID = strings.TrimSpace(billing.ProviderBillingLineID)
	normalizedDimensions, err := NormalizeUsageDimensions(dimensions)
	if err != nil {
		return ProviderBillingObservation{}, nil, err
	}
	if !oneOf(billing.Status, ProviderBillingStatusUnknown, ProviderBillingStatusPending, ProviderBillingStatusFinal, ProviderBillingStatusNotCharged) {
		return ProviderBillingObservation{}, nil, ErrProviderBillingObservationInvalid
	}
	if billing.ProcurementCostMicros != nil {
		if *billing.ProcurementCostMicros < 0 || len(billing.Currency) != 3 || !validUsageDimensionToken(billing.Source) ||
			!oneOf(billing.Confidence, ProcurementCostConfidenceExact, ProcurementCostConfidenceDerived, ProcurementCostConfidenceEstimated, ProcurementCostConfidenceUnallocated) {
			return ProviderBillingObservation{}, nil, ErrProviderBillingObservationInvalid
		}
	} else if billing.Currency != "" || billing.Source != "" || billing.Confidence != "" || billing.PriceID != "" || billing.ProviderBillingLineID != "" {
		return ProviderBillingObservation{}, nil, ErrProviderBillingObservationInvalid
	}
	if len(billing.PriceID) > 160 || len(billing.ProviderBillingLineID) > 160 {
		return ProviderBillingObservation{}, nil, ErrProviderBillingObservationInvalid
	}
	switch billing.Status {
	case ProviderBillingStatusUnknown, ProviderBillingStatusPending:
		if billing.ProcurementCostMicros != nil || len(normalizedDimensions) > 0 {
			return ProviderBillingObservation{}, nil, ErrProviderBillingObservationInvalid
		}
	case ProviderBillingStatusFinal:
		if billing.ProcurementCostMicros == nil && len(normalizedDimensions) == 0 {
			return ProviderBillingObservation{}, nil, ErrProviderBillingObservationInvalid
		}
	case ProviderBillingStatusNotCharged:
		if billing.ProcurementCostMicros != nil || len(normalizedDimensions) > 0 {
			return ProviderBillingObservation{}, nil, ErrProviderBillingObservationInvalid
		}
	}
	return billing, normalizedDimensions, nil
}

// Usage finalization must use the identity snapshot admitted with the
// operation. Current display names are best-effort enrichment only.
func (s *Service) gatewayAuthForOperationUsage(ctx context.Context, operation AIOperation) (GatewayAuthContext, error) {
	key := APIKeyRecord{
		ID: operation.CredentialID, Name: "Operation credential",
		Fingerprint:  prefix(hashAPIKey(operation.CredentialID), 12),
		ProfileScope: operation.ProfileScope, TenantID: operation.TenantID,
		PrincipalType: operation.PrincipalType, PrincipalReference: operation.PrincipalID,
	}
	if operation.CredentialSource == string(gatewaycore.CredentialSourceAPIKey) {
		keys, err := s.repo.ListAPIKeys(ctx)
		if err != nil {
			return GatewayAuthContext{}, err
		}
		for _, candidate := range keys {
			if candidate.ID == operation.CredentialID {
				key = candidate
				break
			}
		}
	} else if !oneOf(operation.CredentialSource, string(gatewaycore.CredentialSourceHMACContext), string(gatewaycore.CredentialSourceJWTJWKS)) {
		return GatewayAuthContext{}, fmt.Errorf("unsupported operation credential source %q", operation.CredentialSource)
	}

	key.ID = operation.CredentialID
	key.ProfileScope = operation.ProfileScope
	key.TenantID = operation.TenantID
	key.PrincipalType = operation.PrincipalType
	key.PrincipalReference = operation.PrincipalID
	if key.Fingerprint == "" {
		key.Fingerprint = prefix(hashAPIKey(operation.CredentialID), 12)
	}
	auth := GatewayAuthContext{APIKey: key, ExternalSubjectReference: operation.ExternalSubjectReference}
	if operation.ProfileScope == ProfileScopePlatform {
		tenant, principal, err := s.operationPlatformIdentity(ctx, operation)
		if err != nil {
			return GatewayAuthContext{}, err
		}
		auth.PlatformTenant = &tenant
		auth.GatewayPrincipal = &principal
		auth.APIKey.PlatformTenantID = tenant.ID
		auth.APIKey.GatewayPrincipalID = principal.ID
	}
	if strings.TrimSpace(operation.IntegrationID) != "" {
		integration, err := s.operationExternalAuthIntegration(ctx, operation.IntegrationID)
		if err != nil {
			return GatewayAuthContext{}, err
		}
		auth.ExternalAuthIntegration = &integration
	}
	return auth, nil
}

func (s *Service) operationPlatformIdentity(ctx context.Context, operation AIOperation) (PlatformTenant, GatewayPrincipal, error) {
	tenant := PlatformTenant{ID: operation.TenantID, Name: operation.TenantID}
	principal := GatewayPrincipal{ID: operation.PrincipalID, TenantID: operation.TenantID, Name: operation.PrincipalID, PrincipalType: operation.PrincipalType}
	tenants, err := s.repo.ListPlatformTenants(ctx)
	if err != nil {
		return PlatformTenant{}, GatewayPrincipal{}, err
	}
	for _, candidate := range tenants {
		if candidate.ID == operation.TenantID {
			tenant = candidate
			break
		}
	}
	principals, err := s.repo.ListGatewayPrincipals(ctx)
	if err != nil {
		return PlatformTenant{}, GatewayPrincipal{}, err
	}
	for _, candidate := range principals {
		if candidate.ID == operation.PrincipalID && candidate.TenantID == operation.TenantID {
			principal = candidate
			break
		}
	}
	return tenant, principal, nil
}

func (s *Service) operationExternalAuthIntegration(ctx context.Context, id string) (ExternalAuthIntegration, error) {
	integrations, err := s.repo.ListExternalAuthIntegrations(ctx)
	if err != nil {
		return ExternalAuthIntegration{}, err
	}
	for _, integration := range integrations {
		if integration.ID == id {
			return integration, nil
		}
	}
	return ExternalAuthIntegration{ID: id}, nil
}

func durableProviderUsageDimensions(job AIJob, result ProviderDispatchResult, artifacts []Artifact) (UsageDimensions, error) {
	observed := UsageDimensions{}
	var outputBytes int64
	finalCount := int64(0)
	for _, artifact := range artifacts {
		if artifact.Role != ArtifactRoleFinal || !durableArtifactDeliverable(job.ArtifactPolicy, artifact) {
			continue
		}
		finalCount++
		outputBytes = saturatingUsageAdd(outputBytes, artifact.SizeBytes)
	}
	if strings.EqualFold(job.Modality, GatewayModalityImage) {
		observed[UsageDimensionOutputImages] = UsageDimension{
			Quantity: finalCount, Unit: UsageUnitCount,
			Source: "core_artifact", Confidence: UsageConfidenceObserved,
		}
	}
	if outputBytes > 0 {
		observed[UsageDimensionOutputBytes] = UsageDimension{
			Quantity: outputBytes, Unit: UsageUnitByte,
			Source: "core_artifact", Confidence: UsageConfidenceObserved,
		}
	}
	return MergeUsageDimensions(result.UsageDimensions, observed)
}

func durableArtifactDeliverable(policy string, artifact Artifact) bool {
	if policy == GatewayArtifactPolicyCustomerSink {
		return artifact.Status == ArtifactStatusDelivered
	}
	return oneOf(artifact.Status, ArtifactStatusReady, ArtifactStatusDelivered)
}
