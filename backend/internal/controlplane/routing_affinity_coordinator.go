package controlplane

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrRoutingAffinityCoordinatorConfig = errors.New("invalid routing affinity coordinator configuration")
	ErrRoutingAffinityBindingInvalid    = errors.New("invalid routing affinity binding")
	ErrRoutingAffinityStateInvalid      = errors.New("routing affinity coordinator state is invalid")
)

// RoutingAffinityCoordinator makes the first binding for a HMAC scope win
// across instances. The Repository remains the durable audit and fallback copy.
type RoutingAffinityCoordinator interface {
	Find(ctx context.Context, scopeKey string) (RoutingAffinityBinding, bool, error)
	Claim(ctx context.Context, binding RoutingAffinityBinding, ttl time.Duration) (RoutingAffinityBinding, bool, error)
	Refresh(ctx context.Context, binding RoutingAffinityBinding, ttl time.Duration) (bool, error)
}

func validRoutingAffinityBinding(binding RoutingAffinityBinding, ttl time.Duration) bool {
	if ttl <= 0 || strings.TrimSpace(binding.ScopeKey) == "" || strings.TrimSpace(binding.Kind) == "" || strings.TrimSpace(binding.ProviderID) == "" {
		return false
	}
	if !oneOf(binding.Kind, AffinityBindingSupplier, AffinityBindingAccount) || strings.TrimSpace(binding.Model) == "" || strings.TrimSpace(binding.Protocol) == "" {
		return false
	}
	if binding.Kind == AffinityBindingAccount && (strings.TrimSpace(binding.ProviderAccountID) == "" || strings.TrimSpace(binding.RouteID) == "") {
		return false
	}
	return true
}

func sameRoutingAffinityOwner(left, right RoutingAffinityBinding) bool {
	return left.Kind == right.Kind && left.ProviderID == right.ProviderID && left.ProviderAccountID == right.ProviderAccountID && left.RouteID == right.RouteID && left.Model == right.Model && left.Protocol == right.Protocol && left.PolicyVersion == right.PolicyVersion
}
