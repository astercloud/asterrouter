package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func registerAlertAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	registerAlertAdminRoutesForScope(admin, control, "")
}

func registerAlertAdminRoutesForScope(admin *gin.RouterGroup, control *controlplane.Service, profileScope string) {
	admin.GET("/alerts", func(c *gin.Context) {
		query, err := scopeAlertQuery(c.Request.Context(), control, principalAccess(c), alertQuery(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1112, err.Error())
			return
		}
		query.ProfileScope = profileScope
		data, err := control.ListAlertEventsQuery(c.Request.Context(), query)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1112, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/alerts/summary", func(c *gin.Context) {
		query, err := scopeAlertQuery(c.Request.Context(), control, principalAccess(c), alertQuery(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1112, err.Error())
			return
		}
		query.ProfileScope = profileScope
		data, err := control.AlertSummaryQuery(c.Request.Context(), query)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1112, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/alerts/:id/acknowledge", func(c *gin.Context) {
		if err := requireAlertInScope(c.Request.Context(), control, c.Param("id"), profileScope); err != nil {
			httpx.Error(c, http.StatusNotFound, 1451, "alert not found")
			return
		}
		if err := requireAlertInAccess(c.Request.Context(), control, c.Param("id"), principalAccess(c)); err != nil {
			httpx.Error(c, http.StatusForbidden, 1451, err.Error())
			return
		}
		data, err := control.AcknowledgeAlert(c.Request.Context(), actor(c), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1520, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/alerts/:id/resolve", func(c *gin.Context) {
		if err := requireAlertInScope(c.Request.Context(), control, c.Param("id"), profileScope); err != nil {
			httpx.Error(c, http.StatusNotFound, 1451, "alert not found")
			return
		}
		if err := requireAlertInAccess(c.Request.Context(), control, c.Param("id"), principalAccess(c)); err != nil {
			httpx.Error(c, http.StatusForbidden, 1451, err.Error())
			return
		}
		data, err := control.ResolveAlert(c.Request.Context(), actor(c), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1521, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}

func requireAlertInScope(ctx context.Context, control *controlplane.Service, id, profileScope string) error {
	if profileScope == "" {
		return nil
	}
	event, err := control.AlertEventByID(ctx, id)
	if err != nil {
		return err
	}
	if event.ProfileScope != profileScope {
		return httpxError("alert not found")
	}
	return nil
}

func alertQuery(c *gin.Context) controlplane.AlertQuery {
	return controlplane.AlertQuery{
		Limit:        intQuery(c, "limit", 50),
		Offset:       intQuery(c, "offset", 0),
		Search:       strings.TrimSpace(c.Query("q")),
		Type:         strings.TrimSpace(c.Query("type")),
		Severity:     strings.TrimSpace(c.Query("severity")),
		Status:       strings.TrimSpace(c.Query("status")),
		ResourceType: strings.TrimSpace(c.Query("resource_type")),
		CreatedFrom:  timeQuery(c, "from"),
		CreatedTo:    timeQuery(c, "to"),
	}
}
