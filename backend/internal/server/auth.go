package server

import (
	"net/http"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func requireAdminAuth(token string, authSvc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if authSvc != nil {
			provided := bearerToken(c)
			if provided == "" || provided == "oidc-cookie" {
				provided, _ = c.Cookie("asterrouter_session")
			}
			if provided == "" {
				provided = strings.TrimSpace(c.GetHeader("X-Admin-Token"))
			}
			principal, ok := authSvc.Verify(provided)
			if !ok {
				httpx.Error(c, http.StatusUnauthorized, 1401, "login required")
				c.Abort()
				return
			}
			c.Set("actor", principal.Subject)
			c.Set("role", principal.Role)
			c.Next()
			return
		}
		if token == "" {
			c.Next()
			return
		}
		authHeader := c.GetHeader("Authorization")
		provided := c.GetHeader("X-Admin-Token")
		if strings.HasPrefix(authHeader, "Bearer ") {
			provided = strings.TrimPrefix(authHeader, "Bearer ")
		}
		if provided != token {
			httpx.Error(c, http.StatusUnauthorized, 1401, "admin token required")
			c.Abort()
			return
		}
		c.Next()
	}
}

func role(c *gin.Context) string {
	if value, ok := c.Get("role"); ok {
		if roleValue, ok := value.(string); ok && strings.TrimSpace(roleValue) != "" {
			return roleValue
		}
	}
	return "super_admin"
}

func bearerToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}
	return ""
}

func actor(c *gin.Context) string {
	if value, ok := c.Get("actor"); ok {
		if actorValue, ok := value.(string); ok && strings.TrimSpace(actorValue) != "" {
			return actorValue
		}
	}
	if value := strings.TrimSpace(c.GetHeader("X-Actor")); value != "" {
		return value
	}
	return "local-admin"
}
