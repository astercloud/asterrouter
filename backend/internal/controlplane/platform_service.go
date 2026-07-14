package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	defaultPlatformTenantID      = "ptn_default"
	defaultPlatformPrincipalID   = "gpr_default_service"
	defaultPlatformTenantSlug    = "default"
	defaultPlatformTenantName    = "Default Platform Tenant"
	defaultPlatformPrincipalName = "Default Service"
)

type platformCredentialIdentity struct {
	tenant    PlatformTenant
	principal GatewayPrincipal
}

// EnsurePlatformBootstrap creates an explicit platform root for a Platform
// installation. It never maps existing enterprise users or relay customers
// into platform entities.
func (s *Service) EnsurePlatformBootstrap(ctx context.Context) error {
	tenants, err := s.repo.ListPlatformTenants(ctx)
	if err != nil {
		return err
	}
	var tenant PlatformTenant
	foundTenant := false
	for _, item := range tenants {
		if item.ID == defaultPlatformTenantID || item.Slug == defaultPlatformTenantSlug {
			tenant = item
			foundTenant = true
			break
		}
	}
	if !foundTenant {
		now := s.nowUTC()
		tenant = PlatformTenant{
			ID: defaultPlatformTenantID, Name: defaultPlatformTenantName, Slug: defaultPlatformTenantSlug,
			Status: PlatformTenantStatusActive, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.repo.SavePlatformTenant(ctx, tenant); err != nil {
			return err
		}
	}

	principals, err := s.repo.ListGatewayPrincipals(ctx)
	if err != nil {
		return err
	}
	for _, principal := range principals {
		if principal.ID == defaultPlatformPrincipalID {
			return nil
		}
	}
	now := s.nowUTC()
	return s.repo.SaveGatewayPrincipal(ctx, GatewayPrincipal{
		ID: defaultPlatformPrincipalID, TenantID: tenant.ID, Name: defaultPlatformPrincipalName,
		PrincipalType: GatewayPrincipalTypeService, Status: GatewayPrincipalStatusActive,
		CreatedAt: now, UpdatedAt: now,
	})
}

func (s *Service) ListPlatformTenants(ctx context.Context) ([]PlatformTenant, error) {
	return s.repo.ListPlatformTenants(ctx)
}

func (s *Service) CreatePlatformTenant(ctx context.Context, actor string, req PlatformTenantRequest) (PlatformTenant, error) {
	tenant, err := platformTenantFromRequest(req, s.nowUTC())
	if err != nil {
		return PlatformTenant{}, err
	}
	if err := s.requirePlatformTenantSlugAvailable(ctx, tenant.Slug, ""); err != nil {
		return PlatformTenant{}, err
	}
	tenant.ID = "ptn_" + randomID(10)
	if err := s.repo.SavePlatformTenant(ctx, tenant); err != nil {
		return PlatformTenant{}, err
	}
	if err := s.auditPlatform(ctx, actor, "create", "platform_tenant", tenant.ID, fmt.Sprintf("Created platform tenant %s", tenant.Name), &tenant, nil); err != nil {
		return PlatformTenant{}, err
	}
	return tenant, nil
}

func (s *Service) UpdatePlatformTenant(ctx context.Context, actor, id string, req PlatformTenantRequest) (PlatformTenant, error) {
	existing, err := s.platformTenantByID(ctx, id)
	if err != nil {
		return PlatformTenant{}, err
	}
	tenant, err := platformTenantFromRequest(req, existing.CreatedAt)
	if err != nil {
		return PlatformTenant{}, err
	}
	if err := s.requirePlatformTenantSlugAvailable(ctx, tenant.Slug, existing.ID); err != nil {
		return PlatformTenant{}, err
	}
	tenant.ID = existing.ID
	tenant.UpdatedAt = s.nowUTC()
	if err := s.repo.SavePlatformTenant(ctx, tenant); err != nil {
		return PlatformTenant{}, err
	}
	if err := s.auditPlatform(ctx, actor, "update", "platform_tenant", tenant.ID, fmt.Sprintf("Updated platform tenant %s", tenant.Name), &tenant, nil); err != nil {
		return PlatformTenant{}, err
	}
	return tenant, nil
}

func (s *Service) ListGatewayPrincipals(ctx context.Context) ([]GatewayPrincipal, error) {
	return s.repo.ListGatewayPrincipals(ctx)
}

func (s *Service) CreateGatewayPrincipal(ctx context.Context, actor string, req GatewayPrincipalRequest) (GatewayPrincipal, error) {
	principal, err := gatewayPrincipalFromRequest(req, s.nowUTC())
	if err != nil {
		return GatewayPrincipal{}, err
	}
	tenant, err := s.activePlatformTenantByID(ctx, principal.TenantID)
	if err != nil {
		return GatewayPrincipal{}, err
	}
	if err := s.requireGatewayPrincipalNameAvailable(ctx, principal.TenantID, principal.Name, ""); err != nil {
		return GatewayPrincipal{}, err
	}
	principal.ID = "gpr_" + randomID(10)
	if err := s.repo.SaveGatewayPrincipal(ctx, principal); err != nil {
		return GatewayPrincipal{}, err
	}
	if err := s.auditPlatform(ctx, actor, "create", "gateway_principal", principal.ID, fmt.Sprintf("Created gateway principal %s", principal.Name), &tenant, &principal); err != nil {
		return GatewayPrincipal{}, err
	}
	return principal, nil
}

func (s *Service) UpdateGatewayPrincipal(ctx context.Context, actor, id string, req GatewayPrincipalRequest) (GatewayPrincipal, error) {
	existing, err := s.gatewayPrincipalByID(ctx, id)
	if err != nil {
		return GatewayPrincipal{}, err
	}
	if strings.TrimSpace(req.TenantID) == "" {
		req.TenantID = existing.TenantID
	}
	if strings.TrimSpace(req.TenantID) != existing.TenantID {
		return GatewayPrincipal{}, errors.New("gateway principal tenant_id is immutable")
	}
	principal, err := gatewayPrincipalFromRequest(req, existing.CreatedAt)
	if err != nil {
		return GatewayPrincipal{}, err
	}
	tenant, err := s.platformTenantByID(ctx, principal.TenantID)
	if err != nil {
		return GatewayPrincipal{}, err
	}
	if err := s.requireGatewayPrincipalNameAvailable(ctx, principal.TenantID, principal.Name, existing.ID); err != nil {
		return GatewayPrincipal{}, err
	}
	principal.ID = existing.ID
	principal.UpdatedAt = s.nowUTC()
	if err := s.repo.SaveGatewayPrincipal(ctx, principal); err != nil {
		return GatewayPrincipal{}, err
	}
	if err := s.auditPlatform(ctx, actor, "update", "gateway_principal", principal.ID, fmt.Sprintf("Updated gateway principal %s", principal.Name), &tenant, &principal); err != nil {
		return GatewayPrincipal{}, err
	}
	return principal, nil
}

func (s *Service) CreatePlatformAPIKey(ctx context.Context, actor string, req APIKeyCreateRequest) (APIKeyCreateResponse, error) {
	if err := validatePlatformKeyRequestOwnership(req); err != nil {
		return APIKeyCreateResponse{}, err
	}
	identity, err := s.activePlatformCredentialIdentity(ctx, req.PlatformTenantID, req.GatewayPrincipalID)
	if err != nil {
		return APIKeyCreateResponse{}, err
	}
	return s.createAPIKey(ctx, actor, req, &identity)
}

func (s *Service) UpdatePlatformAPIKey(ctx context.Context, actor, id string, req APIKeyUpdateRequest) (APIKeyRecord, error) {
	key, err := s.apiKeyByID(ctx, id)
	if err != nil {
		return APIKeyRecord{}, err
	}
	if key.ProfileScope != ProfileScopePlatform {
		return APIKeyRecord{}, errors.New("platform api key not found")
	}
	if strings.TrimSpace(req.KeyType) != "" && req.KeyType != APIKeyTypeWorkspace && req.KeyType != APIKeyTypeService {
		return APIKeyRecord{}, errors.New("platform API keys must use workspace or service ownership")
	}
	if strings.TrimSpace(req.CustomerID) != "" || strings.TrimSpace(req.OwnerUserID) != "" {
		return APIKeyRecord{}, errors.New("platform API keys cannot reference relay customers or enterprise users")
	}
	if strings.TrimSpace(req.PlatformTenantID) != "" && strings.TrimSpace(req.PlatformTenantID) != key.PlatformTenantID {
		return APIKeyRecord{}, errors.New("platform API key tenant ownership is immutable")
	}
	if strings.TrimSpace(req.GatewayPrincipalID) != "" && strings.TrimSpace(req.GatewayPrincipalID) != key.GatewayPrincipalID {
		return APIKeyRecord{}, errors.New("platform API key principal ownership is immutable")
	}
	if _, err := s.activePlatformCredentialIdentity(ctx, key.PlatformTenantID, key.GatewayPrincipalID); err != nil {
		return APIKeyRecord{}, err
	}
	return s.updateAPIKey(ctx, actor, key, req)
}

func (s *Service) RotatePlatformAPIKey(ctx context.Context, actor, id string) (APIKeyCreateResponse, error) {
	return s.RotatePlatformAPIKeyWithGrace(ctx, actor, id, 0)
}

func (s *Service) RotatePlatformAPIKeyWithGrace(ctx context.Context, actor, id string, gracePeriodSeconds int) (APIKeyCreateResponse, error) {
	if gracePeriodSeconds < 0 || gracePeriodSeconds > 86400 {
		return APIKeyCreateResponse{}, errors.New("grace_period_seconds must be between 0 and 86400")
	}
	key, err := s.apiKeyByID(ctx, id)
	if err != nil {
		return APIKeyCreateResponse{}, err
	}
	if key.ProfileScope != ProfileScopePlatform {
		return APIKeyCreateResponse{}, errors.New("platform api key not found")
	}
	identity, err := s.activePlatformCredentialIdentity(ctx, key.PlatformTenantID, key.GatewayPrincipalID)
	if err != nil {
		return APIKeyCreateResponse{}, err
	}
	return s.rotateAPIKey(ctx, actor, key, &identity, time.Duration(gracePeriodSeconds)*time.Second)
}

func (s *Service) DisablePlatformAPIKey(ctx context.Context, actor, id string) error {
	key, err := s.apiKeyByID(ctx, id)
	if err != nil {
		return err
	}
	if key.ProfileScope != ProfileScopePlatform {
		return errors.New("platform api key not found")
	}
	identity, err := s.platformCredentialIdentity(ctx, key.PlatformTenantID, key.GatewayPrincipalID)
	if err != nil {
		return err
	}
	if err := s.repo.DisableAPIKey(ctx, key.ID, s.nowUTC()); err != nil {
		return err
	}
	return s.auditPlatform(ctx, actor, "disable", "api_key", key.ID, "Disabled platform API key", &identity.tenant, &identity.principal)
}

func (s *Service) activePlatformCredentialIdentity(ctx context.Context, tenantID, principalID string) (platformCredentialIdentity, error) {
	identity, err := s.platformCredentialIdentity(ctx, tenantID, principalID)
	if err != nil {
		return platformCredentialIdentity{}, err
	}
	if identity.tenant.Status != PlatformTenantStatusActive || identity.principal.Status != GatewayPrincipalStatusActive {
		return platformCredentialIdentity{}, errors.New("gateway principal is not active for platform tenant")
	}
	return identity, nil
}

func (s *Service) platformCredentialIdentity(ctx context.Context, tenantID, principalID string) (platformCredentialIdentity, error) {
	tenant, err := s.platformTenantByID(ctx, tenantID)
	if err != nil {
		return platformCredentialIdentity{}, err
	}
	principal, err := s.gatewayPrincipalByID(ctx, principalID)
	if err != nil {
		return platformCredentialIdentity{}, err
	}
	if principal.TenantID != tenant.ID {
		return platformCredentialIdentity{}, errors.New("gateway principal does not belong to platform tenant")
	}
	return platformCredentialIdentity{tenant: tenant, principal: principal}, nil
}

func (s *Service) activePlatformTenantByID(ctx context.Context, id string) (PlatformTenant, error) {
	tenant, err := s.platformTenantByID(ctx, id)
	if err != nil {
		return PlatformTenant{}, err
	}
	if tenant.Status != PlatformTenantStatusActive {
		return PlatformTenant{}, errors.New("platform tenant is not active")
	}
	return tenant, nil
}

func (s *Service) platformTenantByID(ctx context.Context, id string) (PlatformTenant, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return PlatformTenant{}, errors.New("platform tenant_id is required")
	}
	tenants, err := s.repo.ListPlatformTenants(ctx)
	if err != nil {
		return PlatformTenant{}, err
	}
	for _, tenant := range tenants {
		if tenant.ID == id {
			return tenant, nil
		}
	}
	return PlatformTenant{}, errors.New("platform tenant not found")
}

func (s *Service) gatewayPrincipalByID(ctx context.Context, id string) (GatewayPrincipal, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return GatewayPrincipal{}, errors.New("gateway principal_id is required")
	}
	principals, err := s.repo.ListGatewayPrincipals(ctx)
	if err != nil {
		return GatewayPrincipal{}, err
	}
	for _, principal := range principals {
		if principal.ID == id {
			return principal, nil
		}
	}
	return GatewayPrincipal{}, errors.New("gateway principal not found")
}

func (s *Service) requirePlatformTenantSlugAvailable(ctx context.Context, slug, exceptID string) error {
	tenants, err := s.repo.ListPlatformTenants(ctx)
	if err != nil {
		return err
	}
	for _, tenant := range tenants {
		if tenant.ID != exceptID && tenant.Slug == slug {
			return errors.New("platform tenant slug already exists")
		}
	}
	return nil
}

func (s *Service) requireGatewayPrincipalNameAvailable(ctx context.Context, tenantID, name, exceptID string) error {
	principals, err := s.repo.ListGatewayPrincipals(ctx)
	if err != nil {
		return err
	}
	for _, principal := range principals {
		if principal.ID != exceptID && principal.TenantID == tenantID && strings.EqualFold(principal.Name, name) {
			return errors.New("gateway principal name already exists for platform tenant")
		}
	}
	return nil
}

func platformTenantFromRequest(req PlatformTenantRequest, createdAt time.Time) (PlatformTenant, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 120 {
		return PlatformTenant{}, errors.New("platform tenant name must contain 1 to 120 characters")
	}
	slug := normalizePlatformSlug(req.Slug)
	if slug == "" {
		return PlatformTenant{}, errors.New("platform tenant slug must contain lowercase letters, digits, or hyphens")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = PlatformTenantStatusActive
	}
	if !oneOf(status, PlatformTenantStatusActive, PlatformTenantStatusDisabled) {
		return PlatformTenant{}, errors.New("platform tenant status must be active or disabled")
	}
	return PlatformTenant{Name: name, Slug: slug, EntitlementReference: strings.TrimSpace(req.EntitlementReference), Status: status, CreatedAt: createdAt, UpdatedAt: createdAt}, nil
}

func gatewayPrincipalFromRequest(req GatewayPrincipalRequest, createdAt time.Time) (GatewayPrincipal, error) {
	tenantID := strings.TrimSpace(req.TenantID)
	if tenantID == "" {
		return GatewayPrincipal{}, errors.New("gateway principal tenant_id is required")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 120 {
		return GatewayPrincipal{}, errors.New("gateway principal name must contain 1 to 120 characters")
	}
	principalType := strings.TrimSpace(req.PrincipalType)
	if principalType == "" {
		principalType = GatewayPrincipalTypeService
	}
	if !oneOf(principalType, GatewayPrincipalTypeService, GatewayPrincipalTypeDeveloper, GatewayPrincipalTypeIntegration) {
		return GatewayPrincipal{}, errors.New("gateway principal type must be service, developer, or integration")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = GatewayPrincipalStatusActive
	}
	if !oneOf(status, GatewayPrincipalStatusActive, GatewayPrincipalStatusDisabled) {
		return GatewayPrincipal{}, errors.New("gateway principal status must be active or disabled")
	}
	return GatewayPrincipal{TenantID: tenantID, Name: name, PrincipalType: principalType, ExternalSubjectReference: strings.TrimSpace(req.ExternalSubjectReference), Status: status, CreatedAt: createdAt, UpdatedAt: createdAt}, nil
}

func validatePlatformKeyRequestOwnership(req APIKeyCreateRequest) error {
	keyType := strings.TrimSpace(req.KeyType)
	if keyType != "" && keyType != APIKeyTypeWorkspace && keyType != APIKeyTypeService {
		return errors.New("platform API keys must use workspace or service ownership")
	}
	if strings.TrimSpace(req.CustomerID) != "" || strings.TrimSpace(req.OwnerUserID) != "" {
		return errors.New("platform API keys cannot reference relay customers or enterprise users")
	}
	if strings.TrimSpace(req.PlatformTenantID) == "" || strings.TrimSpace(req.GatewayPrincipalID) == "" {
		return errors.New("platform API keys require platform_tenant_id and gateway_principal_id")
	}
	return nil
}

func normalizePlatformSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 63 {
		return ""
	}
	for index, char := range value {
		if unicode.IsLower(char) || unicode.IsDigit(char) || char == '-' {
			if char == '-' && (index == 0 || index == len(value)-1) {
				return ""
			}
			continue
		}
		return ""
	}
	return value
}

func (s *Service) auditPlatform(ctx context.Context, actor, action, resourceType, resourceID, summary string, tenant *PlatformTenant, principal *GatewayPrincipal) error {
	return s.repo.AddAuditLog(ctx, s.newPlatformAuditLog(actor, action, resourceType, resourceID, summary, tenant, principal))
}

func (s *Service) newPlatformAuditLog(actor, action, resourceType, resourceID, summary string, tenant *PlatformTenant, principal *GatewayPrincipal) AuditLog {
	if strings.TrimSpace(actor) == "" {
		actor = "local-admin"
	}
	event := AuditLog{ID: "audit_" + randomID(12), Actor: actor, Action: action, ResourceType: resourceType, ResourceID: resourceID, Summary: summary, ProfileScope: ProfileScopePlatform, CreatedAt: s.nowUTC()}
	if tenant != nil {
		event.PlatformTenantID = tenant.ID
		event.PlatformTenantName = tenant.Name
	}
	if principal != nil {
		event.GatewayPrincipalID = principal.ID
		event.GatewayPrincipalName = principal.Name
	}
	return event
}
