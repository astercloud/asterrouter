package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/gin-gonic/gin"
)

func TestPricingRuleRoutesOnlyAcceptEnterpriseUsageCostRules(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	router := gin.New()
	registerPricingRuleRoutes(router.Group("/costs"), service)

	valid := httptest.NewRequest(http.MethodPost, "/costs/pricing-rules", bytes.NewBufferString(`{"name":"usage cost","purpose":"usage_cost","scope_type":"global","scope_id":"","model":"*","currency":"USD","authoring_mode":"raw","expression":"v1: fixed_line(\"request\", \"request\", 1)","test_cases":[]}`))
	valid.Header.Set("Content-Type", "application/json")
	validRecorder := httptest.NewRecorder()
	router.ServeHTTP(validRecorder, valid)
	if validRecorder.Code != http.StatusOK {
		t.Fatalf("enterprise pricing create status=%d body=%s", validRecorder.Code, validRecorder.Body.String())
	}

	for _, body := range []string{
		`{"name":"legacy purpose","purpose":"customer_charge","scope_type":"global","scope_id":"","model":"*","currency":"USD","authoring_mode":"raw","expression":"v1: fixed_line(\"request\", \"request\", 1)","test_cases":[]}`,
		`{"name":"legacy scope","purpose":"usage_cost","scope_type":"operator_plan","scope_id":"plan-a","model":"*","currency":"USD","authoring_mode":"raw","expression":"v1: fixed_line(\"request\", \"request\", 1)","test_cases":[]}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/costs/pricing-rules", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("legacy pricing create status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}
