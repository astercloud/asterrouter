package gatewaycore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type TextResponseEventType string

const (
	TextEventStart         TextResponseEventType = "start"
	TextEventTextDelta     TextResponseEventType = "text_delta"
	TextEventToolCallStart TextResponseEventType = "tool_call_start"
	TextEventToolCallDelta TextResponseEventType = "tool_call_delta"
	TextEventUsage         TextResponseEventType = "usage"
	TextEventFinish        TextResponseEventType = "finish"
)

type CanonicalTextResponseEvent struct {
	Type       TextResponseEventType `json:"type"`
	ID         string                `json:"id,omitempty"`
	Model      string                `json:"model,omitempty"`
	Text       string                `json:"text,omitempty"`
	ToolIndex  int                   `json:"tool_index,omitempty"`
	ToolCall   *TextToolCall         `json:"tool_call,omitempty"`
	Arguments  string                `json:"arguments,omitempty"`
	Usage      *TextUsage            `json:"usage,omitempty"`
	StopReason string                `json:"stop_reason,omitempty"`
}

type TextStreamDecoder struct {
	format      UpstreamFormat
	model       string
	id          string
	started     bool
	finished    bool
	toolIDs     map[int]string
	toolNames   map[int]string
	geminiTools int
	lastUsage   TextUsage
	pendingStop string
}

func NewTextStreamDecoder(format UpstreamFormat, model string) *TextStreamDecoder {
	return &TextStreamDecoder{format: format, model: model, toolIDs: map[int]string{}, toolNames: map[int]string{}}
}

func (d *TextStreamDecoder) Feed(eventName string, data []byte) ([]CanonicalTextResponseEvent, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	switch d.format {
	case UpstreamFormatOpenAIChat:
		return d.decodeOpenAIChatChunk(data)
	case UpstreamFormatOpenAIResponses:
		return d.decodeOpenAIResponsesEvent(eventName, data)
	case UpstreamFormatAnthropic:
		return d.decodeAnthropicEvent(eventName, data)
	case UpstreamFormatGemini:
		return d.decodeGeminiChunk(data)
	case UpstreamFormatBedrockConverse:
		return d.decodeBedrockEvent(eventName, data)
	default:
		return nil, unsupportedTextFeature("upstream_format", string(d.format))
	}
}

func (d *TextStreamDecoder) Finish() ([]CanonicalTextResponseEvent, error) {
	if d.finished {
		return nil, nil
	}
	if d.pendingStop != "" {
		return d.finish(d.pendingStop), nil
	}
	return nil, fmt.Errorf("%w: upstream stream ended without a terminal event", ErrInvalidCanonicalRequest)
}

func (d *TextStreamDecoder) start(id, model string) []CanonicalTextResponseEvent {
	if id != "" {
		d.id = id
	}
	if model != "" {
		d.model = model
	}
	if d.id == "" {
		d.id = "resp_" + stableTextID(d.model+fmt.Sprint(time.Now().UnixNano()))
	}
	if d.started {
		return nil
	}
	d.started = true
	return []CanonicalTextResponseEvent{{Type: TextEventStart, ID: d.id, Model: d.model}}
}

func (d *TextStreamDecoder) finish(reason string) []CanonicalTextResponseEvent {
	if d.finished {
		return nil
	}
	d.finished = true
	return []CanonicalTextResponseEvent{{Type: TextEventFinish, ID: d.id, Model: d.model, StopReason: normalizeStopReason(reason)}}
}

func (d *TextStreamDecoder) decodeOpenAIChatChunk(data []byte) ([]CanonicalTextResponseEvent, error) {
	if string(data) == "[DONE]" {
		reason := d.pendingStop
		if reason == "" {
			reason = "stop"
		}
		return d.finish(reason), nil
	}
	var chunk struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Delta struct {
				Content   *string `json:"content"`
				ToolCalls []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			PromptDetails    struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(data, &chunk) != nil {
		return nil, fmt.Errorf("%w: invalid OpenAI stream chunk", ErrInvalidCanonicalRequest)
	}
	events := d.start(chunk.ID, chunk.Model)
	if len(chunk.Choices) > 1 {
		return nil, unsupportedTextFeature("stream_choices", "multiple choices are not portable")
	}
	if len(chunk.Choices) == 1 {
		choice := chunk.Choices[0]
		if choice.Delta.Content != nil {
			events = append(events, CanonicalTextResponseEvent{Type: TextEventTextDelta, Text: *choice.Delta.Content})
		}
		for _, call := range choice.Delta.ToolCalls {
			if call.ID != "" || call.Function.Name != "" {
				id := call.ID
				if id == "" {
					id = d.toolIDs[call.Index]
				}
				if id == "" {
					id = "call_" + stableTextID(fmt.Sprintf("%s:%d", d.id, call.Index))
				}
				d.toolIDs[call.Index] = id
				if call.Function.Name != "" {
					d.toolNames[call.Index] = call.Function.Name
				}
				events = append(events, CanonicalTextResponseEvent{Type: TextEventToolCallStart, ToolIndex: call.Index, ToolCall: &TextToolCall{ID: id, Name: d.toolNames[call.Index], Arguments: json.RawMessage(`{}`)}})
			}
			if call.Function.Arguments != "" {
				events = append(events, CanonicalTextResponseEvent{Type: TextEventToolCallDelta, ToolIndex: call.Index, Arguments: call.Function.Arguments})
			}
		}
	}
	if chunk.Usage != nil {
		usage := TextUsage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens, CacheReadTokens: chunk.Usage.PromptDetails.CachedTokens}
		d.lastUsage = usage
		events = append(events, CanonicalTextResponseEvent{Type: TextEventUsage, Usage: &usage})
	}
	if len(chunk.Choices) == 1 && chunk.Choices[0].FinishReason != nil {
		d.pendingStop = normalizeStopReason(*chunk.Choices[0].FinishReason)
	}
	return events, nil
}

func (d *TextStreamDecoder) decodeOpenAIResponsesEvent(eventName string, data []byte) ([]CanonicalTextResponseEvent, error) {
	var event map[string]json.RawMessage
	if json.Unmarshal(data, &event) != nil {
		return nil, fmt.Errorf("%w: invalid OpenAI Responses event", ErrInvalidCanonicalRequest)
	}
	var eventType string
	_ = json.Unmarshal(event["type"], &eventType)
	if eventType == "" {
		eventType = eventName
	}
	events := []CanonicalTextResponseEvent{}
	switch eventType {
	case "response.created", "response.in_progress":
		var response struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		}
		_ = json.Unmarshal(event["response"], &response)
		events = append(events, d.start(response.ID, response.Model)...)
	case "response.output_text.delta":
		var delta string
		_ = json.Unmarshal(event["delta"], &delta)
		events = append(events, d.start("", "")...)
		events = append(events, CanonicalTextResponseEvent{Type: TextEventTextDelta, Text: delta})
	case "response.output_item.added":
		var item struct {
			Type   string `json:"type"`
			ID     string `json:"id"`
			CallID string `json:"call_id"`
			Name   string `json:"name"`
		}
		_ = json.Unmarshal(event["item"], &item)
		if item.Type == "function_call" {
			var index int
			_ = json.Unmarshal(event["output_index"], &index)
			id := item.CallID
			if id == "" {
				id = item.ID
			}
			d.toolIDs[index] = id
			d.toolNames[index] = item.Name
			events = append(events, CanonicalTextResponseEvent{Type: TextEventToolCallStart, ToolIndex: index, ToolCall: &TextToolCall{ID: id, Name: item.Name, Arguments: json.RawMessage(`{}`)}})
		}
	case "response.function_call_arguments.delta":
		var delta string
		var index int
		_ = json.Unmarshal(event["delta"], &delta)
		_ = json.Unmarshal(event["output_index"], &index)
		events = append(events, CanonicalTextResponseEvent{Type: TextEventToolCallDelta, ToolIndex: index, Arguments: delta})
	case "response.completed", "response.failed", "response.incomplete":
		var response struct {
			ID     string `json:"id"`
			Model  string `json:"model"`
			Status string `json:"status"`
			Usage  struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
				InputDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"input_tokens_details"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(event["response"], &response)
		events = append(events, d.start(response.ID, response.Model)...)
		usage := TextUsage{InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens, CacheReadTokens: response.Usage.InputDetails.CachedTokens}
		if usage.InputTokens > 0 || usage.OutputTokens > 0 {
			d.lastUsage = usage
			events = append(events, CanonicalTextResponseEvent{Type: TextEventUsage, Usage: &usage})
		}
		reason := "stop"
		if eventType != "response.completed" {
			reason = "error"
		}
		if len(d.toolIDs) > 0 {
			reason = "tool_use"
		}
		events = append(events, d.finish(reason)...)
	case "error":
		return nil, fmt.Errorf("%w: upstream Responses stream error", ErrInvalidCanonicalRequest)
	}
	return events, nil
}

func (d *TextStreamDecoder) decodeAnthropicEvent(eventName string, data []byte) ([]CanonicalTextResponseEvent, error) {
	var event map[string]json.RawMessage
	if json.Unmarshal(data, &event) != nil {
		return nil, fmt.Errorf("%w: invalid Anthropic stream event", ErrInvalidCanonicalRequest)
	}
	var eventType string
	_ = json.Unmarshal(event["type"], &eventType)
	if eventType == "" {
		eventType = eventName
	}
	events := []CanonicalTextResponseEvent{}
	switch eventType {
	case "message_start":
		var message struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Usage struct {
				InputTokens int `json:"input_tokens"`
				CacheRead   int `json:"cache_read_input_tokens"`
				CacheWrite  int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(event["message"], &message)
		events = append(events, d.start(message.ID, message.Model)...)
		if message.Usage.InputTokens > 0 || message.Usage.CacheRead > 0 || message.Usage.CacheWrite > 0 {
			d.lastUsage.InputTokens = message.Usage.InputTokens
			d.lastUsage.CacheReadTokens = message.Usage.CacheRead
			d.lastUsage.CacheWriteTokens = message.Usage.CacheWrite
			usage := d.lastUsage
			events = append(events, CanonicalTextResponseEvent{Type: TextEventUsage, Usage: &usage})
		}
	case "content_block_start":
		var index int
		var block struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		_ = json.Unmarshal(event["index"], &index)
		_ = json.Unmarshal(event["content_block"], &block)
		if block.Type == "tool_use" {
			d.toolIDs[index] = block.ID
			d.toolNames[index] = block.Name
			events = append(events, CanonicalTextResponseEvent{Type: TextEventToolCallStart, ToolIndex: index, ToolCall: &TextToolCall{ID: block.ID, Name: block.Name, Arguments: json.RawMessage(`{}`)}})
		} else if block.Type != "text" {
			return nil, unsupportedTextFeature("anthropic_stream_block", block.Type)
		}
	case "content_block_delta":
		var index int
		var delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
		}
		_ = json.Unmarshal(event["index"], &index)
		_ = json.Unmarshal(event["delta"], &delta)
		switch delta.Type {
		case "text_delta":
			events = append(events, CanonicalTextResponseEvent{Type: TextEventTextDelta, Text: delta.Text})
		case "input_json_delta":
			events = append(events, CanonicalTextResponseEvent{Type: TextEventToolCallDelta, ToolIndex: index, Arguments: delta.PartialJSON})
		default:
			return nil, unsupportedTextFeature("anthropic_stream_delta", delta.Type)
		}
	case "message_delta":
		var delta struct {
			StopReason string `json:"stop_reason"`
		}
		var usage struct {
			OutputTokens int `json:"output_tokens"`
		}
		_ = json.Unmarshal(event["delta"], &delta)
		_ = json.Unmarshal(event["usage"], &usage)
		if usage.OutputTokens > 0 {
			d.lastUsage.OutputTokens = usage.OutputTokens
			copy := d.lastUsage
			events = append(events, CanonicalTextResponseEvent{Type: TextEventUsage, Usage: &copy})
		}
		if delta.StopReason != "" {
			events = append(events, d.finish(delta.StopReason)...)
		}
	case "message_stop":
		events = append(events, d.finish("stop")...)
	case "error":
		return nil, fmt.Errorf("%w: upstream Anthropic stream error", ErrInvalidCanonicalRequest)
	}
	return events, nil
}

func (d *TextStreamDecoder) decodeGeminiChunk(data []byte) ([]CanonicalTextResponseEvent, error) {
	var payload struct {
		ResponseID string `json:"responseId"`
		Model      string `json:"modelVersion"`
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         *string `json:"text"`
					FunctionCall *struct {
						Name string          `json:"name"`
						ID   string          `json:"id"`
						Args json.RawMessage `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		Usage struct {
			Input  int `json:"promptTokenCount"`
			Output int `json:"candidatesTokenCount"`
			Cache  int `json:"cachedContentTokenCount"`
		} `json:"usageMetadata"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return nil, fmt.Errorf("%w: invalid Gemini stream chunk", ErrInvalidCanonicalRequest)
	}
	events := d.start(payload.ResponseID, payload.Model)
	if len(payload.Candidates) > 1 {
		return nil, unsupportedTextFeature("stream_candidates", "multiple Gemini candidates are not portable")
	}
	if len(payload.Candidates) == 1 {
		candidate := payload.Candidates[0]
		for _, part := range candidate.Content.Parts {
			if part.Text != nil {
				events = append(events, CanonicalTextResponseEvent{Type: TextEventTextDelta, Text: *part.Text})
			} else if part.FunctionCall != nil {
				id := part.FunctionCall.ID
				if id == "" {
					id = "call_" + stableTextID(part.FunctionCall.Name+fmt.Sprint(d.geminiTools))
				}
				index := d.geminiTools
				d.geminiTools++
				d.toolIDs[index] = id
				d.toolNames[index] = part.FunctionCall.Name
				events = append(events, CanonicalTextResponseEvent{Type: TextEventToolCallStart, ToolIndex: index, ToolCall: &TextToolCall{ID: id, Name: part.FunctionCall.Name, Arguments: json.RawMessage(`{}`)}})
				if rawJSONPresent(part.FunctionCall.Args) {
					events = append(events, CanonicalTextResponseEvent{Type: TextEventToolCallDelta, ToolIndex: index, Arguments: string(part.FunctionCall.Args)})
				}
			}
		}
		if candidate.FinishReason != "" {
			d.pendingStop = normalizeStopReason(candidate.FinishReason)
		}
	}
	if payload.Usage.Input > 0 || payload.Usage.Output > 0 {
		usage := TextUsage{InputTokens: payload.Usage.Input, OutputTokens: payload.Usage.Output, CacheReadTokens: payload.Usage.Cache}
		d.lastUsage = usage
		events = append(events, CanonicalTextResponseEvent{Type: TextEventUsage, Usage: &usage})
	}
	return events, nil
}

func (d *TextStreamDecoder) decodeBedrockEvent(eventName string, data []byte) ([]CanonicalTextResponseEvent, error) {
	events := []CanonicalTextResponseEvent{}
	switch eventName {
	case "messageStart":
		events = append(events, d.start("", d.model)...)
	case "contentBlockStart":
		var payload struct {
			Start struct {
				ToolUse *struct {
					ToolUseID string `json:"toolUseId"`
					Name      string `json:"name"`
				} `json:"toolUse"`
			} `json:"start"`
			ContentBlockIndex int `json:"contentBlockIndex"`
		}
		if json.Unmarshal(data, &payload) != nil {
			return nil, ErrInvalidCanonicalRequest
		}
		if payload.Start.ToolUse != nil {
			index := payload.ContentBlockIndex
			d.toolIDs[index] = payload.Start.ToolUse.ToolUseID
			d.toolNames[index] = payload.Start.ToolUse.Name
			events = append(events, CanonicalTextResponseEvent{Type: TextEventToolCallStart, ToolIndex: index, ToolCall: &TextToolCall{ID: payload.Start.ToolUse.ToolUseID, Name: payload.Start.ToolUse.Name, Arguments: json.RawMessage(`{}`)}})
		}
	case "contentBlockDelta":
		var payload struct {
			Delta struct {
				Text    *string `json:"text"`
				ToolUse *struct {
					Input string `json:"input"`
				} `json:"toolUse"`
			} `json:"delta"`
			ContentBlockIndex int `json:"contentBlockIndex"`
		}
		if json.Unmarshal(data, &payload) != nil {
			return nil, ErrInvalidCanonicalRequest
		}
		if payload.Delta.Text != nil {
			events = append(events, CanonicalTextResponseEvent{Type: TextEventTextDelta, Text: *payload.Delta.Text})
		}
		if payload.Delta.ToolUse != nil {
			events = append(events, CanonicalTextResponseEvent{Type: TextEventToolCallDelta, ToolIndex: payload.ContentBlockIndex, Arguments: payload.Delta.ToolUse.Input})
		}
	case "messageStop":
		var payload struct {
			StopReason string `json:"stopReason"`
		}
		_ = json.Unmarshal(data, &payload)
		d.pendingStop = normalizeStopReason(payload.StopReason)
	case "metadata":
		var payload struct {
			Usage struct {
				Input      int `json:"inputTokens"`
				Output     int `json:"outputTokens"`
				CacheRead  int `json:"cacheReadInputTokens"`
				CacheWrite int `json:"cacheWriteInputTokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(data, &payload)
		usage := TextUsage{InputTokens: payload.Usage.Input, OutputTokens: payload.Usage.Output, CacheReadTokens: payload.Usage.CacheRead, CacheWriteTokens: payload.Usage.CacheWrite}
		d.lastUsage = usage
		events = append(events, CanonicalTextResponseEvent{Type: TextEventUsage, Usage: &usage})
		if d.pendingStop != "" {
			events = append(events, d.finish(d.pendingStop)...)
		}
	default:
		if strings.HasSuffix(strings.ToLower(eventName), "exception") {
			return nil, fmt.Errorf("%w: Bedrock stream %s", ErrInvalidCanonicalRequest, eventName)
		}
	}
	return events, nil
}

type TextStreamEncoder struct {
	protocol       Protocol
	id             string
	model          string
	createdAt      int64
	started        bool
	finished       bool
	openBlocks     map[int]bool
	toolCalls      map[int]*TextToolCall
	toolArguments  map[int]string
	usage          TextUsage
	responsesIndex int
	responsesText  int
	responsesTools map[int]int
	text           strings.Builder
	nextBlock      int
	anthropicText  int
	anthropicTools map[int]int
}

func NewTextStreamEncoder(protocol Protocol) *TextStreamEncoder {
	return &TextStreamEncoder{
		protocol: protocol, createdAt: time.Now().Unix(), openBlocks: map[int]bool{},
		toolCalls: map[int]*TextToolCall{}, toolArguments: map[int]string{},
		responsesText: -1, responsesTools: map[int]int{}, anthropicText: -1, anthropicTools: map[int]int{},
	}
}

func (e *TextStreamEncoder) Encode(events []CanonicalTextResponseEvent) ([][]byte, error) {
	out := [][]byte{}
	for _, event := range events {
		chunks, err := e.encode(event)
		if err != nil {
			return nil, err
		}
		out = append(out, chunks...)
	}
	return out, nil
}

func (e *TextStreamEncoder) encode(event CanonicalTextResponseEvent) ([][]byte, error) {
	if event.ID != "" {
		e.id = event.ID
	}
	if event.Model != "" {
		e.model = event.Model
	}
	switch e.protocol {
	case ProtocolOpenAIChat:
		return e.encodeOpenAIChat(event)
	case ProtocolOpenAIResponses:
		return e.encodeOpenAIResponses(event)
	case ProtocolAnthropicMessages:
		return e.encodeAnthropic(event)
	case ProtocolGeminiGenerate:
		return e.encodeGemini(event)
	default:
		return nil, unsupportedTextFeature("client_protocol", string(e.protocol))
	}
}

func (e *TextStreamEncoder) encodeOpenAIChat(event CanonicalTextResponseEvent) ([][]byte, error) {
	chunks := [][]byte{}
	base := func(delta any, finish any, usage any) []byte {
		payload := map[string]any{"id": e.id, "object": "chat.completion.chunk", "created": e.createdAt, "model": e.model, "choices": []any{}}
		if delta != nil || finish != nil {
			payload["choices"] = []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}
		}
		if usage != nil {
			payload["usage"] = usage
		}
		return sseData(payload)
	}
	switch event.Type {
	case TextEventStart:
		if !e.started {
			e.started = true
			chunks = append(chunks, base(map[string]any{"role": "assistant", "content": ""}, nil, nil))
		}
	case TextEventTextDelta:
		chunks = append(chunks, base(map[string]any{"content": event.Text}, nil, nil))
	case TextEventToolCallStart:
		e.toolCalls[event.ToolIndex] = event.ToolCall
		chunks = append(chunks, base(map[string]any{"tool_calls": []any{map[string]any{"index": event.ToolIndex, "id": event.ToolCall.ID, "type": "function", "function": map[string]any{"name": event.ToolCall.Name, "arguments": ""}}}}, nil, nil))
	case TextEventToolCallDelta:
		e.toolArguments[event.ToolIndex] += event.Arguments
		chunks = append(chunks, base(map[string]any{"tool_calls": []any{map[string]any{"index": event.ToolIndex, "function": map[string]any{"arguments": event.Arguments}}}}, nil, nil))
	case TextEventUsage:
		if event.Usage != nil {
			e.usage = *event.Usage
			chunks = append(chunks, base(nil, nil, map[string]any{"prompt_tokens": e.usage.InputTokens, "completion_tokens": e.usage.OutputTokens, "total_tokens": e.usage.TotalTokens()}))
		}
	case TextEventFinish:
		if !e.finished {
			e.finished = true
			chunks = append(chunks, base(map[string]any{}, openAIStopReason(event.StopReason), nil), []byte("data: [DONE]\n\n"))
		}
	}
	return chunks, nil
}

func (e *TextStreamEncoder) encodeOpenAIResponses(event CanonicalTextResponseEvent) ([][]byte, error) {
	chunks := [][]byte{}
	emit := func(name string, payload map[string]any) {
		payload["type"] = name
		chunks = append(chunks, sseEvent(name, payload))
	}
	switch event.Type {
	case TextEventStart:
		if !e.started {
			e.started = true
			emit("response.created", map[string]any{"response": map[string]any{"id": e.id, "object": "response", "created_at": e.createdAt, "status": "in_progress", "model": e.model, "output": []any{}}})
		}
	case TextEventTextDelta:
		e.text.WriteString(event.Text)
		if !e.openBlocks[-1] {
			e.openBlocks[-1] = true
			index := e.responsesIndex
			e.responsesIndex++
			e.responsesText = index
			emit("response.output_item.added", map[string]any{"output_index": index, "item": map[string]any{"id": "msg_" + stableTextID(e.id), "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}})
			emit("response.content_part.added", map[string]any{"item_id": "msg_" + stableTextID(e.id), "output_index": index, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
		}
		emit("response.output_text.delta", map[string]any{"item_id": "msg_" + stableTextID(e.id), "output_index": e.responsesText, "content_index": 0, "delta": event.Text})
	case TextEventToolCallStart:
		if event.ToolCall == nil {
			return nil, fmt.Errorf("%w: tool call start is missing a tool call", ErrInvalidCanonicalRequest)
		}
		index := e.responsesIndex
		e.responsesIndex++
		e.toolCalls[event.ToolIndex] = event.ToolCall
		e.responsesTools[event.ToolIndex] = index
		emit("response.output_item.added", map[string]any{"output_index": index, "item": map[string]any{"id": "fc_" + stableTextID(event.ToolCall.ID), "type": "function_call", "status": "in_progress", "arguments": "", "call_id": event.ToolCall.ID, "name": event.ToolCall.Name}})
	case TextEventToolCallDelta:
		call := e.toolCalls[event.ToolIndex]
		index, ok := e.responsesTools[event.ToolIndex]
		if call == nil || !ok {
			return nil, fmt.Errorf("%w: tool call delta arrived before tool call start", ErrInvalidCanonicalRequest)
		}
		e.toolArguments[event.ToolIndex] += event.Arguments
		emit("response.function_call_arguments.delta", map[string]any{"item_id": "fc_" + stableTextID(call.ID), "output_index": index, "delta": event.Arguments})
	case TextEventUsage:
		if event.Usage != nil {
			e.usage = *event.Usage
		}
	case TextEventFinish:
		if !e.finished {
			e.finished = true
			output := make([]any, e.responsesIndex)
			if e.responsesText >= 0 {
				itemID := "msg_" + stableTextID(e.id)
				part := map[string]any{"type": "output_text", "text": e.text.String(), "annotations": []any{}}
				item := map[string]any{"id": itemID, "type": "message", "status": "completed", "role": "assistant", "content": []any{part}}
				output[e.responsesText] = item
				emit("response.output_text.done", map[string]any{"item_id": itemID, "output_index": e.responsesText, "content_index": 0, "text": e.text.String()})
				emit("response.content_part.done", map[string]any{"item_id": itemID, "output_index": e.responsesText, "content_index": 0, "part": part})
				emit("response.output_item.done", map[string]any{"output_index": e.responsesText, "item": item})
			}
			toolIndexes := sortedTextToolIndexes(e.toolCalls)
			for _, toolIndex := range toolIndexes {
				call := e.toolCalls[toolIndex]
				outputIndex := e.responsesTools[toolIndex]
				arguments := e.toolArguments[toolIndex]
				itemID := "fc_" + stableTextID(call.ID)
				item := map[string]any{"id": itemID, "type": "function_call", "status": "completed", "arguments": arguments, "call_id": call.ID, "name": call.Name}
				output[outputIndex] = item
				emit("response.function_call_arguments.done", map[string]any{"item_id": itemID, "output_index": outputIndex, "arguments": arguments})
				emit("response.output_item.done", map[string]any{"output_index": outputIndex, "item": item})
			}
			emit("response.completed", map[string]any{"response": map[string]any{"id": e.id, "object": "response", "created_at": e.createdAt, "status": "completed", "model": e.model, "output": output, "usage": map[string]any{"input_tokens": e.usage.InputTokens, "output_tokens": e.usage.OutputTokens, "total_tokens": e.usage.TotalTokens()}}})
		}
	}
	return chunks, nil
}

func (e *TextStreamEncoder) encodeAnthropic(event CanonicalTextResponseEvent) ([][]byte, error) {
	chunks := [][]byte{}
	emit := func(name string, payload map[string]any) {
		payload["type"] = name
		chunks = append(chunks, sseEvent(name, payload))
	}
	switch event.Type {
	case TextEventStart:
		if !e.started {
			e.started = true
			emit("message_start", map[string]any{"message": map[string]any{"id": e.id, "type": "message", "role": "assistant", "model": e.model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": 0, "output_tokens": 0}}})
		}
	case TextEventTextDelta:
		if e.anthropicText < 0 {
			e.anthropicText = e.nextBlock
			e.nextBlock++
			e.openBlocks[e.anthropicText] = true
			emit("content_block_start", map[string]any{"index": e.anthropicText, "content_block": map[string]any{"type": "text", "text": ""}})
		}
		emit("content_block_delta", map[string]any{"index": e.anthropicText, "delta": map[string]any{"type": "text_delta", "text": event.Text}})
	case TextEventToolCallStart:
		if event.ToolCall == nil {
			return nil, fmt.Errorf("%w: tool call start is missing a tool call", ErrInvalidCanonicalRequest)
		}
		index := e.nextBlock
		e.nextBlock++
		e.anthropicTools[event.ToolIndex] = index
		e.openBlocks[index] = true
		e.toolCalls[event.ToolIndex] = event.ToolCall
		emit("content_block_start", map[string]any{"index": index, "content_block": map[string]any{"type": "tool_use", "id": event.ToolCall.ID, "name": event.ToolCall.Name, "input": map[string]any{}}})
	case TextEventToolCallDelta:
		index, ok := e.anthropicTools[event.ToolIndex]
		if !ok {
			return nil, fmt.Errorf("%w: tool call delta arrived before tool call start", ErrInvalidCanonicalRequest)
		}
		e.toolArguments[event.ToolIndex] += event.Arguments
		emit("content_block_delta", map[string]any{"index": index, "delta": map[string]any{"type": "input_json_delta", "partial_json": event.Arguments}})
	case TextEventUsage:
		if event.Usage != nil {
			e.usage = *event.Usage
		}
	case TextEventFinish:
		if !e.finished {
			e.finished = true
			indexes := make([]int, 0, len(e.openBlocks))
			for index := range e.openBlocks {
				indexes = append(indexes, index)
			}
			sort.Ints(indexes)
			for _, index := range indexes {
				emit("content_block_stop", map[string]any{"index": index})
			}
			emit("message_delta", map[string]any{"delta": map[string]any{"stop_reason": anthropicStopReason(event.StopReason), "stop_sequence": nil}, "usage": map[string]any{"output_tokens": e.usage.OutputTokens}})
			emit("message_stop", map[string]any{})
		}
	}
	return chunks, nil
}

func (e *TextStreamEncoder) encodeGemini(event CanonicalTextResponseEvent) ([][]byte, error) {
	chunks := [][]byte{}
	emit := func(parts []any, finish string) {
		candidate := map[string]any{"content": map[string]any{"role": "model", "parts": parts}, "index": 0}
		if finish != "" {
			candidate["finishReason"] = finish
		}
		payload := map[string]any{"responseId": e.id, "modelVersion": e.model, "candidates": []any{candidate}}
		if finish != "" {
			payload["usageMetadata"] = map[string]any{"promptTokenCount": e.usage.InputTokens, "candidatesTokenCount": e.usage.OutputTokens, "totalTokenCount": e.usage.TotalTokens()}
		}
		chunks = append(chunks, sseData(payload))
	}
	switch event.Type {
	case TextEventStart:
		e.started = true
	case TextEventTextDelta:
		emit([]any{map[string]any{"text": event.Text}}, "")
	case TextEventToolCallStart:
		e.toolCalls[event.ToolIndex] = event.ToolCall
	case TextEventToolCallDelta:
		e.toolArguments[event.ToolIndex] += event.Arguments
	case TextEventUsage:
		if event.Usage != nil {
			e.usage = *event.Usage
		}
	case TextEventFinish:
		if !e.finished {
			e.finished = true
			parts := []any{}
			for _, index := range sortedTextToolIndexes(e.toolCalls) {
				call := e.toolCalls[index]
				arguments := json.RawMessage(e.toolArguments[index])
				if !json.Valid(arguments) {
					arguments = json.RawMessage(`{}`)
				}
				parts = append(parts, map[string]any{"functionCall": map[string]any{"id": call.ID, "name": call.Name, "args": arguments}})
			}
			emit(parts, geminiStopReason(event.StopReason))
		}
	}
	return chunks, nil
}

func sortedTextToolIndexes(values map[int]*TextToolCall) []int {
	indexes := make([]int, 0, len(values))
	for index := range values {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	return indexes
}

func sseData(payload any) []byte {
	data, _ := json.Marshal(payload)
	return append(append([]byte("data: "), data...), []byte("\n\n")...)
}
func sseEvent(name string, payload any) []byte {
	data, _ := json.Marshal(payload)
	out := []byte("event: " + name + "\n")
	out = append(out, []byte("data: ")...)
	out = append(out, data...)
	out = append(out, []byte("\n\n")...)
	return out
}
