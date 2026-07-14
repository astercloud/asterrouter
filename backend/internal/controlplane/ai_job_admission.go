package controlplane

import (
	"errors"
	"fmt"
	"strings"
)

var ErrAIJobQueueCapacityExceeded = errors.New("durable ai job queue capacity exceeded")

type AIJobAdmissionLimits struct {
	Profile   int
	Tenant    int
	Principal int
}

func (limits AIJobAdmissionLimits) validate() error {
	if limits.Profile < 0 || limits.Tenant < 0 || limits.Principal < 0 {
		return errors.New("ai job admission limits must be non-negative")
	}
	return nil
}

func (limits AIJobAdmissionLimits) enabled() bool {
	return limits.Profile > 0 || limits.Tenant > 0 || limits.Principal > 0
}

type aiJobAdmissionCounts struct {
	Profile   int
	Tenant    int
	Principal int
}

func enforceAIJobAdmissionLimits(limits AIJobAdmissionLimits, counts aiJobAdmissionCounts) error {
	if limits.Profile > 0 && counts.Profile >= limits.Profile {
		return fmt.Errorf("%w: profile", ErrAIJobQueueCapacityExceeded)
	}
	if limits.Tenant > 0 && counts.Tenant >= limits.Tenant {
		return fmt.Errorf("%w: tenant", ErrAIJobQueueCapacityExceeded)
	}
	if limits.Principal > 0 && counts.Principal >= limits.Principal {
		return fmt.Errorf("%w: principal", ErrAIJobQueueCapacityExceeded)
	}
	return nil
}

func aiJobCountsTowardQueueAdmission(job AIJob) bool {
	return oneOf(job.Status, AIJobStatusQueued, AIJobStatusDispatching)
}

func aiJobAdmissionCountsForJobs(jobs map[string]AIJob, candidate AIJob) aiJobAdmissionCounts {
	var counts aiJobAdmissionCounts
	for _, job := range jobs {
		if !aiJobCountsTowardQueueAdmission(job) || strings.TrimSpace(job.ProfileScope) != strings.TrimSpace(candidate.ProfileScope) {
			continue
		}
		counts.Profile++
		if strings.TrimSpace(job.TenantID) != strings.TrimSpace(candidate.TenantID) {
			continue
		}
		counts.Tenant++
		if aiJobPrincipalFairKey(job) == aiJobPrincipalFairKey(candidate) {
			counts.Principal++
		}
	}
	return counts
}
