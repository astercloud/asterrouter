package server

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

const (
	cookieSessionTokenMarker = "oidc-cookie"
	sessionCookieName        = "asterrouter_session"
	csrfCookieName           = "asterrouter_csrf"
	csrfHeaderName           = "X-CSRF-Token"
	cookieSessionMode        = "cookie"
	externalOAuthStatePrefix = "asterrouter_oauth_state_"
	externalOAuthCookiePath  = "/api/v1/auth"
	externalOAuthStateTTL    = 10 * time.Minute
)

var errExternalOAuthStateMismatch = errors.New("external OAuth state is not bound to this browser")

func requireAdminAuth(token string, authSvc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if authSvc != nil {
			provided := requestSessionToken(c)
			if provided == "" {
				provided = strings.TrimSpace(c.GetHeader("X-Admin-Token"))
			}
			principal, err := authSvc.VerifyWithError(provided)
			if err != nil {
				if errors.Is(err, auth.ErrSessionStateUnavailable) {
					recordAuthenticationError(c, "verify_session", err)
					httpx.Error(c, http.StatusServiceUnavailable, 1401, "authentication service is unavailable")
					c.Abort()
					return
				}
				httpx.Error(c, http.StatusUnauthorized, 1401, "login required")
				c.Abort()
				return
			}
			if !verifyCookieSessionCSRF(c) {
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

func requestSessionToken(c *gin.Context) string {
	provided := bearerToken(c)
	if provided == "" || provided == cookieSessionTokenMarker {
		provided, _ = c.Cookie(sessionCookieName)
	}
	return provided
}

func cookieSessionRequested(mode string) bool {
	return strings.EqualFold(strings.TrimSpace(mode), cookieSessionMode)
}

func usesCookieSession(c *gin.Context) bool {
	provided := bearerToken(c)
	if provided != "" && provided != cookieSessionTokenMarker {
		return false
	}
	_, err := c.Cookie(sessionCookieName)
	return err == nil
}

func verifyCookieSessionCSRF(c *gin.Context) bool {
	if !usesCookieSession(c) || c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
		return true
	}
	if site := strings.ToLower(strings.TrimSpace(c.GetHeader("Sec-Fetch-Site"))); site == "cross-site" {
		httpx.Error(c, http.StatusForbidden, 1403, "CSRF verification failed")
		c.Abort()
		return false
	}
	if origin := strings.TrimSpace(c.GetHeader("Origin")); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.User != nil || !strings.EqualFold(parsed.Host, c.Request.Host) {
			httpx.Error(c, http.StatusForbidden, 1403, "CSRF verification failed")
			c.Abort()
			return false
		}
	}
	cookieToken, _ := c.Cookie(csrfCookieName)
	headerToken := strings.TrimSpace(c.GetHeader(csrfHeaderName))
	if cookieToken == "" || len(cookieToken) != len(headerToken) || subtle.ConstantTimeCompare([]byte(cookieToken), []byte(headerToken)) != 1 {
		httpx.Error(c, http.StatusForbidden, 1403, "CSRF verification failed")
		c.Abort()
		return false
	}
	return true
}

func secureAuthCookie(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return true
	}
	if c.Request.TLS != nil {
		return true
	}
	forwardedProto, _, _ := strings.Cut(c.GetHeader("X-Forwarded-Proto"), ",")
	if strings.EqualFold(strings.TrimSpace(forwardedProto), "https") {
		return true
	}

	host := strings.TrimSpace(c.Request.Host)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	} else {
		host = strings.Trim(host, "[]")
	}
	if strings.EqualFold(host, "localhost") {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

func setExternalOAuthStateCookie(c *gin.Context, provider, state string) {
	expiresAt := time.Now().UTC().Add(externalOAuthStateTTL)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     externalOAuthStateCookieName(provider),
		Value:    strings.TrimSpace(state),
		Path:     externalOAuthCookiePath,
		Expires:  expiresAt,
		MaxAge:   int(externalOAuthStateTTL / time.Second),
		HttpOnly: true,
		Secure:   secureAuthCookie(c),
		SameSite: http.SameSiteLaxMode,
	})
	c.Header("Cache-Control", "no-store")
}

func consumeExternalOAuthStateCookie(c *gin.Context, provider, state string) bool {
	expected, err := c.Cookie(externalOAuthStateCookieName(provider))
	clearExternalOAuthStateCookie(c, provider)
	state = strings.TrimSpace(state)
	expected = strings.TrimSpace(expected)
	return err == nil && state != "" && len(state) == len(expected) && subtle.ConstantTimeCompare([]byte(state), []byte(expected)) == 1
}

func clearExternalOAuthStateCookie(c *gin.Context, provider string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     externalOAuthStateCookieName(provider),
		Path:     externalOAuthCookiePath,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secureAuthCookie(c),
		SameSite: http.SameSiteLaxMode,
	})
	c.Header("Cache-Control", "no-store")
}

func externalOAuthStateCookieName(provider string) string {
	return externalOAuthStatePrefix + strings.ToLower(strings.TrimSpace(provider))
}

func setCookieSession(c *gin.Context, result auth.LoginResult) (auth.LoginResult, error) {
	csrfToken, err := auth.RandomToken(32)
	if err != nil {
		return auth.LoginResult{}, err
	}
	maxAge := max(1, int(time.Until(result.ExpiresAt).Seconds()))
	secure := secureAuthCookie(c)
	http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookieName, Value: result.AccessToken, Path: "/", Expires: result.ExpiresAt.UTC(), MaxAge: maxAge, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	http.SetCookie(c.Writer, &http.Cookie{Name: csrfCookieName, Value: csrfToken, Path: "/", Expires: result.ExpiresAt.UTC(), MaxAge: maxAge, Secure: secure, SameSite: http.SameSiteLaxMode})
	c.Header("Cache-Control", "no-store")
	result.AccessToken = cookieSessionTokenMarker
	return result, nil
}

func clearCookieSession(c *gin.Context) {
	expires := time.Unix(1, 0).UTC()
	secure := secureAuthCookie(c)
	http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookieName, Path: "/", Expires: expires, MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	http.SetCookie(c.Writer, &http.Cookie{Name: csrfCookieName, Path: "/", Expires: expires, MaxAge: -1, Secure: secure, SameSite: http.SameSiteLaxMode})
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

func signedContextToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(authHeader, "Aster-Context ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "Aster-Context "))
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
