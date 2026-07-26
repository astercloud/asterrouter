package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/gin-gonic/gin"
)

func TestMetricsRequireExplicitTokenAndExposeBoundedHTTPLabels(t *testing.T) {
	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("synthetic frontend"), 0o600); err != nil {
		t.Fatal(err)
	}
	withoutMetrics := newTestHandler(t, RuntimeConfig{FrontendDir: frontendDir})
	disabledRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	disabledResponse := httptest.NewRecorder()
	withoutMetrics.ServeHTTP(disabledResponse, disabledRequest)
	if disabledResponse.Code != http.StatusNotFound {
		t.Fatalf("disabled metrics status=%d body=%s", disabledResponse.Code, disabledResponse.Body.String())
	}

	handler := newTestHandler(t, RuntimeConfig{MetricsToken: "metrics-secret"})
	for _, path := range []string{"/health", "/ready"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	customMethodRequest := httptest.NewRequest("SYNTHETIC-METHOD", "/health", nil)
	customMethodResponse := httptest.NewRecorder()
	handler.ServeHTTP(customMethodResponse, customMethodRequest)

	unauthorizedRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorizedRequest)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized metrics status=%d", unauthorizedResponse.Code)
	}

	metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRequest.Header.Set("Authorization", "Bearer metrics-secret")
	metricsResponse := httptest.NewRecorder()
	handler.ServeHTTP(metricsResponse, metricsRequest)
	body := metricsResponse.Body.String()
	for _, expected := range []string{
		`asterrouter_http_requests_total{method="GET",route="/health",status="200"} 1`,
		`asterrouter_http_requests_total{method="GET",route="/ready",status="200"} 1`,
		"asterrouter_http_requests_in_flight 0",
		"asterrouter_readiness_status 1",
		`asterrouter_readiness_checks_total{result="ready"} 1`,
		`asterrouter_http_requests_total{method="OTHER",route="unmatched",status="404"} 1`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, body)
		}
	}
	if strings.Contains(body, "metrics-secret") {
		t.Fatalf("metrics exposed authentication token: %s", body)
	}
	if strings.Contains(body, "SYNTHETIC-METHOD") {
		t.Fatalf("metrics exposed an unbounded HTTP method label: %s", body)
	}
}

func TestMetricsReportFailedReadinessWithoutDependencyDetails(t *testing.T) {
	const sensitiveMarker = "postgres://metrics-user:metrics-password@database.internal/asterrouter"
	repository := unhealthySettingsRepository{
		Repository: settings.NewMemoryRepository(),
		healthErr:  errors.New(sensitiveMarker),
	}
	handler := New(Options{
		Runtime: RuntimeConfig{MetricsToken: "metrics-secret"},
		SettingsService: settings.NewService(repository, settings.ServiceOptions{
			Version: "test", StorageMode: "postgres",
		}),
	})

	readyRequest := httptest.NewRequest(http.MethodGet, "/ready", nil).WithContext(context.Background())
	readyResponse := httptest.NewRecorder()
	handler.ServeHTTP(readyResponse, readyRequest)
	if readyResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready status=%d body=%s", readyResponse.Code, readyResponse.Body.String())
	}

	metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRequest.Header.Set("X-Metrics-Token", "metrics-secret")
	metricsResponse := httptest.NewRecorder()
	handler.ServeHTTP(metricsResponse, metricsRequest)
	body := metricsResponse.Body.String()
	if !strings.Contains(body, "asterrouter_readiness_status 0") || !strings.Contains(body, `asterrouter_readiness_checks_total{result="unavailable"} 1`) {
		t.Fatalf("failed readiness metrics are incomplete: %s", body)
	}
	if strings.Contains(body, sensitiveMarker) || strings.Contains(body, "database.internal") {
		t.Fatalf("metrics exposed dependency details: %s", body)
	}
}

func TestMetricsCountConcurrentRequests(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{MetricsToken: "metrics-secret"})
	var wait sync.WaitGroup
	for index := 0; index < 64; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			request := httptest.NewRequest(http.MethodGet, "/health", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Errorf("health status=%d body=%s", response.Code, response.Body.String())
			}
		}()
	}
	wait.Wait()

	metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRequest.Header.Set("Authorization", "Bearer metrics-secret")
	metricsResponse := httptest.NewRecorder()
	handler.ServeHTTP(metricsResponse, metricsRequest)
	if expected := `asterrouter_http_requests_total{method="GET",route="/health",status="200"} 64`; !strings.Contains(metricsResponse.Body.String(), expected) {
		t.Fatalf("concurrent request count is incomplete:\n%s", metricsResponse.Body.String())
	}
}

func TestMetricsRecordRecoveredPanicAsServerError(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	metrics := newServerMetrics()
	router := gin.New()
	router.Use(metrics.middleware())
	router.Use(gin.Recovery())
	router.GET("/panic", func(*gin.Context) { panic("synthetic panic") })

	request := httptest.NewRequest(http.MethodGet, "/panic", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("panic status=%d body=%s", response.Code, response.Body.String())
	}
	if expected := `asterrouter_http_requests_total{method="GET",route="/panic",status="500"} 1`; !strings.Contains(metrics.render(), expected) {
		t.Fatalf("panic metric did not record 500:\n%s", metrics.render())
	}
}

func TestMetricsExposeBoundedCapacityAdmissionsAndProviderSnapshots(t *testing.T) {
	metrics := newServerMetrics()
	metrics.ObserveCapacityAdmission(controlplane.CapacityAdmissionEvent{Scope: "tenant", Result: "rejected", Reason: "tenant_concurrency_exhausted"})
	metrics.ObserveCapacityAdmission(controlplane.CapacityAdmissionEvent{Scope: "provider_account", Result: "acquired"})
	metrics.ObserveCapacityAdmission(controlplane.CapacityAdmissionEvent{Scope: "sensitive-scope", Result: "sensitive-result", Reason: "sensitive-reason"})
	metrics.setProviderCapacitySnapshotSource(func(context.Context) ([]controlplane.ProviderCapacityMetricSnapshot, error) {
		return []controlplane.ProviderCapacityMetricSnapshot{{
			ProviderID: "provider-a", ProviderAccountID: "account-a", Schedulable: true, CircuitOpen: false,
			Current:          controlplane.ProviderCapacitySnapshot{CapacityUnits: 2, Requests: 8, Tokens: 90},
			ConcurrencyLimit: 3, RPMLimit: 10, TPMLimit: 100,
		}}, nil
	})
	body := metrics.render()
	for _, expected := range []string{
		`asterrouter_capacity_admissions_total{scope="tenant",result="rejected",reason="tenant_concurrency"} 1`,
		`asterrouter_capacity_admissions_total{scope="provider_account",result="acquired",reason="none"} 1`,
		`asterrouter_capacity_admissions_total{scope="other",result="other",reason="other"} 1`,
		"asterrouter_provider_capacity_snapshot_status 1",
		`asterrouter_provider_account_schedulable{provider="provider-a",provider_account="account-a"} 1`,
		`asterrouter_provider_capacity_current{provider="provider-a",provider_account="account-a",dimension="concurrency"} 2`,
		`asterrouter_provider_capacity_limit{provider="provider-a",provider_account="account-a",dimension="tpm"} 100`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("capacity metrics missing %q:\n%s", expected, body)
		}
	}
	for _, forbidden := range []string{"sensitive-scope", "sensitive-result", "sensitive-reason"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("capacity metrics exposed unbounded label %q: %s", forbidden, body)
		}
	}
}

func TestMetricsHideProviderSnapshotFailureDetails(t *testing.T) {
	const sensitiveMarker = "postgres://capacity-user:capacity-password@database.internal/metrics"
	metrics := newServerMetrics()
	metrics.setProviderCapacitySnapshotSource(func(context.Context) ([]controlplane.ProviderCapacityMetricSnapshot, error) {
		return nil, errors.New(sensitiveMarker)
	})
	body := metrics.render()
	if !strings.Contains(body, "asterrouter_provider_capacity_snapshot_status 0") {
		t.Fatalf("provider snapshot failure status missing: %s", body)
	}
	if strings.Contains(body, sensitiveMarker) || strings.Contains(body, "database.internal") {
		t.Fatalf("provider snapshot metrics exposed dependency details: %s", body)
	}
}
