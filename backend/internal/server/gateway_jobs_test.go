package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestGatewayDurableJobLifecycleAndIdempotency(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	if _, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "public-image-job", Name: "Public image job", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatalf("CreateGatewayModel(): %v", err)
	}
	createdKey, err := control.CreateAPIKey(context.Background(), "test", durableJobAPIKeyRequest("job owner"))
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	body := `{"model":"public-image-job","operation":"image_generation","modality":"image","input":{"prompt":"synthetic","count":1}}`

	first := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", createdKey.Key, "job-http-idem-1", body)
	if first.Code != http.StatusAccepted || first.Header().Get("Location") == "" || first.Header().Get("X-AsterRouter-Operation-ID") == "" {
		t.Fatalf("first status=%d headers=%v body=%s", first.Code, first.Header(), first.Body.String())
	}
	var accepted publicAIJobResponse
	if err := json.Unmarshal(first.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode accepted job: %v", err)
	}
	if accepted.ID == "" || accepted.Status != controlplane.AIJobStatusQueued || accepted.Capability.Modality != "image" || accepted.ArtifactPolicy != controlplane.GatewayArtifactPolicyTemporary {
		t.Fatalf("accepted job=%+v", accepted)
	}
	if strings.Contains(first.Body.String(), "synthetic") || strings.Contains(first.Body.String(), createdKey.Key) {
		t.Fatalf("job response leaked request or credential: %s", first.Body.String())
	}

	replayBody := `{
  "input": {"count": 1, "prompt": "synthetic"},
  "modality": "image",
  "operation": "image_generation",
  "model": "public-image-job"
}`
	replay := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", createdKey.Key, "job-http-idem-1", replayBody)
	var replayed publicAIJobResponse
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotent-Replayed") != "true" || json.Unmarshal(replay.Body.Bytes(), &replayed) != nil || replayed.ID != accepted.ID {
		t.Fatalf("replay status=%d headers=%v body=%s", replay.Code, replay.Header(), replay.Body.String())
	}
	conflict := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", createdKey.Key, "job-http-idem-1", strings.Replace(body, "synthetic", "different", 1))
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "idempotency_conflict") {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	rotated, err := control.RotateAPIKey(context.Background(), "test", createdKey.Record.ID)
	if err != nil {
		t.Fatalf("RotateAPIKey(): %v", err)
	}
	retiredKeyGet := performGatewayJobRequest(handler, http.MethodGet, "/v1/jobs/"+accepted.ID, createdKey.Key, "", "")
	if retiredKeyGet.Code != http.StatusUnauthorized {
		t.Fatalf("retired key get status=%d body=%s", retiredKeyGet.Code, retiredKeyGet.Body.String())
	}
	get := performGatewayJobRequest(handler, http.MethodGet, "/v1/jobs/"+accepted.ID, rotated.Key, "", "")
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), accepted.ID) {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}
	cancel := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs/"+accepted.ID+"/cancel", rotated.Key, "", "")
	if cancel.Code != http.StatusOK || !strings.Contains(cancel.Body.String(), `"status":"canceled"`) {
		t.Fatalf("cancel status=%d body=%s", cancel.Code, cancel.Body.String())
	}
	cancelReplay := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs/"+accepted.ID+"/cancel", rotated.Key, "", "")
	if cancelReplay.Code != http.StatusOK || !strings.Contains(cancelReplay.Body.String(), `"status_version":2`) {
		t.Fatalf("cancel replay status=%d body=%s", cancelReplay.Code, cancelReplay.Body.String())
	}
}

func TestGatewayDurableJobQueueBackpressure(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	if _, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "limited-image-job", Name: "Limited image job", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	if err := control.SetAIJobAdmissionLimits(controlplane.AIJobAdmissionLimits{Principal: 1}); err != nil {
		t.Fatal(err)
	}
	request := durableJobAPIKeyRequest("limited job owner")
	request.ModelAllowlist = []string{"limited-image-job"}
	owner, err := control.CreateAPIKey(context.Background(), "test", request)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"limited-image-job","operation":"image_generation","modality":"image","input":{"prompt":"synthetic"}}`
	first := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", owner.Key, "limited-job-first", body)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	second := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", owner.Key, "limited-job-second", body)
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" || !strings.Contains(second.Body.String(), "queue_capacity_exceeded") {
		t.Fatalf("second status=%d headers=%v body=%s", second.Code, second.Header(), second.Body.String())
	}
}

func TestGatewayDurableJobAuthorizationAndNonDisclosure(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	if _, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "isolated-image-job", Name: "Isolated image job", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	ownerRequest := durableJobAPIKeyRequest("owner")
	ownerRequest.ModelAllowlist = []string{"isolated-image-job"}
	owner, err := control.CreateAPIKey(context.Background(), "test", ownerRequest)
	if err != nil {
		t.Fatal(err)
	}
	otherRequest := ownerRequest
	otherRequest.Name = "other principal"
	other, err := control.CreateAPIKey(context.Background(), "test", otherRequest)
	if err != nil {
		t.Fatal(err)
	}
	noReadRequest := ownerRequest
	noReadRequest.Name = "no read scope"
	noReadRequest.Scopes = []string{controlplane.GatewayScopeInvoke, controlplane.GatewayScopeJobsCancel}
	noRead, err := control.CreateAPIKey(context.Background(), "test", noReadRequest)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"isolated-image-job","operation":"image_generation","modality":"image","input":{"prompt":"synthetic"}}`
	created := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", owner.Key, "isolated-job-idem", body)
	var job publicAIJobResponse
	if created.Code != http.StatusAccepted || json.Unmarshal(created.Body.Bytes(), &job) != nil {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}

	for _, test := range []struct {
		name     string
		key      string
		code     int
		typeName string
	}{
		{name: "other principal", key: other.Key, code: http.StatusNotFound, typeName: "resource_not_found"},
		{name: "missing read scope", key: noRead.Key, code: http.StatusForbidden, typeName: "policy_not_allowed"},
		{name: "missing credential", key: "", code: http.StatusUnauthorized, typeName: "invalid_api_key"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := performGatewayJobRequest(handler, http.MethodGet, "/v1/jobs/"+job.ID, test.key, "", "")
			if response.Code != test.code || !strings.Contains(response.Body.String(), test.typeName) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}

	missingIdempotency := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", owner.Key, "", body)
	if missingIdempotency.Code != http.StatusBadRequest || !strings.Contains(missingIdempotency.Body.String(), "idempotency_key_required") {
		t.Fatalf("missing idempotency status=%d body=%s", missingIdempotency.Code, missingIdempotency.Body.String())
	}
	wrongModality := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", owner.Key, "wrong-modality-idem", strings.Replace(body, `"modality":"image"`, `"modality":"video"`, 1))
	if wrongModality.Code != http.StatusForbidden || !strings.Contains(wrongModality.Body.String(), "policy_not_allowed") {
		t.Fatalf("wrong modality status=%d body=%s", wrongModality.Code, wrongModality.Body.String())
	}
}

func durableJobAPIKeyRequest(name string) controlplane.APIKeyCreateRequest {
	return controlplane.APIKeyCreateRequest{
		Name: name, ModelAllowlist: []string{"public-image-job"},
		Scopes:            []string{controlplane.GatewayScopeInvoke, controlplane.GatewayScopeJobsRead, controlplane.GatewayScopeJobsCancel},
		AllowedModalities: []string{"image"}, AllowedOperations: []string{"image_generation"},
		LanePolicy: controlplane.GatewayLanePolicyDurableOnly, ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	}
}

func performGatewayJobRequest(handler http.Handler, method, target, key, idempotencyKey, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
