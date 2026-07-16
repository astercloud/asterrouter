package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestRealtimeSessionRepositoryContract(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) Repository
	}{
		{name: "memory", open: func(*testing.T) Repository { return NewMemoryRepository() }},
		{name: "postgres", open: func(t *testing.T) Repository {
			schema := testutil.NewPostgresSchema(t)
			repo, err := NewPostgresRepository(context.Background(), schema.URL)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = repo.Close() })
			return repo
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repo := test.open(t)
			svc := NewService(repo, "/v1", "realtime-session-test-secret")
			now := time.Date(2026, time.July, 16, 8, 0, 0, 0, time.UTC)
			svc.now = func() time.Time { return now }
			requestID := testutil.UniqueID("realtime")
			request := gatewaycore.CanonicalRequest{
				ID: "op_" + requestID, ClientRequestID: requestID, Fingerprint: "fingerprint-" + requestID,
				IdempotencyKey: "idempotency-" + requestID, Protocol: gatewaycore.ProtocolRealtime,
				Operation: GatewayOperationRealtimeSession, Modality: GatewayModalityAudio, Lane: gatewaycore.LaneDirect,
				Model: "public-realtime", Stream: true, Payload: []byte(`{"model":"public-realtime"}`),
			}
			auth := gatewaycore.CanonicalAuthContext{
				CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: "credential-" + requestID,
				ProfileScope: ProfileScopePlatform, TenantID: "tenant-" + requestID,
				PrincipalType: APIKeyTypeService, PrincipalID: "principal-" + requestID,
			}
			operation, created, err := svc.BeginCanonicalOperation(ctx, auth, request)
			if err != nil || !created {
				t.Fatalf("BeginCanonicalOperation() created=%t err=%v", created, err)
			}
			if err := svc.MarkAIOperationRunning(ctx, operation.ID); err != nil {
				t.Fatal(err)
			}
			operation.Status = AIOperationStatusRunning
			provider := GatewayProvider{ID: "provider-" + requestID, AccountID: "account-" + requestID, UpstreamModel: "upstream-realtime"}
			attempt, err := svc.BeginAIAttempt(ctx, operation.ID, 1, provider)
			if err != nil {
				t.Fatal(err)
			}
			session, created, err := svc.BeginRealtimeSession(ctx, operation, attempt, provider)
			if err != nil || !created || session.Status != RealtimeSessionStatusConnecting || session.Version != 1 {
				t.Fatalf("BeginRealtimeSession() session=%+v created=%t err=%v", session, created, err)
			}
			replayed, created, err := svc.BeginRealtimeSession(ctx, operation, attempt, provider)
			if err != nil || created || replayed.ID != session.ID {
				t.Fatalf("replayed session=%+v created=%t err=%v", replayed, created, err)
			}

			now = now.Add(time.Second)
			connectedAt := now
			session, err = svc.UpdateRealtimeSession(ctx, session.ID, session.Version, RealtimeSessionUpdate{
				Status: RealtimeSessionStatusConnected, ConnectedAt: &connectedAt,
			})
			if err != nil || session.Version != 2 || session.ConnectedAt == nil {
				t.Fatalf("connected session=%+v err=%v", session, err)
			}

			now = now.Add(time.Second)
			session, err = svc.UpdateRealtimeSession(ctx, session.ID, session.Version, RealtimeSessionUpdate{
				Status: RealtimeSessionStatusConnected, ConnectedAt: &connectedAt,
				InputAudioBytes: 120, OutputAudioBytes: 80, ClientMessageCount: 3, ProviderMessageCount: 4,
				TransferBytes: 900, UsageVersion: 1,
			})
			if err != nil || session.Version != 3 || session.InputAudioBytes != 120 || session.UsageVersion != 1 {
				t.Fatalf("progress session=%+v err=%v", session, err)
			}
			if _, err := svc.UpdateRealtimeSession(ctx, session.ID, session.Version, RealtimeSessionUpdate{
				Status: RealtimeSessionStatusConnected, ConnectedAt: &connectedAt, InputAudioBytes: 119,
				OutputAudioBytes: 80, ClientMessageCount: 3, ProviderMessageCount: 4, TransferBytes: 900, UsageVersion: 1,
			}); !errors.Is(err, ErrRealtimeSessionStateConflict) {
				t.Fatalf("decreasing counter error=%v", err)
			}

			now = now.Add(time.Second)
			closedAt := now
			session, err = svc.UpdateRealtimeSession(ctx, session.ID, session.Version, RealtimeSessionUpdate{
				Status: RealtimeSessionStatusCompleted, ConnectedAt: &connectedAt, ClosedAt: &closedAt,
				InputAudioBytes: 120, OutputAudioBytes: 80, ClientMessageCount: 3, ProviderMessageCount: 4,
				TransferBytes: 900, UsageVersion: 1,
			})
			if err != nil || session.Status != RealtimeSessionStatusCompleted || session.SessionDurationMS != 2000 || session.Version != 4 {
				t.Fatalf("completed session=%+v err=%v", session, err)
			}
			persisted, found, err := svc.RealtimeSession(ctx, session.ID)
			if err != nil || !found || persisted.OperationID != operation.ID || persisted.AttemptID != attempt.ID || persisted.ProviderAccountID != provider.AccountID || persisted.SessionDurationMS != 2000 {
				t.Fatalf("persisted session=%+v found=%t err=%v", persisted, found, err)
			}
			if _, err := svc.UpdateRealtimeSession(ctx, session.ID, session.Version, RealtimeSessionUpdate{
				Status: RealtimeSessionStatusCompleted, ConnectedAt: &connectedAt, ClosedAt: &closedAt,
				InputAudioBytes: 120, OutputAudioBytes: 80, ClientMessageCount: 3, ProviderMessageCount: 4,
				TransferBytes: 900, UsageVersion: 1,
			}); !errors.Is(err, ErrRealtimeSessionStateConflict) {
				t.Fatalf("terminal update error=%v", err)
			}
		})
	}
}
