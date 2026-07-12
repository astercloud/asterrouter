package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	officialFeedMetadataPurpose = "official_data_feed_metadata"
	officialFeedDownloadPurpose = "official_data_feed_download"
	maxOfficialFeedHTTPBytes    = 48 * 1024 * 1024
)

var (
	ErrOfficialFeedSyncDisabled = errors.New("official data feed sync is disabled")
	ErrOfficialFeedSyncMode     = errors.New("official data feed sync requires online or private_mirror mode")
	ErrOfficialFeedRemote       = errors.New("official data feed remote request failed")
)

type officialFeedRemoteMetadata struct {
	ServiceKey        string            `json:"service_key"`
	FeedID            string            `json:"feed_id"`
	FeedVersion       string            `json:"feed_version"`
	DataSchemaVersion string            `json:"data_schema_version"`
	DownloadURL       string            `json:"download_url,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	RequestID         string            `json:"request_id,omitempty"`
	ExpiresAt         time.Time         `json:"expires_at"`
	Revocations       []feedRevocation  `json:"revocations,omitempty"`
}

type officialFeedDownloadGrant struct {
	ServiceKey  string            `json:"service_key"`
	FeedID      string            `json:"feed_id"`
	DownloadURL string            `json:"download_url"`
	Headers     map[string]string `json:"headers,omitempty"`
	RequestID   string            `json:"request_id,omitempty"`
	SHA256      string            `json:"sha256,omitempty"`
	SizeBytes   int64             `json:"size_bytes,omitempty"`
	ExpiresAt   time.Time         `json:"expires_at"`
}

func (s *Service) SyncOfficialFeed(ctx context.Context, serviceKey string) (result OfficialFeedSyncResult, err error) {
	serviceKey = strings.TrimSpace(serviceKey)
	startedAt := s.now().UTC()
	run := OfficialFeedSyncRun{
		ID:         "fsr_" + randomID(18),
		ServiceKey: serviceKey,
		Mode:       normalizeOfficialCatalogConfig(s.catalogConfig).Mode,
		Status:     "failed",
		StartedAt:  startedAt,
	}
	defer func() {
		run.FinishedAt = s.now().UTC()
		if err == nil {
			run.Status = "succeeded"
		} else {
			run.ErrorCode = officialFeedSyncErrorCode(err)
			run.Error = err.Error()
		}
		saveErr := s.repo.SaveOfficialFeedSyncRun(ctx, officialFeedSyncRunRecord{OfficialFeedSyncRun: run})
		result.Run = run
		if err == nil && saveErr != nil {
			err = saveErr
			result.Run.Status = "failed"
			result.Run.ErrorCode = "sync_run_persist_failed"
			result.Run.Error = saveErr.Error()
		}
	}()

	if serviceKey == "" {
		return result, ErrOfficialFeedInvalid
	}
	cfg, configErr := s.effectiveCatalogConfig(ctx)
	if configErr != nil {
		return result, configErr
	}
	run.Mode = cfg.Mode
	if cfg.Mode == CatalogModeDisabled {
		return result, ErrOfficialFeedSyncDisabled
	}
	if cfg.Mode != CatalogModeOnline && cfg.Mode != CatalogModePrivateMirror {
		return result, ErrOfficialFeedSyncMode
	}
	if cfg.PublicKeyID == "" || cfg.PublicKeyBase64 == "" {
		return result, ErrCatalogNotConfigured
	}
	license, activationSecret, licenseErr := s.officialFeedLicense(ctx, serviceKey)
	if licenseErr != nil {
		return result, licenseErr
	}
	clientInfo, clientErr := s.OfficialFeedClientInfo(ctx)
	if clientErr != nil {
		return result, clientErr
	}
	latestURL, urlErr := officialFeedLatestURL(cfg, serviceKey)
	if urlErr != nil {
		return result, urlErr
	}
	run.SourceURL = latestURL
	requestID := "feedreq_" + randomID(20)
	metadata, responseRequestID, metadataErr := s.fetchOfficialFeedMetadata(ctx, cfg, latestURL, requestID, license, activationSecret, clientInfo)
	if responseRequestID != "" {
		requestID = responseRequestID
	}
	run.RequestID = requestID
	if metadataErr != nil {
		return result, metadataErr
	}
	if revocationErr := validateFeedRevocations(metadata.Revocations, "", s.now().UTC()); revocationErr != nil {
		return result, revocationErr
	}
	if revocationErr := s.applyOfficialFeedRevocations(ctx, metadata.ServiceKey, metadata.Revocations, s.now().UTC()); revocationErr != nil {
		return result, revocationErr
	}
	run.FeedID = metadata.FeedID
	if metadata.RequestID != "" {
		run.RequestID = metadata.RequestID
	}
	envelope, downloadRequestID, downloadErr := s.downloadOfficialFeedEnvelope(ctx, cfg, metadata, license, activationSecret, clientInfo, run.RequestID)
	if downloadRequestID != "" {
		run.RequestID = downloadRequestID
	}
	if downloadErr != nil {
		return result, downloadErr
	}
	var downloaded encryptedFeedPackage
	if unmarshalErr := json.Unmarshal(envelope.Payload, &downloaded); unmarshalErr != nil ||
		downloaded.ServiceKey != metadata.ServiceKey ||
		downloaded.FeedID != metadata.FeedID ||
		downloaded.FeedVersion != metadata.FeedVersion ||
		downloaded.DataSchemaVersion != metadata.DataSchemaVersion {
		return result, ErrOfficialFeedInvalid
	}
	rawEnvelope, marshalErr := json.Marshal(envelope)
	if marshalErr != nil {
		return result, marshalErr
	}
	feed, importErr := s.ImportOfficialFeed(ctx, OfficialFeedImportRequest{Envelope: rawEnvelope})
	if importErr != nil {
		return result, importErr
	}
	for _, revocation := range metadata.Revocations {
		if strings.TrimSpace(revocation.FeedID) == feed.FeedID {
			feed.Status = "revoked"
			result.Feed = feed
			break
		}
	}
	result.Feed = feed
	run.FeedID = feed.FeedID
	return result, nil
}

func (s *Service) OfficialFeedSyncRuns(ctx context.Context, serviceKey string, limit int) ([]OfficialFeedSyncRun, error) {
	records, err := s.repo.ListOfficialFeedSyncRuns(ctx, strings.TrimSpace(serviceKey), limit)
	if err != nil {
		return nil, err
	}
	out := make([]OfficialFeedSyncRun, 0, len(records))
	for _, record := range records {
		out = append(out, record.OfficialFeedSyncRun)
	}
	return out, nil
}

func (s *Service) officialFeedLicense(ctx context.Context, serviceKey string) (licenseRecord, string, error) {
	license, ok, err := s.repo.LatestLicense(ctx)
	if err != nil {
		return licenseRecord{}, "", err
	}
	if !ok || !licenseRecordActive(license, s.now().UTC()) {
		return licenseRecord{}, "", ErrLicenseNotFound
	}
	if !licenseAllowsResource(license, "data_feed", serviceKey, s.now().UTC()) {
		return licenseRecord{}, "", ErrOfficialFeedEntitlement
	}
	secret := ""
	if strings.TrimSpace(license.ActivationSecretCiphertext) != "" {
		secret, err = decryptSecret(s.secretKey, license.ActivationSecretCiphertext)
		if err != nil {
			return licenseRecord{}, "", err
		}
	}
	return license, secret, nil
}

func (s *Service) fetchOfficialFeedMetadata(ctx context.Context, cfg OfficialCatalogConfig, endpoint string, requestID string, license licenseRecord, activationSecret string, client OfficialFeedClientInfo) (officialFeedRemoteMetadata, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return officialFeedRemoteMetadata{}, "", err
	}
	s.applyOfficialFeedRequestHeaders(req, requestID, license, activationSecret, client)
	response, err := s.httpClient.Do(req)
	if err != nil {
		return officialFeedRemoteMetadata{}, "", err
	}
	defer response.Body.Close()
	responseRequestID := officialResponseRequestID(response, requestID)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return officialFeedRemoteMetadata{}, responseRequestID, fmt.Errorf("%w: metadata returned status %d", ErrOfficialFeedRemote, response.StatusCode)
	}
	body, err := readOfficialFeedHTTPBody(response.Body)
	if err != nil {
		return officialFeedRemoteMetadata{}, responseRequestID, err
	}
	envelope, err := decodeCatalogEnvelope(body)
	if err != nil {
		return officialFeedRemoteMetadata{}, responseRequestID, ErrCatalogSignature
	}
	if err := verifySignedEnvelope(envelope, cfg, officialFeedMetadataPurpose, s.now().UTC()); err != nil {
		return officialFeedRemoteMetadata{}, responseRequestID, ErrCatalogSignature
	}
	var metadata officialFeedRemoteMetadata
	if err := json.Unmarshal(envelope.Payload, &metadata); err != nil {
		return officialFeedRemoteMetadata{}, responseRequestID, ErrOfficialFeedInvalid
	}
	metadata.ServiceKey = strings.TrimSpace(metadata.ServiceKey)
	metadata.FeedID = strings.TrimSpace(metadata.FeedID)
	if metadata.ServiceKey == "" || metadata.FeedID == "" || strings.TrimSpace(metadata.FeedVersion) == "" || strings.TrimSpace(metadata.DataSchemaVersion) == "" || metadata.ServiceKey != strings.TrimSpace(clientServiceKey(endpoint)) || !metadata.ExpiresAt.After(s.now().UTC()) {
		return officialFeedRemoteMetadata{}, responseRequestID, ErrOfficialFeedInvalid
	}
	return metadata, responseRequestID, nil
}

func (s *Service) downloadOfficialFeedEnvelope(ctx context.Context, cfg OfficialCatalogConfig, metadata officialFeedRemoteMetadata, license licenseRecord, activationSecret string, client OfficialFeedClientInfo, requestID string) (catalogEnvelope, string, error) {
	endpoint := strings.TrimSpace(metadata.DownloadURL)
	if endpoint == "" {
		var err error
		endpoint, err = officialFeedDownloadURL(cfg, metadata.ServiceKey, metadata.FeedID)
		if err != nil {
			return catalogEnvelope{}, requestID, err
		}
	}
	if !isHTTPURL(endpoint) {
		return catalogEnvelope{}, requestID, ErrOfficialFeedInvalid
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return catalogEnvelope{}, requestID, err
	}
	s.applyOfficialFeedRequestHeaders(req, requestID, license, activationSecret, client)
	for key, value := range metadata.Headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	response, err := s.httpClient.Do(req)
	if err != nil {
		return catalogEnvelope{}, requestID, err
	}
	defer response.Body.Close()
	responseRequestID := officialResponseRequestID(response, requestID)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return catalogEnvelope{}, responseRequestID, fmt.Errorf("%w: download authorization returned status %d", ErrOfficialFeedRemote, response.StatusCode)
	}
	body, err := readOfficialFeedHTTPBody(response.Body)
	if err != nil {
		return catalogEnvelope{}, responseRequestID, err
	}
	envelope, err := decodeCatalogEnvelope(body)
	if err != nil {
		return catalogEnvelope{}, responseRequestID, ErrCatalogSignature
	}
	if envelope.Purpose == officialFeedEnvelopePurpose {
		return envelope, responseRequestID, nil
	}
	if err := verifySignedEnvelope(envelope, cfg, officialFeedDownloadPurpose, s.now().UTC()); err != nil {
		return catalogEnvelope{}, responseRequestID, ErrCatalogSignature
	}
	var grant officialFeedDownloadGrant
	if err := json.Unmarshal(envelope.Payload, &grant); err != nil {
		return catalogEnvelope{}, responseRequestID, ErrOfficialFeedInvalid
	}
	if grant.ServiceKey != metadata.ServiceKey || grant.FeedID != metadata.FeedID || !isHTTPURL(grant.DownloadURL) || !grant.ExpiresAt.After(s.now().UTC()) || grant.SizeBytes <= 0 || len(normalizeSHA256(grant.SHA256)) != sha256.Size*2 {
		return catalogEnvelope{}, responseRequestID, ErrOfficialFeedInvalid
	}
	if grant.RequestID != "" {
		responseRequestID = grant.RequestID
	}
	return s.downloadOfficialFeedObject(ctx, cfg, grant, responseRequestID)
}

func (s *Service) downloadOfficialFeedObject(ctx context.Context, cfg OfficialCatalogConfig, grant officialFeedDownloadGrant, requestID string) (catalogEnvelope, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, grant.DownloadURL, nil)
	if err != nil {
		return catalogEnvelope{}, requestID, err
	}
	req.Header.Set("X-Aster-Request-ID", requestID)
	for key, value := range grant.Headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	response, err := s.httpClient.Do(req)
	if err != nil {
		return catalogEnvelope{}, requestID, err
	}
	defer response.Body.Close()
	responseRequestID := officialResponseRequestID(response, requestID)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return catalogEnvelope{}, responseRequestID, fmt.Errorf("%w: object returned status %d", ErrOfficialFeedRemote, response.StatusCode)
	}
	body, err := readOfficialFeedHTTPBody(response.Body)
	if err != nil {
		return catalogEnvelope{}, responseRequestID, err
	}
	if grant.SizeBytes > 0 && int64(len(body)) != grant.SizeBytes {
		return catalogEnvelope{}, responseRequestID, ErrOfficialFeedInvalid
	}
	if strings.TrimSpace(grant.SHA256) != "" {
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != normalizeSHA256(grant.SHA256) {
			return catalogEnvelope{}, responseRequestID, ErrOfficialFeedInvalid
		}
	}
	envelope, err := decodeCatalogEnvelope(body)
	if err != nil || envelope.Purpose != officialFeedEnvelopePurpose {
		return catalogEnvelope{}, responseRequestID, ErrCatalogSignature
	}
	if err := verifySignedEnvelope(envelope, cfg, officialFeedEnvelopePurpose, s.now().UTC()); err != nil {
		return catalogEnvelope{}, responseRequestID, ErrCatalogSignature
	}
	return envelope, responseRequestID, nil
}

func (s *Service) applyOfficialFeedRequestHeaders(req *http.Request, requestID string, license licenseRecord, activationSecret string, client OfficialFeedClientInfo) {
	req.Header.Set("X-Aster-Request-ID", requestID)
	req.Header.Set("X-Aster-Core-Version", s.coreVersion)
	req.Header.Set("X-Aster-License-ID", license.LicenseID)
	req.Header.Set("X-Aster-Instance-ID", license.InstanceID)
	req.Header.Set("X-Aster-Feed-Public-Key", client.EncryptionPublicKey)
	if strings.TrimSpace(activationSecret) != "" {
		req.Header.Set("X-Aster-Activation-Secret", activationSecret)
	}
	licenseCfg, err := s.effectiveLicenseConfig(req.Context())
	if err == nil && licenseCfg.Fingerprint != "" {
		req.Header.Set("X-Aster-Instance-Fingerprint", licenseCfg.Fingerprint)
	}
}

func officialFeedLatestURL(cfg OfficialCatalogConfig, serviceKey string) (string, error) {
	return officialFeedServiceEndpoint(cfg, serviceKey, "latest")
}

func officialFeedDownloadURL(cfg OfficialCatalogConfig, serviceKey string, feedID string) (string, error) {
	return officialFeedServiceEndpoint(cfg, serviceKey, url.PathEscape(strings.TrimSpace(feedID))+"/download")
}

func officialFeedServiceEndpoint(cfg OfficialCatalogConfig, serviceKey string, suffix string) (string, error) {
	base, err := officialServicesBaseURL(cfg)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + url.PathEscape(strings.TrimSpace(serviceKey)) + "/feeds/" + strings.TrimLeft(suffix, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func officialServicesBaseURL(cfg OfficialCatalogConfig) (string, error) {
	value := strings.TrimSpace(cfg.ServicesURL)
	if value == "" {
		value = strings.TrimSpace(cfg.URL)
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", ErrCatalogNotConfigured
	}
	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(path, "/catalog/index"):
		path = strings.TrimSuffix(path, "/catalog/index") + "/services"
	case strings.HasSuffix(path, "/catalog/bootstrap"):
		path = strings.TrimSuffix(path, "/catalog/bootstrap") + "/services"
	case strings.HasSuffix(path, "/services"):
	default:
		path += "/services"
	}
	parsed.Path = path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func readOfficialFeedHTTPBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxOfficialFeedHTTPBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxOfficialFeedHTTPBytes {
		return nil, ErrOfficialFeedInvalid
	}
	return body, nil
}

func officialResponseRequestID(response *http.Response, fallback string) string {
	for _, key := range []string{"X-Aster-Request-ID", "X-Request-ID"} {
		if value := strings.TrimSpace(response.Header.Get(key)); value != "" {
			return value
		}
	}
	return fallback
}

func clientServiceKey(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for index := 0; index+2 < len(parts); index++ {
		if parts[index] == "services" && parts[index+2] == "feeds" {
			value, _ := url.PathUnescape(parts[index+1])
			return value
		}
	}
	return ""
}

func officialFeedSyncErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrOfficialFeedSyncDisabled):
		return "sync_disabled"
	case errors.Is(err, ErrOfficialFeedSyncMode):
		return "sync_mode_invalid"
	case errors.Is(err, ErrLicenseNotFound):
		return "license_missing"
	case errors.Is(err, ErrOfficialFeedEntitlement):
		return "entitlement_missing"
	case errors.Is(err, ErrCatalogSignature):
		return "signature_invalid"
	case errors.Is(err, ErrOfficialFeedBinding):
		return "instance_binding_invalid"
	case errors.Is(err, ErrOfficialFeedDecrypt):
		return "decrypt_failed"
	case errors.Is(err, ErrOfficialFeedReplay):
		return "rollback_rejected"
	case errors.Is(err, ErrOfficialFeedExpired):
		return "feed_expired"
	case errors.Is(err, ErrOfficialFeedRemote):
		return "remote_request_failed"
	case errors.Is(err, ErrCatalogNotConfigured):
		return "not_configured"
	case errors.Is(err, ErrOfficialFeedInvalid):
		return "feed_invalid"
	default:
		return "internal_error"
	}
}
