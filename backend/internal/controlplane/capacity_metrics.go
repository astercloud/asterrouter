package controlplane

import (
	"context"
	"errors"
	"sort"
	"strings"
)

type CapacityAdmissionEvent struct {
	Scope  string
	Result string
	Reason string
}

type CapacityAdmissionObserver interface {
	ObserveCapacityAdmission(CapacityAdmissionEvent)
}

type ProviderCapacityMetricSnapshot struct {
	ProviderID        string
	ProviderAccountID string
	Schedulable       bool
	CircuitOpen       bool
	Current           ProviderCapacitySnapshot
	ConcurrencyLimit  int
	RPMLimit          int
	TPMLimit          int
}

func (s *Service) SetCapacityAdmissionObserver(observer CapacityAdmissionObserver) {
	if s == nil {
		return
	}
	s.capacityAdmissionObserverMu.Lock()
	s.capacityAdmissionObserver = observer
	s.capacityAdmissionObserverMu.Unlock()
}

func (s *Service) observeCapacityAdmission(scope, reason string, acquired bool, err error) {
	if s == nil {
		return
	}
	result := "rejected"
	if err != nil {
		result = "error"
	} else if acquired {
		result = "acquired"
	}
	s.capacityAdmissionObserverMu.RLock()
	observer := s.capacityAdmissionObserver
	s.capacityAdmissionObserverMu.RUnlock()
	if observer != nil {
		observer.ObserveCapacityAdmission(CapacityAdmissionEvent{Scope: scope, Result: result, Reason: reason})
	}
}

func (s *Service) observeCredentialCapacityAdmission(authLimits gatewayCredentialCapacityLimits, reason string, acquired bool, err error) {
	credentialLimited := authLimits.qps > 0 || authLimits.rpm > 0 || authLimits.tpm > 0 || authLimits.concurrency > 0
	applicationLimited := authLimits.applicationConcurrency > 0
	if !acquired && err == nil {
		if reason == "application_concurrency_exhausted" {
			s.observeCapacityAdmission("application", reason, false, nil)
			return
		}
		if credentialLimited {
			s.observeCapacityAdmission("credential", reason, false, nil)
		}
		return
	}
	if credentialLimited {
		s.observeCapacityAdmission("credential", reason, acquired, err)
	}
	if applicationLimited {
		s.observeCapacityAdmission("application", reason, acquired, err)
	}
}

func (s *Service) ProviderCapacityMetrics(ctx context.Context) ([]ProviderCapacityMetricSnapshot, error) {
	if s == nil {
		return nil, errors.New("control plane service is not configured")
	}
	accounts, err := s.repo.ListProviderAccounts(ctx)
	if err != nil {
		return nil, err
	}
	store := s.currentProviderCapacityStore()
	if store == nil {
		return nil, errors.New("provider capacity store is not available")
	}
	snapshots := make([]ProviderCapacityMetricSnapshot, 0, len(accounts))
	for _, account := range accounts {
		accountID := strings.TrimSpace(account.ID)
		if accountID == "" {
			continue
		}
		current, err := store.Snapshot(ctx, accountID)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, ProviderCapacityMetricSnapshot{
			ProviderID: strings.TrimSpace(account.ProviderID), ProviderAccountID: accountID,
			Schedulable: account.Status == AccountStatusActive && account.Schedulable,
			CircuitOpen: account.CircuitState == CircuitStateOpen,
			Current:     current, ConcurrencyLimit: account.Concurrency, RPMLimit: account.RPMLimit, TPMLimit: account.TPMLimit,
		})
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].ProviderAccountID < snapshots[j].ProviderAccountID
	})
	return snapshots, nil
}

type gatewayCredentialCapacityLimits struct {
	qps                    int
	rpm                    int
	tpm                    int
	concurrency            int
	applicationConcurrency int
}
