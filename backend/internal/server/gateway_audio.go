package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/gin-gonic/gin"
)

const (
	gatewayAudioRequestBodyLimit = 32 << 20
	gatewayAudioFileBodyLimit    = 25 << 20
	gatewayAudioFormFieldsLimit  = 1 << 20
)

func registerGatewayAudioRoutes(r *gin.Engine, control *controlplane.Service) {
	for _, route := range []struct {
		path     string
		protocol gatewaycore.Protocol
	}{
		{path: "/v1/audio/transcriptions", protocol: gatewaycore.ProtocolOpenAIAudioTranscriptions},
		{path: "/v1/audio/translations", protocol: gatewaycore.ProtocolOpenAIAudioTranslations},
	} {
		route := route
		r.POST(route.path, func(c *gin.Context) {
			request, err := readGatewayAudioMultipartRequest(c, route.protocol)
			if err != nil {
				writeGatewayAudioParseError(c, route.protocol, err, "invalid audio multipart payload")
				return
			}
			handleGatewayProtocolRequest(c, control, route.protocol, request)
		})
	}

	r.POST("/v1/audio/speech", func(c *gin.Context) {
		request, err := readGatewayProtocolBody(c, gatewaycore.CanonicalizeOpenAIAudioSpeech)
		if err != nil {
			writeGatewayProtocolParseError(c, gatewaycore.ProtocolOpenAIAudioSpeech, err, "invalid audio speech payload")
			return
		}
		handleGatewayProtocolRequest(c, control, gatewaycore.ProtocolOpenAIAudioSpeech, request)
	})
}

func readGatewayAudioMultipartRequest(c *gin.Context, protocol gatewaycore.Protocol) (gatewaycore.CanonicalRequest, error) {
	if c == nil || c.Request == nil {
		return gatewaycore.CanonicalRequest{}, gatewaycore.ErrInvalidCanonicalRequest
	}
	contentType := strings.TrimSpace(c.GetHeader("Content-Type"))
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") || strings.TrimSpace(parameters["boundary"]) == "" {
		return gatewaycore.CanonicalRequest{}, gatewaycore.ErrInvalidCanonicalRequest
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, gatewayAudioRequestBodyLimit)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return gatewaycore.CanonicalRequest{}, errGatewayRequestTooLarge
		}
		return gatewaycore.CanonicalRequest{}, err
	}
	reader := multipart.NewReader(bytes.NewReader(raw), parameters["boundary"])
	fields := make(map[string][]string)
	var input *gatewaycore.CanonicalInputArtifact
	var fieldBytes int64
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return gatewaycore.CanonicalRequest{}, gatewaycore.ErrInvalidCanonicalRequest
		}
		name := strings.TrimSpace(part.FormName())
		filename := strings.TrimSpace(part.FileName())
		if filename != "" {
			if name != "file" || input != nil {
				_ = part.Close()
				return gatewaycore.CanonicalRequest{}, gatewaycore.ErrInvalidCanonicalRequest
			}
			content, readErr := io.ReadAll(io.LimitReader(part, gatewayAudioFileBodyLimit+1))
			_ = part.Close()
			if readErr != nil || len(content) == 0 || len(content) > gatewayAudioFileBodyLimit {
				if len(content) > gatewayAudioFileBodyLimit {
					return gatewaycore.CanonicalRequest{}, errGatewayRequestTooLarge
				}
				return gatewaycore.CanonicalRequest{}, gatewaycore.ErrInvalidCanonicalRequest
			}
			digest := sha256.Sum256(content)
			partMediaType := strings.TrimSpace(part.Header.Get("Content-Type"))
			if partMediaType == "" {
				partMediaType = http.DetectContentType(content)
			}
			normalizedMediaType, _, mediaTypeErr := mime.ParseMediaType(partMediaType)
			if mediaTypeErr != nil || strings.TrimSpace(normalizedMediaType) == "" {
				return gatewaycore.CanonicalRequest{}, gatewaycore.ErrInvalidCanonicalRequest
			}
			input = &gatewaycore.CanonicalInputArtifact{
				Filename: path.Base(strings.ReplaceAll(filename, "\\", "/")), MediaType: normalizedMediaType,
				SizeBytes: int64(len(content)), SHA256: hex.EncodeToString(digest[:]), Content: content,
			}
			continue
		}
		if name == "" || name == "file" {
			_ = part.Close()
			return gatewaycore.CanonicalRequest{}, gatewaycore.ErrInvalidCanonicalRequest
		}
		value, readErr := io.ReadAll(io.LimitReader(part, gatewayAudioFormFieldsLimit-fieldBytes+1))
		_ = part.Close()
		fieldBytes += int64(len(value))
		if readErr != nil || fieldBytes > gatewayAudioFormFieldsLimit {
			return gatewaycore.CanonicalRequest{}, errGatewayRequestTooLarge
		}
		fields[name] = append(fields[name], string(value))
	}
	if input == nil {
		return gatewaycore.CanonicalRequest{}, gatewaycore.ErrInvalidCanonicalRequest
	}
	request, err := gatewaycore.CanonicalizeOpenAIAudioFile(fields, *input, raw, contentType, c.Request.Header, protocol)
	if err != nil {
		return gatewaycore.CanonicalRequest{}, err
	}
	request.SourceIP = gatewaySourceIP(c.Request)
	return request, nil
}

func writeGatewayAudioParseError(c *gin.Context, protocol gatewaycore.Protocol, err error, message string) {
	if errors.Is(err, errGatewayRequestTooLarge) {
		openAIError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "audio request exceeds the 32 MiB request or 25 MiB file limit")
		return
	}
	writeGatewayProtocolParseError(c, protocol, err, message)
}

func persistGatewayInputArtifact(ctx context.Context, control *controlplane.Service, operation controlplane.AIOperation, request gatewaycore.CanonicalRequest) (controlplane.Artifact, error) {
	if request.InputArtifact == nil {
		return controlplane.Artifact{}, nil
	}
	input := request.InputArtifact
	create := controlplane.ArtifactCreateInput{
		ID: "artifact_input_" + strings.TrimPrefix(operation.ID, "aio_"), OperationID: operation.ID,
		Role: controlplane.ArtifactRoleInput, Policy: operation.ArtifactPolicy, MediaType: input.MediaType,
		ExpectedSizeBytes: input.SizeBytes, ExpectedSHA256: input.SHA256, MaxBytes: gatewayAudioFileBodyLimit,
	}
	switch operation.ArtifactPolicy {
	case controlplane.GatewayArtifactPolicyTemporary, controlplane.GatewayArtifactPolicyManaged:
		driver, available := control.PrimaryArtifactStoreDriver()
		if !available {
			return controlplane.Artifact{}, controlplane.ErrArtifactStoreRequired
		}
		create.StoreDriver = driver
		return control.CreateArtifactFromReader(ctx, create, bytes.NewReader(input.Content))
	case controlplane.GatewayArtifactPolicyProxyOnly, controlplane.GatewayArtifactPolicyMetadataOnly, controlplane.GatewayArtifactPolicyCustomerSink:
		return control.CreateArtifactFromReader(ctx, create, nil)
	default:
		return controlplane.Artifact{}, gatewaycore.ErrInvalidCanonicalRequest
	}
}

func gatewayAudioProtocol(protocol gatewaycore.Protocol) bool {
	return protocol == gatewaycore.ProtocolOpenAIAudioTranscriptions || protocol == gatewaycore.ProtocolOpenAIAudioTranslations || protocol == gatewaycore.ProtocolOpenAIAudioSpeech
}

func gatewayProtocolRouteSummary(request gatewaycore.CanonicalRequest, provider controlplane.GatewayProvider) string {
	if !gatewayAudioProtocol(request.Protocol) {
		return gatewayRouteSummary(request.Model, provider)
	}
	summary := "Forwarded " + request.Operation + " request for model " + request.Model + " to provider " + provider.ID
	if provider.AccountID != "" {
		summary += " account " + provider.AccountID
	}
	if provider.UpstreamModel != "" && provider.UpstreamModel != request.Model {
		summary += " upstream_model " + provider.UpstreamModel
	}
	return summary
}

func gatewayProtocolRequestSummary(request gatewaycore.CanonicalRequest) string {
	if !gatewayAudioProtocol(request.Protocol) {
		return fmt.Sprintf("chat.completions stream=%t messages=%d", request.Stream, request.MessageCount)
	}
	return fmt.Sprintf("%s response_mode=%s", request.Operation, request.ResponseMode)
}

func gatewayAudioUsageDimensions(request gatewaycore.CanonicalRequest, outputBytes int64) controlplane.UsageDimensions {
	dimensions := make(controlplane.UsageDimensions)
	if request.InputCharacters > 0 {
		dimensions[controlplane.UsageDimensionInputCharacters] = controlplane.UsageDimension{
			Quantity: request.InputCharacters, Unit: controlplane.UsageUnitCharacter, Source: "request", Confidence: controlplane.UsageConfidenceObserved,
		}
	}
	if request.InputAudioDurationMS > 0 {
		dimensions[controlplane.UsageDimensionInputAudioMilliseconds] = controlplane.UsageDimension{
			Quantity: request.InputAudioDurationMS, Unit: controlplane.UsageUnitMillisecond, Source: "request", Confidence: controlplane.UsageConfidenceEstimated,
		}
	}
	if request.AudioDurationMS > 0 {
		dimensions[controlplane.UsageDimensionOutputAudioMilliseconds] = controlplane.UsageDimension{
			Quantity: request.AudioDurationMS, Unit: controlplane.UsageUnitMillisecond, Source: "request", Confidence: controlplane.UsageConfidenceEstimated,
		}
	}
	if outputBytes > 0 {
		dimensions[controlplane.UsageDimensionOutputBytes] = controlplane.UsageDimension{
			Quantity: outputBytes, Unit: controlplane.UsageUnitByte, Source: "gateway", Confidence: controlplane.UsageConfidenceObserved,
		}
	}
	transferBytes := outputBytes
	if request.InputArtifact != nil && request.InputArtifact.SizeBytes > 0 {
		transferBytes += request.InputArtifact.SizeBytes
	}
	if transferBytes > 0 {
		dimensions[controlplane.UsageDimensionTransferBytes] = controlplane.UsageDimension{
			Quantity: transferBytes, Unit: controlplane.UsageUnitByte, Source: "gateway", Confidence: controlplane.UsageConfidenceObserved,
		}
	}
	return dimensions
}
