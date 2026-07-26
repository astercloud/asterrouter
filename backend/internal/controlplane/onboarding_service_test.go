package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestOnboardingSessionRepositoryContract(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) Repository
	}{
		{name: "memory", open: func(*testing.T) Repository { return NewMemoryRepository() }},
		{name: "postgres", open: func(t *testing.T) Repository {
			schema := testutil.NewPostgresSchema(t)
			repo, err := NewPostgresRepository(context.Background(), schema.URL)
			if err != nil {
				t.Fatalf("NewPostgresRepository(): %v", err)
			}
			return repo
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			ctx := context.Background()
			now := time.Date(2026, time.July, 26, 10, 0, 0, 0, time.UTC)
			first := OnboardingSession{
				ID: "onb-first", Actor: "admin@example.test", IdempotencyKey: "onboarding-idempotency",
				Status: OnboardingStatusInProgress, CurrentStep: OnboardingStepStarted, Version: 1,
				CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour),
			}
			stored, created, err := repo.CreateOrGetOnboardingSession(ctx, first)
			if err != nil || !created || stored.ID != first.ID {
				t.Fatalf("CreateOrGetOnboardingSession() stored=%+v created=%t err=%v", stored, created, err)
			}
			replay := first
			replay.ID = "onb-must-not-win"
			stored, created, err = repo.CreateOrGetOnboardingSession(ctx, replay)
			if err != nil || created || stored.ID != first.ID {
				t.Fatalf("CreateOrGetOnboardingSession() replay=%+v created=%t err=%v", stored, created, err)
			}
			otherActor := first
			otherActor.ID = "onb-other-actor"
			otherActor.Actor = "other@example.test"
			if _, created, err = repo.CreateOrGetOnboardingSession(ctx, otherActor); err != nil || !created {
				t.Fatalf("CreateOrGetOnboardingSession(other actor) created=%t err=%v", created, err)
			}

			first.ProviderID = "provider-1"
			first.ProviderAccountID = "account-1"
			first.VerificationModel = "public-model"
			first.UpdatedAt = now.Add(time.Minute)
			updated, changed, err := repo.UpdateOnboardingSession(ctx, first, 1)
			if err != nil || !changed || updated.Version != 2 || updated.ProviderID != "provider-1" || updated.VerificationModel != "public-model" {
				t.Fatalf("UpdateOnboardingSession() updated=%+v changed=%t err=%v", updated, changed, err)
			}
			if _, changed, err = repo.UpdateOnboardingSession(ctx, first, 1); err != nil || changed {
				t.Fatalf("stale UpdateOnboardingSession() changed=%t err=%v", changed, err)
			}
			found, ok, err := repo.FindOnboardingSession(ctx, first.ID)
			if err != nil || !ok || found.Version != 2 || found.IdempotencyKey != first.IdempotencyKey {
				t.Fatalf("FindOnboardingSession() found=%+v ok=%t err=%v", found, ok, err)
			}
		})
	}
}

func TestOnboardingServiceCreatesRecoverableAPIKeyWithoutPersistingPlaintext(t *testing.T) {
	const providerSecret = "provider-secret-must-not-leak"
	const upstreamModel = "upstream-chat"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+providerSecret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": upstreamModel}}})
	}))
	defer upstream.Close()

	repo := NewMemoryRepository()
	service := NewService(repo, "/v1", "onboarding-test-encryption-key")
	ctx := context.Background()
	session, err := service.StartOnboardingSession(ctx, "admin@example.test", "first-governed-call")
	if err != nil {
		t.Fatalf("StartOnboardingSession(): %v", err)
	}
	replayed, err := service.StartOnboardingSession(ctx, "admin@example.test", "first-governed-call")
	if err != nil || replayed.ID != session.ID {
		t.Fatalf("StartOnboardingSession() replay=%+v err=%v", replayed, err)
	}
	if _, err := service.OnboardingSession(ctx, "other@example.test", session.ID); err != ErrOnboardingSessionNotFound {
		t.Fatalf("cross-actor OnboardingSession() err=%v", err)
	}

	source, err := service.ConnectOnboardingModelSource(ctx, "admin@example.test", session.ID, OnboardingModelSourceRequest{
		ProviderName: "Primary source", ProviderType: ProviderTypeOpenAICompatible, BaseURL: upstream.URL + "/v1",
		AccountName: "Primary account", AuthType: ProviderAuthAPIKey, Secret: providerSecret, UpstreamModel: upstreamModel,
	})
	if err != nil || source.Health.Status != "ok" || source.Session.CurrentStep != OnboardingStepModelSource {
		t.Fatalf("ConnectOnboardingModelSource() result=%+v err=%v", source, err)
	}
	if source.Account.SecretCiphertext == "" || source.Account.SecretCiphertext == providerSecret {
		t.Fatalf("provider secret was not encrypted: %+v", source.Account)
	}
	encodedSource, _ := json.Marshal(source)
	if strings.Contains(string(encodedSource), providerSecret) || strings.Contains(string(encodedSource), source.Account.SecretCiphertext) {
		t.Fatalf("model source response leaked secret material: %s", encodedSource)
	}
	replayedSource, err := service.ConnectOnboardingModelSource(ctx, "admin@example.test", session.ID, OnboardingModelSourceRequest{})
	if err != nil || replayedSource.Provider.ID != source.Provider.ID || replayedSource.Account.ID != source.Account.ID {
		t.Fatalf("ConnectOnboardingModelSource() replay=%+v err=%v", replayedSource, err)
	}

	published, err := service.PublishOnboardingModel(ctx, "admin@example.test", session.ID, OnboardingPublishedModelRequest{
		ModelID: "team-model", Name: "Team model", Modality: "chat", UpstreamModel: upstreamModel,
	})
	if err != nil || published.Route.UpstreamFormat != UpstreamFormatOpenAIResponses || published.Session.CurrentStep != OnboardingStepPublishedModel {
		t.Fatalf("PublishOnboardingModel() result=%+v err=%v", published, err)
	}

	created, err := service.CreateOnboardingAPIKey(ctx, "admin@example.test", session.ID, OnboardingAPIKeyRequest{
		Name: "Engineering", ConcurrencyLimit: 3, MonthlyTokenLimit: 100000,
	})
	if err != nil || created.Credential == "" || created.APIKey.ID != created.Session.APIKeyID {
		t.Fatalf("CreateOnboardingAPIKey() result=%+v err=%v", created, err)
	}
	replayedAPIKey, err := service.CreateOnboardingAPIKey(ctx, "admin@example.test", session.ID, OnboardingAPIKeyRequest{})
	if err != nil || replayedAPIKey.Credential != created.Credential || replayedAPIKey.APIKey.ID != created.APIKey.ID {
		t.Fatalf("CreateOnboardingAPIKey() replay=%+v err=%v", replayedAPIKey, err)
	}
	keys, err := repo.ListAPIKeys(ctx)
	if err != nil || len(keys) != 1 || keys[0].KeyHash == "" || keys[0].KeyHash == created.Credential {
		t.Fatalf("persisted keys=%+v err=%v", keys, err)
	}
	if err := service.OnboardingCredentialMatches(ctx, created.APIKey.ID, created.Credential); err != nil {
		t.Fatalf("OnboardingCredentialMatches(): %v", err)
	}
	if err := service.OnboardingCredentialMatches(ctx, created.APIKey.ID, "wrong-credential"); err != ErrOnboardingCredential {
		t.Fatalf("OnboardingCredentialMatches(wrong) err=%v", err)
	}

	for _, client := range []string{ClientCodex, ClientClaudeCode, ClientOpenAISDK, ClientAnthropicSDK} {
		config, err := service.APIKeyClientConfig(ctx, created.APIKey.ID, client, "team-model", "https://gateway.example.test/v1")
		if err != nil {
			t.Fatalf("APIKeyClientConfig(%s): %v", client, err)
		}
		encoded, _ := json.Marshal(config)
		if config.ContainsSecret || strings.Contains(string(encoded), created.Credential) || !strings.Contains(config.Content, "ASTERROUTER_API_KEY") {
			t.Fatalf("client config %s leaked or omitted credential reference: %s", client, encoded)
		}
	}

	completed, err := service.CompleteOnboardingVerification(ctx, "admin@example.test", session.ID, ClientVerificationResult{
		Status: "success", Client: ClientCodex, APIKeyID: created.APIKey.ID, Model: "team-model",
		HTTPStatus: http.StatusOK, OperationID: "operation-1", TraceID: "trace-1",
	})
	if err != nil || completed.Status != OnboardingStatusCompleted || completed.CurrentStep != OnboardingStepVerification || len(completed.PendingSteps) != 0 {
		t.Fatalf("CompleteOnboardingVerification() session=%+v err=%v", completed, err)
	}
	completedReplay, err := service.CreateOnboardingAPIKey(ctx, "admin@example.test", session.ID, OnboardingAPIKeyRequest{})
	if err != nil || completedReplay.Session.Status != OnboardingStatusCompleted || completedReplay.Session.CurrentStep != OnboardingStepVerification || completedReplay.Credential != created.Credential {
		t.Fatalf("completed API key replay=%+v err=%v", completedReplay, err)
	}
	audits, err := repo.ListAuditLogs(ctx, 100)
	if err != nil || len(audits) < 6 {
		t.Fatalf("audit logs=%+v err=%v", audits, err)
	}
	for _, audit := range audits {
		if strings.Contains(audit.Summary, providerSecret) || strings.Contains(audit.Summary, created.Credential) {
			t.Fatalf("audit leaked secret material: %+v", audit)
		}
	}
}

func TestOnboardingServicePersistsSafeRecoveryStateAfterSourceFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	service := NewService(NewMemoryRepository(), "/v1", "failure-test-key")
	session, err := service.StartOnboardingSession(context.Background(), "admin", "source-failure-idempotency")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ConnectOnboardingModelSource(context.Background(), "admin", session.ID, OnboardingModelSourceRequest{
		ProviderName: "Unavailable", ProviderType: ProviderTypeOpenAICompatible, BaseURL: upstream.URL + "/v1",
		AccountName: "Unavailable", AuthType: ProviderAuthAPIKey, Secret: "safe-secret", UpstreamModel: "model-a",
	})
	if err == nil || result.Session.Status != OnboardingStatusFailed || result.Session.FailureCode != "model_source_unreachable" || result.Session.ProviderID == "" || result.Session.ProviderAccountID == "" {
		t.Fatalf("failed source result=%+v err=%v", result, err)
	}
	stored, err := service.OnboardingSession(context.Background(), "admin", session.ID)
	if err != nil || stored.RecoveryHint != "check_base_url_credentials_and_upstream_model" || stored.ProviderHealthCheckID == "" {
		t.Fatalf("stored recovery session=%+v err=%v", stored, err)
	}
}

func TestAPIKeyClientConfigShellQuotesUntrustedValues(t *testing.T) {
	service := NewService(NewMemoryRepository(), "/v1", "client-config-test-secret")
	model := "model'$(printf-danger)"
	created, err := service.CreateAPIKey(context.Background(), "admin", APIKeyCreateRequest{
		Name: "Shell quoting", ModelAllowlist: []string{model},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []string{ClientClaudeCode, ClientOpenAISDK, ClientAnthropicSDK} {
		config, err := service.APIKeyClientConfig(context.Background(), created.Record.ID, client, model, "https://router.example.test/edge'$(printf-url)/v1")
		if err != nil {
			t.Fatalf("APIKeyClientConfig(%s): %v", client, err)
		}
		if !strings.Contains(config.Content, "'model'\"'\"'$(printf-danger)'") || !strings.Contains(config.Content, "'https://router.example.test/edge'\"'\"'$(printf-url)") {
			t.Fatalf("client %s content is not shell quoted:\n%s", client, config.Content)
		}
	}
}

func TestOnboardingInputErrorsAreClassified(t *testing.T) {
	service := NewService(NewMemoryRepository(), "/v1", "input-classification-secret")
	_, err := service.StartOnboardingSession(context.Background(), "admin", "short")
	if !errors.Is(err, ErrOnboardingInvalidInput) {
		t.Fatalf("StartOnboardingSession() error=%v, want ErrOnboardingInvalidInput", err)
	}
}

func TestOnboardingSourceRecoversAfterSessionUpdateConflict(t *testing.T) {
	const upstreamModel = "recovery-model"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": upstreamModel}}})
	}))
	defer upstream.Close()

	base := NewMemoryRepository()
	repo := &failFirstOnboardingUpdateRepository{Repository: base}
	service := NewService(repo, "/v1", "conflict-recovery-secret")
	ctx := context.Background()
	session, err := service.StartOnboardingSession(ctx, "admin", "conflict-recovery-session")
	if err != nil {
		t.Fatal(err)
	}
	request := OnboardingModelSourceRequest{
		ProviderName: "Recovery", ProviderType: ProviderTypeOpenAICompatible, BaseURL: upstream.URL + "/v1",
		AccountName: "Recovery", AuthType: ProviderAuthAPIKey, Secret: "recovery-secret", UpstreamModel: upstreamModel,
	}
	if _, err := service.ConnectOnboardingModelSource(ctx, "admin", session.ID, request); !errors.Is(err, ErrOnboardingSessionConflict) {
		t.Fatalf("first ConnectOnboardingModelSource() error=%v, want conflict", err)
	}
	stored, err := service.OnboardingSession(ctx, "admin", session.ID)
	if err != nil || stored.CurrentStep != OnboardingStepStarted || stored.ProviderID != "" {
		t.Fatalf("session after conflict=%+v err=%v", stored, err)
	}
	result, err := service.ConnectOnboardingModelSource(ctx, "admin", session.ID, request)
	if err != nil || result.Session.CurrentStep != OnboardingStepModelSource || result.Health.Status != "ok" {
		t.Fatalf("recovered source=%+v err=%v", result, err)
	}
	providers, _ := base.ListProviders(ctx)
	accounts, _ := base.ListProviderAccounts(ctx)
	if len(providers) != 1 || len(accounts) != 1 || providers[0].ID != result.Provider.ID || accounts[0].ID != result.Account.ID {
		t.Fatalf("recovered objects providers=%+v accounts=%+v", providers, accounts)
	}
}

func TestOnboardingSessionExpiryPreventsFurtherMutation(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, "/v1", "expiry-test-secret")
	now := time.Date(2026, time.July, 26, 8, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	session, err := service.StartOnboardingSession(context.Background(), "admin", "expiry-test-session")
	if err != nil {
		t.Fatal(err)
	}
	now = session.ExpiresAt
	_, err = service.ConnectOnboardingModelSource(context.Background(), "admin", session.ID, OnboardingModelSourceRequest{})
	if !errors.Is(err, ErrOnboardingSessionExpired) {
		t.Fatalf("ConnectOnboardingModelSource() error=%v, want expired", err)
	}
	stored, getErr := service.OnboardingSession(context.Background(), "admin", session.ID)
	providers, listErr := repo.ListProviders(context.Background())
	if getErr != nil || listErr != nil || stored.ID != session.ID || len(providers) != 0 {
		t.Fatalf("expired session stored=%+v providers=%+v get_err=%v list_err=%v", stored, providers, getErr, listErr)
	}
}

func TestOnboardingConcurrentReplayKeepsSinglePublishedModelAndAPIKey(t *testing.T) {
	const upstreamModel = "concurrent-upstream"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": upstreamModel}}})
	}))
	defer upstream.Close()

	repo := NewMemoryRepository()
	service := NewService(repo, "/v1", "concurrent-replay-secret")
	ctx := context.Background()
	session, err := service.StartOnboardingSession(ctx, "admin", "concurrent-replay-session")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ConnectOnboardingModelSource(ctx, "admin", session.ID, OnboardingModelSourceRequest{
		ProviderName: "Concurrent", ProviderType: ProviderTypeOpenAICompatible, BaseURL: upstream.URL + "/v1",
		AccountName: "Concurrent", AuthType: ProviderAuthAPIKey, Secret: "concurrent-secret", UpstreamModel: upstreamModel,
	})
	if err != nil {
		t.Fatal(err)
	}

	publishRequest := OnboardingPublishedModelRequest{ModelID: "concurrent-model", Name: "Concurrent", Modality: "chat", UpstreamModel: upstreamModel}
	publishErrors := runConcurrently(2, func() error {
		_, err := service.PublishOnboardingModel(ctx, "admin", session.ID, publishRequest)
		return err
	})
	assertOnlyConflictOrSuccess(t, publishErrors)
	published, err := service.PublishOnboardingModel(ctx, "admin", session.ID, publishRequest)
	if err != nil {
		t.Fatalf("PublishOnboardingModel() recovery: %v", err)
	}
	models, _ := repo.ListGatewayModels(ctx)
	routes, _ := repo.ListModelRoutes(ctx)
	if len(models) != 1 || len(routes) != 1 || models[0].ID != published.Model.ID || routes[0].ID != published.Route.ID {
		t.Fatalf("published objects models=%+v routes=%+v", models, routes)
	}

	apiKeyErrors := runConcurrently(2, func() error {
		_, err := service.CreateOnboardingAPIKey(ctx, "admin", session.ID, OnboardingAPIKeyRequest{Name: "Concurrent application"})
		return err
	})
	assertOnlyConflictOrSuccess(t, apiKeyErrors)
	apiKey, err := service.CreateOnboardingAPIKey(ctx, "admin", session.ID, OnboardingAPIKeyRequest{Name: "Concurrent application"})
	if err != nil {
		t.Fatalf("CreateOnboardingAPIKey() recovery: %v", err)
	}
	keys, _ := repo.ListAPIKeys(ctx)
	if len(keys) != 1 || keys[0].ID != apiKey.APIKey.ID || apiKey.Credential == "" {
		t.Fatalf("API key objects keys=%+v result=%+v", keys, apiKey)
	}
}

type failFirstOnboardingUpdateRepository struct {
	Repository
	mu     sync.Mutex
	failed bool
}

func (r *failFirstOnboardingUpdateRepository) UpdateOnboardingSession(ctx context.Context, session OnboardingSession, expectedVersion int64) (OnboardingSession, bool, error) {
	r.mu.Lock()
	if !r.failed {
		r.failed = true
		r.mu.Unlock()
		return OnboardingSession{}, false, nil
	}
	r.mu.Unlock()
	return r.Repository.UpdateOnboardingSession(ctx, session, expectedVersion)
}

func runConcurrently(count int, fn func() error) []error {
	start := make(chan struct{})
	errorsByWorker := make([]error, count)
	var wait sync.WaitGroup
	wait.Add(count)
	for index := 0; index < count; index++ {
		go func(index int) {
			defer wait.Done()
			<-start
			errorsByWorker[index] = fn()
		}(index)
	}
	close(start)
	wait.Wait()
	return errorsByWorker
}

func assertOnlyConflictOrSuccess(t *testing.T, results []error) {
	t.Helper()
	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, ErrOnboardingSessionConflict) {
			t.Fatalf("concurrent replay error=%v, want success or conflict", err)
		}
	}
	if successes == 0 {
		t.Fatal("concurrent replay had no successful request")
	}
}
