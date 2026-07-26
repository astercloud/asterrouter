package server

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/gin-gonic/gin"
)

const authRequestBodyLimit = 64 * 1024

type HumanVerifier interface {
	Verify(ctx context.Context, secret, response, remoteIP string) error
}

type AuthenticationEmailSender interface {
	Send(ctx context.Context, event, recipient, userName, actionURL string) error
}

type configuredAuthenticationEmailSender struct {
	settings *settings.Service
}

func (s configuredAuthenticationEmailSender) Send(ctx context.Context, event, recipient, userName, actionURL string) error {
	return sendConfiguredEmail(ctx, s.settings, event, recipient, userName, actionURL)
}

func bindAuthJSON(c *gin.Context, value any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, authRequestBodyLimit)
	return c.ShouldBindJSON(value)
}

func allowAuthRequest(c *gin.Context, limiter *authAttemptLimiter, message string) bool {
	return allowAuthRequestForKey(c, limiter, c.ClientIP(), message)
}

func allowAuthRequestForKey(c *gin.Context, limiter *authAttemptLimiter, key, message string) bool {
	allowed, retryAfter := limiter.AllowWithRetry(key, time.Now().UTC())
	if allowed {
		return true
	}
	seconds := int((retryAfter + time.Second - 1) / time.Second)
	c.Header("Retry-After", strconv.Itoa(seconds))
	httpx.Error(c, http.StatusTooManyRequests, 1429, message)
	return false
}

func defaultHumanVerifier(value HumanVerifier) HumanVerifier {
	if value != nil {
		return value
	}
	return auth.TurnstileVerifier{}
}

func defaultAuthenticationEmailSender(value AuthenticationEmailSender, service *settings.Service) AuthenticationEmailSender {
	if value != nil {
		return value
	}
	return configuredAuthenticationEmailSender{settings: service}
}
