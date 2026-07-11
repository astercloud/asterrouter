package server

import (
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func registerGovernancePolicyAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/policies", func(c *gin.Context) {
		data, err := control.ListGovernancePolicies(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1127, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/policies", func(c *gin.Context) {
		var req controlplane.GovernancePolicyRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1530, "invalid policy payload")
			return
		}
		data, err := control.CreateGovernancePolicy(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1531, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/policies/:id", func(c *gin.Context) {
		var req controlplane.GovernancePolicyRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1530, "invalid policy payload")
			return
		}
		data, err := control.UpdateGovernancePolicy(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1531, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}
