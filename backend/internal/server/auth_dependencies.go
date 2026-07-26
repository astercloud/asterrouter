package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/gin-gonic/gin"
)

const (
	authRequestBodyLimit       = 64 * 1024
	authEmailDispatchLimit     = 8
	authEmailDeliveryTimeout   = 15 * time.Second
	authEmailRollbackTimeout   = 5 * time.Second
	externalMFAChallengeCookie = "asterrouter_mfa_challenge"
	externalMFAChallengePath   = "/api/v1/auth/totp/login"
)

var errAuthenticationEmailDispatcherBusy = errors.New("authentication email dispatcher is busy")

type HumanVerifier interface {
	Verify(ctx context.Context, secret, response, remoteIP string) error
}

type AuthenticationEmailSender interface {
	Send(ctx context.Context, event, recipient, userName, actionURL string) error
}

type authenticationEmailFailureAwareSender interface {
	SendWithFailureCallback(ctx context.Context, event, recipient, userName, actionURL string, onFailure func(context.Context) error) error
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
	return s.SendWithFailureCallback(context.Background(), event, recipient, userName, actionURL, nil)
}

func (s *asyncAuthenticationEmailSender) SendWithFailureCallback(_ context.Context, event, recipient, userName, actionURL string, onFailure func(context.Context) error) error {
	if s == nil || s.delegate == nil {
		return errors.New("authentication email sender is not configured")
	}
	select {
	case s.slots <- struct{}{}:
		go s.deliver(event, recipient, userName, actionURL, onFailure)
		return nil
	default:
		return errAuthenticationEmailDispatcherBusy
	}
}

func (s *asyncAuthenticationEmailSender) deliver(event, recipient, userName, actionURL string, onFailure func(context.Context) error) {
	defer func() { <-s.slots }()
	deliveryCtx, cancel := context.WithTimeout(context.Background(), authEmailDeliveryTimeout)
	defer cancel()
	deliveryErr := func() (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("authentication email dispatcher panicked: %v", recovered)
			}
		}()
		return s.delegate.Send(deliveryCtx, event, recipient, userName, actionURL)
	}()
	if deliveryErr == nil {
		return
	}
	slog.Error("authentication email delivery failed", "event", event, "error", deliveryErr)
	if onFailure == nil {
		return
	}
	rollbackCtx, cancelRollback := context.WithTimeout(context.Background(), authEmailRollbackTimeout)
	defer cancelRollback()
	if err := onFailure(rollbackCtx); err != nil {
		slog.Error("authentication email token rollback failed", "event", event, "error", err)
	}
}

func sendAuthenticationEmail(sender AuthenticationEmailSender, ctx context.Context, event, recipient, userName, actionURL string, onFailure func(context.Context) error) error {
	if aware, ok := sender.(authenticationEmailFailureAwareSender); ok {
		return aware.SendWithFailureCallback(ctx, event, recipient, userName, actionURL, onFailure)
	}
	return sender.Send(ctx, event, recipient, userName, actionURL)
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

func redirectExternalLoginFailure(c *gin.Context, provider string, err error) {
	if err != nil {
		_ = c.Error(err)
	}
	query := url.Values{"external": {"error"}}
	if provider = strings.TrimSpace(provider); provider != "" {
		query.Set("provider", provider)
	}
	c.Redirect(http.StatusFound, "/login?"+query.Encode())
}

func recordAuthenticationError(c *gin.Context, operation string, err error) {
	if err == nil {
		return
	}
	_ = c.Error(err)
	fingerprint := sha256.Sum256([]byte(err.Error()))
	slog.Error("authentication operation failed", "operation", operation, "error_type", fmt.Sprintf("%T", err), "error_fingerprint", hex.EncodeToString(fingerprint[:8]))
}

func redirectExternalMFAChallenge(c *gin.Context, challenge string, expiresAt time.Time) {
	c.Header("Cache-Control", "no-store")
	setMFAChallengeCookie(c, challenge, expiresAt)
	c.Redirect(http.StatusFound, "/login?mfa=required")
}

func writeMFAChallenge(c *gin.Context, challenge string, expiresAt time.Time, cookieSession bool) {
	c.Header("Cache-Control", "no-store")
	data := gin.H{"mfa_required": true, "expires_at": expiresAt}
	if cookieSession {
		setMFAChallengeCookie(c, challenge, expiresAt)
	} else {
		data["challenge"] = challenge
	}
	httpx.OK(c, data)
}

func setMFAChallengeCookie(c *gin.Context, challenge string, expiresAt time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     externalMFAChallengeCookie,
		Value:    challenge,
		Path:     externalMFAChallengePath,
		Expires:  expiresAt.UTC(),
		MaxAge:   max(1, int(time.Until(expiresAt).Seconds())),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearExternalMFAChallengeCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     externalMFAChallengeCookie,
		Path:     externalMFAChallengePath,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
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
