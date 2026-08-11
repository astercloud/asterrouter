package server

import (
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

type applicationPrincipalRequest struct {
	Name                     string `json:"name"`
	Type                     string `json:"type"`
	ExternalSubjectReference string `json:"external_subject_reference"`
	Status                   string `json:"status"`
}

type externalIntegrationRequest struct {
	PrincipalID       string   `json:"principal_id"`
	Name              string   `json:"name"`
	Protocol          string   `json:"protocol"`
	KeyID             string   `json:"key_id"`
	Secret            string   `json:"secret"`
	Issuer            string   `json:"issuer"`
	JWKSURL           string   `json:"jwks_url"`
	SubjectClaim      string   `json:"subject_claim"`
	ModelsClaim       string   `json:"models_claim"`
	QPSLimitClaim     string   `json:"qps_limit_claim"`
	MonthlyTokenClaim string   `json:"monthly_token_limit_claim"`
	Audience          string   `json:"audience"`
	PolicyID          string   `json:"policy_id"`
	ModelAllowlist    []string `json:"model_allowlist"`
	QPSLimit          int      `json:"qps_limit"`
	MonthlyTokenLimit int      `json:"monthly_token_limit"`
	MaxTTLSeconds     int      `json:"max_ttl_seconds"`
	Status            string   `json:"status"`
}

func registerApplicationRoutes(group *gin.RouterGroup, control *controlplane.Service) {
	if control == nil {
		return
	}
	group.GET("", func(c *gin.Context) {
		items, err := control.ListApplications(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1510, err.Error())
			return
		}
		httpx.OK(c, items)
	})
	group.POST("", func(c *gin.Context) {
		var req controlplane.ApplicationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1511, "invalid application payload")
			return
		}
		data, err := control.CreateApplication(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1511, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	group.PUT("/:id", func(c *gin.Context) {
		var req controlplane.ApplicationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1511, "invalid application payload")
			return
		}
		data, err := control.UpdateApplication(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1511, err.Error())
			return
		}
		httpx.OK(c, data)
	})

	group.GET("/:id/principals", func(c *gin.Context) {
		items, err := control.ListGatewayPrincipals(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1512, err.Error())
			return
		}
		filtered := make([]controlplane.GatewayPrincipal, 0, len(items))
		for _, item := range items {
			if item.ApplicationID == c.Param("id") {
				filtered = append(filtered, item)
			}
		}
		httpx.OK(c, filtered)
	})
	group.POST("/:id/principals", func(c *gin.Context) {
		var req applicationPrincipalRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1513, "invalid principal payload")
			return
		}
		data, err := control.CreateGatewayPrincipal(c.Request.Context(), actor(c), controlplane.GatewayPrincipalRequest{
			ApplicationID: c.Param("id"), Name: req.Name, PrincipalType: req.Type,
			ExternalSubjectReference: req.ExternalSubjectReference, Status: req.Status,
		})
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1513, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	group.PUT("/:id/principals/:principalID", func(c *gin.Context) {
		var req applicationPrincipalRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1513, "invalid principal payload")
			return
		}
		data, err := control.UpdateGatewayPrincipal(c.Request.Context(), actor(c), c.Param("principalID"), controlplane.GatewayPrincipalRequest{
			ApplicationID: c.Param("id"), Name: req.Name, PrincipalType: req.Type,
			ExternalSubjectReference: req.ExternalSubjectReference, Status: req.Status,
		})
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1513, err.Error())
			return
		}
		httpx.OK(c, data)
	})

	group.GET("/:id/external-integrations", func(c *gin.Context) {
		items, err := control.ListExternalAuthIntegrations(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1514, err.Error())
			return
		}
		filtered := make([]controlplane.ExternalAuthIntegration, 0, len(items))
		for _, item := range items {
			if item.ApplicationID == c.Param("id") {
				filtered = append(filtered, item)
			}
		}
		httpx.OK(c, filtered)
	})
	group.POST("/:id/external-integrations", func(c *gin.Context) {
		req, ok := bindExternalIntegrationRequest(c)
		if !ok {
			return
		}
		data, err := control.CreateExternalAuthIntegration(c.Request.Context(), actor(c), externalIntegrationControlRequest(c.Param("id"), req))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1515, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	group.PUT("/:id/external-integrations/:integrationID", func(c *gin.Context) {
		req, ok := bindExternalIntegrationRequest(c)
		if !ok {
			return
		}
		data, err := control.UpdateExternalAuthIntegration(c.Request.Context(), actor(c), c.Param("integrationID"), externalIntegrationControlRequest(c.Param("id"), req))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1515, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	group.POST("/:id/external-integrations/:integrationID/rotate-secret", func(c *gin.Context) {
		items, err := control.ListExternalAuthIntegrations(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1515, err.Error())
			return
		}
		found := false
		for _, item := range items {
			if item.ID == c.Param("integrationID") && item.ApplicationID == c.Param("id") {
				found = true
				break
			}
		}
		if !found {
			httpx.Error(c, http.StatusNotFound, 1515, "external integration not found")
			return
		}
		data, err := control.RotateExternalAuthIntegrationSecret(c.Request.Context(), actor(c), c.Param("integrationID"))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1515, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}

func bindExternalIntegrationRequest(c *gin.Context) (externalIntegrationRequest, bool) {
	var req externalIntegrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Error(c, http.StatusBadRequest, 1515, "invalid external integration payload")
		return externalIntegrationRequest{}, false
	}
	return req, true
}

func externalIntegrationControlRequest(applicationID string, req externalIntegrationRequest) controlplane.ExternalAuthIntegrationRequest {
	return controlplane.ExternalAuthIntegrationRequest{
		ApplicationID: applicationID, GatewayPrincipalID: req.PrincipalID, Name: req.Name, Protocol: req.Protocol,
		KeyID: req.KeyID, Secret: req.Secret, Issuer: req.Issuer, JWKSURL: req.JWKSURL,
		SubjectClaim: req.SubjectClaim, ModelsClaim: req.ModelsClaim, QPSLimitClaim: req.QPSLimitClaim,
		MonthlyTokenClaim: req.MonthlyTokenClaim, Audience: req.Audience, PolicyID: req.PolicyID,
		ModelAllowlist: req.ModelAllowlist, QPSLimit: req.QPSLimit, MonthlyTokenLimit: req.MonthlyTokenLimit,
		MaxTTLSeconds: req.MaxTTLSeconds, Status: req.Status,
	}
}
