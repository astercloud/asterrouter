package server

import (
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func registerPortalRoutes(portal *gin.RouterGroup, control *controlplane.Service) {
	if control == nil {
		return
	}
	portal.GET("/workspace", func(c *gin.Context) {
		data, err := control.PortalWorkspace(c.Request.Context(), actor(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1200, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	portal.POST("/api-keys", func(c *gin.Context) {
		var req controlplane.APIKeyCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1201, "invalid api key payload")
			return
		}
		data, err := control.CreatePortalAPIKey(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1202, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	portal.POST("/api-keys/:id/rotate", func(c *gin.Context) {
		data, err := control.RotatePortalAPIKey(c.Request.Context(), actor(c), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1203, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	portal.POST("/api-keys/:id/disable", func(c *gin.Context) {
		if err := control.DisablePortalAPIKey(c.Request.Context(), actor(c), c.Param("id")); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1204, err.Error())
			return
		}
		httpx.OK(c, gin.H{"status": "disabled"})
	})
}
