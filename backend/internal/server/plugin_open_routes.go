package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/gin-gonic/gin"
)

func registerPluginOpenRoutes(group *gin.RouterGroup, svc *plugins.Service, control *controlplane.Service) {
	group.GET("/catalog", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		surface := pluginOpenSurface(c)
		if surface == "" {
			httpx.Error(c, http.StatusBadRequest, 1776, "surface is required")
			return
		}
		token, err := svc.AuthorizePluginAPIToken(c.Request.Context(), bearerToken(c), plugins.PluginAPIScopeCatalogRead, "", surface)
		if err != nil {
			writePluginOpenAuthError(c, err)
			return
		}
		c.Set("actor", "plugin-api:"+token.ID)
		catalog, err := svc.CatalogForSurface(c.Request.Context(), surface)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1777, err.Error())
			return
		}
		httpx.OK(c, catalog)
	})

	group.Any("/:id/actions/*action", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		surface := pluginOpenSurface(c)
		if surface == "" {
			httpx.Error(c, http.StatusBadRequest, 1776, "surface is required")
			return
		}
		pluginID := strings.TrimSpace(c.Param("id"))
		token, err := svc.AuthorizePluginAPIToken(c.Request.Context(), bearerToken(c), plugins.PluginAPIScopeAction, pluginID, surface)
		if err != nil {
			writePluginOpenAuthError(c, err)
			return
		}
		if err := svc.RequireSurface(c.Request.Context(), pluginID, surface); err != nil {
			writePluginAPITokenError(c, err)
			return
		}
		c.Set("actor", "plugin-api:"+token.ID)
		actionPath := "/actions/" + strings.TrimPrefix(c.Param("action"), "/")
		response, err := svc.ProxySidecarHTTP(c.Request.Context(), pluginID, actionPath, c.Request)
		if err != nil {
			writeRuntimeError(c, err)
			return
		}
		defer response.Body.Close()
		copyProxyResponseHeaders(c.Writer.Header(), response.Header)
		c.Status(response.StatusCode)
		_, _ = io.Copy(c.Writer, response.Body)
		_ = recordPluginEvent(c, control, "open_api_action", pluginID, fmt.Sprintf("Invoked plugin action %s", strings.TrimPrefix(c.Param("action"), "/")))
	})
}

func pluginOpenSurface(c *gin.Context) string {
	surface := strings.TrimSpace(c.Query("surface"))
	if surface == "" {
		surface = strings.TrimSpace(c.GetHeader("X-Aster-Surface"))
	}
	return surface
}

func writePluginOpenAuthError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, plugins.ErrPluginAPITokenScope):
		httpx.Error(c, http.StatusForbidden, 1774, err.Error())
	default:
		httpx.Error(c, http.StatusUnauthorized, 1778, "plugin API token required")
	}
}
