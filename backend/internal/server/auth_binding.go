package server

import (
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"
)

type authBindingTransaction struct {
	UserID     string
	Provider   string
	ReturnPath string
	CreatedAt  time.Time
}

type authBindingStore struct {
	mu           sync.Mutex
	transactions map[string]authBindingTransaction
	ttl          time.Duration
}

func newAuthBindingStore() *authBindingStore {
	return &authBindingStore{transactions: map[string]authBindingTransaction{}, ttl: 10 * time.Minute}
}

func (s *authBindingStore) Save(state, userID, provider, returnPath string, now time.Time) error {
	state, userID, provider = strings.TrimSpace(state), strings.TrimSpace(userID), strings.TrimSpace(provider)
	if state == "" || userID == "" || provider == "" {
		return errors.New("invalid authentication binding transaction")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transactions[state] = authBindingTransaction{UserID: userID, Provider: provider, ReturnPath: safeAccountReturnPath(returnPath), CreatedAt: now.UTC()}
	s.pruneLocked(now.UTC())
	return nil
}

func safeAccountReturnPath(value string) string {
	switch strings.TrimSpace(value) {
	case "/admin/account", "/console/account", "/operator/account", "/platform/account", "/portal/account", "/customer/account":
		return strings.TrimSpace(value)
	default:
		return "/admin/account"
	}
}

func authBindingRedirect(transaction authBindingTransaction, status, provider, message string) string {
	query := url.Values{"binding": []string{status}}
	if provider != "" {
		query.Set("provider", provider)
	}
	if message != "" {
		query.Set("message", message)
	}
	return safeAccountReturnPath(transaction.ReturnPath) + "?" + query.Encode()
}

func (s *authBindingStore) Consume(state, provider string, now time.Time) (authBindingTransaction, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state = strings.TrimSpace(state)
	transaction, ok := s.transactions[state]
	delete(s.transactions, state)
	if !ok || transaction.Provider != strings.TrimSpace(provider) || now.UTC().Sub(transaction.CreatedAt) > s.ttl {
		return authBindingTransaction{}, false
	}
	return transaction, true
}

func (s *authBindingStore) pruneLocked(now time.Time) {
	for state, transaction := range s.transactions {
		if now.Sub(transaction.CreatedAt) > s.ttl {
			delete(s.transactions, state)
		}
	}
}
