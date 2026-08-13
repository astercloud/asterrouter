package auth

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"strings"
	"time"
)

const externalIdentityHTTPTimeout = 15 * time.Second

func newExternalIdentityHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if caFile := strings.TrimSpace(os.Getenv("SSL_CERT_FILE")); caFile != "" {
		if pem, err := os.ReadFile(caFile); err == nil {
			pool, _ := x509.SystemCertPool()
			if pool == nil {
				pool = x509.NewCertPool()
			}
			if pool.AppendCertsFromPEM(pem) {
				transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
			}
		}
	}
	return &http.Client{Timeout: externalIdentityHTTPTimeout, Transport: transport}
}
