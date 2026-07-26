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
	operatorcore "github.com/astercloud/asterrouter/backend/internal/operator"
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
	OperatorService    *operatorcore.Service
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
			opts.ControlService.SetCustomerNotificationDispatcher(customerEmailNotificationDispatcher{settings: opts.SettingsService})
		}
		opts.ControlService.SetAlertDispatcher(dispatchers)
		if opts.AuthService != nil {
			opts.AuthService.SetSessionVersionResolver(func(subject string) (int64, bool) {
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
		if opts.OperatorService != nil {
			if err := opts.OperatorService.Health(c.Request.Context()); err != nil {
				metrics.recordReadiness(false)
				writeReadinessUnavailable(c, "operator", err)
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
			"default_profile":  data.DefaultProfile,
			"enabled_profiles": data.EnabledProfiles,
			"setup_completed":  data.SetupCompleted,
		})
	})
	api.POST("/setup/profiles", func(c *gin.Context) {
		var req struct {
			Profile string `json:"profile"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		profile := strings.TrimSpace(req.Profile)
		data, err := opts.SettingsService.ApplyInitialProfile(c.Request.Context(), profile)
		if err != nil {
			if errors.Is(err, settings.ErrUnsupportedDeploymentProfile) {
				httpx.Error(c, http.StatusBadRequest, 1401, err.Error())
				return
			}
			if !errors.Is(err, settings.ErrDeploymentProfileInitialized) {
				_ = c.Error(err)
				httpx.Error(c, http.StatusInternalServerError, 1401, "failed to initialize deployment profile")
				return
			}
			data, err = opts.SettingsService.Admin(c.Request.Context())
			if err != nil {
				_ = c.Error(err)
				httpx.Error(c, http.StatusInternalServerError, 1401, "failed to load deployment profile")
				return
			}
			if !data.SetupCompleted || len(data.EnabledProfiles) != 1 || data.EnabledProfiles[0] != profile || data.DefaultProfile != profile {
				httpx.Error(c, http.StatusBadRequest, 1401, settings.ErrDeploymentProfileInitialized.Error())
				return
			}
		}
		if profile == controlplane.ProfileScopePlatform && opts.ControlService != nil {
			if err := opts.ControlService.EnsurePlatformBootstrap(c.Request.Context()); err != nil {
				_ = c.Error(err)
				httpx.Error(c, http.StatusInternalServerError, 1402, "failed to initialize platform domain")
				return
			}
		}
		httpx.OK(c, data.PublicSettings)
	})
	api.POST("/auth/login", func(c *gin.Context) {
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
			httpx.Error(c, http.StatusInternalServerError, 1303, err.Error())
			return
		}
		if security.TurnstileEnabled {
			if err := humanVerifier.Verify(c.Request.Context(), security.TurnstileSecret, req.TurnstileToken, c.ClientIP()); err != nil {
				httpx.Error(c, http.StatusForbidden, 1311, "turnstile verification failed")
				return
			}
		}
		result, err := opts.AuthService.Login(c.Request.Context(), req.Username, req.Password)
		if err == nil && opts.ControlService != nil && opts.AuthService.IsLocalPrincipal(req.Username) {
			if profile, profileErr := opts.ControlService.CurrentAccountProfile(c.Request.Context(), req.Username); profileErr == nil && profile.TOTPEnabled {
				challenge, expires, challengeErr := opts.AuthService.BeginMFA(profile.ID, profile.Role)
				if challengeErr != nil {
					httpx.Error(c, http.StatusInternalServerError, 1315, challengeErr.Error())
					return
				}
				endpointLimiters.loginPrincipal.Reset(loginPrincipalKey)
				httpx.OK(c, gin.H{"mfa_required": true, "challenge": challenge, "expires_at": expires})
				return
			}
		}
		if err != nil {
			policy, policyErr := opts.SettingsService.RegistrationPolicy(c.Request.Context())
			if policyErr == nil && opts.ControlService != nil {
				if user, userErr := opts.ControlService.AuthenticateWorkspaceUser(c.Request.Context(), req.Username, req.Password, policy.EmailVerification); userErr == nil {
					if user.TOTPEnabled {
						challenge, expires, challengeErr := opts.AuthService.BeginMFA(user.ID, user.Role)
						if challengeErr != nil {
							httpx.Error(c, http.StatusInternalServerError, 1315, challengeErr.Error())
							return
						}
						endpointLimiters.loginPrincipal.Reset(loginPrincipalKey)
						httpx.OK(c, gin.H{"mfa_required": true, "challenge": challenge, "expires_at": expires})
						return
					}
					result, err = opts.AuthService.LoginOIDC(user.ID, user.Role)
				}
			}
		}
		if err != nil {
			if errors.Is(err, auth.ErrInvalidCredentials) {
				httpx.Error(c, http.StatusUnauthorized, 1302, "invalid username or password")
				return
			}
			httpx.Error(c, http.StatusInternalServerError, 1303, err.Error())
			return
		}
		endpointLimiters.loginPrincipal.Reset(loginPrincipalKey)
		httpx.OK(c, enrichLoginResult(c.Request.Context(), opts.ControlService, result))
	})
	api.POST("/auth/register", func(c *gin.Context) {
		if !allowAuthRequest(c, endpointLimiters.register, "too many registration attempts") {
			return
		}
		policy, err := opts.SettingsService.RegistrationPolicy(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1303, err.Error())
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
			httpx.Error(c, http.StatusInternalServerError, 1303, securityErr.Error())
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
			} else if errors.Is(err, controlplane.ErrUserEmailExists) {
				httpx.Error(c, http.StatusConflict, 1322, "email is already registered")
			} else {
				_ = c.Error(err)
				httpx.Error(c, http.StatusBadRequest, 1322, "registration could not be completed")
			}
			return
		}
		emailDeliveryFailed := false
		if policy.EmailVerification && !opts.Runtime.DemoMode {
			public, _ := opts.SettingsService.Public(c.Request.Context())
			verifyURL := strings.TrimRight(public.PublicBaseURL, "/") + "/verify-email?token=" + url.QueryEscape(token)
			if mailErr := authEmailSender.Send(c.Request.Context(), "email_verification", user.Email, user.DisplayName, verifyURL); mailErr != nil {
				emailDeliveryFailed = true
				_ = c.Error(mailErr)
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
			// 统一文案：不区分 token 不存在 / 已过期 / 已使用，避免据此探测他人注册状态。
			httpx.Error(c, http.StatusBadRequest, 1323, "email verification link is invalid or expired")
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
			httpx.Error(c, http.StatusInternalServerError, 1303, "authentication settings are unavailable")
			return
		}
		if !policy.EmailVerification {
			httpx.OK(c, gin.H{"accepted": true})
			return
		}
		user, token, err := opts.ControlService.RenewEmailVerification(c.Request.Context(), req.Email)
		if err == nil {
			public, _ := opts.SettingsService.Public(c.Request.Context())
			verifyURL := strings.TrimRight(public.PublicBaseURL, "/") + "/verify-email?token=" + url.QueryEscape(token)
			if mailErr := authEmailSender.Send(c.Request.Context(), "email_verification", user.Email, user.DisplayName, verifyURL); mailErr != nil {
				_ = c.Error(mailErr)
			}
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
			httpx.Error(c, http.StatusInternalServerError, 1303, "authentication settings are unavailable")
			return
		}
		if !policy.PasswordReset {
			httpx.Error(c, http.StatusForbidden, 1329, "password reset is disabled")
			return
		}
		security, securityErr := opts.SettingsService.LoginSecurity(c.Request.Context())
		if securityErr != nil {
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
			public, _ := opts.SettingsService.Public(c.Request.Context())
			resetURL := strings.TrimRight(public.PublicBaseURL, "/") + "/reset-password?token=" + url.QueryEscape(token)
			if mailErr := authEmailSender.Send(c.Request.Context(), "password_reset", user.Email, user.DisplayName, resetURL); mailErr != nil {
				_ = c.Error(mailErr)
			}
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
		if err := opts.ControlService.CompletePasswordReset(c.Request.Context(), req.Token, req.Password); err != nil {
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
		httpx.OK(c, gin.H{"reset": true})
	})
	api.POST("/auth/logout", func(c *gin.Context) {
		if opts.AuthService != nil && opts.ControlService != nil {
			provided := bearerToken(c)
			if provided == "" {
				provided, _ = c.Cookie("asterrouter_session")
			}
			if principal, ok := opts.AuthService.Verify(provided); ok {
				_ = opts.ControlService.RevokeAccountSessions(c.Request.Context(), principal.Subject)
			}
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("asterrouter_session", "", -1, "/", "", true, true)
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
		entry, err := opts.OIDCService.Begin(time.Now().UTC())
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1304, err.Error())
			return
		}
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
		entry, err := opts.FeishuService.Begin(time.Now().UTC())
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1308, err.Error())
			return
		}
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
		entry, err := opts.DingTalkService.Begin(time.Now().UTC())
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1308, err.Error())
			return
		}
		c.Redirect(http.StatusFound, opts.DingTalkService.AuthorizationURL(entry))
	})
	api.GET("/auth/oidc/callback", func(c *gin.Context) {
		if opts.OIDCService == nil || opts.AuthService == nil || opts.ControlService == nil {
			httpx.Error(c, http.StatusNotFound, 1404, "oidc is not configured")
			return
		}
		profile, err := opts.OIDCService.Complete(c.Request.Context(), c.Query("state"), c.Query("code"), time.Now().UTC())
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1305, err.Error())
			return
		}
		if transaction, binding := opts.authBindingStore.Consume(c.Query("state"), "oidc", time.Now().UTC()); binding {
			if err := opts.ControlService.BindCurrentAuthIdentity(c.Request.Context(), transaction.UserID, opts.OIDCService.IssuerURL(), profile.Subject, profile.Email); err != nil {
				c.Redirect(http.StatusFound, authBindingRedirect(transaction, "error", "", err.Error()))
				return
			}
			c.Redirect(http.StatusFound, authBindingRedirect(transaction, "success", "oidc", ""))
			return
		}
		adminSettings, settingsErr := opts.SettingsService.Admin(c.Request.Context())
		if settingsErr != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1327, "user defaults are unavailable")
			return
		}
		defaults := workspaceUserDefaults(adminSettings, "oidc")
		user, err := opts.ControlService.ProvisionOIDCUser(c.Request.Context(), opts.OIDCService.IssuerURL(), profile.Subject, profile.Email, profile.DisplayName, profile.Department, defaults)
		if err != nil {
			httpx.Error(c, http.StatusForbidden, 1306, err.Error())
			return
		}
		if user.TOTPEnabled {
			challenge, expires, err := opts.AuthService.BeginMFA(user.ID, user.Role)
			if err != nil {
				httpx.Error(c, http.StatusInternalServerError, 1315, err.Error())
				return
			}
			c.Redirect(http.StatusFound, "/login?mfa="+url.QueryEscape(challenge)+"&expires="+url.QueryEscape(expires.Format(time.RFC3339)))
			return
		}
		result, err := opts.AuthService.LoginOIDC(user.ID, user.Role)
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1307, err.Error())
			return
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("asterrouter_session", result.AccessToken, int(time.Until(result.ExpiresAt).Seconds()), "/", "", true, true)
		c.Redirect(http.StatusFound, "/login?oidc=success")
	})
	api.GET("/auth/feishu/callback", func(c *gin.Context) {
		if opts.FeishuService == nil || opts.AuthService == nil || opts.ControlService == nil {
			httpx.Error(c, http.StatusNotFound, 1404, "feishu login is not configured")
			return
		}
		entry, err := opts.FeishuService.Consume(c.Query("state"), time.Now().UTC())
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1309, err.Error())
			return
		}
		profile, err := opts.FeishuService.Complete(c.Request.Context(), c.Query("code"), entry.Verifier)
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1310, err.Error())
			return
		}
		if transaction, binding := opts.authBindingStore.Consume(c.Query("state"), "feishu", time.Now().UTC()); binding {
			if err := opts.ControlService.BindCurrentAuthIdentity(c.Request.Context(), transaction.UserID, "feishu:"+opts.FeishuService.Region(), profile.Subject, profile.Email); err != nil {
				c.Redirect(http.StatusFound, authBindingRedirect(transaction, "error", "", err.Error()))
				return
			}
			c.Redirect(http.StatusFound, authBindingRedirect(transaction, "success", "feishu", ""))
			return
		}
		adminSettings, settingsErr := opts.SettingsService.Admin(c.Request.Context())
		if settingsErr != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1327, "user defaults are unavailable")
			return
		}
		defaults := workspaceUserDefaults(adminSettings, "feishu")
		user, err := opts.ControlService.ProvisionOIDCUser(c.Request.Context(), "feishu:"+opts.FeishuService.Region(), profile.Subject, profile.Email, profile.DisplayName, profile.Department, defaults)
		if err != nil {
			httpx.Error(c, http.StatusForbidden, 1306, err.Error())
			return
		}
		if user.TOTPEnabled {
			challenge, expires, err := opts.AuthService.BeginMFA(user.ID, user.Role)
			if err != nil {
				httpx.Error(c, http.StatusInternalServerError, 1315, err.Error())
				return
			}
			c.Redirect(http.StatusFound, "/login?mfa="+url.QueryEscape(challenge)+"&expires="+url.QueryEscape(expires.Format(time.RFC3339)))
			return
		}
		result, err := opts.AuthService.LoginOIDC(user.ID, user.Role)
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1307, err.Error())
			return
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("asterrouter_session", result.AccessToken, int(time.Until(result.ExpiresAt).Seconds()), "/", "", true, true)
		c.Redirect(http.StatusFound, "/login?provider=feishu")
	})
	api.GET("/auth/dingtalk/callback", func(c *gin.Context) {
		if opts.DingTalkService == nil || opts.AuthService == nil || opts.ControlService == nil {
			httpx.Error(c, http.StatusNotFound, 1404, "DingTalk login is not configured")
			return
		}
		profile, err := opts.DingTalkService.Complete(c.Request.Context(), c.Query("state"), c.Query("code"), time.Now().UTC())
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1310, err.Error())
			return
		}
		if transaction, binding := opts.authBindingStore.Consume(c.Query("state"), "dingtalk", time.Now().UTC()); binding {
			if err := opts.ControlService.BindCurrentAuthIdentity(c.Request.Context(), transaction.UserID, "dingtalk", profile.Subject, profile.Email); err != nil {
				c.Redirect(http.StatusFound, authBindingRedirect(transaction, "error", "", err.Error()))
				return
			}
			c.Redirect(http.StatusFound, authBindingRedirect(transaction, "success", "dingtalk", ""))
			return
		}
		adminSettings, err := opts.SettingsService.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1327, "user defaults are unavailable")
			return
		}
		defaults := workspaceUserDefaults(adminSettings, "dingtalk")
		user, err := opts.ControlService.ProvisionOIDCUser(c.Request.Context(), "dingtalk", profile.Subject, profile.Email, profile.DisplayName, profile.Department, defaults)
		if err != nil {
			httpx.Error(c, http.StatusForbidden, 1306, err.Error())
			return
		}
		if user.TOTPEnabled {
			challenge, expires, challengeErr := opts.AuthService.BeginMFA(user.ID, user.Role)
			if challengeErr != nil {
				httpx.Error(c, http.StatusInternalServerError, 1315, challengeErr.Error())
				return
			}
			c.Redirect(http.StatusFound, "/login?mfa="+url.QueryEscape(challenge)+"&expires="+url.QueryEscape(expires.Format(time.RFC3339)))
			return
		}
		result, err := opts.AuthService.LoginOIDC(user.ID, user.Role)
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1307, err.Error())
			return
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("asterrouter_session", result.AccessToken, int(time.Until(result.ExpiresAt).Seconds()), "/", "", true, true)
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
			entry, err := social.Begin(time.Now().UTC())
			if err != nil {
				httpx.Error(c, http.StatusServiceUnavailable, 1308, err.Error())
				return
			}
			c.Redirect(http.StatusFound, social.AuthorizationURL(entry))
		})
		api.GET("/auth/oauth/"+provider+"/callback", func(c *gin.Context) {
			profile, err := social.Complete(c.Request.Context(), c.Query("state"), c.Query("code"), time.Now().UTC())
			if err != nil {
				httpx.Error(c, http.StatusUnauthorized, 1305, err.Error())
				return
			}
			if transaction, binding := opts.authBindingStore.Consume(c.Query("state"), provider, time.Now().UTC()); binding {
				if err := opts.ControlService.BindCurrentAuthIdentity(c.Request.Context(), transaction.UserID, provider, profile.Subject, profile.Email); err != nil {
					c.Redirect(http.StatusFound, authBindingRedirect(transaction, "error", "", err.Error()))
					return
				}
				c.Redirect(http.StatusFound, authBindingRedirect(transaction, "success", provider, ""))
				return
			}
			if err := authorizeSocialProvision(c.Request.Context(), opts.SettingsService, opts.ControlService, provider, profile.Subject, profile.Email); err != nil {
				httpx.Error(c, http.StatusForbidden, 1329, err.Error())
				return
			}
			adminSettings, err := opts.SettingsService.Admin(c.Request.Context())
			if err != nil {
				httpx.Error(c, http.StatusServiceUnavailable, 1327, "user defaults are unavailable")
				return
			}
			defaults := workspaceUserDefaults(adminSettings, provider)
			user, err := opts.ControlService.ProvisionOIDCUser(c.Request.Context(), provider, profile.Subject, profile.Email, profile.DisplayName, "", defaults)
			if err != nil {
				httpx.Error(c, http.StatusForbidden, 1306, err.Error())
				return
			}
			if user.TOTPEnabled {
				challenge, expires, challengeErr := opts.AuthService.BeginMFA(user.ID, user.Role)
				if challengeErr != nil {
					httpx.Error(c, http.StatusInternalServerError, 1315, challengeErr.Error())
					return
				}
				c.Redirect(http.StatusFound, "/login?mfa="+url.QueryEscape(challenge)+"&expires="+url.QueryEscape(expires.Format(time.RFC3339)))
				return
			}
			result, err := opts.AuthService.LoginOIDC(user.ID, user.Role)
			if err != nil {
				httpx.Error(c, http.StatusUnauthorized, 1307, err.Error())
				return
			}
			c.SetSameSite(http.SameSiteLaxMode)
			c.SetCookie("asterrouter_session", result.AccessToken, int(time.Until(result.ExpiresAt).Seconds()), "/", "", true, true)
			c.Redirect(http.StatusFound, "/login?oauth="+url.QueryEscape(provider)+"&status=success")
		})
	}
	api.POST("/auth/totp/login", func(c *gin.Context) {
		if !allowAuthRequest(c, endpointLimiters.totpLogin, "too many MFA attempts") {
			return
		}
		if opts.AuthService == nil || opts.ControlService == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1300, "auth service is not available")
			return
		}
		var req struct {
			Challenge string `json:"challenge"`
			Code      string `json:"code"`
		}
		if err := bindAuthJSON(c, &req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		userID, role, ok := opts.AuthService.InspectMFA(req.Challenge)
		if !ok {
			httpx.Error(c, http.StatusUnauthorized, 1316, "MFA challenge is invalid or expired")
			return
		}
		if _, err := opts.ControlService.VerifyUserTOTP(c.Request.Context(), userID, req.Code); err != nil {
			if opts.AuthService.RecordMFAFailure(req.Challenge) {
				httpx.Error(c, http.StatusUnauthorized, 1316, "MFA challenge is invalid or expired")
				return
			}
			httpx.Error(c, http.StatusUnauthorized, 1317, "invalid TOTP code")
			return
		}
		consumedUserID, consumedRole, ok := opts.AuthService.ConsumeMFA(req.Challenge)
		if !ok || consumedUserID != userID || consumedRole != role {
			httpx.Error(c, http.StatusUnauthorized, 1316, "MFA challenge is invalid or expired")
			return
		}
		result, err := opts.AuthService.LoginOIDC(userID, role)
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1307, err.Error())
			return
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("asterrouter_session", result.AccessToken, int(time.Until(result.ExpiresAt).Seconds()), "/", "", true, true)
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
	registerAccountRoutes(api, opts, endpointLimiters.totpManagement)
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
	systemAPI := api.Group("/system")
	systemAPI.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	systemAPI.Use(requireSystemAdministrator(opts.ControlService))
	systemAPI.GET("/profiles", func(c *gin.Context) {
		current, err := opts.SettingsService.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1004, err.Error())
			return
		}
		httpx.OK(c, profileBundleResponse(current))
	})
	systemAPI.PUT("/profiles", func(c *gin.Context) {
		var req profileBundleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1402, "invalid profile bundle payload")
			return
		}
		current, err := opts.SettingsService.ApplyProfiles(c.Request.Context(), req.EnabledProfiles, req.DefaultProfile)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1403, err.Error())
			return
		}
		if current.DefaultProfile == controlplane.ProfileScopePlatform && opts.ControlService != nil {
			if err := opts.ControlService.EnsurePlatformBootstrap(c.Request.Context()); err != nil {
				httpx.Error(c, http.StatusInternalServerError, 1404, "failed to initialize platform domain")
				return
			}
		}
		httpx.OK(c, profileBundleResponse(current))
	})

	admin := api.Group("/admin")
	admin.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	admin.Use(requireProfile(opts.SettingsService, "enterprise"))
	admin.Use(requireSurfaceAccess(opts.ControlService, controlplane.SurfaceEnterprise))
	admin.Use(requireRBAC(opts.ControlService))
	registerAdminRoutes(admin, opts.ControlService, exportJobStore, opts.AIJobRuntime)
	registerAPIKeyClientRoutes(admin.Group("/api-keys"), opts.ControlService, opts.SettingsService)
	registerPluginRoutes(admin.Group("/plugins"), opts.PluginService, opts.ControlService, "enterprise")
	registerSystemRoutes(admin.Group("/system"), opts.SystemService, opts.SettingsService, opts.ControlService)
	onboarding := api.Group("/onboarding")
	onboarding.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	onboarding.Use(requireProfile(opts.SettingsService, "enterprise"))
	onboarding.Use(requireSurfaceAccess(opts.ControlService, controlplane.SurfaceEnterprise))
	onboarding.Use(requireRBAC(opts.ControlService))
	registerOnboardingRoutes(onboarding, opts.ControlService, opts.SettingsService)
	supply := api.Group("/supply")
	supply.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	supply.Use(requireProfile(opts.SettingsService, "enterprise"))
	supply.Use(requireSurfaceAccess(opts.ControlService, controlplane.SurfaceEnterprise))
	supply.Use(requireRBAC(opts.ControlService))
	registerSupplyRoutes(supply, opts.ControlService)
	admin.GET("/settings", func(c *gin.Context) {
		data, err := opts.SettingsService.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1004, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/settings", func(c *gin.Context) {
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
		if !requireProfileBundleChange(c, opts.ControlService, previous, req) {
			return
		}
		data, err := opts.SettingsService.Update(c.Request.Context(), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1403, err.Error())
			return
		}
		data.RuntimeRestartReasons = authenticationRestartReasons(previous, data)
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
	admin.POST("/settings/retention/cleanup", func(c *gin.Context) {
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
	admin.POST("/settings/smtp/test", func(c *gin.Context) {
		var req struct {
			Recipient string `json:"recipient"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || !strings.Contains(req.Recipient, "@") {
			httpx.Error(c, http.StatusBadRequest, 1402, "valid recipient is required")
			return
		}
		smtpSettings, err := opts.SettingsService.SMTPConfig(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1501, err.Error())
			return
		}
		mailer := auth.SMTPMailer{Config: smtpConfig(smtpSettings)}
		if err := mailer.Send(c.Request.Context(), strings.TrimSpace(req.Recipient), "AsterRouter SMTP test", "SMTP configuration is working."); err != nil {
			httpx.Error(c, http.StatusBadGateway, 1502, err.Error())
			return
		}
		httpx.OK(c, gin.H{"sent": true})
	})
	admin.GET("/settings/email-templates/defaults", func(c *gin.Context) { httpx.OK(c, auth.DefaultEmailTemplates()) })
	admin.POST("/settings/email-templates/preview", func(c *gin.Context) {
		var req struct {
			Subject string `json:"subject"`
			HTML    string `json:"html"`
		}
		if c.ShouldBindJSON(&req) != nil {
			httpx.Error(c, http.StatusBadRequest, 1402, "invalid template payload")
			return
		}
		data := auth.EmailTemplateData{SiteName: "AsterRouter", UserName: "Enterprise User", ActionURL: "https://example.test/action", Amount: "100.00", Limit: "100000", Period: "monthly", Message: "Access expires in 7 days."}
		subject, htmlBody, err := auth.RenderEmailTemplate(req.Subject, req.HTML, data)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1420, err.Error())
			return
		}
		httpx.OK(c, gin.H{"subject": subject, "html": htmlBody})
	})
	admin.POST("/settings/email-templates/test", func(c *gin.Context) {
		var req struct{ Recipient, Subject, HTML string }
		if c.ShouldBindJSON(&req) != nil || !strings.Contains(req.Recipient, "@") {
			httpx.Error(c, http.StatusBadRequest, 1402, "valid recipient is required")
			return
		}
		data := auth.EmailTemplateData{SiteName: "AsterRouter", UserName: "Enterprise User", ActionURL: "https://example.test/action", Amount: "100.00", Limit: "100000", Period: "monthly", Message: "Access expires in 7 days."}
		subject, htmlBody, err := auth.RenderEmailTemplate(req.Subject, req.HTML, data)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1420, err.Error())
			return
		}
		smtpSettings, err := opts.SettingsService.SMTPConfig(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1501, err.Error())
			return
		}
		if err := (auth.SMTPMailer{Config: smtpConfig(smtpSettings)}).SendHTML(c.Request.Context(), req.Recipient, subject, htmlBody); err != nil {
			httpx.Error(c, http.StatusBadGateway, 1502, err.Error())
			return
		}
		httpx.OK(c, gin.H{"sent": true})
	})

	portal := api.Group("/portal")
	portal.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	portal.Use(requireProfile(opts.SettingsService, "enterprise"))
	portal.Use(requireSurfaceAccess(opts.ControlService, controlplane.SurfacePortal))
	registerPortalRoutes(portal, opts.ControlService, opts.SettingsService)

	customer := api.Group("/customer")
	customer.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	customer.Use(requireProfile(opts.SettingsService, "relay_operator"))
	customer.Use(requireSurfaceAccess(opts.ControlService, controlplane.SurfaceCustomer))
	registerPortalRoutes(customer, opts.ControlService, opts.SettingsService)
	registerCustomerRoutes(customer, opts.ControlService)

	operatorAPI := api.Group("/operator")
	operatorAPI.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	operatorAPI.Use(requireProfile(opts.SettingsService, "relay_operator"))
	operatorAPI.Use(requireSurfaceAccess(opts.ControlService, controlplane.SurfaceRelayOperator))
	registerOperatorRoutes(operatorAPI, opts.OperatorService, opts.ControlService)
	registerSharedCoreRoutes(operatorAPI, opts.ControlService, false)
	registerSurfaceSettings(operatorAPI, opts.SettingsService, opts.ControlService)
	registerSystemRoutes(operatorAPI.Group("/system"), opts.SystemService, opts.SettingsService, opts.ControlService)
	registerPluginRoutes(operatorAPI.Group("/plugins"), opts.PluginService, opts.ControlService, "relay_operator")

	consoleAPI := api.Group("/console")
	consoleAPI.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	consoleAPI.Use(requireProfile(opts.SettingsService, "personal"))
	consoleAPI.Use(requireSurfaceAccess(opts.ControlService, controlplane.SurfacePersonal))
	registerSharedCoreRoutes(consoleAPI, opts.ControlService, true)
	consoleAPI.GET("/dashboard", func(c *gin.Context) {
		data, err := opts.ControlService.Dashboard(c.Request.Context())
		sharedCoreResponse(c, data, err)
	})
	registerSurfaceSettings(consoleAPI, opts.SettingsService, opts.ControlService)
	registerSystemRoutes(consoleAPI.Group("/system"), opts.SystemService, opts.SettingsService, opts.ControlService)
	registerPluginRoutes(consoleAPI.Group("/plugins"), opts.PluginService, opts.ControlService, "personal")

	platformAPI := api.Group("/platform")
	platformAPI.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	platformAPI.Use(requireProfile(opts.SettingsService, "platform"))
	platformAPI.Use(requireSurfaceAccess(opts.ControlService, controlplane.SurfacePlatform))
	platformAPI.Use(requireSurfaceRBAC(opts.ControlService, controlplane.SurfacePlatform))
	registerPlatformRoutes(platformAPI, opts.ControlService, opts.PluginService, opts.AIJobRuntime)
	registerSurfaceSettings(platformAPI, opts.SettingsService, opts.ControlService)
	registerSystemRoutes(platformAPI.Group("/system"), opts.SystemService, opts.SettingsService, opts.ControlService)
	registerGatewayRoutes(r, opts.ControlService, opts.DurableAIJobs, opts.PluginService)

	serveSPA(r, opts.Runtime.FrontendDir)
	return r
}

func writeReadinessUnavailable(c *gin.Context, dependency string, err error) {
	slog.Warn("readiness dependency unavailable", "dependency", dependency, "error", err)
	httpx.Error(c, http.StatusServiceUnavailable, 1001, "service dependency is unavailable")
}

func enrichLoginResult(ctx context.Context, control *controlplane.Service, result auth.LoginResult) auth.LoginResult {
	result.User.AllowedSurfaces = allowedSurfacesForActor(ctx, control, result.User.Username)
	return result
}

type profileBundleRequest struct {
	EnabledProfiles []string `json:"enabled_profiles"`
	DefaultProfile  string   `json:"default_profile"`
}

func profileBundleResponse(current settings.AdminSettings) profileBundleRequest {
	return profileBundleRequest{
		EnabledProfiles: current.EnabledProfiles,
		DefaultProfile:  current.DefaultProfile,
	}
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
	result := controlplane.WorkspaceUserDefaults{BalanceMicros: admin.DefaultBalanceMicros, ConcurrencyLimit: admin.DefaultConcurrency, RPMLimit: admin.DefaultRPM}
	if override, ok := admin.AuthSourceDefaults[source]; ok && override.Enabled {
		result = controlplane.WorkspaceUserDefaults{BalanceMicros: override.BalanceMicros, ConcurrencyLimit: override.Concurrency, RPMLimit: override.RPM}
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
