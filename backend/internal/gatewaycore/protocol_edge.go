package gatewaycore

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var (
	ErrCredentialMissing           = errors.New("gateway credential is required")
	ErrCredentialConflict          = errors.New("multiple gateway credentials are not allowed")
	ErrCredentialTransportRejected = errors.New("credential transport is not allowed for this protocol")
	ErrQueryCredentialRejected     = errors.New("gateway credentials in URL query parameters are not allowed")
	ErrInvalidCanonicalRequest     = errors.New("invalid canonical gateway request")
)

const (
	maxRequestIDBytes   = 128
	maxIdempotencyBytes = 256
	maxStickyKeyBytes   = 256
)

func ExtractCredential(req *http.Request, protocol Protocol) (CredentialEnvelope, error) {
	if req == nil {
		return CredentialEnvelope{}, ErrCredentialMissing
	}
	for _, key := range []string{"api_key", "key", "access_token", "token"} {
		if strings.TrimSpace(req.URL.Query().Get(key)) != "" {
			return CredentialEnvelope{}, ErrQueryCredentialRejected
		}
	}

	candidates := make([]CredentialEnvelope, 0, 3)
	authorization := strings.TrimSpace(req.Header.Get("Authorization"))
	if authorization != "" {
		parts := strings.Fields(authorization)
		if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
			return CredentialEnvelope{}, ErrCredentialTransportRejected
		}
		switch strings.ToLower(parts[0]) {
		case "bearer":
			candidates = append(candidates, CredentialEnvelope{BearerToken: parts[1], Transport: "authorization_bearer"})
		case "aster-context":
			candidates = append(candidates, CredentialEnvelope{SignedContext: parts[1], Transport: "authorization_aster_context"})
		default:
			return CredentialEnvelope{}, ErrCredentialTransportRejected
		}
	}
	if value := strings.TrimSpace(req.Header.Get("X-API-Key")); value != "" {
		if protocol != ProtocolAnthropicMessages {
			return CredentialEnvelope{}, ErrCredentialTransportRejected
		}
		candidates = append(candidates, CredentialEnvelope{BearerToken: value, Transport: "anthropic_x_api_key"})
	}
	if value := strings.TrimSpace(req.Header.Get("X-Goog-API-Key")); value != "" {
		if protocol != ProtocolGeminiGenerate {
			return CredentialEnvelope{}, ErrCredentialTransportRejected
		}
		candidates = append(candidates, CredentialEnvelope{BearerToken: value, Transport: "gemini_x_goog_api_key"})
	}
	if len(candidates) == 0 {
		return CredentialEnvelope{}, ErrCredentialMissing
	}
	if len(candidates) != 1 {
		return CredentialEnvelope{}, ErrCredentialConflict
	}
	return candidates[0], nil
}

func CanonicalizeOpenAIChat(raw []byte, header http.Header) (CanonicalRequest, error) {
	var payload struct {
		Model    string            `json:"model"`
		Messages []json.RawMessage `json:"messages"`
		Stream   bool              `json:"stream"`
		User     string            `json:"user"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return CanonicalRequest{}, ErrInvalidCanonicalRequest
	}
	payload.Model = strings.TrimSpace(payload.Model)
	if payload.Model == "" {
		return CanonicalRequest{}, fmt.Errorf("%w: model is required", ErrInvalidCanonicalRequest)
	}
	requestID, err := canonicalRequestID(header)
	if err != nil {
		return CanonicalRequest{}, err
	}
	idempotencyKey := strings.TrimSpace(header.Get("Idempotency-Key"))
	if len(idempotencyKey) > maxIdempotencyBytes {
		return CanonicalRequest{}, fmt.Errorf("%w: idempotency key is too long", ErrInvalidCanonicalRequest)
	}
	stickyKey := strings.TrimSpace(header.Get("X-AsterRouter-Sticky-Key"))
	if stickyKey == "" {
		stickyKey = strings.TrimSpace(payload.User)
	}
	if len(stickyKey) > maxStickyKeyBytes {
		stickyKey = stickyKey[:maxStickyKeyBytes]
	}
	fingerprint := sha256.Sum256(raw)
	return CanonicalRequest{
		ID:              "op_" + requestID,
		ClientRequestID: requestID,
		Fingerprint:     hex.EncodeToString(fingerprint[:]),
		Protocol:        ProtocolOpenAIChat,
		Operation:       "chat_completion",
		Modality:        "text",
		Lane:            LaneDirect,
		Model:           payload.Model,
		Stream:          payload.Stream,
		MessageCount:    len(payload.Messages),
		IdempotencyKey:  idempotencyKey,
		StickyKey:       stickyKey,
		Payload:         append(json.RawMessage(nil), raw...),
	}, nil
}

func CanonicalizeOpenAIModels(header http.Header) (CanonicalRequest, error) {
	requestID, err := canonicalRequestID(header)
	if err != nil {
		return CanonicalRequest{}, err
	}
	fingerprint := sha256.Sum256([]byte("GET /v1/models"))
	return CanonicalRequest{
		ID:              "op_" + requestID,
		ClientRequestID: requestID,
		Fingerprint:     hex.EncodeToString(fingerprint[:]),
		Protocol:        ProtocolOpenAIModels,
		Operation:       "list_models",
		Modality:        "metadata",
		Lane:            LaneDirect,
	}, nil
}

func CanonicalizeOpenAIImageGeneration(raw []byte, header http.Header) (CanonicalRequest, error) {
	var payload map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil || payload == nil {
		return CanonicalRequest{}, ErrInvalidCanonicalRequest
	}
	model, _ := payload["model"].(string)
	prompt, _ := payload["prompt"].(string)
	model = strings.TrimSpace(model)
	prompt = strings.TrimSpace(prompt)
	if model == "" || prompt == "" {
		return CanonicalRequest{}, fmt.Errorf("%w: model and prompt are required", ErrInvalidCanonicalRequest)
	}
	outputCount := 1
	if value, exists := payload["n"]; exists {
		number, ok := value.(float64)
		if !ok || number != float64(int(number)) || number < 1 || number > 10 {
			return CanonicalRequest{}, fmt.Errorf("%w: n must be an integer from 1 to 10", ErrInvalidCanonicalRequest)
		}
		outputCount = int(number)
	}
	responseMode := strings.ToLower(strings.TrimSpace(stringValue(payload["response_mode"])))
	stream, _ := payload["stream"].(bool)
	if responseMode == "" {
		if stream {
			responseMode = "stream"
		} else {
			responseMode = "blocking"
		}
	}
	if responseMode != "blocking" && responseMode != "stream" && responseMode != "async" {
		return CanonicalRequest{}, fmt.Errorf("%w: invalid response_mode", ErrInvalidCanonicalRequest)
	}
	previewMode := strings.ToLower(strings.TrimSpace(stringValue(payload["preview_mode"])))
	if previewMode == "" {
		previewMode = "none"
		if partial, ok := payload["partial_images"].(float64); ok && partial > 0 {
			previewMode = "required"
		}
	}
	if previewMode != "none" && previewMode != "preferred" && previewMode != "required" {
		return CanonicalRequest{}, fmt.Errorf("%w: invalid preview_mode", ErrInvalidCanonicalRequest)
	}
	if previewMode == "required" && responseMode != "stream" {
		return CanonicalRequest{}, fmt.Errorf("%w: required previews need response_mode=stream", ErrInvalidCanonicalRequest)
	}
	deliveryMode := strings.ToLower(strings.TrimSpace(stringValue(payload["delivery_mode"])))
	if deliveryMode == "" {
		deliveryMode = "inline"
		if responseMode == "async" {
			deliveryMode = "artifact"
		}
	}
	if deliveryMode != "inline" && deliveryMode != "artifact" && deliveryMode != "customer_sink" {
		return CanonicalRequest{}, fmt.Errorf("%w: invalid delivery_mode", ErrInvalidCanonicalRequest)
	}
	requestID, err := canonicalRequestID(header)
	if err != nil {
		return CanonicalRequest{}, err
	}
	idempotencyKey := strings.TrimSpace(header.Get("Idempotency-Key"))
	if idempotencyKey == "" || len(idempotencyKey) > maxIdempotencyBytes {
		return CanonicalRequest{}, fmt.Errorf("%w: a valid idempotency key is required", ErrInvalidCanonicalRequest)
	}
	input := make(map[string]any, len(payload))
	for key, value := range payload {
		switch key {
		case "model", "response_mode", "preview_mode", "delivery_mode", "stream", "partial_images":
			continue
		default:
			input[key] = value
		}
	}
	input["prompt"] = prompt
	input["n"] = outputCount
	canonicalPayload, err := json.Marshal(map[string]any{
		"model": model, "operation": "image_generation", "modality": "image", "input": input,
		"response_mode": responseMode, "preview_mode": previewMode, "delivery_mode": deliveryMode,
	})
	if err != nil {
		return CanonicalRequest{}, ErrInvalidCanonicalRequest
	}
	fingerprint := sha256.Sum256(canonicalPayload)
	lane := LaneDirect
	if responseMode == "async" {
		lane = LaneDurable
	}
	return CanonicalRequest{
		ID: "op_" + requestID, ClientRequestID: requestID, Fingerprint: hex.EncodeToString(fingerprint[:]),
		Protocol: ProtocolOpenAIImages, Operation: "image_generation", Modality: "image", Lane: lane,
		Model: model, Stream: responseMode == "stream", IdempotencyKey: idempotencyKey,
		ResponseMode: responseMode, PreviewMode: previewMode, DeliveryMode: deliveryMode, OutputCount: outputCount,
		Payload: canonicalPayload,
	}, nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func CanonicalizeDurableJob(raw []byte, header http.Header) (CanonicalRequest, error) {
	var payload struct {
		Model     string          `json:"model"`
		Operation string          `json:"operation"`
		Modality  string          `json:"modality"`
		Input     json.RawMessage `json:"input"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return CanonicalRequest{}, ErrInvalidCanonicalRequest
	}
	payload.Model = strings.TrimSpace(payload.Model)
	payload.Operation = strings.ToLower(strings.TrimSpace(payload.Operation))
	payload.Modality = strings.ToLower(strings.TrimSpace(payload.Modality))
	if payload.Model == "" || !validCanonicalToken(payload.Operation) || !validCanonicalToken(payload.Modality) || len(payload.Input) == 0 || string(payload.Input) == "null" {
		return CanonicalRequest{}, ErrInvalidCanonicalRequest
	}
	requestID, err := canonicalRequestID(header)
	if err != nil {
		return CanonicalRequest{}, err
	}
	idempotencyKey := strings.TrimSpace(header.Get("Idempotency-Key"))
	if len(idempotencyKey) > maxIdempotencyBytes {
		return CanonicalRequest{}, fmt.Errorf("%w: idempotency key is too long", ErrInvalidCanonicalRequest)
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return CanonicalRequest{}, ErrInvalidCanonicalRequest
	}
	normalizedObject, ok := normalized.(map[string]any)
	if !ok {
		return CanonicalRequest{}, ErrInvalidCanonicalRequest
	}
	normalizedObject["model"] = payload.Model
	normalizedObject["operation"] = payload.Operation
	normalizedObject["modality"] = payload.Modality
	canonicalPayload, err := json.Marshal(normalized)
	if err != nil {
		return CanonicalRequest{}, ErrInvalidCanonicalRequest
	}
	fingerprint := sha256.Sum256(canonicalPayload)
	return CanonicalRequest{
		ID:              "op_" + requestID,
		ClientRequestID: requestID,
		Fingerprint:     hex.EncodeToString(fingerprint[:]),
		Protocol:        ProtocolAsterJobs,
		Operation:       payload.Operation,
		Modality:        payload.Modality,
		Lane:            LaneDurable,
		Model:           payload.Model,
		IdempotencyKey:  idempotencyKey,
		Payload:         canonicalPayload,
	}, nil
}

func canonicalRequestID(header http.Header) (string, error) {
	clientRequestID := strings.TrimSpace(header.Get("X-Client-Request-ID"))
	requestID := strings.TrimSpace(header.Get("X-Request-ID"))
	if clientRequestID != "" && requestID != "" && clientRequestID != requestID {
		return "", fmt.Errorf("%w: conflicting request ids", ErrInvalidCanonicalRequest)
	}
	if clientRequestID != "" {
		requestID = clientRequestID
	}
	if requestID != "" && (!validRequestID(requestID) || len(requestID) > maxRequestIDBytes) {
		return "", fmt.Errorf("%w: request id contains unsupported characters or is too long", ErrInvalidCanonicalRequest)
	}
	if requestID != "" {
		return requestID, nil
	}
	generated, err := randomRequestID()
	if err != nil {
		return "", err
	}
	return generated, nil
}

func validCanonicalToken(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			continue
		}
		if index > 0 && (char == ':' || char == '_' || char == '-') {
			continue
		}
		return false
	}
	return true
}

func randomRequestID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate gateway request id: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func validRequestID(value string) bool {
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			continue
		}
		switch char {
		case '-', '_', '.', ':':
			continue
		default:
			return false
		}
	}
	return value != ""
}
