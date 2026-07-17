package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/gin-gonic/gin"
)

func translateGatewayTextResponse(request gatewaycore.CanonicalRequest, provider controlplane.GatewayProvider, upstreamBody []byte) ([]byte, gatewayUsageObservation, error) {
	response, err := gatewaycore.DecodeCanonicalTextResponse(gatewaycore.UpstreamFormat(provider.UpstreamFormat), upstreamBody, provider.UpstreamModel)
	if err != nil {
		return nil, gatewayUsageObservation{}, err
	}
	response.Model = request.Model
	translated, err := gatewaycore.EncodeCanonicalTextResponse(request.Protocol, response)
	if err != nil {
		return nil, gatewayUsageObservation{}, err
	}
	if !upstreamTextUsagePresent(upstreamBody) {
		return translated, gatewayUsageObservation{UsageNormalizationStatus: usageNormalizationMissing}, nil
	}
	return translated, normalizeCanonicalTextUsage(response.Usage, gatewaycore.UpstreamFormat(provider.UpstreamFormat)), nil
}

func upstreamTextUsagePresent(raw []byte) bool {
	var payload map[string]json.RawMessage
	if json.Unmarshal(raw, &payload) != nil {
		return false
	}
	for _, key := range []string{"usage", "usageMetadata"} {
		value, ok := payload[key]
		if !ok || string(value) == "null" {
			continue
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(value, &object) == nil {
			return true
		}
	}
	return false
}

func normalizeCanonicalTextUsage(usage gatewaycore.TextUsage, format gatewaycore.UpstreamFormat) gatewayUsageObservation {
	var usagePayload map[string]any
	switch format {
	case gatewaycore.UpstreamFormatAnthropic:
		usagePayload = map[string]any{"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens}
		if usage.CacheReadTokens > 0 {
			usagePayload["cache_read_input_tokens"] = usage.CacheReadTokens
		}
		if usage.CacheWriteTokens > 0 {
			usagePayload["cache_creation_input_tokens"] = usage.CacheWriteTokens
		}
	case gatewaycore.UpstreamFormatGemini:
		usagePayload = map[string]any{"promptTokenCount": usage.InputTokens, "candidatesTokenCount": usage.OutputTokens}
		if usage.CacheReadTokens > 0 {
			usagePayload["cachedContentTokenCount"] = usage.CacheReadTokens
		}
	default:
		usagePayload = map[string]any{"prompt_tokens": usage.InputTokens, "completion_tokens": usage.OutputTokens}
		if usage.CacheReadTokens > 0 {
			usagePayload["cached_tokens"] = usage.CacheReadTokens
		}
		if usage.CacheWriteTokens > 0 {
			usagePayload["cache_write_tokens"] = usage.CacheWriteTokens
		}
	}
	payload, _ := json.Marshal(map[string]any{"usage": usagePayload})
	return gatewaycore.NormalizeUsage(payload)
}

type replayReadCloser struct {
	io.Reader
	io.Closer
}

func prepareCanonicalTextStreamResponse(resp *http.Response, request gatewaycore.CanonicalRequest, provider controlplane.GatewayProvider) error {
	if resp == nil || resp.Body == nil || request.Text == nil || !request.Stream || resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "application/vnd.amazon.eventstream") {
		replay, err := prepareBedrockStreamResponse(resp.Body, request, provider)
		if err != nil {
			return err
		}
		resp.Body = replay
		return nil
	}

	originalBody := resp.Body
	reader := bufio.NewReaderSize(originalBody, 32*1024)
	decoder := gatewaycore.NewTextStreamDecoder(gatewaycore.UpstreamFormat(provider.UpstreamFormat), provider.UpstreamModel)
	encoder := gatewaycore.NewTextStreamEncoder(request.Protocol)
	var captured bytes.Buffer
	var eventName string
	var data bytes.Buffer

	replay := func() {
		prefix := append([]byte(nil), captured.Bytes()...)
		resp.Body = &replayReadCloser{Reader: io.MultiReader(bytes.NewReader(prefix), reader), Closer: originalBody}
	}
	flush := func() (bool, error) {
		if data.Len() == 0 {
			eventName = ""
			return false, nil
		}
		events, err := decoder.Feed(eventName, bytes.TrimSuffix(data.Bytes(), []byte("\n")))
		data.Reset()
		eventName = ""
		if err != nil {
			return false, err
		}
		chunks, err := encoder.Encode(events)
		if err != nil {
			return false, err
		}
		for _, chunk := range chunks {
			if len(chunk) > 0 {
				return true, nil
			}
		}
		return false, nil
	}

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			captured.WriteString(line)
			trimmed := strings.TrimRight(line, "\r\n")
			switch {
			case trimmed == "":
				ready, flushErr := flush()
				if flushErr != nil {
					return flushErr
				}
				if ready {
					replay()
					return nil
				}
			case strings.HasPrefix(trimmed, "event:"):
				eventName = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			case strings.HasPrefix(trimmed, "data:"):
				data.WriteString(strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
				data.WriteByte('\n')
			}
		}
		if data.Len() > maxGatewaySSEPendingLineBytes || captured.Len() > maxGatewaySSEPendingLineBytes {
			return fmt.Errorf("upstream SSE bootstrap exceeds %d bytes", maxGatewaySSEPendingLineBytes)
		}
		if errors.Is(err, io.EOF) {
			ready, flushErr := flush()
			if flushErr != nil {
				return flushErr
			}
			if ready {
				replay()
				return nil
			}
			if _, finishErr := decoder.Finish(); finishErr != nil {
				return finishErr
			}
			return errors.New("upstream stream ended before producing a client event")
		}
		if err != nil {
			return err
		}
	}
}

func streamCanonicalGatewayResponse(c *gin.Context, resp *http.Response, request gatewaycore.CanonicalRequest, provider controlplane.GatewayProvider, startedAt time.Time) (gatewayUsageObservation, *int64, error) {
	if request.Text == nil {
		return streamUpstreamResponse(c, resp, startedAt)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, body, ttftMS, err := readUpstreamResponseLimit(resp, startedAt, gatewayUpstreamBodyLimit)
		if err != nil {
			return gatewayUsageObservation{}, ttftMS, err
		}
		return parseGatewayUsage(body), ttftMS, &gatewayUpstreamStatusError{StatusCode: resp.StatusCode, Message: gatewayUpstreamErrorMessage(resp.StatusCode, body)}
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(resp.StatusCode)

	decoder := gatewaycore.NewTextStreamDecoder(gatewaycore.UpstreamFormat(provider.UpstreamFormat), provider.UpstreamModel)
	encoder := gatewaycore.NewTextStreamEncoder(request.Protocol)
	reader := bufio.NewReaderSize(resp.Body, 32*1024)
	var eventName string
	var data bytes.Buffer
	var usage gatewayUsageObservation
	var ttftMS *int64

	emit := func(events []gatewaycore.CanonicalTextResponseEvent) error {
		for _, event := range events {
			if event.Type == gatewaycore.TextEventUsage && event.Usage != nil {
				usage = mergeGatewayUsageObservation(usage, normalizeCanonicalTextUsage(*event.Usage, gatewaycore.UpstreamFormat(provider.UpstreamFormat)))
			}
		}
		chunks, err := encoder.Encode(events)
		if err != nil {
			return err
		}
		for _, chunk := range chunks {
			if len(chunk) == 0 {
				continue
			}
			if ttftMS == nil {
				value := time.Since(startedAt).Milliseconds()
				ttftMS = &value
			}
			if _, err := c.Writer.Write(chunk); err != nil {
				return err
			}
			c.Writer.Flush()
		}
		return nil
	}
	if strings.Contains(contentType, "application/vnd.amazon.eventstream") {
		err := streamCanonicalBedrockResponse(c, resp.Body, decoder, emit)
		if err != nil {
			return usage, ttftMS, err
		}
		if usage.UsageNormalizationStatus == "" {
			usage.UsageNormalizationStatus = usageNormalizationMissing
		}
		return usage, ttftMS, nil
	}

	flush := func() error {
		if data.Len() == 0 {
			eventName = ""
			return nil
		}
		events, err := decoder.Feed(eventName, bytes.TrimSuffix(data.Bytes(), []byte("\n")))
		data.Reset()
		eventName = ""
		if err != nil {
			return err
		}
		return emit(events)
	}

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			switch {
			case trimmed == "":
				if flushErr := flush(); flushErr != nil {
					return usage, ttftMS, flushErr
				}
			case strings.HasPrefix(trimmed, "event:"):
				eventName = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			case strings.HasPrefix(trimmed, "data:"):
				data.WriteString(strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
				data.WriteByte('\n')
			}
		}
		if errors.Is(err, io.EOF) {
			if flushErr := flush(); flushErr != nil {
				return usage, ttftMS, flushErr
			}
			finishEvents, finishErr := decoder.Finish()
			if finishErr != nil {
				return usage, ttftMS, finishErr
			}
			if emitErr := emit(finishEvents); emitErr != nil {
				return usage, ttftMS, emitErr
			}
			if usage.UsageNormalizationStatus == "" {
				usage.UsageNormalizationStatus = usageNormalizationMissing
			}
			return usage, ttftMS, nil
		}
		if err != nil {
			return usage, ttftMS, err
		}
		if data.Len() > maxGatewaySSEPendingLineBytes {
			return usage, ttftMS, fmt.Errorf("upstream SSE event exceeds %d bytes", maxGatewaySSEPendingLineBytes)
		}
	}
}
