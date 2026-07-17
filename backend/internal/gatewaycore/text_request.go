package gatewaycore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type TextRole string

const (
	TextRoleUser      TextRole = "user"
	TextRoleAssistant TextRole = "assistant"
)

type TextContentKind string

const (
	TextContentText       TextContentKind = "text"
	TextContentToolCall   TextContentKind = "tool_call"
	TextContentToolResult TextContentKind = "tool_result"
)

type TextContent struct {
	Kind       TextContentKind `json:"kind"`
	Text       string          `json:"text,omitempty"`
	ToolCall   *TextToolCall   `json:"tool_call,omitempty"`
	ToolResult *TextToolResult `json:"tool_result,omitempty"`
}

type TextToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type TextToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name,omitempty"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
}

type TextMessage struct {
	Role    TextRole      `json:"role"`
	Content []TextContent `json:"content"`
}

type TextTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type TextToolChoice struct {
	Mode string `json:"mode"`
	Name string `json:"name,omitempty"`
}

type TextGenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"top_p,omitempty"`
	TopK            *int     `json:"top_k,omitempty"`
	MaxOutputTokens *int     `json:"max_output_tokens,omitempty"`
	StopSequences   []string `json:"stop_sequences,omitempty"`
}

type TextResponseFormat struct {
	Type   string          `json:"type"`
	Name   string          `json:"name,omitempty"`
	Schema json.RawMessage `json:"schema,omitempty"`
	Strict bool            `json:"strict,omitempty"`
}

type CanonicalTextRequest struct {
	Model          string               `json:"model"`
	System         []string             `json:"system,omitempty"`
	Messages       []TextMessage        `json:"messages"`
	Tools          []TextTool           `json:"tools,omitempty"`
	ToolChoice     TextToolChoice       `json:"tool_choice,omitempty"`
	Generation     TextGenerationConfig `json:"generation,omitempty"`
	ResponseFormat TextResponseFormat   `json:"response_format,omitempty"`
	Stream         bool                 `json:"stream"`
	ClientUser     string               `json:"client_user,omitempty"`
}

type UpstreamFormat string

const (
	UpstreamFormatOpenAIChat      UpstreamFormat = "openai_chat"
	UpstreamFormatOpenAIResponses UpstreamFormat = "openai_responses"
	UpstreamFormatAnthropic       UpstreamFormat = "anthropic_messages"
	UpstreamFormatGemini          UpstreamFormat = "gemini_generate_content"
	UpstreamFormatBedrockConverse UpstreamFormat = "bedrock_converse"
)

var ErrUnsupportedTextFeature = errors.New("unsupported text protocol feature")

type UnsupportedTextFeatureError struct {
	Feature string
	Detail  string
}

func (e *UnsupportedTextFeatureError) Error() string {
	if strings.TrimSpace(e.Detail) == "" {
		return fmt.Sprintf("%s: %s", ErrUnsupportedTextFeature, e.Feature)
	}
	return fmt.Sprintf("%s: %s (%s)", ErrUnsupportedTextFeature, e.Feature, e.Detail)
}

func (e *UnsupportedTextFeatureError) Unwrap() error { return ErrUnsupportedTextFeature }

func unsupportedTextFeature(feature, detail string) error {
	return &UnsupportedTextFeatureError{Feature: feature, Detail: detail}
}

func DecodeCanonicalTextRequest(protocol Protocol, raw []byte, modelOverride string, streamOverride *bool) (CanonicalTextRequest, error) {
	switch protocol {
	case ProtocolOpenAIChat:
		return decodeOpenAIChatRequest(raw)
	case ProtocolOpenAIResponses:
		return decodeOpenAIResponsesRequest(raw)
	case ProtocolAnthropicMessages:
		return decodeAnthropicRequest(raw)
	case ProtocolGeminiGenerate:
		return decodeGeminiRequest(raw, modelOverride, streamOverride)
	default:
		return CanonicalTextRequest{}, unsupportedTextFeature("client_protocol", string(protocol))
	}
}

func EncodeCanonicalTextRequest(request CanonicalTextRequest, format UpstreamFormat, upstreamModel string) ([]byte, error) {
	request.Model = strings.TrimSpace(upstreamModel)
	if request.Model == "" {
		return nil, fmt.Errorf("%w: upstream model is required", ErrInvalidCanonicalRequest)
	}
	if err := validateCanonicalTextRequest(request); err != nil {
		return nil, err
	}
	switch format {
	case UpstreamFormatOpenAIChat:
		return encodeOpenAIChatRequest(request)
	case UpstreamFormatOpenAIResponses:
		return encodeOpenAIResponsesRequest(request)
	case UpstreamFormatAnthropic:
		return encodeAnthropicRequest(request)
	case UpstreamFormatGemini:
		return encodeGeminiRequest(request)
	case UpstreamFormatBedrockConverse:
		return encodeBedrockConverseRequest(request)
	default:
		return nil, unsupportedTextFeature("upstream_format", string(format))
	}
}

func CanonicalTextRequestSupportsFormat(request CanonicalTextRequest, format UpstreamFormat) error {
	_, err := EncodeCanonicalTextRequest(request, format, "capability-check")
	return err
}

func validateCanonicalTextRequest(request CanonicalTextRequest) error {
	if strings.TrimSpace(request.Model) == "" || len(request.Messages) == 0 {
		return fmt.Errorf("%w: model and messages are required", ErrInvalidCanonicalRequest)
	}
	for i, message := range request.Messages {
		if message.Role != TextRoleUser && message.Role != TextRoleAssistant {
			return fmt.Errorf("%w: messages[%d] has invalid role", ErrInvalidCanonicalRequest, i)
		}
		if len(message.Content) == 0 {
			return fmt.Errorf("%w: messages[%d] has no content", ErrInvalidCanonicalRequest, i)
		}
		for j, content := range message.Content {
			switch content.Kind {
			case TextContentText:
			case TextContentToolCall:
				if content.ToolCall == nil || strings.TrimSpace(content.ToolCall.ID) == "" || strings.TrimSpace(content.ToolCall.Name) == "" || !json.Valid(content.ToolCall.Arguments) {
					return fmt.Errorf("%w: messages[%d].content[%d] has invalid tool call", ErrInvalidCanonicalRequest, i, j)
				}
			case TextContentToolResult:
				if content.ToolResult == nil || strings.TrimSpace(content.ToolResult.ToolCallID) == "" {
					return fmt.Errorf("%w: messages[%d].content[%d] has invalid tool result", ErrInvalidCanonicalRequest, i, j)
				}
			default:
				return fmt.Errorf("%w: messages[%d].content[%d] has invalid kind", ErrInvalidCanonicalRequest, i, j)
			}
		}
	}
	for i, tool := range request.Tools {
		if strings.TrimSpace(tool.Name) == "" || !json.Valid(tool.InputSchema) {
			return fmt.Errorf("%w: tools[%d] is invalid", ErrInvalidCanonicalRequest, i)
		}
	}
	if request.ToolChoice.Mode == "named" && strings.TrimSpace(request.ToolChoice.Name) == "" {
		return fmt.Errorf("%w: named tool choice requires a name", ErrInvalidCanonicalRequest)
	}
	return nil
}

func decodeOpenAIChatRequest(raw []byte) (CanonicalTextRequest, error) {
	var payload struct {
		Model               string            `json:"model"`
		Messages            []json.RawMessage `json:"messages"`
		Stream              bool              `json:"stream"`
		Tools               []json.RawMessage `json:"tools"`
		ToolChoice          json.RawMessage   `json:"tool_choice"`
		Temperature         *float64          `json:"temperature"`
		TopP                *float64          `json:"top_p"`
		MaxTokens           *int              `json:"max_tokens"`
		MaxCompletionTokens *int              `json:"max_completion_tokens"`
		Stop                json.RawMessage   `json:"stop"`
		ResponseFormat      json.RawMessage   `json:"response_format"`
		User                string            `json:"user"`
		N                   *int              `json:"n"`
		Logprobs            json.RawMessage   `json:"logprobs"`
		Modalities          json.RawMessage   `json:"modalities"`
		Audio               json.RawMessage   `json:"audio"`
		Prediction          json.RawMessage   `json:"prediction"`
	}
	if len(raw) == 0 || decodeStrictTextJSON(raw, &payload) != nil {
		return CanonicalTextRequest{}, ErrInvalidCanonicalRequest
	}
	if payload.N != nil && *payload.N != 1 {
		return CanonicalTextRequest{}, unsupportedTextFeature("n", "only one choice can be routed losslessly")
	}
	for name, value := range map[string]json.RawMessage{"logprobs": payload.Logprobs, "modalities": payload.Modalities, "audio": payload.Audio, "prediction": payload.Prediction} {
		if rawJSONPresent(value) {
			return CanonicalTextRequest{}, unsupportedTextFeature(name, "not represented by the canonical text contract")
		}
	}
	request := CanonicalTextRequest{
		Model: payload.Model, Stream: payload.Stream, ClientUser: strings.TrimSpace(payload.User),
		Generation: TextGenerationConfig{Temperature: payload.Temperature, TopP: payload.TopP},
	}
	if payload.MaxCompletionTokens != nil {
		request.Generation.MaxOutputTokens = payload.MaxCompletionTokens
	} else {
		request.Generation.MaxOutputTokens = payload.MaxTokens
	}
	var err error
	request.Generation.StopSequences, err = decodeStopSequences(payload.Stop)
	if err != nil {
		return CanonicalTextRequest{}, err
	}
	request.ResponseFormat, err = decodeOpenAIResponseFormat(payload.ResponseFormat)
	if err != nil {
		return CanonicalTextRequest{}, err
	}
	request.Tools, err = decodeOpenAITools(payload.Tools)
	if err != nil {
		return CanonicalTextRequest{}, err
	}
	request.ToolChoice, err = decodeOpenAIToolChoice(payload.ToolChoice)
	if err != nil {
		return CanonicalTextRequest{}, err
	}
	for i, rawMessage := range payload.Messages {
		message, system, err := decodeOpenAIMessage(rawMessage)
		if err != nil {
			return CanonicalTextRequest{}, fmt.Errorf("messages[%d]: %w", i, err)
		}
		if system != "" {
			request.System = append(request.System, system)
			continue
		}
		request.Messages = append(request.Messages, message)
	}
	if err := validateCanonicalTextRequest(request); err != nil {
		return CanonicalTextRequest{}, err
	}
	return request, nil
}

func decodeOpenAIMessage(raw json.RawMessage) (TextMessage, string, error) {
	var message struct {
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
		ToolCallID string `json:"tool_call_id"`
		Name       string `json:"name"`
	}
	if json.Unmarshal(raw, &message) != nil {
		return TextMessage{}, "", ErrInvalidCanonicalRequest
	}
	role := strings.ToLower(strings.TrimSpace(message.Role))
	content, err := decodeOpenAIContent(message.Content)
	if err != nil {
		return TextMessage{}, "", err
	}
	switch role {
	case "system", "developer":
		if len(content) != 1 || content[0].Kind != TextContentText {
			return TextMessage{}, "", unsupportedTextFeature("system_content", "system messages must contain text only")
		}
		return TextMessage{}, content[0].Text, nil
	case "user", "assistant":
		out := TextMessage{Role: TextRole(role), Content: content}
		for _, call := range message.ToolCalls {
			if call.Type != "" && call.Type != "function" {
				return TextMessage{}, "", unsupportedTextFeature("tool_type", call.Type)
			}
			arguments, err := normalizeToolArguments(call.Function.Arguments)
			if err != nil {
				return TextMessage{}, "", err
			}
			out.Content = append(out.Content, TextContent{Kind: TextContentToolCall, ToolCall: &TextToolCall{ID: call.ID, Name: call.Function.Name, Arguments: arguments}})
		}
		if len(out.Content) == 0 {
			return TextMessage{}, "", ErrInvalidCanonicalRequest
		}
		return out, "", nil
	case "tool":
		text, err := textOnlyContent(content)
		if err != nil {
			return TextMessage{}, "", err
		}
		return TextMessage{Role: TextRoleUser, Content: []TextContent{{Kind: TextContentToolResult, ToolResult: &TextToolResult{ToolCallID: message.ToolCallID, Name: message.Name, Content: text}}}}, "", nil
	default:
		return TextMessage{}, "", unsupportedTextFeature("message_role", role)
	}
}

func decodeOpenAIContent(raw json.RawMessage) ([]TextContent, error) {
	if !rawJSONPresent(raw) || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []TextContent{{Kind: TextContentText, Text: text}}, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return nil, ErrInvalidCanonicalRequest
	}
	out := make([]TextContent, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text", "input_text", "output_text":
			out = append(out, TextContent{Kind: TextContentText, Text: part.Text})
		default:
			return nil, unsupportedTextFeature("content_block", part.Type)
		}
	}
	return out, nil
}

func decodeOpenAIResponsesRequest(raw []byte) (CanonicalTextRequest, error) {
	var payload struct {
		Model           string            `json:"model"`
		Input           json.RawMessage   `json:"input"`
		Instructions    json.RawMessage   `json:"instructions"`
		Stream          bool              `json:"stream"`
		Tools           []json.RawMessage `json:"tools"`
		ToolChoice      json.RawMessage   `json:"tool_choice"`
		Temperature     *float64          `json:"temperature"`
		TopP            *float64          `json:"top_p"`
		MaxOutputTokens *int              `json:"max_output_tokens"`
		Text            json.RawMessage   `json:"text"`
		Reasoning       json.RawMessage   `json:"reasoning"`
		PreviousID      string            `json:"previous_response_id"`
		Include         json.RawMessage   `json:"include"`
	}
	if len(raw) == 0 || decodeStrictTextJSON(raw, &payload) != nil {
		return CanonicalTextRequest{}, ErrInvalidCanonicalRequest
	}
	if rawJSONPresent(payload.Reasoning) || payload.PreviousID != "" || rawJSONPresent(payload.Include) {
		return CanonicalTextRequest{}, unsupportedTextFeature("responses_state_or_reasoning", "stateful and reasoning extensions cannot be converted losslessly")
	}
	request := CanonicalTextRequest{Model: payload.Model, Stream: payload.Stream, Generation: TextGenerationConfig{Temperature: payload.Temperature, TopP: payload.TopP, MaxOutputTokens: payload.MaxOutputTokens}}
	if rawJSONPresent(payload.Instructions) {
		var instruction string
		if json.Unmarshal(payload.Instructions, &instruction) != nil {
			return CanonicalTextRequest{}, unsupportedTextFeature("instructions", "only text instructions are supported")
		}
		request.System = append(request.System, instruction)
	}
	var err error
	request.ResponseFormat, err = decodeResponsesTextFormat(payload.Text)
	if err != nil {
		return CanonicalTextRequest{}, err
	}
	request.Tools, err = decodeResponsesTools(payload.Tools)
	if err != nil {
		return CanonicalTextRequest{}, err
	}
	request.ToolChoice, err = decodeOpenAIToolChoice(payload.ToolChoice)
	if err != nil {
		return CanonicalTextRequest{}, err
	}
	request.Messages, err = decodeResponsesInput(payload.Input)
	if err != nil {
		return CanonicalTextRequest{}, err
	}
	if err := validateCanonicalTextRequest(request); err != nil {
		return CanonicalTextRequest{}, err
	}
	return request, nil
}

func decodeResponsesInput(raw json.RawMessage) ([]TextMessage, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []TextMessage{{Role: TextRoleUser, Content: []TextContent{{Kind: TextContentText, Text: text}}}}, nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || len(items) == 0 {
		return nil, fmt.Errorf("%w: input is required", ErrInvalidCanonicalRequest)
	}
	out := make([]TextMessage, 0, len(items))
	for i, itemRaw := range items {
		var item struct {
			Type      string          `json:"type"`
			Role      string          `json:"role"`
			Content   json.RawMessage `json:"content"`
			CallID    string          `json:"call_id"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
			Output    json.RawMessage `json:"output"`
		}
		if json.Unmarshal(itemRaw, &item) != nil {
			return nil, fmt.Errorf("input[%d]: %w", i, ErrInvalidCanonicalRequest)
		}
		switch item.Type {
		case "", "message":
			message, _, err := decodeOpenAIMessage(itemRaw)
			if err != nil {
				return nil, fmt.Errorf("input[%d]: %w", i, err)
			}
			out = append(out, message)
		case "function_call":
			arguments, err := normalizeToolArguments(item.Arguments)
			if err != nil {
				return nil, err
			}
			out = append(out, TextMessage{Role: TextRoleAssistant, Content: []TextContent{{Kind: TextContentToolCall, ToolCall: &TextToolCall{ID: item.CallID, Name: item.Name, Arguments: arguments}}}})
		case "function_call_output":
			var output string
			if json.Unmarshal(item.Output, &output) != nil {
				output = string(item.Output)
			}
			out = append(out, TextMessage{Role: TextRoleUser, Content: []TextContent{{Kind: TextContentToolResult, ToolResult: &TextToolResult{ToolCallID: item.CallID, Content: output}}}})
		default:
			return nil, unsupportedTextFeature("responses_input_item", item.Type)
		}
	}
	return out, nil
}

func decodeAnthropicRequest(raw []byte) (CanonicalTextRequest, error) {
	var payload struct {
		Model         string            `json:"model"`
		System        json.RawMessage   `json:"system"`
		Messages      []json.RawMessage `json:"messages"`
		Stream        bool              `json:"stream"`
		Tools         []json.RawMessage `json:"tools"`
		ToolChoice    json.RawMessage   `json:"tool_choice"`
		Temperature   *float64          `json:"temperature"`
		TopP          *float64          `json:"top_p"`
		TopK          *int              `json:"top_k"`
		MaxTokens     *int              `json:"max_tokens"`
		StopSequences []string          `json:"stop_sequences"`
		Thinking      json.RawMessage   `json:"thinking"`
		MCPServers    json.RawMessage   `json:"mcp_servers"`
	}
	if len(raw) == 0 || decodeStrictTextJSON(raw, &payload) != nil {
		return CanonicalTextRequest{}, ErrInvalidCanonicalRequest
	}
	if payload.MaxTokens == nil || *payload.MaxTokens <= 0 {
		return CanonicalTextRequest{}, fmt.Errorf("%w: max_tokens must be greater than zero", ErrInvalidCanonicalRequest)
	}
	if rawJSONPresent(payload.Thinking) || rawJSONPresent(payload.MCPServers) {
		return CanonicalTextRequest{}, unsupportedTextFeature("anthropic_extension", "thinking and MCP server blocks are provider-specific")
	}
	request := CanonicalTextRequest{Model: payload.Model, Stream: payload.Stream, Generation: TextGenerationConfig{Temperature: payload.Temperature, TopP: payload.TopP, TopK: payload.TopK, MaxOutputTokens: payload.MaxTokens, StopSequences: payload.StopSequences}}
	var err error
	request.System, err = decodeAnthropicSystem(payload.System)
	if err != nil {
		return CanonicalTextRequest{}, err
	}
	request.Tools, err = decodeAnthropicTools(payload.Tools)
	if err != nil {
		return CanonicalTextRequest{}, err
	}
	request.ToolChoice, err = decodeAnthropicToolChoice(payload.ToolChoice)
	if err != nil {
		return CanonicalTextRequest{}, err
	}
	for i, rawMessage := range payload.Messages {
		message, err := decodeAnthropicMessage(rawMessage)
		if err != nil {
			return CanonicalTextRequest{}, fmt.Errorf("messages[%d]: %w", i, err)
		}
		request.Messages = append(request.Messages, message)
	}
	if err := validateCanonicalTextRequest(request); err != nil {
		return CanonicalTextRequest{}, err
	}
	return request, nil
}

func decodeAnthropicSystem(raw json.RawMessage) ([]string, error) {
	if !rawJSONPresent(raw) {
		return nil, nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []string{text}, nil
	}
	var blocks []struct {
		Type         string          `json:"type"`
		Text         string          `json:"text"`
		CacheControl json.RawMessage `json:"cache_control"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return nil, ErrInvalidCanonicalRequest
	}
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != "text" || rawJSONPresent(block.CacheControl) {
			return nil, unsupportedTextFeature("anthropic_system_block", block.Type)
		}
		out = append(out, block.Text)
	}
	return out, nil
}

func decodeAnthropicMessage(raw json.RawMessage) (TextMessage, error) {
	var message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &message) != nil {
		return TextMessage{}, ErrInvalidCanonicalRequest
	}
	role := TextRole(strings.ToLower(strings.TrimSpace(message.Role)))
	if role != TextRoleUser && role != TextRoleAssistant {
		return TextMessage{}, unsupportedTextFeature("message_role", string(role))
	}
	var text string
	if json.Unmarshal(message.Content, &text) == nil {
		return TextMessage{Role: role, Content: []TextContent{{Kind: TextContentText, Text: text}}}, nil
	}
	var blocks []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
		IsError   bool            `json:"is_error"`
	}
	if json.Unmarshal(message.Content, &blocks) != nil || len(blocks) == 0 {
		return TextMessage{}, ErrInvalidCanonicalRequest
	}
	out := TextMessage{Role: role}
	for _, block := range blocks {
		switch block.Type {
		case "text":
			out.Content = append(out.Content, TextContent{Kind: TextContentText, Text: block.Text})
		case "tool_use":
			arguments, err := normalizeToolArguments(block.Input)
			if err != nil {
				return TextMessage{}, err
			}
			out.Content = append(out.Content, TextContent{Kind: TextContentToolCall, ToolCall: &TextToolCall{ID: block.ID, Name: block.Name, Arguments: arguments}})
		case "tool_result":
			content, err := anthropicToolResultText(block.Content)
			if err != nil {
				return TextMessage{}, err
			}
			out.Content = append(out.Content, TextContent{Kind: TextContentToolResult, ToolResult: &TextToolResult{ToolCallID: block.ToolUseID, Content: content, IsError: block.IsError}})
		default:
			return TextMessage{}, unsupportedTextFeature("anthropic_content_block", block.Type)
		}
	}
	return out, nil
}

func decodeGeminiRequest(raw []byte, modelOverride string, streamOverride *bool) (CanonicalTextRequest, error) {
	var payload struct {
		Contents          []json.RawMessage `json:"contents"`
		SystemInstruction json.RawMessage   `json:"systemInstruction"`
		Tools             []json.RawMessage `json:"tools"`
		ToolConfig        json.RawMessage   `json:"toolConfig"`
		GenerationConfig  struct {
			Temperature      *float64        `json:"temperature"`
			TopP             *float64        `json:"topP"`
			TopK             *int            `json:"topK"`
			MaxOutputTokens  *int            `json:"maxOutputTokens"`
			StopSequences    []string        `json:"stopSequences"`
			CandidateCount   *int            `json:"candidateCount"`
			ResponseMIMEType string          `json:"responseMimeType"`
			ResponseSchema   json.RawMessage `json:"responseSchema"`
		} `json:"generationConfig"`
		SafetySettings json.RawMessage `json:"safetySettings"`
		CachedContent  string          `json:"cachedContent"`
	}
	if len(raw) == 0 || decodeStrictTextJSON(raw, &payload) != nil {
		return CanonicalTextRequest{}, ErrInvalidCanonicalRequest
	}
	if rawJSONPresent(payload.SafetySettings) || payload.CachedContent != "" {
		return CanonicalTextRequest{}, unsupportedTextFeature("gemini_extension", "safety settings and cached content are provider-specific")
	}
	if payload.GenerationConfig.CandidateCount != nil && *payload.GenerationConfig.CandidateCount != 1 {
		return CanonicalTextRequest{}, unsupportedTextFeature("candidateCount", "only one candidate can be routed losslessly")
	}
	stream := false
	if streamOverride != nil {
		stream = *streamOverride
	}
	request := CanonicalTextRequest{Model: modelOverride, Stream: stream, Generation: TextGenerationConfig{Temperature: payload.GenerationConfig.Temperature, TopP: payload.GenerationConfig.TopP, TopK: payload.GenerationConfig.TopK, MaxOutputTokens: payload.GenerationConfig.MaxOutputTokens, StopSequences: payload.GenerationConfig.StopSequences}}
	if payload.GenerationConfig.ResponseMIMEType != "" {
		if payload.GenerationConfig.ResponseMIMEType != "application/json" {
			return CanonicalTextRequest{}, unsupportedTextFeature("responseMimeType", payload.GenerationConfig.ResponseMIMEType)
		}
		request.ResponseFormat = TextResponseFormat{Type: "json_object"}
		if rawJSONPresent(payload.GenerationConfig.ResponseSchema) {
			request.ResponseFormat = TextResponseFormat{Type: "json_schema", Name: "response", Schema: append(json.RawMessage(nil), payload.GenerationConfig.ResponseSchema...)}
		}
	}
	var err error
	request.System, err = decodeGeminiSystem(payload.SystemInstruction)
	if err != nil {
		return CanonicalTextRequest{}, err
	}
	request.Tools, err = decodeGeminiTools(payload.Tools)
	if err != nil {
		return CanonicalTextRequest{}, err
	}
	request.ToolChoice, err = decodeGeminiToolChoice(payload.ToolConfig)
	if err != nil {
		return CanonicalTextRequest{}, err
	}
	for i, rawContent := range payload.Contents {
		message, err := decodeGeminiContent(rawContent)
		if err != nil {
			return CanonicalTextRequest{}, fmt.Errorf("contents[%d]: %w", i, err)
		}
		request.Messages = append(request.Messages, message)
	}
	if err := validateCanonicalTextRequest(request); err != nil {
		return CanonicalTextRequest{}, err
	}
	return request, nil
}

func decodeGeminiSystem(raw json.RawMessage) ([]string, error) {
	if !rawJSONPresent(raw) {
		return nil, nil
	}
	message, err := decodeGeminiContent(raw)
	if err != nil {
		return nil, err
	}
	text, err := textOnlyContent(message.Content)
	if err != nil {
		return nil, unsupportedTextFeature("systemInstruction", "only text parts are supported")
	}
	return []string{text}, nil
}

func decodeGeminiContent(raw json.RawMessage) (TextMessage, error) {
	var content struct {
		Role  string `json:"role"`
		Parts []struct {
			Text         *string `json:"text"`
			FunctionCall *struct {
				Name string          `json:"name"`
				Args json.RawMessage `json:"args"`
				ID   string          `json:"id"`
			} `json:"functionCall"`
			FunctionResponse *struct {
				Name     string          `json:"name"`
				Response json.RawMessage `json:"response"`
				ID       string          `json:"id"`
			} `json:"functionResponse"`
			InlineData json.RawMessage `json:"inlineData"`
			FileData   json.RawMessage `json:"fileData"`
		} `json:"parts"`
	}
	if json.Unmarshal(raw, &content) != nil || len(content.Parts) == 0 {
		return TextMessage{}, ErrInvalidCanonicalRequest
	}
	role := TextRoleUser
	if strings.ToLower(content.Role) == "model" || strings.ToLower(content.Role) == "assistant" {
		role = TextRoleAssistant
	}
	out := TextMessage{Role: role}
	for _, part := range content.Parts {
		switch {
		case part.Text != nil:
			out.Content = append(out.Content, TextContent{Kind: TextContentText, Text: *part.Text})
		case part.FunctionCall != nil:
			arguments, err := normalizeToolArguments(part.FunctionCall.Args)
			if err != nil {
				return TextMessage{}, err
			}
			id := strings.TrimSpace(part.FunctionCall.ID)
			if id == "" {
				id = "call_" + stableTextID(part.FunctionCall.Name+string(arguments))
			}
			out.Content = append(out.Content, TextContent{Kind: TextContentToolCall, ToolCall: &TextToolCall{ID: id, Name: part.FunctionCall.Name, Arguments: arguments}})
		case part.FunctionResponse != nil:
			id := strings.TrimSpace(part.FunctionResponse.ID)
			if id == "" {
				id = "call_" + stableTextID(part.FunctionResponse.Name)
			}
			out.Content = append(out.Content, TextContent{Kind: TextContentToolResult, ToolResult: &TextToolResult{ToolCallID: id, Name: part.FunctionResponse.Name, Content: string(part.FunctionResponse.Response)}})
		case rawJSONPresent(part.InlineData) || rawJSONPresent(part.FileData):
			return TextMessage{}, unsupportedTextFeature("gemini_media_part", "media is outside the canonical text contract")
		default:
			return TextMessage{}, ErrInvalidCanonicalRequest
		}
	}
	return out, nil
}

func encodeOpenAIChatRequest(request CanonicalTextRequest) ([]byte, error) {
	messages := make([]any, 0, len(request.System)+len(request.Messages)*2)
	if len(request.System) > 0 {
		messages = append(messages, map[string]any{"role": "system", "content": strings.Join(request.System, "\n\n")})
	}
	for _, message := range request.Messages {
		encoded, err := encodeOpenAIMessage(message)
		if err != nil {
			return nil, err
		}
		messages = append(messages, encoded...)
	}
	payload := map[string]any{"model": request.Model, "messages": messages, "stream": request.Stream}
	applyOpenAIGeneration(payload, request.Generation)
	if request.Stream {
		payload["stream_options"] = map[string]any{"include_usage": true}
	}
	if len(request.Tools) > 0 {
		payload["tools"] = encodeOpenAITools(request.Tools)
		if choice := encodeOpenAIToolChoice(request.ToolChoice); choice != nil {
			payload["tool_choice"] = choice
		}
	}
	if format := encodeOpenAIResponseFormat(request.ResponseFormat); format != nil {
		payload["response_format"] = format
	}
	if request.ClientUser != "" {
		payload["user"] = request.ClientUser
	}
	return json.Marshal(payload)
}

func encodeOpenAIResponsesRequest(request CanonicalTextRequest) ([]byte, error) {
	input := make([]any, 0, len(request.Messages)*2)
	for _, message := range request.Messages {
		encoded, err := encodeResponsesMessage(message)
		if err != nil {
			return nil, err
		}
		input = append(input, encoded...)
	}
	payload := map[string]any{"model": request.Model, "input": input, "stream": request.Stream}
	if len(request.System) > 0 {
		payload["instructions"] = strings.Join(request.System, "\n\n")
	}
	if request.Generation.Temperature != nil {
		payload["temperature"] = *request.Generation.Temperature
	}
	if request.Generation.TopP != nil {
		payload["top_p"] = *request.Generation.TopP
	}
	if request.Generation.MaxOutputTokens != nil {
		payload["max_output_tokens"] = *request.Generation.MaxOutputTokens
	}
	if len(request.Generation.StopSequences) > 0 || request.Generation.TopK != nil {
		return nil, unsupportedTextFeature("generation", "OpenAI Responses cannot express top_k or stop sequences")
	}
	if len(request.Tools) > 0 {
		payload["tools"] = encodeResponsesTools(request.Tools)
		if choice := encodeOpenAIToolChoice(request.ToolChoice); choice != nil {
			payload["tool_choice"] = choice
		}
	}
	if request.ResponseFormat.Type != "" {
		format := encodeResponsesTextFormat(request.ResponseFormat)
		payload["text"] = map[string]any{"format": format}
	}
	return json.Marshal(payload)
}

func encodeAnthropicRequest(request CanonicalTextRequest) ([]byte, error) {
	messages := make([]any, 0, len(request.Messages))
	for _, message := range request.Messages {
		blocks, err := encodeAnthropicContent(message.Content)
		if err != nil {
			return nil, err
		}
		messages = append(messages, map[string]any{"role": string(message.Role), "content": blocks})
	}
	payload := map[string]any{"model": request.Model, "messages": messages, "stream": request.Stream}
	if len(request.System) > 0 {
		payload["system"] = strings.Join(request.System, "\n\n")
	}
	if request.Generation.MaxOutputTokens == nil {
		return nil, unsupportedTextFeature("max_output_tokens", "Anthropic requires max_tokens")
	}
	payload["max_tokens"] = *request.Generation.MaxOutputTokens
	if request.Generation.Temperature != nil {
		payload["temperature"] = *request.Generation.Temperature
	}
	if request.Generation.TopP != nil {
		payload["top_p"] = *request.Generation.TopP
	}
	if request.Generation.TopK != nil {
		payload["top_k"] = *request.Generation.TopK
	}
	if len(request.Generation.StopSequences) > 0 {
		payload["stop_sequences"] = request.Generation.StopSequences
	}
	if len(request.Tools) > 0 {
		payload["tools"] = encodeAnthropicTools(request.Tools)
		if choice := encodeAnthropicToolChoice(request.ToolChoice); choice != nil {
			payload["tool_choice"] = choice
		}
	}
	if request.ResponseFormat.Type != "" {
		return nil, unsupportedTextFeature("response_format", "Anthropic Messages has no lossless response format field")
	}
	return json.Marshal(payload)
}

func encodeGeminiRequest(request CanonicalTextRequest) ([]byte, error) {
	contents := make([]any, 0, len(request.Messages))
	toolNames := canonicalToolCallNames(request.Messages)
	for _, message := range request.Messages {
		parts, err := encodeGeminiParts(message.Content, toolNames)
		if err != nil {
			return nil, err
		}
		role := "user"
		if message.Role == TextRoleAssistant {
			role = "model"
		}
		contents = append(contents, map[string]any{"role": role, "parts": parts})
	}
	payload := map[string]any{"contents": contents}
	if len(request.System) > 0 {
		payload["systemInstruction"] = map[string]any{"parts": []any{map[string]any{"text": strings.Join(request.System, "\n\n")}}}
	}
	generation := map[string]any{}
	if request.Generation.Temperature != nil {
		generation["temperature"] = *request.Generation.Temperature
	}
	if request.Generation.TopP != nil {
		generation["topP"] = *request.Generation.TopP
	}
	if request.Generation.TopK != nil {
		generation["topK"] = *request.Generation.TopK
	}
	if request.Generation.MaxOutputTokens != nil {
		generation["maxOutputTokens"] = *request.Generation.MaxOutputTokens
	}
	if len(request.Generation.StopSequences) > 0 {
		generation["stopSequences"] = request.Generation.StopSequences
	}
	if request.ResponseFormat.Type != "" {
		generation["responseMimeType"] = "application/json"
		if request.ResponseFormat.Type == "json_schema" {
			generation["responseSchema"] = json.RawMessage(request.ResponseFormat.Schema)
		}
	}
	if len(generation) > 0 {
		payload["generationConfig"] = generation
	}
	if len(request.Tools) > 0 && request.ToolChoice.Mode != "none" {
		declarations := make([]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			declarations = append(declarations, map[string]any{"name": tool.Name, "description": tool.Description, "parameters": json.RawMessage(tool.InputSchema)})
		}
		payload["tools"] = []any{map[string]any{"functionDeclarations": declarations}}
		if config := encodeGeminiToolChoice(request.ToolChoice); config != nil {
			payload["toolConfig"] = config
		}
	}
	return json.Marshal(payload)
}

func encodeBedrockConverseRequest(request CanonicalTextRequest) ([]byte, error) {
	messages := make([]any, 0, len(request.Messages))
	for _, message := range request.Messages {
		content := make([]any, 0, len(message.Content))
		for _, block := range message.Content {
			switch block.Kind {
			case TextContentText:
				content = append(content, map[string]any{"text": block.Text})
			case TextContentToolCall:
				content = append(content, map[string]any{"toolUse": map[string]any{"toolUseId": block.ToolCall.ID, "name": block.ToolCall.Name, "input": json.RawMessage(block.ToolCall.Arguments)}})
			case TextContentToolResult:
				status := "success"
				if block.ToolResult.IsError {
					status = "error"
				}
				content = append(content, map[string]any{"toolResult": map[string]any{"toolUseId": block.ToolResult.ToolCallID, "status": status, "content": []any{map[string]any{"text": block.ToolResult.Content}}}})
			}
		}
		messages = append(messages, map[string]any{"role": string(message.Role), "content": content})
	}
	payload := map[string]any{"messages": messages}
	if len(request.System) > 0 {
		system := make([]any, 0, len(request.System))
		for _, text := range request.System {
			system = append(system, map[string]any{"text": text})
		}
		payload["system"] = system
	}
	inference := map[string]any{}
	if request.Generation.Temperature != nil {
		inference["temperature"] = *request.Generation.Temperature
	}
	if request.Generation.TopP != nil {
		inference["topP"] = *request.Generation.TopP
	}
	if request.Generation.MaxOutputTokens != nil {
		inference["maxTokens"] = *request.Generation.MaxOutputTokens
	}
	if len(request.Generation.StopSequences) > 0 {
		inference["stopSequences"] = request.Generation.StopSequences
	}
	if request.Generation.TopK != nil {
		return nil, unsupportedTextFeature("top_k", "Bedrock Converse inferenceConfig cannot express top_k portably")
	}
	if len(inference) > 0 {
		payload["inferenceConfig"] = inference
	}
	if len(request.Tools) > 0 && request.ToolChoice.Mode != "none" {
		tools := make([]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			tools = append(tools, map[string]any{"toolSpec": map[string]any{"name": tool.Name, "description": tool.Description, "inputSchema": map[string]any{"json": json.RawMessage(tool.InputSchema)}}})
		}
		config := map[string]any{"tools": tools}
		if choice := encodeBedrockToolChoice(request.ToolChoice); choice != nil {
			config["toolChoice"] = choice
		}
		payload["toolConfig"] = config
	}
	if request.ResponseFormat.Type != "" {
		return nil, unsupportedTextFeature("response_format", "Bedrock Converse has no portable response format field")
	}
	return json.Marshal(payload)
}

func decodeOpenAITools(rawTools []json.RawMessage) ([]TextTool, error) {
	out := make([]TextTool, 0, len(rawTools))
	for i, raw := range rawTools {
		var tool struct {
			Type     string `json:"type"`
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
				Strict      bool            `json:"strict"`
			} `json:"function"`
		}
		if json.Unmarshal(raw, &tool) != nil || tool.Type != "function" {
			return nil, fmt.Errorf("tools[%d]: %w", i, unsupportedTextFeature("tool_type", tool.Type))
		}
		schema := tool.Function.Parameters
		if !rawJSONPresent(schema) {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, TextTool{Name: tool.Function.Name, Description: tool.Function.Description, InputSchema: append(json.RawMessage(nil), schema...)})
	}
	return out, nil
}

func decodeResponsesTools(rawTools []json.RawMessage) ([]TextTool, error) {
	out := make([]TextTool, 0, len(rawTools))
	for i, raw := range rawTools {
		var tool struct {
			Type        string          `json:"type"`
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		}
		if json.Unmarshal(raw, &tool) != nil || tool.Type != "function" {
			return nil, fmt.Errorf("tools[%d]: %w", i, unsupportedTextFeature("tool_type", tool.Type))
		}
		if !rawJSONPresent(tool.Parameters) {
			tool.Parameters = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, TextTool{Name: tool.Name, Description: tool.Description, InputSchema: append(json.RawMessage(nil), tool.Parameters...)})
	}
	return out, nil
}

func decodeAnthropicTools(rawTools []json.RawMessage) ([]TextTool, error) {
	out := make([]TextTool, 0, len(rawTools))
	for i, raw := range rawTools {
		var tool struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"input_schema"`
			Type        string          `json:"type"`
		}
		if json.Unmarshal(raw, &tool) != nil || (tool.Type != "" && tool.Type != "custom") {
			return nil, fmt.Errorf("tools[%d]: %w", i, unsupportedTextFeature("tool_type", tool.Type))
		}
		out = append(out, TextTool{Name: tool.Name, Description: tool.Description, InputSchema: append(json.RawMessage(nil), tool.InputSchema...)})
	}
	return out, nil
}

func decodeGeminiTools(rawTools []json.RawMessage) ([]TextTool, error) {
	out := []TextTool{}
	for i, raw := range rawTools {
		var group struct {
			FunctionDeclarations []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			} `json:"functionDeclarations"`
			CodeExecution json.RawMessage `json:"codeExecution"`
			GoogleSearch  json.RawMessage `json:"googleSearch"`
		}
		if json.Unmarshal(raw, &group) != nil {
			return nil, fmt.Errorf("tools[%d]: %w", i, ErrInvalidCanonicalRequest)
		}
		if rawJSONPresent(group.CodeExecution) || rawJSONPresent(group.GoogleSearch) {
			return nil, unsupportedTextFeature("gemini_builtin_tool", "built-in tools are provider-specific")
		}
		for _, tool := range group.FunctionDeclarations {
			out = append(out, TextTool{Name: tool.Name, Description: tool.Description, InputSchema: append(json.RawMessage(nil), tool.Parameters...)})
		}
	}
	return out, nil
}

func decodeOpenAIToolChoice(raw json.RawMessage) (TextToolChoice, error) {
	if !rawJSONPresent(raw) {
		return TextToolChoice{}, nil
	}
	var mode string
	if json.Unmarshal(raw, &mode) == nil {
		switch mode {
		case "none", "auto", "required":
			return TextToolChoice{Mode: mode}, nil
		default:
			return TextToolChoice{}, unsupportedTextFeature("tool_choice", mode)
		}
	}
	var choice struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &choice) != nil {
		return TextToolChoice{}, ErrInvalidCanonicalRequest
	}
	name := choice.Function.Name
	if name == "" {
		name = choice.Name
	}
	if (choice.Type == "function" || choice.Type == "tool") && name != "" {
		return TextToolChoice{Mode: "named", Name: name}, nil
	}
	return TextToolChoice{}, unsupportedTextFeature("tool_choice", choice.Type)
}

func decodeAnthropicToolChoice(raw json.RawMessage) (TextToolChoice, error) {
	if !rawJSONPresent(raw) {
		return TextToolChoice{}, nil
	}
	var choice struct {
		Type            string `json:"type"`
		Name            string `json:"name"`
		DisableParallel bool   `json:"disable_parallel_tool_use"`
	}
	if json.Unmarshal(raw, &choice) != nil {
		return TextToolChoice{}, ErrInvalidCanonicalRequest
	}
	if choice.DisableParallel {
		return TextToolChoice{}, unsupportedTextFeature("disable_parallel_tool_use", "parallel tool policy is not portable")
	}
	switch choice.Type {
	case "auto", "any":
		return TextToolChoice{Mode: map[string]string{"auto": "auto", "any": "required"}[choice.Type]}, nil
	case "tool":
		return TextToolChoice{Mode: "named", Name: choice.Name}, nil
	case "none":
		return TextToolChoice{Mode: "none"}, nil
	default:
		return TextToolChoice{}, unsupportedTextFeature("tool_choice", choice.Type)
	}
}

func decodeGeminiToolChoice(raw json.RawMessage) (TextToolChoice, error) {
	if !rawJSONPresent(raw) {
		return TextToolChoice{}, nil
	}
	var config struct {
		FunctionCallingConfig struct {
			Mode    string   `json:"mode"`
			Allowed []string `json:"allowedFunctionNames"`
		} `json:"functionCallingConfig"`
	}
	if json.Unmarshal(raw, &config) != nil {
		return TextToolChoice{}, ErrInvalidCanonicalRequest
	}
	switch strings.ToUpper(config.FunctionCallingConfig.Mode) {
	case "", "AUTO":
		return TextToolChoice{Mode: "auto"}, nil
	case "NONE":
		return TextToolChoice{Mode: "none"}, nil
	case "ANY":
		if len(config.FunctionCallingConfig.Allowed) == 0 {
			return TextToolChoice{Mode: "required"}, nil
		}
		if len(config.FunctionCallingConfig.Allowed) == 1 {
			return TextToolChoice{Mode: "named", Name: config.FunctionCallingConfig.Allowed[0]}, nil
		}
		return TextToolChoice{}, unsupportedTextFeature("allowedFunctionNames", "multiple named tools cannot be represented portably")
	default:
		return TextToolChoice{}, unsupportedTextFeature("tool_choice", config.FunctionCallingConfig.Mode)
	}
}

func decodeOpenAIResponseFormat(raw json.RawMessage) (TextResponseFormat, error) {
	if !rawJSONPresent(raw) {
		return TextResponseFormat{}, nil
	}
	var value struct {
		Type       string `json:"type"`
		JSONSchema struct {
			Name   string          `json:"name"`
			Schema json.RawMessage `json:"schema"`
			Strict bool            `json:"strict"`
		} `json:"json_schema"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return TextResponseFormat{}, ErrInvalidCanonicalRequest
	}
	switch value.Type {
	case "text":
		return TextResponseFormat{}, nil
	case "json_object":
		return TextResponseFormat{Type: "json_object"}, nil
	case "json_schema":
		return TextResponseFormat{Type: "json_schema", Name: value.JSONSchema.Name, Schema: append(json.RawMessage(nil), value.JSONSchema.Schema...), Strict: value.JSONSchema.Strict}, nil
	default:
		return TextResponseFormat{}, unsupportedTextFeature("response_format", value.Type)
	}
}

func decodeResponsesTextFormat(raw json.RawMessage) (TextResponseFormat, error) {
	if !rawJSONPresent(raw) {
		return TextResponseFormat{}, nil
	}
	var text struct {
		Format struct {
			Type   string          `json:"type"`
			Name   string          `json:"name"`
			Schema json.RawMessage `json:"schema"`
			Strict bool            `json:"strict"`
		} `json:"format"`
	}
	if json.Unmarshal(raw, &text) != nil {
		return TextResponseFormat{}, ErrInvalidCanonicalRequest
	}
	switch text.Format.Type {
	case "", "text":
		return TextResponseFormat{}, nil
	case "json_object":
		return TextResponseFormat{Type: "json_object"}, nil
	case "json_schema":
		return TextResponseFormat{Type: "json_schema", Name: text.Format.Name, Schema: append(json.RawMessage(nil), text.Format.Schema...), Strict: text.Format.Strict}, nil
	default:
		return TextResponseFormat{}, unsupportedTextFeature("response_format", text.Format.Type)
	}
}

func decodeStopSequences(raw json.RawMessage) ([]string, error) {
	if !rawJSONPresent(raw) || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return []string{single}, nil
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil {
		return nil, ErrInvalidCanonicalRequest
	}
	return values, nil
}

func encodeOpenAIMessage(message TextMessage) ([]any, error) {
	textParts := []any{}
	toolCalls := []any{}
	out := []any{}
	for _, content := range message.Content {
		switch content.Kind {
		case TextContentText:
			textParts = append(textParts, map[string]any{"type": "text", "text": content.Text})
		case TextContentToolCall:
			toolCalls = append(toolCalls, map[string]any{"id": content.ToolCall.ID, "type": "function", "function": map[string]any{"name": content.ToolCall.Name, "arguments": string(content.ToolCall.Arguments)}})
		case TextContentToolResult:
			if len(textParts) > 0 || len(toolCalls) > 0 {
				return nil, unsupportedTextFeature("mixed_tool_result", "OpenAI tool messages cannot mix with regular content")
			}
			out = append(out, map[string]any{"role": "tool", "tool_call_id": content.ToolResult.ToolCallID, "content": content.ToolResult.Content})
		}
	}
	if len(textParts) > 0 || len(toolCalls) > 0 {
		item := map[string]any{"role": string(message.Role)}
		if len(textParts) == 1 {
			item["content"] = textParts[0].(map[string]any)["text"]
		} else if len(textParts) > 1 {
			item["content"] = textParts
		}
		if len(toolCalls) > 0 {
			item["tool_calls"] = toolCalls
		}
		out = append([]any{item}, out...)
	}
	return out, nil
}

func encodeResponsesMessage(message TextMessage) ([]any, error) {
	out := []any{}
	parts := []any{}
	partType := "input_text"
	if message.Role == TextRoleAssistant {
		partType = "output_text"
	}
	for _, content := range message.Content {
		switch content.Kind {
		case TextContentText:
			parts = append(parts, map[string]any{"type": partType, "text": content.Text})
		case TextContentToolCall:
			out = append(out, map[string]any{"type": "function_call", "call_id": content.ToolCall.ID, "name": content.ToolCall.Name, "arguments": string(content.ToolCall.Arguments)})
		case TextContentToolResult:
			out = append(out, map[string]any{"type": "function_call_output", "call_id": content.ToolResult.ToolCallID, "output": content.ToolResult.Content})
		}
	}
	if len(parts) > 0 {
		out = append([]any{map[string]any{"type": "message", "role": string(message.Role), "content": parts}}, out...)
	}
	return out, nil
}

func encodeAnthropicContent(contents []TextContent) ([]any, error) {
	out := make([]any, 0, len(contents))
	for _, content := range contents {
		switch content.Kind {
		case TextContentText:
			out = append(out, map[string]any{"type": "text", "text": content.Text})
		case TextContentToolCall:
			out = append(out, map[string]any{"type": "tool_use", "id": content.ToolCall.ID, "name": content.ToolCall.Name, "input": json.RawMessage(content.ToolCall.Arguments)})
		case TextContentToolResult:
			out = append(out, map[string]any{"type": "tool_result", "tool_use_id": content.ToolResult.ToolCallID, "content": content.ToolResult.Content, "is_error": content.ToolResult.IsError})
		}
	}
	return out, nil
}

func encodeGeminiParts(contents []TextContent, toolNames map[string]string) ([]any, error) {
	out := make([]any, 0, len(contents))
	for _, content := range contents {
		switch content.Kind {
		case TextContentText:
			out = append(out, map[string]any{"text": content.Text})
		case TextContentToolCall:
			out = append(out, map[string]any{"functionCall": map[string]any{"id": content.ToolCall.ID, "name": content.ToolCall.Name, "args": json.RawMessage(content.ToolCall.Arguments)}})
		case TextContentToolResult:
			name := strings.TrimSpace(content.ToolResult.Name)
			if name == "" {
				name = toolNames[content.ToolResult.ToolCallID]
			}
			if name == "" {
				return nil, unsupportedTextFeature("tool_result_name", "Gemini requires a function name for every tool result")
			}
			out = append(out, map[string]any{"functionResponse": map[string]any{"id": content.ToolResult.ToolCallID, "name": name, "response": map[string]any{"content": content.ToolResult.Content}}})
		}
	}
	return out, nil
}

func canonicalToolCallNames(messages []TextMessage) map[string]string {
	names := map[string]string{}
	for _, message := range messages {
		for _, content := range message.Content {
			if content.Kind == TextContentToolCall && content.ToolCall != nil {
				names[content.ToolCall.ID] = content.ToolCall.Name
			}
		}
	}
	return names
}

func encodeOpenAITools(tools []TextTool) []any {
	out := make([]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{"type": "function", "function": map[string]any{"name": tool.Name, "description": tool.Description, "parameters": json.RawMessage(tool.InputSchema)}})
	}
	return out
}
func encodeResponsesTools(tools []TextTool) []any {
	out := make([]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description, "parameters": json.RawMessage(tool.InputSchema)})
	}
	return out
}
func encodeAnthropicTools(tools []TextTool) []any {
	out := make([]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{"name": tool.Name, "description": tool.Description, "input_schema": json.RawMessage(tool.InputSchema)})
	}
	return out
}

func encodeOpenAIToolChoice(choice TextToolChoice) any {
	switch choice.Mode {
	case "none", "auto", "required":
		return choice.Mode
	case "named":
		return map[string]any{"type": "function", "function": map[string]any{"name": choice.Name}}
	default:
		return nil
	}
}
func encodeAnthropicToolChoice(choice TextToolChoice) any {
	switch choice.Mode {
	case "none", "auto":
		return map[string]any{"type": choice.Mode}
	case "required":
		return map[string]any{"type": "any"}
	case "named":
		return map[string]any{"type": "tool", "name": choice.Name}
	default:
		return nil
	}
}
func encodeGeminiToolChoice(choice TextToolChoice) any {
	mode := ""
	allowed := []string(nil)
	switch choice.Mode {
	case "none":
		mode = "NONE"
	case "auto":
		mode = "AUTO"
	case "required":
		mode = "ANY"
	case "named":
		mode = "ANY"
		allowed = []string{choice.Name}
	default:
		return nil
	}
	config := map[string]any{"mode": mode}
	if len(allowed) > 0 {
		config["allowedFunctionNames"] = allowed
	}
	return map[string]any{"functionCallingConfig": config}
}
func encodeBedrockToolChoice(choice TextToolChoice) any {
	switch choice.Mode {
	case "auto":
		return map[string]any{"auto": map[string]any{}}
	case "required":
		return map[string]any{"any": map[string]any{}}
	case "named":
		return map[string]any{"tool": map[string]any{"name": choice.Name}}
	default:
		return nil
	}
}

func applyOpenAIGeneration(payload map[string]any, generation TextGenerationConfig) {
	if generation.Temperature != nil {
		payload["temperature"] = *generation.Temperature
	}
	if generation.TopP != nil {
		payload["top_p"] = *generation.TopP
	}
	if generation.MaxOutputTokens != nil {
		payload["max_completion_tokens"] = *generation.MaxOutputTokens
	}
	if len(generation.StopSequences) > 0 {
		payload["stop"] = generation.StopSequences
	}
}
func encodeOpenAIResponseFormat(format TextResponseFormat) any {
	switch format.Type {
	case "":
		return nil
	case "json_object":
		return map[string]any{"type": "json_object"}
	case "json_schema":
		return map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": format.Name, "schema": json.RawMessage(format.Schema), "strict": format.Strict}}
	default:
		return nil
	}
}
func encodeResponsesTextFormat(format TextResponseFormat) any {
	switch format.Type {
	case "json_object":
		return map[string]any{"type": "json_object"}
	case "json_schema":
		return map[string]any{"type": "json_schema", "name": format.Name, "schema": json.RawMessage(format.Schema), "strict": format.Strict}
	default:
		return map[string]any{"type": "text"}
	}
}

func normalizeToolArguments(raw json.RawMessage) (json.RawMessage, error) {
	if !rawJSONPresent(raw) {
		return json.RawMessage(`{}`), nil
	}
	var encoded string
	if json.Unmarshal(raw, &encoded) == nil {
		raw = json.RawMessage(encoded)
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("%w: tool arguments must be valid JSON", ErrInvalidCanonicalRequest)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, err
	}
	return compact.Bytes(), nil
}

func anthropicToolResultText(raw json.RawMessage) (string, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return "", ErrInvalidCanonicalRequest
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != "text" {
			return "", unsupportedTextFeature("tool_result_content", block.Type)
		}
		parts = append(parts, block.Text)
	}
	return strings.Join(parts, "\n"), nil
}
func textOnlyContent(content []TextContent) (string, error) {
	parts := make([]string, 0, len(content))
	for _, item := range content {
		if item.Kind != TextContentText {
			return "", unsupportedTextFeature("content_block", string(item.Kind))
		}
		parts = append(parts, item.Text)
	}
	return strings.Join(parts, "\n"), nil
}
func rawJSONPresent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte("[]")) && !bytes.Equal(trimmed, []byte("{}"))
}

func decodeStrictTextJSON(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return io.ErrUnexpectedEOF
		}
		return err
	}
	return nil
}
func stableTextID(value string) string {
	var hash uint64 = 1469598103934665603
	for i := 0; i < len(value); i++ {
		hash ^= uint64(value[i])
		hash *= 1099511628211
	}
	return fmt.Sprintf("%x", hash)
}
