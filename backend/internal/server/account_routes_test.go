package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestWorkspaceAccountProfileEndpoints(t *testing.T) {
	handler, control := newAuthTestRuntime(t)
	user, _, err := control.RegisterWorkspaceUser(t.Context(), "account@example.test", "current-password", "Account User", false)
	if err != nil {
		t.Fatalf("RegisterWorkspaceUser(): %v", err)
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"account@example.test","password":"current-password"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	var loginResp struct {
		Data auth.LoginResult `json:"data"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil || loginResp.Data.AccessToken == "" {
		t.Fatalf("login response = %s error=%v", loginRec.Body.String(), err)
	}
	token := loginResp.Data.AccessToken

	profileReq := httptest.NewRequest(http.MethodGet, "/api/v1/account/profile", nil)
	profileReq.Header.Set("Authorization", "Bearer "+token)
	profileRec := httptest.NewRecorder()
	handler.ServeHTTP(profileRec, profileReq)
	if profileRec.Code != http.StatusOK {
		t.Fatalf("profile status=%d body=%s", profileRec.Code, profileRec.Body.String())
	}
	var profileResp struct {
		Data accountProfileResponse `json:"data"`
	}
	if err := json.Unmarshal(profileRec.Body.Bytes(), &profileResp); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	if profileResp.Data.ID != user.ID || profileResp.Data.Email != user.Email || len(profileResp.Data.LoginMethods) != 6 {
		t.Fatalf("profile mismatch: %+v", profileResp.Data)
	}

	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/account/profile", bytes.NewBufferString(`{"display_name":"Updated Account","avatar_data_url":"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJ"}`))
	updateReq.Header.Set("Authorization", "Bearer "+token)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}

	passwordReq := httptest.NewRequest(http.MethodPut, "/api/v1/account/password", bytes.NewBufferString(`{"current_password":"current-password","new_password":"updated-password"}`))
	passwordReq.Header.Set("Authorization", "Bearer "+token)
	passwordReq.Header.Set("Content-Type", "application/json")
	passwordRec := httptest.NewRecorder()
	handler.ServeHTTP(passwordRec, passwordReq)
	if passwordRec.Code != http.StatusOK {
		t.Fatalf("password status=%d body=%s", passwordRec.Code, passwordRec.Body.String())
	}
}

func TestLogoutPersistentlyRevokesExistingBearerToken(t *testing.T) {
	handler, control := newAuthTestRuntime(t)
	_, _, err := control.RegisterWorkspaceUser(t.Context(), "logout@example.test", "current-password", "Logout User", false)
	if err != nil {
		t.Fatal(err)
	}
	login := func() string {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"logout@example.test","password":"current-password"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var response struct {
			Data auth.LoginResult `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.Data.AccessToken == "" {
			t.Fatalf("login status=%d body=%s err=%v", rec.Code, rec.Body.String(), err)
		}
		return response.Data.AccessToken
	}
	oldToken := login()
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+oldToken)
	logoutRec := httptest.NewRecorder()
	handler.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("logout status=%d body=%s", logoutRec.Code, logoutRec.Body.String())
	}
	oldReq := httptest.NewRequest(http.MethodGet, "/api/v1/account/profile", nil)
	oldReq.Header.Set("Authorization", "Bearer "+oldToken)
	oldRec := httptest.NewRecorder()
	handler.ServeHTTP(oldRec, oldReq)
	if oldRec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token status=%d body=%s", oldRec.Code, oldRec.Body.String())
	}
	newToken := login()
	newReq := httptest.NewRequest(http.MethodGet, "/api/v1/account/profile", nil)
	newReq.Header.Set("Authorization", "Bearer "+newToken)
	newRec := httptest.NewRecorder()
	handler.ServeHTTP(newRec, newReq)
	if newRec.Code != http.StatusOK {
		t.Fatalf("new token status=%d body=%s", newRec.Code, newRec.Body.String())
	}
}

func TestPasswordChangeRevokesExistingBearerToken(t *testing.T) {
	handler, control := newAuthTestRuntime(t)
	_, _, err := control.RegisterWorkspaceUser(t.Context(), "change@example.test", "current-password", "Change User", false)
	if err != nil {
		t.Fatal(err)
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"change@example.test","password":"current-password"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	var loginResp struct {
		Data auth.LoginResult `json:"data"`
	}
	_ = json.Unmarshal(loginRec.Body.Bytes(), &loginResp)
	token := loginResp.Data.AccessToken
	passwordReq := httptest.NewRequest(http.MethodPut, "/api/v1/account/password", bytes.NewBufferString(`{"current_password":"current-password","new_password":"updated-password"}`))
	passwordReq.Header.Set("Authorization", "Bearer "+token)
	passwordReq.Header.Set("Content-Type", "application/json")
	passwordRec := httptest.NewRecorder()
	handler.ServeHTTP(passwordRec, passwordReq)
	if passwordRec.Code != http.StatusOK {
		t.Fatalf("password status=%d body=%s", passwordRec.Code, passwordRec.Body.String())
	}
	profileReq := httptest.NewRequest(http.MethodGet, "/api/v1/account/profile", nil)
	profileReq.Header.Set("Authorization", "Bearer "+token)
	profileRec := httptest.NewRecorder()
	handler.ServeHTTP(profileRec, profileReq)
	if profileRec.Code != http.StatusUnauthorized {
		t.Fatalf("old token status=%d body=%s", profileRec.Code, profileRec.Body.String())
	}
}

func TestPasswordResetImmediatelyRevokesExistingBearerToken(t *testing.T) {
	handler, control := newAuthTestRuntime(t)
	user, _, err := control.RegisterWorkspaceUser(t.Context(), "reset-session@example.test", "current-password", "Reset Session User", false)
	if err != nil {
		t.Fatal(err)
	}
	login := func(password string) (string, int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"reset-session@example.test","password":"`+password+`"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var response struct {
			Data auth.LoginResult `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &response)
		return response.Data.AccessToken, rec.Code
	}

	oldToken, status := login("current-password")
	if status != http.StatusOK || oldToken == "" {
		t.Fatalf("initial login status=%d", status)
	}
	_, resetToken, err := control.BeginPasswordReset(t.Context(), user.Email)
	if err != nil {
		t.Fatal(err)
	}
	resetReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/reset-password", bytes.NewBufferString(`{"token":"`+resetToken+`","password":"updated-password"}`))
	resetReq.Header.Set("Content-Type", "application/json")
	resetRec := httptest.NewRecorder()
	handler.ServeHTTP(resetRec, resetReq)
	if resetRec.Code != http.StatusOK {
		t.Fatalf("reset status=%d body=%s", resetRec.Code, resetRec.Body.String())
	}
	profileReq := httptest.NewRequest(http.MethodGet, "/api/v1/account/profile", nil)
	profileReq.Header.Set("Authorization", "Bearer "+oldToken)
	profileRec := httptest.NewRecorder()
	handler.ServeHTTP(profileRec, profileReq)
	if profileRec.Code != http.StatusUnauthorized {
		t.Fatalf("old token status=%d body=%s", profileRec.Code, profileRec.Body.String())
	}
	if _, status := login("current-password"); status != http.StatusUnauthorized {
		t.Fatalf("old password login status=%d", status)
	}
	if newToken, status := login("updated-password"); status != http.StatusOK || newToken == "" {
		t.Fatalf("new password login status=%d", status)
	}
}

func TestLocalAdministratorLoginRequiresTOTP(t *testing.T) {
	handler, control := newAuthTestRuntime(t)
	setup, err := control.BeginTOTPSetup(t.Context(), "admin")
	if err != nil {
		t.Fatalf("BeginTOTPSetup(): %v", err)
	}
	code := auth.GenerateTOTPCode(setup.Secret, time.Now().UTC())
	if err := control.ConfirmTOTP(t.Context(), "admin", code); err != nil {
		t.Fatalf("ConfirmTOTP(): %v", err)
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var loginResp struct {
		Data struct {
			MFARequired bool   `json:"mfa_required"`
			Challenge   string `json:"challenge"`
		} `json:"data"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if !loginResp.Data.MFARequired || loginResp.Data.Challenge == "" {
		t.Fatalf("login did not require MFA: %s", loginRec.Body.String())
	}

	mfaReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/totp/login", bytes.NewBufferString(`{"challenge":"`+loginResp.Data.Challenge+`","code":"`+code+`"}`))
	mfaReq.Header.Set("Content-Type", "application/json")
	mfaRec := httptest.NewRecorder()
	handler.ServeHTTP(mfaRec, mfaReq)
	if mfaRec.Code != http.StatusOK {
		t.Fatalf("MFA login status=%d body=%s", mfaRec.Code, mfaRec.Body.String())
	}
}

func TestTOTPChangesImmediatelyRevokeExistingBearerTokens(t *testing.T) {
	handler, control := newAuthTestRuntime(t)
	_, _, err := control.RegisterWorkspaceUser(t.Context(), "totp-session@example.test", "current-password", "TOTP Session User", false)
	if err != nil {
		t.Fatal(err)
	}

	login := func(code string) string {
		t.Helper()
		loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"totp-session@example.test","password":"current-password"}`))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		handler.ServeHTTP(loginRec, loginReq)
		var loginResponse struct {
			Data struct {
				AccessToken string `json:"access_token"`
				MFARequired bool   `json:"mfa_required"`
				Challenge   string `json:"challenge"`
			} `json:"data"`
		}
		if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResponse); err != nil || loginRec.Code != http.StatusOK {
			t.Fatalf("login status=%d body=%s err=%v", loginRec.Code, loginRec.Body.String(), err)
		}
		if !loginResponse.Data.MFARequired {
			if loginResponse.Data.AccessToken == "" {
				t.Fatalf("login token missing: %s", loginRec.Body.String())
			}
			return loginResponse.Data.AccessToken
		}
		mfaReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/totp/login", bytes.NewBufferString(`{"challenge":"`+loginResponse.Data.Challenge+`","code":"`+code+`"}`))
		mfaReq.Header.Set("Content-Type", "application/json")
		mfaRec := httptest.NewRecorder()
		handler.ServeHTTP(mfaRec, mfaReq)
		var mfaResponse struct {
			Data auth.LoginResult `json:"data"`
		}
		if err := json.Unmarshal(mfaRec.Body.Bytes(), &mfaResponse); err != nil || mfaRec.Code != http.StatusOK || mfaResponse.Data.AccessToken == "" {
			t.Fatalf("MFA login status=%d body=%s err=%v", mfaRec.Code, mfaRec.Body.String(), err)
		}
		return mfaResponse.Data.AccessToken
	}
	assertRevoked := func(token string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/account/profile", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("old token status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	postWithToken := func(path, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	token := login("")
	setupRec := postWithToken("/api/v1/auth/totp/setup", `{}`, token)
	var setupResponse struct {
		Data controlplane.TOTPSetup `json:"data"`
	}
	if err := json.Unmarshal(setupRec.Body.Bytes(), &setupResponse); err != nil || setupRec.Code != http.StatusOK || setupResponse.Data.Secret == "" {
		t.Fatalf("setup status=%d body=%s err=%v", setupRec.Code, setupRec.Body.String(), err)
	}
	code := auth.GenerateTOTPCode(setupResponse.Data.Secret, time.Now().UTC())
	confirmRec := postWithToken("/api/v1/auth/totp/confirm", `{"code":"`+code+`"}`, token)
	if confirmRec.Code != http.StatusOK {
		t.Fatalf("confirm status=%d body=%s", confirmRec.Code, confirmRec.Body.String())
	}
	assertRevoked(token)

	token = login(code)
	recoveryRec := postWithToken("/api/v1/auth/totp/recovery-codes", `{}`, token)
	if recoveryRec.Code != http.StatusOK {
		t.Fatalf("recovery status=%d body=%s", recoveryRec.Code, recoveryRec.Body.String())
	}
	assertRevoked(token)

	token = login(code)
	disableRec := postWithToken("/api/v1/auth/totp/disable", `{"code":"`+code+`"}`, token)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", disableRec.Code, disableRec.Body.String())
	}
	assertRevoked(token)
	if finalToken := login(""); finalToken == "" {
		t.Fatal("password login did not recover after TOTP disable")
	}
}

func TestLocalAdministratorAccountIsFullyMutable(t *testing.T) {
	handler := newAuthTestHandler(t)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	var loginResp struct {
		Data auth.LoginResult `json:"data"`
	}
	_ = json.Unmarshal(loginRec.Body.Bytes(), &loginResp)

	profileReq := httptest.NewRequest(http.MethodGet, "/api/v1/account/profile", nil)
	profileReq.Header.Set("Authorization", "Bearer "+loginResp.Data.AccessToken)
	profileRec := httptest.NewRecorder()
	handler.ServeHTTP(profileRec, profileReq)
	var profileResp struct {
		Data controlplane.AccountProfile `json:"data"`
	}
	_ = json.Unmarshal(profileRec.Body.Bytes(), &profileResp)
	if profileRec.Code != http.StatusOK || profileResp.Data.ManagedByConfig || !profileResp.Data.PasswordEnabled {
		t.Fatalf("local admin profile status=%d body=%s", profileRec.Code, profileRec.Body.String())
	}

	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/account/profile", bytes.NewBufferString(`{"display_name":"Changed","avatar_data_url":""}`))
	updateReq.Header.Set("Authorization", "Bearer "+loginResp.Data.AccessToken)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("local admin update status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}

	passwordReq := httptest.NewRequest(http.MethodPut, "/api/v1/account/password", bytes.NewBufferString(`{"current_password":"secret","new_password":"updated-admin-password"}`))
	passwordReq.Header.Set("Authorization", "Bearer "+loginResp.Data.AccessToken)
	passwordReq.Header.Set("Content-Type", "application/json")
	passwordRec := httptest.NewRecorder()
	handler.ServeHTTP(passwordRec, passwordReq)
	if passwordRec.Code != http.StatusOK {
		t.Fatalf("local admin password status=%d body=%s", passwordRec.Code, passwordRec.Body.String())
	}

	oldLoginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"secret"}`))
	oldLoginReq.Header.Set("Content-Type", "application/json")
	oldLoginRec := httptest.NewRecorder()
	handler.ServeHTTP(oldLoginRec, oldLoginReq)
	if oldLoginRec.Code != http.StatusUnauthorized {
		t.Fatalf("old local admin password status=%d body=%s", oldLoginRec.Code, oldLoginRec.Body.String())
	}

	newLoginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"updated-admin-password"}`))
	newLoginReq.Header.Set("Content-Type", "application/json")
	newLoginRec := httptest.NewRecorder()
	handler.ServeHTTP(newLoginRec, newLoginReq)
	if newLoginRec.Code != http.StatusOK {
		t.Fatalf("new local admin password status=%d body=%s", newLoginRec.Code, newLoginRec.Body.String())
	}
}
