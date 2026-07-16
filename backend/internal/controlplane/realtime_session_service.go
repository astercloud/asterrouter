package controlplane

import (
	"context"
	"errors"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

func (s *Service) BeginRealtimeSession(ctx context.Context, operation AIOperation, attempt AIAttempt, provider GatewayProvider) (RealtimeSession, bool, error) {
	if operation.ID == "" || operation.Protocol != string(gatewaycore.ProtocolRealtime) || operation.Operation != GatewayOperationRealtimeSession ||
		operation.Modality != GatewayModalityAudio || operation.Lane != string(gatewaycore.LaneDirect) || operation.Status != AIOperationStatusRunning ||
		attempt.OperationID != operation.ID || attempt.Status != AIAttemptStatusRunning || attempt.ID == "" {
		return RealtimeSession{}, false, errors.New("invalid realtime operation or attempt")
	}
	now := s.nowUTC()
	session := RealtimeSession{
		ID: "rts_" + randomID(12), OperationID: operation.ID, AttemptID: attempt.ID,
		ProfileScope: operation.ProfileScope, TenantID: operation.TenantID, CredentialID: operation.CredentialID,
		PrincipalType: operation.PrincipalType, PrincipalID: operation.PrincipalID, Model: operation.Model,
		ProviderID: provider.ID, ProviderAccountID: provider.AccountID, UpstreamModel: provider.UpstreamModel,
		Status: RealtimeSessionStatusConnecting, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	return s.repo.CreateOrGetRealtimeSession(ctx, session)
}

func (s *Service) RealtimeSession(ctx context.Context, id string) (RealtimeSession, bool, error) {
	return s.repo.FindRealtimeSession(ctx, strings.TrimSpace(id))
}

func (s *Service) UpdateRealtimeSession(ctx context.Context, id string, expectedVersion int, update RealtimeSessionUpdate) (RealtimeSession, error) {
	update.UpdatedAt = s.nowUTC()
	updated, changed, err := s.repo.UpdateRealtimeSession(ctx, strings.TrimSpace(id), expectedVersion, update)
	if err != nil {
		return RealtimeSession{}, err
	}
	if !changed {
		return updated, ErrRealtimeSessionStateConflict
	}
	return updated, nil
}
