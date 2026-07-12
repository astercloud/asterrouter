package server

import (
	"testing"
	"time"
)

func TestAuthAttemptLimiterBlocksAndResets(t *testing.T) {
	l := newAuthAttemptLimiter(2, time.Minute)
	now := time.Now()
	if !l.Allow("ip", now) || !l.Allow("ip", now) || l.Allow("ip", now) {
		t.Fatal("limiter did not enforce threshold")
	}
	l.Reset("ip")
	if !l.Allow("ip", now) {
		t.Fatal("reset did not clear attempts")
	}
	if !l.Allow("other", now) {
		t.Fatal("keys must be isolated")
	}
}
