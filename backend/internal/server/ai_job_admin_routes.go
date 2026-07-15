package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func registerAIJobAdminRoutes(group *gin.RouterGroup, control *controlplane.Service, runtime AIJobRuntimeStatusProvider, profileScope string) {
	group.GET("/ai-jobs", func(c *gin.Context) {
		data, err := control.ListAIJobsAdmin(c.Request.Context(), aiJobAdminQuery(c, profileScope))
		if err != nil {
			writeAIJobAdminError(c, err)
			return
		}
		httpx.OK(c, data)
	})
	group.GET("/ai-jobs/summary", func(c *gin.Context) {
		data, err := control.AIJobSummaryAdmin(c.Request.Context(), aiJobAdminQuery(c, profileScope))
		if err != nil {
			writeAIJobAdminError(c, err)
			return
		}
		httpx.OK(c, data)
	})
	group.GET("/ai-jobs/runtime", func(c *gin.Context) {
		if runtime == nil {
			httpx.OK(c, controlplane.DurableAIJobRuntimeStatus{QueueDriver: "unavailable"})
			return
		}
		httpx.OK(c, runtime.Status())
	})
	group.GET("/ai-jobs/:id", func(c *gin.Context) {
		data, err := control.AIJobAdmin(c.Request.Context(), c.Param("id"))
		if err == nil && !aiJobAdminScopeMatches(data.Job, profileScope) {
			err = controlplane.ErrAIJobNotFound
		}
		if err != nil {
			writeAIJobAdminError(c, err)
			return
		}
		httpx.OK(c, data)
	})
	group.POST("/ai-jobs/:id/cancel", func(c *gin.Context) {
		data, err := control.AIJobAdmin(c.Request.Context(), c.Param("id"))
		if err == nil && !aiJobAdminScopeMatches(data.Job, profileScope) {
			err = controlplane.ErrAIJobNotFound
		}
		if err != nil {
			writeAIJobAdminError(c, err)
			return
		}
		result, err := control.CancelAIJobAdmin(c.Request.Context(), actor(c), data.Job.ID)
		if err != nil {
			writeAIJobAdminError(c, err)
			return
		}
		httpx.OK(c, result)
	})
	group.POST("/ai-jobs/:id/attempts/:attemptID/reconcile", func(c *gin.Context) {
		data, err := control.AIJobAdmin(c.Request.Context(), c.Param("id"))
		if err == nil && !aiJobAdminScopeMatches(data.Job, profileScope) {
			err = controlplane.ErrAIJobNotFound
		}
		if err != nil {
			writeAIJobAdminError(c, err)
			return
		}
		result, err := control.ScheduleAIAttemptReconciliationAdmin(c.Request.Context(), actor(c), data.Job.ID, c.Param("attemptID"))
		if err != nil {
			writeAIJobAdminError(c, err)
			return
		}
		httpx.OK(c, result)
	})
}

func aiJobAdminQuery(c *gin.Context, profileScope string) controlplane.AIJobQuery {
	if strings.TrimSpace(profileScope) == "" {
		profileScope = strings.TrimSpace(c.Query("profile_scope"))
	}
	return controlplane.AIJobQuery{
		Search: strings.TrimSpace(c.Query("q")), ProfileScope: strings.TrimSpace(profileScope),
		TenantID: strings.TrimSpace(c.Query("tenant_id")), Model: strings.TrimSpace(c.Query("model")),
		Modality: strings.TrimSpace(c.Query("modality")), Operation: strings.TrimSpace(c.Query("operation")),
		Status: strings.TrimSpace(c.Query("status")), ArtifactPolicy: strings.TrimSpace(c.Query("artifact_policy")),
		Limit: intQuery(c, "limit", 50), Offset: intQuery(c, "offset", 0),
	}
}

func aiJobAdminScopeMatches(job controlplane.AIJobAdminRecord, profileScope string) bool {
	profileScope = strings.TrimSpace(profileScope)
	return profileScope == "" || job.ProfileScope == profileScope
}

func writeAIJobAdminError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, controlplane.ErrAIJobNotFound), errors.Is(err, controlplane.ErrAIAttemptNotFound):
		httpx.Error(c, http.StatusNotFound, 1570, err.Error())
	case errors.Is(err, controlplane.ErrAIJobAdminQueryInvalid):
		httpx.Error(c, http.StatusBadRequest, 1571, err.Error())
	case errors.Is(err, controlplane.ErrAIJobNotCancelable), errors.Is(err, controlplane.ErrAIAttemptReconcileScheduling),
		errors.Is(err, controlplane.ErrAIAttemptDispatchState):
		httpx.Error(c, http.StatusConflict, 1572, err.Error())
	default:
		httpx.Error(c, http.StatusInternalServerError, 1573, err.Error())
	}
}
