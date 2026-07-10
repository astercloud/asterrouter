package server

import (
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func registerModelPricingAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/model-pricings", func(c *gin.Context) {
		data, err := control.ListModelPricings(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1112, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/model-pricings", func(c *gin.Context) {
		var req controlplane.ModelPricingRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1510, "invalid model pricing payload")
			return
		}
		data, err := control.CreateModelPricing(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1511, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/model-pricings/:id", func(c *gin.Context) {
		var req controlplane.ModelPricingRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1510, "invalid model pricing payload")
			return
		}
		data, err := control.UpdateModelPricing(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1511, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}
