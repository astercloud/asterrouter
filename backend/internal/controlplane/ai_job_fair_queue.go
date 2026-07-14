package controlplane

import (
	"strings"
	"time"
)

const (
	aiJobMaxPriority           = 9
	aiJobPriorityAgingInterval = time.Minute
)

type aiJobDispatchActivity struct {
	Job          AIJob
	DispatchedAt time.Time
}

type aiJobFairLevelStats struct {
	InFlight       int
	Selected       int
	LastDispatchAt time.Time
	HasDispatch    bool
}

type aiJobFairCandidate struct {
	Job               *AIJob
	ProfileKey        string
	TenantKey         string
	PrincipalKey      string
	EffectivePriority int
}

func rankAIJobFairCandidates(candidates, jobs []AIJob, activities []aiJobDispatchActivity, now time.Time, limit int) []AIJob {
	if limit <= 0 || len(candidates) == 0 {
		return []AIJob{}
	}
	remaining := make([]aiJobFairCandidate, 0, len(candidates))
	for index := range candidates {
		remaining = append(remaining, newAIJobFairCandidate(&candidates[index], now))
	}
	profileStats := map[string]*aiJobFairLevelStats{}
	tenantStats := map[string]*aiJobFairLevelStats{}
	principalStats := map[string]*aiJobFairLevelStats{}
	for _, job := range jobs {
		if !aiJobInFlightForFairness(job.Status) {
			continue
		}
		fairStats(profileStats, aiJobProfileFairKey(job)).InFlight++
		fairStats(tenantStats, aiJobTenantFairKey(job)).InFlight++
		fairStats(principalStats, aiJobPrincipalFairKey(job)).InFlight++
	}
	for _, activity := range activities {
		if activity.DispatchedAt.IsZero() {
			continue
		}
		updateAIJobLastDispatch(fairStats(profileStats, aiJobProfileFairKey(activity.Job)), activity.DispatchedAt)
		updateAIJobLastDispatch(fairStats(tenantStats, aiJobTenantFairKey(activity.Job)), activity.DispatchedAt)
		updateAIJobLastDispatch(fairStats(principalStats, aiJobPrincipalFairKey(activity.Job)), activity.DispatchedAt)
	}

	out := make([]AIJob, 0, min(limit, len(remaining)))
	for len(remaining) > 0 && len(out) < limit {
		best := 0
		for index := 1; index < len(remaining); index++ {
			if aiJobFairCandidateLess(remaining[index], remaining[best], profileStats, tenantStats, principalStats) {
				best = index
			}
		}
		selected := remaining[best]
		out = append(out, *selected.Job)
		fairStats(profileStats, selected.ProfileKey).Selected++
		fairStats(tenantStats, selected.TenantKey).Selected++
		fairStats(principalStats, selected.PrincipalKey).Selected++
		remaining[best] = remaining[len(remaining)-1]
		remaining = remaining[:len(remaining)-1]
	}
	return out
}

func newAIJobFairCandidate(job *AIJob, now time.Time) aiJobFairCandidate {
	return aiJobFairCandidate{
		Job: job, ProfileKey: aiJobProfileFairKey(*job), TenantKey: aiJobTenantFairKey(*job),
		PrincipalKey: aiJobPrincipalFairKey(*job), EffectivePriority: effectiveAIJobPriority(*job, now),
	}
}

func aiJobFairCandidateLess(left, right aiJobFairCandidate, profileStats, tenantStats, principalStats map[string]*aiJobFairLevelStats) bool {
	leftReclaim := left.Job.Status == AIJobStatusDispatching
	rightReclaim := right.Job.Status == AIJobStatusDispatching
	if leftReclaim != rightReclaim {
		return leftReclaim
	}
	if comparison := compareAIJobFairLevel(
		fairStats(profileStats, left.ProfileKey), fairStats(profileStats, right.ProfileKey), left.ProfileKey, right.ProfileKey,
	); comparison != 0 {
		return comparison < 0
	}
	if comparison := compareAIJobFairLevel(
		fairStats(tenantStats, left.TenantKey), fairStats(tenantStats, right.TenantKey), left.TenantKey, right.TenantKey,
	); comparison != 0 {
		return comparison < 0
	}
	if comparison := compareAIJobFairLevel(
		fairStats(principalStats, left.PrincipalKey), fairStats(principalStats, right.PrincipalKey), left.PrincipalKey, right.PrincipalKey,
	); comparison != 0 {
		return comparison < 0
	}
	if left.EffectivePriority != right.EffectivePriority {
		return left.EffectivePriority > right.EffectivePriority
	}
	if !left.Job.NextEligibleAt.Equal(right.Job.NextEligibleAt) {
		return left.Job.NextEligibleAt.Before(right.Job.NextEligibleAt)
	}
	if !left.Job.CreatedAt.Equal(right.Job.CreatedAt) {
		return left.Job.CreatedAt.Before(right.Job.CreatedAt)
	}
	return left.Job.ID < right.Job.ID
}

func compareAIJobFairLevel(left, right *aiJobFairLevelStats, leftKey, rightKey string) int {
	leftLoad := left.InFlight + left.Selected
	rightLoad := right.InFlight + right.Selected
	if leftLoad < rightLoad {
		return -1
	}
	if leftLoad > rightLoad {
		return 1
	}
	if left.HasDispatch != right.HasDispatch {
		if !left.HasDispatch {
			return -1
		}
		return 1
	}
	if left.HasDispatch && !left.LastDispatchAt.Equal(right.LastDispatchAt) {
		if left.LastDispatchAt.Before(right.LastDispatchAt) {
			return -1
		}
		return 1
	}
	return strings.Compare(leftKey, rightKey)
}

func effectiveAIJobPriority(job AIJob, now time.Time) int {
	priority := job.Priority
	if priority < 0 {
		priority = 0
	}
	if priority > aiJobMaxPriority {
		priority = aiJobMaxPriority
	}
	waitStartedAt := job.NextEligibleAt
	if waitStartedAt.IsZero() || waitStartedAt.Before(job.CreatedAt) {
		waitStartedAt = job.CreatedAt
	}
	if now.After(waitStartedAt) {
		priority += int(now.Sub(waitStartedAt) / aiJobPriorityAgingInterval)
	}
	if priority > aiJobMaxPriority {
		priority = aiJobMaxPriority
	}
	return priority
}

func aiJobInFlightForFairness(status string) bool {
	return oneOf(status, AIJobStatusDispatching, AIJobStatusRunning, AIJobStatusCanceling, AIJobStatusUnknown)
}

func aiJobReadyForClaim(job AIJob, now time.Time) bool {
	if job.Status == AIJobStatusQueued {
		return !job.NextEligibleAt.After(now) && (job.QueueLeaseUntil == nil || !job.QueueLeaseUntil.After(now))
	}
	return job.Status == AIJobStatusDispatching && job.QueueLeaseUntil != nil && !job.QueueLeaseUntil.After(now)
}

func fairStats(values map[string]*aiJobFairLevelStats, key string) *aiJobFairLevelStats {
	if values[key] == nil {
		values[key] = &aiJobFairLevelStats{}
	}
	return values[key]
}

func updateAIJobLastDispatch(stats *aiJobFairLevelStats, dispatchedAt time.Time) {
	if !stats.HasDispatch || dispatchedAt.After(stats.LastDispatchAt) {
		stats.LastDispatchAt = dispatchedAt
		stats.HasDispatch = true
	}
}

func aiJobProfileFairKey(job AIJob) string {
	return strings.TrimSpace(job.ProfileScope)
}

func aiJobTenantFairKey(job AIJob) string {
	return strings.Join([]string{aiJobProfileFairKey(job), strings.TrimSpace(job.TenantID)}, "\x00")
}

func aiJobPrincipalFairKey(job AIJob) string {
	return strings.Join([]string{
		aiJobTenantFairKey(job), strings.TrimSpace(job.CredentialSource), strings.TrimSpace(job.IntegrationID),
		strings.TrimSpace(job.PrincipalType), strings.TrimSpace(job.PrincipalID), strings.TrimSpace(job.ExternalSubjectReference),
	}, "\x00")
}
