package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func registerSupplyRoutes(group *gin.RouterGroup, control *controlplane.Service) {
	if control == nil {
		return
	}
	group.GET("/utilization", func(c *gin.Context) {
		query, err := supplyUtilizationQuery(c)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1521, err.Error())
			return
		}
		query.APIKeyIDs, err = departmentAPIKeyIDs(c.Request.Context(), control, principalAccess(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1114, err.Error())
			return
		}
		data, err := control.SupplyUtilization(c.Request.Context(), query)
		if err != nil {
			writeSupplyError(c, err)
			return
		}
		httpx.OK(c, data)
	})
	group.GET("/recommendations", func(c *gin.Context) {
		query, err := supplyUtilizationQuery(c)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1521, err.Error())
			return
		}
		query.APIKeyIDs, err = departmentAPIKeyIDs(c.Request.Context(), control, principalAccess(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1114, err.Error())
			return
		}
		data, err := control.CapacityRecommendations(c.Request.Context(), query)
		if err != nil {
			writeSupplyError(c, err)
			return
		}
		httpx.OK(c, data)
	})
}

func supplyUtilizationQuery(c *gin.Context) (controlplane.SupplyUtilizationQuery, error) {
	from, err := strictSupplyTimeQuery(c, "from")
	if err != nil {
		return controlplane.SupplyUtilizationQuery{}, err
	}
	to, err := strictSupplyTimeQuery(c, "to")
	if err != nil {
		return controlplane.SupplyUtilizationQuery{}, err
	}
	windowHoursValue := strings.TrimSpace(c.Query("window_hours"))
	if windowHoursValue != "" {
		if !from.IsZero() || !to.IsZero() {
			return controlplane.SupplyUtilizationQuery{}, errors.New("window_hours cannot be combined with from or to")
		}
		windowHours, parseErr := strconv.Atoi(windowHoursValue)
		if parseErr != nil || windowHours <= 0 || windowHours > 31*24 {
			return controlplane.SupplyUtilizationQuery{}, controlplane.ErrSupplyWindowInvalid
		}
		to = time.Now().UTC()
		from = to.Add(-time.Duration(windowHours) * time.Hour)
	}
	return controlplane.SupplyUtilizationQuery{From: from, To: to}, nil
}

func strictSupplyTimeQuery(c *gin.Context, key string) (time.Time, error) {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, errors.New("invalid " + key + " time")
}

func writeSupplyError(c *gin.Context, err error) {
	if errors.Is(err, controlplane.ErrSupplyWindowInvalid) {
		httpx.Error(c, http.StatusBadRequest, 1521, err.Error())
		return
	}
	httpx.Error(c, http.StatusInternalServerError, 1114, err.Error())
}
