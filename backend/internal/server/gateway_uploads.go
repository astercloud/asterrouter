package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/gin-gonic/gin"
)

const (
	publicUploadMaxBytes      = controlplane.ArtifactDefaultMaxBytes
	publicUploadChunkMaxBytes = int64(64 << 20)
)

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
		initOnly := contentLength == 0
		totalLength := contentLength
		if initOnly {
			uploadLength, parseErr := strconv.ParseInt(strings.TrimSpace(c.GetHeader("Upload-Length")), 10, 64)
			if parseErr != nil || uploadLength <= 0 {
				openAIError(c, http.StatusBadRequest, "invalid_request_error", "Upload-Length must be a positive integer for an upload session")
				return
			}
			totalLength = uploadLength
		}
		if totalLength <= 0 || totalLength > publicUploadMaxBytes {
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
			"content_length": totalLength, "media_type": mediaType, "sha256": checksum,
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
		if initOnly {
			state := controlplane.ArtifactUploadState{ExpectedSize: totalLength, ExpectedSHA256: checksum, Offset: 0, MediaType: mediaType, StoreDriver: storeDriver}
			stateReference, stateErr := json.Marshal(state)
			if stateErr != nil {
				_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "upload_session_metadata_failed")
				_ = control.CompleteAIOperation(c.Request.Context(), operation.ID, controlplane.AIOperationStatusFailed, "upload_session_metadata_failed")
				openAIError(c, http.StatusInternalServerError, "server_error", "failed to initialize upload session")
				return
			}
			artifact, createErr := control.CreatePendingArtifact(c.Request.Context(), controlplane.ArtifactCreateInput{
				OperationID: operation.ID, Role: controlplane.ArtifactRoleInput, Policy: auth.ArtifactPolicy,
				MediaType: mediaType, ExternalReference: string(stateReference),
			})
			if createErr != nil {
				_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "upload_session_failed")
				_ = control.CompleteAIOperation(c.Request.Context(), operation.ID, controlplane.AIOperationStatusFailed, "upload_session_failed")
				writeGatewayError(c, createErr)
				return
			}
			updated, changed, updateErr := control.UpdateArtifactUploadState(c.Request.Context(), auth, artifact.ID, artifact.StatusVersion, state)
			if updateErr != nil || !changed {
				_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "upload_session_state_failed")
				_ = control.CompleteAIOperation(c.Request.Context(), operation.ID, controlplane.AIOperationStatusFailed, "upload_session_state_failed")
				if updateErr != nil {
					writeGatewayError(c, updateErr)
				} else {
					openAIError(c, http.StatusConflict, "upload_state_conflict", "upload session state changed concurrently")
				}
				return
			}
			c.Header("Location", "/v1/uploads/"+updated.ID)
			c.Header("X-AsterRouter-Operation-ID", operation.ID)
			c.Header("Upload-Offset", "0")
			c.JSON(http.StatusCreated, newPublicUploadResponse(updated))
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
		if err := control.ReleaseBillingHold(c.Request.Context(), operation.ID, "upload_no_provider_charge"); err != nil {
			_ = control.CompleteAIOperation(c.Request.Context(), operation.ID, controlplane.AIOperationStatusFailed, "upload_billing_release_failed")
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

	r.PATCH("/v1/uploads/:upload_id", func(c *gin.Context) {
		if control == nil {
			openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
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
		session, state, found, err := control.ArtifactUploadStateForAuth(c.Request.Context(), auth, c.Param("upload_id"))
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		if !found {
			openAIError(c, http.StatusNotFound, "resource_not_found", "upload not found")
			return
		}
		if session.Status == controlplane.ArtifactStatusReady && state.Completed {
			c.Header("Upload-Offset", strconv.FormatInt(state.Offset, 10))
			c.JSON(http.StatusOK, newPublicUploadResponse(session))
			return
		}
		if session.Status != controlplane.ArtifactStatusUploading {
			openAIError(c, http.StatusConflict, "upload_state_conflict", "upload session is not accepting chunks")
			return
		}
		offset, parseErr := strconv.ParseInt(strings.TrimSpace(c.GetHeader("Upload-Offset")), 10, 64)
		if parseErr != nil || offset < 0 {
			openAIError(c, http.StatusBadRequest, "invalid_upload_offset", "Upload-Offset must be a non-negative integer")
			return
		}
		if offset != state.Offset {
			c.Header("Upload-Offset", strconv.FormatInt(state.Offset, 10))
			openAIError(c, http.StatusConflict, "upload_offset_conflict", "Upload-Offset does not match the current session offset")
			return
		}
		chunkLength := c.Request.ContentLength
		if chunkLength <= 0 || chunkLength > publicUploadChunkMaxBytes || offset+chunkLength > state.ExpectedSize {
			openAIError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "upload chunk size is outside the allowed range")
			return
		}
		checksum := strings.ToLower(strings.TrimSpace(c.GetHeader("X-Checksum-SHA256")))
		if len(checksum) != 64 {
			openAIError(c, http.StatusBadRequest, "invalid_request_error", "X-Checksum-SHA256 is required for upload chunks")
			return
		}
		if _, err := hex.DecodeString(checksum); err != nil {
			openAIError(c, http.StatusBadRequest, "invalid_request_error", "X-Checksum-SHA256 must be hexadecimal")
			return
		}
		chunks, err := control.ListArtifactUploadChunksForAuth(c.Request.Context(), auth, session.ID)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		for _, chunk := range chunks {
			var chunkState struct {
				Offset int64  `json:"offset"`
				Size   int64  `json:"size"`
				SHA256 string `json:"sha256"`
			}
			if json.Unmarshal([]byte(chunk.ExternalReference), &chunkState) == nil && chunkState.Offset == offset {
				if chunkState.Size == chunkLength && strings.EqualFold(chunkState.SHA256, checksum) {
					c.Header("Upload-Offset", strconv.FormatInt(state.Offset, 10))
					c.JSON(http.StatusOK, newPublicUploadResponse(session))
					return
				}
				openAIError(c, http.StatusConflict, "upload_offset_conflict", "a different chunk already occupies this offset")
				return
			}
		}
		chunkDigest := sha256.Sum256([]byte(session.ID + ":" + strconv.FormatInt(offset, 10)))
		chunkID := "artifact_" + hex.EncodeToString(chunkDigest[:])[:24]
		mediaType := strings.TrimSpace(c.GetHeader("Content-Type"))
		if mediaType == "" {
			mediaType = state.MediaType
		}
		chunkMetadata, _ := json.Marshal(map[string]interface{}{"offset": offset, "size": chunkLength, "sha256": checksum, "media_type": mediaType})
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, publicUploadChunkMaxBytes)
		_, err = control.CreateArtifactFromReader(c.Request.Context(), controlplane.ArtifactCreateInput{
			ID: chunkID, OperationID: session.OperationID, SourceArtifactID: session.ID, Role: controlplane.ArtifactRoleDerived,
			Policy: session.Policy, MediaType: mediaType, StoreDriver: state.StoreDriver, ExternalReference: string(chunkMetadata),
			ExpectedSizeBytes: chunkLength, ExpectedSHA256: checksum, MaxBytes: publicUploadChunkMaxBytes,
		}, c.Request.Body)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		state.Offset += chunkLength
		updated, changed, err := control.UpdateArtifactUploadState(c.Request.Context(), auth, session.ID, session.StatusVersion, state)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		if !changed {
			c.Header("Upload-Offset", strconv.FormatInt(state.Offset-chunkLength, 10))
			openAIError(c, http.StatusConflict, "upload_offset_conflict", "upload session changed concurrently")
			return
		}
		c.Header("Upload-Offset", strconv.FormatInt(state.Offset, 10))
		c.JSON(http.StatusOK, newPublicUploadResponse(updated))
	})

	r.POST("/v1/uploads/:upload_id/complete", func(c *gin.Context) {
		if control == nil {
			openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
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
		session, state, found, err := control.ArtifactUploadStateForAuth(c.Request.Context(), auth, c.Param("upload_id"))
		if err != nil || !found {
			if err != nil {
				writeGatewayError(c, err)
			} else {
				openAIError(c, http.StatusNotFound, "resource_not_found", "upload not found")
			}
			return
		}
		if session.Status != controlplane.ArtifactStatusReady && state.Offset != state.ExpectedSize {
			c.Header("Upload-Offset", strconv.FormatInt(state.Offset, 10))
			openAIError(c, http.StatusConflict, "upload_incomplete", "upload session has not received all expected bytes")
			return
		}
		artifact, err := control.CompleteArtifactUpload(c.Request.Context(), auth, session.ID, session.StatusVersion)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		operation, operationFound, operationErr := control.AIOperation(c.Request.Context(), artifact.OperationID)
		if operationErr != nil || !operationFound {
			openAIError(c, http.StatusInternalServerError, "server_error", "upload operation is unavailable")
			return
		}
		if operation.Status != controlplane.AIOperationStatusSucceeded {
			if releaseErr := control.ReleaseBillingHold(c.Request.Context(), artifact.OperationID, "upload_no_provider_charge"); releaseErr != nil {
				writeGatewayError(c, releaseErr)
				return
			}
			if completeErr := control.CompleteAIOperation(c.Request.Context(), artifact.OperationID, controlplane.AIOperationStatusSucceeded, ""); completeErr != nil {
				writeGatewayError(c, completeErr)
				return
			}
		}
		c.Header("Upload-Offset", strconv.FormatInt(artifact.SizeBytes, 10))
		c.JSON(http.StatusOK, newPublicUploadResponse(artifact))
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
	offset := artifact.SizeBytes
	var state struct {
		Offset int64 `json:"offset"`
	}
	if json.Unmarshal([]byte(artifact.ExternalReference), &state) == nil && state.Offset >= 0 {
		offset = state.Offset
	}
	links := map[string]string{
		"self": "/v1/uploads/" + artifact.ID, "artifact": "/v1/artifacts/" + artifact.ID,
	}
	if artifact.Status == controlplane.ArtifactStatusReady || artifact.Status == controlplane.ArtifactStatusDelivered {
		links["content"] = "/v1/artifacts/" + artifact.ID + "/content"
	}
	return publicUploadResponse{
		ID: artifact.ID, Object: "upload", OperationID: artifact.OperationID, ArtifactID: artifact.ID,
		Status: artifact.Status, Offset: offset, SizeBytes: artifact.SizeBytes, MediaType: artifact.MediaType,
		ExpiresAt: artifact.RetainUntil, Links: links,
	}
}
