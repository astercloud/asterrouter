package server

import (
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

var errArtifactRangeInvalid = errors.New("invalid artifact byte range")

type publicArtifactResponse struct {
	ID               string            `json:"id"`
	Object           string            `json:"object"`
	OperationID      string            `json:"operation_id"`
	JobID            string            `json:"job_id,omitempty"`
	AttemptID        string            `json:"attempt_id,omitempty"`
	SourceArtifactID string            `json:"source_artifact_id,omitempty"`
	Role             string            `json:"role"`
	Policy           string            `json:"policy"`
	Status           string            `json:"status"`
	StatusVersion    int               `json:"status_version"`
	MediaType        string            `json:"media_type,omitempty"`
	SizeBytes        int64             `json:"size_bytes"`
	SHA256           string            `json:"sha256,omitempty"`
	ErrorType        string            `json:"error_type,omitempty"`
	RetainUntil      time.Time         `json:"retain_until"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	ReadyAt          *time.Time        `json:"ready_at,omitempty"`
	DeliveredAt      *time.Time        `json:"delivered_at,omitempty"`
	DeletedAt        *time.Time        `json:"deleted_at,omitempty"`
	Links            map[string]string `json:"links"`
}

func registerGatewayArtifactRoutes(r *gin.Engine, control *controlplane.Service) {
	r.GET("/v1/artifacts/:artifact_id", func(c *gin.Context) {
		auth, ok := authorizePublicArtifactAction(c, control, controlplane.GatewayScopeArtifactsRead)
		if !ok {
			return
		}
		artifact, found, err := control.ArtifactForAuth(c.Request.Context(), auth, c.Param("artifact_id"))
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		if !found {
			artifactNotFound(c)
			return
		}
		c.JSON(http.StatusOK, newPublicArtifactResponse(artifact))
	})

	r.GET("/v1/jobs/:job_id/artifacts", func(c *gin.Context) {
		auth, ok := authorizePublicArtifactAction(c, control, controlplane.GatewayScopeArtifactsRead)
		if !ok {
			return
		}
		artifacts, found, err := control.ArtifactsForJobAndAuth(c.Request.Context(), auth, c.Param("job_id"))
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		if !found {
			openAIError(c, http.StatusNotFound, "resource_not_found", "ai job not found")
			return
		}
		data := make([]publicArtifactResponse, 0, len(artifacts))
		for _, artifact := range artifacts {
			data = append(data, newPublicArtifactResponse(artifact))
		}
		c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
	})

	r.GET("/v1/artifacts/:artifact_id/content", func(c *gin.Context) {
		auth, ok := authorizePublicArtifactAction(c, control, controlplane.GatewayScopeArtifactsRead)
		if !ok {
			return
		}
		artifact, found, err := control.ArtifactForAuth(c.Request.Context(), auth, c.Param("artifact_id"))
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		if !found {
			artifactNotFound(c)
			return
		}
		byteRange, err := parseArtifactRange(c.GetHeader("Range"), artifact.SizeBytes)
		if err != nil {
			c.Header("Content-Range", fmt.Sprintf("bytes */%d", artifact.SizeBytes))
			openAIError(c, http.StatusRequestedRangeNotSatisfiable, "invalid_range", "artifact byte range is not satisfiable")
			return
		}
		artifact, opened, _, err := control.OpenArtifactForAuth(c.Request.Context(), auth, artifact.ID, byteRange)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		defer opened.Body.Close()
		writeArtifactContent(c, artifact, opened, byteRange != nil)
	})

	r.DELETE("/v1/artifacts/:artifact_id", func(c *gin.Context) {
		auth, ok := authorizePublicArtifactAction(c, control, controlplane.GatewayScopeArtifactsDelete)
		if !ok {
			return
		}
		artifact, found, err := control.RequestArtifactDeletionForAuth(c.Request.Context(), auth, c.Param("artifact_id"))
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		if !found {
			artifactNotFound(c)
			return
		}
		c.JSON(http.StatusAccepted, newPublicArtifactResponse(artifact))
	})
}

func authorizePublicArtifactAction(c *gin.Context, control *controlplane.Service, scope string) (auth gatewaycore.CanonicalAuthContext, ok bool) {
	if control == nil {
		openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
		return auth, false
	}
	return authorizePublicAIJobAction(c, control, scope)
}

func artifactNotFound(c *gin.Context) {
	openAIError(c, http.StatusNotFound, "resource_not_found", "artifact not found")
}

func newPublicArtifactResponse(artifact controlplane.Artifact) publicArtifactResponse {
	links := map[string]string{"self": "/v1/artifacts/" + artifact.ID}
	if artifact.StoreDriver != controlplane.ArtifactStoreDriverNone && artifact.StoreKey != "" &&
		(artifact.Status == controlplane.ArtifactStatusReady || artifact.Status == controlplane.ArtifactStatusDelivered) {
		links["content"] = "/v1/artifacts/" + artifact.ID + "/content"
	}
	return publicArtifactResponse{
		ID: artifact.ID, Object: "artifact", OperationID: artifact.OperationID, JobID: artifact.JobID,
		AttemptID: artifact.AttemptID, SourceArtifactID: artifact.SourceArtifactID, Role: artifact.Role, Policy: artifact.Policy,
		Status: artifact.Status, StatusVersion: artifact.StatusVersion, MediaType: artifact.MediaType, SizeBytes: artifact.SizeBytes,
		SHA256: artifact.SHA256, ErrorType: artifact.ErrorType, RetainUntil: artifact.RetainUntil, CreatedAt: artifact.CreatedAt,
		UpdatedAt: artifact.UpdatedAt, ReadyAt: artifact.ReadyAt, DeliveredAt: artifact.DeliveredAt, DeletedAt: artifact.DeletedAt,
		Links: links,
	}
}

func parseArtifactRange(value string, total int64) (*controlplane.ArtifactByteRange, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if total <= 0 || !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return nil, errArtifactRangeInvalid
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes="), "-")
	if len(parts) != 2 {
		return nil, errArtifactRangeInvalid
	}
	if parts[0] == "" {
		suffix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || suffix <= 0 {
			return nil, errArtifactRangeInvalid
		}
		if suffix > total {
			suffix = total
		}
		return &controlplane.ArtifactByteRange{Offset: total - suffix, Length: suffix}, nil
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= total {
		return nil, errArtifactRangeInvalid
	}
	length := total - start
	if parts[1] != "" {
		end, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return nil, errArtifactRangeInvalid
		}
		if end >= total {
			end = total - 1
		}
		length = end - start + 1
	}
	return &controlplane.ArtifactByteRange{Offset: start, Length: length}, nil
}

func writeArtifactContent(c *gin.Context, artifact controlplane.Artifact, opened controlplane.ArtifactRead, partial bool) {
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Length", strconv.FormatInt(opened.SizeBytes, 10))
	c.Header("X-Content-Type-Options", "nosniff")
	if artifact.SHA256 != "" {
		c.Header("ETag", `"`+artifact.SHA256+`"`)
	}
	mediaType := strings.TrimSpace(artifact.MediaType)
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	c.Header("Content-Type", mediaType)
	status := http.StatusOK
	if partial {
		status = http.StatusPartialContent
		c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", opened.Offset, opened.Offset+opened.SizeBytes-1, opened.TotalBytes))
	}
	c.Status(status)
	_, _ = io.Copy(c.Writer, opened.Body)
}
