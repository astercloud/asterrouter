package gatewaycore

import "testing"

func TestNormalizeUsagePreservesCacheFieldPresenceAcrossSchemas(t *testing.T) {
	tests := []struct {
		name            string
		body            string
		wantInput       int
		wantOutput      int
		wantRead        *int
		wantWrite5m     *int
		wantCacheFields bool
		wantStatus      string
	}{
		{
			name:      "openai cache hit",
			body:      `{"usage":{"prompt_tokens":100,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":80}}}`,
			wantInput: 100, wantOutput: 5, wantRead: intTestPointer(80), wantCacheFields: true, wantStatus: UsageNormalizationOpenAI,
		},
		{
			name:      "openai explicit zero",
			body:      `{"usage":{"prompt_tokens":100,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":0}}}`,
			wantInput: 100, wantOutput: 5, wantRead: intTestPointer(0), wantCacheFields: true, wantStatus: UsageNormalizationOpenAI,
		},
		{
			name:      "openai missing cache field",
			body:      `{"usage":{"prompt_tokens":100,"completion_tokens":5}}`,
			wantInput: 100, wantOutput: 5, wantCacheFields: false, wantStatus: UsageNormalizationOpenAI,
		},
		{
			name:      "anthropic cache write",
			body:      `{"usage":{"input_tokens":20,"output_tokens":3,"cache_creation_input_tokens":80,"cache_read_input_tokens":0}}`,
			wantInput: 100, wantOutput: 3, wantRead: intTestPointer(0), wantWrite5m: intTestPointer(80), wantCacheFields: true, wantStatus: UsageNormalizationAnthropic,
		},
		{
			name:      "gemini cached prompt and thoughts",
			body:      `{"usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":60,"candidatesTokenCount":20,"thoughtsTokenCount":5,"totalTokenCount":125}}`,
			wantInput: 100, wantOutput: 25, wantRead: intTestPointer(60), wantCacheFields: true, wantStatus: UsageNormalizationGemini,
		},
		{
			name:      "gemini cache details fallback",
			body:      `{"usageMetadata":{"promptTokenCount":100,"cacheTokensDetails":[{"modality":"TEXT","tokenCount":40},{"modality":"IMAGE","tokenCount":20}],"candidatesTokenCount":4}}`,
			wantInput: 100, wantOutput: 4, wantRead: intTestPointer(60), wantCacheFields: true, wantStatus: UsageNormalizationGemini,
		},
		{
			name:      "gemini explicit zero cache",
			body:      `{"usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":0,"candidatesTokenCount":4}}`,
			wantInput: 100, wantOutput: 4, wantRead: intTestPointer(0), wantCacheFields: true, wantStatus: UsageNormalizationGemini,
		},
		{
			name:      "gemini malformed cache details",
			body:      `{"usageMetadata":{"promptTokenCount":100,"cacheTokensDetails":[{"modality":"TEXT","tokenCount":"60"}],"candidatesTokenCount":4}}`,
			wantInput: 100, wantOutput: 4, wantCacheFields: false, wantStatus: UsageNormalizationGemini,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NormalizeUsage([]byte(test.body))
			if got.InputTokens != test.wantInput || got.OutputTokens != test.wantOutput || got.CacheFieldsPresent != test.wantCacheFields || got.UsageNormalizationStatus != test.wantStatus {
				t.Fatalf("NormalizeUsage() = %+v", got)
			}
			assertOptionalInt(t, "cache read", got.CacheReadTokens, test.wantRead)
			assertOptionalInt(t, "cache write 5m", got.CacheWrite5mTokens, test.wantWrite5m)
		})
	}
}

func TestMergeNormalizedUsageKeepsLastPresentValues(t *testing.T) {
	first := NormalizeUsage([]byte(`{"usage":{"input_tokens":20,"output_tokens":1,"cache_creation_input_tokens":80,"cache_read_input_tokens":0}}`))
	second := NormalizeUsage([]byte(`{"message":{"usage":{"input_tokens":20,"output_tokens":2,"cache_creation_input_tokens":0,"cache_read_input_tokens":80}}}`))
	got := MergeNormalizedUsage(first, second)
	if got.InputTokens != 100 || got.OutputTokens != 2 || got.CacheReadTokens == nil || *got.CacheReadTokens != 80 || got.CacheWrite5mTokens == nil || *got.CacheWrite5mTokens != 0 || got.UsageNormalizationStatus != UsageNormalizationAnthropic {
		t.Fatalf("MergeNormalizedUsage() = %+v", got)
	}
}

func TestNormalizeUsageReportsMissingAndInvalid(t *testing.T) {
	if got := NormalizeUsage([]byte(`{"id":"no-usage"}`)); got.UsageNormalizationStatus != UsageNormalizationMissing {
		t.Fatalf("missing usage status = %q", got.UsageNormalizationStatus)
	}
	if got := NormalizeUsage([]byte(`{"usage":`)); got.UsageNormalizationStatus != UsageNormalizationInvalid {
		t.Fatalf("invalid usage status = %q", got.UsageNormalizationStatus)
	}
}

func intTestPointer(value int) *int {
	return &value
}

func assertOptionalInt(t *testing.T, name string, got, want *int) {
	t.Helper()
	if got == nil || want == nil {
		if got != want {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
		return
	}
	if *got != *want {
		t.Fatalf("%s = %d, want %d", name, *got, *want)
	}
}
