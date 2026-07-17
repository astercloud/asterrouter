package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/gin-gonic/gin"
)

func TestWriteGatewayErrorReturnsBillingHoldContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, err := range []error{controlplane.ErrBillingHoldBudgetExceeded, controlplane.ErrBillingHoldEstimateUnavailable} {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		writeGatewayError(context, err)
		if recorder.Code != http.StatusPaymentRequired || !strings.Contains(recorder.Body.String(), `"type":"budget_hold_failed"`) {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}

func TestGatewayProtocolErrorsUseClientEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		protocol gatewaycore.Protocol
		want     string
	}{
		{protocol: gatewaycore.ProtocolOpenAIChat, want: `"type":"rate_limit_error"`},
		{protocol: gatewaycore.ProtocolOpenAIResponses, want: `"type":"rate_limit_error"`},
		{protocol: gatewaycore.ProtocolAnthropicMessages, want: `"type":"rate_limit_error"`},
		{protocol: gatewaycore.ProtocolGeminiGenerate, want: `"status":"RESOURCE_EXHAUSTED"`},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		writeGatewayProtocolError(context, test.protocol, http.StatusTooManyRequests, "upstream_error", "limited")
		if recorder.Code != http.StatusTooManyRequests || !strings.Contains(recorder.Body.String(), test.want) {
			t.Fatalf("protocol=%s status=%d body=%s", test.protocol, recorder.Code, recorder.Body.String())
		}
	}
}

func TestGatewayProtocolStreamErrorsUseClientEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		protocol gatewaycore.Protocol
		want     []string
	}{
		{protocol: gatewaycore.ProtocolOpenAIChat, want: []string{"data: ", `"type":"upstream_error"`}},
		{protocol: gatewaycore.ProtocolOpenAIResponses, want: []string{"event: error", `"code":"upstream_error"`}},
		{protocol: gatewaycore.ProtocolAnthropicMessages, want: []string{"event: error", `"type":"api_error"`}},
		{protocol: gatewaycore.ProtocolGeminiGenerate, want: []string{"data: ", `"status":"INTERNAL"`}},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		if err := writeGatewayProtocolStreamError(context, test.protocol, "terminated"); err != nil {
			t.Fatal(err)
		}
		for _, want := range test.want {
			if !strings.Contains(recorder.Body.String(), want) {
				t.Fatalf("protocol=%s body=%s missing=%s", test.protocol, recorder.Body.String(), want)
			}
		}
	}
}

func TestWriteGatewayErrorReturnsMediaQuotaContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		err    error
		status int
		kind   string
	}{
		{err: controlplane.ErrBillingHoldImageQuotaExceeded, status: http.StatusTooManyRequests, kind: "image_quota_exceeded"},
		{err: controlplane.ErrBillingHoldVideoQuotaExceeded, status: http.StatusTooManyRequests, kind: "video_quota_exceeded"},
		{err: controlplane.ErrBillingHoldAudioQuotaExceeded, status: http.StatusTooManyRequests, kind: "audio_quota_exceeded"},
		{err: controlplane.ErrBillingHoldUsageEstimate, status: http.StatusBadRequest, kind: "usage_estimate_required"},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		writeGatewayError(context, test.err)
		if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), `"type":"`+test.kind+`"`) {
			t.Fatalf("error=%v status=%d body=%s", test.err, recorder.Code, recorder.Body.String())
		}
	}
}
