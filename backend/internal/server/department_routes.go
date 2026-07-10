package server

import (
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func registerDepartmentAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/departments", func(c *gin.Context) {
		data, err := control.ListDepartments(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1122, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/departments", func(c *gin.Context) {
		var req controlplane.DepartmentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1525, "invalid department payload")
			return
		}
		data, err := control.CreateDepartment(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1526, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/departments/:id", func(c *gin.Context) {
		var req controlplane.DepartmentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1525, "invalid department payload")
			return
		}
		data, err := control.UpdateDepartment(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1526, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}
