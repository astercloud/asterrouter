package gatewaycore

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestCanonicalizeOpenAIEmbeddingNormalizesSupportedInputs(t *testing.T) {
	dimensions := 3
	request, err := CanonicalizeOpenAIEmbedding([]byte(`{"model":"published-embedding","input":["first","second"],"encoding_format":"float","dimensions":3,"user":"subject"}`), http.Header{"X-Request-Id": []string{"embedding-1"}})
	if err != nil {
		t.Fatalf("CanonicalizeOpenAIEmbedding(): %v", err)
	}
	if request.ID != "op_embedding-1" || request.Protocol != ProtocolOpenAIEmbeddings || request.Operation != "embedding" || request.Modality != "embedding" || request.Lane != LaneDirect || request.Stream || request.Model != "published-embedding" || request.MessageCount != 2 || request.Embedding == nil || request.Embedding.Dimensions == nil || *request.Embedding.Dimensions != dimensions {
		t.Fatalf("canonical embedding request=%+v", request)
	}
	body, err := EncodeOpenAIEmbeddingRequest(*request.Embedding, "upstream-embedding")
	if err != nil {
		t.Fatalf("EncodeOpenAIEmbeddingRequest(): %v", err)
	}
	if !bytes.Contains(body, []byte(`"model":"upstream-embedding"`)) || !bytes.Contains(body, []byte(`"input":["first","second"]`)) || !bytes.Contains(body, []byte(`"dimensions":3`)) {
		t.Fatalf("encoded embedding request=%s", body)
	}
}

func TestCanonicalizeOpenAIEmbeddingRejectsUnsupportedOrOversizedInput(t *testing.T) {
	tests := []string{
		`{"model":"embedding","input":"","encoding_format":"float"}`,
		`{"model":"embedding","input":"hello","encoding_format":"hex"}`,
		`{"model":"embedding","input":"hello","dimensions":0}`,
		`{"model":"embedding","input":["hello",1]}`,
		`{"model":"embedding","input":"hello","extra":true}`,
		`{"model":"embedding","input":"` + strings.Repeat("x", MaxEmbeddingInputItemBytes+1) + `"}`,
	}
	for _, body := range tests {
		if _, err := CanonicalizeOpenAIEmbedding([]byte(body), nil); !errors.Is(err, ErrInvalidCanonicalRequest) && !errors.Is(err, ErrUnsupportedEmbeddingFeature) {
			t.Fatalf("body length=%d error=%v", len(body), err)
		}
	}
}
