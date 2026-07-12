package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/gin-gonic/gin"
)

func registerPluginRoutes(group *gin.RouterGroup, svc *plugins.Service, control *controlplane.Service, surface string) {
	group.Use(func(c *gin.Context) {
		if svc == nil || c.Param("id") == "" {
			c.Next()
			return
		}
		if err := svc.RequireSurface(c.Request.Context(), c.Param("id"), surface); err != nil {
			if errors.Is(err, plugins.ErrPluginNotFound) {
				httpx.Error(c, http.StatusNotFound, 1704, err.Error())
			} else if errors.Is(err, plugins.ErrPluginSurface) {
				httpx.Error(c, http.StatusForbidden, 1705, err.Error())
			} else {
				httpx.Error(c, http.StatusInternalServerError, 1701, err.Error())
			}
			c.Abort()
			return
		}
		c.Next()
	})
	group.GET("", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		catalog, err := svc.CatalogForSurface(c.Request.Context(), surface)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1701, err.Error())
			return
		}
		httpx.OK(c, catalog)
	})
	group.GET("/catalog-sync/status", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		status, err := svc.OfficialCatalogStatus(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1720, err.Error())
			return
		}
		httpx.OK(c, status)
	})
	group.GET("/license/status", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		status, err := svc.LicenseStatus(c.Request.Context())
		if err != nil {
			writeLicenseError(c, err)
			return
		}
		httpx.OK(c, status)
	})
	group.GET("/feeds/client", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		info, err := svc.OfficialFeedClientInfo(c.Request.Context())
		if err != nil {
			writeOfficialFeedError(c, err)
			return
		}
		httpx.OK(c, info)
	})
	group.GET("/feeds", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		items, err := svc.OfficialFeedStatuses(c.Request.Context(), c.Query("service_key"))
		if err != nil {
			writeOfficialFeedError(c, err)
			return
		}
		httpx.OK(c, items)
	})
	group.GET("/feeds/sync-runs", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		items, err := svc.OfficialFeedSyncRuns(c.Request.Context(), c.Query("service_key"), intQuery(c, "limit", 20))
		if err != nil {
			writeOfficialFeedError(c, err)
			return
		}
		httpx.OK(c, items)
	})
	group.POST("/feeds/sync", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		var req plugins.OfficialFeedSyncRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1780, "invalid official feed sync payload")
			return
		}
		result, err := svc.SyncOfficialFeed(c.Request.Context(), req.ServiceKey)
		if err != nil {
			_ = recordPluginEvent(c, control, "feed_sync_failed", req.ServiceKey, fmt.Sprintf("Official feed sync failed for service %s (%s)", req.ServiceKey, result.Run.ErrorCode))
			writeOfficialFeedError(c, err)
			return
		}
		_ = recordPluginEvent(c, control, "feed_sync", result.Feed.ServiceKey, fmt.Sprintf("Synchronized official feed %s for service %s", result.Feed.FeedID, result.Feed.ServiceKey))
		httpx.OK(c, result)
	})
	group.POST("/feeds/import", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		var req plugins.OfficialFeedImportRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1780, "invalid official feed import payload")
			return
		}
		status, err := svc.ImportOfficialFeed(c.Request.Context(), req)
		if err != nil {
			writeOfficialFeedError(c, err)
			return
		}
		_ = recordPluginEvent(c, control, "feed_import", status.ServiceKey, fmt.Sprintf("Imported official feed %s for service %s", status.FeedID, status.ServiceKey))
		httpx.OK(c, status)
	})
	group.GET("/api-tokens", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		items, err := svc.ListPluginAPITokens(c.Request.Context(), c.Query("plugin_id"))
		if err != nil {
			writePluginAPITokenError(c, err)
			return
		}
		filtered := make([]plugins.PluginAPIToken, 0, len(items))
		for _, item := range items {
			if pluginTokenHasSurface(item, surface) {
				filtered = append(filtered, item)
			}
		}
		httpx.OK(c, filtered)
	})
	group.POST("/api-tokens", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		var req plugins.PluginAPITokenCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1770, "invalid plugin API token payload")
			return
		}
		req.Surfaces = []string{surface}
		result, err := svc.CreatePluginAPIToken(c.Request.Context(), req)
		if err != nil {
			writePluginAPITokenError(c, err)
			return
		}
		_ = recordPluginEvent(c, control, "api_token_create", result.Token.ID, fmt.Sprintf("Created plugin API token %s", result.Token.Name))
		httpx.OK(c, result)
	})
	group.DELETE("/api-tokens/:token_id", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		items, err := svc.ListPluginAPITokens(c.Request.Context(), "")
		if err != nil {
			writePluginAPITokenError(c, err)
			return
		}
		allowed := false
		for _, item := range items {
			if item.ID == c.Param("token_id") && pluginTokenHasSurface(item, surface) {
				allowed = true
				break
			}
		}
		if !allowed {
			writePluginAPITokenError(c, plugins.ErrPluginAPITokenNotFound)
			return
		}
		token, err := svc.RevokePluginAPIToken(c.Request.Context(), c.Param("token_id"))
		if err != nil {
			writePluginAPITokenError(c, err)
			return
		}
		_ = recordPluginEvent(c, control, "api_token_revoke", token.ID, fmt.Sprintf("Revoked plugin API token %s", token.Name))
		httpx.OK(c, token)
	})
	group.POST("/license/activate", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		var req plugins.LicenseActivateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1745, "invalid license activation payload")
			return
		}
		status, err := svc.ActivateLicense(c.Request.Context(), req)
		if err != nil {
			writeLicenseError(c, err)
			return
		}
		_ = recordPluginEvent(c, control, "license_activate", status.LicenseID, fmt.Sprintf("Activated license %s", status.LicenseID))
		httpx.OK(c, status)
	})
	group.POST("/license/redeem", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		var req plugins.LicenseRedeemRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1747, "invalid license redeem payload")
			return
		}
		status, err := svc.RedeemLicense(c.Request.Context(), req)
		if err != nil {
			writeLicenseError(c, err)
			return
		}
		_ = recordPluginEvent(c, control, "license_redeem", status.LicenseID, fmt.Sprintf("Redeemed license %s", status.LicenseID))
		httpx.OK(c, status)
	})
	group.POST("/license/import", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		var req plugins.LicenseImportRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1746, "invalid license import payload")
			return
		}
		status, err := svc.ImportLicense(c.Request.Context(), req)
		if err != nil {
			writeLicenseError(c, err)
			return
		}
		_ = recordPluginEvent(c, control, "license_import", status.LicenseID, fmt.Sprintf("Imported license %s", status.LicenseID))
		httpx.OK(c, status)
	})
	group.POST("/catalog-sync", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		status, err := svc.SyncOfficialCatalog(c.Request.Context())
		if err != nil {
			writeCatalogSyncError(c, err, status)
			return
		}
		_ = recordPluginEvent(c, control, "catalog_sync", "official_catalog", fmt.Sprintf("Synced official catalog version %d", status.CatalogVersion))
		httpx.OK(c, status)
	})
	group.GET("/:id/runtime/status", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		status, err := svc.SidecarRuntimeStatus(c.Request.Context(), c.Param("id"))
		if err != nil {
			writeRuntimeError(c, err)
			return
		}
		httpx.OK(c, status)
	})
	group.Any("/:id/runtime/proxy/*proxy_path", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		response, err := svc.ProxySidecarHTTP(c.Request.Context(), c.Param("id"), c.Param("proxy_path"), c.Request)
		if err != nil {
			writeRuntimeError(c, err)
			return
		}
		defer response.Body.Close()
		copyProxyResponseHeaders(c.Writer.Header(), response.Header)
		c.Status(response.StatusCode)
		_, _ = io.Copy(c.Writer, response.Body)
	})
	group.GET("/:id/frontend/contribution", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		raw, err := svc.PluginFrontendContribution(c.Request.Context(), c.Param("id"))
		if err != nil {
			writeFrontendError(c, err)
			return
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", raw)
	})
	group.GET("/:id/frontend/assets/*asset_path", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		assetPath, err := svc.PluginFrontendAssetPath(c.Request.Context(), c.Param("id"), c.Param("asset_path"))
		if err != nil {
			writeFrontendError(c, err)
			return
		}
		c.File(assetPath)
	})
	group.POST("/:id/enable", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		plugin, err := svc.Enable(c.Request.Context(), c.Param("id"))
		if err != nil {
			writePluginError(c, err)
			return
		}
		_ = recordPluginEvent(c, control, "enable", plugin.ID, fmt.Sprintf("Enabled plugin %s", plugin.Name))
		httpx.OK(c, plugin)
	})
	group.POST("/:id/disable", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		plugin, err := svc.Disable(c.Request.Context(), c.Param("id"))
		if err != nil {
			writePluginError(c, err)
			return
		}
		_ = recordPluginEvent(c, control, "disable", plugin.ID, fmt.Sprintf("Disabled plugin %s", plugin.Name))
		httpx.OK(c, plugin)
	})
	group.GET("/:id/config", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		config, err := svc.Config(c.Request.Context(), c.Param("id"))
		if err != nil {
			writePluginError(c, err)
			return
		}
		httpx.OK(c, config)
	})
	group.PUT("/:id/config", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		var req plugins.ConfigRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1710, "invalid plugin config payload")
			return
		}
		config, err := svc.UpdateConfig(c.Request.Context(), c.Param("id"), req)
		if err != nil {
			writePluginError(c, err)
			return
		}
		_ = recordPluginEvent(c, control, "configure", config.PluginID, fmt.Sprintf("Updated plugin config %s", config.PluginID))
		httpx.OK(c, config)
	})
	group.GET("/:id/packages", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		packages, err := svc.Packages(c.Request.Context(), c.Param("id"))
		if err != nil {
			writePackageError(c, err)
			return
		}
		httpx.OK(c, packages)
	})
	group.POST("/:id/packages/:package_id/download", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		var req plugins.PackageDownloadRequest
		if c.Request.ContentLength != 0 {
			if err := c.ShouldBindJSON(&req); err != nil {
				httpx.Error(c, http.StatusBadRequest, 1735, "invalid package download payload")
				return
			}
		}
		result, err := svc.DownloadPackage(c.Request.Context(), c.Param("id"), c.Param("package_id"), req)
		if err != nil {
			writePackageError(c, err)
			return
		}
		_ = recordPluginEvent(c, control, "package_download", result.Package.PluginID, fmt.Sprintf("Downloaded plugin package %s", result.Package.PackageID))
		httpx.OK(c, result)
	})
	group.POST("/:id/packages/:package_id/install", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		result, err := svc.InstallPackage(c.Request.Context(), c.Param("id"), c.Param("package_id"))
		if err != nil {
			writePackageError(c, err)
			return
		}
		_ = recordPluginEvent(c, control, "package_install", result.PluginID, fmt.Sprintf("Installed plugin package %s", result.PackageID))
		httpx.OK(c, result)
	})
	group.POST("/:id/packages/:package_id/import", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		var req plugins.PackageImportRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1736, "invalid package import payload")
			return
		}
		result, err := svc.ImportPackage(c.Request.Context(), c.Param("id"), c.Param("package_id"), req)
		if err != nil {
			writePackageError(c, err)
			return
		}
		_ = recordPluginEvent(c, control, "package_import", result.Package.PluginID, fmt.Sprintf("Imported plugin package %s", result.Package.PackageID))
		httpx.OK(c, result)
	})
	group.POST("/:id/packages/:package_id/uninstall", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		result, err := svc.UninstallPackage(c.Request.Context(), c.Param("id"), c.Param("package_id"))
		if err != nil {
			writePackageError(c, err)
			return
		}
		_ = recordPluginEvent(c, control, "package_uninstall", result.PluginID, fmt.Sprintf("Uninstalled plugin package %s", result.PackageID))
		httpx.OK(c, result)
	})
	group.GET("/:id/deliveries", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		data, err := svc.DeliveryAttempts(c.Request.Context(), plugins.DeliveryQuery{
			PluginID: c.Param("id"),
			AlertID:  c.Query("alert_id"),
			Status:   c.Query("status"),
			Limit:    intQuery(c, "limit", 50),
			Offset:   intQuery(c, "offset", 0),
		})
		if err != nil {
			writePluginError(c, err)
			return
		}
		httpx.OK(c, data)
	})
}

func pluginTokenHasSurface(token plugins.PluginAPIToken, surface string) bool {
	for _, item := range token.Surfaces {
		if item == surface {
			return true
		}
	}
	return false
}

func writePluginError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, plugins.ErrPluginNotFound):
		httpx.Error(c, http.StatusNotFound, 1704, err.Error())
	case errors.Is(err, plugins.ErrPluginLocked), errors.Is(err, plugins.ErrPluginCoreRequired):
		httpx.Error(c, http.StatusConflict, 1709, err.Error())
	case errors.Is(err, plugins.ErrPluginNotConfigurable), errors.Is(err, plugins.ErrPluginConfigInvalid):
		httpx.Error(c, http.StatusBadRequest, 1711, err.Error())
	default:
		httpx.Error(c, http.StatusInternalServerError, 1701, err.Error())
	}
}

func writeCatalogSyncError(c *gin.Context, err error, status plugins.OfficialCatalogStatus) {
	switch {
	case errors.Is(err, plugins.ErrCatalogSyncDisabled):
		httpx.Error(c, http.StatusConflict, 1721, err.Error())
	case errors.Is(err, plugins.ErrCatalogNotConfigured):
		httpx.Error(c, http.StatusConflict, 1722, err.Error())
	case errors.Is(err, plugins.ErrCatalogSignature):
		httpx.Error(c, http.StatusForbidden, 1723, err.Error())
	default:
		if status.Error != "" {
			httpx.Error(c, http.StatusBadGateway, 1724, status.Error)
			return
		}
		httpx.Error(c, http.StatusBadGateway, 1724, err.Error())
	}
}

func writePackageError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, plugins.ErrPluginNotFound), errors.Is(err, plugins.ErrPackageNotFound):
		httpx.Error(c, http.StatusNotFound, 1730, err.Error())
	case errors.Is(err, plugins.ErrPackageNotInstalled):
		httpx.Error(c, http.StatusNotFound, 1730, err.Error())
	case errors.Is(err, plugins.ErrPluginLocked), errors.Is(err, plugins.ErrPackageIncompatible), errors.Is(err, plugins.ErrPackageRevoked), errors.Is(err, plugins.ErrPackageNotCached), errors.Is(err, plugins.ErrPackageImport), errors.Is(err, plugins.ErrCatalogSyncDisabled), errors.Is(err, plugins.ErrCatalogNotConfigured):
		httpx.Error(c, http.StatusConflict, 1731, err.Error())
	case errors.Is(err, plugins.ErrPackageSignature):
		httpx.Error(c, http.StatusForbidden, 1733, err.Error())
	default:
		httpx.Error(c, http.StatusBadGateway, 1734, err.Error())
	}
}

func writeRuntimeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, plugins.ErrPluginNotFound):
		httpx.Error(c, http.StatusNotFound, 1751, err.Error())
	case errors.Is(err, plugins.ErrPackageNotInstalled):
		httpx.Error(c, http.StatusConflict, 1752, err.Error())
	case errors.Is(err, plugins.ErrPluginDisabled), errors.Is(err, plugins.ErrPluginLocked):
		httpx.Error(c, http.StatusConflict, 1753, err.Error())
	case errors.Is(err, plugins.ErrPluginRuntimeUnavailable):
		httpx.Error(c, http.StatusBadGateway, 1754, err.Error())
	default:
		httpx.Error(c, http.StatusInternalServerError, 1755, err.Error())
	}
}

func writeFrontendError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, plugins.ErrPackageNotInstalled), errors.Is(err, plugins.ErrPluginFrontendNotFound):
		httpx.Error(c, http.StatusNotFound, 1760, err.Error())
	default:
		httpx.Error(c, http.StatusInternalServerError, 1761, err.Error())
	}
}

func copyProxyResponseHeaders(target http.Header, source http.Header) {
	for key, values := range source {
		if shouldDropProxyHeader(key) {
			continue
		}
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

func shouldDropProxyHeader(key string) bool {
	switch http.CanonicalHeaderKey(key) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	default:
		return false
	}
}

func writeLicenseError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, plugins.ErrLicenseNotFound):
		httpx.Error(c, http.StatusNotFound, 1740, err.Error())
	case errors.Is(err, plugins.ErrLicenseNotConfigured), errors.Is(err, plugins.ErrLicenseInvalid), errors.Is(err, plugins.ErrLicenseActivation), errors.Is(err, plugins.ErrLicenseRedeem), errors.Is(err, plugins.ErrLicenseBinding):
		httpx.Error(c, http.StatusConflict, 1741, err.Error())
	case errors.Is(err, plugins.ErrLicenseSignature):
		httpx.Error(c, http.StatusForbidden, 1743, err.Error())
	default:
		httpx.Error(c, http.StatusBadGateway, 1744, err.Error())
	}
}

func writePluginAPITokenError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, plugins.ErrPluginAPITokenNotFound), errors.Is(err, plugins.ErrPluginNotFound):
		httpx.Error(c, http.StatusNotFound, 1771, err.Error())
	case errors.Is(err, plugins.ErrPluginAPITokenInvalid):
		httpx.Error(c, http.StatusBadRequest, 1772, err.Error())
	case errors.Is(err, plugins.ErrPluginAPITokenExpired):
		httpx.Error(c, http.StatusUnauthorized, 1773, err.Error())
	case errors.Is(err, plugins.ErrPluginAPITokenScope), errors.Is(err, plugins.ErrPluginSurface):
		httpx.Error(c, http.StatusForbidden, 1774, err.Error())
	default:
		httpx.Error(c, http.StatusInternalServerError, 1775, err.Error())
	}
}

func writeOfficialFeedError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, plugins.ErrLicenseNotFound), errors.Is(err, plugins.ErrOfficialFeedNotFound):
		httpx.Error(c, http.StatusNotFound, 1781, err.Error())
	case errors.Is(err, plugins.ErrOfficialFeedEntitlement), errors.Is(err, plugins.ErrOfficialFeedBinding):
		httpx.Error(c, http.StatusForbidden, 1782, err.Error())
	case errors.Is(err, plugins.ErrCatalogSignature):
		httpx.Error(c, http.StatusForbidden, 1783, err.Error())
	case errors.Is(err, plugins.ErrOfficialFeedInvalid), errors.Is(err, plugins.ErrOfficialFeedDecrypt), errors.Is(err, plugins.ErrOfficialFeedReplay), errors.Is(err, plugins.ErrOfficialFeedExpired), errors.Is(err, plugins.ErrCatalogNotConfigured):
		httpx.Error(c, http.StatusConflict, 1784, err.Error())
	case errors.Is(err, plugins.ErrOfficialFeedSyncDisabled), errors.Is(err, plugins.ErrOfficialFeedSyncMode):
		httpx.Error(c, http.StatusConflict, 1786, err.Error())
	case errors.Is(err, plugins.ErrOfficialFeedRemote):
		httpx.Error(c, http.StatusBadGateway, 1787, err.Error())
	default:
		httpx.Error(c, http.StatusInternalServerError, 1785, err.Error())
	}
}

func recordPluginEvent(c *gin.Context, control *controlplane.Service, action string, pluginID string, summary string) error {
	if control == nil {
		return nil
	}
	return control.RecordPluginEvent(c.Request.Context(), actor(c), action, pluginID, summary)
}
