package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func serveSPA(r *gin.Engine, dir string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	if _, err := os.Stat(filepath.Join(abs, "index.html")); err != nil {
		r.NoRoute(func(c *gin.Context) {
			httpx.Error(c, http.StatusNotFound, 1404, "frontend build not found")
		})
		return
	}
	r.Static("/assets", filepath.Join(abs, "assets"))
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/v1/") {
			httpx.Error(c, http.StatusNotFound, 1404, "not found")
			return
		}
		c.File(filepath.Join(abs, "index.html"))
	})
}
