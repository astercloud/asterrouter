package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTurnstileVerifierSendsExpectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("secret") != "secret" || r.Form.Get("response") != "token" || r.Form.Get("remoteip") != "1.2.3.4" {
			t.Fatalf("form = %v", r.Form)
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()
	if err := (TurnstileVerifier{Endpoint: server.URL}).Verify(context.Background(), "secret", "token", "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
}

func TestTurnstileVerifierFailsClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"error-codes":["invalid-input-response"]}`))
	}))
	defer server.Close()
	err := (TurnstileVerifier{Endpoint: server.URL}).Verify(context.Background(), "secret", "bad", "")
	if err == nil || !strings.Contains(err.Error(), "invalid-input-response") {
		t.Fatalf("error = %v", err)
	}
}
