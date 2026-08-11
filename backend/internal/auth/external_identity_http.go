package auth

import (
	"net/http"
	"time"
)

const externalIdentityHTTPTimeout = 15 * time.Second

func newExternalIdentityHTTPClient() *http.Client {
	return &http.Client{Timeout: externalIdentityHTTPTimeout}
}
