package plugins

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

func TestBuiltinOpenAIImageAdapterRunsDurableJobToArtifact(t *testing.T) {
	imageBytes := []byte("synthetic-image-bytes")
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		upstreamCalls.Add(1)
		if request.URL.Path != "/v1/images/generations" || request.Method != http.MethodPost ||
			request.Header.Get("Authorization") != "Bearer provider-secret" || request.Header.Get("Idempotency-Key") == "" {
			http.Error(response, "invalid request", http.StatusBadRequest)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || payload["model"] != "image-upstream" || payload["prompt"] != "synthetic" || payload["n"] != float64(1) {
			http.Error(response, "invalid payload", http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("X-Request-ID", "provider-request-1")
		_ = json.NewEncoder(response).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(imageBytes)}}})
	}))
	defer upstream.Close()

	pluginService := NewServiceWithOptions(NewMemoryRepository(), ServiceOptions{ProviderAdapterHTTPClient: upstream.Client()})
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		t.Fatal(err)
	}
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1", "openai-image-runtime-secret")
	if err := controlService.SetArtifactStore(controlplane.NewMemoryArtifactStore()); err != nil {
		t.Fatal(err)
	}
	provider, err := controlService.CreateProvider(context.Background(), "test", controlplane.ProviderRequest{
		Name: "Image provider", Type: "openai_compatible", BaseURL: upstream.URL + "/v1",
		Status: controlplane.ProviderStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := controlService.CreateProviderAccount(context.Background(), "test", controlplane.ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Image account", Platform: "openai_compatible", AuthType: "api_key",
		Status: controlplane.AccountStatusActive, Schedulable: boolPointer(true), Models: []string{"image-upstream"}, Secret: "provider-secret", Concurrency: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := controlService.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "public-image", Name: "Public image", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlService.CreateModelRoute(context.Background(), "test", controlplane.ModelRouteRequest{
		GatewayModelID: model.ID, RouteGroup: controlplane.DefaultModelRouteGroup, ProviderAccountID: account.ID,
		UpstreamModel: "image-upstream", Priority: 1, Weight: 100, Status: controlplane.ModelRouteStatusActive, UpstreamFormat: controlplane.UpstreamFormatNativeMedia,
	}); err != nil {
		t.Fatal(err)
	}
	queue, err := controlplane.NewMemoryAIJobDeliveryQueue(200 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlplane.NewDurableAIJobRuntime(controlService, queue, pluginService, controlplane.DurableAIJobRuntimeConfig{
		WorkerID: "openai-image-test", LeaseDuration: 200 * time.Millisecond, SchedulerInterval: 5 * time.Millisecond,
		DeliveryWait: 5 * time.Millisecond, ReconcileInterval: 10 * time.Millisecond, RebuildInterval: 50 * time.Millisecond, BatchSize: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	runtimeCtx, cancelRuntime := context.WithCancel(context.Background())
	runtimeDone := make(chan error, 1)
	go func() { runtimeDone <- runtime.Run(runtimeCtx, func(string, error) {}) }()
	t.Cleanup(cancelRuntime)

	auth := gatewaycore.CanonicalAuthContext{
		CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: "image-key", ProfileScope: controlplane.ProfileScopePlatform,
		TenantID: "image-tenant", PrincipalType: controlplane.APIKeyTypeService, PrincipalID: "image-principal",
		ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	}
	payload := []byte(`{"model":"public-image","operation":"image_generation","modality":"image","input":{"prompt":"synthetic","count":1}}`)
	request := gatewaycore.CanonicalRequest{
		ID: "image-request", ClientRequestID: "image-client-request", Fingerprint: "image-fingerprint", IdempotencyKey: "image-idempotency",
		Protocol: gatewaycore.ProtocolAsterJobs, Lane: gatewaycore.LaneDurable, Model: "public-image", Modality: "image", Operation: "image_generation", Payload: payload,
	}
	waitForPluginCondition(t, time.Second, func() bool {
		supported, supportErr := runtime.SupportsDurableAIJob(context.Background(), auth, request)
		return supportErr == nil && supported
	})
	job, created, err := controlService.BeginDurableAIJob(context.Background(), auth, request)
	if err != nil || !created {
		t.Fatalf("BeginDurableAIJob() job=%+v created=%t err=%v", job, created, err)
	}
	waitForPluginCondition(t, 2*time.Second, func() bool {
		current, found, findErr := controlService.AIJobForAuth(context.Background(), auth, job.ID)
		return findErr == nil && found && current.Status == controlplane.AIJobStatusSucceeded
	})
	artifacts, found, err := controlService.ArtifactsForJobAndAuth(context.Background(), auth, job.ID)
	if err != nil || !found || len(artifacts) != 1 || artifacts[0].Status != controlplane.ArtifactStatusReady || artifacts[0].MediaType != "image/png" {
		t.Fatalf("artifacts=%+v found=%t err=%v", artifacts, found, err)
	}
	_, opened, found, err := controlService.OpenArtifactForAuth(context.Background(), auth, artifacts[0].ID, nil)
	if err != nil || !found {
		t.Fatalf("OpenArtifactForAuth() found=%t err=%v", found, err)
	}
	storedBytes, readErr := io.ReadAll(opened.Body)
	_ = opened.Body.Close()
	if readErr != nil || string(storedBytes) != string(imageBytes) {
		t.Fatalf("stored artifact=%q err=%v", storedBytes, readErr)
	}
	if calls := upstreamCalls.Load(); calls != 1 {
		t.Fatalf("upstream calls=%d, want exactly one", calls)
	}
	usage, err := controlService.UsageReport(context.Background(), 10)
	if err != nil || len(usage.Recent) != 1 || usage.TotalOutputImages != 1 || usage.Recent[0].UsageDimensions[controlplane.UsageDimensionOutputImages].Quantity != 1 {
		t.Fatalf("durable image usage=%+v err=%v", usage, err)
	}
	hold, found, err := controlService.BillingHoldForOperation(context.Background(), job.OperationID)
	if err != nil || !found || hold.Status != controlplane.BillingHoldStatusSettled {
		t.Fatalf("durable image billing hold=%+v found=%t err=%v", hold, found, err)
	}

	cancelRuntime()
	select {
	case err := <-runtimeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("durable runtime did not stop")
	}
	if job.RequestPayload != "" || job.RequestPayloadCiphertext == "" || strings.Contains(job.RequestPayloadCiphertext, "synthetic") {
		t.Fatalf("public job leaked request payload: %+v", job)
	}
}

func TestBuiltinOpenAIImageAdapterDownloadsURLOutputs(t *testing.T) {
	imageBytes := []byte("synthetic-url-image")
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/output.png" {
			response.Header().Set("Content-Type", "image/png")
			_, _ = response.Write(imageBytes)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"data": []map[string]string{{"url": "http://" + request.Host + "/output.png"}}})
	}))
	defer upstream.Close()

	service := NewServiceWithOptions(NewMemoryRepository(), ServiceOptions{ProviderAdapterHTTPClient: upstream.Client()})
	provider := controlplane.GatewayProvider{Type: "openai_compatible", BaseURL: upstream.URL + "/v1", APIKey: "provider-secret", AccountID: "account", UpstreamModel: "image"}
	result, err := service.dispatchBuiltinOpenAIImage(context.Background(), provider, controlplane.AIJob{Modality: "image", Operation: "image_generation", ArtifactPolicy: controlplane.GatewayArtifactPolicyManaged}, controlplane.AIAttempt{}, controlplane.ProviderDispatchCommand{
		Intent: controlplane.ProviderDispatchIntent{DispatchKey: "dispatch-url"}, Payload: []byte(`{"input":{"prompt":"synthetic"}}`),
	})
	if err != nil || result.Outcome != controlplane.ProviderDispatchOutcomeAccepted || len(result.Outputs) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	opened, openErr := service.openBuiltinOpenAIImageOutput(result.Task.ProviderTaskID, result.Outputs[0])
	if openErr != nil {
		t.Fatalf("open cached URL output: %v", openErr)
	}
	defer opened.Close()
	stored, readErr := io.ReadAll(opened)
	if readErr != nil || string(stored) != string(imageBytes) {
		t.Fatalf("stored=%q err=%v", stored, readErr)
	}
}

func TestBuiltinOpenAIImageAdapterClassifiesFailuresWithoutLeakingBodies(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      int
		wantOutcome string
	}{
		{name: "invalid request", status: http.StatusBadRequest, wantOutcome: controlplane.ProviderDispatchOutcomeProvenNotCreated},
		{name: "rate limited", status: http.StatusTooManyRequests, wantOutcome: controlplane.ProviderDispatchOutcomeUnknown},
		{name: "server failure", status: http.StatusInternalServerError, wantOutcome: controlplane.ProviderDispatchOutcomeUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				http.Error(response, "provider-secret and synthetic prompt", test.status)
			}))
			defer upstream.Close()
			service := NewServiceWithOptions(NewMemoryRepository(), ServiceOptions{ProviderAdapterHTTPClient: upstream.Client()})
			provider := controlplane.GatewayProvider{BaseURL: upstream.URL + "/v1", APIKey: "provider-secret", AccountID: "account", UpstreamModel: "image"}
			result, err := service.dispatchBuiltinOpenAIImage(context.Background(), provider, controlplane.AIJob{}, controlplane.AIAttempt{}, controlplane.ProviderDispatchCommand{
				Intent: controlplane.ProviderDispatchIntent{DispatchKey: "dispatch"}, Payload: []byte(`{"input":{"prompt":"synthetic"}}`),
			})
			if err == nil || result.Outcome != test.wantOutcome || strings.Contains(err.Error(), "provider-secret") || strings.Contains(err.Error(), "synthetic prompt") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestBuiltinOpenAIImageAdapterRejectsArtifactPoliciesItCannotMaterialize(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if err := service.EnsureSeedData(context.Background()); err != nil {
		t.Fatal(err)
	}
	provider := controlplane.GatewayProvider{Type: "openai_compatible"}
	for _, policy := range []string{controlplane.GatewayArtifactPolicyMetadataOnly, controlplane.GatewayArtifactPolicyProxyOnly} {
		selected, supported, err := service.SelectDurableAIJobAdapter(context.Background(), provider, controlplane.AIJob{
			Modality: "image", Operation: "image_generation", ArtifactPolicy: policy,
		})
		if err != nil || supported || selected != "" {
			t.Fatalf("policy=%s selected=%q supported=%t err=%v", policy, selected, supported, err)
		}
	}
}

func TestBuiltinOpenAIImageAdapterSelectsDirectFinalOnlyContract(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if err := service.EnsureSeedData(context.Background()); err != nil {
		t.Fatal(err)
	}
	provider := controlplane.GatewayProvider{Type: "openai_compatible"}
	request := gatewaycore.CanonicalRequest{
		Protocol: gatewaycore.ProtocolOpenAIImages, Operation: controlplane.GatewayOperationImageGeneration,
		Modality: controlplane.GatewayModalityImage, Lane: gatewaycore.LaneDirect, PreviewMode: "preferred",
	}
	selected, supported, err := service.SelectDirectAIAdapter(context.Background(), provider, request, controlplane.GatewayArtifactPolicyTemporary)
	if err != nil || !supported || selected != OpenAICompatibleProviderPluginID {
		t.Fatalf("selected=%q supported=%t err=%v", selected, supported, err)
	}
	request.PreviewMode = "required"
	selected, supported, err = service.SelectDirectAIAdapter(context.Background(), provider, request, controlplane.GatewayArtifactPolicyTemporary)
	if err != nil || supported || selected != "" {
		t.Fatalf("required preview selected=%q supported=%t err=%v", selected, supported, err)
	}
}

func boolPointer(value bool) *bool { return &value }

func waitForPluginCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not satisfied before timeout")
}
