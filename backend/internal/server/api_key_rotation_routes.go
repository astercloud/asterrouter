package server

import (
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/gin-gonic/gin"
)

func bindAPIKeyRotateRequest(c *gin.Context) (controlplane.APIKeyRotateRequest, error) {
	var req controlplane.APIKeyRotateRequest
	if c.Request.Body == nil || c.Request.ContentLength == 0 {
		return req, nil
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		return controlplane.APIKeyRotateRequest{}, err
	}
	return req, nil
}
