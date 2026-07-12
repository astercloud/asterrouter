package server

import (
	"sync"
	"time"
)

type authAttemptWindow struct {
	Count   int
	ResetAt time.Time
}
type authAttemptLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	attempts map[string]authAttemptWindow
}

func newAuthAttemptLimiter(limit int, window time.Duration) *authAttemptLimiter {
	return &authAttemptLimiter{limit: limit, window: window, attempts: map[string]authAttemptWindow{}}
}

func (l *authAttemptLimiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	v := l.attempts[key]
	if v.ResetAt.IsZero() || !now.Before(v.ResetAt) {
		v = authAttemptWindow{ResetAt: now.Add(l.window)}
	}
	if v.Count >= l.limit {
		l.attempts[key] = v
		return false
	}
	v.Count++
	l.attempts[key] = v
	if len(l.attempts) > 10000 {
		for k, a := range l.attempts {
			if !now.Before(a.ResetAt) {
				delete(l.attempts, k)
			}
		}
	}
	return true
}

func (l *authAttemptLimiter) Reset(key string) { l.mu.Lock(); delete(l.attempts, key); l.mu.Unlock() }
