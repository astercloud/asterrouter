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
