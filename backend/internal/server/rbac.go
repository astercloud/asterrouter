package server

import (
	"net/http"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func requireRBAC(control *controlplane.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if control == nil {
			c.Next()
			return
		}
		permission := permissionForRequest(c)
		if permission == "" {
			c.Next()
			return
		}
		allowed, access, err := control.ActorCan(c.Request.Context(), actor(c), permission)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1450, err.Error())
			c.Abort()
			return
		}
		if !allowed {
			httpx.Error(c, http.StatusForbidden, 1451, "permission denied")
			c.Abort()
			return
		}
		c.Set("principal_access", access)
		c.Next()
	}
}

func permissionForRequest(c *gin.Context) string {
	path := strings.TrimPrefix(c.FullPath(), "/api/v1/admin")
	if path == "" {
		path = strings.TrimPrefix(c.Request.URL.Path, "/api/v1/admin")
	}
	method := c.Request.Method
	if strings.HasPrefix(path, "/plugins") {
		if method == http.MethodGet {
			return controlplane.PermissionAdminRead
		}
		return controlplane.PermissionPluginManage
	}
	if strings.HasPrefix(path, "/system") {
		if method == http.MethodGet {
			return controlplane.PermissionAdminRead
		}
		return controlplane.PermissionSystemManage
	}
	if strings.HasPrefix(path, "/export-jobs") {
		if method == http.MethodGet {
			return controlplane.PermissionAdminAudit
		}
		return controlplane.PermissionExportManage
	}
	if strings.HasPrefix(path, "/audit-logs") || strings.Contains(path, "/export") {
		return controlplane.PermissionAdminAudit
	}
	if path == "/settings" {
		if method == http.MethodGet {
			return controlplane.PermissionAdminRead
		}
		return controlplane.PermissionSettingsManage
	}
	if method == http.MethodGet {
		return controlplane.PermissionAdminRead
	}
	return controlplane.PermissionAdminWrite
}
