package controlplane

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"time"
)

const gatewaySchedulingWindow = time.Minute

type gatewayRateSample struct {
	at     time.Time
	tokens int
}

type gatewayScheduler struct {
	mu             sync.Mutex
	rateSamples    map[string][]gatewayRateSample
	halfOpenProbes map[string]bool
}

func newGatewayScheduler() *gatewayScheduler {
	return &gatewayScheduler{
		rateSamples:    map[string][]gatewayRateSample{},
		halfOpenProbes: map[string]bool{},
	}
}

// TryAcquireProviderAccountPermit preserves the synchronous caller API while
// delegating authoritative concurrency and rate admission to CapacityStore.
func (s *Service) TryAcquireProviderAccountPermit(provider GatewayProvider, estimatedTokens int) (ProviderAccountPermit, string, bool) {
	permit, reason, acquired, err := s.TryAcquireProviderAccountPermitContext(context.Background(), provider, estimatedTokens, "")
	if err != nil {
		return ProviderAccountPermit{}, "capacity_store_unavailable", false
	}
	return permit, reason, acquired
}

func (s *Service) providerAccountRateHeadroom(account ProviderAccount, now time.Time) float64 {
	if s.scheduler == nil {
		return 1
	}
	concurrencyUsed := s.providerAccountSlotUsage(account.ID)
	s.scheduler.mu.Lock()
	defer s.scheduler.mu.Unlock()
	samples := s.scheduler.pruneSamples(account.ID, now)
	requests, tokens := rateWindowUsage(samples)
	rpmHeadroom := remainingRatio(requests, account.RPMLimit)
	tpmHeadroom := remainingRatio(tokens, account.TPMLimit)
	concurrencyHeadroom := remainingRatio(concurrencyUsed, account.Concurrency)
	return math.Min(rpmHeadroom, math.Min(tpmHeadroom, concurrencyHeadroom))
}

func (s *gatewayScheduler) pruneSamples(accountID string, now time.Time) []gatewayRateSample {
	cutoff := now.Add(-gatewaySchedulingWindow)
	samples := s.rateSamples[accountID]
	kept := samples[:0]
	for _, sample := range samples {
		if sample.at.After(cutoff) {
			kept = append(kept, sample)
		}
	}
	s.rateSamples[accountID] = kept
	return kept
}

func rateWindowUsage(samples []gatewayRateSample) (requests int, tokens int) {
	for _, sample := range samples {
		requests++
		tokens += sample.tokens
	}
	return requests, tokens
}

func remainingRatio(used int, limit int) float64 {
	if limit <= 0 {
		return 1
	}
	remaining := float64(limit-used) / float64(limit)
	if remaining < 0 {
		return 0
	}
	if remaining > 1 {
		return 1
	}
	return remaining
}

func weightedCandidateScore(weight int) float64 {
	if weight <= 0 {
		weight = 1
	}
	u := rand.Float64()
	if u == 0 {
		u = math.SmallestNonzeroFloat64
	}
	return -math.Log(u) / float64(weight)
}
