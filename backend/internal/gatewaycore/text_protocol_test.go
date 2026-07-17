package gatewaycore

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCanonicalTextRequestProtocolMatrix(t *testing.T) {
	tests := []struct {
		name     string
		protocol Protocol
		model    string
		stream   *bool
		body     string
	}{
		{name: "openai chat", protocol: ProtocolOpenAIChat, body: `{"model":"public-model","messages":[{"role":"system","content":"policy"},{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup","description":"Lookup","parameters":{"type":"object","properties":{"id":{"type":"string"}}}}}],"tool_choice":"auto","max_completion_tokens":128,"stream":true}`},
		{name: "openai responses", protocol: ProtocolOpenAIResponses, body: `{"model":"public-model","instructions":"policy","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}],"tools":[{"type":"function","name":"lookup","description":"Lookup","parameters":{"type":"object","properties":{}}}],"max_output_tokens":128,"stream":true}`},
		{name: "anthropic", protocol: ProtocolAnthropicMessages, body: `{"model":"public-model","system":"policy","messages":[{"role":"user","content":"hello"}],"tools":[{"name":"lookup","description":"Lookup","input_schema":{"type":"object","properties":{}}}],"max_tokens":128,"stream":true}`},
		{name: "gemini", protocol: ProtocolGeminiGenerate, model: "public-model", stream: boolPointer(true), body: `{"systemInstruction":{"parts":[{"text":"policy"}]},"contents":[{"role":"user","parts":[{"text":"hello"}]}],"tools":[{"functionDeclarations":[{"name":"lookup","description":"Lookup","parameters":{"type":"object","properties":{}}}]}],"generationConfig":{"maxOutputTokens":128}}`},
	}
	formats := []UpstreamFormat{UpstreamFormatOpenAIChat, UpstreamFormatOpenAIResponses, UpstreamFormatAnthropic, UpstreamFormatGemini, UpstreamFormatBedrockConverse}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := DecodeCanonicalTextRequest(test.protocol, []byte(test.body), test.model, test.stream)
			if err != nil {
				t.Fatalf("DecodeCanonicalTextRequest(): %v", err)
			}
			if request.Model != "public-model" || len(request.System) != 1 || len(request.Messages) != 1 || len(request.Tools) != 1 || !request.Stream {
				t.Fatalf("request=%+v", request)
			}
			for _, format := range formats {
				encoded, err := EncodeCanonicalTextRequest(request, format, "upstream-model")
				if err != nil {
					t.Fatalf("format %s: %v", format, err)
				}
				if !json.Valid(encoded) || !strings.Contains(string(encoded), "hello") || strings.Contains(string(encoded), "public-model") {
					t.Fatalf("format %s body=%s", format, encoded)
				}
			}
		})
	}
}

func TestCanonicalTextRequestRejectsLossyFeatures(t *testing.T) {
	tests := []struct {
		protocol Protocol
		model    string
		body     string
	}{
		{protocol: ProtocolOpenAIChat, body: `{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/a.png"}}]}]}`},
		{protocol: ProtocolOpenAIResponses, body: `{"model":"m","input":"hello","reasoning":{"effort":"high"}}`},
		{protocol: ProtocolAnthropicMessages, body: `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"enabled","budget_tokens":32}}`},
		{protocol: ProtocolGeminiGenerate, model: "m", body: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"safetySettings":[{"category":"HARM_CATEGORY_HATE_SPEECH","threshold":"BLOCK_NONE"}]}`},
	}
	for _, test := range tests {
		_, err := DecodeCanonicalTextRequest(test.protocol, []byte(test.body), test.model, nil)
		if !errors.Is(err, ErrUnsupportedTextFeature) {
			t.Fatalf("protocol=%s error=%v", test.protocol, err)
		}
	}

	request, err := DecodeCanonicalTextRequest(ProtocolOpenAIChat, []byte(`{"model":"m","messages":[{"role":"user","content":"hello"}],"response_format":{"type":"json_object"}}`), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeCanonicalTextRequest(request, UpstreamFormatAnthropic, "claude"); !errors.Is(err, ErrUnsupportedTextFeature) {
		t.Fatalf("Anthropic response format error=%v", err)
	}
}

func TestCanonicalAnthropicRequestRequiresPositiveMaxTokens(t *testing.T) {
	tests := []string{
		`{"model":"m","messages":[{"role":"user","content":"hello"}]}`,
		`{"model":"m","max_tokens":0,"messages":[{"role":"user","content":"hello"}]}`,
	}
	for _, body := range tests {
		if _, err := DecodeCanonicalTextRequest(ProtocolAnthropicMessages, []byte(body), "", nil); !errors.Is(err, ErrInvalidCanonicalRequest) {
			t.Fatalf("body=%s error=%v", body, err)
		}
	}
}

func TestCanonicalTextRequestInfersGeminiToolResultName(t *testing.T) {
	request, err := DecodeCanonicalTextRequest(ProtocolOpenAIChat, []byte(`{
		"model":"m",
		"messages":[
			{"role":"user","content":"lookup"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"id\":\"1\"}"}}]},
			{"role":"tool","tool_call_id":"call-1","content":"done"}
		]
	}`), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeCanonicalTextRequest(request, UpstreamFormatGemini, "gemini")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"functionResponse":{"id":"call-1","name":"lookup"`) {
		t.Fatalf("Gemini request = %s", encoded)
	}
}

func TestCanonicalTextRequestDisablesBedrockToolsWithoutSilentDrop(t *testing.T) {
	request, err := DecodeCanonicalTextRequest(ProtocolOpenAIChat, []byte(`{
		"model":"m","messages":[{"role":"user","content":"hello"}],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],
		"tool_choice":"none"
	}`), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeCanonicalTextRequest(request, UpstreamFormatBedrockConverse, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "toolConfig") {
		t.Fatalf("Bedrock request enabled tools despite tool_choice=none: %s", encoded)
	}
}

func TestCanonicalTextResponseProtocolMatrix(t *testing.T) {
	upstreams := []struct {
		format UpstreamFormat
		body   string
	}{
		{format: UpstreamFormatOpenAIChat, body: `{"id":"resp-1","model":"upstream","choices":[{"message":{"role":"assistant","content":"done","tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"id\":\"1\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`},
		{format: UpstreamFormatOpenAIResponses, body: `{"id":"resp-1","model":"upstream","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]},{"type":"function_call","call_id":"call-1","name":"lookup","arguments":"{\"id\":\"1\"}"}],"usage":{"input_tokens":10,"output_tokens":3}}`},
		{format: UpstreamFormatAnthropic, body: `{"id":"resp-1","model":"upstream","content":[{"type":"text","text":"done"},{"type":"tool_use","id":"call-1","name":"lookup","input":{"id":"1"}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":3}}`},
		{format: UpstreamFormatGemini, body: `{"responseId":"resp-1","modelVersion":"upstream","candidates":[{"content":{"role":"model","parts":[{"text":"done"},{"functionCall":{"id":"call-1","name":"lookup","args":{"id":"1"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3}}`},
		{format: UpstreamFormatBedrockConverse, body: `{"output":{"message":{"role":"assistant","content":[{"text":"done"},{"toolUse":{"toolUseId":"call-1","name":"lookup","input":{"id":"1"}}}]}},"stopReason":"tool_use","usage":{"inputTokens":10,"outputTokens":3}}`},
	}
	clients := []Protocol{ProtocolOpenAIChat, ProtocolOpenAIResponses, ProtocolAnthropicMessages, ProtocolGeminiGenerate}
	for _, upstream := range upstreams {
		response, err := DecodeCanonicalTextResponse(upstream.format, []byte(upstream.body), "upstream")
		if err != nil {
			t.Fatalf("format %s: %v", upstream.format, err)
		}
		response.Model = "public-model"
		if response.Usage.InputTokens != 10 || response.Usage.OutputTokens != 3 || !hasToolCall(response.Content) {
			t.Fatalf("format %s response=%+v", upstream.format, response)
		}
		for _, client := range clients {
			encoded, err := EncodeCanonicalTextResponse(client, response)
			if err != nil || !json.Valid(encoded) || !strings.Contains(string(encoded), "public-model") || !strings.Contains(string(encoded), "lookup") {
				t.Fatalf("format=%s client=%s body=%s err=%v", upstream.format, client, encoded, err)
			}
		}
	}
}

func TestCanonicalTextResponseNormalizesGeminiSPIIStopReason(t *testing.T) {
	response, err := DecodeCanonicalTextResponse(UpstreamFormatGemini, []byte(`{
		"responseId":"resp-safety",
		"modelVersion":"gemini",
		"candidates":[{"finishReason":"SPII"}],
		"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":1}
	}`), "gemini")
	if err != nil {
		t.Fatal(err)
	}
	if response.StopReason != "content_filter" {
		t.Fatalf("stop reason = %q", response.StopReason)
	}
	if len(response.Content) != 0 {
		t.Fatalf("blocked response content = %+v", response.Content)
	}
	encoded, err := EncodeCanonicalTextResponse(ProtocolOpenAIChat, response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"finish_reason":"content_filter"`) {
		t.Fatalf("OpenAI response = %s", encoded)
	}
}

func TestCanonicalStreamTranslatesAnthropicToOpenAI(t *testing.T) {
	decoder := NewTextStreamDecoder(UpstreamFormatAnthropic, "claude")
	encoder := NewTextStreamEncoder(ProtocolOpenAIChat)
	inputs := []struct{ event, data string }{
		{event: "message_start", data: `{"type":"message_start","message":{"id":"msg-1","model":"claude","usage":{"input_tokens":7}}}`},
		{event: "content_block_start", data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{event: "content_block_delta", data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`},
		{event: "message_delta", data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`},
	}
	var output strings.Builder
	for _, input := range inputs {
		events, err := decoder.Feed(input.event, []byte(input.data))
		if err != nil {
			t.Fatal(err)
		}
		chunks, err := encoder.Encode(events)
		if err != nil {
			t.Fatal(err)
		}
		for _, chunk := range chunks {
			output.Write(chunk)
		}
	}
	if _, err := decoder.Finish(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"content":"hello"`) || !strings.Contains(output.String(), `"finish_reason":"stop"`) || !strings.Contains(output.String(), "data: [DONE]") {
		t.Fatalf("output=%s", output.String())
	}
}

func TestCanonicalResponsesStreamMaintainsOutputIndexesAndFinalOutput(t *testing.T) {
	encoder := NewTextStreamEncoder(ProtocolOpenAIResponses)
	events := []CanonicalTextResponseEvent{
		{Type: TextEventStart, ID: "resp-1", Model: "public-model"},
		{Type: TextEventToolCallStart, ToolIndex: 4, ToolCall: &TextToolCall{ID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{}`)}},
		{Type: TextEventToolCallDelta, ToolIndex: 4, Arguments: `{"id":"1"}`},
		{Type: TextEventTextDelta, Text: "done"},
		{Type: TextEventUsage, Usage: &TextUsage{InputTokens: 3, OutputTokens: 2}},
		{Type: TextEventFinish, StopReason: "tool_use"},
	}
	chunks, err := encoder.Encode(events)
	if err != nil {
		t.Fatal(err)
	}
	output := string(bytes.Join(chunks, nil))
	if !strings.Contains(output, `"type":"response.function_call_arguments.delta"`) || !strings.Contains(output, `"output_index":0`) {
		t.Fatalf("tool output index is invalid: %s", output)
	}
	if !strings.Contains(output, `"type":"response.output_text.delta"`) || !strings.Contains(output, `"output_index":1`) {
		t.Fatalf("text output index is invalid: %s", output)
	}
	if !strings.Contains(output, `"status":"completed"`) || !strings.Contains(output, `"text":"done"`) || !strings.Contains(output, `"arguments":"{\"id\":\"1\"}"`) {
		t.Fatalf("final response output is incomplete: %s", output)
	}
}

func TestCanonicalAnthropicToolOnlyStreamStartsAtBlockZero(t *testing.T) {
	encoder := NewTextStreamEncoder(ProtocolAnthropicMessages)
	chunks, err := encoder.Encode([]CanonicalTextResponseEvent{
		{Type: TextEventStart, ID: "msg-1", Model: "claude"},
		{Type: TextEventToolCallStart, ToolIndex: 3, ToolCall: &TextToolCall{ID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{}`)}},
		{Type: TextEventToolCallDelta, ToolIndex: 3, Arguments: `{"id":"1"}`},
		{Type: TextEventFinish, StopReason: "tool_use"},
	})
	if err != nil {
		t.Fatal(err)
	}
	output := string(bytes.Join(chunks, nil))
	if !strings.Contains(output, `"index":0`) || strings.Contains(output, `"index":1`) {
		t.Fatalf("Anthropic tool-only stream has a non-contiguous block index: %s", output)
	}
}

func TestCanonicalOpenAIChatStreamEmitsUsageBeforeDone(t *testing.T) {
	decoder := NewTextStreamDecoder(UpstreamFormatOpenAIChat, "upstream")
	encoder := NewTextStreamEncoder(ProtocolOpenAIChat)
	inputs := []string{
		`{"id":"chatcmpl-1","model":"upstream","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`,
		`{"id":"chatcmpl-1","model":"upstream","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`,
		`[DONE]`,
	}
	var output strings.Builder
	for _, input := range inputs {
		events, err := decoder.Feed("", []byte(input))
		if err != nil {
			t.Fatal(err)
		}
		chunks, err := encoder.Encode(events)
		if err != nil {
			t.Fatal(err)
		}
		for _, chunk := range chunks {
			output.Write(chunk)
		}
	}
	usageIndex := strings.Index(output.String(), `"usage":{"completion_tokens":2`)
	doneIndex := strings.Index(output.String(), "data: [DONE]")
	if usageIndex < 0 || doneIndex < 0 || usageIndex > doneIndex {
		t.Fatalf("usage must precede [DONE]: %s", output.String())
	}
}

func TestCanonicalGeminiStreamAcceptsUsageAfterFinishChunk(t *testing.T) {
	decoder := NewTextStreamDecoder(UpstreamFormatGemini, "gemini")
	encoder := NewTextStreamEncoder(ProtocolOpenAIChat)
	inputs := []string{
		`{"responseId":"gemini-1","modelVersion":"gemini","candidates":[{"content":{"role":"model","parts":[{"text":"done"}]},"finishReason":"STOP"}]}`,
		`{"responseId":"gemini-1","modelVersion":"gemini","usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3}}`,
	}
	var output strings.Builder
	for _, input := range inputs {
		events, err := decoder.Feed("", []byte(input))
		if err != nil {
			t.Fatal(err)
		}
		chunks, err := encoder.Encode(events)
		if err != nil {
			t.Fatal(err)
		}
		for _, chunk := range chunks {
			output.Write(chunk)
		}
	}
	finish, err := decoder.Finish()
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := encoder.Encode(finish)
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range chunks {
		output.Write(chunk)
	}
	usageIndex := strings.Index(output.String(), `"usage":{"completion_tokens":3`)
	doneIndex := strings.Index(output.String(), "data: [DONE]")
	if usageIndex < 0 || doneIndex < 0 || usageIndex > doneIndex {
		t.Fatalf("usage must precede [DONE]: %s", output.String())
	}
}

func boolPointer(value bool) *bool { return &value }
