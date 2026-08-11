package controlplane

import (
	"context"
	"errors"
	"testing"
)

func TestPlatformCredentialRequiresApplicationPrincipalAndSnapshotsGatewayEvidence(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	if err := svc.EnsureApplicationBootstrap(ctx); err != nil {
		t.Fatalf("EnsureApplicationBootstrap(): %v", err)
	}

	if _, err := svc.CreateApplicationAPIKey(ctx, "operator", APIKeyCreateRequest{Name: "unbound", KeyType: APIKeyTypeService, ModelAllowlist: []string{"model"}}); err == nil {
		t.Fatal("CreateApplicationAPIKey() accepted a key without application/principal")
	}
	application, err := svc.CreateApplication(ctx, "operator", ApplicationRequest{Name: "Studio One", Slug: "studio-one", ConcurrencyLimit: 7})
	if err != nil {
		t.Fatalf("CreateApplication(): %v", err)
	}
	if application.ConcurrencyLimit != 7 {
		t.Fatalf("application concurrency limit=%d, want 7", application.ConcurrencyLimit)
	}
	principal, err := svc.CreateGatewayPrincipal(ctx, "operator", GatewayPrincipalRequest{ApplicationID: application.ID, Name: "Production backend", PrincipalType: GatewayPrincipalTypeService})
	if err != nil {
		t.Fatalf("CreateGatewayPrincipal(): %v", err)
	}
	created, err := svc.CreateApplicationAPIKey(ctx, "operator", APIKeyCreateRequest{
		Name: "studio-key", KeyType: APIKeyTypeService, ModelAllowlist: []string{"model"}, ApplicationID: application.ID, GatewayPrincipalID: principal.ID,
	})
	if err != nil {
		t.Fatalf("CreateApplicationAPIKey(): %v", err)
	}
	if created.Record.ApplicationID != application.ID || created.Record.GatewayPrincipalID != principal.ID {
		t.Fatalf("platform key ownership=%+v", created.Record)
	}
	if _, err := svc.UpdateAPIKey(ctx, "operator", created.Record.ID, APIKeyUpdateRequest{Name: "wrong surface", ModelAllowlist: []string{"model"}}); err == nil {
		t.Fatal("UpdateAPIKey() accepted a platform key through generic control plane")
	}
	if _, err := svc.UpdateApplicationAPIKey(ctx, "operator", created.Record.ID, APIKeyUpdateRequest{Name: "moved", ModelAllowlist: []string{"model"}, ApplicationID: defaultApplicationID}); err == nil {
		t.Fatal("UpdateApplicationAPIKey() accepted application reassignment")
	}

	auth, err := svc.AuthorizeGatewayModel(ctx, created.Key, "model")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if auth.Application == nil || auth.GatewayPrincipal == nil || auth.Application.ID != application.ID || auth.GatewayPrincipal.ID != principal.ID {
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

	usage, err := svc.UsageReportQuery(ctx, UsageQuery{ApplicationID: application.ID, GatewayPrincipalID: principal.ID})
	if err != nil || len(usage.Recent) != 1 {
		t.Fatalf("platform usage=%+v err=%v", usage, err)
	}
	if usage.Recent[0].ApplicationName != application.Name || usage.Recent[0].GatewayPrincipalName != principal.Name {
		t.Fatalf("usage snapshot=%+v", usage.Recent[0])
	}
	traces, err := svc.ListGatewayTracesQuery(ctx, GatewayTraceQuery{ApplicationID: application.ID, GatewayPrincipalID: principal.ID})
	if err != nil || len(traces) != 1 || traces[0].ApplicationName != application.Name || traces[0].GatewayPrincipalName != principal.Name {
		t.Fatalf("platform traces=%+v err=%v", traces, err)
	}
	audit, err := svc.ListAuditLogsQuery(ctx, AuditLogQuery{ApplicationID: application.ID, GatewayPrincipalID: principal.ID})
	if err != nil || len(audit) == 0 {
		t.Fatalf("platform audit=%+v err=%v", audit, err)
	}
	for _, event := range audit {
		if event.ApplicationID != application.ID || event.GatewayPrincipalID != principal.ID {
			t.Fatalf("audit snapshot=%+v", event)
		}
	}

	_, err = svc.UpdateGatewayPrincipal(ctx, "operator", principal.ID, GatewayPrincipalRequest{ApplicationID: principal.ApplicationID, Name: principal.Name, PrincipalType: principal.PrincipalType, Status: GatewayPrincipalStatusDisabled})
	if err != nil {
		t.Fatalf("disable principal: %v", err)
	}
	if _, err := svc.AuthenticateGatewayKey(ctx, created.Key); !errors.Is(err, ErrGatewayUnauthorized) {
		t.Fatalf("AuthenticateGatewayKey(disabled principal) error=%v, want ErrGatewayUnauthorized", err)
	}
}

func TestApplicationRejectsNegativeConcurrencyLimit(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	if _, err := svc.CreateApplication(context.Background(), "operator", ApplicationRequest{Name: "Invalid", Slug: "invalid", ConcurrencyLimit: -1}); err == nil {
		t.Fatal("CreateApplication() accepted a negative concurrency limit")
	}
}

func TestPlatformDomainDoesNotCreateWorkspaceUsers(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	if err := svc.EnsureApplicationBootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateApplication(ctx, "operator", ApplicationRequest{Name: "Partner", Slug: "partner"}); err != nil {
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
