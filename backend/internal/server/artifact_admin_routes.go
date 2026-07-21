package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func registerArtifactAdminRoutes(group *gin.RouterGroup, control *controlplane.Service, profileScope string) {
	group.GET("/artifacts", func(c *gin.Context) {
		data, err := control.ListArtifactsAdmin(c.Request.Context(), artifactAdminQuery(c, profileScope))
		if err != nil {
			writeArtifactAdminError(c, err)
			return
		}
		httpx.OK(c, data)
	})
	group.GET("/artifacts/summary", func(c *gin.Context) {
		data, err := control.ArtifactSummaryAdmin(c.Request.Context(), artifactAdminQuery(c, profileScope))
		if err != nil {
			writeArtifactAdminError(c, err)
			return
		}
		httpx.OK(c, data)
	})
	group.GET("/artifacts/:id", func(c *gin.Context) {
		data, err := control.ArtifactAdmin(c.Request.Context(), c.Param("id"))
		if err == nil && !artifactAdminScopeMatches(data.Artifact, profileScope) {
			err = controlplane.ErrArtifactNotFound
		}
		if err != nil {
			writeArtifactAdminError(c, err)
			return
		}
		httpx.OK(c, data)
	})
	group.GET("/artifacts/:id/content", func(c *gin.Context) {
		data, err := control.ArtifactAdmin(c.Request.Context(), c.Param("id"))
		if err == nil && !artifactAdminScopeMatches(data.Artifact, profileScope) {
			err = controlplane.ErrArtifactNotFound
		}
		if err != nil {
			writeArtifactAdminError(c, err)
			return
		}
		byteRange, err := parseArtifactRange(c.GetHeader("Range"), data.Artifact.SizeBytes)
		if err != nil {
			c.Header("Content-Range", fmt.Sprintf("bytes */%d", data.Artifact.SizeBytes))
			httpx.Error(c, http.StatusRequestedRangeNotSatisfiable, 1568, "artifact byte range is not satisfiable")
			return
		}
		artifact, opened, found, err := control.OpenArtifactAdmin(c.Request.Context(), data.Artifact.ID, profileScope, byteRange)
		if err == nil && !found {
			err = controlplane.ErrArtifactNotFound
		}
		if err != nil {
			writeArtifactAdminError(c, err)
			return
		}
		defer opened.Body.Close()
		writeArtifactContent(c, artifact, opened, byteRange != nil)
	})
	group.POST("/artifacts/:id/retry-delivery", func(c *gin.Context) {
		data, err := control.ArtifactAdmin(c.Request.Context(), c.Param("id"))
		if err == nil && !artifactAdminScopeMatches(data.Artifact, profileScope) {
			err = controlplane.ErrArtifactNotFound
		}
		if err != nil {
			writeArtifactAdminError(c, err)
			return
		}
		result, err := control.RetryArtifactDelivery(c.Request.Context(), actor(c), data.Artifact.ID)
		if err != nil {
			writeArtifactAdminError(c, err)
			return
		}
		httpx.OK(c, result)
	})
	group.GET("/artifact-runtimes", func(c *gin.Context) {
		httpx.OK(c, control.ArtifactRuntimes())
	})
}

func artifactAdminQuery(c *gin.Context, profileScope string) controlplane.ArtifactQuery {
	if strings.TrimSpace(profileScope) == "" {
		profileScope = strings.TrimSpace(c.Query("profile_scope"))
	}
	return controlplane.ArtifactQuery{
		ProfileScope: strings.TrimSpace(profileScope),
		TenantID:     strings.TrimSpace(c.Query("tenant_id")),
		Search:       strings.TrimSpace(c.Query("q")),
		OperationID:  strings.TrimSpace(c.Query("operation_id")),
		JobID:        strings.TrimSpace(c.Query("job_id")),
		AttemptID:    strings.TrimSpace(c.Query("attempt_id")),
		Role:         strings.TrimSpace(c.Query("role")),
		Policy:       strings.TrimSpace(c.Query("policy")),
		Status:       strings.TrimSpace(c.Query("status")),
		Limit:        intQuery(c, "limit", 50),
		Offset:       intQuery(c, "offset", 0),
	}
}

func artifactAdminScopeMatches(artifact controlplane.ArtifactAdminRecord, profileScope string) bool {
	profileScope = strings.TrimSpace(profileScope)
	return profileScope == "" || artifact.ProfileScope == profileScope
}

func writeArtifactAdminError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, controlplane.ErrArtifactNotFound):
		httpx.Error(c, http.StatusNotFound, 1560, err.Error())
	case errors.Is(err, controlplane.ErrArtifactAdminQueryInvalid):
		httpx.Error(c, http.StatusBadRequest, 1561, err.Error())
	case errors.Is(err, controlplane.ErrArtifactDeliveryRetry), errors.Is(err, controlplane.ErrAIAttemptDispatchState):
		httpx.Error(c, http.StatusConflict, 1562, err.Error())
	case errors.Is(err, controlplane.ErrArtifactSinkRequired):
		httpx.Error(c, http.StatusServiceUnavailable, 1563, err.Error())
	case errors.Is(err, controlplane.ErrArtifactUnavailable):
		httpx.Error(c, http.StatusGone, 1565, err.Error())
	case errors.Is(err, controlplane.ErrArtifactStoreRequired), errors.Is(err, controlplane.ErrArtifactProxyRequired):
		httpx.Error(c, http.StatusServiceUnavailable, 1566, err.Error())
	case errors.Is(err, controlplane.ErrArtifactIntegrity):
		httpx.Error(c, http.StatusUnprocessableEntity, 1567, err.Error())
	default:
		httpx.Error(c, http.StatusInternalServerError, 1564, err.Error())
	}
}
