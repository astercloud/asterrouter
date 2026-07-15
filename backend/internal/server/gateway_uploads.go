package server

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/gin-gonic/gin"
)

const publicUploadMaxBytes = controlplane.ArtifactDefaultMaxBytes

type publicUploadResponse struct {
	ID          string            `json:"id"`
	Object      string            `json:"object"`
	OperationID string            `json:"operation_id"`
	ArtifactID  string            `json:"artifact_id"`
	Status      string            `json:"status"`
	Offset      int64             `json:"offset"`
	SizeBytes   int64             `json:"size_bytes"`
	MediaType   string            `json:"media_type,omitempty"`
	ExpiresAt   time.Time         `json:"expires_at"`
	Links       map[string]string `json:"links"`
}

func registerGatewayUploadRoutes(r *gin.Engine, control *controlplane.Service) {
	r.POST("/v1/uploads", func(c *gin.Context) {
		if control == nil {
			openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
			return
		}
		if offset := strings.TrimSpace(c.GetHeader("Upload-Offset")); offset != "" && offset != "0" {
			openAIError(c, http.StatusNotImplemented, "upload_resumable_not_supported", "non-zero resumable upload offsets are not supported by this deployment")
			return
		}
		credential, err := gatewaycore.ExtractCredential(c.Request, gatewaycore.ProtocolAsterJobs)
		if err != nil {
			writeGatewayError(c, controlplane.ErrGatewayUnauthorized)
			return
		}
		auth, err := control.AuthorizeGatewayCredentialScope(c.Request.Context(), credential, gatewaySourceIP(c.Request), controlplane.GatewayScopeArtifactsWrite)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		if auth.ArtifactPolicy == controlplane.GatewayArtifactPolicyProxyOnly || auth.ArtifactPolicy == controlplane.GatewayArtifactPolicyMetadataOnly {
			openAIError(c, http.StatusConflict, "upload_storage_required", "the credential artifact policy does not permit retained input content")
			return
		}
		storeDriver, configured := control.PrimaryArtifactStoreDriver()
		if !configured {
			openAIError(c, http.StatusServiceUnavailable, "artifact_store_unavailable", "no artifact content store is configured")
			return
		}
		idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
		if idempotencyKey == "" {
			openAIError(c, http.StatusBadRequest, "invalid_request_error", "Idempotency-Key is required for uploads")
			return
		}
		contentLength := c.Request.ContentLength
		if contentLength == 0 || contentLength > publicUploadMaxBytes {
			openAIError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "upload size is outside the allowed range")
			return
		}
		mediaType := strings.TrimSpace(c.GetHeader("Content-Type"))
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		checksum := strings.ToLower(strings.TrimSpace(c.GetHeader("X-Checksum-SHA256")))
		if len(checksum) != 64 {
			openAIError(c, http.StatusBadRequest, "invalid_request_error", "X-Checksum-SHA256 is required for idempotent uploads")
			return
		}
		if _, err := hex.DecodeString(checksum); err != nil {
			openAIError(c, http.StatusBadRequest, "invalid_request_error", "X-Checksum-SHA256 must be hexadecimal")
			return
		}
		requestPayload, err := json.Marshal(map[string]interface{}{
			"content_length": contentLength, "media_type": mediaType, "sha256": checksum,
		})
		if err != nil {
			openAIError(c, http.StatusBadRequest, "invalid_request_error", "invalid upload metadata")
			return
		}
		requestID := strings.TrimSpace(c.GetHeader("X-Request-Id"))
		if requestID == "" {
			requestID = strings.TrimSpace(c.GetHeader("X-Client-Request-Id"))
		}
		canonicalRaw := append([]byte(`{"model":"artifact-upload","operation":"artifact_upload","modality":"input","input":`), requestPayload...)
		canonicalRaw = append(canonicalRaw, '}')
		request, err := gatewaycore.CanonicalizeDurableJob(canonicalRaw, http.Header{
			"X-Request-Id":    []string{requestID},
			"Idempotency-Key": []string{idempotencyKey},
		})
		if err != nil {
			openAIError(c, http.StatusBadRequest, "invalid_request_error", "invalid upload metadata")
			return
		}
		request.Protocol = gatewaycore.ProtocolAsterJobs
		request.Operation = "artifact_upload"
		request.Modality = "input"
		request.Lane = gatewaycore.LaneDirect
		request.Model = "artifact-upload"
		request.Payload = requestPayload
		request.SourceIP = gatewaySourceIP(c.Request)
		operation, created, err := control.BeginCanonicalOperation(c.Request.Context(), auth, request)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		if !created {
			artifacts, queryErr := control.DirectArtifactsForAuth(c.Request.Context(), auth, operation.ID)
			if queryErr != nil || len(artifacts) == 0 {
				openAIError(c, http.StatusConflict, "upload_replay_unavailable", "the original upload result is not available")
				return
			}
			c.Header("Idempotent-Replayed", "true")
			c.JSON(http.StatusOK, newPublicUploadResponse(artifacts[0]))
			return
		}
		if err := control.MarkAIOperationRunning(c.Request.Context(), operation.ID); err != nil {
			_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "upload_operation_start_failed")
			_ = control.CompleteAIOperation(c.Request.Context(), operation.ID, controlplane.AIOperationStatusFailed, "upload_operation_start_failed")
			writeGatewayError(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, publicUploadMaxBytes)
		artifact, err := control.CreateArtifactFromReader(c.Request.Context(), controlplane.ArtifactCreateInput{
			OperationID: operation.ID, Role: controlplane.ArtifactRoleInput, Policy: auth.ArtifactPolicy,
			MediaType: mediaType, StoreDriver: storeDriver, ExpectedSizeBytes: contentLength,
			ExpectedSHA256: checksum, MaxBytes: publicUploadMaxBytes,
		}, c.Request.Body)
		if err != nil {
			_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "upload_failed")
			_ = control.CompleteAIOperation(c.Request.Context(), operation.ID, controlplane.AIOperationStatusFailed, "upload_failed")
			writeGatewayError(c, err)
			return
		}
		if err := control.CompleteAIOperation(c.Request.Context(), operation.ID, controlplane.AIOperationStatusSucceeded, ""); err != nil {
			writeGatewayError(c, err)
			return
		}
		c.Header("Location", "/v1/uploads/"+artifact.ID)
		c.Header("X-AsterRouter-Operation-ID", operation.ID)
		c.JSON(http.StatusCreated, newPublicUploadResponse(artifact))
	})

	r.GET("/v1/uploads/:upload_id", func(c *gin.Context) {
		auth, ok := authorizePublicArtifactAction(c, control, controlplane.GatewayScopeArtifactsRead)
		if !ok {
			return
		}
		artifact, found, err := control.ArtifactForAuth(c.Request.Context(), auth, c.Param("upload_id"))
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		if !found || artifact.Role != controlplane.ArtifactRoleInput {
			openAIError(c, http.StatusNotFound, "resource_not_found", "upload not found")
			return
		}
		c.JSON(http.StatusOK, newPublicUploadResponse(artifact))
	})
}

func newPublicUploadResponse(artifact controlplane.Artifact) publicUploadResponse {
	return publicUploadResponse{
		ID: artifact.ID, Object: "upload", OperationID: artifact.OperationID, ArtifactID: artifact.ID,
		Status: artifact.Status, Offset: artifact.SizeBytes, SizeBytes: artifact.SizeBytes, MediaType: artifact.MediaType,
		ExpiresAt: artifact.RetainUntil, Links: map[string]string{
			"self": "/v1/uploads/" + artifact.ID, "artifact": "/v1/artifacts/" + artifact.ID,
			"content": "/v1/artifacts/" + artifact.ID + "/content",
		},
	}
}
