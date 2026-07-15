package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
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
