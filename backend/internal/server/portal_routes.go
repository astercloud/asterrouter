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
		data, err := control.PortalWorkspace(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1200, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}
