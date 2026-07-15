package controlplane

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

var (
	ErrArtifactAdminQueryInvalid = errors.New("invalid artifact admin query")
	ErrArtifactDeliveryRetry     = errors.New("artifact delivery cannot be retried")
)

type ArtifactSummary struct {
	Total     int64            `json:"total"`
	SizeBytes int64            `json:"size_bytes"`
	ByStatus  map[string]int64 `json:"by_status"`
}

type ArtifactAdminRecord struct {
	ID               string     `json:"id"`
	OperationID      string     `json:"operation_id"`
	JobID            string     `json:"job_id,omitempty"`
	AttemptID        string     `json:"attempt_id,omitempty"`
	SourceArtifactID string     `json:"source_artifact_id,omitempty"`
	ProfileScope     string     `json:"profile_scope"`
	TenantID         string     `json:"tenant_id,omitempty"`
	Role             string     `json:"role"`
	Policy           string     `json:"policy"`
	Status           string     `json:"status"`
	StatusVersion    int        `json:"status_version"`
	MediaType        string     `json:"media_type,omitempty"`
	SizeBytes        int64      `json:"size_bytes"`
	SHA256           string     `json:"sha256,omitempty"`
	StoreDriver      string     `json:"store_driver"`
	ErrorType        string     `json:"error_type,omitempty"`
	SinkID           string     `json:"sink_id,omitempty"`
	ProviderID       string     `json:"provider_id,omitempty"`
	RuntimeStatus    string     `json:"runtime_status,omitempty"`
	RetainUntil      time.Time  `json:"retain_until"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	ReadyAt          *time.Time `json:"ready_at,omitempty"`
	DeliveredAt      *time.Time `json:"delivered_at,omitempty"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty"`
}

type ArtifactAdminDetail struct {
	Artifact ArtifactAdminRecord `json:"artifact"`
	Events   []ArtifactEvent     `json:"events"`
}

type ArtifactRuntime struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Status string `json:"status"`
}

type ArtifactDeliveryRetryResult struct {
	ArtifactID  string    `json:"artifact_id"`
	AttemptID   string    `json:"attempt_id"`
	Status      string    `json:"status"`
	ScheduledAt time.Time `json:"scheduled_at"`
}

func newArtifactSummary() ArtifactSummary {
	return ArtifactSummary{ByStatus: map[string]int64{}}
}

func (s *Service) ListArtifactsAdmin(ctx context.Context, query ArtifactQuery) ([]ArtifactAdminRecord, error) {
	if err := validateArtifactAdminQuery(query); err != nil {
		return nil, err
	}
	artifacts, err := s.repo.QueryArtifacts(ctx, query)
	if err != nil {
		return nil, err
	}
	result := make([]ArtifactAdminRecord, 0, len(artifacts))
	for _, artifact := range artifacts {
		record, err := s.artifactAdminRecord(ctx, artifact)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, nil
}

func (s *Service) ArtifactSummaryAdmin(ctx context.Context, query ArtifactQuery) (ArtifactSummary, error) {
	if err := validateArtifactAdminQuery(query); err != nil {
		return ArtifactSummary{}, err
	}
	query.Limit = 0
	query.Offset = 0
	return s.repo.SummarizeArtifacts(ctx, query)
}

func (s *Service) ArtifactAdmin(ctx context.Context, id string) (ArtifactAdminDetail, error) {
	artifact, found, err := s.repo.FindArtifact(ctx, strings.TrimSpace(id))
	if err != nil {
		return ArtifactAdminDetail{}, err
	}
	if !found {
		return ArtifactAdminDetail{}, ErrArtifactNotFound
	}
	record, err := s.artifactAdminRecord(ctx, artifact)
	if err != nil {
		return ArtifactAdminDetail{}, err
	}
	events, err := s.repo.ListArtifactEvents(ctx, artifact.ID)
	if err != nil {
		return ArtifactAdminDetail{}, err
	}
	return ArtifactAdminDetail{Artifact: record, Events: events}, nil
}

func (s *Service) ArtifactRuntimes() []ArtifactRuntime {
	s.artifactSinkMu.RLock()
	runtimes := make([]ArtifactRuntime, 0, len(s.artifactSinks))
	for id := range s.artifactSinks {
		runtimes = append(runtimes, ArtifactRuntime{Kind: "sink", ID: id, Status: "registered"})
	}
	s.artifactSinkMu.RUnlock()
	s.artifactProxyMu.RLock()
	for id := range s.artifactProxies {
		runtimes = append(runtimes, ArtifactRuntime{Kind: "proxy", ID: id, Status: "registered"})
	}
	s.artifactProxyMu.RUnlock()
	sort.Slice(runtimes, func(i, j int) bool {
		if runtimes[i].Kind == runtimes[j].Kind {
			return runtimes[i].ID < runtimes[j].ID
		}
		return runtimes[i].Kind < runtimes[j].Kind
	})
	return runtimes
}

func (s *Service) RetryArtifactDelivery(ctx context.Context, actor, id string) (ArtifactDeliveryRetryResult, error) {
	artifact, found, err := s.repo.FindArtifact(ctx, strings.TrimSpace(id))
	if err != nil {
		return ArtifactDeliveryRetryResult{}, err
	}
	if !found {
		return ArtifactDeliveryRetryResult{}, ErrArtifactNotFound
	}
	if artifact.Policy != GatewayArtifactPolicyCustomerSink || artifact.Status != ArtifactStatusDeliveryFailed || !artifact.RetainUntil.After(s.nowUTC()) {
		return ArtifactDeliveryRetryResult{}, ErrArtifactDeliveryRetry
	}
	job, found, err := s.repo.FindAIJob(ctx, artifact.JobID)
	if err != nil {
		return ArtifactDeliveryRetryResult{}, err
	}
	if !found || job.OperationID != artifact.OperationID || job.ArtifactPolicy != artifact.Policy || strings.TrimSpace(job.ArtifactSinkID) == "" {
		return ArtifactDeliveryRetryResult{}, ErrArtifactDeliveryRetry
	}
	sink, registered := s.artifactSink(job.ArtifactSinkID)
	if !registered || !sink.Accepts(artifactOwnerFromJob(job)) {
		return ArtifactDeliveryRetryResult{}, ErrArtifactSinkRequired
	}
	attempt, found, err := s.repo.FindAIAttempt(ctx, artifact.AttemptID)
	if err != nil {
		return ArtifactDeliveryRetryResult{}, err
	}
	if !found || attempt.OperationID != artifact.OperationID || attempt.Status != AIAttemptStatusRunning ||
		!oneOf(attempt.DispatchState, AIAttemptDispatchSubmitted, AIAttemptDispatchAccepted, AIAttemptDispatchUnknown) {
		return ArtifactDeliveryRetryResult{}, ErrArtifactDeliveryRetry
	}
	now := s.nowUTC()
	requested := attempt
	requested.ReconcileAfter = &now
	requested.UpdatedAt = now
	audit := s.newAuditLog(actor, "retry_delivery", "artifact", artifact.ID, "Scheduled failed artifact delivery for retry")
	if _, changed, err := s.repo.ScheduleArtifactDeliveryRetry(ctx, artifact.ID, requested, attempt.DispatchVersion, audit); err != nil {
		return ArtifactDeliveryRetryResult{}, err
	} else if !changed {
		return ArtifactDeliveryRetryResult{}, ErrAIAttemptDispatchState
	}
	return ArtifactDeliveryRetryResult{ArtifactID: artifact.ID, AttemptID: attempt.ID, Status: "scheduled", ScheduledAt: now}, nil
}

func validateArtifactAdminQuery(query ArtifactQuery) error {
	if strings.TrimSpace(query.Role) != "" && !validArtifactRole(strings.TrimSpace(query.Role)) {
		return ErrArtifactAdminQueryInvalid
	}
	if strings.TrimSpace(query.Policy) != "" && !validArtifactPolicy(strings.TrimSpace(query.Policy)) {
		return ErrArtifactAdminQueryInvalid
	}
	if strings.TrimSpace(query.Status) != "" && !validArtifactStatus(strings.TrimSpace(query.Status)) {
		return ErrArtifactAdminQueryInvalid
	}
	if query.Offset < 0 || query.Limit < 0 {
		return ErrArtifactAdminQueryInvalid
	}
	return nil
}

func validArtifactStatus(status string) bool {
	return oneOf(status, ArtifactStatusPending, ArtifactStatusUploading, ArtifactStatusReady, ArtifactStatusFailed,
		ArtifactStatusDelivering, ArtifactStatusDelivered, ArtifactStatusDeliveryFailed, ArtifactStatusDeleteRequested,
		ArtifactStatusDeleting, ArtifactStatusDeleted, ArtifactStatusDeleteFailed, ArtifactStatusExpired)
}

func (s *Service) artifactAdminRecord(ctx context.Context, artifact Artifact) (ArtifactAdminRecord, error) {
	record := ArtifactAdminRecord{
		ID: artifact.ID, OperationID: artifact.OperationID, JobID: artifact.JobID, AttemptID: artifact.AttemptID,
		SourceArtifactID: artifact.SourceArtifactID, ProfileScope: artifact.ProfileScope, TenantID: artifact.TenantID,
		Role: artifact.Role, Policy: artifact.Policy, Status: artifact.Status, StatusVersion: artifact.StatusVersion,
		MediaType: artifact.MediaType, SizeBytes: artifact.SizeBytes, SHA256: artifact.SHA256, StoreDriver: artifact.StoreDriver,
		ErrorType: artifact.ErrorType, RetainUntil: artifact.RetainUntil, CreatedAt: artifact.CreatedAt, UpdatedAt: artifact.UpdatedAt,
		ReadyAt: artifact.ReadyAt, DeliveredAt: artifact.DeliveredAt, DeletedAt: artifact.DeletedAt,
	}
	if artifact.JobID != "" {
		job, found, err := s.repo.FindAIJob(ctx, artifact.JobID)
		if err != nil {
			return ArtifactAdminRecord{}, err
		}
		if found && job.OperationID == artifact.OperationID {
			record.SinkID = job.ArtifactSinkID
			if record.SinkID != "" {
				if _, registered := s.artifactSink(record.SinkID); registered {
					record.RuntimeStatus = "registered"
				} else {
					record.RuntimeStatus = "unavailable"
				}
			}
		}
	}
	if artifact.AttemptID != "" {
		attempt, found, err := s.repo.FindAIAttempt(ctx, artifact.AttemptID)
		if err != nil {
			return ArtifactAdminRecord{}, err
		}
		if found && attempt.OperationID == artifact.OperationID {
			record.ProviderID = attempt.ProviderID
			if artifact.Policy == GatewayArtifactPolicyProxyOnly {
				if _, registered := s.artifactProxy(attempt.ProviderID); registered {
					record.RuntimeStatus = "registered"
				} else {
					record.RuntimeStatus = "unavailable"
				}
			}
		}
	}
	return record, nil
}
