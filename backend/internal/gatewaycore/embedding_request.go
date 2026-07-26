package gatewaycore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	MaxEmbeddingInputs          = 2048
	MaxEmbeddingInputItemBytes  = 1 << 20
	MaxEmbeddingTotalInputBytes = 8 << 20
	MaxEmbeddingDimensions      = 65536
)

var ErrUnsupportedEmbeddingFeature = errors.New("unsupported embedding feature")

type CanonicalEmbeddingRequest struct {
	Model          string   `json:"model"`
	Inputs         []string `json:"inputs"`
	EncodingFormat string   `json:"encoding_format"`
	Dimensions     *int     `json:"dimensions,omitempty"`
	ClientUser     string   `json:"client_user,omitempty"`
}

func CanonicalizeOpenAIEmbedding(raw []byte, header http.Header) (CanonicalRequest, error) {
	var payload struct {
		Model          string          `json:"model"`
		Input          json.RawMessage `json:"input"`
		EncodingFormat string          `json:"encoding_format"`
		Dimensions     *int            `json:"dimensions"`
		User           string          `json:"user"`
	}
	if len(raw) == 0 || decodeStrictTextJSON(raw, &payload) != nil {
		return CanonicalRequest{}, ErrInvalidCanonicalRequest
	}
	inputs, err := decodeEmbeddingInputs(payload.Input)
	if err != nil {
		return CanonicalRequest{}, err
	}
	encodingFormat := strings.ToLower(strings.TrimSpace(payload.EncodingFormat))
	if encodingFormat == "" {
		encodingFormat = "float"
	}
	if encodingFormat != "float" && encodingFormat != "base64" {
		return CanonicalRequest{}, fmt.Errorf("%w: encoding_format %q", ErrUnsupportedEmbeddingFeature, encodingFormat)
	}
	if payload.Dimensions != nil && (*payload.Dimensions <= 0 || *payload.Dimensions > MaxEmbeddingDimensions) {
		return CanonicalRequest{}, fmt.Errorf("%w: dimensions must be between 1 and %d", ErrInvalidCanonicalRequest, MaxEmbeddingDimensions)
	}
	request := CanonicalEmbeddingRequest{
		Model: strings.TrimSpace(payload.Model), Inputs: inputs, EncodingFormat: encodingFormat,
		Dimensions: payload.Dimensions, ClientUser: strings.TrimSpace(payload.User),
	}
	if request.Model == "" {
		return CanonicalRequest{}, fmt.Errorf("%w: model is required", ErrInvalidCanonicalRequest)
	}
	normalized, err := json.Marshal(request)
	if err != nil {
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
	fingerprint := sha256.Sum256(normalized)
	return CanonicalRequest{
		ID: "op_" + requestID, ClientRequestID: requestID, Fingerprint: hex.EncodeToString(fingerprint[:]),
		Protocol: ProtocolOpenAIEmbeddings, Operation: "embedding", Modality: "embedding", Lane: LaneDirect,
		Model: request.Model, MessageCount: len(request.Inputs), IdempotencyKey: idempotencyKey, StickyKey: request.ClientUser,
		InputCharacters: int64(embeddingInputBytes(request.Inputs)), Embedding: &request, Payload: normalized,
	}, nil
}

func EncodeOpenAIEmbeddingRequest(request CanonicalEmbeddingRequest, upstreamModel string) ([]byte, error) {
	request.Model = strings.TrimSpace(upstreamModel)
	if request.Model == "" || len(request.Inputs) == 0 {
		return nil, ErrInvalidCanonicalRequest
	}
	payload := map[string]any{
		"model": request.Model, "input": request.Inputs, "encoding_format": request.EncodingFormat,
	}
	if request.Dimensions != nil {
		payload["dimensions"] = *request.Dimensions
	}
	if request.ClientUser != "" {
		payload["user"] = request.ClientUser
	}
	return json.Marshal(payload)
}

func decodeEmbeddingInputs(raw json.RawMessage) ([]string, error) {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return validateEmbeddingInputs([]string{single})
	}
	var multiple []string
	if json.Unmarshal(raw, &multiple) != nil {
		return nil, fmt.Errorf("%w: input must be a string or string array", ErrInvalidCanonicalRequest)
	}
	return validateEmbeddingInputs(multiple)
}

func validateEmbeddingInputs(inputs []string) ([]string, error) {
	if len(inputs) == 0 || len(inputs) > MaxEmbeddingInputs {
		return nil, fmt.Errorf("%w: input count must be between 1 and %d", ErrInvalidCanonicalRequest, MaxEmbeddingInputs)
	}
	total := 0
	for _, input := range inputs {
		if input == "" || len(input) > MaxEmbeddingInputItemBytes {
			return nil, fmt.Errorf("%w: embedding input item is empty or too large", ErrInvalidCanonicalRequest)
		}
		total += len(input)
		if total > MaxEmbeddingTotalInputBytes {
			return nil, fmt.Errorf("%w: embedding input is too large", ErrInvalidCanonicalRequest)
		}
	}
	return append([]string(nil), inputs...), nil
}

func embeddingInputBytes(inputs []string) int {
	total := 0
	for _, input := range inputs {
		total += len(input)
	}
	return total
}
