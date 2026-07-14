package controlplane

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrAIJobReadyIndexConfig   = errors.New("invalid ai job ready index configuration")
	ErrAIJobReadyIndexConflict = errors.New("ai job ready index version conflict")
)

const (
	AIJobReadyScopeAll       = "all"
	AIJobReadyScopeProfile   = "profile"
	AIJobReadyScopeTenant    = "tenant"
	AIJobReadyScopePrincipal = "principal"
)

// AIJobReadyEntry contains scheduling metadata only. Request payloads,
// credentials, provider secrets, and artifact data must never enter an index.
type AIJobReadyEntry struct {
	JobID                    string    `json:"job_id"`
	Status                   string    `json:"status"`
	StatusVersion            int       `json:"status_version"`
	FenceToken               int64     `json:"fence_token"`
	ProfileScope             string    `json:"profile_scope"`
	TenantID                 string    `json:"tenant_id"`
	CredentialSource         string    `json:"credential_source"`
	IntegrationID            string    `json:"integration_id"`
	PrincipalType            string    `json:"principal_type"`
	PrincipalID              string    `json:"principal_id"`
	ExternalSubjectReference string    `json:"external_subject_reference"`
	Priority                 int       `json:"priority"`
	ReadyAt                  time.Time `json:"ready_at"`
	CreatedAt                time.Time `json:"created_at"`
}

type AIJobReadyReference struct {
	JobID         string
	StatusVersion int
}

type AIJobReadyQuery struct {
	ReadyAt time.Time
	Limit   int
}

type AIJobReadyScope struct {
	Level                    string
	ProfileScope             string
	TenantID                 string
	CredentialSource         string
	IntegrationID            string
	PrincipalType            string
	PrincipalID              string
	ExternalSubjectReference string
}

// AIJobReadyIndex is a rebuildable candidate index. Candidates are hints;
// callers must re-read and claim the authoritative Job in the Repository.
type AIJobReadyIndex interface {
	Register(context.Context, AIJobReadyEntry) error
	Remove(context.Context, AIJobReadyReference) error
	Candidates(context.Context, AIJobReadyQuery) ([]AIJobReadyEntry, error)
	Count(context.Context, AIJobReadyScope) (int64, error)
}

func newAIJobReadyEntry(job AIJob) (AIJobReadyEntry, error) {
	readyAt := aiJobReadyAt(job)
	if job.Status == AIJobStatusDispatching && job.QueueLeaseUntil == nil {
		return AIJobReadyEntry{}, ErrAIJobReadyIndexConfig
	}
	entry := AIJobReadyEntry{
		JobID: job.ID, Status: job.Status, StatusVersion: job.StatusVersion, FenceToken: job.FenceToken,
		ProfileScope: job.ProfileScope, TenantID: job.TenantID, CredentialSource: job.CredentialSource,
		IntegrationID: job.IntegrationID, PrincipalType: job.PrincipalType, PrincipalID: job.PrincipalID,
		ExternalSubjectReference: job.ExternalSubjectReference, Priority: job.Priority, ReadyAt: readyAt, CreatedAt: job.CreatedAt,
	}
	normalizeAIJobReadyEntry(&entry)
	if err := validateAIJobReadyEntry(entry); err != nil {
		return AIJobReadyEntry{}, err
	}
	return entry, nil
}

func normalizeAIJobReadyEntry(entry *AIJobReadyEntry) {
	if entry == nil {
		return
	}
	entry.JobID = strings.TrimSpace(entry.JobID)
	entry.Status = strings.TrimSpace(entry.Status)
	entry.ProfileScope = strings.TrimSpace(entry.ProfileScope)
	entry.TenantID = strings.TrimSpace(entry.TenantID)
	entry.CredentialSource = strings.TrimSpace(entry.CredentialSource)
	entry.IntegrationID = strings.TrimSpace(entry.IntegrationID)
	entry.PrincipalType = strings.TrimSpace(entry.PrincipalType)
	entry.PrincipalID = strings.TrimSpace(entry.PrincipalID)
	entry.ExternalSubjectReference = strings.TrimSpace(entry.ExternalSubjectReference)
	entry.ReadyAt = entry.ReadyAt.UTC()
	entry.CreatedAt = entry.CreatedAt.UTC()
}

func aiJobReadyAt(job AIJob) time.Time {
	if job.Status == AIJobStatusDispatching && job.QueueLeaseUntil != nil {
		return *job.QueueLeaseUntil
	}
	return job.NextEligibleAt
}

func validateAIJobReadyEntry(entry AIJobReadyEntry) error {
	if strings.TrimSpace(entry.JobID) == "" || entry.StatusVersion <= 0 || strings.TrimSpace(entry.TenantID) == "" ||
		strings.TrimSpace(entry.PrincipalID) == "" || entry.ReadyAt.IsZero() || entry.CreatedAt.IsZero() ||
		!oneOf(entry.Status, AIJobStatusQueued, AIJobStatusDispatching) {
		return ErrAIJobReadyIndexConfig
	}
	return nil
}

func (entry AIJobReadyEntry) reference() AIJobReadyReference {
	return AIJobReadyReference{JobID: entry.JobID, StatusVersion: entry.StatusVersion}
}

func aiJobReadyScopeForEntry(level string, entry AIJobReadyEntry) AIJobReadyScope {
	return AIJobReadyScope{
		Level: level, ProfileScope: entry.ProfileScope, TenantID: entry.TenantID, CredentialSource: entry.CredentialSource,
		IntegrationID: entry.IntegrationID, PrincipalType: entry.PrincipalType, PrincipalID: entry.PrincipalID,
		ExternalSubjectReference: entry.ExternalSubjectReference,
	}
}

func validateAIJobReadyScope(scope AIJobReadyScope) error {
	switch scope.Level {
	case AIJobReadyScopeAll:
		return nil
	case AIJobReadyScopeProfile:
		return nil
	case AIJobReadyScopeTenant:
		if strings.TrimSpace(scope.TenantID) != "" {
			return nil
		}
	case AIJobReadyScopePrincipal:
		if strings.TrimSpace(scope.TenantID) != "" && strings.TrimSpace(scope.PrincipalID) != "" {
			return nil
		}
	}
	return ErrAIJobReadyIndexConfig
}

func aiJobReadyScopeKey(scope AIJobReadyScope) string {
	switch scope.Level {
	case AIJobReadyScopeAll:
		return AIJobReadyScopeAll
	case AIJobReadyScopeProfile:
		return strings.TrimSpace(scope.ProfileScope)
	case AIJobReadyScopeTenant:
		return strings.Join([]string{strings.TrimSpace(scope.ProfileScope), strings.TrimSpace(scope.TenantID)}, "\x00")
	case AIJobReadyScopePrincipal:
		return strings.Join([]string{
			strings.TrimSpace(scope.ProfileScope), strings.TrimSpace(scope.TenantID), strings.TrimSpace(scope.CredentialSource),
			strings.TrimSpace(scope.IntegrationID), strings.TrimSpace(scope.PrincipalType), strings.TrimSpace(scope.PrincipalID),
			strings.TrimSpace(scope.ExternalSubjectReference),
		}, "\x00")
	default:
		return ""
	}
}

type MemoryAIJobReadyIndex struct {
	mu      sync.RWMutex
	entries map[string]AIJobReadyEntry
}

func NewMemoryAIJobReadyIndex() *MemoryAIJobReadyIndex {
	return &MemoryAIJobReadyIndex{entries: map[string]AIJobReadyEntry{}}
}

var _ AIJobReadyIndex = (*MemoryAIJobReadyIndex)(nil)

func (index *MemoryAIJobReadyIndex) Register(_ context.Context, entry AIJobReadyEntry) error {
	normalizeAIJobReadyEntry(&entry)
	if err := validateAIJobReadyEntry(entry); err != nil {
		return err
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	current, found := index.entries[entry.JobID]
	if found && current.StatusVersion > entry.StatusVersion {
		return nil
	}
	if found && current.StatusVersion == entry.StatusVersion && current != entry {
		return ErrAIJobReadyIndexConflict
	}
	index.entries[entry.JobID] = entry
	return nil
}

func (index *MemoryAIJobReadyIndex) Remove(_ context.Context, reference AIJobReadyReference) error {
	if strings.TrimSpace(reference.JobID) == "" || reference.StatusVersion <= 0 {
		return ErrAIJobReadyIndexConfig
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	if current, found := index.entries[reference.JobID]; found && current.StatusVersion == reference.StatusVersion {
		delete(index.entries, reference.JobID)
	}
	return nil
}

func (index *MemoryAIJobReadyIndex) Candidates(_ context.Context, query AIJobReadyQuery) ([]AIJobReadyEntry, error) {
	if query.ReadyAt.IsZero() || query.Limit <= 0 {
		return []AIJobReadyEntry{}, nil
	}
	index.mu.RLock()
	groups := map[string][]AIJobReadyEntry{}
	for _, entry := range index.entries {
		if !entry.ReadyAt.After(query.ReadyAt) {
			key := aiJobReadyScopeKey(aiJobReadyScopeForEntry(AIJobReadyScopePrincipal, entry))
			groups[key] = append(groups[key], entry)
		}
	}
	index.mu.RUnlock()
	return roundRobinAIJobReadyCandidates(groups, query.Limit), nil
}

func (index *MemoryAIJobReadyIndex) Count(_ context.Context, scope AIJobReadyScope) (int64, error) {
	if err := validateAIJobReadyScope(scope); err != nil {
		return 0, err
	}
	want := aiJobReadyScopeKey(scope)
	index.mu.RLock()
	defer index.mu.RUnlock()
	var count int64
	for _, entry := range index.entries {
		if scope.Level == AIJobReadyScopeAll || aiJobReadyScopeKey(aiJobReadyScopeForEntry(scope.Level, entry)) == want {
			count++
		}
	}
	return count, nil
}

func roundRobinAIJobReadyCandidates(groups map[string][]AIJobReadyEntry, limit int) []AIJobReadyEntry {
	if limit <= 0 || len(groups) == 0 {
		return []AIJobReadyEntry{}
	}
	keys := make([]string, 0, len(groups))
	for key, entries := range groups {
		sort.Slice(entries, func(i, j int) bool {
			if !entries[i].ReadyAt.Equal(entries[j].ReadyAt) {
				return entries[i].ReadyAt.Before(entries[j].ReadyAt)
			}
			if entries[i].Priority != entries[j].Priority {
				return entries[i].Priority > entries[j].Priority
			}
			if !entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
				return entries[i].CreatedAt.Before(entries[j].CreatedAt)
			}
			return entries[i].JobID < entries[j].JobID
		})
		groups[key] = entries
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := groups[keys[i]][0], groups[keys[j]][0]
		if !left.ReadyAt.Equal(right.ReadyAt) {
			return left.ReadyAt.Before(right.ReadyAt)
		}
		return keys[i] < keys[j]
	})
	out := make([]AIJobReadyEntry, 0, limit)
	for round := 0; len(out) < limit; round++ {
		added := false
		for _, key := range keys {
			if round >= len(groups[key]) {
				continue
			}
			out = append(out, groups[key][round])
			added = true
			if len(out) == limit {
				break
			}
		}
		if !added {
			break
		}
	}
	return out
}
