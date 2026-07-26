package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/gin-gonic/gin"
)

type clientVerificationRequest struct {
	Client     string `json:"client"`
	Model      string `json:"model"`
	Credential string `json:"credential"`
}

func registerOnboardingRoutes(group *gin.RouterGroup, control *controlplane.Service, settingsService *settings.Service) {
	if control == nil {
		return
	}
	group.Use(requireGlobalOnboardingAccess())
	group.GET("/compatibility-records", func(c *gin.Context) {
		routerVersion := "unknown"
		if settingsService != nil {
			current, err := settingsService.Admin(c.Request.Context())
			if err != nil {
				httpx.Error(c, http.StatusServiceUnavailable, 1570, "compatibility records are unavailable")
				return
			}
			routerVersion = current.Version
		}
		httpx.OK(c, control.CompatibilityManifest(routerVersion))
	})
	group.POST("/sessions", func(c *gin.Context) {
		session, err := control.StartOnboardingSession(c.Request.Context(), actor(c), c.GetHeader("Idempotency-Key"))
		if err != nil {
			writeOnboardingError(c, err)
			return
		}
		httpx.OK(c, session)
	})
	group.GET("/sessions/:id", func(c *gin.Context) {
		session, err := control.OnboardingSession(c.Request.Context(), actor(c), c.Param("id"))
		if err != nil {
			writeOnboardingError(c, err)
			return
		}
		httpx.OK(c, session)
	})
	group.POST("/sessions/:id/model-source", func(c *gin.Context) {
		var req controlplane.OnboardingModelSourceRequest
		if err := decodeStrictAdminJSON(c, &req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1560, "invalid onboarding model source payload")
			return
		}
		result, err := control.ConnectOnboardingModelSource(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			if result.Session.ID != "" {
				c.JSON(http.StatusUnprocessableEntity, httpx.Response{Code: 1561, Message: err.Error(), Data: result})
				return
			}
			writeOnboardingError(c, err)
			return
		}
		httpx.OK(c, result)
	})
	group.POST("/sessions/:id/published-model", func(c *gin.Context) {
		var req controlplane.OnboardingPublishedModelRequest
		if err := decodeStrictAdminJSON(c, &req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1562, "invalid onboarding published model payload")
			return
		}
		result, err := control.PublishOnboardingModel(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			if result.Session.ID != "" {
				c.JSON(http.StatusUnprocessableEntity, httpx.Response{Code: 1563, Message: "published model could not be connected", Data: result})
				return
			}
			writeOnboardingError(c, err)
			return
		}
		httpx.OK(c, result)
	})
	group.POST("/sessions/:id/api-key", func(c *gin.Context) {
		var req controlplane.OnboardingAPIKeyRequest
		if err := decodeStrictAdminJSON(c, &req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1564, "invalid onboarding API key payload")
			return
		}
		result, err := control.CreateOnboardingAPIKey(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			writeOnboardingError(c, err)
			return
		}
		httpx.OK(c, result)
	})
	group.POST("/sessions/:id/verification", func(c *gin.Context) {
		session, err := control.OnboardingSession(c.Request.Context(), actor(c), c.Param("id"))
		if err != nil {
			writeOnboardingError(c, err)
			return
		}
		if session.Status == controlplane.OnboardingStatusCompleted && session.VerificationOperationID != "" {
			httpx.OK(c, controlplane.OnboardingVerificationResult{Session: session, Verification: verificationFromSession(session)})
			return
		}
		var req clientVerificationRequest
		if err := decodeStrictAdminJSON(c, &req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1565, "invalid onboarding verification payload")
			return
		}
		result, err := verifyAPIKeyClient(c, control, session.APIKeyID, req, c.GetHeader("Idempotency-Key"))
		if err != nil {
			writeOnboardingError(c, err)
			return
		}
		updated, err := control.CompleteOnboardingVerification(c.Request.Context(), actor(c), session.ID, result)
		if err != nil {
			writeOnboardingError(c, err)
			return
		}
		httpx.OK(c, controlplane.OnboardingVerificationResult{Session: updated, Verification: result})
	})
}

func registerAPIKeyClientRoutes(group *gin.RouterGroup, control *controlplane.Service, settingsService *settings.Service) {
	if control == nil {
		return
	}
	group.GET("/:id", func(c *gin.Context) {
		if err := requireAPIKeyInAccess(c.Request.Context(), control, c.Param("id"), principalAccess(c)); err != nil {
			httpx.Error(c, http.StatusNotFound, 1566, "API key not found")
			return
		}
		key, err := control.OnboardingAPIKey(c.Request.Context(), c.Param("id"))
		if err != nil {
			writeOnboardingError(c, err)
			return
		}
		httpx.OK(c, key)
	})
	group.GET("/:id/client-config", func(c *gin.Context) {
		if err := requireAPIKeyInAccess(c.Request.Context(), control, c.Param("id"), principalAccess(c)); err != nil {
			httpx.Error(c, http.StatusNotFound, 1566, "API key not found")
			return
		}
		gatewayURL, err := publicGatewayURL(c, settingsService)
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1567, "public gateway URL is unavailable")
			return
		}
		config, err := control.APIKeyClientConfig(c.Request.Context(), c.Param("id"), c.Query("client"), c.Query("model"), gatewayURL)
		if err != nil {
			writeOnboardingError(c, err)
			return
		}
		httpx.OK(c, config)
	})
	group.POST("/:id/client-verifications", func(c *gin.Context) {
		if err := requireAPIKeyInAccess(c.Request.Context(), control, c.Param("id"), principalAccess(c)); err != nil {
			httpx.Error(c, http.StatusNotFound, 1566, "API key not found")
			return
		}
		var req clientVerificationRequest
		if err := decodeStrictAdminJSON(c, &req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1568, "invalid client verification payload")
			return
		}
		result, err := verifyAPIKeyClient(c, control, c.Param("id"), req, c.GetHeader("Idempotency-Key"))
		if err != nil {
			writeOnboardingError(c, err)
			return
		}
		if err := control.RecordAPIKeyVerification(c.Request.Context(), actor(c), result); err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1569, "failed to record client verification")
			return
		}
		httpx.OK(c, result)
	})
}

func requireGlobalOnboardingAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		access := principalAccess(c)
		if !access.Global {
			httpx.Error(c, http.StatusForbidden, 1451, "onboarding requires global administration access")
			c.Abort()
			return
		}
		c.Next()
	}
}

func verifyAPIKeyClient(c *gin.Context, control *controlplane.Service, apiKeyID string, req clientVerificationRequest, idempotencyKey string) (controlplane.ClientVerificationResult, error) {
	if len(strings.TrimSpace(idempotencyKey)) < 8 || len(strings.TrimSpace(idempotencyKey)) > 128 {
		return controlplane.ClientVerificationResult{}, fmtOnboardingInput("Idempotency-Key must contain between 8 and 128 characters")
	}
	if err := control.OnboardingCredentialMatches(c.Request.Context(), apiKeyID, req.Credential); err != nil {
		return controlplane.ClientVerificationResult{}, err
	}
	client := strings.TrimSpace(req.Client)
	model := strings.TrimSpace(req.Model)
	protocol, body, err := clientVerificationEnvelope(client, model)
	if err != nil {
		return controlplane.ClientVerificationResult{}, fmtOnboardingInput(err.Error())
	}
	internalRequest := httptest.NewRequest(http.MethodPost, clientVerificationProtocolPath(protocol), bytes.NewReader(body))
	internalRequest = internalRequest.WithContext(c.Request.Context())
	internalRequest.RemoteAddr = "127.0.0.1:0"
	internalRequest.Header.Set("Content-Type", "application/json")
	if protocol == gatewaycore.ProtocolAnthropicMessages {
		internalRequest.Header.Set("X-API-Key", strings.TrimSpace(req.Credential))
		internalRequest.Header.Set("Anthropic-Version", "2023-06-01")
	} else {
		internalRequest.Header.Set("Authorization", "Bearer "+strings.TrimSpace(req.Credential))
	}
	internalRequest.Header.Set("Idempotency-Key", strings.TrimSpace(idempotencyKey))
	canonical, err := canonicalizeClientVerification(protocol, body, internalRequest.Header)
	if err != nil {
		return controlplane.ClientVerificationResult{}, err
	}
	canonical.SourceIP = "127.0.0.1"
	recorder := httptest.NewRecorder()
	internalContext, _ := gin.CreateTestContext(recorder)
	internalContext.Request = internalRequest
	handleGatewayProtocolRequest(internalContext, control, protocol, canonical)

	result := controlplane.ClientVerificationResult{
		Status: "failed", Client: client, APIKeyID: apiKeyID, Model: model,
		HTTPStatus: recorder.Code, OperationID: recorder.Header().Get("X-AsterRouter-Operation-ID"),
	}
	if recorder.Code >= 200 && recorder.Code < 300 {
		result.Status = "success"
	} else {
		result.ErrorCode, result.RecoveryAction = classifyClientVerificationFailure(recorder.Code, recorder.Body.Bytes())
	}
	if result.OperationID != "" {
		traces, traceErr := control.ListGatewayTracesQuery(c.Request.Context(), controlplane.GatewayTraceQuery{APIKeyID: apiKeyID, Limit: 50})
		if traceErr != nil {
			return controlplane.ClientVerificationResult{}, traceErr
		}
		for _, trace := range traces {
			if trace.OperationID == result.OperationID {
				result.TraceID = trace.ID
				break
			}
		}
	}
	return result, nil
}

func clientVerificationEnvelope(client, model string) (gatewaycore.Protocol, []byte, error) {
	if model == "" {
		return "", nil, errors.New("model is required")
	}
	var payload any
	var protocol gatewaycore.Protocol
	switch client {
	case controlplane.ClientCodex:
		protocol = gatewaycore.ProtocolOpenAIResponses
		payload = map[string]any{"model": model, "input": "Reply with OK.", "max_output_tokens": 8}
	case controlplane.ClientOpenAISDK:
		protocol = gatewaycore.ProtocolOpenAIChat
		payload = map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": "Reply with OK."}}, "max_tokens": 8}
	case controlplane.ClientClaudeCode, controlplane.ClientAnthropicSDK:
		protocol = gatewaycore.ProtocolAnthropicMessages
		payload = map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": "Reply with OK."}}, "max_tokens": 8}
	default:
		return "", nil, errors.New("client is not supported")
	}
	body, err := json.Marshal(payload)
	return protocol, body, err
}

func canonicalizeClientVerification(protocol gatewaycore.Protocol, body []byte, header http.Header) (gatewaycore.CanonicalRequest, error) {
	switch protocol {
	case gatewaycore.ProtocolOpenAIResponses:
		return gatewaycore.CanonicalizeOpenAIResponses(body, header)
	case gatewaycore.ProtocolOpenAIChat:
		return gatewaycore.CanonicalizeOpenAIChat(body, header)
	case gatewaycore.ProtocolAnthropicMessages:
		return gatewaycore.CanonicalizeAnthropicMessages(body, header)
	default:
		return gatewaycore.CanonicalRequest{}, gatewaycore.ErrInvalidCanonicalRequest
	}
}

func clientVerificationProtocolPath(protocol gatewaycore.Protocol) string {
	switch protocol {
	case gatewaycore.ProtocolOpenAIResponses:
		return "/v1/responses"
	case gatewaycore.ProtocolAnthropicMessages:
		return "/v1/messages"
	default:
		return "/v1/chat/completions"
	}
}

func classifyClientVerificationFailure(status int, body []byte) (string, string) {
	var payload struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)
	code := strings.TrimSpace(payload.Error.Type)
	if code == "" {
		switch status {
		case http.StatusUnauthorized:
			code = "credential_invalid"
		case http.StatusForbidden:
			code = "policy_denied"
		case http.StatusTooManyRequests:
			code = "capacity_or_quota_limited"
		case http.StatusBadGateway:
			code = "upstream_unavailable"
		case http.StatusServiceUnavailable:
			code = "route_unavailable"
		default:
			code = "verification_failed"
		}
	}
	recovery := "inspect_trace_and_retry"
	if status >= http.StatusInternalServerError {
		recovery = "check_route_and_provider_health"
	}
	switch status {
	case http.StatusUnauthorized:
		recovery = "use_the_onboarding_credential"
	case http.StatusForbidden:
		recovery = "check_api_key_model_and_policy_scope"
	case http.StatusTooManyRequests:
		recovery = "check_api_key_and_provider_capacity"
	case http.StatusBadRequest:
		recovery = "check_client_protocol_compatibility"
	}
	return code, recovery
}

func publicGatewayURL(c *gin.Context, settingsService *settings.Service) (string, error) {
	if settingsService == nil {
		return "", errors.New("settings service is unavailable")
	}
	public, err := settingsService.Public(c.Request.Context())
	if err != nil {
		return "", err
	}
	rawBase := strings.TrimSpace(public.PublicBaseURL)
	if rawBase == "" {
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		if forwarded := strings.ToLower(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))); forwarded == "http" || forwarded == "https" {
			scheme = forwarded
		}
		host := strings.TrimSpace(c.Request.Host)
		if host == "" {
			return "", errors.New("request host is unavailable")
		}
		rawBase = scheme + "://" + host
	}
	base, err := normalizedPublicHTTPBase(rawBase)
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(public.GatewayBasePath)
	if path == "" {
		path = "/v1"
	}
	parsedPath, err := url.ParseRequestURI(path)
	if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || parsedPath.Scheme != "" || parsedPath.Host != "" || parsedPath.RawQuery != "" || parsedPath.Fragment != "" || strings.Contains(path, "\\") {
		return "", errors.New("gateway base path is invalid")
	}
	return strings.TrimRight(base+"/"+strings.TrimLeft(path, "/"), "/"), nil
}

func normalizedPublicHTTPBase(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "#") {
		return "", errors.New("public base URL is invalid")
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Contains(parsed.Host, "\\") {
		return "", errors.New("public base URL is invalid")
	}
	if port := parsed.Port(); port != "" {
		value, portErr := strconv.Atoi(port)
		if portErr != nil || value < 1 || value > 65535 {
			return "", errors.New("public base URL port is invalid")
		}
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func verificationFromSession(session controlplane.OnboardingSession) controlplane.ClientVerificationResult {
	status := "failed"
	if session.Status == controlplane.OnboardingStatusCompleted {
		status = "success"
	}
	return controlplane.ClientVerificationResult{
		Status: status, Client: session.VerificationClient, APIKeyID: session.APIKeyID, Model: session.VerificationModel,
		HTTPStatus: session.VerificationHTTPStatus, OperationID: session.VerificationOperationID, TraceID: session.VerificationTraceID,
		ErrorCode: session.VerificationErrorCode, RecoveryAction: session.VerificationRecoveryAction,
	}
}

func writeOnboardingError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, controlplane.ErrOnboardingSessionNotFound), errors.Is(err, controlplane.ErrOnboardingAPIKeyNotFound):
		httpx.Error(c, http.StatusNotFound, 1570, "resource not found")
	case errors.Is(err, controlplane.ErrOnboardingSessionExpired):
		httpx.Error(c, http.StatusGone, 1571, err.Error())
	case errors.Is(err, controlplane.ErrOnboardingStepOrder), errors.Is(err, controlplane.ErrOnboardingSessionConflict):
		httpx.Error(c, http.StatusConflict, 1572, err.Error())
	case errors.Is(err, controlplane.ErrOnboardingCredential):
		httpx.Error(c, http.StatusForbidden, 1573, "credential does not match API key")
	case errors.Is(err, controlplane.ErrGatewayForbidden):
		httpx.Error(c, http.StatusForbidden, 1573, "API key does not allow this model")
	case errors.Is(err, controlplane.ErrOnboardingInvalidInput):
		httpx.Error(c, http.StatusBadRequest, 1574, err.Error())
	default:
		_ = c.Error(err)
		httpx.Error(c, http.StatusInternalServerError, 1575, "onboarding request failed")
	}
}

func fmtOnboardingInput(message string) error {
	return fmt.Errorf("%w: %s", controlplane.ErrOnboardingInvalidInput, message)
}
