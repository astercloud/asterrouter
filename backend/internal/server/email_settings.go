package server

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/gin-gonic/gin"
)

type smtpTestRequest struct {
	Recipient    string `json:"recipient"`
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     int    `json:"smtp_port"`
	SMTPUsername string `json:"smtp_username"`
	SMTPPassword string `json:"smtp_password"`
	SMTPFrom     string `json:"smtp_from"`
	SMTPFromName string `json:"smtp_from_name"`
	SMTPUseTLS   bool   `json:"smtp_use_tls"`
}

func (r smtpTestRequest) settings() settings.SMTPSettings {
	return settings.SMTPSettings{
		Host: r.SMTPHost, Port: r.SMTPPort, Username: r.SMTPUsername, Password: r.SMTPPassword,
		From: r.SMTPFrom, FromName: r.SMTPFromName, UseTLS: r.SMTPUseTLS,
	}
}

func registerEmailSettings(group *gin.RouterGroup, service *settings.Service) {
	group.POST("/settings/smtp/test-connection", func(c *gin.Context) {
		var req smtpTestRequest
		if c.ShouldBindJSON(&req) != nil {
			httpx.Error(c, http.StatusBadRequest, 1402, "invalid SMTP payload")
			return
		}
		config, err := service.ResolveSMTPConfig(c.Request.Context(), req.settings())
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1402, err.Error())
			return
		}
		if err := (auth.SMTPMailer{Config: smtpConfig(config)}).TestConnection(c.Request.Context()); err != nil {
			httpx.Error(c, http.StatusBadGateway, 1502, err.Error())
			return
		}
		httpx.OK(c, gin.H{"connected": true})
	})

	group.POST("/settings/smtp/test", func(c *gin.Context) {
		var req smtpTestRequest
		if c.ShouldBindJSON(&req) != nil || !validMailbox(req.Recipient) {
			httpx.Error(c, http.StatusBadRequest, 1402, "valid recipient is required")
			return
		}
		config, err := service.ResolveSMTPConfig(c.Request.Context(), req.settings())
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1402, err.Error())
			return
		}
		if err := (auth.SMTPMailer{Config: smtpConfig(config)}).Send(c.Request.Context(), strings.TrimSpace(req.Recipient), "AsterRouter SMTP test", "SMTP configuration is working."); err != nil {
			httpx.Error(c, http.StatusBadGateway, 1502, err.Error())
			return
		}
		httpx.OK(c, gin.H{"sent": true})
	})

	group.GET("/settings/email-templates/defaults", func(c *gin.Context) {
		httpx.OK(c, auth.DefaultEmailTemplates())
	})
	group.GET("/settings/email-templates", func(c *gin.Context) {
		catalog, err := service.EmailTemplateCatalog(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1004, err.Error())
			return
		}
		httpx.OK(c, catalog)
	})
	group.GET("/settings/email-templates/:event/:locale", func(c *gin.Context) {
		template, err := service.EmailTemplate(c.Request.Context(), c.Param("event"), c.Param("locale"))
		writeEmailTemplateResult(c, template, err)
	})
	group.PUT("/settings/email-templates/:event/:locale", func(c *gin.Context) {
		var req struct {
			Subject string `json:"subject"`
			HTML    string `json:"html"`
		}
		if c.ShouldBindJSON(&req) != nil {
			httpx.Error(c, http.StatusBadRequest, 1402, "invalid template payload")
			return
		}
		template, err := service.UpdateEmailTemplate(c.Request.Context(), c.Param("event"), c.Param("locale"), req.Subject, req.HTML)
		writeEmailTemplateResult(c, template, err)
	})
	group.POST("/settings/email-templates/:event/:locale/restore", func(c *gin.Context) {
		template, err := service.RestoreEmailTemplate(c.Request.Context(), c.Param("event"), c.Param("locale"))
		writeEmailTemplateResult(c, template, err)
	})
	group.POST("/settings/email-templates/preview", func(c *gin.Context) {
		var req struct {
			Subject string `json:"subject"`
			HTML    string `json:"html"`
		}
		if c.ShouldBindJSON(&req) != nil {
			httpx.Error(c, http.StatusBadRequest, 1402, "invalid template payload")
			return
		}
		subject, htmlBody, err := renderEmailTemplatePreview(req.Subject, req.HTML)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1420, err.Error())
			return
		}
		httpx.OK(c, gin.H{"subject": subject, "html": htmlBody})
	})
	group.POST("/settings/email-templates/test", func(c *gin.Context) {
		var req struct {
			smtpTestRequest
			Subject string `json:"subject"`
			HTML    string `json:"html"`
		}
		if c.ShouldBindJSON(&req) != nil || !validMailbox(req.Recipient) {
			httpx.Error(c, http.StatusBadRequest, 1402, "valid recipient is required")
			return
		}
		subject, htmlBody, err := renderEmailTemplatePreview(req.Subject, req.HTML)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1420, err.Error())
			return
		}
		config, err := service.ResolveSMTPConfig(c.Request.Context(), req.settings())
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1402, err.Error())
			return
		}
		if err := (auth.SMTPMailer{Config: smtpConfig(config)}).SendHTML(c.Request.Context(), strings.TrimSpace(req.Recipient), subject, htmlBody); err != nil {
			httpx.Error(c, http.StatusBadGateway, 1502, err.Error())
			return
		}
		httpx.OK(c, gin.H{"sent": true})
	})
}

func writeEmailTemplateResult(c *gin.Context, template settings.EmailTemplateDetail, err error) {
	if err == nil {
		httpx.OK(c, template)
		return
	}
	if errors.Is(err, settings.ErrSettingsChanged) {
		httpx.Error(c, http.StatusConflict, 1421, err.Error())
		return
	}
	httpx.Error(c, http.StatusBadRequest, 1420, err.Error())
}

func renderEmailTemplatePreview(subject, htmlBody string) (string, string, error) {
	return auth.RenderEmailTemplate(subject, htmlBody, auth.EmailTemplateData{
		SiteName: "AsterRouter", UserName: "Enterprise User", ActionURL: "https://example.test/action",
		Title: "Service notification", Amount: "100.00", Limit: "100000", Period: "monthly", Message: "Access expires in 7 days.",
	})
}

func validMailbox(value string) bool {
	value = strings.TrimSpace(value)
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value
}
