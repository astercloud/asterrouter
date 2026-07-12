package server

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/gin-gonic/gin"
)

func registerPluginHostRoutes(group *gin.RouterGroup, svc *plugins.Service, control *controlplane.Service) {
	group.GET("/:plugin_id/feeds/:service_key", func(c *gin.Context) {
		if !requestFromLoopback(c.Request) {
			httpx.Error(c, http.StatusForbidden, 1790, "plugin host API is only available on loopback")
			return
		}
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		token := bearerToken(c)
		payload, err := svc.SidecarFeedPayload(c.Request.Context(), c.Param("plugin_id"), token, c.Param("service_key"))
		if err != nil {
			writePluginHostError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		if control != nil {
			_ = control.RecordPluginEvent(c.Request.Context(), "plugin:"+c.Param("plugin_id"), "feed_read", c.Param("service_key"), fmt.Sprintf("Plugin %s read official feed %s", c.Param("plugin_id"), c.Param("service_key")))
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
	})
}

func writePluginHostError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, plugins.ErrPluginHostUnauthorized):
		httpx.Error(c, http.StatusUnauthorized, 1791, err.Error())
	case errors.Is(err, plugins.ErrPluginHostPermission), errors.Is(err, plugins.ErrOfficialFeedEntitlement), errors.Is(err, plugins.ErrOfficialFeedBinding):
		httpx.Error(c, http.StatusForbidden, 1792, err.Error())
	case errors.Is(err, plugins.ErrOfficialFeedNotFound), errors.Is(err, plugins.ErrLicenseNotFound):
		httpx.Error(c, http.StatusNotFound, 1793, err.Error())
	case errors.Is(err, plugins.ErrOfficialFeedExpired):
		httpx.Error(c, http.StatusConflict, 1794, err.Error())
	default:
		httpx.Error(c, http.StatusInternalServerError, 1795, err.Error())
	}
}

func requestFromLoopback(request *http.Request) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr))
	if err != nil {
		return false
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
