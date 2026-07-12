package auth

import (
	"testing"
	"time"
)

func TestValidateTOTPWithRFCVector(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	now := time.Unix(59, 0)
	code := totpCode(secret, now.Unix()/30)
	if !ValidateTOTP(secret, code, now) || ValidateTOTP(secret, "000000", now) {
		t.Fatal("TOTP validation mismatch")
	}
}
