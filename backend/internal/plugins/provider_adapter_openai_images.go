package plugins

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

const (
	openAIImageResponseLimit  = 64 << 20
	openAIImageCacheLimit     = 128 << 20
	openAIImageCacheTTL       = 2 * time.Minute
	openAIImageRequestTimeout = 10 * time.Minute
)

var (
	ErrOpenAIImageRequest  = errors.New("openai-compatible image request is invalid")
	ErrOpenAIImageResponse = errors.New("openai-compatible image response is invalid")
	ErrOpenAIImageCache    = errors.New("openai-compatible image output cache is unavailable")
)

type openAIImageTaskCache struct {
	result    controlplane.ProviderDispatchResult
	data      map[string][]byte
	sizeBytes int64
	expiresAt time.Time
}

type openAIImageGenerationResponse struct {
	Data []struct {
		B64JSON string `json:"b64_json"`
	} `json:"data"`
}

func supportsBuiltinOpenAIImageAdapter(provider controlplane.GatewayProvider, job controlplane.AIJob) bool {
	policySupported := job.ArtifactPolicy == controlplane.GatewayArtifactPolicyTemporary ||
		job.ArtifactPolicy == controlplane.GatewayArtifactPolicyManaged ||
		job.ArtifactPolicy == controlplane.GatewayArtifactPolicyCustomerSink
	return policySupported && strings.EqualFold(strings.TrimSpace(provider.Type), "openai_compatible") &&
		strings.EqualFold(strings.TrimSpace(job.Modality), "image") &&
		strings.EqualFold(strings.TrimSpace(job.Operation), "image_generation")
}

func (s *Service) dispatchBuiltinOpenAIImage(ctx context.Context, provider controlplane.GatewayProvider, job controlplane.AIJob, attempt controlplane.AIAttempt, command controlplane.ProviderDispatchCommand) (controlplane.ProviderDispatchResult, error) {
	requestCtx, cancel := context.WithTimeout(ctx, openAIImageRequestTimeout)
	defer cancel()
	requestBody, outputMediaType, err := buildOpenAIImageRequest(provider, command.Payload)
	if err != nil {
		return controlplane.ProviderDispatchResult{Outcome: controlplane.ProviderDispatchOutcomeProvenNotCreated}, err
	}
	endpoint, err := openAIImageEndpoint(provider.BaseURL)
	if err != nil {
		return controlplane.ProviderDispatchResult{Outcome: controlplane.ProviderDispatchOutcomeProvenNotCreated}, err
	}
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return controlplane.ProviderDispatchResult{Outcome: controlplane.ProviderDispatchOutcomeProvenNotCreated}, err
	}
	request.Header.Set("Authorization", "Bearer "+provider.APIKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", command.Intent.DispatchKey)
	response, err := s.providerAdapterHTTPClient.Do(request)
	if err != nil {
		return controlplane.ProviderDispatchResult{Outcome: controlplane.ProviderDispatchOutcomeUnknown}, fmt.Errorf("openai-compatible image transport failed")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if openAIImageStatusProvesNotCreated(response.StatusCode) {
			return controlplane.ProviderDispatchResult{Outcome: controlplane.ProviderDispatchOutcomeProvenNotCreated}, fmt.Errorf("openai-compatible image provider rejected the request with status %d", response.StatusCode)
		}
		return controlplane.ProviderDispatchResult{Outcome: controlplane.ProviderDispatchOutcomeUnknown}, fmt.Errorf("openai-compatible image provider returned status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, openAIImageResponseLimit+1))
	if err != nil || len(body) > openAIImageResponseLimit {
		return controlplane.ProviderDispatchResult{Outcome: controlplane.ProviderDispatchOutcomeUnknown}, ErrOpenAIImageResponse
	}
	var decoded openAIImageGenerationResponse
	if err := json.Unmarshal(body, &decoded); err != nil || len(decoded.Data) == 0 {
		return controlplane.ProviderDispatchResult{Outcome: controlplane.ProviderDispatchOutcomeUnknown}, ErrOpenAIImageResponse
	}
	taskID := openAIImageTaskID(provider.AccountID, command.Intent.DispatchKey)
	outputs := make([]controlplane.ProviderOutputDescriptor, 0, len(decoded.Data))
	outputData := make(map[string][]byte, len(decoded.Data))
	for index, item := range decoded.Data {
		if strings.TrimSpace(item.B64JSON) == "" {
			return controlplane.ProviderDispatchResult{Outcome: controlplane.ProviderDispatchOutcomeUnknown}, fmt.Errorf("%w: b64_json output is required for durable delivery", ErrOpenAIImageResponse)
		}
		data, decodeErr := base64.StdEncoding.DecodeString(item.B64JSON)
		if decodeErr != nil || len(data) == 0 {
			return controlplane.ProviderDispatchResult{Outcome: controlplane.ProviderDispatchOutcomeUnknown}, ErrOpenAIImageResponse
		}
		outputID := fmt.Sprintf("image-%d", index+1)
		digest := sha256.Sum256(data)
		outputs = append(outputs, controlplane.ProviderOutputDescriptor{
			OutputID: outputID, Role: controlplane.ArtifactRoleFinal, MediaType: outputMediaType,
			ExpectedSizeBytes: int64(len(data)), ExpectedSHA256: hex.EncodeToString(digest[:]),
			ProviderReference: "adapter-cache://" + taskID + "/" + outputID,
		})
		outputData[outputID] = data
	}
	result := controlplane.ProviderDispatchResult{
		Outcome: controlplane.ProviderDispatchOutcomeAccepted,
		Task: controlplane.ProviderTaskReference{
			ProviderTaskID: taskID, ProviderRequestID: strings.TrimSpace(response.Header.Get("X-Request-ID")), Status: "succeeded",
		},
		Outputs: outputs, ReconcileAfter: time.Now().UTC(),
	}
	if err := s.cacheOpenAIImageTask(result, outputData); err != nil {
		return controlplane.ProviderDispatchResult{Outcome: controlplane.ProviderDispatchOutcomeUnknown}, err
	}
	return result, nil
}

func (s *Service) reconcileBuiltinOpenAIImage(task controlplane.ProviderTaskReference) (controlplane.ProviderDispatchResult, error) {
	result, found := s.cachedOpenAIImageTask(task.ProviderTaskID)
	if !found {
		return controlplane.ProviderDispatchResult{
			Outcome:        controlplane.ProviderDispatchOutcomeUnknown,
			Task:           controlplane.ProviderTaskReference{ProviderTaskID: task.ProviderTaskID, ProviderRequestID: task.ProviderRequestID, Status: "unknown"},
			ReconcileAfter: time.Now().UTC().Add(time.Minute),
		}, ErrOpenAIImageCache
	}
	return result, nil
}

func (s *Service) openBuiltinOpenAIImageOutput(taskID string, output controlplane.ProviderOutputDescriptor) (io.ReadCloser, error) {
	data, found := s.cachedOpenAIImageOutput(taskID, output)
	if !found {
		return nil, ErrOpenAIImageCache
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func buildOpenAIImageRequest(provider controlplane.GatewayProvider, payload []byte) ([]byte, string, error) {
	var canonical struct {
		Input map[string]any `json:"input"`
	}
	if !json.Valid(payload) || json.Unmarshal(payload, &canonical) != nil || canonical.Input == nil {
		return nil, "", ErrOpenAIImageRequest
	}
	prompt, _ := canonical.Input["prompt"].(string)
	if strings.TrimSpace(prompt) == "" || strings.TrimSpace(provider.UpstreamModel) == "" {
		return nil, "", ErrOpenAIImageRequest
	}
	input := make(map[string]any, len(canonical.Input)+1)
	for key, value := range canonical.Input {
		input[key] = value
	}
	if count, exists := input["count"]; exists {
		if _, hasN := input["n"]; !hasN {
			input["n"] = count
		}
		delete(input, "count")
	}
	input["model"] = provider.UpstreamModel
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(provider.UpstreamModel)), "dall-e-") {
		input["response_format"] = "b64_json"
	} else if strings.EqualFold(fmt.Sprint(input["response_format"]), "url") {
		delete(input, "response_format")
	}
	mediaType := "image/png"
	if format, _ := input["output_format"].(string); strings.EqualFold(format, "jpeg") || strings.EqualFold(format, "jpg") {
		mediaType = "image/jpeg"
	} else if strings.EqualFold(format, "webp") {
		mediaType = "image/webp"
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, "", ErrOpenAIImageRequest
	}
	return body, mediaType, nil
}

func openAIImageEndpoint(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrOpenAIImageRequest
	}
	parsed.Path = path.Join(parsed.Path, "images/generations")
	parsed.RawPath = ""
	return parsed.String(), nil
}

func openAIImageStatusProvesNotCreated(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}

func openAIImageTaskID(accountID, dispatchKey string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(accountID) + "\x00" + strings.TrimSpace(dispatchKey)))
	return "openai_image_" + hex.EncodeToString(digest[:16])
}

func (s *Service) cacheOpenAIImageTask(result controlplane.ProviderDispatchResult, data map[string][]byte) error {
	var sizeBytes int64
	for _, output := range data {
		sizeBytes += int64(len(output))
	}
	if sizeBytes <= 0 || sizeBytes > openAIImageCacheLimit {
		return ErrOpenAIImageCache
	}
	now := time.Now().UTC()
	expiresAt := now.Add(openAIImageCacheTTL)
	taskID := strings.TrimSpace(result.Task.ProviderTaskID)
	s.openAIImageCacheMu.Lock()
	s.removeExpiredOpenAIImageTasksLocked(now)
	if existing, found := s.openAIImageTasks[taskID]; found {
		s.openAIImageCacheBytes -= existing.sizeBytes
		delete(s.openAIImageTasks, taskID)
	}
	if s.openAIImageCacheBytes+sizeBytes > openAIImageCacheLimit {
		s.openAIImageCacheMu.Unlock()
		return ErrOpenAIImageCache
	}
	s.openAIImageTasks[taskID] = openAIImageTaskCache{result: result, data: data, sizeBytes: sizeBytes, expiresAt: expiresAt}
	s.openAIImageCacheBytes += sizeBytes
	s.openAIImageCacheMu.Unlock()
	time.AfterFunc(openAIImageCacheTTL, func() { s.expireOpenAIImageTask(taskID, expiresAt) })
	return nil
}

func (s *Service) cachedOpenAIImageTask(taskID string) (controlplane.ProviderDispatchResult, bool) {
	now := time.Now().UTC()
	s.openAIImageCacheMu.Lock()
	defer s.openAIImageCacheMu.Unlock()
	s.removeExpiredOpenAIImageTasksLocked(now)
	task, found := s.openAIImageTasks[strings.TrimSpace(taskID)]
	if !found {
		return controlplane.ProviderDispatchResult{}, false
	}
	result := task.result
	result.Outputs = append([]controlplane.ProviderOutputDescriptor(nil), task.result.Outputs...)
	return result, true
}

func (s *Service) cachedOpenAIImageOutput(taskID string, output controlplane.ProviderOutputDescriptor) ([]byte, bool) {
	now := time.Now().UTC()
	s.openAIImageCacheMu.Lock()
	defer s.openAIImageCacheMu.Unlock()
	s.removeExpiredOpenAIImageTasksLocked(now)
	task, found := s.openAIImageTasks[strings.TrimSpace(taskID)]
	if !found || output.ProviderReference != "adapter-cache://"+strings.TrimSpace(taskID)+"/"+output.OutputID {
		return nil, false
	}
	data, found := task.data[output.OutputID]
	return data, found
}

func (s *Service) expireOpenAIImageTask(taskID string, expiresAt time.Time) {
	s.openAIImageCacheMu.Lock()
	defer s.openAIImageCacheMu.Unlock()
	if task, found := s.openAIImageTasks[taskID]; found && task.expiresAt.Equal(expiresAt) {
		s.openAIImageCacheBytes -= task.sizeBytes
		delete(s.openAIImageTasks, taskID)
	}
}

func (s *Service) removeExpiredOpenAIImageTasksLocked(now time.Time) {
	for taskID, task := range s.openAIImageTasks {
		if !task.expiresAt.After(now) {
			s.openAIImageCacheBytes -= task.sizeBytes
			delete(s.openAIImageTasks, taskID)
		}
	}
}

func (s *Service) clearOpenAIImageTaskCache() {
	s.openAIImageCacheMu.Lock()
	defer s.openAIImageCacheMu.Unlock()
	s.openAIImageTasks = map[string]openAIImageTaskCache{}
	s.openAIImageCacheBytes = 0
}
