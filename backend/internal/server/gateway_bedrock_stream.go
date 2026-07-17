package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream/eventstreamapi"
	"github.com/gin-gonic/gin"
)

type bedrockStreamReader struct {
	decoder    *eventstream.Decoder
	reader     io.Reader
	payloadBuf []byte
}

func newBedrockStreamReader(reader io.Reader) *bedrockStreamReader {
	return &bedrockStreamReader{decoder: eventstream.NewDecoder(), reader: reader}
}

func (r *bedrockStreamReader) Next() (string, []byte, error) {
	message, err := r.decoder.Decode(r.reader, r.payloadBuf[:0])
	if err != nil {
		return "", nil, err
	}
	r.payloadBuf = message.Payload
	messageType, err := requiredBedrockHeader(message.Headers, eventstreamapi.MessageTypeHeader)
	if err != nil {
		return "", nil, err
	}
	switch messageType {
	case eventstreamapi.EventMessageType:
		eventType, headerErr := requiredBedrockHeader(message.Headers, eventstreamapi.EventTypeHeader)
		if headerErr != nil {
			return "", nil, headerErr
		}
		return eventType, message.Payload, nil
	case eventstreamapi.ExceptionMessageType:
		exceptionType, headerErr := requiredBedrockHeader(message.Headers, eventstreamapi.ExceptionTypeHeader)
		if headerErr != nil {
			return "", nil, headerErr
		}
		return "", nil, bedrockStreamError(exceptionType, message.Payload)
	case eventstreamapi.ErrorMessageType:
		code := bedrockHeader(message.Headers, eventstreamapi.ErrorCodeHeader)
		if code == "" {
			code = "unknown_error"
		}
		detail := bedrockHeader(message.Headers, eventstreamapi.ErrorMessageHeader)
		if detail == "" {
			detail = bedrockPayloadMessage(message.Payload)
		}
		return "", nil, fmt.Errorf("Bedrock stream %s: %s", code, detail)
	default:
		return "", nil, fmt.Errorf("Bedrock stream has unsupported message type %q", messageType)
	}
}

func requiredBedrockHeader(headers eventstream.Headers, name string) (string, error) {
	value := bedrockHeader(headers, name)
	if value == "" {
		return "", fmt.Errorf("Bedrock stream header %s is missing", name)
	}
	return value, nil
}

func bedrockHeader(headers eventstream.Headers, name string) string {
	value := headers.Get(name)
	if value == nil {
		return ""
	}
	return strings.TrimSpace(value.String())
}

func bedrockStreamError(code string, payload []byte) error {
	detail := bedrockPayloadMessage(payload)
	if detail == "" {
		detail = code
	}
	return fmt.Errorf("Bedrock stream %s: %s", code, detail)
}

func bedrockPayloadMessage(payload []byte) string {
	var body struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(payload, &body) == nil && strings.TrimSpace(body.Message) != "" {
		return strings.TrimSpace(body.Message)
	}
	return strings.TrimSpace(string(bytes.TrimSpace(payload)))
}

func prepareBedrockStreamResponse(respBody io.ReadCloser, request gatewaycore.CanonicalRequest, provider controlplane.GatewayProvider) (io.ReadCloser, error) {
	var captured bytes.Buffer
	limited := io.LimitReader(respBody, maxGatewaySSEPendingLineBytes+1)
	reader := newBedrockStreamReader(io.TeeReader(limited, &captured))
	decoder := gatewaycore.NewTextStreamDecoder(gatewaycore.UpstreamFormat(provider.UpstreamFormat), provider.UpstreamModel)
	encoder := gatewaycore.NewTextStreamEncoder(request.Protocol)

	for {
		eventName, payload, err := reader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				events, finishErr := decoder.Finish()
				if finishErr != nil {
					return nil, finishErr
				}
				ready, encodeErr := canonicalEventsProduceOutput(encoder, events)
				if encodeErr != nil {
					return nil, encodeErr
				}
				if ready {
					return replayCapturedBody(respBody, captured.Bytes()), nil
				}
				return nil, errors.New("upstream stream ended before producing a client event")
			}
			if captured.Len() > maxGatewaySSEPendingLineBytes {
				return nil, fmt.Errorf("upstream Bedrock bootstrap exceeds %d bytes", maxGatewaySSEPendingLineBytes)
			}
			return nil, err
		}
		if captured.Len() > maxGatewaySSEPendingLineBytes {
			return nil, fmt.Errorf("upstream Bedrock bootstrap exceeds %d bytes", maxGatewaySSEPendingLineBytes)
		}
		events, decodeErr := decoder.Feed(eventName, payload)
		if decodeErr != nil {
			return nil, decodeErr
		}
		ready, encodeErr := canonicalEventsProduceOutput(encoder, events)
		if encodeErr != nil {
			return nil, encodeErr
		}
		if ready {
			return replayCapturedBody(respBody, captured.Bytes()), nil
		}
	}
}

func replayCapturedBody(body io.ReadCloser, captured []byte) io.ReadCloser {
	prefix := append([]byte(nil), captured...)
	return &replayReadCloser{Reader: io.MultiReader(bytes.NewReader(prefix), body), Closer: body}
}

func canonicalEventsProduceOutput(encoder *gatewaycore.TextStreamEncoder, events []gatewaycore.CanonicalTextResponseEvent) (bool, error) {
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

func streamCanonicalBedrockResponse(c *gin.Context, body io.Reader, decoder *gatewaycore.TextStreamDecoder, emit func([]gatewaycore.CanonicalTextResponseEvent) error) error {
	reader := newBedrockStreamReader(body)
	for {
		eventName, payload, err := reader.Next()
		if errors.Is(err, io.EOF) {
			events, finishErr := decoder.Finish()
			if finishErr != nil {
				return finishErr
			}
			return emit(events)
		}
		if err != nil {
			return err
		}
		events, decodeErr := decoder.Feed(eventName, payload)
		if decodeErr != nil {
			return decodeErr
		}
		if emitErr := emit(events); emitErr != nil {
			return emitErr
		}
		if c.Request.Context().Err() != nil {
			return c.Request.Context().Err()
		}
	}
}
