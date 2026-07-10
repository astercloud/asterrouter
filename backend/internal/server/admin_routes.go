package server

import (
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func registerAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service, exportJobs CSVExportJobStore) {
	if control == nil {
		return
	}
	registerDashboardAdminRoutes(admin, control)
	registerProviderAdminRoutes(admin, control)
	registerProjectAdminRoutes(admin, control)
	registerIdentityAdminRoutes(admin, control)
	registerDepartmentAdminRoutes(admin, control)
	registerRoutingAdminRoutes(admin, control)
	registerAPIKeyAdminRoutes(admin, control)
	registerModelPricingAdminRoutes(admin, control)
	registerObservabilityAdminRoutes(admin, control)
	registerAlertAdminRoutes(admin, control)
	registerCSVExportJobRoutes(admin.Group("/export-jobs"), control, exportJobs)
}

func registerDashboardAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/dashboard", func(c *gin.Context) {
		data, err := control.Dashboard(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1100, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}

func registerProviderAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/providers", func(c *gin.Context) {
		data, err := control.ListProviders(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1101, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/provider-health-checks", func(c *gin.Context) {
		data, err := control.ListProviderHealthChecks(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1110, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/providers", func(c *gin.Context) {
		var req controlplane.ProviderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1500, "invalid provider payload")
			return
		}
		data, err := control.CreateProvider(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1501, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/providers/:id", func(c *gin.Context) {
		var req controlplane.ProviderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1500, "invalid provider payload")
			return
		}
		data, err := control.UpdateProvider(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1501, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/providers/:id/check", func(c *gin.Context) {
		data, err := control.CheckProvider(c.Request.Context(), actor(c), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1501, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}

func registerProjectAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/projects", func(c *gin.Context) {
		data, err := control.ListProjects(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1102, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/projects", func(c *gin.Context) {
		var req controlplane.ProjectRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1502, "invalid project payload")
			return
		}
		data, err := control.CreateProject(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1503, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/projects/:id", func(c *gin.Context) {
		var req controlplane.ProjectRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1502, "invalid project payload")
			return
		}
		data, err := control.UpdateProject(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1503, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/applications", func(c *gin.Context) {
		data, err := control.ListApplications(c.Request.Context(), "")
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1103, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/applications/:id", func(c *gin.Context) {
		var req controlplane.ApplicationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1504, "invalid application payload")
			return
		}
		data, err := control.UpdateApplication(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1505, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/projects/:projectID/applications", func(c *gin.Context) {
		data, err := control.ListApplications(c.Request.Context(), c.Param("projectID"))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1104, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/projects/:projectID/applications", func(c *gin.Context) {
		var req controlplane.ApplicationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1504, "invalid application payload")
			return
		}
		req.ProjectID = c.Param("projectID")
		data, err := control.CreateApplication(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1505, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}

func registerRoutingAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/routing-groups", func(c *gin.Context) {
		data, err := control.ListRoutingGroups(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1108, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/routing-groups", func(c *gin.Context) {
		var req controlplane.RoutingGroupRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1510, "invalid routing group payload")
			return
		}
		data, err := control.CreateRoutingGroup(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1511, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/routing-groups/:id", func(c *gin.Context) {
		var req controlplane.RoutingGroupRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1510, "invalid routing group payload")
			return
		}
		data, err := control.UpdateRoutingGroup(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1511, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/provider-accounts", func(c *gin.Context) {
		data, err := control.ListProviderAccounts(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1109, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/provider-account-health-checks", func(c *gin.Context) {
		data, err := control.ListProviderAccountHealthChecks(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1111, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/provider-accounts", func(c *gin.Context) {
		var req controlplane.ProviderAccountRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1512, "invalid provider account payload")
			return
		}
		data, err := control.CreateProviderAccount(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1513, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/provider-accounts/:id", func(c *gin.Context) {
		var req controlplane.ProviderAccountRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1512, "invalid provider account payload")
			return
		}
		data, err := control.UpdateProviderAccount(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1513, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/provider-accounts/:id/check", func(c *gin.Context) {
		data, err := control.CheckProviderAccount(c.Request.Context(), actor(c), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1513, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}

func registerAPIKeyAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/api-keys", func(c *gin.Context) {
		data, err := control.ListAPIKeys(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1105, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/api-keys", func(c *gin.Context) {
		var req controlplane.APIKeyCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1506, "invalid api key payload")
			return
		}
		data, err := control.CreateAPIKey(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1507, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/api-keys/:id", func(c *gin.Context) {
		var req controlplane.APIKeyUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1506, "invalid api key payload")
			return
		}
		data, err := control.UpdateAPIKey(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1507, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/api-keys/:id/rotate", func(c *gin.Context) {
		data, err := control.RotateAPIKey(c.Request.Context(), actor(c), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1507, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/api-keys/:id/disable", func(c *gin.Context) {
		if err := control.DisableAPIKey(c.Request.Context(), actor(c), c.Param("id")); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1508, err.Error())
			return
		}
		httpx.OK(c, gin.H{"status": "disabled"})
	})
}

func registerObservabilityAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/audit-logs", func(c *gin.Context) {
		data, err := control.ListAuditLogsQuery(c.Request.Context(), auditLogQuery(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1106, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/audit-logs/summary", func(c *gin.Context) {
		data, err := control.AuditLogSummaryQuery(c.Request.Context(), auditLogQuery(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1106, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/audit-logs/export", func(c *gin.Context) {
		data, err := collectAuditLogsForExport(c, control)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1106, err.Error())
			return
		}
		writeCSV(c, "audit-logs.csv", auditLogCSVRows(data))
	})
	admin.GET("/usage", func(c *gin.Context) {
		data, err := control.UsageReportQuery(c.Request.Context(), usageQuery(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1107, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/usage/export", func(c *gin.Context) {
		data, err := collectUsageRecordsForExport(c, control)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1107, err.Error())
			return
		}
		writeCSV(c, "usage-records.csv", usageCSVRows(data))
	})
	admin.GET("/cost-allocation", func(c *gin.Context) {
		data, err := control.CostAllocationReportQuery(c.Request.Context(), c.Query("dimension"), usageQuery(c))
		if err != nil {
			writeCostAllocationError(c, err)
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/cost-allocation/export", func(c *gin.Context) {
		query := usageQuery(c)
		query.Limit, query.Offset = exportWindow(c)
		data, err := control.CostAllocationReportQuery(c.Request.Context(), c.Query("dimension"), query)
		if err != nil {
			writeCostAllocationError(c, err)
			return
		}
		writeCSV(c, "cost-allocation.csv", costAllocationCSVRows(data))
	})
	admin.GET("/gateway-traces", func(c *gin.Context) {
		data, err := control.ListGatewayTracesQuery(c.Request.Context(), gatewayTraceQuery(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1109, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/gateway-traces/summary", func(c *gin.Context) {
		data, err := control.GatewayTraceSummaryQuery(c.Request.Context(), gatewayTraceQuery(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1109, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/gateway-traces/export", func(c *gin.Context) {
		data, err := collectGatewayTracesForExport(c, control)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1109, err.Error())
			return
		}
		writeCSV(c, "gateway-traces.csv", gatewayTraceCSVRows(data))
	})
}
