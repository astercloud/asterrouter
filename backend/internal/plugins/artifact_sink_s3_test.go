package plugins

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestArtifactSinkDestinationsSortAndProtectSecrets(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := newArtifactSinkTestService(t, repo, nil, nil)

	for _, id := range []string{"sink-z", "sink-a"} {
		request := testArtifactSinkDestinationRequest(id)
		request.Secrets[artifactSinkSessionToken] = "session-" + id
		if _, err := svc.UpsertArtifactSinkDestination(ctx, ArtifactS3SinkPluginID, id, request); err != nil {
			t.Fatalf("UpsertArtifactSinkDestination(%s): %v", id, err)
		}
	}

	destinations, err := svc.ArtifactSinkDestinations(ctx, ArtifactS3SinkPluginID)
	if err != nil {
		t.Fatalf("ArtifactSinkDestinations(): %v", err)
	}
	if got := []string{destinations[0].ID, destinations[1].ID}; !reflect.DeepEqual(got, []string{"sink-a", "sink-z"}) {
		t.Fatalf("destination order = %v", got)
	}
	for _, destination := range destinations {
		for name, hint := range destination.SecretHints {
			if hint == "" || strings.Contains(hint, destination.ID) {
				t.Fatalf("secret hint %s was not masked: %q", name, hint)
			}
		}
	}

	record, found, err := repo.FindConfig(ctx, ArtifactS3SinkPluginID)
	if err != nil || !found {
		t.Fatalf("FindConfig() found=%v err=%v", found, err)
	}
	serialized := strings.Join(mapValues(record.SecretCiphertexts), " ")
	for _, secret := range []string{"access-sink-a", "secret-sink-a", "session-sink-a"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("plaintext secret persisted: %q", secret)
		}
	}

	accessKey := record.SecretCiphertexts[artifactSinkConfigSecretKey("sink-a", artifactSinkAccessKey)]
	secretKey := record.SecretCiphertexts[artifactSinkConfigSecretKey("sink-a", artifactSinkSecretKey)]
	update := testArtifactSinkDestinationRequest("sink-a")
	update.Name = "Updated destination"
	update.Secrets = map[string]string{}
	update.ClearSessionToken = true
	updated, err := svc.UpsertArtifactSinkDestination(ctx, ArtifactS3SinkPluginID, "sink-a", update)
	if err != nil {
		t.Fatalf("update destination: %v", err)
	}
	if updated.Name != update.Name || updated.SecretHints[artifactSinkAccessKey] == "" || updated.SecretHints[artifactSinkSecretKey] == "" {
		t.Fatalf("updated destination = %+v", updated)
	}
	if _, found := updated.SecretHints[artifactSinkSessionToken]; found {
		t.Fatalf("session token hint was not cleared: %+v", updated.SecretHints)
	}
	record, _, _ = repo.FindConfig(ctx, ArtifactS3SinkPluginID)
	if record.SecretCiphertexts[artifactSinkConfigSecretKey("sink-a", artifactSinkAccessKey)] != accessKey ||
		record.SecretCiphertexts[artifactSinkConfigSecretKey("sink-a", artifactSinkSecretKey)] != secretKey {
		t.Fatal("existing credentials were not preserved")
	}
	if record.SecretCiphertexts[artifactSinkConfigSecretKey("sink-a", artifactSinkSessionToken)] != "" {
		t.Fatal("session token ciphertext was not cleared")
	}
}

func TestArtifactSinkDestinationValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ArtifactSinkDestinationRequest)
	}{
		{name: "HTTP endpoint", mutate: func(request *ArtifactSinkDestinationRequest) { request.Endpoint = "http://storage.example" }},
		{name: "endpoint path", mutate: func(request *ArtifactSinkDestinationRequest) { request.Endpoint = "https://storage.example/api" }},
		{name: "HTTP reference URL", mutate: func(request *ArtifactSinkDestinationRequest) { request.ReferenceBaseURL = "http://cdn.example/files" }},
		{name: "R2 without endpoint", mutate: func(request *ArtifactSinkDestinationRequest) { request.Provider = "r2"; request.Endpoint = "" }},
		{name: "OSS without endpoint", mutate: func(request *ArtifactSinkDestinationRequest) { request.Provider = "oss"; request.Endpoint = "" }},
		{name: "unknown profile scope", mutate: func(request *ArtifactSinkDestinationRequest) { request.AllowedProfileScope = "unknown" }},
		{name: "missing access key", mutate: func(request *ArtifactSinkDestinationRequest) { delete(request.Secrets, artifactSinkAccessKey) }},
		{name: "missing secret key", mutate: func(request *ArtifactSinkDestinationRequest) { delete(request.Secrets, artifactSinkSecretKey) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := newArtifactSinkTestService(t, NewMemoryRepository(), nil, nil)
			request := testArtifactSinkDestinationRequest("invalid")
			test.mutate(&request)
			_, err := svc.UpsertArtifactSinkDestination(context.Background(), ArtifactS3SinkPluginID, "invalid", request)
			if !errors.Is(err, ErrPluginConfigInvalid) {
				t.Fatalf("err = %v, want ErrPluginConfigInvalid", err)
			}
		})
	}
}

func TestArtifactSinkFactoryReceivesDecryptedConfiguration(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	registry := newArtifactSinkTestRegistry()
	var received controlplane.S3ArtifactStoreConfig
	factory := func(_ context.Context, config controlplane.S3ArtifactStoreConfig) (ArtifactSinkObjectStore, error) {
		received = config
		return &artifactSinkTestStore{}, nil
	}
	svc := newArtifactSinkTestService(t, repo, registry, factory)
	request := testArtifactSinkDestinationRequest("sink-a")
	request.Endpoint = "https://storage.example"
	request.Secrets[artifactSinkSessionToken] = "temporary-session"
	if _, err := svc.UpsertArtifactSinkDestination(ctx, ArtifactS3SinkPluginID, "sink-a", request); err != nil {
		t.Fatalf("configure destination: %v", err)
	}
	if _, err := svc.Enable(ctx, ArtifactS3SinkPluginID); err != nil {
		t.Fatalf("Enable(): %v", err)
	}
	if received.Endpoint != request.Endpoint || received.Region != request.Region || received.Bucket != request.Bucket || received.Prefix != request.Prefix ||
		received.AccessKey != request.Secrets[artifactSinkAccessKey] || received.SecretKey != request.Secrets[artifactSinkSecretKey] ||
		received.SessionToken != request.Secrets[artifactSinkSessionToken] || received.PathStyle != request.PathStyle {
		t.Fatalf("factory config = %+v", received)
	}
}

func TestS3ArtifactSinkOwnerIdempotencyAndReferences(t *testing.T) {
	store := &artifactSinkTestStore{}
	sink := &s3ArtifactSink{destination: artifactSinkDestinationRecord{
		ID: "customer-media", Provider: "r2", Bucket: "media", Prefix: "generated",
		ReferenceBaseURL: "https://cdn.example/media", AllowedProfileScope: "platform", AllowedTenantID: "tenant-a",
	}, store: store}
	owner := controlplane.ArtifactOwner{ProfileScope: "platform", TenantID: "tenant-a", PrincipalID: "principal-a", ExternalSubjectReference: "customer@example.com"}
	if sink.Accepts(controlplane.ArtifactOwner{ProfileScope: "enterprise", TenantID: "tenant-a"}) {
		t.Fatal("sink accepted an unauthorized profile")
	}
	if sink.Accepts(controlplane.ArtifactOwner{ProfileScope: "platform", TenantID: "tenant-b"}) {
		t.Fatal("sink accepted an unauthorized tenant")
	}
	request := controlplane.ArtifactSinkRequest{
		SinkID: sink.ID(), IdempotencyKey: "artifact-1", ArtifactID: "artifact-1", MediaType: "image/png", ExpectedSizeBytes: 7, Owner: owner,
	}
	first, err := sink.DeliverArtifact(context.Background(), request, bytes.NewBufferString("content"))
	if err != nil {
		t.Fatalf("DeliverArtifact(first): %v", err)
	}
	second, err := sink.DeliverArtifact(context.Background(), request, bytes.NewBufferString("content"))
	if err != nil {
		t.Fatalf("DeliverArtifact(second): %v", err)
	}
	if first.ExternalReference != second.ExternalReference || !strings.HasPrefix(first.ExternalReference, "https://cdn.example/media/generated/owners/") || !strings.HasSuffix(first.ExternalReference, "/artifact-1") {
		t.Fatalf("external reference = %q second=%q", first.ExternalReference, second.ExternalReference)
	}
	if len(store.puts) != 2 || store.puts[0] != store.puts[1] || strings.Contains(store.puts[0], owner.ExternalSubjectReference) {
		t.Fatalf("idempotent object keys = %v", store.puts)
	}
	otherRequest := request
	otherRequest.Owner.PrincipalID = "principal-b"
	if _, err := sink.DeliverArtifact(context.Background(), otherRequest, bytes.NewBufferString("content")); err != nil {
		t.Fatalf("DeliverArtifact(other tenant identity): %v", err)
	}
	if store.puts[2] == store.puts[0] {
		t.Fatalf("different owners shared object key %q", store.puts[0])
	}
	if err := sink.DeleteArtifact(context.Background(), request); err != nil {
		t.Fatalf("DeleteArtifact(): %v", err)
	}
	if len(store.deletes) != 1 || store.deletes[0] != store.puts[0] {
		t.Fatalf("delete keys = %v puts=%v", store.deletes, store.puts)
	}

	invalid := request
	invalid.IdempotencyKey = "different"
	if _, err := sink.DeliverArtifact(context.Background(), invalid, bytes.NewBufferString("content")); !errors.Is(err, controlplane.ErrArtifactSinkInvalid) {
		t.Fatalf("invalid idempotency err = %v", err)
	}
}

func TestArtifactSinkPluginLifecycleAndRestart(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	registry := newArtifactSinkTestRegistry()
	svc := newArtifactSinkTestService(t, repo, registry, nil)
	for _, id := range []string{"sink-b", "sink-a"} {
		if _, err := svc.UpsertArtifactSinkDestination(ctx, ArtifactS3SinkPluginID, id, testArtifactSinkDestinationRequest(id)); err != nil {
			t.Fatalf("configure %s: %v", id, err)
		}
	}
	if _, err := svc.Enable(ctx, ArtifactS3SinkPluginID); err != nil {
		t.Fatalf("Enable(): %v", err)
	}
	if got := registry.IDs(); !reflect.DeepEqual(got, []string{"sink-a", "sink-b"}) {
		t.Fatalf("registered sinks = %v", got)
	}

	if err := svc.DeleteArtifactSinkDestination(ctx, ArtifactS3SinkPluginID, "sink-b"); err != nil {
		t.Fatalf("DeleteArtifactSinkDestination(): %v", err)
	}
	if got := registry.IDs(); !reflect.DeepEqual(got, []string{"sink-a"}) {
		t.Fatalf("registered sinks after delete = %v", got)
	}
	if !reflect.DeepEqual(registry.removed, []string{"sink-b"}) {
		t.Fatalf("removed sinks = %v", registry.removed)
	}

	restartedRegistry := newArtifactSinkTestRegistry()
	restarted := newArtifactSinkTestService(t, repo, restartedRegistry, nil)
	if err := restarted.StartEnabledArtifactSinks(ctx); err != nil {
		t.Fatalf("StartEnabledArtifactSinks(): %v", err)
	}
	if got := restartedRegistry.IDs(); !reflect.DeepEqual(got, []string{"sink-a"}) {
		t.Fatalf("restarted sinks = %v", got)
	}
	if _, err := restarted.Disable(ctx, ArtifactS3SinkPluginID); err != nil {
		t.Fatalf("Disable(): %v", err)
	}
	if got := restartedRegistry.IDs(); len(got) != 0 {
		t.Fatalf("sinks remained after disable: %v", got)
	}
}

func TestArtifactSinkEnableRequiresRegistryAndRollsBackStatus(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := newArtifactSinkTestService(t, repo, nil, nil)
	if _, err := svc.UpsertArtifactSinkDestination(ctx, ArtifactS3SinkPluginID, "sink-a", testArtifactSinkDestinationRequest("sink-a")); err != nil {
		t.Fatalf("configure destination: %v", err)
	}
	if _, err := svc.Enable(ctx, ArtifactS3SinkPluginID); !errors.Is(err, ErrArtifactSinkRegistryRequired) {
		t.Fatalf("Enable() err = %v, want ErrArtifactSinkRegistryRequired", err)
	}
	plugin, found, err := repo.FindPlugin(ctx, ArtifactS3SinkPluginID)
	if err != nil || !found || plugin.Status != StatusDisabled {
		t.Fatalf("plugin after rollback = %+v found=%v err=%v", plugin, found, err)
	}
}

func TestArtifactSinkRegistrationFailureRestoresPreviousRuntime(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	registry := newArtifactSinkTestRegistry()
	svc := newArtifactSinkTestService(t, repo, registry, nil)
	for _, id := range []string{"sink-a", "sink-b"} {
		request := testArtifactSinkDestinationRequest(id)
		if id == "sink-b" {
			request.Enabled = false
		}
		if _, err := svc.UpsertArtifactSinkDestination(ctx, ArtifactS3SinkPluginID, id, request); err != nil {
			t.Fatalf("configure %s: %v", id, err)
		}
	}
	if _, err := svc.Enable(ctx, ArtifactS3SinkPluginID); err != nil {
		t.Fatalf("Enable(): %v", err)
	}
	previous := registry.Sink("sink-a")
	registry.failSetID = "sink-b"
	update := testArtifactSinkDestinationRequest("sink-b")
	if _, err := svc.UpsertArtifactSinkDestination(ctx, ArtifactS3SinkPluginID, "sink-b", update); err == nil {
		t.Fatal("expected runtime registration failure")
	}
	if registry.Sink("sink-a") != previous || registry.Sink("sink-b") != nil {
		t.Fatalf("runtime was not restored: ids=%v", registry.IDs())
	}
	destinations, err := svc.ArtifactSinkDestinations(ctx, ArtifactS3SinkPluginID)
	if err != nil {
		t.Fatalf("ArtifactSinkDestinations(): %v", err)
	}
	if destinations[1].Enabled {
		t.Fatalf("failed runtime update was persisted: %+v", destinations[1])
	}
}

func TestArtifactSinkPersistenceFailureRestoresRuntime(t *testing.T) {
	ctx := context.Background()
	repo := &failingArtifactSinkConfigRepository{MemoryRepository: NewMemoryRepository()}
	registry := newArtifactSinkTestRegistry()
	svc := newArtifactSinkTestService(t, repo, registry, nil)
	original := testArtifactSinkDestinationRequest("sink-a")
	if _, err := svc.UpsertArtifactSinkDestination(ctx, ArtifactS3SinkPluginID, "sink-a", original); err != nil {
		t.Fatalf("configure destination: %v", err)
	}
	if _, err := svc.Enable(ctx, ArtifactS3SinkPluginID); err != nil {
		t.Fatalf("Enable(): %v", err)
	}
	repo.failNextSave = true
	updated := testArtifactSinkDestinationRequest("sink-a")
	updated.Bucket = "new-bucket"
	if _, err := svc.UpsertArtifactSinkDestination(ctx, ArtifactS3SinkPluginID, "sink-a", updated); !errors.Is(err, errArtifactSinkConfigSave) {
		t.Fatalf("update err = %v", err)
	}
	restored, ok := registry.Sink("sink-a").(*s3ArtifactSink)
	if !ok || restored.destination.Bucket != original.Bucket {
		t.Fatalf("runtime sink was not restored after persistence failure: %+v", restored)
	}
	destinations, err := svc.ArtifactSinkDestinations(ctx, ArtifactS3SinkPluginID)
	if err != nil || destinations[0].Bucket != original.Bucket {
		t.Fatalf("persisted destinations = %+v err=%v", destinations, err)
	}
}

func newArtifactSinkTestService(t *testing.T, repo Repository, registry ArtifactSinkRegistry, factory ArtifactSinkStoreFactory) *Service {
	t.Helper()
	if factory == nil {
		factory = func(context.Context, controlplane.S3ArtifactStoreConfig) (ArtifactSinkObjectStore, error) {
			return &artifactSinkTestStore{}, nil
		}
	}
	svc := NewServiceWithOptions(repo, ServiceOptions{
		SecretKey: "artifact-sink-test-secret", ArtifactSinkRegistry: registry, ArtifactSinkStoreFactory: factory,
	})
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	return svc
}

func testArtifactSinkDestinationRequest(id string) ArtifactSinkDestinationRequest {
	return ArtifactSinkDestinationRequest{
		Name: "Destination " + id, Provider: "s3", Region: "us-east-1", Bucket: "bucket-" + id,
		Prefix: "artifacts", ReferenceBaseURL: "https://cdn.example/" + id, PathStyle: true, Enabled: true,
		Secrets: map[string]string{artifactSinkAccessKey: "access-" + id, artifactSinkSecretKey: "secret-" + id},
	}
}

func mapValues(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

type artifactSinkTestStore struct {
	mu      sync.Mutex
	puts    []string
	deletes []string
}

func (s *artifactSinkTestStore) Put(_ context.Context, key string, body io.Reader, _ int64, _ string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content, err := io.ReadAll(body)
	if err != nil {
		return 0, err
	}
	s.puts = append(s.puts, key)
	return int64(len(content)), nil
}

func (s *artifactSinkTestStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletes = append(s.deletes, key)
	return nil
}

type artifactSinkTestRegistry struct {
	mu        sync.Mutex
	sinks     map[string]controlplane.ArtifactSink
	removed   []string
	failSetID string
}

func newArtifactSinkTestRegistry() *artifactSinkTestRegistry {
	return &artifactSinkTestRegistry{sinks: map[string]controlplane.ArtifactSink{}}
}

func (r *artifactSinkTestRegistry) SetArtifactSink(sink controlplane.ArtifactSink) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sink.ID() == r.failSetID {
		return errors.New("synthetic registry failure")
	}
	r.sinks[sink.ID()] = sink
	return nil
}

func (r *artifactSinkTestRegistry) RemoveArtifactSink(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sinks, id)
	r.removed = append(r.removed, id)
}

func (r *artifactSinkTestRegistry) IDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.sinks))
	for id := range r.sinks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (r *artifactSinkTestRegistry) Sink(id string) controlplane.ArtifactSink {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sinks[id]
}

var errArtifactSinkConfigSave = errors.New("synthetic artifact sink config save failure")

type failingArtifactSinkConfigRepository struct {
	*MemoryRepository
	failNextSave bool
}

func (r *failingArtifactSinkConfigRepository) SaveConfig(ctx context.Context, record configRecord) error {
	if r.failNextSave {
		r.failNextSave = false
		return errArtifactSinkConfigSave
	}
	return r.MemoryRepository.SaveConfig(ctx, record)
}
