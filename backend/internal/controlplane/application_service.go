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
	defaultApplicationID   = "app_default"
	defaultPrincipalID     = "prn_default_service"
	defaultApplicationSlug = "default"
	defaultApplicationName = "Default Application"
	defaultPrincipalName   = "Default Service"
)

type applicationCredentialIdentity struct {
	application Application
	principal   GatewayPrincipal
}

// EnsureApplicationBootstrap creates the default enterprise application and
// service principal used by organization-scoped credentials.
func (s *Service) EnsureApplicationBootstrap(ctx context.Context) error {
	applications, err := s.repo.ListApplications(ctx)
	if err != nil {
		return err
	}
	var application Application
	foundApplication := false
	for _, item := range applications {
		if item.ID == defaultApplicationID || item.Slug == defaultApplicationSlug {
			application = item
			foundApplication = true
			break
		}
	}
	if !foundApplication {
		now := s.nowUTC()
		application = Application{
			ID: defaultApplicationID, Name: defaultApplicationName, Slug: defaultApplicationSlug,
			Status: ApplicationStatusActive, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.repo.SaveApplication(ctx, application); err != nil {
			return err
		}
	}

	principals, err := s.repo.ListGatewayPrincipals(ctx)
	if err != nil {
		return err
	}
	for _, principal := range principals {
		if principal.ID == defaultPrincipalID {
			return nil
		}
	}
	now := s.nowUTC()
	return s.repo.SaveGatewayPrincipal(ctx, GatewayPrincipal{
		ID: defaultPrincipalID, ApplicationID: application.ID, Name: defaultPrincipalName,
		PrincipalType: GatewayPrincipalTypeService, Status: GatewayPrincipalStatusActive,
		CreatedAt: now, UpdatedAt: now,
	})
}

func (s *Service) ListApplications(ctx context.Context) ([]Application, error) {
	return s.repo.ListApplications(ctx)
}

func (s *Service) CreateApplication(ctx context.Context, actor string, req ApplicationRequest) (Application, error) {
	application, err := applicationFromRequest(req, s.nowUTC())
	if err != nil {
		return Application{}, err
	}
	if err := s.requireApplicationSlugAvailable(ctx, application.Slug, ""); err != nil {
		return Application{}, err
	}
	application.ID = "app_" + randomID(10)
	if err := s.repo.SaveApplication(ctx, application); err != nil {
		return Application{}, err
	}
	if err := s.auditApplication(ctx, actor, "create", "application", application.ID, fmt.Sprintf("Created application %s", application.Name), &application, nil); err != nil {
		return Application{}, err
	}
	return application, nil
}

func (s *Service) UpdateApplication(ctx context.Context, actor, id string, req ApplicationRequest) (Application, error) {
	existing, err := s.applicationByID(ctx, id)
	if err != nil {
		return Application{}, err
	}
	application, err := applicationFromRequest(req, existing.CreatedAt)
	if err != nil {
		return Application{}, err
	}
	if err := s.requireApplicationSlugAvailable(ctx, application.Slug, existing.ID); err != nil {
		return Application{}, err
	}
	application.ID = existing.ID
	application.UpdatedAt = s.nowUTC()
	if err := s.repo.SaveApplication(ctx, application); err != nil {
		return Application{}, err
	}
	if err := s.auditApplication(ctx, actor, "update", "application", application.ID, fmt.Sprintf("Updated application %s", application.Name), &application, nil); err != nil {
		return Application{}, err
	}
	return application, nil
}

func (s *Service) ListGatewayPrincipals(ctx context.Context) ([]GatewayPrincipal, error) {
	return s.repo.ListGatewayPrincipals(ctx)
}

func (s *Service) CreateGatewayPrincipal(ctx context.Context, actor string, req GatewayPrincipalRequest) (GatewayPrincipal, error) {
	principal, err := gatewayPrincipalFromRequest(req, s.nowUTC())
	if err != nil {
		return GatewayPrincipal{}, err
	}
	application, err := s.activeApplicationByID(ctx, principal.ApplicationID)
	if err != nil {
		return GatewayPrincipal{}, err
	}
	if err := s.requireGatewayPrincipalNameAvailable(ctx, principal.ApplicationID, principal.Name, ""); err != nil {
		return GatewayPrincipal{}, err
	}
	principal.ID = "prn_" + randomID(10)
	if err := s.repo.SaveGatewayPrincipal(ctx, principal); err != nil {
		return GatewayPrincipal{}, err
	}
	if err := s.auditApplication(ctx, actor, "create", "gateway_principal", principal.ID, fmt.Sprintf("Created gateway principal %s", principal.Name), &application, &principal); err != nil {
		return GatewayPrincipal{}, err
	}
	return principal, nil
}

func (s *Service) UpdateGatewayPrincipal(ctx context.Context, actor, id string, req GatewayPrincipalRequest) (GatewayPrincipal, error) {
	existing, err := s.gatewayPrincipalByID(ctx, id)
	if err != nil {
		return GatewayPrincipal{}, err
	}
	if strings.TrimSpace(req.ApplicationID) == "" {
		req.ApplicationID = existing.ApplicationID
	}
	if strings.TrimSpace(req.ApplicationID) != existing.ApplicationID {
		return GatewayPrincipal{}, errors.New("gateway principal application_id is immutable")
	}
	principal, err := gatewayPrincipalFromRequest(req, existing.CreatedAt)
	if err != nil {
		return GatewayPrincipal{}, err
	}
	application, err := s.applicationByID(ctx, principal.ApplicationID)
	if err != nil {
		return GatewayPrincipal{}, err
	}
	if err := s.requireGatewayPrincipalNameAvailable(ctx, principal.ApplicationID, principal.Name, existing.ID); err != nil {
		return GatewayPrincipal{}, err
	}
	principal.ID = existing.ID
	principal.UpdatedAt = s.nowUTC()
	if err := s.repo.SaveGatewayPrincipal(ctx, principal); err != nil {
		return GatewayPrincipal{}, err
	}
	if err := s.auditApplication(ctx, actor, "update", "gateway_principal", principal.ID, fmt.Sprintf("Updated gateway principal %s", principal.Name), &application, &principal); err != nil {
		return GatewayPrincipal{}, err
	}
	return principal, nil
}

func (s *Service) CreateApplicationAPIKey(ctx context.Context, actor string, req APIKeyCreateRequest) (APIKeyCreateResponse, error) {
	if err := validateApplicationKeyRequestOwnership(req); err != nil {
		return APIKeyCreateResponse{}, err
	}
	identity, err := s.activeApplicationCredentialIdentity(ctx, req.ApplicationID, req.GatewayPrincipalID)
	if err != nil {
		return APIKeyCreateResponse{}, err
	}
	return s.createAPIKey(ctx, actor, req, &identity)
}

func (s *Service) UpdateApplicationAPIKey(ctx context.Context, actor, id string, req APIKeyUpdateRequest) (APIKeyRecord, error) {
	key, err := s.apiKeyByID(ctx, id)
	if err != nil {
		return APIKeyRecord{}, err
	}
	if !isApplicationAPIKey(key) {
		return APIKeyRecord{}, errors.New("application API key not found")
	}
	if strings.TrimSpace(req.KeyType) != "" && req.KeyType != APIKeyTypeWorkspace && req.KeyType != APIKeyTypeService {
		return APIKeyRecord{}, errors.New("application API keys must use workspace or service ownership")
	}
	if strings.TrimSpace(req.OwnerUserID) != "" {
		return APIKeyRecord{}, errors.New("application API keys cannot reference relay customers or enterprise users")
	}
	if strings.TrimSpace(req.ApplicationID) != "" && strings.TrimSpace(req.ApplicationID) != key.ApplicationID {
		return APIKeyRecord{}, errors.New("application API key application ownership is immutable")
	}
	if strings.TrimSpace(req.GatewayPrincipalID) != "" && strings.TrimSpace(req.GatewayPrincipalID) != key.GatewayPrincipalID {
		return APIKeyRecord{}, errors.New("application API key principal ownership is immutable")
	}
	if _, err := s.activeApplicationCredentialIdentity(ctx, key.ApplicationID, key.GatewayPrincipalID); err != nil {
		return APIKeyRecord{}, err
	}
	return s.updateAPIKey(ctx, actor, key, req)
}

func (s *Service) RotateApplicationAPIKey(ctx context.Context, actor, id string) (APIKeyCreateResponse, error) {
	return s.RotateApplicationAPIKeyWithGrace(ctx, actor, id, 0)
}

func (s *Service) RotateApplicationAPIKeyWithGrace(ctx context.Context, actor, id string, gracePeriodSeconds int) (APIKeyCreateResponse, error) {
	if gracePeriodSeconds < 0 || gracePeriodSeconds > 86400 {
		return APIKeyCreateResponse{}, errors.New("grace_period_seconds must be between 0 and 86400")
	}
	key, err := s.apiKeyByID(ctx, id)
	if err != nil {
		return APIKeyCreateResponse{}, err
	}
	if !isApplicationAPIKey(key) {
		return APIKeyCreateResponse{}, errors.New("application API key not found")
	}
	identity, err := s.activeApplicationCredentialIdentity(ctx, key.ApplicationID, key.GatewayPrincipalID)
	if err != nil {
		return APIKeyCreateResponse{}, err
	}
	return s.rotateAPIKey(ctx, actor, key, &identity, time.Duration(gracePeriodSeconds)*time.Second)
}

func (s *Service) DisableApplicationAPIKey(ctx context.Context, actor, id string) error {
	key, err := s.apiKeyByID(ctx, id)
	if err != nil {
		return err
	}
	if !isApplicationAPIKey(key) {
		return errors.New("application API key not found")
	}
	identity, err := s.applicationCredentialIdentity(ctx, key.ApplicationID, key.GatewayPrincipalID)
	if err != nil {
		return err
	}
	if err := s.repo.DisableAPIKey(ctx, key.ID, s.nowUTC()); err != nil {
		return err
	}
	return s.auditApplication(ctx, actor, "disable", "api_key", key.ID, "Disabled application API key", &identity.application, &identity.principal)
}

func (s *Service) activeApplicationCredentialIdentity(ctx context.Context, applicationID, principalID string) (applicationCredentialIdentity, error) {
	identity, err := s.applicationCredentialIdentity(ctx, applicationID, principalID)
	if err != nil {
		return applicationCredentialIdentity{}, err
	}
	if identity.application.Status != ApplicationStatusActive || identity.principal.Status != GatewayPrincipalStatusActive {
		return applicationCredentialIdentity{}, errors.New("gateway principal is not active for application")
	}
	return identity, nil
}

func (s *Service) applicationCredentialIdentity(ctx context.Context, applicationID, principalID string) (applicationCredentialIdentity, error) {
	application, err := s.applicationByID(ctx, applicationID)
	if err != nil {
		return applicationCredentialIdentity{}, err
	}
	principal, err := s.gatewayPrincipalByID(ctx, principalID)
	if err != nil {
		return applicationCredentialIdentity{}, err
	}
	if principal.ApplicationID != application.ID {
		return applicationCredentialIdentity{}, errors.New("gateway principal does not belong to application")
	}
	return applicationCredentialIdentity{application: application, principal: principal}, nil
}

func (s *Service) activeApplicationByID(ctx context.Context, id string) (Application, error) {
	application, err := s.applicationByID(ctx, id)
	if err != nil {
		return Application{}, err
	}
	if application.Status != ApplicationStatusActive {
		return Application{}, errors.New("application is not active")
	}
	return application, nil
}

func (s *Service) applicationByID(ctx context.Context, id string) (Application, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Application{}, errors.New("application_id is required")
	}
	applications, err := s.repo.ListApplications(ctx)
	if err != nil {
		return Application{}, err
	}
	for _, application := range applications {
		if application.ID == id {
			return application, nil
		}
	}
	return Application{}, errors.New("application not found")
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

func (s *Service) requireApplicationSlugAvailable(ctx context.Context, slug, exceptID string) error {
	applications, err := s.repo.ListApplications(ctx)
	if err != nil {
		return err
	}
	for _, application := range applications {
		if application.ID != exceptID && application.Slug == slug {
			return errors.New("application slug already exists")
		}
	}
	return nil
}

func (s *Service) requireGatewayPrincipalNameAvailable(ctx context.Context, applicationID, name, exceptID string) error {
	principals, err := s.repo.ListGatewayPrincipals(ctx)
	if err != nil {
		return err
	}
	for _, principal := range principals {
		if principal.ID != exceptID && principal.ApplicationID == applicationID && strings.EqualFold(principal.Name, name) {
			return errors.New("gateway principal name already exists for application")
		}
	}
	return nil
}

func applicationFromRequest(req ApplicationRequest, createdAt time.Time) (Application, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 120 {
		return Application{}, errors.New("application name must contain 1 to 120 characters")
	}
	slug := normalizeApplicationSlug(req.Slug)
	if slug == "" {
		return Application{}, errors.New("application slug must contain lowercase letters, digits, or hyphens")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = ApplicationStatusActive
	}
	if !oneOf(status, ApplicationStatusActive, ApplicationStatusDisabled) {
		return Application{}, errors.New("application status must be active or disabled")
	}
	if req.ConcurrencyLimit < 0 {
		return Application{}, errors.New("application concurrency_limit must be non-negative")
	}
	return Application{Name: name, Slug: slug, EntitlementReference: strings.TrimSpace(req.EntitlementReference), ConcurrencyLimit: req.ConcurrencyLimit, Status: status, CreatedAt: createdAt, UpdatedAt: createdAt}, nil
}

func gatewayPrincipalFromRequest(req GatewayPrincipalRequest, createdAt time.Time) (GatewayPrincipal, error) {
	applicationID := strings.TrimSpace(req.ApplicationID)
	if applicationID == "" {
		return GatewayPrincipal{}, errors.New("gateway principal application_id is required")
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
	return GatewayPrincipal{ApplicationID: applicationID, Name: name, PrincipalType: principalType, ExternalSubjectReference: strings.TrimSpace(req.ExternalSubjectReference), Status: status, CreatedAt: createdAt, UpdatedAt: createdAt}, nil
}

func validateApplicationKeyRequestOwnership(req APIKeyCreateRequest) error {
	keyType := strings.TrimSpace(req.KeyType)
	if keyType != "" && keyType != APIKeyTypeWorkspace && keyType != APIKeyTypeService {
		return errors.New("application API keys must use workspace or service ownership")
	}
	if strings.TrimSpace(req.OwnerUserID) != "" {
		return errors.New("application API keys cannot be owned by individual users")
	}
	if strings.TrimSpace(req.ApplicationID) == "" || strings.TrimSpace(req.GatewayPrincipalID) == "" {
		return errors.New("application API keys require application_id and gateway_principal_id")
	}
	return nil
}

func normalizeApplicationSlug(value string) string {
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

func (s *Service) auditApplication(ctx context.Context, actor, action, resourceType, resourceID, summary string, application *Application, principal *GatewayPrincipal) error {
	return s.repo.AddAuditLog(ctx, s.newApplicationAuditLog(actor, action, resourceType, resourceID, summary, application, principal))
}

func (s *Service) newApplicationAuditLog(actor, action, resourceType, resourceID, summary string, application *Application, principal *GatewayPrincipal) AuditLog {
	if strings.TrimSpace(actor) == "" {
		actor = "local-admin"
	}
	event := AuditLog{ID: "audit_" + randomID(12), Actor: actor, Action: action, ResourceType: resourceType, ResourceID: resourceID, Summary: summary, CreatedAt: s.nowUTC()}
	if application != nil {
		event.ApplicationID = application.ID
		event.ApplicationName = application.Name
	}
	if principal != nil {
		event.GatewayPrincipalID = principal.ID
		event.GatewayPrincipalName = principal.Name
	}
	return event
}
