package server

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecureAuthCookie(t *testing.T) {
	if !secureAuthCookie(nil) {
		t.Fatal("secureAuthCookie(nil) must fail closed")
	}

	tests := []struct {
		name           string
		target         string
		host           string
		forwardedProto string
		want           bool
	}{
		{name: "public HTTP host", target: "http://router.example.test/login", want: true},
		{name: "public HTTPS host", target: "https://router.example.test/login", want: true},
		{name: "forwarded HTTPS", target: "http://router.example.test/login", forwardedProto: "https", want: true},
		{name: "forwarded HTTPS chain", target: "http://router.example.test/login", forwardedProto: "https, http", want: true},
		{name: "loopback IPv4 HTTP", target: "http://127.0.0.1:18080/login", want: false},
		{name: "loopback IPv6 HTTP", target: "http://[::1]:18080/login", want: false},
		{name: "localhost HTTP", target: "http://localhost:18080/login", want: false},
		{name: "loopback HTTPS", target: "https://127.0.0.1:18080/login", want: true},
		{name: "empty host fails closed", target: "http://router.example.test/login", host: " ", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", test.target, nil)
			if test.host != "" {
				request.Host = test.host
			}
			if test.forwardedProto != "" {
				request.Header.Set("X-Forwarded-Proto", test.forwardedProto)
			}
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = request
			if got := secureAuthCookie(context); got != test.want {
				t.Fatalf("secureAuthCookie() = %v, want %v", got, test.want)
			}
		})
	}
}
