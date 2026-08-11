package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/gin-gonic/gin"
)

func handleGatewayCountTokensRequest(c *gin.Context, control *controlplane.Service, request gatewaycore.CanonicalRequest) {
	startedAt := time.Now()
	if control == nil {
		writeGatewayProtocolError(c, request.Protocol, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
		return
	}
	c.Header("X-Request-ID", request.ClientRequestID)
	c.Set(gatewayFingerprintContextKey, request.Fingerprint)
	credential, err := gatewaycore.ExtractCredential(c.Request, request.Protocol)
	if err != nil {
		writeGatewayProtocolControlError(c, request.Protocol, controlplane.ErrGatewayUnauthorized)
		return
	}
	auth, canonicalAuth, err := control.AuthorizeCanonicalGatewayRequest(c.Request.Context(), credential, request)
	if err != nil {
		writeGatewayProtocolControlError(c, request.Protocol, err)
		return
	}
	if err := control.EnforceGatewayPolicy(c.Request.Context(), auth); err != nil {
		recordGatewayCountTokensResult(c, control, auth, request, controlplane.GatewayProvider{}, "error", http.StatusTooManyRequests, gatewayPolicyErrorType(err), err.Error(), "", startedAt)
		writeGatewayProtocolControlError(c, request.Protocol, err)
		return
	}
	plan, err := control.PlanCanonicalGatewayRequest(c.Request.Context(), canonicalAuth, request)
	if err != nil {
		recordGatewayCountTokensResult(c, control, auth, request, controlplane.GatewayProvider{}, "error", 0, "provider_selection_error", err.Error(), "", startedAt)
		writeGatewayProtocolControlError(c, request.Protocol, err)
		return
	}
	if len(plan.Candidates) == 0 {
		err = controlplane.ErrGatewayRouteUnavailable
		recordGatewayCountTokensResult(c, control, auth, request, controlplane.GatewayProvider{}, "error", http.StatusServiceUnavailable, "route_unavailable", err.Error(), marshalRouteEvidence(plan.Exclusions, nil), startedAt)
		writeGatewayProtocolControlError(c, request.Protocol, err)
		return
	}
	if _, err := exactCountTokensCompatibilityGroup(plan.Candidates); err != nil {
		recordGatewayCountTokensResult(c, control, auth, request, controlplane.GatewayProvider{}, "error", http.StatusBadRequest, "unsupported_feature", err.Error(), marshalRouteEvidence(plan.Exclusions, nil), startedAt)
		writeGatewayProtocolError(c, request.Protocol, http.StatusBadRequest, "unsupported_feature", err.Error())
		return
	}
	affinity := controlplane.GatewayAffinityInput{
		ApplicationID: canonicalAuth.ApplicationID, PrincipalID: canonicalAuth.PrincipalID, CredentialID: canonicalAuth.CredentialID,
		Model: request.Model, Protocol: string(request.Protocol), RouteGroup: plan.RouteGroup, StickyKey: request.StickyKey, PolicyVersion: canonicalAuth.PolicyVersion,
	}
	cohortKey := control.GatewayEffectivePricingCohortKey(affinity)
	candidates := control.PreferGatewayCandidatesWithAffinity(c.Request.Context(), affinity, control.OrderGatewayCandidatesByEffectivePricing(c.Request.Context(), request.Model, string(request.Protocol), cohortKey, plan.Candidates))
	response, provider, release, attempts, err := attemptGatewayCountTokensCandidates(c, control, candidates, request)
	routeAttempts := marshalRouteEvidence(plan.Exclusions, attempts)
	if response == nil {
		if err == nil {
			err = errNoSchedulableSlot
		}
		recordGatewayCountTokensResult(c, control, auth, request, provider, "upstream_error", http.StatusBadGateway, "transport_error", err.Error(), routeAttempts, startedAt)
		writeGatewayProtocolError(c, request.Protocol, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	defer response.Body.Close()
	defer release()
	_, body, _, readErr := readUpstreamResponseLimit(response, startedAt, gatewayUpstreamBodyLimit)
	if readErr != nil {
		recordGatewayCountTokensResult(c, control, auth, request, provider, "upstream_error", http.StatusBadGateway, "response_read_error", readErr.Error(), routeAttempts, startedAt)
		writeGatewayProtocolError(c, request.Protocol, http.StatusBadGateway, "upstream_error", readErr.Error())
		return
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message := gatewayUpstreamErrorMessage(response.StatusCode, body)
		recordGatewayCountTokensResult(c, control, auth, request, provider, "upstream_error", response.StatusCode, "upstream_status", message, routeAttempts, startedAt)
		writeGatewayProtocolError(c, request.Protocol, response.StatusCode, "upstream_error", message)
		return
	}
	var payload struct {
		InputTokens *int `json:"input_tokens"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.InputTokens == nil || *payload.InputTokens < 0 {
		err = errors.New("upstream count tokens response is invalid")
		recordGatewayCountTokensResult(c, control, auth, request, provider, "upstream_error", http.StatusBadGateway, "invalid_upstream_response", err.Error(), routeAttempts, startedAt)
		writeGatewayProtocolError(c, request.Protocol, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	summary := fmt.Sprintf("input_tokens=%d upstream_request_id=%s", *payload.InputTokens, upstreamRequestID(response))
	recordGatewayCountTokensResult(c, control, auth, request, provider, "forwarded", response.StatusCode, "", summary, routeAttempts, startedAt)
	c.JSON(http.StatusOK, gin.H{"input_tokens": *payload.InputTokens})
}

func exactCountTokensCompatibilityGroup(candidates []controlplane.GatewayProvider) (string, error) {
	group := ""
	for _, candidate := range candidates {
		if candidate.Type != controlplane.ProviderTypeAnthropicCompatible || candidate.UpstreamFormat != controlplane.UpstreamFormatAnthropic || strings.TrimSpace(candidate.UpstreamModel) == "" {
			return "", errors.New("exact token counting is not supported by every candidate route")
		}
		candidateGroup := strings.ToLower(strings.TrimSpace(candidate.UpstreamModel))
		if group == "" {
			group = candidateGroup
			continue
		}
		if candidateGroup != group {
			return "", errors.New("exact token counting requires one tokenizer compatibility group")
		}
	}
	if group == "" {
		return "", errors.New("exact token counting has no compatible candidate route")
	}
	return group, nil
}

func attemptGatewayCountTokensCandidates(c *gin.Context, control *controlplane.Service, candidates []controlplane.GatewayProvider, request gatewaycore.CanonicalRequest) (*http.Response, controlplane.GatewayProvider, func(), []gatewayRouteAttempt, error) {
	var attempts []gatewayRouteAttempt
	var provider controlplane.GatewayProvider
	var transportErr error
	for index, candidate := range candidates {
		provider = candidate
		permit, reason, acquired, err := control.TryAcquireProviderAccountPermitContext(c.Request.Context(), candidate, 0, "provider_count_"+request.ClientRequestID+"_"+strconv.Itoa(index+1))
		if err != nil {
			return nil, candidate, nil, attempts, err
		}
		if !acquired {
			attempts = append(attempts, gatewayRouteAttempt{AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "skipped", Detail: reason})
			continue
		}
		body, err := gatewaycore.EncodeAnthropicCountTokensRequest(*request.Text, candidate.UpstreamModel)
		if err == nil {
			var upstreamRequest *http.Request
			upstreamRequest, err = gatewayProviderAdapters.BuildCountTokensRequest(c.Request.Context(), candidate, body)
			if err == nil {
				var response *http.Response
				response, err = gatewayHTTPClient(false).Do(upstreamRequest)
				if err == nil {
					isLast := index == len(candidates)-1
					if !isLast && isProviderAccountFailureStatus(response.StatusCode) {
						preview, _ := io.ReadAll(io.LimitReader(response.Body, failureBodyPreviewLimit))
						_ = response.Body.Close()
						permit.Release()
						_ = control.RecordProviderAccountFailure(c.Request.Context(), candidate.AccountID, response.StatusCode, string(preview))
						detail := fmt.Sprintf("upstream http status %d", response.StatusCode)
						attempts = append(attempts, gatewayRouteAttempt{AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "failed", Detail: detail})
						transportErr = errors.Join(transportErr, errors.New(detail))
						continue
					}
					if isProviderAccountFailureStatus(response.StatusCode) {
						_ = control.RecordProviderAccountFailure(c.Request.Context(), candidate.AccountID, response.StatusCode, "")
					} else {
						_ = control.RecordProviderAccountSuccess(c.Request.Context(), candidate.AccountID)
					}
					_ = control.TouchProviderAccountUsage(c.Request.Context(), candidate.AccountID)
					attempts = append(attempts, gatewayRouteAttempt{AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "selected"})
					return response, candidate, permit.Release, attempts, nil
				}
			}
		}
		permit.Release()
		_ = control.RecordProviderAccountFailure(c.Request.Context(), candidate.AccountID, 0, err.Error())
		attempts = append(attempts, gatewayRouteAttempt{AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "failed", Detail: err.Error()})
		transportErr = errors.Join(transportErr, err)
	}
	return nil, provider, nil, attempts, transportErr
}

func recordGatewayCountTokensResult(c *gin.Context, control *controlplane.Service, auth controlplane.GatewayAuthContext, request gatewaycore.CanonicalRequest, provider controlplane.GatewayProvider, status string, httpStatus int, errorType, summary, routeAttempts string, startedAt time.Time) {
	_ = control.RecordGatewayCall(c.Request.Context(), auth, request.Model, status, "Counted input tokens for model "+request.Model)
	trace := gatewayTraceInput(request, provider, status, httpStatus, errorType, time.Since(startedAt).Milliseconds(), 0, 0, summary, routeAttempts)
	recordGatewayTrace(control, c, auth, trace)
}
