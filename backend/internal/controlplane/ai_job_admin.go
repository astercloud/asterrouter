package controlplane

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrAIJobAdminQueryInvalid       = errors.New("invalid ai job admin query")
	ErrAIJobNotFound                = errors.New("ai job not found")
	ErrAIAttemptReconcileScheduling = errors.New("ai attempt cannot be scheduled for reconciliation")
)

type AIJobQuery struct {
	Search         string
	ProfileScope   string
	TenantID       string
	Model          string
	Modality       string
	Operation      string
	Status         string
	ArtifactPolicy string
	Limit          int
	Offset         int
}

type AIJobSummary struct {
	Total    int64            `json:"total"`
	ByStatus map[string]int64 `json:"by_status"`
}

type AIJobAdminRecord struct {
	ID             string     `json:"id"`
	OperationID    string     `json:"operation_id"`
	ProfileScope   string     `json:"profile_scope"`
	TenantID       string     `json:"tenant_id,omitempty"`
	Protocol       string     `json:"protocol"`
	Operation      string     `json:"operation"`
	Modality       string     `json:"modality"`
	Model          string     `json:"model"`
	ArtifactPolicy string     `json:"artifact_policy"`
	ArtifactSinkID string     `json:"artifact_sink_id,omitempty"`
	Status         string     `json:"status"`
	StatusVersion  int        `json:"status_version"`
	Priority       int        `json:"priority"`
	NextEligibleAt time.Time  `json:"next_eligible_at"`
	ErrorType      string     `json:"error_type,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	ExpiresAt      time.Time  `json:"expires_at"`
}

type AIAttemptAdminRecord struct {
	ID                  string     `json:"id"`
	AttemptNumber       int        `json:"attempt_number"`
	ProviderID          string     `json:"provider_id"`
	ProviderAccountID   string     `json:"provider_account_id"`
	ProviderAdapterID   string     `json:"provider_adapter_id"`
	RouteID             string     `json:"route_id"`
	UpstreamModel       string     `json:"upstream_model"`
	Status              string     `json:"status"`
	ErrorType           string     `json:"error_type,omitempty"`
	DispatchState       string     `json:"dispatch_state"`
	DispatchVersion     int        `json:"dispatch_version"`
	ProviderTaskID      string     `json:"provider_task_id,omitempty"`
	ProviderTaskStatus  string     `json:"provider_task_status,omitempty"`
	DispatchSubmittedAt *time.Time `json:"dispatch_submitted_at,omitempty"`
	ProviderAcceptedAt  *time.Time `json:"provider_accepted_at,omitempty"`
	LastReconciledAt    *time.Time `json:"last_reconciled_at,omitempty"`
	ReconcileAfter      *time.Time `json:"reconcile_after,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
}

type AIJobAdminDetail struct {
	Job       AIJobAdminRecord       `json:"job"`
	Attempts  []AIAttemptAdminRecord `json:"attempts"`
	Events    []AIJobEvent           `json:"events"`
	Artifacts []ArtifactAdminRecord  `json:"artifacts"`
}

type AIJobAdminActionResult struct {
	JobID     string    `json:"job_id"`
	Status    string    `json:"status"`
	Changed   bool      `json:"changed"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AIAttemptReconcileScheduleResult struct {
	JobID       string    `json:"job_id"`
	AttemptID   string    `json:"attempt_id"`
	Status      string    `json:"status"`
	ScheduledAt time.Time `json:"scheduled_at"`
}

func newAIJobSummary() AIJobSummary {
	return AIJobSummary{ByStatus: map[string]int64{}}
}

func (s *Service) ListAIJobsAdmin(ctx context.Context, query AIJobQuery) ([]AIJobAdminRecord, error) {
	if err := validateAIJobAdminQuery(query); err != nil {
		return nil, err
	}
	jobs, err := s.repo.QueryAIJobs(ctx, query)
	if err != nil {
		return nil, err
	}
	result := make([]AIJobAdminRecord, 0, len(jobs))
	for _, job := range jobs {
		result = append(result, aiJobAdminRecord(job))
	}
	return result, nil
}

func (s *Service) AIJobSummaryAdmin(ctx context.Context, query AIJobQuery) (AIJobSummary, error) {
	if err := validateAIJobAdminQuery(query); err != nil {
		return AIJobSummary{}, err
	}
	query.Limit = 0
	query.Offset = 0
	return s.repo.SummarizeAIJobs(ctx, query)
}

func (s *Service) AIJobAdmin(ctx context.Context, id string) (AIJobAdminDetail, error) {
	job, found, err := s.repo.FindAIJob(ctx, strings.TrimSpace(id))
	if err != nil {
		return AIJobAdminDetail{}, err
	}
	if !found {
		return AIJobAdminDetail{}, ErrAIJobNotFound
	}
	attempts, err := s.repo.ListAIAttemptsByOperationID(ctx, job.OperationID)
	if err != nil {
		return AIJobAdminDetail{}, err
	}
	events, err := s.repo.ListAIJobEvents(ctx, job.ID)
	if err != nil {
		return AIJobAdminDetail{}, err
	}
	artifacts, err := s.repo.QueryArtifacts(ctx, ArtifactQuery{JobID: job.ID, Limit: 100})
	if err != nil {
		return AIJobAdminDetail{}, err
	}
	detail := AIJobAdminDetail{
		Job: aiJobAdminRecord(job), Attempts: make([]AIAttemptAdminRecord, 0, len(attempts)),
		Events: events, Artifacts: make([]ArtifactAdminRecord, 0, len(artifacts)),
	}
	for _, attempt := range attempts {
		detail.Attempts = append(detail.Attempts, aiAttemptAdminRecord(attempt))
	}
	for _, artifact := range artifacts {
		record, recordErr := s.artifactAdminRecord(ctx, artifact)
		if recordErr != nil {
			return AIJobAdminDetail{}, recordErr
		}
		detail.Artifacts = append(detail.Artifacts, record)
	}
	return detail, nil
}

func (s *Service) CancelAIJobAdmin(ctx context.Context, actor, id string) (AIJobAdminActionResult, error) {
	now := s.nowUTC()
	id = strings.TrimSpace(id)
	audit := s.newAuditLog(actor, "cancel", "ai_job", id, "Requested AI job cancellation")
	job, changed, found, err := s.repo.RequestAIJobAdminCancellation(ctx, id, now, audit)
	if err != nil {
		return AIJobAdminActionResult{}, err
	}
	if !found {
		return AIJobAdminActionResult{}, ErrAIJobNotFound
	}
	return AIJobAdminActionResult{JobID: job.ID, Status: job.Status, Changed: changed, UpdatedAt: job.UpdatedAt}, nil
}

func (s *Service) ScheduleAIAttemptReconciliationAdmin(ctx context.Context, actor, jobID, attemptID string) (AIAttemptReconcileScheduleResult, error) {
	job, found, err := s.repo.FindAIJob(ctx, strings.TrimSpace(jobID))
	if err != nil {
		return AIAttemptReconcileScheduleResult{}, err
	}
	if !found {
		return AIAttemptReconcileScheduleResult{}, ErrAIJobNotFound
	}
	attempt, found, err := s.repo.FindAIAttempt(ctx, strings.TrimSpace(attemptID))
	if err != nil {
		return AIAttemptReconcileScheduleResult{}, err
	}
	if !found || attempt.OperationID != job.OperationID {
		return AIAttemptReconcileScheduleResult{}, ErrAIAttemptNotFound
	}
	now := s.nowUTC()
	audit := s.newAuditLog(actor, "schedule_reconciliation", "ai_attempt", attempt.ID, "Scheduled AI attempt for immediate reconciliation")
	updated, changed, err := s.repo.ScheduleAIAttemptReconciliation(ctx, attempt.ID, attempt.DispatchVersion, now, audit)
	if err != nil {
		return AIAttemptReconcileScheduleResult{}, err
	}
	if !changed {
		return AIAttemptReconcileScheduleResult{}, ErrAIAttemptReconcileScheduling
	}
	return AIAttemptReconcileScheduleResult{JobID: job.ID, AttemptID: updated.ID, Status: "scheduled", ScheduledAt: now}, nil
}

func validateAIJobAdminQuery(query AIJobQuery) error {
	if strings.TrimSpace(query.Status) != "" && !validAIJobStatus(strings.TrimSpace(query.Status)) {
		return ErrAIJobAdminQueryInvalid
	}
	if strings.TrimSpace(query.ArtifactPolicy) != "" && !validArtifactPolicy(strings.TrimSpace(query.ArtifactPolicy)) {
		return ErrAIJobAdminQueryInvalid
	}
	if query.Limit < 0 || query.Offset < 0 {
		return ErrAIJobAdminQueryInvalid
	}
	return nil
}

func validAIJobStatus(status string) bool {
	return oneOf(status, AIJobStatusAccepted, AIJobStatusQueued, AIJobStatusDispatching, AIJobStatusRunning,
		AIJobStatusCanceling, AIJobStatusCanceled, AIJobStatusSucceeded, AIJobStatusFailed, AIJobStatusUnknown, AIJobStatusExpired)
}

func aiJobAdminRecord(job AIJob) AIJobAdminRecord {
	return AIJobAdminRecord{
		ID: job.ID, OperationID: job.OperationID, ProfileScope: job.ProfileScope, TenantID: job.TenantID,
		Protocol: job.Protocol, Operation: job.Operation, Modality: job.Modality, Model: job.Model,
		ArtifactPolicy: job.ArtifactPolicy, ArtifactSinkID: job.ArtifactSinkID, Status: job.Status,
		StatusVersion: job.StatusVersion, Priority: job.Priority, NextEligibleAt: job.NextEligibleAt,
		ErrorType: job.ErrorType, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
		CompletedAt: cloneTimePointer(job.CompletedAt), ExpiresAt: job.ExpiresAt,
	}
}

func aiAttemptAdminRecord(attempt AIAttempt) AIAttemptAdminRecord {
	return AIAttemptAdminRecord{
		ID: attempt.ID, AttemptNumber: attempt.AttemptNumber, ProviderID: attempt.ProviderID,
		ProviderAccountID: attempt.ProviderAccountID, ProviderAdapterID: attempt.ProviderAdapterID,
		RouteID: attempt.RouteID, UpstreamModel: attempt.UpstreamModel, Status: attempt.Status,
		ErrorType: attempt.ErrorType, DispatchState: attempt.DispatchState, DispatchVersion: attempt.DispatchVersion,
		ProviderTaskID: attempt.ProviderTaskID, ProviderTaskStatus: attempt.ProviderTaskStatus,
		DispatchSubmittedAt: cloneTimePointer(attempt.DispatchSubmittedAt), ProviderAcceptedAt: cloneTimePointer(attempt.ProviderAcceptedAt),
		LastReconciledAt: cloneTimePointer(attempt.LastReconciledAt), ReconcileAfter: cloneTimePointer(attempt.ReconcileAfter),
		CreatedAt: attempt.CreatedAt, UpdatedAt: attempt.UpdatedAt, CompletedAt: cloneTimePointer(attempt.CompletedAt),
	}
}
