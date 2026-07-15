package controlplane

import "testing"

func TestNormalizeProviderTerminalBillingValidation(t *testing.T) {
	amount := int64(17)
	validDimensions := UsageDimensions{UsageDimensionOutputImages: {
		Quantity: 1, Unit: UsageUnitCount, Source: "provider", Confidence: UsageConfidenceReported,
	}}
	tests := []struct {
		name       string
		billing    ProviderBillingObservation
		dimensions UsageDimensions
		wantError  bool
	}{
		{
			name:       "final with dimensions",
			billing:    ProviderBillingObservation{Status: ProviderBillingStatusFinal},
			dimensions: validDimensions,
		},
		{
			name: "final with procurement cost",
			billing: ProviderBillingObservation{
				Status: ProviderBillingStatusFinal, ProcurementCostMicros: &amount, Currency: "USD",
				Source: "provider_invoice", Confidence: ProcurementCostConfidenceExact,
			},
		},
		{name: "not charged", billing: ProviderBillingObservation{Status: ProviderBillingStatusNotCharged}},
		{name: "pending", billing: ProviderBillingObservation{Status: ProviderBillingStatusPending}},
		{name: "unknown", billing: ProviderBillingObservation{Status: ProviderBillingStatusUnknown}},
		{name: "invalid status", billing: ProviderBillingObservation{Status: "settled"}, wantError: true},
		{name: "negative procurement cost", billing: ProviderBillingObservation{Status: ProviderBillingStatusFinal, ProcurementCostMicros: int64Pointer(-1), Currency: "USD", Source: "provider", Confidence: ProcurementCostConfidenceExact}, wantError: true},
		{name: "missing currency", billing: ProviderBillingObservation{Status: ProviderBillingStatusFinal, ProcurementCostMicros: &amount, Source: "provider", Confidence: ProcurementCostConfidenceExact}, wantError: true},
		{name: "invalid source", billing: ProviderBillingObservation{Status: ProviderBillingStatusFinal, ProcurementCostMicros: &amount, Currency: "USD", Source: "Provider Invoice", Confidence: ProcurementCostConfidenceExact}, wantError: true},
		{name: "invalid confidence", billing: ProviderBillingObservation{Status: ProviderBillingStatusFinal, ProcurementCostMicros: &amount, Currency: "USD", Source: "provider", Confidence: "guess"}, wantError: true},
		{name: "final without evidence", billing: ProviderBillingObservation{Status: ProviderBillingStatusFinal}, wantError: true},
		{name: "pending with dimensions", billing: ProviderBillingObservation{Status: ProviderBillingStatusPending}, dimensions: validDimensions, wantError: true},
		{name: "not charged with dimensions", billing: ProviderBillingObservation{Status: ProviderBillingStatusNotCharged}, dimensions: validDimensions, wantError: true},
		{name: "metadata without cost", billing: ProviderBillingObservation{Status: ProviderBillingStatusFinal, Source: "provider"}, dimensions: validDimensions, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := normalizeProviderTerminalBilling(test.billing, test.dimensions)
			if (err != nil) != test.wantError {
				t.Fatalf("normalize error=%v wantError=%t", err, test.wantError)
			}
		})
	}
}

func TestNormalizeProviderSuccessBillingAllowsPendingProcurement(t *testing.T) {
	dimensions := UsageDimensions{UsageDimensionOutputImages: {
		Quantity: 1, Unit: UsageUnitCount, Source: "core_artifact", Confidence: UsageConfidenceObserved,
	}}
	if _, err := normalizeProviderSuccessBilling(ProviderBillingObservation{Status: ProviderBillingStatusPending}, dimensions); err != nil {
		t.Fatalf("pending success billing error=%v", err)
	}
	amount := int64(23)
	final, err := normalizeProviderSuccessBilling(ProviderBillingObservation{
		Status: ProviderBillingStatusFinal, ProcurementCostMicros: &amount, Currency: "usd",
		Source: "provider_invoice", Confidence: ProcurementCostConfidenceExact,
	}, dimensions)
	if err != nil || final.Currency != "USD" || final.ProcurementCostMicros == nil || *final.ProcurementCostMicros != amount {
		t.Fatalf("final success billing=%+v err=%v", final, err)
	}
}

func TestApplyProviderBillingUsageFieldsDisablesEstimates(t *testing.T) {
	input := GatewayUsageInput{}
	applyProviderBillingUsageFields(&input, ProviderBillingObservation{Status: ProviderBillingStatusUnknown})
	if !input.SkipProcurementCostEstimate || input.ProcurementCostMicros != nil {
		t.Fatalf("unknown billing fields=%+v", input)
	}
	applyProviderBillingUsageFields(&input, ProviderBillingObservation{Status: ProviderBillingStatusNotCharged})
	if input.ProcurementCostMicros == nil || *input.ProcurementCostMicros != 0 || input.ProcurementCostSource != "provider_not_charged" || input.ProcurementCostConfidence != ProcurementCostConfidenceExact {
		t.Fatalf("not-charged billing fields=%+v", input)
	}
}

func TestRecordDirectAIProviderUsageRejectsMismatchedAttempt(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	err := svc.RecordDirectAIProviderUsage(t.Context(), AIOperation{ID: "operation-a"}, AIAttempt{
		ID: "attempt-a", OperationID: "operation-b",
	}, ProviderDispatchResult{}, GatewayUsageInput{})
	if err == nil {
		t.Fatal("mismatched direct attempt was accepted")
	}
}

func int64Pointer(value int64) *int64 {
	return &value
}
