package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestGatewayNormalAndStreamingSoak(t *testing.T) {
	if os.Getenv("ASTER_GATEWAY_SOAK") != "1" {
		t.Skip("ASTER_GATEWAY_SOAK=1 is not set")
	}
	duration := 30 * time.Minute
	if value := os.Getenv("ASTER_GATEWAY_SOAK_DURATION"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed < time.Second {
			t.Fatalf("ASTER_GATEWAY_SOAK_DURATION must be at least 1s: %q", value)
		}
		duration = parsed
	}
	interval := 100 * time.Millisecond
	if value := os.Getenv("ASTER_GATEWAY_SOAK_INTERVAL"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed < time.Millisecond {
			t.Fatalf("ASTER_GATEWAY_SOAK_INTERVAL must be at least 1ms: %q", value)
		}
		interval = parsed
	}

	upstream := testutil.NewFakeOpenAI(t)
	handlers, control, key := gatewaySoakRuntimes(t, upstream, controlplane.APIKeyCreateRequest{
		ConcurrencyLimit:    1,
		MonthlyBudgetMicros: 1_000_000_000,
	})
	publishGatewayOutputTokenPricing(t, control, "Soak usage")
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	beforeGoroutines := runtime.NumGoroutine()

	started := time.Now()
	deadline := started.Add(duration)
	nextProgress := started.Add(5 * time.Minute)
	requests := 0
	operationIDs := make([]string, 0, int(duration/interval)+1)
	for time.Now().Before(deadline) {
		stream := requests%2 == 1
		if stream {
			upstream.SetMode(testutil.OpenAIStream)
		} else {
			upstream.SetMode(testutil.OpenAINormal)
		}
		body := `{"model":"public-model","messages":[{"role":"user","content":"synthetic soak"}]`
		if stream {
			body += `,"stream":true`
		}
		body += `}`
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		handlers[requests%len(handlers)].ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d stream=%t status=%d body=%s", requests, stream, rec.Code, rec.Body.String())
		}
		operationID := rec.Header().Get("X-AsterRouter-Operation-ID")
		if operationID == "" {
			t.Fatalf("request %d did not return an operation id", requests)
		}
		operationIDs = append(operationIDs, operationID)
		requests++
		if time.Now().After(nextProgress) {
			t.Logf("soak_progress elapsed=%s requests=%d", time.Since(started).Round(time.Second), requests)
			nextProgress = nextProgress.Add(5 * time.Minute)
		}
		time.Sleep(interval)
	}
	for _, operationID := range operationIDs {
		operation, found, err := control.AIOperation(context.Background(), operationID)
		if err != nil || !found || operation.Status != controlplane.AIOperationStatusSucceeded {
			t.Fatalf("operation %s state=%+v found=%t err=%v", operationID, operation, found, err)
		}
		attempts, err := control.AIAttemptsForOperation(context.Background(), operationID)
		if err != nil || len(attempts) != 1 || attempts[0].Status != controlplane.AIAttemptStatusSucceeded {
			t.Fatalf("operation %s attempts=%+v err=%v", operationID, attempts, err)
		}
		hold, found, err := control.BillingHoldForOperation(context.Background(), operationID)
		if err != nil || !found || hold.Status != controlplane.BillingHoldStatusSettled {
			t.Fatalf("operation %s billing hold=%+v found=%t err=%v", operationID, hold, found, err)
		}
	}

	const evidenceWindow = 500
	usage, err := control.UsageReport(context.Background(), evidenceWindow)
	if err != nil {
		t.Fatalf("UsageReport(): %v", err)
	}
	traceSummary, err := control.GatewayTraceSummaryQuery(context.Background(), controlplane.GatewayTraceQuery{})
	if err != nil {
		t.Fatalf("GatewayTraceSummaryQuery(): %v", err)
	}
	traces, err := control.ListGatewayTraces(context.Background(), evidenceWindow)
	if err != nil {
		t.Fatalf("ListGatewayTraces(): %v", err)
	}
	wantRecent := requests
	if wantRecent > evidenceWindow {
		wantRecent = evidenceWindow
	}
	if usage.TotalRequests != requests || traceSummary.Total != requests || len(usage.Recent) != wantRecent || len(traces) != wantRecent {
		t.Fatalf("evidence requests=%d usage_total=%d trace_total=%d usage_recent=%d traces_recent=%d want_recent=%d", requests, usage.TotalRequests, traceSummary.Total, len(usage.Recent), len(traces), wantRecent)
	}

	runtime.GC()
	time.Sleep(250 * time.Millisecond)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	afterGoroutines := runtime.NumGoroutine()
	goroutineDelta := afterGoroutines - beforeGoroutines
	if goroutineDelta > 16 {
		t.Fatalf("goroutine growth exceeds threshold: before=%d after=%d delta=%d", beforeGoroutines, afterGoroutines, goroutineDelta)
	}

	heapDelta := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("soak_duration=%s requests=%s interval=%s goroutines_before=%d goroutines_after=%d goroutine_delta=%d heap_alloc_delta_bytes=%d",
		time.Since(started).Round(time.Millisecond), strconv.Itoa(requests), interval, beforeGoroutines, afterGoroutines, goroutineDelta, heapDelta)
}

func gatewaySoakRuntimes(t *testing.T, upstream *testutil.FakeOpenAI, keyRequest controlplane.APIKeyCreateRequest) ([]http.Handler, *controlplane.Service, string) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("ASTER_TEST_DATABASE_URL")) == "" {
		handler, control, key := gatewayContractRuntimeWithKeyRequest(t, upstream, keyRequest)
		return []http.Handler{handler}, control, key
	}

	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	handlers := make([]http.Handler, 0, 2)
	services := make([]*controlplane.Service, 0, 2)
	for index := 0; index < 2; index++ {
		settingsRepository, err := settings.NewPostgresRepository(ctx, schema.URL)
		if err != nil {
			t.Fatalf("open settings repository %d: %v", index, err)
		}
		t.Cleanup(func() { _ = settingsRepository.Close() })
		controlRepository, err := controlplane.NewPostgresRepository(ctx, schema.URL)
		if err != nil {
			t.Fatalf("open control repository %d: %v", index, err)
		}
		t.Cleanup(func() { _ = controlRepository.Close() })
		control := controlplane.NewService(controlRepository, "/v1", "soak-shared-secret")
		if err := control.EnsureSeedData(ctx); err != nil {
			t.Fatalf("seed control repository %d: %v", index, err)
		}
		handlers = append(handlers, New(Options{
			SettingsService: settings.NewService(settingsRepository, settings.ServiceOptions{
				Version: "test", StorageMode: "postgres",
			}),
			ControlService: control,
			SystemService:  system.NewService(system.Config{Version: "test", BuildType: "source"}),
		}))
		services = append(services, control)
	}
	_, control, key := configureGatewayContractRuntime(t, handlers[0], services[0], upstream, keyRequest)
	return handlers, control, key
}
