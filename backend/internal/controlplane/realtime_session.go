package controlplane

import (
	"errors"
	"strings"
	"time"
)

const (
	RealtimeSessionStatusConnecting = "connecting"
	RealtimeSessionStatusConnected  = "connected"
	RealtimeSessionStatusCompleted  = "completed"
	RealtimeSessionStatusFailed     = "failed"
	RealtimeSessionStatusCanceled   = "canceled"
)

var ErrRealtimeSessionStateConflict = errors.New("realtime session state changed concurrently")

// RealtimeSession stores control-plane facts only. WebSocket events and media
// bytes are intentionally excluded from persistent session state.
type RealtimeSession struct {
	ID                   string     `json:"id"`
	OperationID          string     `json:"operation_id"`
	AttemptID            string     `json:"attempt_id"`
	ProfileScope         string     `json:"profile_scope"`
	TenantID             string     `json:"tenant_id"`
	CredentialID         string     `json:"credential_id"`
	PrincipalType        string     `json:"principal_type"`
	PrincipalID          string     `json:"principal_id"`
	Model                string     `json:"model"`
	ProviderID           string     `json:"provider_id"`
	ProviderAccountID    string     `json:"provider_account_id"`
	UpstreamModel        string     `json:"upstream_model"`
	Status               string     `json:"status"`
	Version              int        `json:"version"`
	InputAudioBytes      int64      `json:"input_audio_bytes"`
	OutputAudioBytes     int64      `json:"output_audio_bytes"`
	ClientMessageCount   int64      `json:"client_message_count"`
	ProviderMessageCount int64      `json:"provider_message_count"`
	TransferBytes        int64      `json:"transfer_bytes"`
	UsageVersion         int        `json:"usage_version"`
	SessionDurationMS    int64      `json:"session_duration_ms"`
	ErrorType            string     `json:"error_type,omitempty"`
	ConnectedAt          *time.Time `json:"connected_at,omitempty"`
	ClosedAt             *time.Time `json:"closed_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type RealtimeSessionUpdate struct {
	Status               string
	InputAudioBytes      int64
	OutputAudioBytes     int64
	ClientMessageCount   int64
	ProviderMessageCount int64
	TransferBytes        int64
	UsageVersion         int
	ErrorType            string
	ConnectedAt          *time.Time
	ClosedAt             *time.Time
	UpdatedAt            time.Time
}

func validateRealtimeSession(session RealtimeSession) error {
	if strings.TrimSpace(session.ID) == "" || strings.TrimSpace(session.OperationID) == "" || strings.TrimSpace(session.AttemptID) == "" ||
		strings.TrimSpace(session.CredentialID) == "" || strings.TrimSpace(session.Model) == "" || session.Status != RealtimeSessionStatusConnecting ||
		session.Version != 1 || session.CreatedAt.IsZero() || session.UpdatedAt.IsZero() || session.ConnectedAt != nil || session.ClosedAt != nil ||
		session.InputAudioBytes != 0 || session.OutputAudioBytes != 0 || session.ClientMessageCount != 0 || session.ProviderMessageCount != 0 ||
		session.TransferBytes != 0 || session.UsageVersion != 0 || session.SessionDurationMS != 0 || session.ErrorType != "" {
		return errors.New("invalid realtime session")
	}
	return nil
}

func applyRealtimeSessionUpdate(current RealtimeSession, update RealtimeSessionUpdate) (RealtimeSession, error) {
	if !validRealtimeSessionTransition(current.Status, update.Status) || update.UpdatedAt.IsZero() || update.UpdatedAt.Before(current.UpdatedAt) ||
		update.InputAudioBytes < current.InputAudioBytes || update.OutputAudioBytes < current.OutputAudioBytes ||
		update.ClientMessageCount < current.ClientMessageCount || update.ProviderMessageCount < current.ProviderMessageCount ||
		update.TransferBytes < current.TransferBytes || update.UsageVersion < current.UsageVersion {
		return RealtimeSession{}, ErrRealtimeSessionStateConflict
	}
	terminal := oneOf(update.Status, RealtimeSessionStatusCompleted, RealtimeSessionStatusFailed, RealtimeSessionStatusCanceled)
	if update.Status == RealtimeSessionStatusConnected && update.ConnectedAt == nil || terminal && update.ClosedAt == nil ||
		update.Status == RealtimeSessionStatusCompleted && strings.TrimSpace(update.ErrorType) != "" ||
		update.Status == RealtimeSessionStatusFailed && strings.TrimSpace(update.ErrorType) == "" {
		return RealtimeSession{}, errors.New("invalid realtime session update")
	}
	connectedAt := cloneTimePointer(current.ConnectedAt)
	if update.ConnectedAt != nil {
		if connectedAt != nil && !connectedAt.Equal(*update.ConnectedAt) {
			return RealtimeSession{}, ErrRealtimeSessionStateConflict
		}
		connectedAt = cloneTimePointer(update.ConnectedAt)
	}
	closedAt := cloneTimePointer(update.ClosedAt)
	durationMS := int64(0)
	if closedAt != nil && connectedAt != nil {
		if closedAt.Before(*connectedAt) {
			return RealtimeSession{}, errors.New("realtime session closed before it connected")
		}
		durationMS = closedAt.Sub(*connectedAt).Milliseconds()
	}
	current.Status = update.Status
	current.Version++
	current.InputAudioBytes = update.InputAudioBytes
	current.OutputAudioBytes = update.OutputAudioBytes
	current.ClientMessageCount = update.ClientMessageCount
	current.ProviderMessageCount = update.ProviderMessageCount
	current.TransferBytes = update.TransferBytes
	current.UsageVersion = update.UsageVersion
	current.SessionDurationMS = durationMS
	current.ErrorType = strings.TrimSpace(update.ErrorType)
	current.ConnectedAt = connectedAt
	current.ClosedAt = closedAt
	current.UpdatedAt = update.UpdatedAt.UTC()
	return current, nil
}

func validRealtimeSessionTransition(from, to string) bool {
	if from == to && oneOf(from, RealtimeSessionStatusConnecting, RealtimeSessionStatusConnected) {
		return true
	}
	switch from {
	case RealtimeSessionStatusConnecting:
		return oneOf(to, RealtimeSessionStatusConnected, RealtimeSessionStatusFailed, RealtimeSessionStatusCanceled)
	case RealtimeSessionStatusConnected:
		return oneOf(to, RealtimeSessionStatusCompleted, RealtimeSessionStatusFailed, RealtimeSessionStatusCanceled)
	default:
		return false
	}
}
