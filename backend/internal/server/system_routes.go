package server

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
	"github.com/gin-gonic/gin"
)

func registerSystemRoutes(group *gin.RouterGroup, svc *system.Service, settingsSvc *settings.Service, control *controlplane.Service) {
	group.GET("/version", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1600, "system update service is not available")
			return
		}
		info, err := svc.CheckUpdate(c.Request.Context(), false, updateChannel(c, settingsSvc))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1601, err.Error())
			return
		}
		httpx.OK(c, gin.H{
			"version":           info.CurrentVersion,
			"build_type":        info.BuildType,
			"restart_supported": info.RestartSupported,
			"platform":          info.Platform,
		})
	})
	group.GET("/check-updates", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1600, "system update service is not available")
			return
		}
		info, err := svc.CheckUpdate(c.Request.Context(), c.Query("force") == "true", updateChannel(c, settingsSvc))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1602, err.Error())
			return
		}
		_ = recordSystemEvent(c, control, "check_update", "version", fmt.Sprintf("Checked updates current=%s latest=%s has_update=%t", info.CurrentVersion, info.LatestVersion, info.HasUpdate))
		httpx.OK(c, info)
	})
	group.POST("/update", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1600, "system update service is not available")
			return
		}
		operationID := systemOperationID(c, "update")
		result, err := svc.PerformUpdate(c.Request.Context(), updateChannel(c, settingsSvc), operationID)
		if err != nil {
			_ = recordSystemEvent(c, control, "update_failed", operationID, err.Error())
			writeSystemOperationError(c, err, result)
			return
		}
		_ = recordSystemEvent(c, control, "update", operationID, result.Message)
		httpx.OK(c, result)
	})
	group.POST("/rollback", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1600, "system update service is not available")
			return
		}
		operationID := systemOperationID(c, "rollback")
		result, err := svc.Rollback(operationID)
		if err != nil {
			_ = recordSystemEvent(c, control, "rollback_failed", operationID, err.Error())
			writeSystemOperationError(c, err, result)
			return
		}
		_ = recordSystemEvent(c, control, "rollback", operationID, result.Message)
		httpx.OK(c, result)
	})
	group.POST("/restart", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1600, "system update service is not available")
			return
		}
		operationID := systemOperationID(c, "restart")
		result, err := svc.Restart(operationID, 500*time.Millisecond)
		if err != nil {
			_ = recordSystemEvent(c, control, "restart_rejected", operationID, err.Error())
			writeSystemOperationError(c, err, result)
			return
		}
		_ = recordSystemEvent(c, control, "restart", operationID, result.Message)
		httpx.OK(c, result)
	})
	group.GET("/backups", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1600, "system service is not available")
			return
		}
		data, err := svc.ListBackups(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1610, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	group.POST("/backups", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1600, "system service is not available")
			return
		}
		operationID := systemOperationID(c, "backup")
		data, err := svc.CreateBackup(c.Request.Context(), operationID)
		if err != nil {
			_ = recordSystemEvent(c, control, "backup_failed", operationID, err.Error())
			writeArchiveError(c, err)
			return
		}
		_ = recordSystemEvent(c, control, "backup", data.ID, fmt.Sprintf("Created backup %s", data.ID))
		httpx.OK(c, data)
	})
	group.GET("/backups/:id/download", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1600, "system service is not available")
			return
		}
		path, err := svc.BackupArchivePath(c.Param("id"))
		if err != nil {
			writeArchiveError(c, err)
			return
		}
		c.FileAttachment(path, filepath.Base(path))
	})
	group.POST("/backups/restore", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1600, "system service is not available")
			return
		}
		var request system.RestoreRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1611, "invalid restore request")
			return
		}
		operationID := systemOperationID(c, "restore")
		data, err := svc.RestoreBackup(c.Request.Context(), operationID, request)
		if err != nil {
			_ = recordSystemEvent(c, control, "restore_failed", operationID, err.Error())
			writeArchiveError(c, err)
			return
		}
		_ = recordSystemEvent(c, control, "restore", request.BackupID, fmt.Sprintf("Restored backup %s", request.BackupID))
		httpx.OK(c, data)
	})
	group.POST("/diagnostics", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1600, "system service is not available")
			return
		}
		operationID := systemOperationID(c, "diagnostic")
		data, err := svc.CreateDiagnosticBundle(c.Request.Context(), operationID, systemDiagnosticDetails(c, settingsSvc, control))
		if err != nil {
			_ = recordSystemEvent(c, control, "diagnostic_failed", operationID, err.Error())
			writeArchiveError(c, err)
			return
		}
		_ = recordSystemEvent(c, control, "diagnostic", data.ID, fmt.Sprintf("Created diagnostic bundle %s", data.ID))
		httpx.OK(c, data)
	})
	group.GET("/diagnostics/:id/download", func(c *gin.Context) {
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1600, "system service is not available")
			return
		}
		path, err := svc.DiagnosticArchivePath(c.Param("id"))
		if err != nil {
			writeArchiveError(c, err)
			return
		}
		c.FileAttachment(path, filepath.Base(path))
	})
}

func systemDiagnosticDetails(c *gin.Context, settingsSvc *settings.Service, control *controlplane.Service) map[string]any {
	details := map[string]any{}
	if settingsSvc != nil {
		if data, err := settingsSvc.Public(c.Request.Context()); err == nil {
			details["settings"] = map[string]any{
				"default_profile":     data.DefaultProfile,
				"enabled_profiles":    data.EnabledProfiles,
				"default_locale":      data.DefaultLocale,
				"enabled_locales":     data.EnabledLocales,
				"service_center_mode": data.ServiceCenterMode,
				"storage_mode":        data.StorageMode,
				"demo_mode":           data.DemoMode,
			}
			details["settings_health"] = "ok"
		} else {
			details["settings_health"] = "error"
		}
	}
	if control != nil {
		if err := control.Health(c.Request.Context()); err == nil {
			details["control_plane_health"] = "ok"
		} else {
			details["control_plane_health"] = "error"
		}
	}
	return details
}

func writeArchiveError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, system.ErrBackupConfirmation):
		httpx.Error(c, http.StatusConflict, 1612, err.Error())
	case errors.Is(err, system.ErrBackupNotFound):
		httpx.Error(c, http.StatusNotFound, 1613, err.Error())
	case errors.Is(err, system.ErrBackupToolMissing):
		httpx.Error(c, http.StatusConflict, 1614, err.Error())
	case errors.Is(err, system.ErrBackupDatabase):
		httpx.Error(c, http.StatusConflict, 1617, err.Error())
	case errors.Is(err, system.ErrBackupInvalid):
		httpx.Error(c, http.StatusBadRequest, 1615, err.Error())
	default:
		httpx.Error(c, http.StatusInternalServerError, 1616, err.Error())
	}
}

func updateChannel(c *gin.Context, settingsSvc *settings.Service) string {
	if settingsSvc == nil {
		return "stable"
	}
	data, err := settingsSvc.Admin(c.Request.Context())
	if err != nil {
		return "stable"
	}
	if data.UpdateChannel == "" {
		return "stable"
	}
	return data.UpdateChannel
}

func writeSystemOperationError(c *gin.Context, err error, result system.ApplyResult) {
	status := http.StatusInternalServerError
	code := 1603
	switch {
	case errors.Is(err, system.ErrUpdateNotConfigured),
		errors.Is(err, system.ErrUpdateUnsupported),
		errors.Is(err, system.ErrNoCompatibleAsset),
		errors.Is(err, system.ErrChecksumRequired),
		errors.Is(err, system.ErrUpdateSignature),
		errors.Is(err, system.ErrRestartUnsupported):
		status = http.StatusConflict
		code = 1604
	case errors.Is(err, system.ErrNoUpdateAvailable):
		status = http.StatusOK
		code = 0
	}
	if code == 0 {
		httpx.OK(c, result)
		return
	}
	message := err.Error()
	if result.ManualAction != "" {
		message += ": " + result.ManualAction
	}
	httpx.Error(c, status, code, message)
}

func systemOperationID(c *gin.Context, action string) string {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		key = strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return "sys_" + action + "_" + key
}

func recordSystemEvent(c *gin.Context, control *controlplane.Service, action string, resourceID string, summary string) error {
	if control == nil {
		return nil
	}
	return control.RecordSystemEvent(c.Request.Context(), actor(c), action, resourceID, summary)
}
