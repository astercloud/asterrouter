package server

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/gin-gonic/gin"
)

// registerGatewayMediaJobRoutes exposes protocol-friendly video/audio entry
// points while reusing the durable Job admission, queue, artifact and billing
// pipeline. The endpoint is intentionally asynchronous: providers may expose
// streaming progress, but the public contract remains the resumable Job event
// stream instead of holding an HTTP request open through provider polling.
func registerGatewayMediaJobRoutes(r *gin.Engine, control *controlplane.Service, durableJobs DurableAIJobAdmission) {
	for _, route := range []struct {
		path      string
		modality  string
		operation string
	}{
		{path: "/v1/videos/generations", modality: controlplane.GatewayModalityVideo, operation: controlplane.GatewayOperationVideoGeneration},
		{path: "/v1/audio/generations", modality: controlplane.GatewayModalityAudio, operation: controlplane.GatewayOperationAudioGeneration},
	} {
		route := route
		r.POST(route.path, func(c *gin.Context) {
			if control == nil {
				openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
				return
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, gatewayRequestBodyLimit)
			raw, err := io.ReadAll(c.Request.Body)
			if err != nil {
				var maxBytesErr *http.MaxBytesError
				if errors.As(err, &maxBytesErr) {
					openAIError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body exceeds 16 MiB limit")
					return
				}
				openAIError(c, http.StatusBadRequest, "invalid_request_error", "invalid media generation payload")
				return
			}
			request, err := gatewaycore.CanonicalizeOpenAIMediaJob(raw, c.Request.Header, route.modality, route.operation)
			if err != nil {
				writeGatewayError(c, err)
				return
			}
			request.SourceIP = gatewaySourceIP(c.Request)
			credential, err := gatewaycore.ExtractCredential(c.Request, gatewaycore.ProtocolAsterJobs)
			if err != nil {
				writeGatewayError(c, controlplane.ErrGatewayUnauthorized)
				return
			}
			legacyAuth, auth, err := control.AuthorizeCanonicalGatewayRequest(c.Request.Context(), credential, request)
			if err != nil {
				writeGatewayError(c, err)
				return
			}
			if err := control.EnforceGatewayPolicy(c.Request.Context(), legacyAuth); err != nil {
				writeGatewayError(c, err)
				return
			}
			if durableJobs == nil {
				openAIError(c, http.StatusServiceUnavailable, "unsupported_capability", "no executable provider adapter is available for this media job")
				return
			}
			supported, err := durableJobs.SupportsDurableAIJob(c.Request.Context(), auth, request)
			if err != nil {
				openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "media job runtime capability check failed")
				return
			}
			if !supported {
				openAIError(c, http.StatusServiceUnavailable, "unsupported_capability", "no executable provider adapter is available for this media job")
				return
			}
			job, created, err := control.BeginDurableAIJob(c.Request.Context(), auth, request)
			if err != nil {
				writeGatewayError(c, err)
				return
			}
			c.Header("Location", "/v1/jobs/"+job.ID)
			c.Header("X-AsterRouter-Operation-ID", job.OperationID)
			status := http.StatusAccepted
			if !created {
				status = http.StatusOK
				c.Header("Idempotent-Replayed", "true")
			}
			if !aiJobPublicTerminal(job.Status) {
				c.Header("Retry-After", strconv.Itoa(controlplane.AIJobDefaultPollAfter))
			}
			c.JSON(status, newPublicAIJobResponse(job))
		})
	}
}
