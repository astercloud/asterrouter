package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
	"github.com/gin-gonic/gin"
)

type Options struct {
	Runtime            RuntimeConfig
	AuthService        *auth.Service
	OIDCService        *auth.OIDCService
	FeishuService      *auth.FeishuService
	GitHubOAuthService *auth.SocialOAuthService
	GoogleOAuthService *auth.SocialOAuthService
	DingTalkService    *auth.DingTalkService
	SettingsService    *settings.Service
	ControlService     *controlplane.Service
	PluginService      *plugins.Service
	SystemService      *system.Service
	ExportJobStore     CSVExportJobStore
	DurableAIJobs      DurableAIJobAdmission
	AIJobRuntime       AIJobRuntimeStatusProvider
	HumanVerifier      HumanVerifier
	AuthEmailSender    AuthenticationEmailSender
	authBindingStore   *authBindingStore
}

type RuntimeConfig struct {
	AdminToken   string
	MetricsToken string
	DemoMode     bool
	FrontendDir  string
}

type AIJobRuntimeStatusProvider interface {
	Status() controlplane.DurableAIJobRuntimeStatus
}

func New(opts Options) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.ForwardedByClientIP = false
	_ = r.SetTrustedProxies(nil)
	if opts.SettingsService != nil {
		if current, err := opts.SettingsService.Admin(context.Background()); err == nil && current.TrustedProxyHeaders {
			if err := r.SetTrustedProxies(current.TrustedProxyCIDRs); err != nil {
				slog.Error("ignore invalid trusted proxy configuration", "error", err)
			} else {
				r.ForwardedByClientIP = true
				r.RemoteIPHeaders = []string{"X-Forwarded-For", "X-Real-IP"}
			}
		}
	}
	r.Use(func(c *gin.Context) {
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Next()
	})
	metrics := newServerMetrics()
	r.Use(metrics.middleware())
	r.Use(gin.Recovery())
	authLimiter := newAuthAttemptLimiter(60, 5*time.Minute)
	endpointLimiters := newAuthEndpointLimiters()
	humanVerifier := defaultHumanVerifier(opts.HumanVerifier)
	authEmailSender := defaultAuthenticationEmailSender(opts.AuthEmailSender, opts.SettingsService)
	if opts.authBindingStore == nil {
		opts.authBindingStore = newAuthBindingStore()
	}
	exportJobStore := opts.ExportJobStore
	if exportJobStore == nil {
		exportJobStore = newCSVExportJobStore()
	}
	if opts.ControlService != nil {
		if strings.TrimSpace(opts.Runtime.MetricsToken) != "" {
			opts.ControlService.SetCapacityAdmissionObserver(metrics)
			metrics.setProviderCapacitySnapshotSource(opts.ControlService.ProviderCapacityMetrics)
		}
		dispatchers := multiAlertDispatcher{}
		if opts.PluginService != nil {
			dispatchers = append(dispatchers, opts.PluginService)
		}
		if opts.SettingsService != nil {
			dispatchers = append(dispatchers, emailAlertDispatcher{control: opts.ControlService, settings: opts.SettingsService})
		}
		opts.ControlService.SetAlertDispatcher(dispatchers)
		if opts.AuthService != nil {
			opts.AuthService.SetSessionVersionResolver(func(subject string) (int64, bool, error) {
				if opts.Runtime.DemoMode && subject == "demo" {
					return 0, true, nil
				}
				return opts.ControlService.SessionVersion(context.Background(), subject)
			})
		}
	}
	if opts.ControlService != nil && opts.SettingsService != nil {
		if current, err := opts.SettingsService.Admin(context.Background()); err == nil && current.DataRetentionDays > 0 {
			_, _ = opts.ControlService.CleanupRetainedData(context.Background(), "system:startup", retentionCutoff(current.DataRetentionDays))
		}
	}

	r.GET("/health", func(c *gin.Context) {
		httpx.OK(c, gin.H{"status": "ok"})
	})
	r.GET("/metrics", requireMetricsToken(opts.Runtime.MetricsToken), metrics.handle)

	r.GET("/ready", func(c *gin.Context) {
		if opts.SettingsService == nil {
			metrics.recordReadiness(false)
			writeReadinessUnavailable(c, "settings", errors.New("settings service is not configured"))
			return
		}
		if err := opts.SettingsService.Health(c.Request.Context()); err != nil {
			metrics.recordReadiness(false)
			writeReadinessUnavailable(c, "settings", err)
			return
		}
		if opts.ControlService != nil {
			if err := opts.ControlService.Health(c.Request.Context()); err != nil {
				metrics.recordReadiness(false)
				writeReadinessUnavailable(c, "control_plane", err)
				return
			}
		}
		if opts.PluginService != nil {
			if err := opts.PluginService.Health(c.Request.Context()); err != nil {
				metrics.recordReadiness(false)
				writeReadinessUnavailable(c, "plugins", err)
				return
			}
		}
		if exportJobStore != nil {
			if err := exportJobStore.Health(c.Request.Context()); err != nil {
				metrics.recordReadiness(false)
				writeReadinessUnavailable(c, "export_jobs", err)
				return
			}
		}
		metrics.recordReadiness(true)
		httpx.OK(c, gin.H{"status": "ready"})
	})

	api := r.Group("/api/v1")
	api.GET("/settings/public", func(c *gin.Context) {
		data, err := opts.SettingsService.Public(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1002, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	api.GET("/legal/:slug", func(c *gin.Context) {
		public, err := opts.SettingsService.Public(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1001, err.Error())
			return
		}
		for _, document := range public.LegalDocuments {
			if document.Slug == c.Param("slug") {
				httpx.OK(c, document)
				return
			}
		}
		httpx.Error(c, http.StatusNotFound, 1404, "legal document not found")
	})
	api.GET("/i18n/locales", func(c *gin.Context) {
		httpx.OK(c, settings.SupportedLocales)
	})
	api.GET("/setup/status", func(c *gin.Context) {
		data, err := opts.SettingsService.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1003, err.Error())
			return
		}
		httpx.OK(c, gin.H{
			"setup_completed": data.SetupCompleted,
		})
	})
	api.POST("/setup", func(c *gin.Context) {
		var req struct {
			OrganizationName string `json:"organization_name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		data, err := opts.SettingsService.CompleteSetup(c.Request.Context(), req.OrganizationName)
		if err != nil {
			switch {
			case errors.Is(err, settings.ErrOrganizationNameRequired):
				httpx.Error(c, http.StatusBadRequest, 1401, err.Error())
			case errors.Is(err, settings.ErrSetupCompleted):
				httpx.Error(c, http.StatusConflict, 1401, err.Error())
			default:
				_ = c.Error(err)
				httpx.Error(c, http.StatusInternalServerError, 1401, "failed to complete setup")
			}
			return
		}
		httpx.OK(c, data.PublicSettings)
	})
	api.POST("/auth/login", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if !allowAuthRequest(c, authLimiter, "too many login attempts") {
			return
		}
		if opts.AuthService == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1300, "auth service is not available")
			return
		}
		var req struct {
			Username          string `json:"username"`
			Password          string `json:"password"`
			TurnstileToken    string `json:"turnstile_token"`
			AgreementAccepted bool   `json:"agreement_accepted"`
			SessionMode       string `json:"session_mode"`
		}
		if err := bindAuthJSON(c, &req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1301, "invalid login payload")
			return
		}
		loginPrincipalKey := strings.ToLower(strings.TrimSpace(req.Username))
		if loginPrincipalKey == "" {
			loginPrincipalKey = "<empty>"
		}
		if !allowAuthRequestForKey(c, endpointLimiters.loginPrincipal, loginPrincipalKey, "too many login attempts") {
			return
		}
		if !agreementAccepted(c.Request.Context(), opts.SettingsService, req.AgreementAccepted) {
			httpx.Error(c, http.StatusForbidden, 1328, "login agreement must be accepted")
			return
		}
		security, err := opts.SettingsService.LoginSecurity(c.Request.Context())
		if err != nil {
			_ = c.Error(err)
			httpx.Error(c, http.StatusServiceUnavailable, 1303, "authentication settings are unavailable")
			return
		}
		if security.TurnstileEnabled {
			if err := humanVerifier.Verify(c.Request.Context(), security.TurnstileSecret, req.TurnstileToken, c.ClientIP()); err != nil {
				httpx.Error(c, http.StatusForbidden, 1311, "turnstile verification failed")
				return
			}
		}
		result, err := opts.AuthService.Login(c.Request.Context(), req.Username, req.Password)
		isDemoPrincipal := opts.Runtime.DemoMode && strings.TrimSpace(req.Username) == "demo"
		if err == nil && opts.ControlService != nil && opts.AuthService.IsLocalPrincipal(req.Username) && !isDemoPrincipal {
			state, stateErr := opts.ControlService.CurrentAccountAuthenticationState(c.Request.Context(), req.Username)
			if stateErr != nil {
				recordAuthenticationError(c, "load_local_mfa_state", stateErr)
				httpx.Error(c, http.StatusInternalServerError, 1303, "authentication failed")
				return
			}
			if state.TOTPEnabled {
				if !allowAuthRequestForKey(c, endpointLimiters.mfaChallengePrincipal, state.UserID, "too many MFA challenges") {
					return
				}
				challenge, expires, challengeErr := opts.AuthService.BeginMFA(state.UserID, state.Role)
				if challengeErr != nil {
					_ = c.Error(challengeErr)
					httpx.Error(c, http.StatusInternalServerError, 1315, "MFA challenge could not be created")
					return
				}
				endpointLimiters.loginPrincipal.Reset(loginPrincipalKey)
				authLimiter.Reset(c.ClientIP())
				writeMFAChallenge(c, challenge, expires, cookieSessionRequested(req.SessionMode))
				return
			}
		}
		if err != nil && opts.ControlService != nil {
			policy, policyErr := opts.SettingsService.RegistrationPolicy(c.Request.Context())
			if policyErr != nil {
				recordAuthenticationError(c, "workspace_login_policy", policyErr)
				httpx.Error(c, http.StatusServiceUnavailable, 1303, "authentication settings are unavailable")
				return
			}
			user, userErr := opts.ControlService.AuthenticateWorkspaceUser(c.Request.Context(), req.Username, req.Password, policy.EmailVerification)
			if userErr != nil {
				if !errors.Is(userErr, controlplane.ErrInvalidWorkspaceCredentials) {
					recordAuthenticationError(c, "workspace_login", userErr)
					httpx.Error(c, http.StatusInternalServerError, 1303, "authentication failed")
					return
				}
			} else {
				if user.TOTPEnabled {
					if !allowAuthRequestForKey(c, endpointLimiters.mfaChallengePrincipal, user.ID, "too many MFA challenges") {
						return
					}
					challenge, expires, challengeErr := opts.AuthService.BeginMFA(user.ID, user.Role)
					if challengeErr != nil {
						_ = c.Error(challengeErr)
						httpx.Error(c, http.StatusInternalServerError, 1315, "MFA challenge could not be created")
						return
					}
					endpointLimiters.loginPrincipal.Reset(loginPrincipalKey)
					authLimiter.Reset(c.ClientIP())
					writeMFAChallenge(c, challenge, expires, cookieSessionRequested(req.SessionMode))
					return
				}
				result, err = opts.AuthService.LoginOIDC(user.ID, user.Role)
			}
		}
		if err != nil {
			if errors.Is(err, auth.ErrInvalidCredentials) {
				httpx.Error(c, http.StatusUnauthorized, 1302, "invalid username or password")
				return
			}
			_ = c.Error(err)
			httpx.Error(c, http.StatusInternalServerError, 1303, "authentication failed")
			return
		}
		endpointLimiters.loginPrincipal.Reset(loginPrincipalKey)
		if cookieSessionRequested(req.SessionMode) {
			result, err = setCookieSession(c, result)
			if err != nil {
				recordAuthenticationError(c, "create_cookie_session", err)
				httpx.Error(c, http.StatusInternalServerError, 1303, "authentication failed")
				return
			}
		}
		authLimiter.Reset(c.ClientIP())
		httpx.OK(c, enrichLoginResult(c.Request.Context(), opts.ControlService, result))
	})
	api.POST("/auth/register", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if !allowAuthRequest(c, endpointLimiters.register, "too many registration attempts") {
			return
		}
		policy, err := opts.SettingsService.RegistrationPolicy(c.Request.Context())
		if err != nil {
			_ = c.Error(err)
			httpx.Error(c, http.StatusServiceUnavailable, 1303, "authentication settings are unavailable")
			return
		}
		if !policy.Enabled {
			httpx.Error(c, http.StatusForbidden, 1320, "registration is disabled")
			return
		}
		var req struct {
			Email             string `json:"email"`
			Password          string `json:"password"`
			DisplayName       string `json:"display_name"`
			InvitationCode    string `json:"invitation_code"`
			AgreementAccepted bool   `json:"agreement_accepted"`
			TurnstileToken    string `json:"turnstile_token"`
		}
		if err := bindAuthJSON(c, &req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		registerPrincipalKey := c.ClientIP() + "\x00" + strings.ToLower(strings.TrimSpace(req.Email))
		if !allowAuthRequestForKey(c, endpointLimiters.registerPrincipal, registerPrincipalKey, "too many registration attempts") {
			return
		}
		if !agreementAccepted(c.Request.Context(), opts.SettingsService, req.AgreementAccepted) {
			httpx.Error(c, http.StatusForbidden, 1328, "login agreement must be accepted")
			return
		}
		domain := ""
		if at := strings.LastIndex(req.Email, "@"); at >= 0 {
			domain = strings.ToLower(req.Email[at+1:])
		}
		if len(policy.AllowedDomains) > 0 {
			if !emailDomainAllowed(policy.AllowedDomains, domain) {
				httpx.Error(c, http.StatusForbidden, 1321, "email domain is not allowed")
				return
			}
		}
		security, securityErr := opts.SettingsService.LoginSecurity(c.Request.Context())
		if securityErr != nil {
			_ = c.Error(securityErr)
			httpx.Error(c, http.StatusServiceUnavailable, 1303, "authentication settings are unavailable")
			return
		}
		if security.TurnstileEnabled {
			if err := humanVerifier.Verify(c.Request.Context(), security.TurnstileSecret, req.TurnstileToken, c.ClientIP()); err != nil {
				httpx.Error(c, http.StatusForbidden, 1311, "turnstile verification failed")
				return
			}
		}
		if err := controlplane.ValidatePasswordStrength(req.Password); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1322, err.Error())
			return
		}
		adminSettings, settingsErr := opts.SettingsService.Admin(c.Request.Context())
		if settingsErr != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1327, "user defaults are unavailable")
			return
		}
		defaults := workspaceUserDefaults(adminSettings, "local")
		invitationConsumed := false
		if policy.InvitationRequired {
			if err := opts.SettingsService.ConsumeInvitationCode(c.Request.Context(), req.InvitationCode); err != nil {
				httpx.Error(c, http.StatusForbidden, 1326, "invitation code is invalid")
				return
			}
			invitationConsumed = true
		}
		user, token, err := opts.ControlService.RegisterWorkspaceUser(c.Request.Context(), req.Email, req.Password, req.DisplayName, policy.EmailVerification, defaults)
		if err != nil {
			if invitationConsumed {
				if restoreErr := opts.SettingsService.RestoreInvitationCode(c.Request.Context(), req.InvitationCode); restoreErr != nil {
					_ = c.Error(restoreErr)
				}
			}
			if errors.Is(err, controlplane.ErrPasswordTooWeak) || errors.Is(err, controlplane.ErrPasswordTooLong) {
				httpx.Error(c, http.StatusBadRequest, 1322, controlplane.ErrPasswordTooWeak.Error())
			} else if errors.Is(err, controlplane.ErrInvalidWorkspaceEmail) {
				httpx.Error(c, http.StatusBadRequest, 1322, controlplane.ErrInvalidWorkspaceEmail.Error())
			} else if errors.Is(err, controlplane.ErrUserEmailExists) {
				httpx.Error(c, http.StatusConflict, 1322, "email is already registered")
			} else {
				recordAuthenticationError(c, "register_workspace_user", err)
				httpx.Error(c, http.StatusInternalServerError, 1322, "registration could not be completed")
			}
			return
		}
		emailDeliveryFailed := false
		if policy.EmailVerification && !opts.Runtime.DemoMode {
			verifyURL, linkErr := authenticationActionURL(c.Request.Context(), opts.SettingsService, "/verify-email", token)
			if linkErr == nil {
				linkErr = sendAuthenticationEmail(authEmailSender, c.Request.Context(), "email_verification", user.Email, user.DisplayName, verifyURL, func(ctx context.Context) error {
					return opts.ControlService.CancelEmailVerificationIssue(ctx, user.ID, token)
				})
			}
			if linkErr != nil {
				emailDeliveryFailed = true
				_ = c.Error(linkErr)
				if cancelErr := opts.ControlService.CancelEmailVerificationIssue(c.Request.Context(), user.ID, token); cancelErr != nil {
					_ = c.Error(cancelErr)
				}
			}
		}
		data := gin.H{"user_id": user.ID, "verification_required": policy.EmailVerification, "email_delivery_failed": emailDeliveryFailed}
		if policy.EmailVerification && opts.Runtime.DemoMode {
			data["verification_token"] = token
		}
		httpx.OK(c, data)
	})
	api.POST("/auth/verify-email", func(c *gin.Context) {
		if !allowAuthRequest(c, endpointLimiters.verifyEmail, "too many verification attempts") {
			return
		}
		var req struct {
			Token string `json:"token"`
		}
		if err := bindAuthJSON(c, &req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		if err := opts.ControlService.VerifyWorkspaceUserEmail(c.Request.Context(), req.Token); err != nil {
			if errors.Is(err, controlplane.ErrVerificationTokenInvalid) {
				// 不区分 token 不存在、已过期或已使用。
				httpx.Error(c, http.StatusBadRequest, 1323, "email verification link is invalid or expired")
			} else {
				recordAuthenticationError(c, "verify_email", err)
				httpx.Error(c, http.StatusInternalServerError, 1323, "email verification failed")
			}
			return
		}
		httpx.OK(c, gin.H{"verified": true})
	})
	api.POST("/auth/resend-verification", func(c *gin.Context) {
		if !allowAuthRequest(c, endpointLimiters.resendVerification, "too many verification requests") {
			return
		}
		var req struct {
			Email          string `json:"email"`
			TurnstileToken string `json:"turnstile_token"`
		}
		if err := bindAuthJSON(c, &req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		security, securityErr := opts.SettingsService.LoginSecurity(c.Request.Context())
		if securityErr != nil {
			recordAuthenticationError(c, "resend_verification_settings", securityErr)
			httpx.Error(c, http.StatusInternalServerError, 1303, "authentication settings are unavailable")
			return
		}
		if security.TurnstileEnabled {
			if err := humanVerifier.Verify(c.Request.Context(), security.TurnstileSecret, req.TurnstileToken, c.ClientIP()); err != nil {
				httpx.Error(c, http.StatusForbidden, 1311, "turnstile verification failed")
				return
			}
		}
		policy, policyErr := opts.SettingsService.RegistrationPolicy(c.Request.Context())
		if policyErr != nil {
			recordAuthenticationError(c, "resend_verification_policy", policyErr)
			httpx.Error(c, http.StatusInternalServerError, 1303, "authentication settings are unavailable")
			return
		}
		if !policy.EmailVerification {
			httpx.OK(c, gin.H{"accepted": true})
			return
		}
		user, token, err := opts.ControlService.RenewEmailVerification(c.Request.Context(), req.Email)
		if err == nil {
			verifyURL, linkErr := authenticationActionURL(c.Request.Context(), opts.SettingsService, "/verify-email", token)
			if linkErr == nil {
				linkErr = sendAuthenticationEmail(authEmailSender, c.Request.Context(), "email_verification", user.Email, user.DisplayName, verifyURL, func(ctx context.Context) error {
					return opts.ControlService.CancelEmailVerificationIssue(ctx, user.ID, token)
				})
			}
			if linkErr != nil {
				_ = c.Error(linkErr)
				if cancelErr := opts.ControlService.CancelEmailVerificationIssue(c.Request.Context(), user.ID, token); cancelErr != nil {
					_ = c.Error(cancelErr)
				}
			}
		} else if !errors.Is(err, controlplane.ErrEmailVerificationUnavailable) {
			recordAuthenticationError(c, "resend_verification", err)
		}
		httpx.OK(c, gin.H{"accepted": true})
	})
	api.POST("/auth/forgot-password", func(c *gin.Context) {
		if !allowAuthRequest(c, endpointLimiters.forgotPassword, "too many password reset requests") {
			return
		}
		var req struct {
			Email          string `json:"email"`
			TurnstileToken string `json:"turnstile_token"`
		}
		if err := bindAuthJSON(c, &req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		policy, policyErr := opts.SettingsService.RegistrationPolicy(c.Request.Context())
		if policyErr != nil {
			recordAuthenticationError(c, "forgot_password_policy", policyErr)
			httpx.Error(c, http.StatusInternalServerError, 1303, "authentication settings are unavailable")
			return
		}
		if !policy.PasswordReset {
			httpx.Error(c, http.StatusForbidden, 1329, "password reset is disabled")
			return
		}
		security, securityErr := opts.SettingsService.LoginSecurity(c.Request.Context())
		if securityErr != nil {
			recordAuthenticationError(c, "forgot_password_settings", securityErr)
			httpx.Error(c, http.StatusInternalServerError, 1303, "authentication settings are unavailable")
			return
		}
		if security.TurnstileEnabled {
			if err := humanVerifier.Verify(c.Request.Context(), security.TurnstileSecret, req.TurnstileToken, c.ClientIP()); err != nil {
				httpx.Error(c, http.StatusForbidden, 1311, "turnstile verification failed")
				return
			}
		}
		user, token, err := opts.ControlService.BeginPasswordReset(c.Request.Context(), req.Email)
		if err == nil {
			resetURL, linkErr := authenticationActionURL(c.Request.Context(), opts.SettingsService, "/reset-password", token)
			if linkErr == nil {
				linkErr = sendAuthenticationEmail(authEmailSender, c.Request.Context(), "password_reset", user.Email, user.DisplayName, resetURL, func(ctx context.Context) error {
					return opts.ControlService.CancelPasswordResetIssue(ctx, user.ID, token)
				})
			}
			if linkErr != nil {
				_ = c.Error(linkErr)
				if cancelErr := opts.ControlService.CancelPasswordResetIssue(c.Request.Context(), user.ID, token); cancelErr != nil {
					_ = c.Error(cancelErr)
				}
			}
		} else if !errors.Is(err, controlplane.ErrPasswordResetUnavailable) {
			recordAuthenticationError(c, "forgot_password", err)
		}
		httpx.OK(c, gin.H{"accepted": true})
	})
	api.POST("/auth/reset-password", func(c *gin.Context) {
		if !allowAuthRequest(c, endpointLimiters.resetPassword, "too many password reset attempts") {
			return
		}
		var req struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}
		if err := bindAuthJSON(c, &req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		user, err := opts.ControlService.CompletePasswordReset(c.Request.Context(), req.Token, req.Password)
		if err != nil {
			// 只回显用户自己可修正的原因（密码强度）与统一的凭据失效文案；
			// 其余错误（如数据库故障）不外泄内部细节。
			switch {
			case errors.Is(err, controlplane.ErrPasswordTooWeak), errors.Is(err, controlplane.ErrPasswordTooLong):
				httpx.Error(c, http.StatusBadRequest, 1325, err.Error())
			case errors.Is(err, controlplane.ErrResetTokenInvalid):
				httpx.Error(c, http.StatusBadRequest, 1325, "password reset link is invalid or expired")
			default:
				_ = c.Error(err)
				httpx.Error(c, http.StatusInternalServerError, 1325, "password reset failed")
			}
			return
		}
		if opts.AuthService != nil && opts.AuthService.IsLocalPrincipal(user.ID) {
			opts.AuthService.SetPasswordHash(user.PasswordHash)
		}
		httpx.OK(c, gin.H{"reset": true})
	})
	api.POST("/auth/logout", func(c *gin.Context) {
		if !verifyCookieSessionCSRF(c) {
			return
		}
		clearCookieSession(c)
		if opts.AuthService != nil && opts.ControlService != nil {
			provided := requestSessionToken(c)
			principal, err := opts.AuthService.VerifyWithError(provided)
			if err == nil {
				if err := opts.ControlService.RevokeAccountSessions(c.Request.Context(), principal.Subject); err != nil {
					recordAuthenticationError(c, "logout_revoke_sessions", err)
					httpx.Error(c, http.StatusInternalServerError, 1307, "logout could not be completed")
					return
				}
			} else if errors.Is(err, auth.ErrSessionStateUnavailable) {
				recordAuthenticationError(c, "logout_session_state", err)
				httpx.Error(c, http.StatusInternalServerError, 1307, "logout could not be completed")
				return
			}
		}
		httpx.OK(c, gin.H{"logged_out": true})
	})
	api.GET("/auth/oidc", func(c *gin.Context) {
		if !agreementAccepted(c.Request.Context(), opts.SettingsService, c.Query("agreement_accepted") == "true") {
			httpx.Error(c, http.StatusForbidden, 1328, "login agreement must be accepted")
			return
		}
		if opts.OIDCService == nil {
			httpx.Error(c, http.StatusNotFound, 1404, "oidc is not configured")
			return
		}
		if !allowAuthRequest(c, endpointLimiters.externalLoginStart, "too many external login attempts") {
			return
		}
		entry, err := opts.OIDCService.Begin(time.Now().UTC())
		if err != nil {
			redirectExternalLoginFailure(c, "oidc", err)
			return
		}
		setExternalOAuthStateCookie(c, "oidc", entry.Value)
		c.Redirect(http.StatusFound, opts.OIDCService.AuthorizationURL(entry))
	})
	api.GET("/auth/feishu", func(c *gin.Context) {
		if !agreementAccepted(c.Request.Context(), opts.SettingsService, c.Query("agreement_accepted") == "true") {
			httpx.Error(c, http.StatusForbidden, 1328, "login agreement must be accepted")
			return
		}
		if opts.FeishuService == nil {
			httpx.Error(c, http.StatusNotFound, 1404, "feishu login is not configured")
			return
		}
		if !allowAuthRequest(c, endpointLimiters.externalLoginStart, "too many external login attempts") {
			return
		}
		entry, err := opts.FeishuService.Begin(time.Now().UTC())
		if err != nil {
			redirectExternalLoginFailure(c, "feishu", err)
			return
		}
		setExternalOAuthStateCookie(c, "feishu", entry.Value)
		c.Redirect(http.StatusFound, opts.FeishuService.AuthorizationURL(entry.Value, auth.PKCEChallenge(entry.Verifier)))
	})
	api.GET("/auth/dingtalk", func(c *gin.Context) {
		if !agreementAccepted(c.Request.Context(), opts.SettingsService, c.Query("agreement_accepted") == "true") {
			httpx.Error(c, http.StatusForbidden, 1328, "login agreement must be accepted")
			return
		}
		if opts.DingTalkService == nil {
			httpx.Error(c, http.StatusNotFound, 1404, "DingTalk login is not configured")
			return
		}
		if !allowAuthRequest(c, endpointLimiters.externalLoginStart, "too many external login attempts") {
			return
		}
		entry, err := opts.DingTalkService.Begin(time.Now().UTC())
		if err != nil {
			redirectExternalLoginFailure(c, "dingtalk", err)
			return
		}
		setExternalOAuthStateCookie(c, "dingtalk", entry.Value)
		c.Redirect(http.StatusFound, opts.DingTalkService.AuthorizationURL(entry))
	})
	api.GET("/auth/oidc/callback", func(c *gin.Context) {
		if opts.OIDCService == nil || opts.AuthService == nil || opts.ControlService == nil {
			httpx.Error(c, http.StatusNotFound, 1404, "oidc is not configured")
			return
		}
		if !consumeExternalOAuthStateCookie(c, "oidc", c.Query("state")) {
			redirectExternalLoginFailure(c, "oidc", errExternalOAuthStateMismatch)
			return
		}
		profile, err := opts.OIDCService.Complete(c.Request.Context(), c.Query("state"), c.Query("code"), time.Now().UTC())
		if err != nil {
			redirectExternalLoginFailure(c, "oidc", err)
			return
		}
		if transaction, binding := opts.authBindingStore.Consume(c.Query("state"), "oidc", time.Now().UTC()); binding {
			if err := opts.ControlService.BindCurrentAuthIdentity(c.Request.Context(), transaction.UserID, opts.OIDCService.IssuerURL(), profile.Subject, profile.Email, profile.EmailVerified); err != nil {
				_ = c.Error(err)
				c.Redirect(http.StatusFound, authBindingRedirect(transaction, "error", "", ""))
				return
			}
			c.Redirect(http.StatusFound, authBindingRedirect(transaction, "success", "oidc", ""))
			return
		}
		adminSettings, settingsErr := opts.SettingsService.Admin(c.Request.Context())
		if settingsErr != nil {
			redirectExternalLoginFailure(c, "oidc", settingsErr)
			return
		}
		defaults := workspaceUserDefaults(adminSettings, "oidc")
		user, err := opts.ControlService.ProvisionOIDCUser(c.Request.Context(), opts.OIDCService.IssuerURL(), profile.Subject, profile.Email, profile.DisplayName, profile.Department, profile.EmailVerified, defaults)
		if err != nil {
			redirectExternalLoginFailure(c, "oidc", err)
			return
		}
		if user.TOTPEnabled {
			challenge, expires, err := opts.AuthService.BeginMFA(user.ID, user.Role)
			if err != nil {
				redirectExternalLoginFailure(c, "oidc", err)
				return
			}
			redirectExternalMFAChallenge(c, challenge, expires)
			return
		}
		result, err := opts.AuthService.LoginOIDC(user.ID, user.Role)
		if err != nil {
			redirectExternalLoginFailure(c, "oidc", err)
			return
		}
		if _, err := setCookieSession(c, result); err != nil {
			redirectExternalLoginFailure(c, "oidc", err)
			return
		}
		c.Redirect(http.StatusFound, "/login?oidc=success")
	})
	api.GET("/auth/feishu/callback", func(c *gin.Context) {
		if opts.FeishuService == nil || opts.AuthService == nil || opts.ControlService == nil {
			httpx.Error(c, http.StatusNotFound, 1404, "feishu login is not configured")
			return
		}
		if !consumeExternalOAuthStateCookie(c, "feishu", c.Query("state")) {
			redirectExternalLoginFailure(c, "feishu", errExternalOAuthStateMismatch)
			return
		}
		entry, err := opts.FeishuService.Consume(c.Query("state"), time.Now().UTC())
		if err != nil {
			redirectExternalLoginFailure(c, "feishu", err)
			return
		}
		profile, err := opts.FeishuService.Complete(c.Request.Context(), c.Query("code"), entry.Verifier)
		if err != nil {
			redirectExternalLoginFailure(c, "feishu", err)
			return
		}
		if transaction, binding := opts.authBindingStore.Consume(c.Query("state"), "feishu", time.Now().UTC()); binding {
			if err := opts.ControlService.BindCurrentAuthIdentity(c.Request.Context(), transaction.UserID, "feishu:"+opts.FeishuService.Region(), profile.Subject, profile.Email, false); err != nil {
				_ = c.Error(err)
				c.Redirect(http.StatusFound, authBindingRedirect(transaction, "error", "", ""))
				return
			}
			c.Redirect(http.StatusFound, authBindingRedirect(transaction, "success", "feishu", ""))
			return
		}
		adminSettings, settingsErr := opts.SettingsService.Admin(c.Request.Context())
		if settingsErr != nil {
			redirectExternalLoginFailure(c, "feishu", settingsErr)
			return
		}
		defaults := workspaceUserDefaults(adminSettings, "feishu")
		user, err := opts.ControlService.ProvisionOIDCUser(c.Request.Context(), "feishu:"+opts.FeishuService.Region(), profile.Subject, profile.Email, profile.DisplayName, profile.Department, false, defaults)
		if err != nil {
			redirectExternalLoginFailure(c, "feishu", err)
			return
		}
		if user.TOTPEnabled {
			challenge, expires, err := opts.AuthService.BeginMFA(user.ID, user.Role)
			if err != nil {
				redirectExternalLoginFailure(c, "feishu", err)
				return
			}
			redirectExternalMFAChallenge(c, challenge, expires)
			return
		}
		result, err := opts.AuthService.LoginOIDC(user.ID, user.Role)
		if err != nil {
			redirectExternalLoginFailure(c, "feishu", err)
			return
		}
		if _, err := setCookieSession(c, result); err != nil {
			redirectExternalLoginFailure(c, "feishu", err)
			return
		}
		c.Redirect(http.StatusFound, "/login?provider=feishu")
	})
	api.GET("/auth/dingtalk/callback", func(c *gin.Context) {
		if opts.DingTalkService == nil || opts.AuthService == nil || opts.ControlService == nil {
			httpx.Error(c, http.StatusNotFound, 1404, "DingTalk login is not configured")
			return
		}
		if !consumeExternalOAuthStateCookie(c, "dingtalk", c.Query("state")) {
			redirectExternalLoginFailure(c, "dingtalk", errExternalOAuthStateMismatch)
			return
		}
		profile, err := opts.DingTalkService.Complete(c.Request.Context(), c.Query("state"), c.Query("code"), time.Now().UTC())
		if err != nil {
			redirectExternalLoginFailure(c, "dingtalk", err)
			return
		}
		if transaction, binding := opts.authBindingStore.Consume(c.Query("state"), "dingtalk", time.Now().UTC()); binding {
			if err := opts.ControlService.BindCurrentAuthIdentity(c.Request.Context(), transaction.UserID, "dingtalk", profile.Subject, profile.Email, false); err != nil {
				_ = c.Error(err)
				c.Redirect(http.StatusFound, authBindingRedirect(transaction, "error", "", ""))
				return
			}
			c.Redirect(http.StatusFound, authBindingRedirect(transaction, "success", "dingtalk", ""))
			return
		}
		adminSettings, err := opts.SettingsService.Admin(c.Request.Context())
		if err != nil {
			redirectExternalLoginFailure(c, "dingtalk", err)
			return
		}
		defaults := workspaceUserDefaults(adminSettings, "dingtalk")
		user, err := opts.ControlService.ProvisionOIDCUser(c.Request.Context(), "dingtalk", profile.Subject, profile.Email, profile.DisplayName, profile.Department, false, defaults)
		if err != nil {
			redirectExternalLoginFailure(c, "dingtalk", err)
			return
		}
		if user.TOTPEnabled {
			challenge, expires, challengeErr := opts.AuthService.BeginMFA(user.ID, user.Role)
			if challengeErr != nil {
				redirectExternalLoginFailure(c, "dingtalk", challengeErr)
				return
			}
			redirectExternalMFAChallenge(c, challenge, expires)
			return
		}
		result, err := opts.AuthService.LoginOIDC(user.ID, user.Role)
		if err != nil {
			redirectExternalLoginFailure(c, "dingtalk", err)
			return
		}
		if _, err := setCookieSession(c, result); err != nil {
			redirectExternalLoginFailure(c, "dingtalk", err)
			return
		}
		c.Redirect(http.StatusFound, "/login?provider=dingtalk")
	})
	for _, social := range []*auth.SocialOAuthService{opts.GitHubOAuthService, opts.GoogleOAuthService} {
		if social == nil {
			continue
		}
		provider := social.Provider()
		api.GET("/auth/oauth/"+provider, func(c *gin.Context) {
			if !agreementAccepted(c.Request.Context(), opts.SettingsService, c.Query("agreement_accepted") == "true") {
				httpx.Error(c, http.StatusForbidden, 1328, "login agreement must be accepted")
				return
			}
			if !allowAuthRequest(c, endpointLimiters.externalLoginStart, "too many external login attempts") {
				return
			}
			entry, err := social.Begin(time.Now().UTC())
			if err != nil {
				redirectExternalLoginFailure(c, provider, err)
				return
			}
			setExternalOAuthStateCookie(c, provider, entry.Value)
			c.Redirect(http.StatusFound, social.AuthorizationURL(entry))
		})
		api.GET("/auth/oauth/"+provider+"/callback", func(c *gin.Context) {
			if !consumeExternalOAuthStateCookie(c, provider, c.Query("state")) {
				redirectExternalLoginFailure(c, provider, errExternalOAuthStateMismatch)
				return
			}
			profile, err := social.Complete(c.Request.Context(), c.Query("state"), c.Query("code"), time.Now().UTC())
			if err != nil {
				redirectExternalLoginFailure(c, provider, err)
				return
			}
			if transaction, binding := opts.authBindingStore.Consume(c.Query("state"), provider, time.Now().UTC()); binding {
				if err := opts.ControlService.BindCurrentAuthIdentity(c.Request.Context(), transaction.UserID, provider, profile.Subject, profile.Email, profile.EmailVerified); err != nil {
					_ = c.Error(err)
					c.Redirect(http.StatusFound, authBindingRedirect(transaction, "error", "", ""))
					return
				}
				c.Redirect(http.StatusFound, authBindingRedirect(transaction, "success", provider, ""))
				return
			}
			if err := authorizeSocialProvision(c.Request.Context(), opts.SettingsService, opts.ControlService, provider, profile.Subject, profile.Email); err != nil {
				redirectExternalLoginFailure(c, provider, err)
				return
			}
			adminSettings, err := opts.SettingsService.Admin(c.Request.Context())
			if err != nil {
				redirectExternalLoginFailure(c, provider, err)
				return
			}
			defaults := workspaceUserDefaults(adminSettings, provider)
			user, err := opts.ControlService.ProvisionOIDCUser(c.Request.Context(), provider, profile.Subject, profile.Email, profile.DisplayName, "", profile.EmailVerified, defaults)
			if err != nil {
				redirectExternalLoginFailure(c, provider, err)
				return
			}
			if user.TOTPEnabled {
				challenge, expires, challengeErr := opts.AuthService.BeginMFA(user.ID, user.Role)
				if challengeErr != nil {
					redirectExternalLoginFailure(c, provider, challengeErr)
					return
				}
				redirectExternalMFAChallenge(c, challenge, expires)
				return
			}
			result, err := opts.AuthService.LoginOIDC(user.ID, user.Role)
			if err != nil {
				redirectExternalLoginFailure(c, provider, err)
				return
			}
			if _, err := setCookieSession(c, result); err != nil {
				redirectExternalLoginFailure(c, provider, err)
				return
			}
			c.Redirect(http.StatusFound, "/login?oauth="+url.QueryEscape(provider)+"&status=success")
		})
	}
	api.POST("/auth/totp/login", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if !allowAuthRequest(c, endpointLimiters.totpLogin, "too many MFA attempts") {
			return
		}
		if opts.AuthService == nil || opts.ControlService == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1300, "auth service is not available")
			return
		}
		var req struct {
			Challenge   string `json:"challenge"`
			Code        string `json:"code"`
			SessionMode string `json:"session_mode"`
		}
		if err := bindAuthJSON(c, &req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		challenge := strings.TrimSpace(req.Challenge)
		challengeFromCookie := false
		if challenge == "" {
			if cookieChallenge, err := c.Cookie(externalMFAChallengeCookie); err == nil {
				challenge = strings.TrimSpace(cookieChallenge)
				challengeFromCookie = true
			}
		}
		userID, role, ok := opts.AuthService.InspectMFA(challenge)
		if !ok {
			if challengeFromCookie {
				clearExternalMFAChallengeCookie(c)
			}
			httpx.Error(c, http.StatusUnauthorized, 1316, "MFA challenge is invalid or expired")
			return
		}
		verifiedUser, err := opts.ControlService.VerifyUserTOTP(c.Request.Context(), userID, req.Code)
		if err != nil {
			if errors.Is(err, controlplane.ErrTOTPInvalidCode) {
				if opts.AuthService.RecordMFAFailure(challenge) {
					if challengeFromCookie {
						clearExternalMFAChallengeCookie(c)
					}
					httpx.Error(c, http.StatusUnauthorized, 1316, "MFA challenge is invalid or expired")
					return
				}
				httpx.Error(c, http.StatusUnauthorized, 1317, "invalid TOTP code")
				return
			}
			if errors.Is(err, controlplane.ErrTOTPNotEnabled) {
				_, _, _ = opts.AuthService.ConsumeMFA(challenge)
				if challengeFromCookie {
					clearExternalMFAChallengeCookie(c)
				}
				httpx.Error(c, http.StatusUnauthorized, 1316, "MFA challenge is invalid or expired")
				return
			}
			recordAuthenticationError(c, "verify_mfa", err)
			httpx.Error(c, http.StatusInternalServerError, 1307, "authentication failed")
			return
		}
		consumedUserID, consumedRole, ok := opts.AuthService.ConsumeMFA(challenge)
		if !ok || consumedUserID != userID || consumedRole != role || verifiedUser.ID != userID {
			if challengeFromCookie {
				clearExternalMFAChallengeCookie(c)
			}
			httpx.Error(c, http.StatusUnauthorized, 1316, "MFA challenge is invalid or expired")
			return
		}
		result, err := opts.AuthService.LoginOIDC(verifiedUser.ID, verifiedUser.Role)
		if err != nil {
			clearExternalMFAChallengeCookie(c)
			_ = c.Error(err)
			httpx.Error(c, http.StatusUnauthorized, 1307, "authentication failed")
			return
		}
		endpointLimiters.mfaChallengePrincipal.Reset(userID)
		endpointLimiters.totpLogin.Reset(c.ClientIP())
		clearExternalMFAChallengeCookie(c)
		if challengeFromCookie || cookieSessionRequested(req.SessionMode) {
			result, err = setCookieSession(c, result)
			if err != nil {
				recordAuthenticationError(c, "create_mfa_cookie_session", err)
				httpx.Error(c, http.StatusInternalServerError, 1307, "authentication failed")
				return
			}
		}
		httpx.OK(c, enrichLoginResult(c.Request.Context(), opts.ControlService, result))
	})

	r.GET("/api/iam/get-captcha-code", func(c *gin.Context) {
		httpx.OK(c, gin.H{
			"captchaOnOff": false,
			"img":          "",
			"uuid":         "",
		})
	})
	api.GET("/auth/me", requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService), func(c *gin.Context) {
		httpx.OK(c, currentAuthUser(c, opts))
	})
	registerAccountRoutes(api, opts, endpointLimiters.totpManagement, endpointLimiters.accountBindingStart)
	api.POST("/auth/totp/setup", requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService), func(c *gin.Context) {
		beginAccountTOTPSetup(c, opts, endpointLimiters.totpManagement)
	})
	api.POST("/auth/totp/confirm", requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService), func(c *gin.Context) {
		confirmAccountTOTP(c, opts, endpointLimiters.totpManagement)
	})
	api.POST("/auth/totp/disable", requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService), func(c *gin.Context) {
		disableAccountTOTP(c, opts, endpointLimiters.totpManagement)
	})
	api.POST("/auth/totp/recovery-codes", requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService), func(c *gin.Context) {
		regenerateAccountTOTPRecoveryCodes(c, opts, endpointLimiters.totpManagement)
	})
	registerPluginOpenRoutes(api.Group("/open/plugins"), opts.PluginService, opts.ControlService)
	registerPluginHostRoutes(api.Group("/plugin-host"), opts.PluginService, opts.ControlService)
	applications := api.Group("/applications")
	applications.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	applications.Use(requireRBAC(opts.ControlService))
	registerApplicationRoutes(applications, opts.ControlService)
	console := api.Group("/console")
	console.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	console.Use(requireRBAC(opts.ControlService))
	registerAdminRoutes(console, opts.ControlService, exportJobStore, opts.AIJobRuntime)
	registerAPIKeyClientRoutes(console.Group("/api-keys"), opts.ControlService, opts.SettingsService)
	registerPluginRoutes(console.Group("/plugins"), opts.PluginService, opts.ControlService)
	registerSystemRoutes(console.Group("/system"), opts.SystemService, opts.SettingsService, opts.ControlService)
	onboarding := api.Group("/onboarding")
	onboarding.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	onboarding.Use(requireRBAC(opts.ControlService))
	registerOnboardingRoutes(onboarding, opts.ControlService, opts.SettingsService)
	supply := api.Group("/supply")
	supply.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	supply.Use(requireRBAC(opts.ControlService))
	registerSupplyRoutes(supply, opts.ControlService)
	console.GET("/settings", func(c *gin.Context) {
		data, err := opts.SettingsService.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1004, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	console.PUT("/settings", func(c *gin.Context) {
		var req settings.AdminSettings
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1402, "invalid settings payload")
			return
		}
		previous, err := opts.SettingsService.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1004, err.Error())
			return
		}
		data, err := opts.SettingsService.Update(c.Request.Context(), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1403, err.Error())
			return
		}
		data.RuntimeRestartReasons = authenticationRestartReasons(previous, data)
		if strings.TrimSpace(req.OIDCClientSecret) != "" {
			data.RuntimeRestartReasons = append(data.RuntimeRestartReasons, "oidc_secret")
		}
		if strings.TrimSpace(req.FeishuAppSecret) != "" {
			data.RuntimeRestartReasons = append(data.RuntimeRestartReasons, "feishu_secret")
		}
		if strings.TrimSpace(req.DingTalkClientSecret) != "" {
			data.RuntimeRestartReasons = append(data.RuntimeRestartReasons, "dingtalk_secret")
		}
		if strings.TrimSpace(req.GitHubOAuthClientSecret) != "" {
			data.RuntimeRestartReasons = append(data.RuntimeRestartReasons, "github_secret")
		}
		if strings.TrimSpace(req.GoogleOAuthClientSecret) != "" {
			data.RuntimeRestartReasons = append(data.RuntimeRestartReasons, "google_secret")
		}
		data.RuntimeRestartRequired = len(data.RuntimeRestartReasons) > 0
		httpx.OK(c, data)
	})
	console.POST("/settings/retention/cleanup", func(c *gin.Context) {
		data, err := opts.SettingsService.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1004, err.Error())
			return
		}
		result, err := opts.ControlService.CleanupRetainedData(c.Request.Context(), actor(c), retentionCutoff(data.DataRetentionDays))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1404, err.Error())
			return
		}
		httpx.OK(c, result)
	})
	registerEmailSettings(console, opts.SettingsService)

	portal := api.Group("/portal")
	portal.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	registerPortalRoutes(portal, opts.ControlService, opts.SettingsService)
	registerGatewayRoutes(r, opts.ControlService, opts.DurableAIJobs, opts.PluginService)

	serveSPA(r, opts.Runtime.FrontendDir)
	return r
}

func writeReadinessUnavailable(c *gin.Context, dependency string, err error) {
	slog.Warn("readiness dependency unavailable", "dependency", dependency, "error", err)
	httpx.Error(c, http.StatusServiceUnavailable, 1001, "service dependency is unavailable")
}

func enrichLoginResult(_ context.Context, _ *controlplane.Service, result auth.LoginResult) auth.LoginResult {
	return result
}

func agreementAccepted(ctx context.Context, service *settings.Service, accepted bool) bool {
	public, err := service.Public(ctx)
	return err == nil && (!public.LoginAgreementEnabled || accepted)
}

func authorizeSocialProvision(ctx context.Context, settingsService *settings.Service, control *controlplane.Service, issuer, subject, email string) error {
	exists, err := control.ExternalIdentityExists(ctx, issuer, subject)
	if err != nil || exists {
		return err
	}
	policy, err := settingsService.RegistrationPolicy(ctx)
	if err != nil {
		return err
	}
	if !policy.Enabled {
		return errors.New("registration is disabled for new social login accounts")
	}
	if policy.InvitationRequired {
		return errors.New("an invitation is required before creating a social login account")
	}
	domain := ""
	if at := strings.LastIndex(strings.ToLower(strings.TrimSpace(email)), "@"); at >= 0 {
		domain = strings.TrimSpace(strings.ToLower(email[at+1:]))
	}
	if len(policy.AllowedDomains) > 0 && !emailDomainAllowed(policy.AllowedDomains, domain) {
		return errors.New("email domain is not allowed")
	}
	return nil
}

func emailDomainAllowed(allowedDomains []string, domain string) bool {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	for _, value := range allowedDomains {
		candidate := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "*.")
		if candidate != "" && (domain == candidate || strings.HasSuffix(domain, "."+candidate)) {
			return true
		}
	}
	return false
}

func authenticationActionURL(ctx context.Context, service *settings.Service, path, token string) (string, error) {
	public, err := service.Public(ctx)
	if err != nil {
		return "", err
	}
	baseURL := strings.TrimSpace(public.PublicBaseURL)
	if baseURL == "" {
		return "", errors.New("public authentication base URL is unavailable")
	}
	if err := settings.ValidateSecureAuthenticationBaseURL(baseURL); err != nil {
		return "", err
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("public authentication base URL is invalid")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + strings.TrimLeft(path, "/")
	query := parsed.Query()
	query.Set("token", token)
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String(), nil
}

func authenticationRestartReasons(previous, current settings.AdminSettings) []string {
	var reasons []string
	if previous.PublicBaseURL != current.PublicBaseURL {
		reasons = append(reasons, "public_base_url")
	}
	if previous.OIDCEnabled != current.OIDCEnabled || previous.OIDCIssuerURL != current.OIDCIssuerURL || previous.OIDCClientID != current.OIDCClientID || previous.OIDCRequireVerifiedEmail != current.OIDCRequireVerifiedEmail {
		reasons = append(reasons, "oidc")
	}
	if previous.FeishuEnabled != current.FeishuEnabled || previous.FeishuRegion != current.FeishuRegion || previous.FeishuAppID != current.FeishuAppID || previous.FeishuConfigured != current.FeishuConfigured {
		reasons = append(reasons, "feishu")
	}
	if previous.DingTalkEnabled != current.DingTalkEnabled || previous.DingTalkClientID != current.DingTalkClientID || previous.DingTalkConfigured != current.DingTalkConfigured {
		reasons = append(reasons, "dingtalk")
	}
	if previous.GitHubOAuthEnabled != current.GitHubOAuthEnabled || previous.GitHubOAuthClientID != current.GitHubOAuthClientID || previous.GitHubOAuthConfigured != current.GitHubOAuthConfigured {
		reasons = append(reasons, "github")
	}
	if previous.GoogleOAuthEnabled != current.GoogleOAuthEnabled || previous.GoogleOAuthClientID != current.GoogleOAuthClientID || previous.GoogleOAuthConfigured != current.GoogleOAuthConfigured {
		reasons = append(reasons, "google")
	}
	if previous.TrustedProxyHeaders != current.TrustedProxyHeaders || !slices.Equal(previous.TrustedProxyCIDRs, current.TrustedProxyCIDRs) {
		reasons = append(reasons, "trusted_proxy_headers")
	}
	return reasons
}

func retentionCutoff(days int) time.Time {
	if days < 1 {
		days = 1
	}
	return time.Now().UTC().AddDate(0, 0, -days)
}

func workspaceUserDefaults(admin settings.AdminSettings, source string) controlplane.WorkspaceUserDefaults {
	result := controlplane.WorkspaceUserDefaults{ConcurrencyLimit: admin.DefaultConcurrency, RPMLimit: admin.DefaultRPM}
	if override, ok := admin.AuthSourceDefaults[source]; ok && override.Enabled {
		result = controlplane.WorkspaceUserDefaults{ConcurrencyLimit: override.Concurrency, RPMLimit: override.RPM}
	}
	return result
}

func sendConfiguredEmail(ctx context.Context, service *settings.Service, event, recipient, userName, actionURL string) error {
	return sendConfiguredEmailData(ctx, service, event, recipient, auth.EmailTemplateData{UserName: userName, ActionURL: actionURL})
}

func sendConfiguredEmailData(ctx context.Context, service *settings.Service, event, recipient string, data auth.EmailTemplateData) error {
	admin, err := service.Admin(ctx)
	if err != nil {
		return err
	}
	locale := admin.DefaultLocale
	var subject, htmlBody string
	for _, item := range admin.EmailTemplates {
		if item.Event == event && item.Locale == locale {
			subject, htmlBody = item.Subject, item.HTML
			break
		}
	}
	if subject == "" {
		for _, item := range auth.DefaultEmailTemplates() {
			if item.Event == event && item.Locale == locale {
				subject, htmlBody = item.Subject, item.HTML
				break
			}
		}
	}
	if subject == "" {
		return errors.New("email template is not configured")
	}
	data.SiteName = admin.SiteName
	subject, htmlBody, err = auth.RenderEmailTemplate(subject, htmlBody, data)
	if err != nil {
		return err
	}
	smtpSettings, err := service.SMTPConfig(ctx)
	if err != nil {
		return err
	}
	return (auth.SMTPMailer{Config: smtpConfig(smtpSettings)}).SendHTML(ctx, recipient, subject, htmlBody)
}

func smtpConfig(value settings.SMTPSettings) auth.SMTPConfig {
	return auth.SMTPConfig{
		Host: value.Host, Port: value.Port, Username: value.Username, Password: value.Password,
		From: value.From, FromName: value.FromName, UseTLS: value.UseTLS,
	}
}
