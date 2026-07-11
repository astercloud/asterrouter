package controlplane

import (
	"context"
	"strings"
)

func (s *Service) validateGovernancePolicyReference(ctx context.Context, policyID string) error {
	policyID = strings.TrimSpace(policyID)
	if policyID == "" {
		return nil
	}
	_, err := s.governancePolicyByID(ctx, policyID)
	return err
}

func (s *Service) effectiveGatewayPolicy(ctx context.Context, key APIKeyRecord, project Project) (*GovernancePolicy, string, error) {
	policies, err := s.repo.ListGovernancePolicies(ctx)
	if err != nil {
		return nil, "", err
	}
	if policy, ok := activePolicyByID(policies, key.PolicyID); ok {
		return &policy, GatewayPolicySourceAPIKeyExplicit, nil
	}
	if policy, ok := activePolicyByScope(policies, GovernancePolicyScopeAPIKey, key.ID); ok {
		return &policy, GatewayPolicySourceAPIKeyScope, nil
	}
	if policy, ok := activePolicyByID(policies, project.PolicyID); ok {
		return &policy, GatewayPolicySourceProjectExplicit, nil
	}
	if policy, ok := activePolicyByScope(policies, GovernancePolicyScopeProject, project.ID); ok {
		return &policy, GatewayPolicySourceProjectScope, nil
	}
	if policy, ok := activePolicyByScope(policies, GovernancePolicyScopeGlobal, ""); ok {
		return &policy, GatewayPolicySourceGlobalScope, nil
	}
	return nil, "", nil
}

func (s *Service) gatewayModelAllowed(auth GatewayAuthContext, model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	if auth.Policy != nil {
		if contains(auth.Policy.ModelDenylist, model) {
			return false
		}
		if len(auth.Policy.ModelAllowlist) > 0 {
			return contains(auth.Policy.ModelAllowlist, model)
		}
	}
	return contains(auth.APIKey.ModelAllowlist, model)
}

func (auth GatewayAuthContext) effectiveQPSLimit() int {
	if auth.Policy != nil && auth.Policy.QPSLimit > 0 {
		return auth.Policy.QPSLimit
	}
	return auth.APIKey.QPSLimit
}

func (auth GatewayAuthContext) effectiveMonthlyTokenLimit() int {
	if auth.Policy != nil && auth.Policy.MonthlyTokenLimit > 0 {
		return auth.Policy.MonthlyTokenLimit
	}
	return auth.APIKey.MonthlyTokenLimit
}

func (auth GatewayAuthContext) effectiveMonthlyBudgetCents() int {
	if auth.Policy != nil && auth.Policy.MonthlyBudgetCents > 0 {
		return auth.Policy.MonthlyBudgetCents
	}
	return auth.Project.MonthlyBudgetCents
}

func (auth GatewayAuthContext) shouldBlockOverage() bool {
	if auth.Policy == nil {
		return true
	}
	return auth.Policy.OverageAction == "" || auth.Policy.OverageAction == GovernancePolicyOverageBlock
}

func activePolicyByID(policies []GovernancePolicy, id string) (GovernancePolicy, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return GovernancePolicy{}, false
	}
	for _, policy := range policies {
		if policy.ID == id && policy.Status == GovernancePolicyStatusActive {
			return policy, true
		}
	}
	return GovernancePolicy{}, false
}

func activePolicyByScope(policies []GovernancePolicy, scopeType string, scopeID string) (GovernancePolicy, bool) {
	for _, policy := range policies {
		if policy.Status == GovernancePolicyStatusActive && policy.ScopeType == scopeType && policy.ScopeID == scopeID {
			return policy, true
		}
	}
	return GovernancePolicy{}, false
}
