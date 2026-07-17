package gatewaycore

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type TextUsage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

func (u TextUsage) TotalTokens() int { return u.InputTokens + u.OutputTokens }

type CanonicalTextResponse struct {
	ID         string        `json:"id"`
	Model      string        `json:"model"`
	Content    []TextContent `json:"content"`
	StopReason string        `json:"stop_reason"`
	Usage      TextUsage     `json:"usage"`
	CreatedAt  int64         `json:"created_at"`
}

func DecodeCanonicalTextResponse(format UpstreamFormat, raw []byte, model string) (CanonicalTextResponse, error) {
	var response CanonicalTextResponse
	var err error
	switch format {
	case UpstreamFormatOpenAIChat:
		response, err = decodeOpenAIChatResponse(raw)
	case UpstreamFormatOpenAIResponses:
		response, err = decodeOpenAIResponsesResponse(raw)
	case UpstreamFormatAnthropic:
		response, err = decodeAnthropicResponse(raw)
	case UpstreamFormatGemini:
		response, err = decodeGeminiResponse(raw)
	case UpstreamFormatBedrockConverse:
		response, err = decodeBedrockConverseResponse(raw)
	default:
		err = unsupportedTextFeature("upstream_format", string(format))
	}
	if err != nil {
		return CanonicalTextResponse{}, err
	}
	if response.Model == "" {
		response.Model = model
	}
	if response.ID == "" {
		response.ID = "resp_" + stableTextID(model+string(raw))
	}
	if response.CreatedAt == 0 {
		response.CreatedAt = time.Now().Unix()
	}
	return response, nil
}

func EncodeCanonicalTextResponse(protocol Protocol, response CanonicalTextResponse) ([]byte, error) {
	switch protocol {
	case ProtocolOpenAIChat:
		return encodeOpenAIChatResponse(response)
	case ProtocolOpenAIResponses:
		return encodeOpenAIResponsesResponse(response)
	case ProtocolAnthropicMessages:
		return encodeAnthropicResponse(response)
	case ProtocolGeminiGenerate:
		return encodeGeminiResponse(response)
	default:
		return nil, unsupportedTextFeature("client_protocol", string(protocol))
	}
}

func decodeOpenAIChatResponse(raw []byte) (CanonicalTextResponse, error) {
	var payload struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Created int64  `json:"created"`
		Choices []struct {
			Message struct {
				Content   json.RawMessage `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			PromptDetails    struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &payload) != nil || len(payload.Choices) != 1 {
		return CanonicalTextResponse{}, fmt.Errorf("%w: invalid OpenAI Chat response", ErrInvalidCanonicalRequest)
	}
	content, err := decodeOpenAIContent(payload.Choices[0].Message.Content)
	if err != nil {
		return CanonicalTextResponse{}, err
	}
	for _, call := range payload.Choices[0].Message.ToolCalls {
		arguments, err := normalizeToolArguments(call.Function.Arguments)
		if err != nil {
			return CanonicalTextResponse{}, err
		}
		content = append(content, TextContent{Kind: TextContentToolCall, ToolCall: &TextToolCall{ID: call.ID, Name: call.Function.Name, Arguments: arguments}})
	}
	return CanonicalTextResponse{
		ID: payload.ID, Model: payload.Model, CreatedAt: payload.Created,
		Content: content, StopReason: normalizeStopReason(payload.Choices[0].FinishReason),
		Usage: TextUsage{InputTokens: payload.Usage.PromptTokens, OutputTokens: payload.Usage.CompletionTokens, CacheReadTokens: payload.Usage.PromptDetails.CachedTokens},
	}, nil
}

func decodeOpenAIResponsesResponse(raw []byte) (CanonicalTextResponse, error) {
	var payload struct {
		ID        string `json:"id"`
		Model     string `json:"model"`
		CreatedAt int64  `json:"created_at"`
		Status    string `json:"status"`
		Output    []struct {
			Type    string `json:"type"`
			ID      string `json:"id"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			CallID    string          `json:"call_id"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"output"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			InputDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &payload) != nil || len(payload.Output) == 0 {
		return CanonicalTextResponse{}, fmt.Errorf("%w: invalid OpenAI Responses response", ErrInvalidCanonicalRequest)
	}
	content := []TextContent{}
	for _, item := range payload.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type != "output_text" && part.Type != "text" {
					return CanonicalTextResponse{}, unsupportedTextFeature("responses_output_part", part.Type)
				}
				content = append(content, TextContent{Kind: TextContentText, Text: part.Text})
			}
		case "function_call":
			arguments, err := normalizeToolArguments(item.Arguments)
			if err != nil {
				return CanonicalTextResponse{}, err
			}
			callID := item.CallID
			if callID == "" {
				callID = item.ID
			}
			content = append(content, TextContent{Kind: TextContentToolCall, ToolCall: &TextToolCall{ID: callID, Name: item.Name, Arguments: arguments}})
		default:
			return CanonicalTextResponse{}, unsupportedTextFeature("responses_output_item", item.Type)
		}
	}
	stopReason := "stop"
	if payload.Status != "" && payload.Status != "completed" {
		stopReason = payload.Status
	}
	if hasToolCall(content) {
		stopReason = "tool_use"
	}
	return CanonicalTextResponse{ID: payload.ID, Model: payload.Model, CreatedAt: payload.CreatedAt, Content: content, StopReason: stopReason, Usage: TextUsage{InputTokens: payload.Usage.InputTokens, OutputTokens: payload.Usage.OutputTokens, CacheReadTokens: payload.Usage.InputDetails.CachedTokens}}, nil
}

func decodeAnthropicResponse(raw []byte) (CanonicalTextResponse, error) {
	var payload struct {
		ID         string          `json:"id"`
		Model      string          `json:"model"`
		Content    json.RawMessage `json:"content"`
		StopReason string          `json:"stop_reason"`
		Usage      struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return CanonicalTextResponse{}, fmt.Errorf("%w: invalid Anthropic response", ErrInvalidCanonicalRequest)
	}
	message, err := decodeAnthropicMessage(mustMarshalJSON(map[string]any{"role": "assistant", "content": json.RawMessage(payload.Content)}))
	if err != nil {
		return CanonicalTextResponse{}, err
	}
	return CanonicalTextResponse{ID: payload.ID, Model: payload.Model, Content: message.Content, StopReason: normalizeStopReason(payload.StopReason), Usage: TextUsage{InputTokens: payload.Usage.InputTokens, OutputTokens: payload.Usage.OutputTokens, CacheReadTokens: payload.Usage.CacheReadInputTokens, CacheWriteTokens: payload.Usage.CacheCreationInputTokens}}, nil
}

func decodeGeminiResponse(raw []byte) (CanonicalTextResponse, error) {
	var payload struct {
		ResponseID string `json:"responseId"`
		Model      string `json:"modelVersion"`
		Candidates []struct {
			Content      json.RawMessage `json:"content"`
			FinishReason string          `json:"finishReason"`
		} `json:"candidates"`
		Usage struct {
			PromptTokens    int `json:"promptTokenCount"`
			CandidateTokens int `json:"candidatesTokenCount"`
			CachedTokens    int `json:"cachedContentTokenCount"`
		} `json:"usageMetadata"`
	}
	if json.Unmarshal(raw, &payload) != nil || len(payload.Candidates) != 1 {
		return CanonicalTextResponse{}, fmt.Errorf("%w: invalid Gemini response", ErrInvalidCanonicalRequest)
	}
	candidate := payload.Candidates[0]
	content := []TextContent{}
	if rawJSONPresent(candidate.Content) {
		message, err := decodeGeminiContent(candidate.Content)
		if err != nil {
			return CanonicalTextResponse{}, err
		}
		content = message.Content
	} else if strings.TrimSpace(candidate.FinishReason) == "" {
		return CanonicalTextResponse{}, fmt.Errorf("%w: Gemini response has neither content nor a finish reason", ErrInvalidCanonicalRequest)
	}
	return CanonicalTextResponse{ID: payload.ResponseID, Model: payload.Model, Content: content, StopReason: normalizeStopReason(candidate.FinishReason), Usage: TextUsage{InputTokens: payload.Usage.PromptTokens, OutputTokens: payload.Usage.CandidateTokens, CacheReadTokens: payload.Usage.CachedTokens}}, nil
}

func decodeBedrockConverseResponse(raw []byte) (CanonicalTextResponse, error) {
	var payload struct {
		Output struct {
			Message json.RawMessage `json:"message"`
		} `json:"output"`
		StopReason string `json:"stopReason"`
		Usage      struct {
			InputTokens           int `json:"inputTokens"`
			OutputTokens          int `json:"outputTokens"`
			CacheReadInputTokens  int `json:"cacheReadInputTokens"`
			CacheWriteInputTokens int `json:"cacheWriteInputTokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &payload) != nil || !rawJSONPresent(payload.Output.Message) {
		return CanonicalTextResponse{}, fmt.Errorf("%w: invalid Bedrock Converse response", ErrInvalidCanonicalRequest)
	}
	var message struct {
		Role    string `json:"role"`
		Content []struct {
			Text    *string `json:"text"`
			ToolUse *struct {
				ToolUseID string          `json:"toolUseId"`
				Name      string          `json:"name"`
				Input     json.RawMessage `json:"input"`
			} `json:"toolUse"`
		} `json:"content"`
	}
	if json.Unmarshal(payload.Output.Message, &message) != nil {
		return CanonicalTextResponse{}, ErrInvalidCanonicalRequest
	}
	content := []TextContent{}
	for _, block := range message.Content {
		switch {
		case block.Text != nil:
			content = append(content, TextContent{Kind: TextContentText, Text: *block.Text})
		case block.ToolUse != nil:
			arguments, err := normalizeToolArguments(block.ToolUse.Input)
			if err != nil {
				return CanonicalTextResponse{}, err
			}
			content = append(content, TextContent{Kind: TextContentToolCall, ToolCall: &TextToolCall{ID: block.ToolUse.ToolUseID, Name: block.ToolUse.Name, Arguments: arguments}})
		default:
			return CanonicalTextResponse{}, unsupportedTextFeature("bedrock_content_block", "non-text Converse output")
		}
	}
	return CanonicalTextResponse{Content: content, StopReason: normalizeStopReason(payload.StopReason), Usage: TextUsage{InputTokens: payload.Usage.InputTokens, OutputTokens: payload.Usage.OutputTokens, CacheReadTokens: payload.Usage.CacheReadInputTokens, CacheWriteTokens: payload.Usage.CacheWriteInputTokens}}, nil
}

func encodeOpenAIChatResponse(response CanonicalTextResponse) ([]byte, error) {
	message := map[string]any{"role": "assistant"}
	text := []string{}
	toolCalls := []any{}
	for _, content := range response.Content {
		switch content.Kind {
		case TextContentText:
			text = append(text, content.Text)
		case TextContentToolCall:
			toolCalls = append(toolCalls, map[string]any{"id": content.ToolCall.ID, "type": "function", "function": map[string]any{"name": content.ToolCall.Name, "arguments": string(content.ToolCall.Arguments)}})
		default:
			return nil, unsupportedTextFeature("response_content", string(content.Kind))
		}
	}
	if len(text) > 0 {
		message["content"] = strings.Join(text, "")
	} else {
		message["content"] = nil
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	payload := map[string]any{"id": response.ID, "object": "chat.completion", "created": response.CreatedAt, "model": response.Model, "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": openAIStopReason(response.StopReason)}}, "usage": map[string]any{"prompt_tokens": response.Usage.InputTokens, "completion_tokens": response.Usage.OutputTokens, "total_tokens": response.Usage.TotalTokens()}}
	if response.Usage.CacheReadTokens > 0 {
		payload["usage"].(map[string]any)["prompt_tokens_details"] = map[string]any{"cached_tokens": response.Usage.CacheReadTokens}
	}
	return json.Marshal(payload)
}

func encodeOpenAIResponsesResponse(response CanonicalTextResponse) ([]byte, error) {
	output := []any{}
	textParts := []any{}
	for _, content := range response.Content {
		switch content.Kind {
		case TextContentText:
			textParts = append(textParts, map[string]any{"type": "output_text", "text": content.Text, "annotations": []any{}})
		case TextContentToolCall:
			output = append(output, map[string]any{"type": "function_call", "id": "fc_" + stableTextID(content.ToolCall.ID), "call_id": content.ToolCall.ID, "name": content.ToolCall.Name, "arguments": string(content.ToolCall.Arguments), "status": "completed"})
		default:
			return nil, unsupportedTextFeature("response_content", string(content.Kind))
		}
	}
	if len(textParts) > 0 {
		output = append([]any{map[string]any{"type": "message", "id": "msg_" + stableTextID(response.ID), "status": "completed", "role": "assistant", "content": textParts}}, output...)
	}
	status := "completed"
	if response.StopReason == "error" {
		status = "failed"
	}
	payload := map[string]any{"id": response.ID, "object": "response", "created_at": response.CreatedAt, "status": status, "model": response.Model, "output": output, "parallel_tool_calls": true, "usage": map[string]any{"input_tokens": response.Usage.InputTokens, "output_tokens": response.Usage.OutputTokens, "total_tokens": response.Usage.TotalTokens()}}
	if response.Usage.CacheReadTokens > 0 {
		payload["usage"].(map[string]any)["input_tokens_details"] = map[string]any{"cached_tokens": response.Usage.CacheReadTokens}
	}
	return json.Marshal(payload)
}

func encodeAnthropicResponse(response CanonicalTextResponse) ([]byte, error) {
	blocks, err := encodeAnthropicContent(response.Content)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{"id": response.ID, "type": "message", "role": "assistant", "model": response.Model, "content": blocks, "stop_reason": anthropicStopReason(response.StopReason), "stop_sequence": nil, "usage": map[string]any{"input_tokens": response.Usage.InputTokens, "output_tokens": response.Usage.OutputTokens, "cache_read_input_tokens": response.Usage.CacheReadTokens, "cache_creation_input_tokens": response.Usage.CacheWriteTokens}}
	return json.Marshal(payload)
}

func encodeGeminiResponse(response CanonicalTextResponse) ([]byte, error) {
	parts, err := encodeGeminiParts(response.Content, nil)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{"responseId": response.ID, "modelVersion": response.Model, "candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": parts}, "finishReason": geminiStopReason(response.StopReason), "index": 0}}, "usageMetadata": map[string]any{"promptTokenCount": response.Usage.InputTokens, "candidatesTokenCount": response.Usage.OutputTokens, "totalTokenCount": response.Usage.TotalTokens(), "cachedContentTokenCount": response.Usage.CacheReadTokens}}
	return json.Marshal(payload)
}

func normalizeStopReason(reason string) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "stop", "end_turn", "stop_sequence", "finished":
		return "stop"
	case "length", "max_tokens", "max_token", "max_tokens_reached":
		return "length"
	case "tool_calls", "tool_use", "function_call", "malformed_function_call":
		return "tool_use"
	case "content_filter", "safety", "recitation", "blocklist", "prohibited_content", "spii":
		return "content_filter"
	case "":
		return "stop"
	default:
		return strings.ToLower(strings.TrimSpace(reason))
	}
}
func openAIStopReason(reason string) string {
	switch normalizeStopReason(reason) {
	case "tool_use":
		return "tool_calls"
	case "content_filter":
		return "content_filter"
	default:
		return normalizeStopReason(reason)
	}
}
func anthropicStopReason(reason string) string {
	switch normalizeStopReason(reason) {
	case "length":
		return "max_tokens"
	case "tool_use":
		return "tool_use"
	default:
		return "end_turn"
	}
}
func geminiStopReason(reason string) string {
	switch normalizeStopReason(reason) {
	case "length":
		return "MAX_TOKENS"
	case "content_filter":
		return "SAFETY"
	case "tool_use":
		return "STOP"
	default:
		return "STOP"
	}
}
func hasToolCall(content []TextContent) bool {
	for _, item := range content {
		if item.Kind == TextContentToolCall {
			return true
		}
	}
	return false
}
func mustMarshalJSON(value any) json.RawMessage { data, _ := json.Marshal(value); return data }
