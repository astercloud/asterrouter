package controlplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

func TestDurableProviderOutputsBecomeDeliverableArtifacts(t *testing.T) {
	tests := []struct {
		modality  string
		mediaType string
		payload   []byte
	}{
		{modality: "image", mediaType: "image/png", payload: []byte("synthetic-image-output")},
		{modality: "video", mediaType: "video/mp4", payload: []byte("synthetic-video-output")},
		{modality: "audio", mediaType: "audio/mpeg", payload: []byte("synthetic-audio-output")},
	}
	for _, test := range tests {
		t.Run(test.modality, func(t *testing.T) {
			fixture := newProviderOutputFixture(t, test.modality, GatewayArtifactPolicyTemporary, test.payload)
			report, err := fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter)
			if err != nil || report.Completed != 1 || report.Errors != 0 {
				t.Fatalf("reconcile report=%+v err=%v", report, err)
			}
			job, found, err := fixture.service.repo.FindAIJob(context.Background(), fixture.job.ID)
			if err != nil || !found || job.Status != AIJobStatusSucceeded {
				t.Fatalf("job=%+v found=%t err=%v", job, found, err)
			}
			artifacts, err := fixture.service.repo.QueryArtifacts(context.Background(), ArtifactQuery{JobID: fixture.job.ID, Limit: 10})
			if err != nil || len(artifacts) != 1 {
				t.Fatalf("artifacts=%+v err=%v", artifacts, err)
			}
			artifact := artifacts[0]
			if artifact.Status != ArtifactStatusReady || artifact.Role != ArtifactRoleFinal || artifact.MediaType != test.mediaType || artifact.SizeBytes != int64(len(test.payload)) {
				t.Fatalf("artifact=%+v", artifact)
			}
			opened, err := fixture.store.Open(context.Background(), artifact.StoreKey, nil)
			if err != nil {
				t.Fatal(err)
			}
			stored, readErr := io.ReadAll(opened.Body)
			_ = opened.Body.Close()
			if readErr != nil || !bytes.Equal(stored, test.payload) {
				t.Fatalf("stored=%q err=%v", stored, readErr)
			}
			openCalls := fixture.adapter.OpenCalls()
			provider := fixture.adapter.Provider()
			attempt := fixture.adapter.Attempt()
			if _, err := fixture.service.ingestProviderOutputs(context.Background(), provider, fixture.job, attempt, fixture.adapter.result.Outputs, fixture.adapter); err != nil {
				t.Fatalf("idempotent ingest: %v", err)
			}
			if fixture.adapter.OpenCalls() != openCalls || fixture.adapter.DispatchCalls() != 1 {
				t.Fatalf("idempotent ingest open=%d dispatch=%d", fixture.adapter.OpenCalls(), fixture.adapter.DispatchCalls())
			}
		})
	}
}

func TestDurableProviderOutputDownloadFailureRecoversWithoutRedispatch(t *testing.T) {
	fixture := newProviderOutputFixture(t, "video", GatewayArtifactPolicyTemporary, []byte("recoverable-video-output"))
	fixture.adapter.openFailures = 1
	first, err := fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter)
	if err == nil || first.Errors != 1 || first.Completed != 0 {
		t.Fatalf("first reconcile=%+v err=%v", first, err)
	}
	assertAIJobStatus(t, fixture.service, fixture.job.ID, AIJobStatusRunning)
	artifacts, _ := fixture.service.repo.QueryArtifacts(context.Background(), ArtifactQuery{JobID: fixture.job.ID, Limit: 10})
	if len(artifacts) != 1 || artifacts[0].Status != ArtifactStatusFailed {
		t.Fatalf("failed artifacts=%+v", artifacts)
	}
	fixture.now = fixture.now.Add(AIJobDefaultRetryAfter + time.Second)
	second, err := fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter)
	if err != nil || second.Completed != 1 || second.Errors != 0 {
		t.Fatalf("second reconcile=%+v err=%v", second, err)
	}
	assertAIJobStatus(t, fixture.service, fixture.job.ID, AIJobStatusSucceeded)
	artifacts, _ = fixture.service.repo.QueryArtifacts(context.Background(), ArtifactQuery{JobID: fixture.job.ID, Limit: 10})
	if len(artifacts) != 1 || artifacts[0].Status != ArtifactStatusReady || fixture.adapter.DispatchCalls() != 1 || fixture.adapter.OpenCalls() != 2 {
		t.Fatalf("recovered artifacts=%+v dispatch=%d open=%d", artifacts, fixture.adapter.DispatchCalls(), fixture.adapter.OpenCalls())
	}
	events, _ := fixture.service.ArtifactEvents(context.Background(), artifacts[0].ID)
	wantStatuses := []string{ArtifactStatusPending, ArtifactStatusUploading, ArtifactStatusFailed, ArtifactStatusUploading, ArtifactStatusReady}
	if len(events) != len(wantStatuses) {
		t.Fatalf("events=%+v", events)
	}
	for index, status := range wantStatuses {
		if events[index].ToStatus != status {
			t.Fatalf("event %d status=%q want=%q", index, events[index].ToStatus, status)
		}
	}
}

func TestConcurrentProviderOutputReconciliationOpensContentOnce(t *testing.T) {
	fixture := newProviderOutputFixture(t, "image", GatewayArtifactPolicyTemporary, []byte("concurrent-image-output"))
	fixture.adapter.blockOpen = make(chan struct{})
	fixture.adapter.opened = make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter)
		firstDone <- err
	}()
	select {
	case <-fixture.adapter.opened:
	case <-time.After(5 * time.Second):
		t.Fatal("first provider output did not open")
	}
	second, secondErr := fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter)
	if secondErr == nil || second.Errors != 1 || !errors.Is(secondErr, ErrArtifactIngestInProgress) {
		t.Fatalf("second reconcile=%+v err=%v", second, secondErr)
	}
	close(fixture.adapter.blockOpen)
	if err := <-firstDone; err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	artifacts, _ := fixture.service.repo.QueryArtifacts(context.Background(), ArtifactQuery{JobID: fixture.job.ID, Limit: 10})
	if len(artifacts) != 1 || artifacts[0].Status != ArtifactStatusReady || fixture.adapter.OpenCalls() != 1 || fixture.adapter.DispatchCalls() != 1 {
		t.Fatalf("artifacts=%+v open=%d dispatch=%d", artifacts, fixture.adapter.OpenCalls(), fixture.adapter.DispatchCalls())
	}
}

func TestProviderOutputPoliciesFailClosed(t *testing.T) {
	payload := []byte("policy-output")
	metadata := newProviderOutputFixture(t, "image", GatewayArtifactPolicyMetadataOnly, payload)
	metadata.adapter.result.Outputs[0].PersistentReference = true
	metadata.adapter.result.Outputs[0].ProviderReference = "provider-file-stable-1"
	artifacts, err := metadata.service.ingestProviderOutputs(context.Background(), metadata.adapter.Provider(), metadata.job, metadata.adapter.Attempt(), metadata.adapter.result.Outputs, metadata.adapter)
	if err != nil || len(artifacts) != 1 || artifacts[0].Status != ArtifactStatusReady || artifacts[0].StoreDriver != ArtifactStoreDriverNone || artifacts[0].ExternalReference == "" || metadata.adapter.OpenCalls() != 0 {
		t.Fatalf("metadata artifacts=%+v open=%d err=%v", artifacts, metadata.adapter.OpenCalls(), err)
	}
	changedReference := append([]ProviderOutputDescriptor(nil), metadata.adapter.result.Outputs...)
	changedReference[0].ProviderReference = "provider-file-stable-2"
	if _, err := metadata.service.ingestProviderOutputs(context.Background(), metadata.adapter.Provider(), metadata.job, metadata.adapter.Attempt(), changedReference, metadata.adapter); !errors.Is(err, ErrArtifactIntegrity) {
		t.Fatalf("metadata reference replay error=%v", err)
	}

	for _, policy := range []string{GatewayArtifactPolicyProxyOnly} {
		fixture := newProviderOutputFixture(t, "image", policy, payload)
		fixture.adapter.result.Outputs[0].ProviderReference = ""
		_, err := fixture.service.ingestProviderOutputs(context.Background(), fixture.adapter.Provider(), fixture.job, fixture.adapter.Attempt(), fixture.adapter.result.Outputs, fixture.adapter)
		if !errors.Is(err, ErrProviderOutputReferenceRequired) {
			t.Fatalf("proxy policy error=%v", err)
		}
	}
}

func TestProxyOnlyProviderOutputUsesAuthorizedPluginReader(t *testing.T) {
	payload := []byte("provider-retained-image")
	fixture := newProviderOutputFixture(t, "image", GatewayArtifactPolicyProxyOnly, payload)
	report, err := fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter)
	if err != nil || report.Completed != 1 || report.Errors != 0 {
		t.Fatalf("reconcile report=%+v err=%v", report, err)
	}
	artifacts, queryErr := fixture.service.repo.QueryArtifacts(context.Background(), ArtifactQuery{JobID: fixture.job.ID, Limit: 10})
	if queryErr != nil || len(artifacts) != 1 || artifacts[0].Status != ArtifactStatusReady || artifacts[0].StoreDriver != ArtifactStoreDriverNone || artifacts[0].ExternalReference == "" {
		t.Fatalf("artifacts=%+v err=%v", artifacts, queryErr)
	}
	if fixture.adapter.OpenCalls() != 0 {
		t.Fatalf("provider body opened during proxy registration=%d", fixture.adapter.OpenCalls())
	}
	auth := gatewaycore.CanonicalAuthContext{
		ProfileScope: ProfileScopePlatform, TenantID: "output-tenant", PrincipalType: APIKeyTypeService, PrincipalID: "output-principal",
	}
	artifact, opened, found, err := fixture.service.OpenArtifactForAuth(context.Background(), auth, artifacts[0].ID, nil)
	if err != nil || !found || artifact.ID != artifacts[0].ID {
		t.Fatalf("open artifact=%+v found=%t err=%v", artifact, found, err)
	}
	proxied, readErr := io.ReadAll(opened.Body)
	_ = opened.Body.Close()
	if readErr != nil || !bytes.Equal(proxied, payload) {
		t.Fatalf("proxied=%q err=%v", proxied, readErr)
	}
	_, ranged, found, err := fixture.service.OpenArtifactForAuth(context.Background(), auth, artifacts[0].ID, &ArtifactByteRange{Offset: 3, Length: 7})
	if err != nil || !found {
		t.Fatalf("range found=%t err=%v", found, err)
	}
	rangePayload, readErr := io.ReadAll(ranged.Body)
	_ = ranged.Body.Close()
	if readErr != nil || !bytes.Equal(rangePayload, payload[3:10]) {
		t.Fatalf("range=%q err=%v", rangePayload, readErr)
	}
	other := auth
	other.PrincipalID = "other-principal"
	if _, _, found, err := fixture.service.OpenArtifactForAuth(context.Background(), other, artifacts[0].ID, nil); err != nil || found {
		t.Fatalf("cross-owner proxy found=%t err=%v", found, err)
	}
}

func TestProxyOnlyFailsClosedWithoutPluginOrValidRange(t *testing.T) {
	missing := newProviderOutputFixture(t, "video", GatewayArtifactPolicyProxyOnly, []byte("provider-retained-video"))
	missing.service.artifactProxyMu.Lock()
	delete(missing.service.artifactProxies, missing.adapter.Provider().ID)
	missing.service.artifactProxyMu.Unlock()
	if _, err := missing.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, missing.adapter); !errors.Is(err, ErrArtifactProxyRequired) {
		t.Fatalf("missing proxy error=%v", err)
	}

	invalid := newProviderOutputFixture(t, "audio", GatewayArtifactPolicyProxyOnly, []byte("provider-retained-audio"))
	if _, err := invalid.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, invalid.adapter); err != nil {
		t.Fatal(err)
	}
	invalid.proxy.invalidRange = true
	artifacts, _ := invalid.service.repo.QueryArtifacts(context.Background(), ArtifactQuery{JobID: invalid.job.ID, Limit: 1})
	auth := gatewaycore.CanonicalAuthContext{ProfileScope: ProfileScopePlatform, TenantID: "output-tenant", PrincipalType: APIKeyTypeService, PrincipalID: "output-principal"}
	if _, _, _, err := invalid.service.OpenArtifactForAuth(context.Background(), auth, artifacts[0].ID, nil); !errors.Is(err, ErrArtifactIntegrity) {
		t.Fatalf("invalid proxy range error=%v", err)
	}
}

func TestCustomerSinkProviderOutputDeliveredIdempotently(t *testing.T) {
	payload := []byte("customer-sink-image-output")
	fixture := newProviderOutputFixture(t, "image", GatewayArtifactPolicyCustomerSink, payload)
	report, err := fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter)
	if err != nil || report.Completed != 1 || report.Errors != 0 {
		t.Fatalf("reconcile report=%+v err=%v", report, err)
	}
	assertAIJobStatus(t, fixture.service, fixture.job.ID, AIJobStatusSucceeded)
	artifacts, queryErr := fixture.service.repo.QueryArtifacts(context.Background(), ArtifactQuery{JobID: fixture.job.ID, Limit: 10})
	if queryErr != nil || len(artifacts) != 1 {
		t.Fatalf("artifacts=%+v err=%v", artifacts, queryErr)
	}
	artifact := artifacts[0]
	if artifact.Status != ArtifactStatusDelivered || artifact.StoreDriver != ArtifactStoreDriverNone || artifact.StoreKey != "" || artifact.ExternalReference == "" || !bytes.Equal(fixture.sink.Payload(), payload) {
		t.Fatalf("artifact=%+v payload=%q", artifact, fixture.sink.Payload())
	}
	request := fixture.sink.Requests()[0]
	if request.IdempotencyKey != artifact.ID || request.SinkID != fixture.job.ArtifactSinkID || request.Owner != artifactOwnerFromJob(fixture.job) {
		t.Fatalf("sink request=%+v", request)
	}
	openCalls := fixture.adapter.OpenCalls()
	if _, err := fixture.service.ingestProviderOutputs(context.Background(), fixture.adapter.Provider(), fixture.job, fixture.adapter.Attempt(), fixture.adapter.result.Outputs, fixture.adapter); err != nil {
		t.Fatalf("idempotent sink ingest: %v", err)
	}
	if fixture.sink.Deliveries() != 1 || fixture.adapter.OpenCalls() != openCalls {
		t.Fatalf("sink deliveries=%d opens=%d", fixture.sink.Deliveries(), fixture.adapter.OpenCalls())
	}
}

func TestCustomerSinkDeliveryFailureRecoversWithoutRedispatch(t *testing.T) {
	fixture := newProviderOutputFixture(t, "video", GatewayArtifactPolicyCustomerSink, []byte("recoverable-customer-video"))
	fixture.sink.failures = 1
	first, err := fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter)
	if err == nil || first.Errors != 1 || first.Completed != 0 {
		t.Fatalf("first reconcile=%+v err=%v", first, err)
	}
	assertAIJobStatus(t, fixture.service, fixture.job.ID, AIJobStatusRunning)
	artifacts, _ := fixture.service.repo.QueryArtifacts(context.Background(), ArtifactQuery{JobID: fixture.job.ID, Limit: 10})
	if len(artifacts) != 1 || artifacts[0].Status != ArtifactStatusDeliveryFailed || artifacts[0].ErrorType != "sink_delivery_failed" || fixture.sink.Deletes() != 1 {
		t.Fatalf("failed artifacts=%+v deletes=%d", artifacts, fixture.sink.Deletes())
	}
	fixture.now = fixture.now.Add(AIJobDefaultRetryAfter + time.Second)
	second, err := fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter)
	if err != nil || second.Completed != 1 || second.Errors != 0 {
		t.Fatalf("second reconcile=%+v err=%v", second, err)
	}
	artifacts, _ = fixture.service.repo.QueryArtifacts(context.Background(), ArtifactQuery{JobID: fixture.job.ID, Limit: 10})
	if len(artifacts) != 1 || artifacts[0].Status != ArtifactStatusDelivered || fixture.sink.Deliveries() != 2 || fixture.adapter.DispatchCalls() != 1 || fixture.adapter.OpenCalls() != 2 {
		t.Fatalf("recovered artifacts=%+v deliveries=%d dispatch=%d open=%d", artifacts, fixture.sink.Deliveries(), fixture.adapter.DispatchCalls(), fixture.adapter.OpenCalls())
	}
	requests := fixture.sink.Requests()
	if len(requests) != 2 || requests[0].IdempotencyKey != requests[1].IdempotencyKey {
		t.Fatalf("sink requests=%+v", requests)
	}
}

func TestCustomerSinkFailsClosedForMissingOrForeignSink(t *testing.T) {
	missing := newProviderOutputFixture(t, "image", GatewayArtifactPolicyCustomerSink, []byte("missing-sink"))
	missing.service.artifactSinkMu.Lock()
	delete(missing.service.artifactSinks, missing.job.ArtifactSinkID)
	missing.service.artifactSinkMu.Unlock()
	if _, err := missing.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, missing.adapter); !errors.Is(err, ErrArtifactSinkRequired) {
		t.Fatalf("missing sink error=%v", err)
	}

	foreign := newProviderOutputFixture(t, "image", GatewayArtifactPolicyCustomerSink, []byte("foreign-sink"))
	foreign.sink.denied = true
	if _, err := foreign.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, foreign.adapter); !errors.Is(err, ErrArtifactSinkForbidden) {
		t.Fatalf("foreign sink error=%v", err)
	}
	if foreign.sink.Deliveries() != 0 {
		t.Fatalf("foreign sink deliveries=%d", foreign.sink.Deliveries())
	}
}

func TestCustomerSinkRejectsIncompleteInvalidOrCorruptDelivery(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*providerOutputFixture)
	}{
		{name: "incomplete reader", configure: func(fixture *providerOutputFixture) { fixture.sink.partial = true }},
		{name: "invalid reference", configure: func(fixture *providerOutputFixture) { fixture.sink.reference = "invalid\nreference" }},
		{name: "sha mismatch", configure: func(fixture *providerOutputFixture) { fixture.adapter.payload = []byte("corrupt-customer-output") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newProviderOutputFixture(t, "image", GatewayArtifactPolicyCustomerSink, []byte("expected-customer-output"))
			test.configure(fixture)
			report, err := fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter)
			if err == nil || report.Errors != 1 || report.Completed != 0 {
				t.Fatalf("reconcile report=%+v err=%v", report, err)
			}
			artifacts, queryErr := fixture.service.repo.QueryArtifacts(context.Background(), ArtifactQuery{JobID: fixture.job.ID, Limit: 10})
			if queryErr != nil || len(artifacts) != 1 || artifacts[0].Status != ArtifactStatusDeliveryFailed || fixture.sink.Deletes() != 1 {
				t.Fatalf("artifacts=%+v deletes=%d err=%v", artifacts, fixture.sink.Deletes(), queryErr)
			}
			assertAIJobStatus(t, fixture.service, fixture.job.ID, AIJobStatusRunning)
		})
	}
}

func TestConcurrentCustomerSinkDeliveryRunsOnce(t *testing.T) {
	fixture := newProviderOutputFixture(t, "audio", GatewayArtifactPolicyCustomerSink, []byte("concurrent-customer-audio"))
	fixture.sink.started = make(chan struct{})
	fixture.sink.block = make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter)
		firstDone <- err
	}()
	select {
	case <-fixture.sink.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first sink delivery did not start")
	}
	second, secondErr := fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter)
	if secondErr == nil || second.Errors != 1 || !errors.Is(secondErr, ErrArtifactDeliveryInProgress) {
		t.Fatalf("second reconcile=%+v err=%v", second, secondErr)
	}
	close(fixture.sink.block)
	if err := <-firstDone; err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if fixture.sink.Deliveries() != 1 {
		t.Fatalf("sink deliveries=%d", fixture.sink.Deliveries())
	}
}

func TestProviderFailureIgnoresOutputsAndTerminates(t *testing.T) {
	fixture := newProviderOutputFixture(t, "image", GatewayArtifactPolicyTemporary, []byte("unused-output"))
	fixture.adapter.result.Task.Status = "failed"
	fixture.adapter.result.Outputs = []ProviderOutputDescriptor{{OutputID: "", Role: ArtifactRoleFinal}}

	report, err := fixture.service.RunDurableAIJobReconcilerOnce(context.Background(), 1, fixture.adapter)
	if err != nil || report.Completed != 1 || report.Errors != 0 {
		t.Fatalf("reconcile report=%+v err=%v", report, err)
	}
	assertAIJobStatus(t, fixture.service, fixture.job.ID, AIJobStatusFailed)
	artifacts, queryErr := fixture.service.repo.QueryArtifacts(context.Background(), ArtifactQuery{JobID: fixture.job.ID, Limit: 10})
	if queryErr != nil || len(artifacts) != 0 || fixture.adapter.OpenCalls() != 0 {
		t.Fatalf("artifacts=%+v open=%d err=%v", artifacts, fixture.adapter.OpenCalls(), queryErr)
	}
}

func TestProviderOutputsDeliverableRequiresCurrentAttempt(t *testing.T) {
	job := AIJob{ID: "job-output", Modality: "image"}
	ready := Artifact{JobID: job.ID, AttemptID: "attempt-current", Role: ArtifactRoleFinal, Status: ArtifactStatusReady}
	if err := providerOutputsDeliverable(job, "attempt-current", []Artifact{ready}); err != nil {
		t.Fatalf("current attempt deliverable error=%v", err)
	}
	ready.AttemptID = "attempt-old"
	if err := providerOutputsDeliverable(job, "attempt-current", []Artifact{ready}); !errors.Is(err, ErrProviderOutputsRequired) {
		t.Fatalf("old attempt deliverable error=%v", err)
	}
}

func TestProviderOutputDescriptorValidation(t *testing.T) {
	tests := [][]ProviderOutputDescriptor{
		{{OutputID: "", Role: ArtifactRoleFinal}},
		{{OutputID: "output", Role: ArtifactRoleInput}},
		{{OutputID: "output", Role: ArtifactRoleFinal, ExpectedSHA256: "bad"}},
		{{OutputID: "duplicate", Role: ArtifactRoleFinal}, {OutputID: "duplicate", Role: ArtifactRolePreview}},
		{{OutputID: "reference", Role: ArtifactRoleFinal, PersistentReference: true}},
	}
	for _, outputs := range tests {
		if _, err := normalizeProviderOutputs(outputs); !errors.Is(err, ErrProviderOutputInvalid) {
			t.Fatalf("outputs=%+v error=%v", outputs, err)
		}
	}
}

type providerOutputFixture struct {
	service *Service
	store   *MemoryArtifactStore
	sink    *mediaArtifactSink
	proxy   *mediaArtifactProxy
	adapter *mediaProviderOutputAdapter
	job     AIJob
	now     time.Time
}

func newProviderOutputFixture(t *testing.T, modality, policy string, payload []byte) *providerOutputFixture {
	t.Helper()
	ctx := context.Background()
	base := time.Date(2026, time.July, 15, 16, 0, 0, 0, time.UTC)
	fixture := &providerOutputFixture{now: base}
	fixture.service = NewService(NewMemoryRepository(), "/v1", "provider-output-secret")
	fixture.service.now = func() time.Time { return fixture.now }
	fixture.store = NewMemoryArtifactStore()
	if err := fixture.service.SetArtifactStore(fixture.store); err != nil {
		t.Fatal(err)
	}
	artifactSinkID := ""
	if policy == GatewayArtifactPolicyCustomerSink {
		fixture.sink = &mediaArtifactSink{id: "sink-" + modality}
		if err := fixture.service.SetArtifactSink(fixture.sink); err != nil {
			t.Fatal(err)
		}
		artifactSinkID = fixture.sink.ID()
	}
	provider, err := fixture.service.CreateProvider(ctx, "test", ProviderRequest{
		Name: "Output provider " + modality, Type: "openai_compatible", BaseURL: "https://provider.example/v1",
		Status: ProviderStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if policy == GatewayArtifactPolicyProxyOnly {
		fixture.proxy = &mediaArtifactProxy{providerID: provider.ID, payload: append([]byte(nil), payload...)}
		if err := fixture.service.SetArtifactProxy(fixture.proxy); err != nil {
			t.Fatal(err)
		}
	}
	account, err := fixture.service.CreateProviderAccount(ctx, "test", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Output account " + modality, Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Models: []string{"output-upstream-" + modality}, Secret: "account-secret", Concurrency: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	modelID := "output-" + modality
	model, err := fixture.service.CreateGatewayModel(ctx, "test", GatewayModelRequest{ModelID: modelID, Name: "Output " + modality, Modality: modality, Status: GatewayModelStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.CreateModelRoute(ctx, "test", ModelRouteRequest{
		GatewayModelID: model.ID, RouteGroup: DefaultModelRouteGroup, ProviderAccountID: account.ID,
		UpstreamModel: "output-upstream-" + modality, Priority: 10, Weight: 100, Status: ModelRouteStatusActive, UpstreamFormat: UpstreamFormatNativeMedia,
	}); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	mediaType := map[string]string{"image": "image/png", "video": "video/mp4", "audio": "audio/mpeg"}[modality]
	fixture.adapter = &mediaProviderOutputAdapter{payload: append([]byte(nil), payload...)}
	fixture.adapter.result = ProviderDispatchResult{
		Outcome: ProviderDispatchOutcomeAccepted,
		Task:    ProviderTaskReference{ProviderTaskID: "task-output-" + modality, Status: "succeeded"},
		Outputs: []ProviderOutputDescriptor{{
			OutputID: "final-" + modality, Role: ArtifactRoleFinal, MediaType: mediaType,
			ExpectedSizeBytes: int64(len(payload)), ExpectedSHA256: hex.EncodeToString(digest[:]),
		}},
		ReconcileAfter: base.Add(time.Hour),
	}
	if policy == GatewayArtifactPolicyProxyOnly {
		fixture.adapter.result.Outputs[0].ProviderReference = "provider-reference-" + modality
	}
	fixture.job, _, err = fixture.service.BeginDurableAIJob(ctx, gatewaycore.CanonicalAuthContext{
		CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: "output-key", ProfileScope: ProfileScopePlatform,
		TenantID: "output-tenant", PrincipalType: APIKeyTypeService, PrincipalID: "output-principal", ArtifactPolicy: policy, ArtifactSinkID: artifactSinkID,
	}, gatewaycore.CanonicalRequest{
		ID: "request-output-" + modality + "-" + policy, ClientRequestID: "client-output", Fingerprint: "fingerprint-output-" + modality + "-" + policy,
		IdempotencyKey: "idem-output-" + modality + "-" + policy, Protocol: gatewaycore.ProtocolAsterJobs,
		Operation: modality + "_generation", Modality: modality, Lane: gatewaycore.LaneDurable, Model: modelID,
		Payload: []byte(`{"input":{"prompt":"synthetic"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := fixture.service.RunDurableAIJobWorkerOnce(ctx, "output-worker", time.Minute, 1, fixture.adapter)
	if err != nil || worker.Accepted != 1 {
		t.Fatalf("worker=%+v err=%v", worker, err)
	}
	fixture.now = base.Add(time.Hour + time.Second)
	return fixture
}

type mediaProviderOutputAdapter struct {
	mu           sync.Mutex
	result       ProviderDispatchResult
	payload      []byte
	dispatches   int
	reconciles   int
	opens        int
	openFailures int
	provider     GatewayProvider
	attempt      AIAttempt
	blockOpen    chan struct{}
	opened       chan struct{}
	openedOnce   sync.Once
}

func (a *mediaProviderOutputAdapter) DispatchProviderTask(_ context.Context, provider GatewayProvider, _ AIJob, attempt AIAttempt, _ ProviderDispatchCommand) (ProviderDispatchResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dispatches++
	a.provider = provider
	a.attempt = attempt
	return ProviderDispatchResult{
		Outcome:        ProviderDispatchOutcomeAccepted,
		Task:           ProviderTaskReference{ProviderTaskID: a.result.Task.ProviderTaskID, Status: "running"},
		ReconcileAfter: a.result.ReconcileAfter,
	}, nil
}

func (a *mediaProviderOutputAdapter) ReconcileProviderTask(_ context.Context, provider GatewayProvider, _ AIJob, attempt AIAttempt, _ ProviderDispatchIntent, _ ProviderTaskReference) (ProviderDispatchResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reconciles++
	a.provider = provider
	a.attempt = attempt
	return a.result, nil
}

func (a *mediaProviderOutputAdapter) OpenProviderOutput(_ context.Context, _ GatewayProvider, _ AIJob, _ AIAttempt, _ ProviderOutputDescriptor) (io.ReadCloser, error) {
	a.mu.Lock()
	a.opens++
	if a.openFailures > 0 {
		a.openFailures--
		a.mu.Unlock()
		return nil, errors.New("synthetic provider output download failure")
	}
	payload := append([]byte(nil), a.payload...)
	block := a.blockOpen
	opened := a.opened
	a.mu.Unlock()
	if opened != nil {
		a.openedOnce.Do(func() { close(opened) })
	}
	if block != nil {
		<-block
	}
	return io.NopCloser(bytes.NewReader(payload)), nil
}

func (a *mediaProviderOutputAdapter) DispatchCalls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dispatches
}
func (a *mediaProviderOutputAdapter) OpenCalls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.opens
}
func (a *mediaProviderOutputAdapter) Provider() GatewayProvider {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.provider
}
func (a *mediaProviderOutputAdapter) Attempt() AIAttempt {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.attempt
}

type mediaArtifactSink struct {
	mu          sync.Mutex
	id          string
	denied      bool
	partial     bool
	failures    int
	deliveries  int
	deletes     int
	payload     []byte
	requests    []ArtifactSinkRequest
	started     chan struct{}
	block       chan struct{}
	reference   string
	startedOnce sync.Once
}

func (s *mediaArtifactSink) ID() string { return s.id }

func (s *mediaArtifactSink) Accepts(ArtifactOwner) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.denied
}

func (s *mediaArtifactSink) DeliverArtifact(_ context.Context, request ArtifactSinkRequest, body io.Reader) (ArtifactSinkResult, error) {
	s.mu.Lock()
	s.deliveries++
	s.requests = append(s.requests, request)
	started := s.started
	block := s.block
	partial := s.partial
	reference := s.reference
	shouldFail := s.failures > 0
	if shouldFail {
		s.failures--
	}
	s.mu.Unlock()
	if started != nil {
		s.startedOnce.Do(func() { close(started) })
	}
	if block != nil {
		<-block
	}
	var payload []byte
	var err error
	if partial {
		buffer := make([]byte, 1)
		read, readErr := body.Read(buffer)
		payload = append(payload, buffer[:read]...)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			err = readErr
		}
	} else {
		payload, err = io.ReadAll(body)
	}
	if err != nil {
		return ArtifactSinkResult{}, err
	}
	s.mu.Lock()
	s.payload = append([]byte(nil), payload...)
	s.mu.Unlock()
	if shouldFail {
		return ArtifactSinkResult{}, errors.New("synthetic customer sink failure")
	}
	if reference == "" {
		reference = "s3://customer-bucket/" + request.ArtifactID
	}
	return ArtifactSinkResult{ExternalReference: reference}, nil
}

func (s *mediaArtifactSink) DeleteArtifact(context.Context, ArtifactSinkRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletes++
	s.payload = nil
	return nil
}

func (s *mediaArtifactSink) Deliveries() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deliveries
}

func (s *mediaArtifactSink) Deletes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deletes
}

func (s *mediaArtifactSink) Payload() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.payload...)
}

func (s *mediaArtifactSink) Requests() []ArtifactSinkRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ArtifactSinkRequest(nil), s.requests...)
}

type mediaArtifactProxy struct {
	mu           sync.Mutex
	providerID   string
	payload      []byte
	requests     []ArtifactProxyRequest
	invalidRange bool
}

func (p *mediaArtifactProxy) ProviderID() string { return p.providerID }

func (p *mediaArtifactProxy) OpenArtifact(_ context.Context, request ArtifactProxyRequest, requested *ArtifactByteRange) (ArtifactRead, error) {
	p.mu.Lock()
	p.requests = append(p.requests, request)
	payload := append([]byte(nil), p.payload...)
	invalidRange := p.invalidRange
	p.mu.Unlock()
	offset, length, err := normalizeArtifactByteRange(int64(len(payload)), requested)
	if err != nil {
		return ArtifactRead{}, err
	}
	content := payload[offset : offset+length]
	reportedOffset := offset
	if invalidRange {
		reportedOffset++
	}
	return ArtifactRead{
		Body: io.NopCloser(bytes.NewReader(content)), Offset: reportedOffset,
		SizeBytes: length, TotalBytes: int64(len(payload)),
	}, nil
}
