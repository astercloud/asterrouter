package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/gin-gonic/gin"
)

const (
	gatewayRequestBodyLimit  = 16 << 20
	gatewayUpstreamBodyLimit = 16 << 20
	// failureBodyPreviewLimit bounds how much of a failed candidate's
	// response body is read for temp-unschedulable keyword matching. The
	// body is discarded either way (that candidate's response is never
	// returned to the caller), so this only needs to be large enough to
	// contain a typical error message.
	failureBodyPreviewLimit = 4 << 10
)

var (
	errGatewayRequestTooLarge   = errors.New("gateway request body is too large")
	errUpstreamResponseTooLarge = errors.New("upstream response body is too large")
	errNoSchedulableSlot        = errors.New("no schedulable provider account slot is available")
)

func registerGatewayRoutes(r *gin.Engine, control *controlplane.Service) {
	r.GET("/v1/models", func(c *gin.Context) {
		if control == nil {
			openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
			return
		}
		models, err := control.GatewayModelsForKey(c.Request.Context(), bearerToken(c))
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		data := make([]gin.H, 0, len(models))
		for _, model := range models {
			data = append(data, gin.H{"id": model, "object": "model", "owned_by": "asterrouter"})
		}
		c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
	})

	r.POST("/v1/chat/completions", func(c *gin.Context) {
		if control == nil {
			openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
			return
		}
		rawBody, req, err := parseChatCompletionRequest(c)
		if err != nil {
			if errors.Is(err, errGatewayRequestTooLarge) {
				openAIError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body exceeds 16 MiB limit")
				return
			}
			openAIError(c, http.StatusBadRequest, "invalid_request_error", "invalid chat completion payload")
			return
		}
		if strings.TrimSpace(req.Model) == "" {
			openAIError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
			return
		}
		auth, err := control.AuthorizeGatewayModel(c.Request.Context(), bearerToken(c), req.Model)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		startedAt := time.Now()
		if err := control.EnforceGatewayPolicy(c.Request.Context(), auth); err != nil {
			errorType := gatewayPolicyErrorType(err)
			_ = control.RecordGatewayCall(c.Request.Context(), auth, req.Model, "policy_rejected", err.Error())
			recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
				Model:     req.Model,
				Status:    "error",
				ErrorType: errorType,
				LatencyMS: time.Since(startedAt).Milliseconds(),
			})
			recordGatewayTrace(control, c, auth, gatewayTraceInput(req, controlplane.GatewayProvider{}, "error", http.StatusTooManyRequests, errorType, time.Since(startedAt).Milliseconds(), 0, 0, err.Error(), ""))
			writeGatewayError(c, err)
			return
		}
		candidates, hasAccountPool, err := control.GatewayProviderCandidatesForModel(c.Request.Context(), req.Model)
		if err != nil {
			recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
				Model:     req.Model,
				Status:    "error",
				ErrorType: "provider_selection_error",
				LatencyMS: time.Since(startedAt).Milliseconds(),
			})
			recordGatewayTrace(control, c, auth, gatewayTraceInput(req, controlplane.GatewayProvider{}, "error", 0, "provider_selection_error", time.Since(startedAt).Milliseconds(), 0, 0, err.Error(), ""))
			writeGatewayError(c, err)
			return
		}
		if len(candidates) == 0 && hasAccountPool {
			routeErr := controlplane.ErrGatewayRouteUnavailable
			_ = control.RecordGatewayCall(c.Request.Context(), auth, req.Model, "policy_rejected", routeErr.Error())
			recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
				Model:     req.Model,
				Status:    "error",
				ErrorType: "route_unavailable",
				LatencyMS: time.Since(startedAt).Milliseconds(),
			})
			recordGatewayTrace(control, c, auth, gatewayTraceInput(req, controlplane.GatewayProvider{}, "error", http.StatusServiceUnavailable, "route_unavailable", time.Since(startedAt).Milliseconds(), 0, 0, routeErr.Error(), ""))
			writeGatewayError(c, routeErr)
			return
		}
		if len(candidates) > 0 {
			resp, provider, release, attempts, attemptErr := attemptGatewayCandidates(c, control, candidates, rawBody, req.Stream)
			routeAttempts := marshalRouteAttempts(attempts)
			if resp == nil {
				if attemptErr == nil {
					attemptErr = errNoSchedulableSlot
				}
				_ = control.RecordGatewayCall(c.Request.Context(), auth, req.Model, "upstream_error", attemptErr.Error())
				recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
					Model:             req.Model,
					ProviderID:        provider.ID,
					ProviderAccountID: provider.AccountID,
					Status:            "upstream_error",
					ErrorType:         "transport_error",
					LatencyMS:         time.Since(startedAt).Milliseconds(),
				})
				recordGatewayTrace(control, c, auth, gatewayTraceInput(req, provider, "upstream_error", 0, "transport_error", time.Since(startedAt).Milliseconds(), 0, 0, attemptErr.Error(), routeAttempts))
				openAIError(c, http.StatusBadGateway, "upstream_error", attemptErr.Error())
				return
			}
			defer resp.Body.Close()
			defer release()

			status := "forwarded"
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				status = "upstream_error"
			}
			summary := gatewayRouteSummary(req.Model, provider)
			if req.Stream {
				if err := control.RecordGatewayCall(c.Request.Context(), auth, req.Model, status, summary); err != nil {
					openAIError(c, http.StatusInternalServerError, "server_error", err.Error())
					return
				}
				streamErr := streamUpstreamResponse(c, resp)
				errorType := ""
				usageStatus := status
				if streamErr != nil {
					errorType = "stream_error"
					usageStatus = "upstream_error"
				}
				responseSummary := "stream completed"
				if streamErr != nil {
					responseSummary = streamErr.Error()
				}
				recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
					Model:             req.Model,
					ProviderID:        provider.ID,
					ProviderAccountID: provider.AccountID,
					Status:            usageStatus,
					ErrorType:         errorType,
					LatencyMS:         time.Since(startedAt).Milliseconds(),
				})
				recordGatewayTrace(control, c, auth, gatewayTraceInput(req, provider, usageStatus, resp.StatusCode, errorType, time.Since(startedAt).Milliseconds(), 0, 0, responseSummary, routeAttempts))
				if streamErr != nil && !c.Writer.Written() {
					openAIError(c, http.StatusBadGateway, "upstream_error", streamErr.Error())
				}
				return
			}

			contentType, upstreamBody, err := readUpstreamResponse(resp)
			if err != nil {
				_ = control.RecordGatewayCall(c.Request.Context(), auth, req.Model, "upstream_error", err.Error())
				recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
					Model:             req.Model,
					ProviderID:        provider.ID,
					ProviderAccountID: provider.AccountID,
					Status:            "upstream_error",
					ErrorType:         "response_read_error",
					LatencyMS:         time.Since(startedAt).Milliseconds(),
				})
				recordGatewayTrace(control, c, auth, gatewayTraceInput(req, provider, "upstream_error", resp.StatusCode, "response_read_error", time.Since(startedAt).Milliseconds(), 0, 0, err.Error(), routeAttempts))
				openAIError(c, http.StatusBadGateway, "upstream_error", err.Error())
				return
			}
			if err := control.RecordGatewayCall(c.Request.Context(), auth, req.Model, status, summary); err != nil {
				openAIError(c, http.StatusInternalServerError, "server_error", err.Error())
				return
			}
			inputTokens, outputTokens := parseUpstreamUsage(upstreamBody)
			errorType := ""
			if status == "upstream_error" {
				errorType = "upstream_status"
			}
			recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
				Model:             req.Model,
				ProviderID:        provider.ID,
				ProviderAccountID: provider.AccountID,
				Status:            status,
				ErrorType:         errorType,
				LatencyMS:         time.Since(startedAt).Milliseconds(),
				InputTokens:       inputTokens,
				OutputTokens:      outputTokens,
			})
			recordGatewayTrace(control, c, auth, gatewayTraceInput(req, provider, status, resp.StatusCode, errorType, time.Since(startedAt).Milliseconds(), inputTokens, outputTokens, upstreamResponseSummary(resp.StatusCode, upstreamBody), routeAttempts))
			c.Data(resp.StatusCode, contentType, upstreamBody)
			return
		}
		if req.Stream {
			_ = control.RecordGatewayCall(c.Request.Context(), auth, req.Model, "unsupported_stream", fmt.Sprintf("Rejected streaming request for model %s without configured provider", req.Model))
			recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
				Model:     req.Model,
				Status:    "error",
				ErrorType: "unsupported_stream",
				LatencyMS: time.Since(startedAt).Milliseconds(),
			})
			recordGatewayTrace(control, c, auth, gatewayTraceInput(req, controlplane.GatewayProvider{}, "error", http.StatusNotImplemented, "unsupported_stream", time.Since(startedAt).Milliseconds(), 0, 0, "streaming request rejected without configured provider", ""))
			openAIError(c, http.StatusNotImplemented, "unsupported_feature", "streaming responses require a configured provider")
			return
		}
		summary := fmt.Sprintf("Accepted chat completion request for model %s", req.Model)
		if err := control.RecordGatewayCall(c.Request.Context(), auth, req.Model, "accepted", summary); err != nil {
			openAIError(c, http.StatusInternalServerError, "server_error", err.Error())
			return
		}
		recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
			Model:     req.Model,
			Status:    "accepted",
			LatencyMS: time.Since(startedAt).Milliseconds(),
		})
		recordGatewayTrace(control, c, auth, gatewayTraceInput(req, controlplane.GatewayProvider{}, "accepted", http.StatusOK, "", time.Since(startedAt).Milliseconds(), 0, 0, "local fallback response", ""))
		now := time.Now().Unix()
		c.JSON(http.StatusOK, gin.H{
			"id":      "chatcmpl_" + time.Now().UTC().Format("20060102150405"),
			"object":  "chat.completion",
			"created": now,
			"model":   req.Model,
			"choices": []gin.H{
				{
					"index": 0,
					"message": gin.H{
						"role":    "assistant",
						"content": "AsterRouter local fallback accepted this gateway request. Configure provider forwarding to call an upstream model.",
					},
					"finish_reason": "stop",
				},
			},
			"usage": gin.H{
				"prompt_tokens":     0,
				"completion_tokens": 0,
				"total_tokens":      0,
			},
		})
	})
}

type chatCompletionRequest struct {
	Model    string           `json:"model"`
	Messages []map[string]any `json:"messages"`
	Stream   bool             `json:"stream"`
}

func parseChatCompletionRequest(c *gin.Context) ([]byte, chatCompletionRequest, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, gatewayRequestBodyLimit)
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return nil, chatCompletionRequest{}, errGatewayRequestTooLarge
		}
		return nil, chatCompletionRequest{}, err
	}
	var req chatCompletionRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		return nil, chatCompletionRequest{}, err
	}
	return rawBody, req, nil
}

// gatewayRouteAttempt records what happened when the gateway tried a single
// candidate route while resolving a chat completion request. It is
// serialized into GatewayTrace.RouteAttempts so operators can see which
// candidates were skipped or failed, and why, without needing verbose logs.
type gatewayRouteAttempt struct {
	AccountID  string `json:"account_id,omitempty"`
	ProviderID string `json:"provider_id,omitempty"`
	Outcome    string `json:"outcome"`
	Detail     string `json:"detail,omitempty"`
}

func marshalRouteAttempts(attempts []gatewayRouteAttempt) string {
	if len(attempts) == 0 {
		return "[]"
	}
	data, err := json.Marshal(attempts)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// isProviderAccountFailureStatus reports whether an upstream HTTP status
// indicates an account-side problem (auth revoked, rate limited, or upstream
// server error) rather than a problem with the request itself. Candidates
// that are not the last one tried are retried against the next candidate
// when they return one of these statuses.
func isProviderAccountFailureStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		return true
	default:
		return statusCode >= 500
	}
}

// attemptGatewayCandidates tries each candidate route in order until one
// produces a response the caller should use. A candidate is skipped without
// being attempted if its concurrency slot is exhausted. A candidate that
// fails at the transport level, or that is not the last candidate and
// returns an account-side failure status, is recorded as a failure (cooling
// the underlying provider account down) and the loop moves to the next
// candidate. The last candidate's response is always accepted as-is, even on
// a failure status, matching the existing behavior of passing upstream error
// responses through to the caller when no better alternative exists.
//
// On success, the returned release func must be called by the caller once
// the response body has been fully consumed (streamed or read). Losing
// candidates' slots are released internally and must not be released again.
func attemptGatewayCandidates(c *gin.Context, control *controlplane.Service, candidates []controlplane.GatewayProvider, rawBody []byte, stream bool) (resp *http.Response, provider controlplane.GatewayProvider, release func(), attempts []gatewayRouteAttempt, transportErr error) {
	for i, candidate := range candidates {
		slotRelease, acquired := control.TryAcquireProviderAccountSlot(candidate.AccountID, candidate.Concurrency)
		if !acquired {
			attempts = append(attempts, gatewayRouteAttempt{AccountID: candidate.AccountID, ProviderID: candidate.ID, Outcome: "skipped", Detail: "at_capacity"})
			continue
		}
		candidateResp, err := forwardChatCompletion(c, candidate, rawBody, stream)
		if err != nil {
			slotRelease()
			if candidate.AccountID != "" {
				_ = control.RecordProviderAccountFailure(c.Request.Context(), candidate.AccountID, 0, err.Error())
			}
			attempts = append(attempts, gatewayRouteAttempt{AccountID: candidate.AccountID, ProviderID: candidate.ID, Outcome: "failed", Detail: err.Error()})
			transportErr = err
			continue
		}
		isLast := i == len(candidates)-1
		if !isLast && isProviderAccountFailureStatus(candidateResp.StatusCode) {
			bodyPreview, _ := io.ReadAll(io.LimitReader(candidateResp.Body, failureBodyPreviewLimit))
			_ = candidateResp.Body.Close()
			slotRelease()
			if candidate.AccountID != "" {
				_ = control.RecordProviderAccountFailure(c.Request.Context(), candidate.AccountID, candidateResp.StatusCode, string(bodyPreview))
			}
			detail := fmt.Sprintf("upstream http status %d", candidateResp.StatusCode)
			attempts = append(attempts, gatewayRouteAttempt{AccountID: candidate.AccountID, ProviderID: candidate.ID, Outcome: "failed", Detail: detail})
			transportErr = errors.New(detail)
			continue
		}
		if candidate.AccountID != "" {
			_ = control.TouchProviderAccountUsage(c.Request.Context(), candidate.AccountID)
		}
		attempts = append(attempts, gatewayRouteAttempt{AccountID: candidate.AccountID, ProviderID: candidate.ID, Outcome: "selected"})
		return candidateResp, candidate, slotRelease, attempts, nil
	}
	return nil, controlplane.GatewayProvider{}, nil, attempts, transportErr
}

func forwardChatCompletion(c *gin.Context, provider controlplane.GatewayProvider, rawBody []byte, stream bool) (*http.Response, error) {
	endpoint := strings.TrimRight(provider.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, endpoint, bytes.NewReader(rawBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	return gatewayHTTPClient(stream).Do(req)
}

func gatewayRouteSummary(model string, provider controlplane.GatewayProvider) string {
	summary := fmt.Sprintf("Forwarded chat completion request for model %s to provider %s", model, provider.ID)
	if provider.AccountID != "" {
		summary += fmt.Sprintf(" account %s", provider.AccountID)
	}
	if provider.SelectionReason != "" {
		summary += "; " + provider.SelectionReason
	}
	return summary
}

func gatewayTraceInput(req chatCompletionRequest, provider controlplane.GatewayProvider, status string, httpStatus int, errorType string, latencyMS int64, inputTokens int, outputTokens int, responseSummary string, routeAttempts string) controlplane.GatewayTraceInput {
	return controlplane.GatewayTraceInput{
		Model:             req.Model,
		Stream:            req.Stream,
		MessageCount:      len(req.Messages),
		ProviderID:        provider.ID,
		ProviderAccountID: provider.AccountID,
		RouteSource:       provider.Source,
		RouteReason:       provider.SelectionReason,
		Status:            status,
		HTTPStatus:        httpStatus,
		ErrorType:         errorType,
		LatencyMS:         latencyMS,
		InputTokens:       inputTokens,
		OutputTokens:      outputTokens,
		RequestSummary:    fmt.Sprintf("chat.completions stream=%t messages=%d", req.Stream, len(req.Messages)),
		ResponseSummary:   responseSummary,
		RouteAttempts:     routeAttempts,
	}
}

func gatewayPolicyErrorType(err error) string {
	switch {
	case errors.Is(err, controlplane.ErrGatewayRateLimited):
		return "rate_limit_exceeded"
	case errors.Is(err, controlplane.ErrGatewayQuotaExceeded):
		return "quota_exceeded"
	case errors.Is(err, controlplane.ErrGatewayBudgetExceeded):
		return "budget_exceeded"
	default:
		return "policy_error"
	}
}

func upstreamResponseSummary(statusCode int, body []byte) string {
	var payload struct {
		ID     string `json:"id"`
		Object string `json:"object"`
		Error  struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)
	parts := []string{fmt.Sprintf("http=%d", statusCode), fmt.Sprintf("bytes=%d", len(body))}
	if payload.ID != "" {
		parts = append(parts, "id="+payload.ID)
	}
	if payload.Object != "" {
		parts = append(parts, "object="+payload.Object)
	}
	if payload.Error.Type != "" {
		parts = append(parts, "error_type="+payload.Error.Type)
	}
	return strings.Join(parts, " ")
}

func gatewayHTTPClient(stream bool) *http.Client {
	if stream {
		return &http.Client{}
	}
	return &http.Client{Timeout: 120 * time.Second}
}

func readUpstreamResponse(resp *http.Response) (string, []byte, error) {
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, gatewayUpstreamBodyLimit+1))
	if err != nil {
		return "", nil, err
	}
	if len(body) > gatewayUpstreamBodyLimit {
		return "", nil, errUpstreamResponseTooLarge
	}
	return contentType, body, nil
}

func parseUpstreamUsage(body []byte) (int, int) {
	var payload struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, 0
	}
	input := payload.Usage.PromptTokens
	if input == 0 {
		input = payload.Usage.InputTokens
	}
	output := payload.Usage.CompletionTokens
	if output == 0 {
		output = payload.Usage.OutputTokens
	}
	return input, output
}

func recordGatewayUsage(control *controlplane.Service, c *gin.Context, auth controlplane.GatewayAuthContext, input controlplane.GatewayUsageInput) {
	if control != nil {
		_ = control.RecordGatewayUsage(c.Request.Context(), auth, input)
	}
}

func recordGatewayTrace(control *controlplane.Service, c *gin.Context, auth controlplane.GatewayAuthContext, input controlplane.GatewayTraceInput) {
	if control != nil {
		_ = control.RecordGatewayTrace(c.Request.Context(), auth, input)
	}
}

func streamUpstreamResponse(c *gin.Context, resp *http.Response) error {
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/event-stream"
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(resp.StatusCode)

	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := c.Writer.Write(buf[:n]); err != nil {
				return err
			}
			c.Writer.Flush()
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
