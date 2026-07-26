package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/gin-gonic/gin"
)

const (
	authRequestBodyLimit     = 64 * 1024
	authEmailDispatchLimit   = 8
	authEmailDeliveryTimeout = 15 * time.Second
)

var errAuthenticationEmailDispatcherBusy = errors.New("authentication email dispatcher is busy")

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

type asyncAuthenticationEmailSender struct {
	delegate AuthenticationEmailSender
	slots    chan struct{}
}

func newAsyncAuthenticationEmailSender(delegate AuthenticationEmailSender, limit int) AuthenticationEmailSender {
	if limit < 1 {
		limit = authEmailDispatchLimit
	}
	return &asyncAuthenticationEmailSender{delegate: delegate, slots: make(chan struct{}, limit)}
}

func (s *asyncAuthenticationEmailSender) Send(_ context.Context, event, recipient, userName, actionURL string) error {
	if s == nil || s.delegate == nil {
		return errors.New("authentication email sender is not configured")
	}
	select {
	case s.slots <- struct{}{}:
		go s.deliver(event, recipient, userName, actionURL)
		return nil
	default:
		return errAuthenticationEmailDispatcherBusy
	}
}

func (s *asyncAuthenticationEmailSender) deliver(event, recipient, userName, actionURL string) {
	defer func() { <-s.slots }()
	deliveryCtx, cancel := context.WithTimeout(context.Background(), authEmailDeliveryTimeout)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("authentication email dispatcher panicked", "event", event, "error", fmt.Sprint(recovered))
		}
	}()
	if err := s.delegate.Send(deliveryCtx, event, recipient, userName, actionURL); err != nil {
		slog.Error("authentication email delivery failed", "event", event, "error", err)
	}
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
	return newAsyncAuthenticationEmailSender(configuredAuthenticationEmailSender{settings: service}, authEmailDispatchLimit)
}
