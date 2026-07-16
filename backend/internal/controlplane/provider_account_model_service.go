package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type providerModelDiscoveryAdapter interface {
	Discover(context.Context, ProviderConnection, ProviderAccount, string) ([]string, error)
}

type openAICompatibleModelDiscoveryAdapter struct{}

type anthropicModelDiscoveryAdapter struct{}

type geminiModelDiscoveryAdapter struct{}

func (openAICompatibleModelDiscoveryAdapter) Discover(ctx context.Context, provider ProviderConnection, _ ProviderAccount, secret string) ([]string, error) {
	models, message, err := probeOpenAICompatibleModelsWithKey(ctx, provider.BaseURL, secret, "Provider account")
	if err != nil {
		return nil, errors.New(message)
	}
	if len(models) == 0 {
		return nil, errors.New("provider account /models endpoint responded without models")
	}
	return models, nil
}

func (anthropicModelDiscoveryAdapter) Discover(ctx context.Context, provider ProviderConnection, _ ProviderAccount, secret string) ([]string, error) {
	var models []string
	afterID := ""
	for page := 0; page < 100; page++ {
		endpoint, err := modelDiscoveryURL(provider.BaseURL, map[string]string{"limit": "1000", "after_id": afterID})
		if err != nil {
			return nil, err
		}
		body, err := requestModelDiscoveryPage(ctx, endpoint, map[string]string{
			"x-api-key":         secret,
			"anthropic-version": "2023-06-01",
		})
		if err != nil {
			return nil, fmt.Errorf("Anthropic /models discovery failed: %w", err)
		}
		var payload struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, errors.New("Anthropic /models response is not a supported model list")
		}
		for _, item := range payload.Data {
			models = append(models, item.ID)
		}
		if !payload.HasMore {
			return requireDiscoveredModels(models, "Anthropic")
		}
		afterID = strings.TrimSpace(payload.LastID)
		if afterID == "" {
			return nil, errors.New("Anthropic /models pagination did not return last_id")
		}
	}
	return nil, errors.New("Anthropic /models pagination exceeded 100 pages")
}

func (geminiModelDiscoveryAdapter) Discover(ctx context.Context, provider ProviderConnection, _ ProviderAccount, secret string) ([]string, error) {
	var models []string
	pageToken := ""
	for page := 0; page < 100; page++ {
		endpoint, err := modelDiscoveryURL(provider.BaseURL, map[string]string{"pageSize": "1000", "pageToken": pageToken})
		if err != nil {
			return nil, err
		}
		body, err := requestModelDiscoveryPage(ctx, endpoint, map[string]string{"x-goog-api-key": secret})
		if err != nil {
			return nil, fmt.Errorf("Gemini /models discovery failed: %w", err)
		}
		var payload struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, errors.New("Gemini /models response is not a supported model list")
		}
		for _, item := range payload.Models {
			models = append(models, strings.TrimPrefix(strings.TrimSpace(item.Name), "models/"))
		}
		pageToken = strings.TrimSpace(payload.NextPageToken)
		if pageToken == "" {
			return requireDiscoveredModels(models, "Gemini")
		}
	}
	return nil, errors.New("Gemini /models pagination exceeded 100 pages")
}

func modelDiscoveryURL(baseURL string, query map[string]string) (string, error) {
	endpoint, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/models")
	if err != nil {
		return "", errors.New("provider /models URL is invalid")
	}
	values := endpoint.Query()
	for key, value := range query {
		if value != "" {
			values.Set(key, value)
		}
	}
	endpoint.RawQuery = values.Encode()
	return endpoint.String(), nil
}

func requestModelDiscoveryPage(ctx context.Context, endpoint string, headers map[string]string) ([]byte, error) {
	requestCtx, cancel := context.WithTimeout(ctx, providerProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("provider /models request cannot be created")
	}
	req.Header.Set("Accept", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	client := &http.Client{
		Timeout: providerProbeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("provider model discovery redirects are not allowed")
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, providerProbeBodyLimit+1))
	if err != nil {
		return nil, errors.New("provider /models response cannot be read")
	}
	if len(body) > providerProbeBodyLimit {
		return nil, errors.New("provider /models response is too large")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("provider /models returned HTTP %d", resp.StatusCode)
	}
	return body, nil
}

func requireDiscoveredModels(models []string, providerName string) ([]string, error) {
	models = cleanStringList(models)
	if len(models) == 0 {
		return nil, fmt.Errorf("%s /models endpoint responded without models", providerName)
	}
	return models, nil
}

func providerModelDiscoveryAdapterFor(providerType string) (providerModelDiscoveryAdapter, bool) {
	switch strings.ToLower(strings.TrimSpace(providerType)) {
	case "openai_compatible", "self_hosted":
		return openAICompatibleModelDiscoveryAdapter{}, true
	case "anthropic":
		return anthropicModelDiscoveryAdapter{}, true
	case "gemini":
		return geminiModelDiscoveryAdapter{}, true
	default:
		return nil, false
	}
}

func (s *Service) GetProviderAccountModelInventory(ctx context.Context, id string) (ProviderAccountModelInventory, error) {
	account, err := s.providerAccountByID(ctx, id)
	if err != nil {
		return ProviderAccountModelInventory{}, err
	}
	models, err := s.providerAccountModels(ctx, account)
	if err != nil {
		return ProviderAccountModelInventory{}, err
	}
	models, err = s.withProviderAccountModelRouteCounts(ctx, models)
	if err != nil {
		return ProviderAccountModelInventory{}, err
	}
	return providerAccountModelInventory(account, models), nil
}

func (s *Service) DiscoverProviderAccountModels(ctx context.Context, actor, id string) (ProviderAccountModelDiscovery, error) {
	account, provider, discovered, err := s.discoverProviderAccountModels(ctx, id)
	if err != nil {
		return ProviderAccountModelDiscovery{}, err
	}
	existing, err := s.providerAccountModels(ctx, account)
	if err != nil {
		return ProviderAccountModelDiscovery{}, err
	}
	enabled := append([]string(nil), account.Models...)
	if account.AutoEnableNewModels {
		enabled = mergeStringLists(enabled, discovered)
	}
	models, discovery := buildProviderAccountModelDiscovery(account, existing, discovered, enabled, time.Now().UTC())
	models, err = s.withProviderAccountModelRouteCounts(ctx, models)
	if err != nil {
		return ProviderAccountModelDiscovery{}, err
	}
	discovery.Models = models
	discovery.AffectedRouteIDs, err = s.affectedModelRouteIDs(ctx, account.ID, discovery.MissingModels)
	if err != nil {
		return ProviderAccountModelDiscovery{}, err
	}
	if err := s.audit(ctx, actor, "discover", "provider_account_models", account.ID, fmt.Sprintf("Discovered %d upstream models for %s via %s", len(discovered), account.Name, provider.Type)); err != nil {
		return ProviderAccountModelDiscovery{}, err
	}
	return discovery, nil
}

func (s *Service) SyncProviderAccountModels(ctx context.Context, actor, id string, req ProviderAccountModelSyncRequest) (ProviderAccountModelSyncResult, error) {
	account, _, discovered, err := s.discoverProviderAccountModels(ctx, id)
	if err != nil {
		return ProviderAccountModelSyncResult{}, err
	}
	enabled := cleanStringList(req.EnabledModels)
	if req.AutoEnableNewModels {
		enabled = mergeStringLists(enabled, discovered)
	}
	existing, err := s.providerAccountModels(ctx, account)
	if err != nil {
		return ProviderAccountModelSyncResult{}, err
	}
	now := time.Now().UTC()
	models, discovery := buildProviderAccountModelDiscovery(account, existing, discovered, enabled, now)
	account.Models = enabled
	account.AutoEnableNewModels = req.AutoEnableNewModels
	account.UpdatedAt = now
	if err := s.repo.SaveProviderAccountWithModels(ctx, account, models); err != nil {
		return ProviderAccountModelSyncResult{}, err
	}
	models, err = s.withProviderAccountModelRouteCounts(ctx, models)
	if err != nil {
		return ProviderAccountModelSyncResult{}, err
	}
	discovery.Models = models
	discovery.AffectedRouteIDs, err = s.affectedModelRouteIDs(ctx, account.ID, discovery.MissingModels)
	if err != nil {
		return ProviderAccountModelSyncResult{}, err
	}
	if err := s.audit(ctx, actor, "sync", "provider_account_models", account.ID, fmt.Sprintf("Synchronized %d upstream models for %s; %d enabled", len(discovered), account.Name, len(enabled))); err != nil {
		return ProviderAccountModelSyncResult{}, err
	}
	return ProviderAccountModelSyncResult{
		Account:   account,
		Inventory: providerAccountModelInventory(account, models),
		Discovery: discovery,
	}, nil
}

func (s *Service) discoverProviderAccountModels(ctx context.Context, id string) (ProviderAccount, ProviderConnection, []string, error) {
	account, err := s.providerAccountByID(ctx, id)
	if err != nil {
		return ProviderAccount{}, ProviderConnection{}, nil, err
	}
	provider, err := s.providerByID(ctx, account.ProviderID)
	if err != nil {
		return ProviderAccount{}, ProviderConnection{}, nil, err
	}
	if account.AuthType != "api_key" {
		return ProviderAccount{}, ProviderConnection{}, nil, fmt.Errorf("provider account auth type %s does not support model discovery", account.AuthType)
	}
	adapter, ok := providerModelDiscoveryAdapterFor(provider.Type)
	if !ok {
		return ProviderAccount{}, ProviderConnection{}, nil, fmt.Errorf("provider type %s does not support model discovery", provider.Type)
	}
	secret, err := decryptSecret(s.secretKey, account.SecretCiphertext)
	if err != nil {
		return ProviderAccount{}, ProviderConnection{}, nil, errors.New("provider account secret cannot be decrypted")
	}
	discovered, err := adapter.Discover(ctx, provider, account, secret)
	if err != nil {
		return ProviderAccount{}, ProviderConnection{}, nil, err
	}
	return account, provider, cleanStringList(discovered), nil
}

func (s *Service) providerAccountModels(ctx context.Context, account ProviderAccount) ([]ProviderAccountModel, error) {
	models, err := s.repo.ListProviderAccountModels(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if len(models) > 0 {
		return models, nil
	}
	seeded := make([]ProviderAccountModel, 0, len(account.Models))
	for _, modelID := range account.Models {
		seeded = append(seeded, ProviderAccountModel{
			ProviderAccountID: account.ID,
			ModelID:           modelID,
			Source:            ProviderAccountModelSourceManual,
			Enabled:           true,
			Availability:      ProviderAccountModelAvailabilityUnverified,
			FirstSeenAt:       account.CreatedAt,
			UpdatedAt:         account.UpdatedAt,
		})
	}
	return seeded, nil
}

func reconcileConfiguredProviderAccountModels(account ProviderAccount, existing []ProviderAccountModel, now time.Time) []ProviderAccountModel {
	byID := make(map[string]ProviderAccountModel, len(existing)+len(account.Models))
	for _, model := range existing {
		model.Enabled = false
		model.UpdatedAt = now
		byID[model.ModelID] = model
	}
	for _, modelID := range account.Models {
		model, ok := byID[modelID]
		if !ok {
			model = ProviderAccountModel{
				ProviderAccountID: account.ID,
				ModelID:           modelID,
				Source:            ProviderAccountModelSourceManual,
				Availability:      ProviderAccountModelAvailabilityUnverified,
				FirstSeenAt:       now,
			}
		}
		model.Enabled = true
		model.UpdatedAt = now
		byID[modelID] = model
	}
	return sortedProviderAccountModels(byID)
}

func buildProviderAccountModelDiscovery(account ProviderAccount, existing []ProviderAccountModel, discovered, enabled []string, now time.Time) ([]ProviderAccountModel, ProviderAccountModelDiscovery) {
	existingByID := make(map[string]ProviderAccountModel, len(existing))
	allIDs := make(map[string]struct{}, len(existing)+len(discovered)+len(enabled))
	for _, model := range existing {
		existingByID[model.ModelID] = model
		allIDs[model.ModelID] = struct{}{}
	}
	discoveredSet := stringSet(discovered)
	enabledSet := stringSet(enabled)
	for modelID := range discoveredSet {
		allIDs[modelID] = struct{}{}
	}
	for modelID := range enabledSet {
		allIDs[modelID] = struct{}{}
	}
	modelsByID := make(map[string]ProviderAccountModel, len(allIDs))
	discovery := ProviderAccountModelDiscovery{AccountID: account.ID, DiscoveredAt: now}
	for modelID := range allIDs {
		model, existed := existingByID[modelID]
		if !existed {
			model = ProviderAccountModel{ProviderAccountID: account.ID, ModelID: modelID, FirstSeenAt: now}
		}
		model.Enabled = enabledSet[modelID]
		model.UpdatedAt = now
		if discoveredSet[modelID] {
			seenAt := now
			model.Source = ProviderAccountModelSourceDiscovered
			model.Availability = ProviderAccountModelAvailabilityAvailable
			model.LastSeenAt = &seenAt
			if existed {
				model.Change = ProviderAccountModelChangeUnchanged
				discovery.UnchangedModels = append(discovery.UnchangedModels, modelID)
			} else {
				model.Change = ProviderAccountModelChangeAdded
				discovery.AddedModels = append(discovery.AddedModels, modelID)
			}
		} else if model.Source == ProviderAccountModelSourceDiscovered {
			model.Availability = ProviderAccountModelAvailabilityMissing
			model.Change = ProviderAccountModelChangeMissing
			discovery.MissingModels = append(discovery.MissingModels, modelID)
		} else {
			model.Source = ProviderAccountModelSourceManual
			model.Availability = ProviderAccountModelAvailabilityUnverified
			model.Change = ProviderAccountModelChangeUnchanged
			discovery.UnchangedModels = append(discovery.UnchangedModels, modelID)
		}
		modelsByID[modelID] = model
	}
	sort.Strings(discovery.AddedModels)
	sort.Strings(discovery.MissingModels)
	sort.Strings(discovery.UnchangedModels)
	models := sortedProviderAccountModels(modelsByID)
	discovery.Models = models
	return models, discovery
}

func (s *Service) withProviderAccountModelRouteCounts(ctx context.Context, models []ProviderAccountModel) ([]ProviderAccountModel, error) {
	routes, err := s.repo.ListModelRoutes(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, route := range routes {
		if route.Status == ModelRouteStatusActive {
			counts[route.ProviderAccountID+"\x00"+route.UpstreamModel]++
		}
	}
	for index := range models {
		models[index].RouteCount = counts[models[index].ProviderAccountID+"\x00"+models[index].ModelID]
	}
	return models, nil
}

func (s *Service) affectedModelRouteIDs(ctx context.Context, accountID string, missingModels []string) ([]string, error) {
	missing := stringSet(missingModels)
	routes, err := s.repo.ListModelRoutes(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, route := range routes {
		if route.ProviderAccountID == accountID && route.Status == ModelRouteStatusActive && missing[route.UpstreamModel] {
			ids = append(ids, route.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func providerAccountModelInventory(account ProviderAccount, models []ProviderAccountModel) ProviderAccountModelInventory {
	var lastDiscoveredAt *time.Time
	for _, model := range models {
		if model.LastSeenAt != nil && (lastDiscoveredAt == nil || model.LastSeenAt.After(*lastDiscoveredAt)) {
			value := *model.LastSeenAt
			lastDiscoveredAt = &value
		}
	}
	return ProviderAccountModelInventory{
		AccountID:           account.ID,
		AutoEnableNewModels: account.AutoEnableNewModels,
		LastDiscoveredAt:    lastDiscoveredAt,
		Models:              models,
	}
}

func sortedProviderAccountModels(models map[string]ProviderAccountModel) []ProviderAccountModel {
	out := make([]ProviderAccountModel, 0, len(models))
	for _, model := range models {
		out = append(out, model)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModelID < out[j].ModelID })
	return out
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}
