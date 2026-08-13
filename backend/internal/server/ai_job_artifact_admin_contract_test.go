package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

func assertConsoleError(t *testing.T, handler http.Handler, method, path string, headers map[string]string, wantStatus, wantCode int) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	record := httptest.NewRecorder()
	handler.ServeHTTP(record, request)
	var response struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(record.Body.Bytes(), &response); err != nil {
		t.Fatalf("%s %s decode error: %v; status=%d body=%s", method, path, err, record.Code, record.Body.String())
	}
	if record.Code != wantStatus || response.Code != wantCode {
		t.Fatalf("%s %s status=%d code=%d, want status=%d code=%d; body=%s", method, path, record.Code, response.Code, wantStatus, wantCode, record.Body.String())
	}
	return record
}

func TestAdminAIJobHTTPFailureAndIdempotencyContracts(t *testing.T) {
	handler, control := newTestRuntimeWithDurableAdmission(t, RuntimeConfig{AdminToken: "secret"}, testAIJobRuntime{})
	ctx := context.Background()
	headers := map[string]string{"Authorization": "Bearer secret"}
	model, err := control.CreateGatewayModel(ctx, "tester", controlplane.GatewayModelRequest{
		ModelID: "admin-job-contract-model", Name: "Admin job contract model", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	beginJob := func(marker string) controlplane.AIJob {
		t.Helper()
		job, _, beginErr := control.BeginDurableAIJob(ctx, gatewaycore.CanonicalAuthContext{
			CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: "admin-job-contract-key",
			ApplicationID: "admin-job-contract-application", PrincipalType: controlplane.APIKeyTypeService, PrincipalID: "admin-job-contract-principal",
			ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
		}, gatewaycore.CanonicalRequest{
			ID: "admin-job-contract-request-" + marker, Fingerprint: "admin-job-contract-fingerprint-" + marker,
			IdempotencyKey: "admin-job-contract-idempotency-" + marker, Protocol: gatewaycore.ProtocolAsterJobs,
			Operation: "image_generation", Modality: "image", Lane: gatewaycore.LaneDurable, Model: model.ModelID,
			Payload: []byte(`{"input":{"prompt":"synthetic"}}`),
		})
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		return job
	}

	assertConsoleError(t, handler, http.MethodGet, "/api/v1/console/ai-jobs/job_missing", headers, http.StatusNotFound, 1570)
	assertConsoleError(t, handler, http.MethodPost, "/api/v1/console/ai-jobs/job_missing/cancel", headers, http.StatusNotFound, 1570)
	assertConsoleError(t, handler, http.MethodGet, "/api/v1/console/ai-jobs?status=invalid", headers, http.StatusBadRequest, 1571)

	cancelable := beginJob("cancelable")
	summaryRequest := httptest.NewRequest(http.MethodGet, "/api/v1/console/ai-jobs/summary?status=queued", nil)
	summaryRequest.Header.Set("Authorization", "Bearer secret")
	summaryRecord := httptest.NewRecorder()
	handler.ServeHTTP(summaryRecord, summaryRequest)
	var summaryResponse struct {
		Code int                       `json:"code"`
		Data controlplane.AIJobSummary `json:"data"`
	}
	if err := json.Unmarshal(summaryRecord.Body.Bytes(), &summaryResponse); err != nil || summaryRecord.Code != http.StatusOK || summaryResponse.Code != 0 || summaryResponse.Data.Total != 1 || summaryResponse.Data.ByStatus[controlplane.AIJobStatusQueued] != 1 {
		t.Fatalf("summary status=%d response=%+v err=%v body=%s", summaryRecord.Code, summaryResponse, err, summaryRecord.Body.String())
	}
	cancelRequest := httptest.NewRequest(http.MethodPost, "/api/v1/console/ai-jobs/"+cancelable.ID+"/cancel", nil)
	cancelRequest.Header.Set("Authorization", "Bearer secret")
	firstCancel := httptest.NewRecorder()
	handler.ServeHTTP(firstCancel, cancelRequest)
	if firstCancel.Code != http.StatusOK {
		t.Fatalf("first cancel status=%d body=%s", firstCancel.Code, firstCancel.Body.String())
	}
	replayRequest := httptest.NewRequest(http.MethodPost, "/api/v1/console/ai-jobs/"+cancelable.ID+"/cancel", nil)
	replayRequest.Header.Set("Authorization", "Bearer secret")
	replay := httptest.NewRecorder()
	handler.ServeHTTP(replay, replayRequest)
	var replayResponse struct {
		Code int `json:"code"`
		Data struct {
			Status  string `json:"status"`
			Changed bool   `json:"changed"`
		} `json:"data"`
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &replayResponse); err != nil || replay.Code != http.StatusOK || replayResponse.Code != 0 || replayResponse.Data.Status != controlplane.AIJobStatusCanceled || replayResponse.Data.Changed {
		t.Fatalf("cancel replay status=%d response=%+v err=%v body=%s", replay.Code, replayResponse, err, replay.Body.String())
	}

	terminal := beginJob("terminal")
	claimed, err := control.ClaimReadyAIJobs(ctx, "admin-job-contract-worker", time.Minute, 1)
	if err != nil || len(claimed) != 1 || claimed[0].ID != terminal.ID {
		t.Fatalf("claim terminal job=%+v err=%v", claimed, err)
	}
	running, err := control.TransitionAIJob(ctx, terminal.ID, claimed[0].StatusVersion, claimed[0].FenceToken, controlplane.AIJobStatusRunning, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.TransitionAIJob(ctx, running.ID, running.StatusVersion, running.FenceToken, controlplane.AIJobStatusSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	assertConsoleError(t, handler, http.MethodPost, "/api/v1/console/ai-jobs/"+terminal.ID+"/cancel", headers, http.StatusConflict, 1572)

	job := beginJob("reconcile-owner")
	foreignJob := beginJob("reconcile-foreign")
	foreignAttempt, err := control.BeginAIAttempt(ctx, foreignJob.OperationID, 1, controlplane.GatewayProvider{
		ID: "provider-foreign", AccountID: "account-foreign", AdapterID: "adapter-foreign", RouteID: "route-foreign", UpstreamModel: "upstream-foreign",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertConsoleError(t, handler, http.MethodPost, fmt.Sprintf("/api/v1/console/ai-jobs/%s/attempts/attempt_missing/reconcile", job.ID), headers, http.StatusNotFound, 1570)
	assertConsoleError(t, handler, http.MethodPost, fmt.Sprintf("/api/v1/console/ai-jobs/%s/attempts/%s/reconcile", job.ID, foreignAttempt.ID), headers, http.StatusNotFound, 1570)
}

func TestAdminArtifactHTTPFailureAndRangeContracts(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	artifact := createAdminRouteArtifact(t, control)

	assertConsoleError(t, handler, http.MethodGet, "/api/v1/console/artifacts/artifact_missing", nil, http.StatusNotFound, 1560)
	assertConsoleError(t, handler, http.MethodGet, "/api/v1/console/artifacts/artifact_missing/content", nil, http.StatusNotFound, 1560)
	assertConsoleError(t, handler, http.MethodPost, "/api/v1/console/artifacts/artifact_missing/retry-delivery", nil, http.StatusNotFound, 1560)
	assertConsoleError(t, handler, http.MethodPost, "/api/v1/console/artifacts/"+artifact.ID+"/retry-delivery", nil, http.StatusConflict, 1562)
	assertConsoleError(t, handler, http.MethodGet, "/api/v1/console/artifacts?status=invalid", nil, http.StatusBadRequest, 1561)

	rangeRecord := assertConsoleError(t, handler, http.MethodGet, "/api/v1/console/artifacts/"+artifact.ID+"/content", map[string]string{"Range": "bytes=999-"}, http.StatusRequestedRangeNotSatisfiable, 1568)
	wantContentRange := fmt.Sprintf("bytes */%d", len(adminRouteArtifactPayload))
	if rangeRecord.Header().Get("Content-Range") != wantContentRange {
		t.Fatalf("invalid range Content-Range=%q, want %q", rangeRecord.Header().Get("Content-Range"), wantContentRange)
	}
}
