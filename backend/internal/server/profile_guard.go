package server

import (
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/gin-gonic/gin"
)

func requireProfile(svc *settings.Service, profile string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			c.Next()
			return
		}
		current, err := svc.Public(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1404, err.Error())
			c.Abort()
			return
		}
		for _, enabled := range current.EnabledProfiles {
			if enabled == profile {
				c.Next()
				return
			}
		}
		httpx.Error(c, http.StatusNotFound, 1405, "profile is not enabled")
		c.Abort()
	}
}
