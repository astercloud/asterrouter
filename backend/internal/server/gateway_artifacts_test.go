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

func TestGatewayArtifactAuthorizationRangeAndDeletion(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	if _, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "artifact-image-model", Name: "Artifact image model", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	request := durableJobAPIKeyRequest("artifact owner")
	request.ModelAllowlist = []string{"artifact-image-model"}
	request.Scopes = append(request.Scopes, controlplane.GatewayScopeArtifactsRead, controlplane.GatewayScopeArtifactsDelete)
	owner, err := control.CreateAPIKey(context.Background(), "test", request)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"artifact-image-model","operation":"image_generation","modality":"image","input":{"prompt":"synthetic"}}`
	created := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", owner.Key, "artifact-http-idem", body)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create job status=%d body=%s", created.Code, created.Body.String())
	}
	var job publicAIJobResponse
	if err := json.Unmarshal(created.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	store := controlplane.NewMemoryArtifactStore()
	if err := control.SetArtifactStore(store); err != nil {
		t.Fatal(err)
	}
	payload := []byte("public-synthetic-image")
	artifact, err := control.CreateArtifactFromReader(context.Background(), controlplane.ArtifactCreateInput{
		OperationID: job.OperationID, JobID: job.ID, Role: controlplane.ArtifactRoleFinal, MediaType: "image/png",
		StoreDriver: controlplane.ArtifactStoreDriverMemory, ExpectedSizeBytes: int64(len(payload)), MaxBytes: 1024,
		ExternalReference: "https://provider.invalid/private-object",
	}, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := control.RotateAPIKey(context.Background(), "test", owner.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	metadata := performGatewayJobRequest(handler, http.MethodGet, "/v1/artifacts/"+artifact.ID, rotated.Key, "", "")
	if metadata.Code != http.StatusOK || !strings.Contains(metadata.Body.String(), artifact.ID) {
		t.Fatalf("metadata status=%d body=%s", metadata.Code, metadata.Body.String())
	}
	for _, secret := range []string{"store_key", "external_reference", "private-object", "provider.invalid"} {
		if strings.Contains(metadata.Body.String(), secret) {
			t.Fatalf("artifact metadata leaked %q: %s", secret, metadata.Body.String())
		}
	}
	list := performGatewayJobRequest(handler, http.MethodGet, "/v1/jobs/"+job.ID+"/artifacts", rotated.Key, "", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), artifact.ID) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	rangeResponse := performGatewayArtifactRequest(handler, http.MethodGet, "/v1/artifacts/"+artifact.ID+"/content", rotated.Key, "bytes=2-7")
	if rangeResponse.Code != http.StatusPartialContent || rangeResponse.Body.String() != string(payload[2:8]) ||
		rangeResponse.Header().Get("Content-Range") == "" || rangeResponse.Header().Get("ETag") == "" {
		t.Fatalf("range status=%d headers=%v body=%q", rangeResponse.Code, rangeResponse.Header(), rangeResponse.Body.String())
	}
	invalidRange := performGatewayArtifactRequest(handler, http.MethodGet, "/v1/artifacts/"+artifact.ID+"/content", rotated.Key, "bytes=999-")
	if invalidRange.Code != http.StatusRequestedRangeNotSatisfiable || invalidRange.Header().Get("Content-Range") == "" {
		t.Fatalf("invalid range status=%d headers=%v body=%s", invalidRange.Code, invalidRange.Header(), invalidRange.Body.String())
	}
	otherRequest := request
	otherRequest.Name = "other artifact owner"
	other, err := control.CreateAPIKey(context.Background(), "test", otherRequest)
	if err != nil {
		t.Fatal(err)
	}
	crossOwner := performGatewayJobRequest(handler, http.MethodGet, "/v1/artifacts/"+artifact.ID, other.Key, "", "")
	if crossOwner.Code != http.StatusNotFound || !strings.Contains(crossOwner.Body.String(), "resource_not_found") {
		t.Fatalf("cross-owner status=%d body=%s", crossOwner.Code, crossOwner.Body.String())
	}
	noScopeRequest := request
	noScopeRequest.Name = "artifact scope denied"
	noScopeRequest.Scopes = []string{controlplane.GatewayScopeInvoke, controlplane.GatewayScopeJobsRead}
	noScope, err := control.CreateAPIKey(context.Background(), "test", noScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	denied := performGatewayJobRequest(handler, http.MethodGet, "/v1/artifacts/"+artifact.ID, noScope.Key, "", "")
	if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), "policy_not_allowed") {
		t.Fatalf("missing scope status=%d body=%s", denied.Code, denied.Body.String())
	}
	deleteResponse := performGatewayJobRequest(handler, http.MethodDelete, "/v1/artifacts/"+artifact.ID, rotated.Key, "", "")
	if deleteResponse.Code != http.StatusAccepted || !strings.Contains(deleteResponse.Body.String(), controlplane.ArtifactStatusDeleteRequested) {
		t.Fatalf("delete status=%d body=%s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if processed, err := control.RunArtifactDeletionWorkerOnce(context.Background(), 1); err != nil || processed != 1 {
		t.Fatalf("delete worker processed=%d err=%v", processed, err)
	}
	deletedContent := performGatewayArtifactRequest(handler, http.MethodGet, "/v1/artifacts/"+artifact.ID+"/content", rotated.Key, "")
	if deletedContent.Code != http.StatusGone || !strings.Contains(deletedContent.Body.String(), "artifact_unavailable") {
		t.Fatalf("deleted content status=%d body=%s", deletedContent.Code, deletedContent.Body.String())
	}
}

func performGatewayArtifactRequest(handler http.Handler, method, target, key, byteRange string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	request.Header.Set("Authorization", "Bearer "+key)
	if byteRange != "" {
		request.Header.Set("Range", byteRange)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
