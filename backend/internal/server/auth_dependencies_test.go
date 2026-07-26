package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type blockingAuthenticationEmailSender struct {
	started  chan context.Context
	release  chan struct{}
	finished chan struct{}
}

type failingAuthenticationEmailSender struct{ err error }

func (s failingAuthenticationEmailSender) Send(context.Context, string, string, string, string) error {
	return s.err
}

func (s *blockingAuthenticationEmailSender) Send(ctx context.Context, _, _, _, _ string) error {
	s.started <- ctx
	<-s.release
	close(s.finished)
	return nil
}

func TestAsyncAuthenticationEmailSenderIsBoundedAndDetachedFromRequest(t *testing.T) {
	delegate := &blockingAuthenticationEmailSender{
		started:  make(chan context.Context, 1),
		release:  make(chan struct{}),
		finished: make(chan struct{}),
	}
	sender := newAsyncAuthenticationEmailSender(delegate, 1)
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	if err := sender.Send(requestCtx, "email_verification", "user@example.test", "User", "https://example.test/verify"); err != nil {
		t.Fatal(err)
	}
	cancelRequest()

	var deliveryCtx context.Context
	select {
	case deliveryCtx = <-delegate.started:
	case <-time.After(time.Second):
		t.Fatal("authentication email delivery was not started")
	}
	select {
	case <-deliveryCtx.Done():
		t.Fatal("authentication email inherited request cancellation")
	default:
	}
	if err := sender.Send(context.Background(), "password_reset", "user@example.test", "User", "https://example.test/reset"); !errors.Is(err, errAuthenticationEmailDispatcherBusy) {
		t.Fatalf("saturated dispatcher error = %v", err)
	}
	close(delegate.release)
	select {
	case <-delegate.finished:
	case <-time.After(time.Second):
		t.Fatal("authentication email delivery did not finish")
	}
}

func TestAsyncAuthenticationEmailSenderRunsFailureCallback(t *testing.T) {
	deliveryErr := errors.New("synthetic SMTP failure")
	sender := newAsyncAuthenticationEmailSender(failingAuthenticationEmailSender{err: deliveryErr}, 1)
	aware, ok := sender.(authenticationEmailFailureAwareSender)
	if !ok {
		t.Fatal("async sender does not expose delivery failure callbacks")
	}
	rolledBack := make(chan struct{})
	if err := aware.SendWithFailureCallback(context.Background(), "email_verification", "user@example.test", "User", "https://example.test/verify", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		close(rolledBack)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rolledBack:
	case <-time.After(time.Second):
		t.Fatal("delivery failure callback was not invoked")
	}
}

func TestExternalMFAChallengeRedirectUsesProtectedCookie(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback", nil)
	expiresAt := time.Now().UTC().Add(5 * time.Minute)

	redirectExternalMFAChallenge(ctx, "sensitive-mfa-challenge", expiresAt)

	if location := recorder.Header().Get("Location"); location != "/login?mfa=required" {
		t.Fatalf("redirect location = %q", location)
	}
	if strings.Contains(recorder.Header().Get("Location"), "sensitive-mfa-challenge") {
		t.Fatal("MFA challenge leaked into redirect URL")
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", recorder.Header().Get("Cache-Control"))
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != externalMFAChallengeCookie || cookie.Value != "sensitive-mfa-challenge" {
		t.Fatalf("challenge cookie = %#v", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != externalMFAChallengePath {
		t.Fatalf("challenge cookie attributes = %#v", cookie)
	}
	if cookie.MaxAge <= 0 || cookie.Expires.Before(expiresAt.Add(-time.Second)) {
		t.Fatalf("challenge cookie expiry = %#v", cookie)
	}
}
