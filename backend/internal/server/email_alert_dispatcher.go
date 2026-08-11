package server

import (
	"context"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/settings"
)

type multiAlertDispatcher []controlplane.AlertDispatcher

func (dispatchers multiAlertDispatcher) DispatchAlert(ctx context.Context, event controlplane.AlertEvent) error {
	for _, dispatcher := range dispatchers {
		if dispatcher != nil {
			if err := dispatcher.DispatchAlert(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

type emailAlertDispatcher struct {
	control  *controlplane.Service
	settings *settings.Service
}

func (d emailAlertDispatcher) DispatchAlert(ctx context.Context, event controlplane.AlertEvent) error {
	if event.Type != controlplane.AlertTypeAPIKeyQuota || event.ResourceType != "api_key" {
		return nil
	}
	keys, err := d.control.ListAPIKeys(ctx)
	if err != nil {
		return err
	}
	ownerID := ""
	for _, key := range keys {
		if key.ID == event.ResourceID {
			ownerID = key.OwnerUserID
			break
		}
	}
	if ownerID == "" {
		return nil
	}
	users, err := d.control.ListWorkspaceUsers(ctx)
	if err != nil {
		return err
	}
	for _, user := range users {
		if user.ID == ownerID && user.Email != "" {
			return sendConfiguredEmailData(ctx, d.settings, "quota_limit", user.Email, auth.EmailTemplateData{UserName: user.DisplayName, Limit: event.Metadata["monthly_token_limit"], Period: "monthly"})
		}
	}
	return nil
}
