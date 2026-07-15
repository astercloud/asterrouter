package controlplane

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRoutingAffinityCoordinatorMakesFirstProviderWinAcrossServices(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	coordinator := &memoryRoutingAffinityCoordinator{bindings: map[string]RoutingAffinityBinding{}}
	serviceA := NewService(NewMemoryRepository(), "/v1", "shared-affinity-secret")
	serviceBRepo := NewMemoryRepository()
	serviceB := NewService(serviceBRepo, "/v1", "shared-affinity-secret")
	serviceA.now = func() time.Time { return now }
	serviceB.now = func() time.Time { return now }
	serviceA.SetRoutingAffinityCoordinator(coordinator)
	serviceB.SetRoutingAffinityCoordinator(coordinator)
	input := GatewayAffinityInput{
		TenantID: "tenant-a", PrincipalID: "principal-a", CredentialID: "credential-a",
		Model: "public-model", Protocol: "openai_chat_completions", RouteGroup: "default", StickyKey: "session-a", PolicyVersion: 1,
	}
	providerA := GatewayProvider{ID: "provider-a", AccountID: "account-a", RouteID: "route-a", StickyEnabled: true, StickyTTLSeconds: 1800}
	providerB := GatewayProvider{ID: "provider-b", AccountID: "account-b", RouteID: "route-b", StickyEnabled: true, StickyTTLSeconds: 1800}
	if err := serviceA.BindGatewayCandidateAffinity(ctx, input, providerA); err != nil {
		t.Fatal(err)
	}
	if err := serviceB.BindGatewayCandidateAffinity(ctx, input, providerB); err != nil {
		t.Fatal(err)
	}

	ordered := serviceB.PreferGatewayCandidatesWithAffinity(ctx, input, []GatewayProvider{providerB, providerA})
	if ordered[0].ID != providerA.ID || ordered[0].AccountID != providerA.AccountID {
		t.Fatalf("coordinated affinity did not converge on the first provider: %+v", ordered)
	}
	accountScope := serviceB.gatewayAffinityScopeKey(AffinityBindingAccount, input)
	winner, found, err := serviceBRepo.FindRoutingAffinityBinding(ctx, accountScope, now)
	if err != nil || !found || winner.ProviderAccountID != providerA.AccountID {
		t.Fatalf("repository fallback copy=%+v found=%t err=%v", winner, found, err)
	}
}

func TestRoutingAffinityCoordinatorFailureFallsBackToRepository(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repo := NewMemoryRepository()
	service := NewService(repo, "/v1", "fallback-affinity-secret")
	service.now = func() time.Time { return now }
	service.SetRoutingAffinityCoordinator(&memoryRoutingAffinityCoordinator{fail: true, bindings: map[string]RoutingAffinityBinding{}})
	input := GatewayAffinityInput{TenantID: "tenant", PrincipalID: "principal", CredentialID: "credential", Model: "model", Protocol: "openai_chat_completions", RouteGroup: "default", PolicyVersion: 1}
	providerA := GatewayProvider{ID: "provider-a"}
	providerB := GatewayProvider{ID: "provider-b"}
	if err := service.BindGatewayCandidateAffinity(ctx, input, providerB); err != nil {
		t.Fatal(err)
	}
	ordered := service.PreferGatewayCandidatesWithAffinity(ctx, input, []GatewayProvider{providerA, providerB})
	if ordered[0].ID != providerB.ID || ordered[0].SelectionReason != "customer supplier affinity reused" {
		t.Fatalf("repository fallback affinity=%+v", ordered)
	}
}

type memoryRoutingAffinityCoordinator struct {
	mu       sync.Mutex
	bindings map[string]RoutingAffinityBinding
	fail     bool
}

func (c *memoryRoutingAffinityCoordinator) Find(_ context.Context, scopeKey string) (RoutingAffinityBinding, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return RoutingAffinityBinding{}, false, errors.New("synthetic coordinator failure")
	}
	binding, found := c.bindings[scopeKey]
	return binding, found, nil
}

func (c *memoryRoutingAffinityCoordinator) Claim(_ context.Context, binding RoutingAffinityBinding, _ time.Duration) (RoutingAffinityBinding, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return RoutingAffinityBinding{}, false, errors.New("synthetic coordinator failure")
	}
	if winner, found := c.bindings[binding.ScopeKey]; found {
		if sameRoutingAffinityOwner(winner, binding) {
			c.bindings[binding.ScopeKey] = binding
			return binding, false, nil
		}
		return winner, false, nil
	}
	c.bindings[binding.ScopeKey] = binding
	return binding, true, nil
}

func (c *memoryRoutingAffinityCoordinator) Refresh(_ context.Context, binding RoutingAffinityBinding, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return false, errors.New("synthetic coordinator failure")
	}
	winner, found := c.bindings[binding.ScopeKey]
	if !found || !sameRoutingAffinityOwner(winner, binding) {
		return false, nil
	}
	c.bindings[binding.ScopeKey] = binding
	return true, nil
}
