package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func registerArtifactAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/artifacts", func(c *gin.Context) {
		data, err := control.ListArtifactsAdmin(c.Request.Context(), artifactAdminQuery(c))
		if err != nil {
			writeArtifactAdminError(c, err)
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/artifacts/summary", func(c *gin.Context) {
		data, err := control.ArtifactSummaryAdmin(c.Request.Context(), artifactAdminQuery(c))
		if err != nil {
			writeArtifactAdminError(c, err)
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/artifacts/:id", func(c *gin.Context) {
		data, err := control.ArtifactAdmin(c.Request.Context(), c.Param("id"))
		if err != nil {
			writeArtifactAdminError(c, err)
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/artifacts/:id/retry-delivery", func(c *gin.Context) {
		data, err := control.RetryArtifactDelivery(c.Request.Context(), actor(c), c.Param("id"))
		if err != nil {
			writeArtifactAdminError(c, err)
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/artifact-runtimes", func(c *gin.Context) {
		httpx.OK(c, control.ArtifactRuntimes())
	})
}

func artifactAdminQuery(c *gin.Context) controlplane.ArtifactQuery {
	return controlplane.ArtifactQuery{
		ProfileScope: strings.TrimSpace(c.Query("profile_scope")),
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
	default:
		httpx.Error(c, http.StatusInternalServerError, 1564, err.Error())
	}
}
