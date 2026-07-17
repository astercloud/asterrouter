package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/gin-gonic/gin"
)

type gatewayUpstreamStatusError struct {
	StatusCode int
	Message    string
}

func (e *gatewayUpstreamStatusError) Error() string {
	return fmt.Sprintf("upstream provider returned HTTP %d: %s", e.StatusCode, e.Message)
}

func gatewayUpstreamErrorMessage(status int, body []byte) string {
	var payload struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)
	message := strings.TrimSpace(payload.Error.Message)
	if message == "" {
		message = strings.TrimSpace(payload.Message)
	}
	if message == "" {
		message = http.StatusText(status)
	}
	if message == "" {
		message = "upstream request failed"
	}
	runes := []rune(message)
	if len(runes) > 1024 {
		message = string(runes[:1024])
	}
	return message
}

func writeGatewayError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, controlplane.ErrGatewayUnauthorized):
		openAIError(c, http.StatusUnauthorized, "invalid_api_key", "invalid or missing gateway api key")
	case errors.Is(err, controlplane.ErrGatewayForbidden):
		openAIError(c, http.StatusForbidden, "model_not_allowed", "gateway api key is not allowed to use this model")
	case errors.Is(err, controlplane.ErrGatewayPolicyForbidden):
		openAIError(c, http.StatusForbidden, "policy_not_allowed", "gateway credential policy does not allow this request")
	case errors.Is(err, controlplane.ErrGatewayIdempotencyConflict):
		openAIError(c, http.StatusConflict, "idempotency_conflict", "idempotency key was already used for a different request")
	case errors.Is(err, controlplane.ErrGatewayIdempotencyReplay):
		openAIError(c, http.StatusConflict, "idempotency_replay_unavailable", "direct request with this idempotency key was already accepted")
	case errors.Is(err, controlplane.ErrAIJobIdempotencyRequired):
		openAIError(c, http.StatusBadRequest, "idempotency_key_required", "durable job creation requires an Idempotency-Key header")
	case errors.Is(err, controlplane.ErrAIJobCapabilityMismatch):
		openAIError(c, http.StatusBadRequest, "capability_mismatch", "gateway model does not support the requested job capability")
	case errors.Is(err, controlplane.ErrAIJobNotCancelable):
		openAIError(c, http.StatusConflict, "job_not_cancelable", "ai job is already in a non-cancelable terminal state")
	case errors.Is(err, controlplane.ErrAIJobStateConflict):
		openAIError(c, http.StatusConflict, "job_state_conflict", "ai job state changed concurrently")
	case errors.Is(err, controlplane.ErrArtifactNotFound), errors.Is(err, controlplane.ErrAIJobNotFound):
		openAIError(c, http.StatusNotFound, "resource_not_found", "requested resource was not found")
	case errors.Is(err, controlplane.ErrAIJobQueueCapacityExceeded):
		c.Header("Retry-After", strconv.Itoa(controlplane.AIJobDefaultPollAfter))
		openAIError(c, http.StatusTooManyRequests, "queue_capacity_exceeded", "durable ai job queue capacity exceeded")
	case errors.Is(err, controlplane.ErrBillingHoldBudgetExceeded):
		openAIError(c, http.StatusPaymentRequired, "budget_hold_failed", "request cost reservation exceeds the available monthly budget")
	case errors.Is(err, controlplane.ErrBillingHoldEstimateUnavailable):
		openAIError(c, http.StatusPaymentRequired, "budget_hold_failed", "request cost cannot be reserved without an applicable price")
	case errors.Is(err, controlplane.ErrBillingHoldImageQuotaExceeded):
		openAIError(c, http.StatusTooManyRequests, "image_quota_exceeded", "gateway credential monthly image quota exceeded")
	case errors.Is(err, controlplane.ErrBillingHoldVideoQuotaExceeded):
		openAIError(c, http.StatusTooManyRequests, "video_quota_exceeded", "gateway credential monthly video quota exceeded")
	case errors.Is(err, controlplane.ErrBillingHoldAudioQuotaExceeded):
		openAIError(c, http.StatusTooManyRequests, "audio_quota_exceeded", "gateway credential monthly audio quota exceeded")
	case errors.Is(err, controlplane.ErrBillingHoldUsageEstimate):
		openAIError(c, http.StatusBadRequest, "usage_estimate_required", "request must include a bounded media usage estimate")
	case errors.Is(err, controlplane.ErrBillingHoldStateConflict):
		openAIError(c, http.StatusConflict, "billing_hold_conflict", "request cost reservation state changed concurrently")
	case errors.Is(err, controlplane.ErrArtifactStateConflict):
		openAIError(c, http.StatusConflict, "artifact_state_conflict", "artifact state changed concurrently")
	case errors.Is(err, controlplane.ErrArtifactTooLarge):
		openAIError(c, http.StatusRequestEntityTooLarge, "artifact_too_large", "artifact exceeds the configured size limit")
	case errors.Is(err, controlplane.ErrArtifactIntegrity):
		openAIError(c, http.StatusUnprocessableEntity, "artifact_integrity_failed", "artifact integrity verification failed")
	case errors.Is(err, controlplane.ErrArtifactUnavailable):
		openAIError(c, http.StatusGone, "artifact_unavailable", "artifact content is unavailable")
	case errors.Is(err, controlplane.ErrArtifactStoreRequired):
		openAIError(c, http.StatusServiceUnavailable, "artifact_store_unavailable", "artifact content store is unavailable")
	case errors.Is(err, controlplane.ErrArtifactProxyRequired):
		openAIError(c, http.StatusServiceUnavailable, "artifact_proxy_unavailable", "artifact provider proxy is unavailable")
	case errors.Is(err, controlplane.ErrArtifactSinkRequired):
		openAIError(c, http.StatusServiceUnavailable, "artifact_sink_unavailable", "artifact customer sink is unavailable")
	case errors.Is(err, controlplane.ErrArtifactUploadIncomplete):
		openAIError(c, http.StatusConflict, "upload_incomplete", "upload session has not received all expected bytes")
	case errors.Is(err, controlplane.ErrArtifactUploadOffset):
		openAIError(c, http.StatusConflict, "upload_offset_conflict", "upload session chunks are not contiguous")
	case errors.Is(err, controlplane.ErrArtifactUploadInvalid):
		openAIError(c, http.StatusBadRequest, "invalid_upload", "upload session metadata is invalid")
	case errors.Is(err, gatewaycore.ErrInvalidCanonicalRequest):
		openAIError(c, http.StatusBadRequest, "invalid_request_error", "invalid gateway request")
	case errors.Is(err, controlplane.ErrGatewayRouteUnavailable):
		openAIError(c, http.StatusServiceUnavailable, "route_unavailable", "no schedulable provider account is available for this model")
	case errors.Is(err, controlplane.ErrGatewayRateLimited):
		openAIError(c, http.StatusTooManyRequests, "rate_limit_exceeded", "gateway api key qps limit exceeded")
	case errors.Is(err, controlplane.ErrGatewayCapacityLimited):
		openAIError(c, http.StatusTooManyRequests, "capacity_limit_exceeded", "gateway credential capacity limit exceeded")
	case errors.Is(err, controlplane.ErrGatewayQuotaExceeded):
		openAIError(c, http.StatusTooManyRequests, "insufficient_quota", "gateway api key monthly token quota exceeded")
	case errors.Is(err, controlplane.ErrGatewayBudgetExceeded):
		openAIError(c, http.StatusTooManyRequests, "insufficient_quota", "workspace key monthly budget exceeded")
	case errors.Is(err, controlplane.ErrGatewayRiskBlocked):
		openAIError(c, http.StatusTooManyRequests, "risk_control_blocked", "gateway api key is temporarily blocked by risk control")
	default:
		openAIError(c, http.StatusInternalServerError, "server_error", err.Error())
	}
}

func openAIError(c *gin.Context, status int, errorType string, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errorType,
		},
	})
}

func writeGatewayProtocolError(c *gin.Context, protocol gatewaycore.Protocol, status int, errorType, message string) {
	if errorType == "upstream_error" && status == http.StatusTooManyRequests {
		errorType = "rate_limit_error"
	}
	switch protocol {
	case gatewaycore.ProtocolAnthropicMessages:
		c.JSON(status, gin.H{"type": "error", "error": gin.H{"type": anthropicErrorType(status, errorType), "message": message}})
	case gatewaycore.ProtocolGeminiGenerate:
		c.JSON(status, gin.H{"error": gin.H{"code": status, "message": message, "status": geminiErrorStatus(status)}})
	default:
		openAIError(c, status, errorType, message)
	}
}

func writeGatewayProtocolStreamError(c *gin.Context, protocol gatewaycore.Protocol, message string) error {
	var payload any
	eventName := ""
	switch protocol {
	case gatewaycore.ProtocolOpenAIResponses:
		eventName = "error"
		payload = gin.H{"type": "error", "code": "upstream_error", "message": message, "param": nil}
	case gatewaycore.ProtocolAnthropicMessages:
		eventName = "error"
		payload = gin.H{"type": "error", "error": gin.H{"type": "api_error", "message": message}}
	case gatewaycore.ProtocolGeminiGenerate:
		payload = gin.H{"error": gin.H{"code": http.StatusBadGateway, "message": message, "status": "INTERNAL"}}
	default:
		payload = gin.H{"error": gin.H{"message": message, "type": "upstream_error"}}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if eventName != "" {
		if _, err := c.Writer.Write([]byte("event: " + eventName + "\n")); err != nil {
			return err
		}
	}
	if _, err := c.Writer.Write(append(append([]byte("data: "), data...), []byte("\n\n")...)); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}

func writeGatewayProtocolControlError(c *gin.Context, protocol gatewaycore.Protocol, err error) {
	switch {
	case errors.Is(err, controlplane.ErrGatewayUnauthorized):
		writeGatewayProtocolError(c, protocol, http.StatusUnauthorized, "invalid_api_key", "invalid or missing gateway api key")
	case errors.Is(err, controlplane.ErrGatewayForbidden):
		writeGatewayProtocolError(c, protocol, http.StatusForbidden, "model_not_allowed", "gateway api key is not allowed to use this model")
	case errors.Is(err, controlplane.ErrGatewayPolicyForbidden):
		writeGatewayProtocolError(c, protocol, http.StatusForbidden, "policy_not_allowed", "gateway policy does not allow this request")
	case errors.Is(err, controlplane.ErrGatewayRouteUnavailable):
		writeGatewayProtocolError(c, protocol, http.StatusServiceUnavailable, "route_unavailable", "no compatible and schedulable provider account is available for this model")
	case errors.Is(err, controlplane.ErrGatewayRateLimited):
		writeGatewayProtocolError(c, protocol, http.StatusTooManyRequests, "rate_limit_exceeded", err.Error())
	case errors.Is(err, controlplane.ErrGatewayCapacityLimited):
		writeGatewayProtocolError(c, protocol, http.StatusTooManyRequests, "capacity_limit_exceeded", err.Error())
	case errors.Is(err, controlplane.ErrGatewayQuotaExceeded):
		writeGatewayProtocolError(c, protocol, http.StatusTooManyRequests, "insufficient_quota", err.Error())
	case errors.Is(err, controlplane.ErrGatewayBudgetExceeded):
		writeGatewayProtocolError(c, protocol, http.StatusTooManyRequests, "budget_exceeded", err.Error())
	case errors.Is(err, controlplane.ErrBillingHoldBudgetExceeded), errors.Is(err, controlplane.ErrBillingHoldEstimateUnavailable):
		writeGatewayProtocolError(c, protocol, http.StatusPaymentRequired, "budget_hold_failed", err.Error())
	case errors.Is(err, controlplane.ErrGatewayIdempotencyConflict):
		writeGatewayProtocolError(c, protocol, http.StatusConflict, "idempotency_conflict", err.Error())
	case errors.Is(err, controlplane.ErrGatewayIdempotencyReplay):
		writeGatewayProtocolError(c, protocol, http.StatusConflict, "idempotency_replay_unavailable", err.Error())
	case errors.Is(err, gatewaycore.ErrUnsupportedTextFeature):
		writeGatewayProtocolError(c, protocol, http.StatusBadRequest, "unsupported_feature", err.Error())
	case errors.Is(err, gatewaycore.ErrInvalidCanonicalRequest):
		writeGatewayProtocolError(c, protocol, http.StatusBadRequest, "invalid_request_error", err.Error())
	default:
		writeGatewayProtocolError(c, protocol, http.StatusInternalServerError, "server_error", err.Error())
	}
}

func anthropicErrorType(status int, errorType string) string {
	switch {
	case status == http.StatusUnauthorized:
		return "authentication_error"
	case status == http.StatusForbidden:
		return "permission_error"
	case status == http.StatusTooManyRequests:
		return "rate_limit_error"
	case status >= http.StatusInternalServerError:
		return "api_error"
	default:
		return "invalid_request_error"
	}
}

func geminiErrorStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "INVALID_ARGUMENT"
	case http.StatusUnauthorized:
		return "UNAUTHENTICATED"
	case http.StatusForbidden:
		return "PERMISSION_DENIED"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusTooManyRequests:
		return "RESOURCE_EXHAUSTED"
	case http.StatusServiceUnavailable:
		return "UNAVAILABLE"
	default:
		return "INTERNAL"
	}
}
