package plugins

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPluginAPITokenIsHashedScopedAndRevocable(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewServiceWithOptions(repo, ServiceOptions{Now: func() time.Time { return now }})
	plugin := Plugin{
		ID:       "com.asterrouter.notification.webhook",
		Name:     "Webhook",
		Surfaces: []string{"personal", "enterprise"},
		Status:   StatusEnabled,
	}
	if err := repo.SavePlugin(ctx, plugin); err != nil {
		t.Fatalf("SavePlugin(): %v", err)
	}
	result, err := svc.CreatePluginAPIToken(ctx, PluginAPITokenCreateRequest{
		Name:     "CI integration",
		PluginID: plugin.ID,
		Scopes:   []string{PluginAPIScopeAction, PluginAPIScopePluginRead},
		Surfaces: []string{"personal"},
	})
	if err != nil {
		t.Fatalf("CreatePluginAPIToken(): %v", err)
	}
	if result.Secret == "" || result.Token.TokenPrefix == result.Secret {
		t.Fatalf("token secret/prefix mismatch: %+v", result)
	}
	records, err := repo.ListPluginAPITokens(ctx, "")
	if err != nil {
		t.Fatalf("ListPluginAPITokens(): %v", err)
	}
	if len(records) != 1 || records[0].TokenHash == result.Secret {
		t.Fatalf("raw API token was stored: %+v", records)
	}
	authorized, err := svc.AuthorizePluginAPIToken(ctx, result.Secret, PluginAPIScopeAction, plugin.ID, "personal")
	if err != nil || authorized.ID != result.Token.ID {
		t.Fatalf("AuthorizePluginAPIToken() = %+v, %v", authorized, err)
	}
	if _, err := svc.AuthorizePluginAPIToken(ctx, result.Secret, PluginAPIScopeAction, plugin.ID, "enterprise"); !errors.Is(err, ErrPluginAPITokenScope) {
		t.Fatalf("surface authorization error = %v, want ErrPluginAPITokenScope", err)
	}
	if _, err := svc.RevokePluginAPIToken(ctx, result.Token.ID); err != nil {
		t.Fatalf("RevokePluginAPIToken(): %v", err)
	}
	if _, err := svc.AuthorizePluginAPIToken(ctx, result.Secret, PluginAPIScopeAction, plugin.ID, "personal"); !errors.Is(err, ErrPluginAPITokenInvalid) {
		t.Fatalf("revoked token authorization error = %v, want ErrPluginAPITokenInvalid", err)
	}
}

func TestPluginAPITokenRequiresPluginBindingForActions(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	_, err := svc.CreatePluginAPIToken(context.Background(), PluginAPITokenCreateRequest{
		Name:     "unbound action",
		Scopes:   []string{PluginAPIScopeAction},
		Surfaces: []string{"personal"},
	})
	if !errors.Is(err, ErrPluginAPITokenInvalid) {
		t.Fatalf("CreatePluginAPIToken() error = %v, want ErrPluginAPITokenInvalid", err)
	}
}
