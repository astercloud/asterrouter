package auth

import (
	"context"
	"testing"
)

func TestSMTPMailerRequiresConfiguration(t *testing.T) {
	if err := (SMTPMailer{}).Send(context.Background(), "user@example.test", "subject", "body"); err == nil {
		t.Fatal("unconfigured SMTP must fail")
	}
}
