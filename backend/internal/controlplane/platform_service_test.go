package controlplane

import (
	"context"
	"errors"
	"testing"
)

func TestPlatformCredentialRequiresTenantPrincipalAndSnapshotsGatewayEvidence(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	if err := svc.EnsurePlatformBootstrap(ctx); err != nil {
		t.Fatalf("EnsurePlatformBootstrap(): %v", err)
	}

	if _, err := svc.CreatePlatformAPIKey(ctx, "operator", APIKeyCreateRequest{Name: "unbound", KeyType: APIKeyTypeService, ModelAllowlist: []string{"model"}}); err == nil {
		t.Fatal("CreatePlatformAPIKey() accepted a key without tenant/principal")
	}
	tenant, err := svc.CreatePlatformTenant(ctx, "operator", PlatformTenantRequest{Name: "Studio One", Slug: "studio-one"})
	if err != nil {
		t.Fatalf("CreatePlatformTenant(): %v", err)
	}
	principal, err := svc.CreateGatewayPrincipal(ctx, "operator", GatewayPrincipalRequest{TenantID: tenant.ID, Name: "Production backend", PrincipalType: GatewayPrincipalTypeService})
	if err != nil {
		t.Fatalf("CreateGatewayPrincipal(): %v", err)
	}
	created, err := svc.CreatePlatformAPIKey(ctx, "operator", APIKeyCreateRequest{
		Name: "studio-key", KeyType: APIKeyTypeService, ModelAllowlist: []string{"model"}, PlatformTenantID: tenant.ID, GatewayPrincipalID: principal.ID,
	})
	if err != nil {
		t.Fatalf("CreatePlatformAPIKey(): %v", err)
	}
	if created.Record.ProfileScope != ProfileScopePlatform || created.Record.PlatformTenantID != tenant.ID || created.Record.GatewayPrincipalID != principal.ID {
		t.Fatalf("platform key ownership=%+v", created.Record)
	}
	if _, err := svc.UpdateAPIKey(ctx, "operator", created.Record.ID, APIKeyUpdateRequest{Name: "wrong surface", ModelAllowlist: []string{"model"}}); err == nil {
		t.Fatal("UpdateAPIKey() accepted a platform key through generic control plane")
	}
	if _, err := svc.UpdatePlatformAPIKey(ctx, "operator", created.Record.ID, APIKeyUpdateRequest{Name: "moved", ModelAllowlist: []string{"model"}, PlatformTenantID: defaultPlatformTenantID}); err == nil {
		t.Fatal("UpdatePlatformAPIKey() accepted tenant reassignment")
	}

	auth, err := svc.AuthorizeGatewayModel(ctx, created.Key, "model")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if auth.PlatformTenant == nil || auth.GatewayPrincipal == nil || auth.PlatformTenant.ID != tenant.ID || auth.GatewayPrincipal.ID != principal.ID {
		t.Fatalf("gateway auth platform context=%+v", auth)
	}
	if err := svc.RecordGatewayCall(ctx, auth, "model", "forwarded", "test platform call"); err != nil {
		t.Fatalf("RecordGatewayCall(): %v", err)
	}
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "model", Status: "forwarded", InputTokens: 3, OutputTokens: 5}); err != nil {
		t.Fatalf("RecordGatewayUsage(): %v", err)
	}
	if err := svc.RecordGatewayTrace(ctx, auth, GatewayTraceInput{Model: "model", Status: "forwarded"}); err != nil {
		t.Fatalf("RecordGatewayTrace(): %v", err)
	}

	usage, err := svc.UsageReportQuery(ctx, UsageQuery{ProfileScope: ProfileScopePlatform, PlatformTenantID: tenant.ID, GatewayPrincipalID: principal.ID})
	if err != nil || len(usage.Recent) != 1 {
		t.Fatalf("platform usage=%+v err=%v", usage, err)
	}
	if usage.Recent[0].PlatformTenantName != tenant.Name || usage.Recent[0].GatewayPrincipalName != principal.Name {
		t.Fatalf("usage snapshot=%+v", usage.Recent[0])
	}
	traces, err := svc.ListGatewayTracesQuery(ctx, GatewayTraceQuery{ProfileScope: ProfileScopePlatform, PlatformTenantID: tenant.ID, GatewayPrincipalID: principal.ID})
	if err != nil || len(traces) != 1 || traces[0].PlatformTenantName != tenant.Name || traces[0].GatewayPrincipalName != principal.Name {
		t.Fatalf("platform traces=%+v err=%v", traces, err)
	}
	audit, err := svc.ListAuditLogsQuery(ctx, AuditLogQuery{ProfileScope: ProfileScopePlatform, PlatformTenantID: tenant.ID, GatewayPrincipalID: principal.ID})
	if err != nil || len(audit) == 0 {
		t.Fatalf("platform audit=%+v err=%v", audit, err)
	}
	for _, event := range audit {
		if event.PlatformTenantID != tenant.ID || event.GatewayPrincipalID != principal.ID {
			t.Fatalf("audit snapshot=%+v", event)
		}
	}

	_, err = svc.UpdateGatewayPrincipal(ctx, "operator", principal.ID, GatewayPrincipalRequest{TenantID: principal.TenantID, Name: principal.Name, PrincipalType: principal.PrincipalType, Status: GatewayPrincipalStatusDisabled})
	if err != nil {
		t.Fatalf("disable principal: %v", err)
	}
	if _, err := svc.AuthenticateGatewayKey(ctx, created.Key); !errors.Is(err, ErrGatewayUnauthorized) {
		t.Fatalf("AuthenticateGatewayKey(disabled principal) error=%v, want ErrGatewayUnauthorized", err)
	}
}

func TestPlatformDomainDoesNotCreateWorkspaceUsers(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	if err := svc.EnsurePlatformBootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreatePlatformTenant(ctx, "operator", PlatformTenantRequest{Name: "Partner", Slug: "partner"}); err != nil {
		t.Fatal(err)
	}
	users, err := svc.ListWorkspaceUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 0 {
		t.Fatalf("platform domain created workspace users: %+v", users)
	}
}
