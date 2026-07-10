package server

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/gin-gonic/gin"
)

func registerPluginRoutes(group *gin.RouterGroup, svc *plugins.Service, control *controlplane.Service) {
	group.GET("", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		catalog, err := svc.Catalog(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1701, err.Error())
			return
		}
		httpx.OK(c, catalog)
	})
	group.POST("/:id/enable", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		plugin, err := svc.Enable(c.Request.Context(), c.Param("id"))
		if err != nil {
			writePluginError(c, err)
			return
		}
		_ = recordPluginEvent(c, control, "enable", plugin.ID, fmt.Sprintf("Enabled plugin %s", plugin.Name))
		httpx.OK(c, plugin)
	})
	group.POST("/:id/disable", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		plugin, err := svc.Disable(c.Request.Context(), c.Param("id"))
		if err != nil {
			writePluginError(c, err)
			return
		}
		_ = recordPluginEvent(c, control, "disable", plugin.ID, fmt.Sprintf("Disabled plugin %s", plugin.Name))
		httpx.OK(c, plugin)
	})
}

func writePluginError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, plugins.ErrPluginNotFound):
		httpx.Error(c, http.StatusNotFound, 1704, err.Error())
	case errors.Is(err, plugins.ErrPluginLocked), errors.Is(err, plugins.ErrPluginCoreRequired):
		httpx.Error(c, http.StatusConflict, 1709, err.Error())
	default:
		httpx.Error(c, http.StatusInternalServerError, 1701, err.Error())
	}
}

func recordPluginEvent(c *gin.Context, control *controlplane.Service, action string, pluginID string, summary string) error {
	if control == nil {
		return nil
	}
	return control.RecordPluginEvent(c.Request.Context(), actor(c), action, pluginID, summary)
}
