package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	PluginAPIScopeCatalogRead = "catalog:read"
	PluginAPIScopePluginRead  = "plugin:read"
	PluginAPIScopeAction      = "plugin:action"
	PluginAPIScopeArtifact    = "artifact:write"
	PluginAPIScopeJob         = "job:write"
	PluginAPIScopeEvent       = "event:read"
)

var (
	ErrPluginAPITokenNotFound = errors.New("plugin API token not found")
	ErrPluginAPITokenInvalid  = errors.New("plugin API token is invalid")
	ErrPluginAPITokenExpired  = errors.New("plugin API token is expired")
	ErrPluginAPITokenScope    = errors.New("plugin API token scope is denied")
)

func (s *Service) CreatePluginAPIToken(ctx context.Context, request PluginAPITokenCreateRequest) (PluginAPITokenCreateResult, error) {
	name := trimForStorage(request.Name, 120)
	pluginID := strings.TrimSpace(request.PluginID)
	scopes := cleanStringList(request.Scopes)
	surfaces := cleanStringList(request.Surfaces)
	if name == "" || len(scopes) == 0 || len(surfaces) == 0 {
		return PluginAPITokenCreateResult{}, fmt.Errorf("%w: name, scopes, and surfaces are required", ErrPluginAPITokenInvalid)
	}
	for _, scope := range scopes {
		if !validPluginAPIScope(scope) {
			return PluginAPITokenCreateResult{}, fmt.Errorf("%w: unsupported scope %q", ErrPluginAPITokenInvalid, scope)
		}
	}
	for _, surface := range surfaces {
		if !validPluginAPISurface(surface) {
			return PluginAPITokenCreateResult{}, fmt.Errorf("%w: unsupported surface %q", ErrPluginAPITokenInvalid, surface)
		}
	}
	if containsString(scopes, PluginAPIScopeAction) && pluginID == "" {
		return PluginAPITokenCreateResult{}, fmt.Errorf("%w: plugin:action requires plugin_id", ErrPluginAPITokenInvalid)
	}
	if pluginID != "" {
		plugin, ok, err := s.repo.FindPlugin(ctx, pluginID)
		if err != nil {
			return PluginAPITokenCreateResult{}, err
		}
		if !ok {
			return PluginAPITokenCreateResult{}, ErrPluginNotFound
		}
		for _, surface := range surfaces {
			if !pluginSurfaceAllowed(plugin, surface) {
				return PluginAPITokenCreateResult{}, ErrPluginSurface
			}
		}
	}
	now := s.now().UTC()
	if request.ExpiresAt != nil && !request.ExpiresAt.After(now) {
		return PluginAPITokenCreateResult{}, fmt.Errorf("%w: expires_at must be in the future", ErrPluginAPITokenInvalid)
	}
	secret := "arpt_" + randomID(48)
	hash := pluginAPITokenHash(secret)
	record := pluginAPITokenRecord{
		PluginAPIToken: PluginAPIToken{
			ID:          "pat_" + randomID(18),
			Name:        name,
			PluginID:    pluginID,
			TokenPrefix: secret[:13],
			Scopes:      scopes,
			Surfaces:    surfaces,
			Status:      PluginAPITokenActive,
			ExpiresAt:   cloneTimePointer(request.ExpiresAt),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		TokenHash: hash,
	}
	if err := s.repo.SavePluginAPIToken(ctx, record); err != nil {
		return PluginAPITokenCreateResult{}, err
	}
	return PluginAPITokenCreateResult{Token: record.PluginAPIToken, Secret: secret}, nil
}

func (s *Service) ListPluginAPITokens(ctx context.Context, pluginID string) ([]PluginAPIToken, error) {
	records, err := s.repo.ListPluginAPITokens(ctx, strings.TrimSpace(pluginID))
	if err != nil {
		return nil, err
	}
	out := make([]PluginAPIToken, 0, len(records))
	for _, record := range records {
		out = append(out, record.PluginAPIToken)
	}
	return out, nil
}

func (s *Service) RevokePluginAPIToken(ctx context.Context, id string) (PluginAPIToken, error) {
	id = strings.TrimSpace(id)
	records, err := s.repo.ListPluginAPITokens(ctx, "")
	if err != nil {
		return PluginAPIToken{}, err
	}
	for _, record := range records {
		if record.ID != id {
			continue
		}
		now := s.now().UTC()
		if err := s.repo.RevokePluginAPIToken(ctx, id, now); err != nil {
			return PluginAPIToken{}, err
		}
		record.Status = PluginAPITokenRevoked
		record.UpdatedAt = now
		return record.PluginAPIToken, nil
	}
	return PluginAPIToken{}, ErrPluginAPITokenNotFound
}

func (s *Service) AuthorizePluginAPIToken(ctx context.Context, secret string, requiredScope string, pluginID string, surface string) (PluginAPIToken, error) {
	secret = strings.TrimSpace(secret)
	if !strings.HasPrefix(secret, "arpt_") || len(secret) < 24 {
		return PluginAPIToken{}, ErrPluginAPITokenInvalid
	}
	record, ok, err := s.repo.FindPluginAPIToken(ctx, pluginAPITokenHash(secret))
	if err != nil {
		return PluginAPIToken{}, err
	}
	if !ok || record.Status != PluginAPITokenActive {
		return PluginAPIToken{}, ErrPluginAPITokenInvalid
	}
	now := s.now().UTC()
	if record.ExpiresAt != nil && !record.ExpiresAt.After(now) {
		return PluginAPIToken{}, ErrPluginAPITokenExpired
	}
	if requiredScope != "" && !containsString(record.Scopes, requiredScope) {
		return PluginAPIToken{}, ErrPluginAPITokenScope
	}
	pluginID = strings.TrimSpace(pluginID)
	if pluginID != "" && record.PluginID != pluginID {
		return PluginAPIToken{}, ErrPluginAPITokenScope
	}
	surface = strings.TrimSpace(surface)
	if surface != "" && !containsString(record.Surfaces, surface) {
		return PluginAPIToken{}, ErrPluginAPITokenScope
	}
	if err := s.repo.TouchPluginAPIToken(ctx, record.ID, now); err != nil {
		return PluginAPIToken{}, err
	}
	record.LastUsedAt = &now
	return record.PluginAPIToken, nil
}

func pluginAPITokenHash(secret string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	return hex.EncodeToString(sum[:])
}

func validPluginAPIScope(scope string) bool {
	switch scope {
	case PluginAPIScopeCatalogRead, PluginAPIScopePluginRead, PluginAPIScopeAction, PluginAPIScopeArtifact, PluginAPIScopeJob, PluginAPIScopeEvent:
		return true
	default:
		return false
	}
}

func validPluginAPISurface(surface string) bool {
	switch surface {
	case "personal", "relay_operator", "enterprise":
		return true
	default:
		return false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	clone := value.UTC()
	return &clone
}
