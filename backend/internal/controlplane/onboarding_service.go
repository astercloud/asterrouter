package controlplane

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

var (
	ErrOnboardingSessionNotFound = errors.New("onboarding session not found")
	ErrOnboardingSessionExpired  = errors.New("onboarding session expired")
	ErrOnboardingSessionConflict = errors.New("onboarding session changed concurrently")
	ErrOnboardingStepOrder       = errors.New("onboarding step is not ready")
	ErrOnboardingInvalidInput    = errors.New("invalid onboarding input")
	ErrOnboardingAPIKeyNotFound  = errors.New("onboarding api key not found")
	ErrOnboardingCredential      = errors.New("onboarding credential does not match api key")
)

type onboardingInputError struct {
	cause error
}

func (e onboardingInputError) Error() string { return e.cause.Error() }
func (e onboardingInputError) Unwrap() error { return ErrOnboardingInvalidInput }

func invalidOnboardingInput(err error) error {
	if err == nil {
		return nil
	}
	return onboardingInputError{cause: err}
}

func onboardingInputErrorf(format string, args ...any) error {
	return invalidOnboardingInput(fmt.Errorf(format, args...))
}

const onboardingSessionTTL = 24 * time.Hour

func (s *Service) StartOnboardingSession(ctx context.Context, actor, idempotencyKey string) (OnboardingSession, error) {
	actor = strings.TrimSpace(actor)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if actor == "" {
		return OnboardingSession{}, onboardingInputErrorf("actor is required")
	}
	if len(idempotencyKey) < 8 || len(idempotencyKey) > 128 {
		return OnboardingSession{}, onboardingInputErrorf("Idempotency-Key must contain between 8 and 128 characters")
	}
	now := s.nowUTC()
	session := OnboardingSession{
		ID: "onb_" + randomID(12), Actor: actor, IdempotencyKey: idempotencyKey,
		Status: OnboardingStatusInProgress, CurrentStep: OnboardingStepStarted, Version: 1,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(onboardingSessionTTL),
	}
	stored, created, err := s.repo.CreateOrGetOnboardingSession(ctx, session)
	if err != nil {
		return OnboardingSession{}, err
	}
	if created {
		if err := s.audit(ctx, actor, "create", "onboarding_session", stored.ID, "Started governed application onboarding"); err != nil {
			return OnboardingSession{}, err
		}
	}
	return presentOnboardingSession(stored), nil
}

func (s *Service) OnboardingSession(ctx context.Context, actor, id string) (OnboardingSession, error) {
	session, err := s.ownedOnboardingSession(ctx, actor, id)
	if err != nil {
		return OnboardingSession{}, err
	}
	return presentOnboardingSession(session), nil
}

func (s *Service) ConnectOnboardingModelSource(ctx context.Context, actor, id string, req OnboardingModelSourceRequest) (OnboardingModelSourceResult, error) {
	session, err := s.ownedActiveOnboardingSession(ctx, actor, id)
	if err != nil {
		return OnboardingModelSourceResult{}, err
	}
	if onboardingStepRank(session.CurrentStep) >= onboardingStepRank(OnboardingStepModelSource) && session.Status != OnboardingStatusFailed {
		return s.onboardingModelSourceResult(ctx, session)
	}
	providerType := strings.TrimSpace(req.ProviderType)
	if !oneOf(providerType, ProviderTypeOpenAICompatible, ProviderTypeAnthropicCompatible, ProviderTypeGeminiCompatible) {
		return OnboardingModelSourceResult{}, onboardingInputErrorf("provider_type must support automatic model discovery during onboarding")
	}
	upstreamModel := strings.TrimSpace(req.UpstreamModel)
	if upstreamModel == "" {
		return OnboardingModelSourceResult{}, onboardingInputErrorf("upstream_model is required")
	}
	if strings.TrimSpace(req.Secret) == "" {
		return OnboardingModelSourceResult{}, onboardingInputErrorf("secret is required")
	}
	provider, err := providerFromRequest(ProviderRequest{
		Name: req.ProviderName, Type: providerType, BaseURL: req.BaseURL, Status: ProviderStatusActive,
	}, s.nowUTC())
	if err != nil {
		return OnboardingModelSourceResult{}, invalidOnboardingInput(err)
	}
	provider.ID = onboardingObjectID("prov_onb_", session.ID)
	providerCreated := true
	if existing, found, findErr := s.providerByExactID(ctx, provider.ID); findErr != nil {
		return OnboardingModelSourceResult{}, findErr
	} else if found {
		providerCreated = false
		provider.CreatedAt = existing.CreatedAt
		provider.UpdatedAt = s.nowUTC()
	}
	if err := s.repo.SaveProvider(ctx, provider); err != nil {
		return OnboardingModelSourceResult{}, err
	}
	if providerCreated {
		if err := s.audit(ctx, actor, "create", "provider", provider.ID, fmt.Sprintf("Connected onboarding provider %s", provider.Name)); err != nil {
			return OnboardingModelSourceResult{}, err
		}
	}

	schedulable := true
	accountRequest := ProviderAccountRequest{
		ProviderID: provider.ID, Name: req.AccountName, Platform: provider.Type, AuthType: req.AuthType,
		AdapterConfig: cloneStringMap(req.AdapterConfig), Status: AccountStatusActive, Schedulable: &schedulable,
		Models: []string{upstreamModel}, Secret: req.Secret, Concurrency: req.Concurrency, RPMLimit: req.RPMLimit, TPMLimit: req.TPMLimit,
	}
	if strings.TrimSpace(accountRequest.Name) == "" {
		accountRequest.Name = provider.Name
	}
	if err := validateProviderAccountAdapter(provider.Type, &accountRequest); err != nil {
		return OnboardingModelSourceResult{}, invalidOnboardingInput(err)
	}
	account, err := providerAccountFromRequest(accountRequest, s.nowUTC(), true, false)
	if err != nil {
		return OnboardingModelSourceResult{}, invalidOnboardingInput(err)
	}
	account.ID = onboardingObjectID("acct_onb_", session.ID)
	account.SecretCiphertext, err = encryptSecret(s.secretKey, req.Secret)
	if err != nil {
		return OnboardingModelSourceResult{}, err
	}
	accountCreated := true
	if existing, found, findErr := s.providerAccountByExactID(ctx, account.ID); findErr != nil {
		return OnboardingModelSourceResult{}, findErr
	} else if found {
		accountCreated = false
		account.CreatedAt = existing.CreatedAt
		account.UpdatedAt = s.nowUTC()
	}
	inventory := reconcileConfiguredProviderAccountModels(account, nil, account.UpdatedAt)
	if err := s.repo.SaveProviderAccountWithModels(ctx, account, inventory); err != nil {
		return OnboardingModelSourceResult{}, err
	}
	if accountCreated {
		if err := s.audit(ctx, actor, "create", "provider_account", account.ID, fmt.Sprintf("Connected onboarding provider account %s", account.Name)); err != nil {
			return OnboardingModelSourceResult{}, err
		}
	}
	session.ProviderID = provider.ID
	session.ProviderAccountID = account.ID
	session.Status = OnboardingStatusInProgress
	session.FailureStage, session.FailureCode, session.RecoveryHint = "", "", ""
	session, err = s.saveOnboardingSession(ctx, session)
	if err != nil {
		return OnboardingModelSourceResult{}, err
	}

	health, err := s.CheckProviderAccount(ctx, actor, account.ID)
	if err != nil || health.Status != "ok" || !contains(health.Models, upstreamModel) {
		session.ProviderHealthCheckID = health.ID
		code := "model_source_unreachable"
		hint := "check_base_url_credentials_and_upstream_model"
		if err == nil && health.Status == "ok" {
			code = "upstream_model_not_discovered"
			hint = "choose_a_model_returned_by_provider_discovery"
		}
		failed, saveErr := s.failOnboardingSession(ctx, session, OnboardingStepModelSource, code, hint)
		if saveErr != nil {
			return OnboardingModelSourceResult{}, saveErr
		}
		return OnboardingModelSourceResult{Session: presentOnboardingSession(failed), Provider: provider, Account: account, Health: health}, errors.New(code)
	}
	session.ProviderHealthCheckID = health.ID
	session.CurrentStep = OnboardingStepModelSource
	session.Status = OnboardingStatusInProgress
	session, err = s.saveOnboardingSession(ctx, session)
	if err != nil {
		return OnboardingModelSourceResult{}, err
	}
	account, _ = s.providerAccountByID(ctx, account.ID)
	return OnboardingModelSourceResult{Session: presentOnboardingSession(session), Provider: provider, Account: account, Health: health}, nil
}

func (s *Service) PublishOnboardingModel(ctx context.Context, actor, id string, req OnboardingPublishedModelRequest) (OnboardingPublishedModelResult, error) {
	session, err := s.ownedActiveOnboardingSession(ctx, actor, id)
	if err != nil {
		return OnboardingPublishedModelResult{}, err
	}
	if onboardingStepRank(session.CurrentStep) < onboardingStepRank(OnboardingStepModelSource) {
		return OnboardingPublishedModelResult{}, ErrOnboardingStepOrder
	}
	if onboardingStepRank(session.CurrentStep) >= onboardingStepRank(OnboardingStepPublishedModel) && session.Status != OnboardingStatusFailed {
		return s.onboardingPublishedModelResult(ctx, session)
	}
	account, err := s.providerAccountByID(ctx, session.ProviderAccountID)
	if err != nil {
		return OnboardingPublishedModelResult{}, err
	}
	provider, err := s.providerByID(ctx, account.ProviderID)
	if err != nil {
		return OnboardingPublishedModelResult{}, err
	}
	model, err := gatewayModelFromRequest(GatewayModelRequest{
		ModelID: req.ModelID, Name: req.Name, Description: req.Description, Modality: req.Modality,
		DefaultRouteGroup: req.RouteGroup, Status: GatewayModelStatusActive,
	}, s.nowUTC())
	if err != nil {
		return OnboardingPublishedModelResult{}, invalidOnboardingInput(err)
	}
	createdModel := true
	if existing, found, findErr := s.gatewayModelByPublicID(ctx, model.ModelID); findErr != nil {
		return OnboardingPublishedModelResult{}, findErr
	} else if found {
		createdModel = false
		if existing.Modality != model.Modality {
			return OnboardingPublishedModelResult{}, onboardingInputErrorf("published model already exists with a different modality")
		}
		model = existing
	} else {
		model.ID = onboardingObjectID("gmodel_onb_", session.ID)
		if err := s.repo.SaveGatewayModel(ctx, model); err != nil {
			return OnboardingPublishedModelResult{}, err
		}
		if err := s.audit(ctx, actor, "create", "gateway_model", model.ID, fmt.Sprintf("Published onboarding model %s", model.ModelID)); err != nil {
			return OnboardingPublishedModelResult{}, err
		}
	}
	session.GatewayModelID = model.ID
	session.Status = OnboardingStatusInProgress
	session.FailureStage, session.FailureCode, session.RecoveryHint = "", "", ""
	session, err = s.saveOnboardingSession(ctx, session)
	if err != nil {
		return OnboardingPublishedModelResult{}, err
	}

	upstreamModel := strings.TrimSpace(req.UpstreamModel)
	if upstreamModel == "" && len(account.Models) > 0 {
		upstreamModel = account.Models[0]
	}
	upstreamFormat := strings.TrimSpace(req.UpstreamFormat)
	if upstreamFormat == "" {
		upstreamFormat = defaultOnboardingUpstreamFormat(provider.Type, model.Modality)
	}
	routeRequest := ModelRouteRequest{
		GatewayModelID: model.ID, RouteGroup: req.RouteGroup, ProviderAccountID: account.ID,
		UpstreamModel: upstreamModel, UpstreamFormat: upstreamFormat, Weight: 100, Status: ModelRouteStatusActive,
	}
	route, err := s.modelRouteFromRequest(ctx, routeRequest, s.nowUTC())
	if err != nil {
		failed, saveErr := s.failOnboardingSession(ctx, session, OnboardingStepPublishedModel, "model_route_invalid", "check_model_protocol_and_source_mapping")
		if saveErr != nil {
			return OnboardingPublishedModelResult{}, saveErr
		}
		return OnboardingPublishedModelResult{Session: presentOnboardingSession(failed), Model: model}, err
	}
	createdRoute := true
	if existing, found, findErr := s.matchingModelRoute(ctx, route); findErr != nil {
		return OnboardingPublishedModelResult{}, findErr
	} else if found {
		createdRoute = false
		route = existing
	} else {
		route.ID = onboardingObjectID("mroute_onb_", session.ID)
		if err := s.repo.SaveModelRoute(ctx, route); err != nil {
			return OnboardingPublishedModelResult{}, err
		}
	}
	if createdRoute {
		if err := s.audit(ctx, actor, "create", "model_route", route.ID, fmt.Sprintf("Connected onboarding route for %s", model.ModelID)); err != nil {
			return OnboardingPublishedModelResult{}, err
		}
	}
	if !createdModel {
		model, _ = s.gatewayModelByID(ctx, model.ID)
	}
	session.ModelRouteID = route.ID
	session.CurrentStep = OnboardingStepPublishedModel
	session.Status = OnboardingStatusInProgress
	session, err = s.saveOnboardingSession(ctx, session)
	if err != nil {
		return OnboardingPublishedModelResult{}, err
	}
	return OnboardingPublishedModelResult{Session: presentOnboardingSession(session), Model: model, Route: route}, nil
}

func (s *Service) CreateOnboardingAPIKey(ctx context.Context, actor, id string, req OnboardingAPIKeyRequest) (OnboardingAPIKeyResult, error) {
	session, err := s.ownedActiveOnboardingSession(ctx, actor, id)
	if err != nil {
		return OnboardingAPIKeyResult{}, err
	}
	if onboardingStepRank(session.CurrentStep) < onboardingStepRank(OnboardingStepPublishedModel) {
		return OnboardingAPIKeyResult{}, ErrOnboardingStepOrder
	}
	if err := validateOnboardingAPIKeyRequest(req); err != nil {
		return OnboardingAPIKeyResult{}, invalidOnboardingInput(err)
	}
	model, err := s.gatewayModelByID(ctx, session.GatewayModelID)
	if err != nil {
		return OnboardingAPIKeyResult{}, err
	}
	rawKey := s.onboardingCredential(session.ID)
	keyID := onboardingObjectID("key_onb_", session.ID)
	if existing, found, findErr := s.apiKeyByExactID(ctx, keyID); findErr != nil {
		return OnboardingAPIKeyResult{}, findErr
	} else if found {
		if session.APIKeyID != existing.ID || onboardingStepRank(session.CurrentStep) < onboardingStepRank(OnboardingStepAPIKey) {
			session.APIKeyID = existing.ID
			session.CurrentStep, session.Status = OnboardingStepAPIKey, OnboardingStatusInProgress
			session.FailureStage, session.FailureCode, session.RecoveryHint = "", "", ""
			session, err = s.saveOnboardingSession(ctx, session)
			if err != nil {
				return OnboardingAPIKeyResult{}, err
			}
		}
		return OnboardingAPIKeyResult{Session: presentOnboardingSession(session), APIKey: presentAPIKeys([]APIKeyRecord{existing}, s.nowUTC())[0], Credential: rawKey}, nil
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "First governed application"
	}
	keyRequest := APIKeyCreateRequest{
		Name: name, ModelAllowlist: []string{model.ModelID},
		Scopes:            []string{GatewayScopeInvoke, GatewayScopeModelsRead},
		AllowedModalities: []string{GatewayModalityMetadata, GatewayModalityText},
		AllowedOperations: []string{GatewayOperationListModels, GatewayOperationChatCompletion, GatewayOperationCountTokens},
		QPSLimit:          req.QPSLimit, RPMLimit: req.RPMLimit, TPMLimit: req.TPMLimit,
		ConcurrencyLimit: req.ConcurrencyLimit, MonthlyTokenLimit: req.MonthlyTokenLimit, MonthlyBudgetMicros: req.MonthlyBudgetMicros,
		KeyType: APIKeyTypeWorkspace,
	}
	record, err := s.buildAPIKeyRecord(ctx, keyRequest, nil, keyID, rawKey, onboardingObjectID("key_family_onb_", session.ID), s.nowUTC())
	if err != nil {
		return OnboardingAPIKeyResult{}, err
	}
	if err := s.repo.SaveAPIKey(ctx, record); err != nil {
		return OnboardingAPIKeyResult{}, err
	}
	if err := s.audit(ctx, actor, "create", "api_key", record.ID, fmt.Sprintf("Created onboarding API key %s", record.Name)); err != nil {
		return OnboardingAPIKeyResult{}, err
	}
	session.APIKeyID = record.ID
	session.CurrentStep, session.Status = OnboardingStepAPIKey, OnboardingStatusInProgress
	session.FailureStage, session.FailureCode, session.RecoveryHint = "", "", ""
	session, err = s.saveOnboardingSession(ctx, session)
	if err != nil {
		return OnboardingAPIKeyResult{}, err
	}
	return OnboardingAPIKeyResult{Session: presentOnboardingSession(session), APIKey: presentAPIKeys([]APIKeyRecord{record}, s.nowUTC())[0], Credential: rawKey}, nil
}

func (s *Service) OnboardingAPIKey(ctx context.Context, id string) (APIKeyRecord, error) {
	key, found, err := s.apiKeyByExactID(ctx, strings.TrimSpace(id))
	if err != nil {
		return APIKeyRecord{}, err
	}
	if !found {
		return APIKeyRecord{}, ErrOnboardingAPIKeyNotFound
	}
	return presentAPIKeys([]APIKeyRecord{key}, s.nowUTC())[0], nil
}

func (s *Service) OnboardingCredentialMatches(ctx context.Context, apiKeyID, rawCredential string) error {
	auth, err := s.authenticateGatewayKey(ctx, rawCredential, false)
	if err != nil {
		if errors.Is(err, ErrGatewayUnauthorized) {
			return ErrOnboardingCredential
		}
		return err
	}
	if auth.APIKey.ID != strings.TrimSpace(apiKeyID) {
		return ErrOnboardingCredential
	}
	return nil
}

func (s *Service) APIKeyClientConfig(ctx context.Context, apiKeyID, client, model, gatewayURL string) (APIKeyClientConfig, error) {
	key, found, err := s.apiKeyByExactID(ctx, strings.TrimSpace(apiKeyID))
	if err != nil {
		return APIKeyClientConfig{}, err
	}
	if !found {
		return APIKeyClientConfig{}, ErrOnboardingAPIKeyNotFound
	}
	client = strings.TrimSpace(client)
	if !validOnboardingClient(client) {
		return APIKeyClientConfig{}, onboardingInputErrorf("client is not supported")
	}
	model = strings.TrimSpace(model)
	if model == "" && len(key.ModelAllowlist) > 0 {
		model = key.ModelAllowlist[0]
	}
	if model == "" || !contains(key.ModelAllowlist, model) {
		return APIKeyClientConfig{}, ErrGatewayForbidden
	}
	gatewayURL = strings.TrimRight(strings.TrimSpace(gatewayURL), "/")
	if !validHTTPURL(gatewayURL) {
		return APIKeyClientConfig{}, errors.New("gateway URL must be an absolute http or https URL")
	}
	config := APIKeyClientConfig{
		APIKeyID: key.ID, Client: client, Model: model, GatewayURL: gatewayURL,
		CredentialEnv: "ASTERROUTER_API_KEY", Environment: map[string]string{}, ContainsSecret: false,
		VerificationPath:     "/api/v1/console/api-keys/" + key.ID + "/client-verifications",
		RecoveryInstructions: []string{"restore_previous_configuration", "unset_asterrouter_environment_variables"},
	}
	shellGateway, shellModel := shellQuote(gatewayURL), shellQuote(model)
	switch client {
	case ClientCodex:
		config.Format = "toml"
		config.FilePath = "~/.codex/config.toml"
		config.Environment = map[string]string{"ASTERROUTER_API_KEY": "<one-time-credential>"}
		content, marshalErr := toml.Marshal(struct {
			ModelProvider  string                        `toml:"model_provider"`
			Model          string                        `toml:"model"`
			ModelProviders map[string]codexModelProvider `toml:"model_providers"`
		}{
			ModelProvider: "asterrouter",
			Model:         model,
			ModelProviders: map[string]codexModelProvider{
				"asterrouter": {
					Name: "AsterRouter", BaseURL: gatewayURL, EnvKey: "ASTERROUTER_API_KEY", WireAPI: "responses",
				},
			},
		})
		if marshalErr != nil {
			return APIKeyClientConfig{}, fmt.Errorf("encode client configuration: %w", marshalErr)
		}
		config.Content = string(content)
	case ClientClaudeCode:
		anthropicBaseURL := strings.TrimSuffix(gatewayURL, "/v1")
		config.Format = "shell"
		config.Environment = map[string]string{
			"ANTHROPIC_API_KEY": "$ASTERROUTER_API_KEY", "ANTHROPIC_BASE_URL": anthropicBaseURL, "ANTHROPIC_MODEL": model,
		}
		config.Content = fmt.Sprintf("export ANTHROPIC_API_KEY=\"$ASTERROUTER_API_KEY\"\nexport ANTHROPIC_BASE_URL=%s\nexport ANTHROPIC_MODEL=%s\n", shellQuote(anthropicBaseURL), shellModel)
	case ClientOpenAISDK:
		config.Format = "shell"
		config.Environment = map[string]string{
			"OPENAI_API_KEY": "$ASTERROUTER_API_KEY", "OPENAI_BASE_URL": gatewayURL, "OPENAI_MODEL": model,
		}
		config.Content = fmt.Sprintf("export OPENAI_API_KEY=\"$ASTERROUTER_API_KEY\"\nexport OPENAI_BASE_URL=%s\nexport OPENAI_MODEL=%s\n", shellGateway, shellModel)
	case ClientAnthropicSDK:
		anthropicBaseURL := strings.TrimSuffix(gatewayURL, "/v1")
		config.Format = "shell"
		config.Environment = map[string]string{
			"ANTHROPIC_API_KEY": "$ASTERROUTER_API_KEY", "ANTHROPIC_BASE_URL": anthropicBaseURL, "ANTHROPIC_MODEL": model,
		}
		config.Content = fmt.Sprintf("export ANTHROPIC_API_KEY=\"$ASTERROUTER_API_KEY\"\nexport ANTHROPIC_BASE_URL=%s\nexport ANTHROPIC_MODEL=%s\n", shellQuote(anthropicBaseURL), shellModel)
	}
	return config, nil
}

type codexModelProvider struct {
	Name    string `toml:"name"`
	BaseURL string `toml:"base_url"`
	EnvKey  string `toml:"env_key"`
	WireAPI string `toml:"wire_api"`
}

func (s *Service) CompleteOnboardingVerification(ctx context.Context, actor, id string, result ClientVerificationResult) (OnboardingSession, error) {
	session, err := s.ownedActiveOnboardingSession(ctx, actor, id)
	if err != nil {
		return OnboardingSession{}, err
	}
	if onboardingStepRank(session.CurrentStep) < onboardingStepRank(OnboardingStepAPIKey) || session.APIKeyID != result.APIKeyID {
		return OnboardingSession{}, ErrOnboardingStepOrder
	}
	session.VerificationClient = result.Client
	session.VerificationModel = result.Model
	session.VerificationOperationID = result.OperationID
	session.VerificationTraceID = result.TraceID
	session.VerificationHTTPStatus = result.HTTPStatus
	session.VerificationErrorCode = result.ErrorCode
	session.VerificationRecoveryAction = result.RecoveryAction
	if result.Status == "success" {
		session.Status = OnboardingStatusCompleted
		session.CurrentStep = OnboardingStepVerification
		session.FailureStage, session.FailureCode, session.RecoveryHint = "", "", ""
	} else {
		session.Status = OnboardingStatusFailed
		session.FailureStage = OnboardingStepVerification
		session.FailureCode = result.ErrorCode
		session.RecoveryHint = result.RecoveryAction
	}
	session, err = s.saveOnboardingSession(ctx, session)
	if err != nil {
		return OnboardingSession{}, err
	}
	if err := s.RecordAPIKeyVerification(ctx, actor, result); err != nil {
		return OnboardingSession{}, err
	}
	return presentOnboardingSession(session), nil
}

func (s *Service) RecordAPIKeyVerification(ctx context.Context, actor string, result ClientVerificationResult) error {
	status := result.Status
	if status != "success" {
		status = "failed"
	}
	return s.audit(ctx, actor, "verify", "api_key", result.APIKeyID, fmt.Sprintf("Client verification %s for %s via %s", status, result.Model, result.Client))
}

func (s *Service) ownedOnboardingSession(ctx context.Context, actor, id string) (OnboardingSession, error) {
	session, found, err := s.repo.FindOnboardingSession(ctx, strings.TrimSpace(id))
	if err != nil {
		return OnboardingSession{}, err
	}
	if !found || session.Actor != strings.TrimSpace(actor) {
		return OnboardingSession{}, ErrOnboardingSessionNotFound
	}
	return session, nil
}

func (s *Service) ownedActiveOnboardingSession(ctx context.Context, actor, id string) (OnboardingSession, error) {
	session, err := s.ownedOnboardingSession(ctx, actor, id)
	if err != nil {
		return OnboardingSession{}, err
	}
	if !s.nowUTC().Before(session.ExpiresAt) {
		return OnboardingSession{}, ErrOnboardingSessionExpired
	}
	return session, nil
}

func (s *Service) saveOnboardingSession(ctx context.Context, session OnboardingSession) (OnboardingSession, error) {
	expectedVersion := session.Version
	session.UpdatedAt = s.nowUTC()
	updated, ok, err := s.repo.UpdateOnboardingSession(ctx, session, expectedVersion)
	if err != nil {
		return OnboardingSession{}, err
	}
	if !ok {
		return OnboardingSession{}, ErrOnboardingSessionConflict
	}
	return updated, nil
}

func (s *Service) failOnboardingSession(ctx context.Context, session OnboardingSession, stage, code, hint string) (OnboardingSession, error) {
	session.Status = OnboardingStatusFailed
	session.FailureStage = stage
	session.FailureCode = code
	session.RecoveryHint = hint
	return s.saveOnboardingSession(ctx, session)
}

func (s *Service) onboardingModelSourceResult(ctx context.Context, session OnboardingSession) (OnboardingModelSourceResult, error) {
	provider, err := s.providerByID(ctx, session.ProviderID)
	if err != nil {
		return OnboardingModelSourceResult{}, err
	}
	account, err := s.providerAccountByID(ctx, session.ProviderAccountID)
	if err != nil {
		return OnboardingModelSourceResult{}, err
	}
	healthChecks, err := s.repo.ListLatestProviderAccountHealthChecks(ctx)
	if err != nil {
		return OnboardingModelSourceResult{}, err
	}
	var health ProviderAccountHealthCheck
	for _, check := range healthChecks {
		if check.AccountID == account.ID {
			health = check
			break
		}
	}
	return OnboardingModelSourceResult{Session: presentOnboardingSession(session), Provider: provider, Account: account, Health: health}, nil
}

func (s *Service) onboardingPublishedModelResult(ctx context.Context, session OnboardingSession) (OnboardingPublishedModelResult, error) {
	model, err := s.gatewayModelByID(ctx, session.GatewayModelID)
	if err != nil {
		return OnboardingPublishedModelResult{}, err
	}
	routes, err := s.repo.ListModelRoutes(ctx)
	if err != nil {
		return OnboardingPublishedModelResult{}, err
	}
	for _, route := range routes {
		if route.ID == session.ModelRouteID {
			return OnboardingPublishedModelResult{Session: presentOnboardingSession(session), Model: model, Route: route}, nil
		}
	}
	return OnboardingPublishedModelResult{}, errors.New("onboarding model route not found")
}

func (s *Service) providerByExactID(ctx context.Context, id string) (ProviderConnection, bool, error) {
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return ProviderConnection{}, false, err
	}
	for _, provider := range providers {
		if provider.ID == id {
			return provider, true, nil
		}
	}
	return ProviderConnection{}, false, nil
}

func (s *Service) providerAccountByExactID(ctx context.Context, id string) (ProviderAccount, bool, error) {
	accounts, err := s.repo.ListProviderAccounts(ctx)
	if err != nil {
		return ProviderAccount{}, false, err
	}
	for _, account := range accounts {
		if account.ID == id {
			return account, true, nil
		}
	}
	return ProviderAccount{}, false, nil
}

func (s *Service) gatewayModelByPublicID(ctx context.Context, modelID string) (GatewayModel, bool, error) {
	models, err := s.repo.ListGatewayModels(ctx)
	if err != nil {
		return GatewayModel{}, false, err
	}
	for _, model := range models {
		if model.ModelID == modelID {
			return model, true, nil
		}
	}
	return GatewayModel{}, false, nil
}

func (s *Service) matchingModelRoute(ctx context.Context, candidate ModelRoute) (ModelRoute, bool, error) {
	routes, err := s.repo.ListModelRoutes(ctx)
	if err != nil {
		return ModelRoute{}, false, err
	}
	key := modelRouteUniqueKey(candidate)
	for _, route := range routes {
		if modelRouteUniqueKey(route) == key {
			return route, true, nil
		}
	}
	return ModelRoute{}, false, nil
}

func (s *Service) apiKeyByExactID(ctx context.Context, id string) (APIKeyRecord, bool, error) {
	keys, err := s.repo.ListAPIKeys(ctx)
	if err != nil {
		return APIKeyRecord{}, false, err
	}
	for _, key := range keys {
		if key.ID == id {
			return key, true, nil
		}
	}
	return APIKeyRecord{}, false, nil
}

func (s *Service) onboardingCredential(sessionID string) string {
	mac := hmac.New(sha256.New, []byte(s.secretKey))
	_, _ = mac.Write([]byte("onboarding-credential:v1:" + sessionID))
	return "ar_" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func onboardingObjectID(prefix, sessionID string) string {
	sum := sha256.Sum256([]byte(prefix + ":" + sessionID))
	return prefix + hex.EncodeToString(sum[:8])
}

func onboardingStepRank(step string) int {
	switch step {
	case OnboardingStepStarted:
		return 0
	case OnboardingStepModelSource:
		return 1
	case OnboardingStepPublishedModel:
		return 2
	case OnboardingStepAPIKey:
		return 3
	case OnboardingStepVerification:
		return 4
	default:
		return -1
	}
}

func presentOnboardingSession(session OnboardingSession) OnboardingSession {
	steps := []string{OnboardingStepModelSource, OnboardingStepPublishedModel, OnboardingStepAPIKey, OnboardingStepVerification}
	rank := onboardingStepRank(session.CurrentStep)
	session.CompletedSteps = make([]string, 0, len(steps))
	session.PendingSteps = make([]string, 0, len(steps))
	for index, step := range steps {
		if index+1 <= rank {
			session.CompletedSteps = append(session.CompletedSteps, step)
		} else {
			session.PendingSteps = append(session.PendingSteps, step)
		}
	}
	return session
}

func defaultOnboardingUpstreamFormat(providerType, modality string) string {
	if modality == GatewayModalityEmbedding {
		return UpstreamFormatOpenAIEmbeddings
	}
	switch providerType {
	case ProviderTypeAnthropicCompatible:
		return UpstreamFormatAnthropic
	case ProviderTypeGeminiCompatible:
		return UpstreamFormatGemini
	default:
		return UpstreamFormatOpenAIResponses
	}
}

func validOnboardingClient(client string) bool {
	return oneOf(client, ClientCodex, ClientClaudeCode, ClientOpenAISDK, ClientAnthropicSDK)
}

func validateOnboardingAPIKeyRequest(req OnboardingAPIKeyRequest) error {
	if req.QPSLimit < 0 || req.RPMLimit < 0 || req.TPMLimit < 0 || req.ConcurrencyLimit < 0 || req.MonthlyTokenLimit < 0 || req.MonthlyBudgetMicros < 0 {
		return errors.New("API key limits must be greater than or equal to 0")
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
