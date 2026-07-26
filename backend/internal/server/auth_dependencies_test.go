package server

import (
	"context"
	"errors"
	"testing"
	"time"
)

type blockingAuthenticationEmailSender struct {
	started  chan context.Context
	release  chan struct{}
	finished chan struct{}
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
