package server

import (
	"sync"
	"time"
)

const authAttemptLimiterMaxKeys = 10000

type authAttemptWindow struct {
	Count   int
	ResetAt time.Time
}
type authAttemptLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	maxKeys  int
	nextGC   time.Time
	attempts map[string]authAttemptWindow
}

func newAuthAttemptLimiter(limit int, window time.Duration) *authAttemptLimiter {
	return &authAttemptLimiter{limit: limit, window: window, maxKeys: authAttemptLimiterMaxKeys, attempts: map[string]authAttemptWindow{}}
}

func (l *authAttemptLimiter) Allow(key string, now time.Time) bool {
	allowed, _ := l.AllowWithRetry(key, now)
	return allowed
}

func (l *authAttemptLimiter) AllowWithRetry(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, exists := l.attempts[key]
	if !exists && len(l.attempts) >= l.maxKeys {
		if l.nextGC.IsZero() || !now.Before(l.nextGC) {
			for currentKey, attempt := range l.attempts {
				if !now.Before(attempt.ResetAt) {
					delete(l.attempts, currentKey)
				}
			}
			gcInterval := min(l.window, time.Minute)
			if gcInterval <= 0 {
				gcInterval = time.Minute
			}
			l.nextGC = now.Add(gcInterval)
		}
		if len(l.attempts) >= l.maxKeys {
			return false, max(l.nextGC.Sub(now), time.Second)
		}
	}
	if v.ResetAt.IsZero() || !now.Before(v.ResetAt) {
		v = authAttemptWindow{ResetAt: now.Add(l.window)}
	}
	if v.Count >= l.limit {
		l.attempts[key] = v
		return false, max(v.ResetAt.Sub(now), time.Second)
	}
	v.Count++
	l.attempts[key] = v
	return true, 0
}

func (l *authAttemptLimiter) Reset(key string) { l.mu.Lock(); delete(l.attempts, key); l.mu.Unlock() }

// authEndpointLimiters 为每个认证入口维护独立的限流桶。
// 注册、找回密码、重发验证邮件都会触发写库或发信，额度比登录更紧；
// 各入口独立计数，避免一个入口被打满后连带锁死其他入口。
//
// 限流按客户端 IP 计数且保存在进程内：多副本部署时每个副本各自计数，
// 只作为兜底护栏，边界防护仍依赖入口层（网关/WAF）的全局限流。
type authEndpointLimiters struct {
	loginPrincipal        *authAttemptLimiter
	mfaChallengePrincipal *authAttemptLimiter
	register              *authAttemptLimiter
	registerPrincipal     *authAttemptLimiter
	forgotPassword        *authAttemptLimiter
	resetPassword         *authAttemptLimiter
	verifyEmail           *authAttemptLimiter
	resendVerification    *authAttemptLimiter
	externalLoginStart    *authAttemptLimiter
	accountBindingStart   *authAttemptLimiter
	totpLogin             *authAttemptLimiter
	totpManagement        *authAttemptLimiter
}

func newAuthEndpointLimiters() *authEndpointLimiters {
	return &authEndpointLimiters{
		loginPrincipal:        newAuthAttemptLimiter(10, 5*time.Minute),
		mfaChallengePrincipal: newAuthAttemptLimiter(10, 5*time.Minute),
		register:              newAuthAttemptLimiter(30, time.Minute),
		registerPrincipal:     newAuthAttemptLimiter(5, time.Minute),
		forgotPassword:        newAuthAttemptLimiter(5, time.Minute),
		resetPassword:         newAuthAttemptLimiter(10, time.Minute),
		verifyEmail:           newAuthAttemptLimiter(10, time.Minute),
		resendVerification:    newAuthAttemptLimiter(3, time.Minute),
		externalLoginStart:    newAuthAttemptLimiter(30, time.Minute),
		accountBindingStart:   newAuthAttemptLimiter(10, 10*time.Minute),
		totpLogin:             newAuthAttemptLimiter(10, time.Minute),
		totpManagement:        newAuthAttemptLimiter(5, 15*time.Minute),
	}
}
