package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

func translateGatewayEmbeddingResponse(request gatewaycore.CanonicalRequest, body []byte) ([]byte, int, error) {
	if request.Embedding == nil {
		return nil, 0, errors.New("canonical embedding request is missing")
	}
	var payload struct {
		Object string `json:"object"`
		Data   []struct {
			Object    string          `json:"object"`
			Embedding json.RawMessage `json:"embedding"`
			Index     int             `json:"index"`
		} `json:"data"`
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Object != "list" || len(payload.Data) != len(request.Embedding.Inputs) {
		return nil, 0, errors.New("upstream embedding response has invalid object or item count")
	}
	seen := make(map[int]struct{}, len(payload.Data))
	dimensions := 0
	data := make([]map[string]any, 0, len(payload.Data))
	for _, item := range payload.Data {
		if item.Index < 0 || item.Index >= len(payload.Data) {
			return nil, 0, errors.New("upstream embedding response has an invalid index")
		}
		if _, duplicate := seen[item.Index]; duplicate {
			return nil, 0, errors.New("upstream embedding response has duplicate indices")
		}
		seen[item.Index] = struct{}{}
		itemDimensions, embedding, err := validateEmbeddingValue(item.Embedding, request.Embedding.EncodingFormat)
		if err != nil {
			return nil, 0, err
		}
		if dimensions == 0 {
			dimensions = itemDimensions
		} else if dimensions != itemDimensions {
			return nil, 0, errors.New("upstream embedding vectors have inconsistent dimensions")
		}
		data = append(data, map[string]any{"object": "embedding", "embedding": embedding, "index": item.Index})
	}
	if request.Embedding.Dimensions != nil && dimensions != *request.Embedding.Dimensions {
		return nil, 0, fmt.Errorf("upstream embedding dimensions=%d, requested=%d", dimensions, *request.Embedding.Dimensions)
	}
	sort.Slice(data, func(i, j int) bool { return data[i]["index"].(int) < data[j]["index"].(int) })
	result := map[string]any{"object": "list", "data": data, "model": request.Model}
	if len(payload.Usage) > 0 && string(payload.Usage) != "null" {
		result["usage"] = payload.Usage
	}
	encoded, err := json.Marshal(result)
	return encoded, dimensions, err
}

func validateEmbeddingValue(raw json.RawMessage, encodingFormat string) (int, any, error) {
	switch encodingFormat {
	case "float":
		var vector []float64
		if json.Unmarshal(raw, &vector) != nil || len(vector) == 0 || len(vector) > gatewaycore.MaxEmbeddingDimensions {
			return 0, nil, errors.New("upstream float embedding is invalid")
		}
		return len(vector), vector, nil
	case "base64":
		var value string
		if json.Unmarshal(raw, &value) != nil || value == "" {
			return 0, nil, errors.New("upstream base64 embedding is invalid")
		}
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(decoded) == 0 || len(decoded)%4 != 0 || len(decoded)/4 > gatewaycore.MaxEmbeddingDimensions {
			return 0, nil, errors.New("upstream base64 embedding is invalid")
		}
		return len(decoded) / 4, value, nil
	default:
		return 0, nil, errors.New("embedding encoding format is unsupported")
	}
}
