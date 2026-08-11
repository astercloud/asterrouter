package controlplane

import (
	"errors"
	"fmt"
	"strings"
)

var ErrAIJobQueueCapacityExceeded = errors.New("durable ai job queue capacity exceeded")

type AIJobAdmissionLimits struct {
	Organization int
	Application  int
	Principal    int
}

func (limits AIJobAdmissionLimits) validate() error {
	if limits.Organization < 0 || limits.Application < 0 || limits.Principal < 0 {
		return errors.New("ai job admission limits must be non-negative")
	}
	return nil
}

func (limits AIJobAdmissionLimits) enabled() bool {
	return limits.Organization > 0 || limits.Application > 0 || limits.Principal > 0
}

type aiJobAdmissionCounts struct {
	Organization int
	Application  int
	Principal    int
}

func enforceAIJobAdmissionLimits(limits AIJobAdmissionLimits, counts aiJobAdmissionCounts) error {
	if limits.Organization > 0 && counts.Organization >= limits.Organization {
		return fmt.Errorf("%w: organization", ErrAIJobQueueCapacityExceeded)
	}
	if limits.Application > 0 && counts.Application >= limits.Application {
		return fmt.Errorf("%w: application", ErrAIJobQueueCapacityExceeded)
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
		if !aiJobCountsTowardQueueAdmission(job) {
			continue
		}
		counts.Organization++
		if strings.TrimSpace(job.ApplicationID) != strings.TrimSpace(candidate.ApplicationID) {
			continue
		}
		counts.Application++
		if aiJobPrincipalFairKey(job) == aiJobPrincipalFairKey(candidate) {
			counts.Principal++
		}
	}
	return counts
}
