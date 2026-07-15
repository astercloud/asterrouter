package controlplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	ProviderBillingAdapterAuto              = "auto"
	ProviderBillingAdapterSub2APICompatible = "sub2api_compatible"

	ProviderBillingDetectionSchemaMatch = "schema_match"

	ProviderBalanceKindWallet       = "wallet_balance"
	ProviderBalanceKindKeyQuota     = "api_key_quota_remaining"
	ProviderBalanceKindSubscription = "subscription_period_remaining"

	providerBillingRequestTimeout = 10 * time.Second
	providerBillingResponseLimit  = 1 << 20
)

var ErrProviderBillingAdapterMismatch = errors.New("provider billing adapter did not match the upstream response")

type ProviderBillingSourceInspectionRequest struct {
	ProviderAccountID string `json:"provider_account_id"`
	AdapterID         string `json:"adapter_id"`
}

type ProviderBillingSourceCapabilities struct {
	UsageCostLines  bool `json:"usage_cost_lines"`
	AggregateUsage  bool `json:"aggregate_usage"`
	Balance         bool `json:"balance"`
	IncrementalSync bool `json:"incremental_sync"`
	PriceFeed       bool `json:"price_feed"`
}

type ProviderBalanceSnapshot struct {
	Kind         string    `json:"kind"`
	AmountMicros int64     `json:"amount_micros"`
	Unlimited    bool      `json:"unlimited"`
	Currency     string    `json:"currency"`
	ObservedAt   time.Time `json:"observed_at"`
}

type ProviderBillingUsageAggregate struct {
	Scope               string `json:"scope"`
	Model               string `json:"model,omitempty"`
	RequestCount        int64  `json:"request_count"`
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
	CacheReadTokens     int64  `json:"cache_read_tokens"`
	ListCostMicros      *int64 `json:"list_cost_micros,omitempty"`
	ActualCostMicros    *int64 `json:"actual_cost_micros,omitempty"`
}

type ProviderBillingSourceInspection struct {
	ProviderID        string                            `json:"provider_id"`
	ProviderAccountID string                            `json:"provider_account_id"`
	ProviderName      string                            `json:"provider_name"`
	ProviderAccount   string                            `json:"provider_account_name"`
	AdapterID         string                            `json:"adapter_id"`
	DetectionStatus   string                            `json:"detection_status"`
	ContractVersion   string                            `json:"contract_version"`
	Currency          string                            `json:"currency"`
	Capabilities      ProviderBillingSourceCapabilities `json:"capabilities"`
	Balance           *ProviderBalanceSnapshot          `json:"balance,omitempty"`
	UsageAggregates   []ProviderBillingUsageAggregate   `json:"usage_aggregates"`
	DiscoveredLines   int                               `json:"discovered_lines"`
	EvidenceHash      string                            `json:"evidence_hash"`
	Warnings          []string                          `json:"warnings"`
	ObservedAt        time.Time                         `json:"observed_at"`
}

type ProviderBillingReadTarget struct {
	BaseURL    string
	Secret     string
	ObservedAt time.Time
}

type ProviderBillingReader interface {
	ID() string
	Inspect(ctx context.Context, target ProviderBillingReadTarget) (ProviderBillingSourceInspection, error)
}

type ProviderBillingReaderFactory func(client *http.Client) ProviderBillingReader

type ProviderBillingAdapterRegistry struct {
	factories map[string]ProviderBillingReaderFactory
	order     []string
}

func NewProviderBillingAdapterRegistry() *ProviderBillingAdapterRegistry {
	registry := &ProviderBillingAdapterRegistry{factories: map[string]ProviderBillingReaderFactory{}}
	registry.Register(ProviderBillingAdapterSub2APICompatible, func(client *http.Client) ProviderBillingReader {
		return &sub2APICompatibleBillingReader{client: client}
	})
	return registry
}

func (r *ProviderBillingAdapterRegistry) Register(id string, factory ProviderBillingReaderFactory) {
	if r == nil || factory == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" || id == ProviderBillingAdapterAuto {
		return
	}
	if _, exists := r.factories[id]; !exists {
		r.order = append(r.order, id)
		sort.Strings(r.order)
	}
	r.factories[id] = factory
}

func (r *ProviderBillingAdapterRegistry) Inspect(ctx context.Context, client *http.Client, adapterID string, target ProviderBillingReadTarget) (ProviderBillingSourceInspection, error) {
	if r == nil {
		return ProviderBillingSourceInspection{}, errors.New("provider billing adapter registry is unavailable")
	}
	adapterID = strings.TrimSpace(adapterID)
	if adapterID == "" {
		adapterID = ProviderBillingAdapterAuto
	}
	if adapterID != ProviderBillingAdapterAuto {
		factory, ok := r.factories[adapterID]
		if !ok {
			return ProviderBillingSourceInspection{}, fmt.Errorf("provider billing adapter %q is not registered", adapterID)
		}
		return factory(client).Inspect(ctx, target)
	}
	for _, id := range r.order {
		result, err := r.factories[id](client).Inspect(ctx, target)
		if errors.Is(err, ErrProviderBillingAdapterMismatch) {
			continue
		}
		return result, err
	}
	return ProviderBillingSourceInspection{}, errors.New("no compatible provider billing adapter was detected")
}

func (s *Service) InspectProviderBillingSource(ctx context.Context, actor string, request ProviderBillingSourceInspectionRequest) (ProviderBillingSourceInspection, error) {
	result, err := s.inspectProviderBillingSource(ctx, request)
	if err != nil {
		return ProviderBillingSourceInspection{}, err
	}
	if err := s.audit(ctx, actor, "inspect", "provider_billing_source", result.ProviderAccountID, fmt.Sprintf("Inspected provider billing source with adapter %s", result.AdapterID)); err != nil {
		return ProviderBillingSourceInspection{}, err
	}
	return result, nil
}

func (s *Service) inspectProviderBillingSource(ctx context.Context, request ProviderBillingSourceInspectionRequest) (ProviderBillingSourceInspection, error) {
	request.ProviderAccountID = strings.TrimSpace(request.ProviderAccountID)
	if request.ProviderAccountID == "" {
		return ProviderBillingSourceInspection{}, errors.New("provider_account_id is required")
	}
	account, err := s.providerAccountByID(ctx, request.ProviderAccountID)
	if err != nil {
		return ProviderBillingSourceInspection{}, err
	}
	provider, err := s.providerByID(ctx, account.ProviderID)
	if err != nil {
		return ProviderBillingSourceInspection{}, err
	}
	if account.AuthType != "api_key" || !account.SecretConfigured || account.SecretCiphertext == "" {
		return ProviderBillingSourceInspection{}, errors.New("provider account must have an API key secret configured")
	}
	secret, err := decryptSecret(s.secretKey, account.SecretCiphertext)
	if err != nil || strings.TrimSpace(secret) == "" {
		return ProviderBillingSourceInspection{}, errors.New("provider account secret cannot be used for billing inspection")
	}
	registry := s.providerBillingAdapters
	if registry == nil {
		registry = NewProviderBillingAdapterRegistry()
	}
	result, err := registry.Inspect(ctx, s.providerBillingHTTPClient, request.AdapterID, ProviderBillingReadTarget{
		BaseURL: provider.BaseURL, Secret: secret, ObservedAt: s.nowUTC(),
	})
	if err != nil {
		return ProviderBillingSourceInspection{}, err
	}
	result.ProviderID = provider.ID
	result.ProviderAccountID = account.ID
	result.ProviderName = provider.Name
	result.ProviderAccount = account.Name
	return result, nil
}

type sub2APICompatibleBillingReader struct {
	client *http.Client
}

func (r *sub2APICompatibleBillingReader) ID() string {
	return ProviderBillingAdapterSub2APICompatible
}

type sub2APIUsageResponse struct {
	Mode       string               `json:"mode"`
	IsValid    *bool                `json:"isValid"`
	Unit       string               `json:"unit"`
	Remaining  json.RawMessage      `json:"remaining"`
	Balance    json.RawMessage      `json:"balance"`
	Quota      *sub2APIQuota        `json:"quota"`
	Usage      *sub2APIUsageSummary `json:"usage"`
	ModelStats []sub2APIModelStat   `json:"model_stats"`
}

type sub2APIQuota struct {
	Remaining json.RawMessage `json:"remaining"`
}

type sub2APIUsageSummary struct {
	Today *sub2APIUsageAggregate `json:"today"`
	Total *sub2APIUsageAggregate `json:"total"`
}

type sub2APIUsageAggregate struct {
	Requests            int64           `json:"requests"`
	InputTokens         int64           `json:"input_tokens"`
	OutputTokens        int64           `json:"output_tokens"`
	CacheCreationTokens int64           `json:"cache_creation_tokens"`
	CacheReadTokens     int64           `json:"cache_read_tokens"`
	Cost                json.RawMessage `json:"cost"`
	ActualCost          json.RawMessage `json:"actual_cost"`
}

type sub2APIModelStat struct {
	Model string `json:"model"`
	sub2APIUsageAggregate
}

func (r *sub2APICompatibleBillingReader) Inspect(ctx context.Context, target ProviderBillingReadTarget) (ProviderBillingSourceInspection, error) {
	endpoint, err := providerBillingUsageURL(target.BaseURL)
	if err != nil {
		return ProviderBillingSourceInspection{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ProviderBillingSourceInspection{}, err
	}
	req.Header.Set("Authorization", "Bearer "+target.Secret)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "AsterRouter/provider-billing-inspection")
	client := r.client
	if client == nil {
		client = &http.Client{Timeout: providerBillingRequestTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return ProviderBillingSourceInspection{}, fmt.Errorf("provider billing endpoint request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ProviderBillingSourceInspection{}, errors.New("provider billing endpoint rejected the account API key")
	}
	if resp.StatusCode == http.StatusNotFound {
		return ProviderBillingSourceInspection{}, ErrProviderBillingAdapterMismatch
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ProviderBillingSourceInspection{}, fmt.Errorf("provider billing endpoint returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, providerBillingResponseLimit+1))
	if err != nil {
		return ProviderBillingSourceInspection{}, errors.New("provider billing response could not be read")
	}
	if len(body) > providerBillingResponseLimit {
		return ProviderBillingSourceInspection{}, errors.New("provider billing response exceeds the size limit")
	}
	var payload sub2APIUsageResponse
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return ProviderBillingSourceInspection{}, ErrProviderBillingAdapterMismatch
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ProviderBillingSourceInspection{}, ErrProviderBillingAdapterMismatch
	}
	if payload.IsValid == nil || (payload.Mode != "quota_limited" && payload.Mode != "unrestricted") {
		return ProviderBillingSourceInspection{}, ErrProviderBillingAdapterMismatch
	}
	currency := strings.ToUpper(strings.TrimSpace(payload.Unit))
	if len(currency) != 3 {
		return ProviderBillingSourceInspection{}, errors.New("provider billing response has an invalid currency")
	}
	result := ProviderBillingSourceInspection{
		AdapterID: ProviderBillingAdapterSub2APICompatible, DetectionStatus: ProviderBillingDetectionSchemaMatch,
		ContractVersion: "sub2api_v1_usage", Currency: currency, ObservedAt: target.ObservedAt,
		Capabilities:    ProviderBillingSourceCapabilities{AggregateUsage: payload.Usage != nil},
		UsageAggregates: []ProviderBillingUsageAggregate{}, DiscoveredLines: 0,
		Warnings: []string{"adapter_schema_match_does_not_prove_vendor_identity", "usage_cost_lines_unavailable"},
	}
	if !*payload.IsValid {
		result.Warnings = append(result.Warnings, "account_key_reported_invalid")
	}
	if payload.Usage != nil {
		for _, item := range []struct {
			scope string
			value *sub2APIUsageAggregate
		}{{"today", payload.Usage.Today}, {"total", payload.Usage.Total}} {
			if item.value == nil {
				continue
			}
			aggregate, err := sub2APIAggregate(item.scope, item.value)
			if err != nil {
				return ProviderBillingSourceInspection{}, err
			}
			result.UsageAggregates = append(result.UsageAggregates, aggregate)
		}
		result.Warnings = append(result.Warnings, "aggregate_totals_are_not_billing_lines")
	}
	for index := range payload.ModelStats {
		model := strings.TrimSpace(payload.ModelStats[index].Model)
		if model == "" {
			continue
		}
		aggregate, err := sub2APIAggregate("model_30d", &payload.ModelStats[index].sub2APIUsageAggregate)
		if err != nil {
			return ProviderBillingSourceInspection{}, err
		}
		aggregate.Model = model
		result.UsageAggregates = append(result.UsageAggregates, aggregate)
	}
	balanceRaw := payload.Balance
	balanceKind := ProviderBalanceKindWallet
	if payload.Mode == "quota_limited" {
		balanceKind = ProviderBalanceKindKeyQuota
		if payload.Quota != nil && len(payload.Quota.Remaining) > 0 {
			balanceRaw = payload.Quota.Remaining
		} else {
			balanceRaw = payload.Remaining
		}
		result.Warnings = append(result.Warnings, "remaining_is_quota_not_wallet_balance")
	} else if len(balanceRaw) == 0 {
		balanceKind = ProviderBalanceKindSubscription
		balanceRaw = payload.Remaining
		result.Warnings = append(result.Warnings, "remaining_may_be_subscription_period_limit")
	}
	if amount, present, err := decimalJSONMicros(balanceRaw); err != nil {
		return ProviderBillingSourceInspection{}, fmt.Errorf("provider billing balance is invalid: %w", err)
	} else if present {
		unlimited := balanceKind == ProviderBalanceKindSubscription && amount == -1_000_000
		if unlimited {
			amount = 0
			result.Warnings = append(result.Warnings, "subscription_remaining_unlimited")
		}
		result.Balance = &ProviderBalanceSnapshot{Kind: balanceKind, AmountMicros: amount, Unlimited: unlimited, Currency: currency, ObservedAt: target.ObservedAt}
		result.Capabilities.Balance = true
	}
	hash := sha256.Sum256(body)
	result.EvidenceHash = hex.EncodeToString(hash[:])
	return result, nil
}

func sub2APIAggregate(scope string, value *sub2APIUsageAggregate) (ProviderBillingUsageAggregate, error) {
	listCost, _, err := nonNegativeDecimalJSONMicros(value.Cost)
	if err != nil {
		return ProviderBillingUsageAggregate{}, fmt.Errorf("provider billing %s list cost is invalid: %w", scope, err)
	}
	actualCost, _, err := nonNegativeDecimalJSONMicros(value.ActualCost)
	if err != nil {
		return ProviderBillingUsageAggregate{}, fmt.Errorf("provider billing %s actual cost is invalid: %w", scope, err)
	}
	return ProviderBillingUsageAggregate{
		Scope: scope, RequestCount: value.Requests, InputTokens: value.InputTokens, OutputTokens: value.OutputTokens,
		CacheCreationTokens: value.CacheCreationTokens, CacheReadTokens: value.CacheReadTokens,
		ListCostMicros: listCost, ActualCostMicros: actualCost,
	}, nil
}

func nonNegativeDecimalJSONMicros(raw json.RawMessage) (*int64, bool, error) {
	value, present, err := decimalJSONMicros(raw)
	if err != nil || !present {
		return nil, present, err
	}
	if value < 0 {
		return nil, true, errors.New("amount must not be negative")
	}
	return &value, true, nil
}

func decimalJSONMicros(raw json.RawMessage) (int64, bool, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return 0, false, nil
	}
	if strings.HasPrefix(value, "\"") {
		var decoded string
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return 0, true, err
		}
		value = strings.TrimSpace(decoded)
	}
	rat := new(big.Rat)
	if _, ok := rat.SetString(value); !ok {
		return 0, true, errors.New("amount is not a decimal number")
	}
	rat.Mul(rat, big.NewRat(1_000_000, 1))
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(rat.Num(), rat.Denom(), remainder)
	absRemainder := new(big.Int).Abs(remainder)
	if new(big.Int).Lsh(absRemainder, 1).Cmp(rat.Denom()) >= 0 {
		if rat.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	if !quotient.IsInt64() {
		return 0, true, errors.New("amount exceeds supported range")
	}
	return quotient.Int64(), true, nil
}

func providerBillingUsageURL(baseURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("provider base URL must be an absolute http or https URL")
	}
	u.RawQuery = ""
	u.Fragment = ""
	path := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(path, "/v1") {
		path += "/v1"
	}
	u.Path = path + "/usage"
	return u.String(), nil
}
