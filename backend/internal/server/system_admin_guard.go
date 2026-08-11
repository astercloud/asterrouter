package server

import (
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func requireSystemAdministrator(control *controlplane.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if control == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1454, "control service is not available")
			c.Abort()
			return
		}
		allowed, err := control.ActorIsSystemAdministrator(c.Request.Context(), actor(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1454, err.Error())
			c.Abort()
			return
		}
		if !allowed {
			httpx.Error(c, http.StatusForbidden, 1455, "system administrator access required")
			c.Abort()
			return
		}
		c.Next()
	}
}
