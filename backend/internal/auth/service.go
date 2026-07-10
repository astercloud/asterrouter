package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrInvalidCredentials = errors.New("invalid username or password")

type Config struct {
	Username         string
	Password         string
	LegacyAdminToken string
	SecretKey        string
	TokenTTL         time.Duration
}

type Service struct {
	username         string
	password         string
	legacyAdminToken string
	secretKey        []byte
	tokenTTL         time.Duration
}

type Principal struct {
	Subject string `json:"sub"`
	Role    string `json:"role"`
	Expires int64  `json:"exp"`
}

type LoginResult struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
	User        User      `json:"user"`
}

type User struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

func NewService(cfg Config) *Service {
	username := strings.TrimSpace(cfg.Username)
	if username == "" {
		username = "admin"
	}
	password := cfg.Password
	if password == "" && strings.TrimSpace(cfg.LegacyAdminToken) != "" {
		password = strings.TrimSpace(cfg.LegacyAdminToken)
	}
	if password == "" {
		password = "admin"
	}
	ttl := cfg.TokenTTL
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	secret := cfg.SecretKey
	if secret == "" {
		secret = "asterrouter-local-development-secret"
	}
	return &Service{
		username:         username,
		password:         password,
		legacyAdminToken: strings.TrimSpace(cfg.LegacyAdminToken),
		secretKey:        []byte(secret),
		tokenTTL:         ttl,
	}
}

func (s *Service) Login(_ context.Context, username string, password string) (LoginResult, error) {
	if strings.TrimSpace(username) != s.username || !constantTimeEqual(password, s.password) {
		return LoginResult{}, ErrInvalidCredentials
	}
	expiresAt := time.Now().UTC().Add(s.tokenTTL)
	user := User{Username: s.username, Role: "super_admin"}
	token, err := s.sign(Principal{Subject: s.username, Role: user.Role, Expires: expiresAt.Unix()})
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresAt:   expiresAt,
		User:        user,
	}, nil
}

func (s *Service) Verify(token string) (Principal, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Principal{}, false
	}
	if s.legacyAdminToken != "" && constantTimeEqual(token, s.legacyAdminToken) {
		return Principal{Subject: s.username, Role: "super_admin", Expires: time.Now().UTC().Add(s.tokenTTL).Unix()}, true
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return Principal{}, false
	}
	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Principal{}, false
	}
	wantSig := s.signature(parts[0])
	if !constantTimeEqual(parts[1], wantSig) {
		return Principal{}, false
	}
	var principal Principal
	if err := json.Unmarshal(payloadRaw, &principal); err != nil {
		return Principal{}, false
	}
	if principal.Expires <= time.Now().UTC().Unix() {
		return Principal{}, false
	}
	return principal, true
}

func (s *Service) sign(principal Principal) (string, error) {
	payloadRaw, err := json.Marshal(principal)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadRaw)
	return payload + "." + s.signature(payload), nil
}

func (s *Service) signature(payload string) string {
	mac := hmac.New(sha256.New, s.secretKey)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func constantTimeEqual(a string, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}
