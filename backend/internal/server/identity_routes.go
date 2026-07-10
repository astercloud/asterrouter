package server

import (
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func registerIdentityAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/users", func(c *gin.Context) {
		data, err := control.ListWorkspaceUsers(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1120, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/users", func(c *gin.Context) {
		var req controlplane.WorkspaceUserRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1520, "invalid user payload")
			return
		}
		data, err := control.CreateWorkspaceUser(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1521, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/users/:id", func(c *gin.Context) {
		var req controlplane.WorkspaceUserRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1520, "invalid user payload")
			return
		}
		data, err := control.UpdateWorkspaceUser(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1521, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/role-bindings", func(c *gin.Context) {
		data, err := control.ListRoleBindings(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1121, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/role-bindings", func(c *gin.Context) {
		var req controlplane.RoleBindingRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1522, "invalid role binding payload")
			return
		}
		data, err := control.CreateRoleBinding(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1523, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.DELETE("/role-bindings/:id", func(c *gin.Context) {
		if err := control.DeleteRoleBinding(c.Request.Context(), actor(c), c.Param("id")); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1524, err.Error())
			return
		}
		httpx.OK(c, gin.H{"status": "deleted"})
	})
}
