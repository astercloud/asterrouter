package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream/eventstreamapi"
	"github.com/gin-gonic/gin"
)

type bedrockTestEvent struct {
	messageType string
	eventType   string
	payload     string
}

func encodeBedrockTestStream(t *testing.T, events ...bedrockTestEvent) []byte {
	t.Helper()
	var out bytes.Buffer
	encoder := eventstream.NewEncoder()
	for _, event := range events {
		headers := eventstream.Headers{}
		headers.Set(eventstreamapi.MessageTypeHeader, eventstream.StringValue(event.messageType))
		if event.messageType == eventstreamapi.EventMessageType {
			headers.Set(eventstreamapi.EventTypeHeader, eventstream.StringValue(event.eventType))
		} else if event.messageType == eventstreamapi.ExceptionMessageType {
			headers.Set(eventstreamapi.ExceptionTypeHeader, eventstream.StringValue(event.eventType))
		}
		headers.Set(eventstreamapi.ContentTypeHeader, eventstream.StringValue("application/json"))
		if err := encoder.Encode(&out, eventstream.Message{Headers: headers, Payload: []byte(event.payload)}); err != nil {
			t.Fatal(err)
		}
	}
	return out.Bytes()
}

func canonicalOpenAIStreamRequest(t *testing.T) gatewaycore.CanonicalRequest {
	t.Helper()
	request, err := gatewaycore.CanonicalizeOpenAIChat([]byte(`{"model":"claude","stream":true,"messages":[{"role":"user","content":"hello"}]}`), http.Header{})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestPrepareCanonicalTextStreamResponseAcceptsAndReplaysBedrock(t *testing.T) {
	raw := encodeBedrockTestStream(t,
		bedrockTestEvent{messageType: "event", eventType: "messageStart", payload: `{"role":"assistant"}`},
		bedrockTestEvent{messageType: "event", eventType: "contentBlockDelta", payload: `{"contentBlockIndex":0,"delta":{"text":"hello"}}`},
		bedrockTestEvent{messageType: "event", eventType: "messageStop", payload: `{"stopReason":"end_turn"}`},
		bedrockTestEvent{messageType: "event", eventType: "metadata", payload: `{"usage":{"inputTokens":4,"outputTokens":2,"totalTokens":6}}`},
	)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/vnd.amazon.eventstream"}}, Body: io.NopCloser(bytes.NewReader(raw))}
	provider := controlplane.GatewayProvider{UpstreamModel: "claude", UpstreamFormat: controlplane.UpstreamFormatBedrockConverse}
	if err := prepareCanonicalTextStreamResponse(resp, canonicalOpenAIStreamRequest(t), provider); err != nil {
		t.Fatal(err)
	}
	replayed, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(replayed, raw) {
		t.Fatalf("replayed Bedrock stream differs: got %d bytes want %d", len(replayed), len(raw))
	}
}

func TestStreamCanonicalGatewayResponseTranslatesBedrock(t *testing.T) {
	raw := encodeBedrockTestStream(t,
		bedrockTestEvent{messageType: "event", eventType: "messageStart", payload: `{"role":"assistant"}`},
		bedrockTestEvent{messageType: "event", eventType: "contentBlockDelta", payload: `{"contentBlockIndex":0,"delta":{"text":"hello"}}`},
		bedrockTestEvent{messageType: "event", eventType: "messageStop", payload: `{"stopReason":"end_turn"}`},
		bedrockTestEvent{messageType: "event", eventType: "metadata", payload: `{"usage":{"inputTokens":4,"outputTokens":2,"totalTokens":6}}`},
	)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/vnd.amazon.eventstream"}}, Body: io.NopCloser(bytes.NewReader(raw))}
	provider := controlplane.GatewayProvider{UpstreamModel: "claude", UpstreamFormat: controlplane.UpstreamFormatBedrockConverse}
	usage, _, err := streamCanonicalGatewayResponse(context, resp, canonicalOpenAIStreamRequest(t), provider, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"content":"hello"`) || !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("translated stream = %s", body)
	}
	if strings.Index(body, `"prompt_tokens":4`) > strings.Index(body, "data: [DONE]") {
		t.Fatalf("usage was emitted after terminal event: %s", body)
	}
	if usage.InputTokens != 4 || usage.OutputTokens != 2 {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestPrepareCanonicalTextStreamResponseRejectsBedrockException(t *testing.T) {
	raw := encodeBedrockTestStream(t, bedrockTestEvent{messageType: "exception", eventType: "throttlingException", payload: `{"message":"synthetic throttle"}`})
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/vnd.amazon.eventstream"}}, Body: io.NopCloser(bytes.NewReader(raw))}
	provider := controlplane.GatewayProvider{UpstreamModel: "claude", UpstreamFormat: controlplane.UpstreamFormatBedrockConverse}
	err := prepareCanonicalTextStreamResponse(resp, canonicalOpenAIStreamRequest(t), provider)
	if err == nil || !strings.Contains(err.Error(), "throttlingException") {
		t.Fatalf("error = %v", err)
	}
}
