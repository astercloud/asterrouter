package server

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	operatorcore "github.com/astercloud/asterrouter/backend/internal/operator"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
	"github.com/gin-gonic/gin"
)

type Options struct {
	Config          config.Config
	AuthService     *auth.Service
	OIDCService     *auth.OIDCService
	FeishuService   *auth.FeishuService
	SettingsService *settings.Service
	ControlService  *controlplane.Service
	OperatorService *operatorcore.Service
	PluginService   *plugins.Service
	SystemService   *system.Service
	ExportJobStore  CSVExportJobStore
}

func New(opts Options) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	authLimiter := newAuthAttemptLimiter(10, 5*time.Minute)
	exportJobStore := opts.ExportJobStore
	if exportJobStore == nil {
		exportJobStore = newCSVExportJobStore()
	}
	if opts.ControlService != nil && opts.PluginService != nil {
		opts.ControlService.SetAlertDispatcher(opts.PluginService)
	}

	r.GET("/health", func(c *gin.Context) {
		httpx.OK(c, gin.H{"status": "ok"})
	})

	r.GET("/ready", func(c *gin.Context) {
		if err := opts.SettingsService.Health(c.Request.Context()); err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1001, err.Error())
			return
		}
		if opts.ControlService != nil {
			if err := opts.ControlService.Health(c.Request.Context()); err != nil {
				httpx.Error(c, http.StatusServiceUnavailable, 1001, err.Error())
				return
			}
		}
		if opts.OperatorService != nil {
			if err := opts.OperatorService.Health(c.Request.Context()); err != nil {
				httpx.Error(c, http.StatusServiceUnavailable, 1001, err.Error())
				return
			}
		}
		if opts.PluginService != nil {
			if err := opts.PluginService.Health(c.Request.Context()); err != nil {
				httpx.Error(c, http.StatusServiceUnavailable, 1001, err.Error())
				return
			}
		}
		if exportJobStore != nil {
			if err := exportJobStore.Health(c.Request.Context()); err != nil {
				httpx.Error(c, http.StatusServiceUnavailable, 1001, err.Error())
				return
			}
		}
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
			Profiles       []string `json:"profiles"`
			DefaultProfile string   `json:"default_profile"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		data, err := opts.SettingsService.ApplyProfiles(c.Request.Context(), req.Profiles, req.DefaultProfile)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1401, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	api.POST("/auth/login", func(c *gin.Context) {
		if !authLimiter.Allow(c.ClientIP(), time.Now().UTC()) {
			httpx.Error(c, http.StatusTooManyRequests, 1429, "too many login attempts")
			return
		}
		if opts.AuthService == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1300, "auth service is not available")
			return
		}
		var req struct {
			Username       string `json:"username"`
			Password       string `json:"password"`
			TurnstileToken string `json:"turnstile_token"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1301, "invalid login payload")
			return
		}
		security, err := opts.SettingsService.LoginSecurity(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1303, err.Error())
			return
		}
		if security.TurnstileEnabled {
			if err := (auth.TurnstileVerifier{}).Verify(c.Request.Context(), security.TurnstileSecret, req.TurnstileToken, c.ClientIP()); err != nil {
				httpx.Error(c, http.StatusForbidden, 1311, "turnstile verification failed")
				return
			}
		}
		result, err := opts.AuthService.Login(c.Request.Context(), req.Username, req.Password)
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
		authLimiter.Reset(c.ClientIP())
		httpx.OK(c, result)
	})
	api.POST("/auth/register", func(c *gin.Context) {
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
			Email          string `json:"email"`
			Password       string `json:"password"`
			DisplayName    string `json:"display_name"`
			InvitationCode string `json:"invitation_code"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		domain := ""
		if at := strings.LastIndex(req.Email, "@"); at >= 0 {
			domain = strings.ToLower(req.Email[at+1:])
		}
		if len(policy.AllowedDomains) > 0 {
			allowed := false
			for _, candidate := range policy.AllowedDomains {
				candidate = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(candidate)), "*.")
				if domain == candidate || strings.HasSuffix(domain, "."+candidate) {
					allowed = true
					break
				}
			}
			if !allowed {
				httpx.Error(c, http.StatusForbidden, 1321, "email domain is not allowed")
				return
			}
		}
		if policy.InvitationRequired {
			if len(req.Password) < 10 {
				httpx.Error(c, http.StatusBadRequest, 1322, "password must contain at least 10 characters")
				return
			}
			if err := opts.SettingsService.ConsumeInvitationCode(c.Request.Context(), req.InvitationCode); err != nil {
				httpx.Error(c, http.StatusForbidden, 1326, err.Error())
				return
			}
		}
		user, token, err := opts.ControlService.RegisterWorkspaceUser(c.Request.Context(), req.Email, req.Password, req.DisplayName, policy.EmailVerification)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1322, err.Error())
			return
		}
		if policy.EmailVerification && !opts.Config.DemoMode {
			host, port, username, password, from, mailErr := opts.SettingsService.SMTPConfig(c.Request.Context())
			if mailErr != nil {
				httpx.Error(c, http.StatusBadGateway, 1324, "verification email could not be sent")
				return
			}
			public, _ := opts.SettingsService.Public(c.Request.Context())
			verifyURL := strings.TrimRight(public.PublicBaseURL, "/") + "/login?verify=" + url.QueryEscape(token)
			mailer := auth.SMTPMailer{Config: auth.SMTPConfig{Host: host, Port: port, Username: username, Password: password, From: from}}
			if mailErr := mailer.Send(c.Request.Context(), user.Email, "Verify your AsterRouter account", "Open this link to verify your account:\n\n"+verifyURL); mailErr != nil {
				httpx.Error(c, http.StatusBadGateway, 1324, "verification email could not be sent")
				return
			}
		}
		data := gin.H{"user_id": user.ID, "verification_required": policy.EmailVerification}
		if policy.EmailVerification && opts.Config.DemoMode {
			data["verification_token"] = token
		}
		httpx.OK(c, data)
	})
	api.POST("/auth/verify-email", func(c *gin.Context) {
		var req struct {
			Token string `json:"token"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		if err := opts.ControlService.VerifyWorkspaceUserEmail(c.Request.Context(), req.Token); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1323, err.Error())
			return
		}
		httpx.OK(c, gin.H{"verified": true})
	})
	api.POST("/auth/resend-verification", func(c *gin.Context) {
		var req struct {
			Email string `json:"email"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		user, token, err := opts.ControlService.RenewEmailVerification(c.Request.Context(), req.Email)
		if err == nil {
			host, port, username, password, from, _ := opts.SettingsService.SMTPConfig(c.Request.Context())
			public, _ := opts.SettingsService.Public(c.Request.Context())
			verifyURL := strings.TrimRight(public.PublicBaseURL, "/") + "/login?verify=" + url.QueryEscape(token)
			_ = (auth.SMTPMailer{Config: auth.SMTPConfig{Host: host, Port: port, Username: username, Password: password, From: from}}).Send(c.Request.Context(), user.Email, "Verify your AsterRouter account", "Open this link to verify your account:\n\n"+verifyURL)
		}
		httpx.OK(c, gin.H{"accepted": true})
	})
	api.POST("/auth/forgot-password", func(c *gin.Context) {
		var req struct {
			Email string `json:"email"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		user, token, err := opts.ControlService.BeginPasswordReset(c.Request.Context(), req.Email)
		if err == nil {
			host, port, username, password, from, _ := opts.SettingsService.SMTPConfig(c.Request.Context())
			public, _ := opts.SettingsService.Public(c.Request.Context())
			resetURL := strings.TrimRight(public.PublicBaseURL, "/") + "/login?reset=" + url.QueryEscape(token)
			_ = (auth.SMTPMailer{Config: auth.SMTPConfig{Host: host, Port: port, Username: username, Password: password, From: from}}).Send(c.Request.Context(), user.Email, "Reset your AsterRouter password", "Open this link to reset your password:\n\n"+resetURL)
		}
		httpx.OK(c, gin.H{"accepted": true})
	})
	api.POST("/auth/reset-password", func(c *gin.Context) {
		var req struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		if err := opts.ControlService.CompletePasswordReset(c.Request.Context(), req.Token, req.Password); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1325, err.Error())
			return
		}
		httpx.OK(c, gin.H{"reset": true})
	})
	api.POST("/auth/logout", func(c *gin.Context) {
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("asterrouter_session", "", -1, "/", "", true, true)
		httpx.OK(c, gin.H{"logged_out": true})
	})
	api.GET("/auth/oidc", func(c *gin.Context) {
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
		user, err := opts.ControlService.ProvisionOIDCUser(c.Request.Context(), opts.OIDCService.IssuerURL(), profile.Subject, profile.Email, profile.DisplayName, profile.Department)
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
		user, err := opts.ControlService.ProvisionOIDCUser(c.Request.Context(), "feishu:"+opts.FeishuService.Region(), profile.Subject, profile.Email, profile.DisplayName, profile.Department)
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
	api.POST("/auth/totp/login", func(c *gin.Context) {
		var req struct {
			Challenge string `json:"challenge"`
			Code      string `json:"code"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		userID, role, ok := opts.AuthService.ConsumeMFA(req.Challenge)
		if !ok {
			httpx.Error(c, http.StatusUnauthorized, 1316, "MFA challenge is invalid or expired")
			return
		}
		if _, err := opts.ControlService.VerifyUserTOTP(c.Request.Context(), userID, req.Code); err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1317, "invalid TOTP code")
			return
		}
		result, err := opts.AuthService.LoginOIDC(userID, role)
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1307, err.Error())
			return
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("asterrouter_session", result.AccessToken, int(time.Until(result.ExpiresAt).Seconds()), "/", "", true, true)
		httpx.OK(c, result)
	})

	r.GET("/api/iam/get-captcha-code", func(c *gin.Context) {
		httpx.OK(c, gin.H{
			"captchaOnOff": false,
			"img":          "",
			"uuid":         "",
		})
	})
	api.GET("/auth/me", requireAdminAuth(opts.Config.AdminToken, opts.AuthService), func(c *gin.Context) {
		httpx.OK(c, gin.H{
			"username": actor(c),
			"role":     role(c),
		})
	})
	api.POST("/auth/totp/setup", requireAdminAuth(opts.Config.AdminToken, opts.AuthService), func(c *gin.Context) {
		data, err := opts.ControlService.BeginTOTPSetup(c.Request.Context(), actor(c))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1312, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	api.POST("/auth/totp/confirm", requireAdminAuth(opts.Config.AdminToken, opts.AuthService), func(c *gin.Context) {
		var req struct {
			Code string `json:"code"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		if err := opts.ControlService.ConfirmTOTP(c.Request.Context(), actor(c), req.Code); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1313, err.Error())
			return
		}
		httpx.OK(c, gin.H{"enabled": true})
	})
	api.POST("/auth/totp/disable", requireAdminAuth(opts.Config.AdminToken, opts.AuthService), func(c *gin.Context) {
		var req struct {
			Code string `json:"code"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		if err := opts.ControlService.DisableTOTP(c.Request.Context(), actor(c), req.Code); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1314, err.Error())
			return
		}
		httpx.OK(c, gin.H{"enabled": false})
	})
	api.POST("/auth/totp/recovery-codes", requireAdminAuth(opts.Config.AdminToken, opts.AuthService), func(c *gin.Context) {
		codes, err := opts.ControlService.GenerateTOTPRecoveryCodes(c.Request.Context(), actor(c))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1318, err.Error())
			return
		}
		httpx.OK(c, gin.H{"codes": codes})
	})
	registerPluginOpenRoutes(api.Group("/open/plugins"), opts.PluginService, opts.ControlService)
	registerPluginHostRoutes(api.Group("/plugin-host"), opts.PluginService, opts.ControlService)

	admin := api.Group("/admin")
	admin.Use(requireAdminAuth(opts.Config.AdminToken, opts.AuthService))
	admin.Use(requireProfile(opts.SettingsService, "enterprise"))
	admin.Use(requireRBAC(opts.ControlService))
	registerAdminRoutes(admin, opts.ControlService, exportJobStore)
	registerPluginRoutes(admin.Group("/plugins"), opts.PluginService, opts.ControlService, "enterprise")
	registerSystemRoutes(admin.Group("/system"), opts.SystemService, opts.SettingsService, opts.ControlService)
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
		data, err := opts.SettingsService.Update(c.Request.Context(), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1403, err.Error())
			return
		}
		httpx.OK(c, data)
	})

	portal := api.Group("/portal")
	portal.Use(requireAdminAuth(opts.Config.AdminToken, opts.AuthService))
	portal.Use(requireProfile(opts.SettingsService, "enterprise"))
	registerPortalRoutes(portal, opts.ControlService)

	operatorAPI := api.Group("/operator")
	operatorAPI.Use(requireAdminAuth(opts.Config.AdminToken, opts.AuthService))
	operatorAPI.Use(requireProfile(opts.SettingsService, "relay_operator"))
	registerOperatorRoutes(operatorAPI, opts.OperatorService)
	registerSharedCoreRoutes(operatorAPI, opts.ControlService, false)
	registerSurfaceSettings(operatorAPI, opts.SettingsService)
	registerSystemRoutes(operatorAPI.Group("/system"), opts.SystemService, opts.SettingsService, opts.ControlService)
	registerPluginRoutes(operatorAPI.Group("/plugins"), opts.PluginService, opts.ControlService, "relay_operator")

	consoleAPI := api.Group("/console")
	consoleAPI.Use(requireAdminAuth(opts.Config.AdminToken, opts.AuthService))
	consoleAPI.Use(requireProfile(opts.SettingsService, "personal"))
	registerSharedCoreRoutes(consoleAPI, opts.ControlService, true)
	consoleAPI.GET("/dashboard", func(c *gin.Context) {
		data, err := opts.ControlService.Dashboard(c.Request.Context())
		sharedCoreResponse(c, data, err)
	})
	registerSurfaceSettings(consoleAPI, opts.SettingsService)
	registerSystemRoutes(consoleAPI.Group("/system"), opts.SystemService, opts.SettingsService, opts.ControlService)
	registerPluginRoutes(consoleAPI.Group("/plugins"), opts.PluginService, opts.ControlService, "personal")

	registerGatewayRoutes(r, opts.ControlService)

	serveSPA(r, opts.Config.FrontendDir)
	return r
}
