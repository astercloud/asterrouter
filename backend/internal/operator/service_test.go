package operator

import (
	"context"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestOperatorCustomerBalanceAndOwnershipLifecycle(t *testing.T) {
	ctx := context.Background()
	control := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	svc := NewService(NewMemoryRepository(), control)
	group, err := svc.SaveGroup(ctx, "", CustomerGroupRequest{Name: "Standard", Status: StatusActive})
	if err != nil {
		t.Fatalf("SaveGroup(): %v", err)
	}
	plan, err := svc.SavePlan(ctx, "", PlanRequest{Name: "Starter", RateMultiplier: 1, Status: StatusActive})
	if err != nil {
		t.Fatalf("SavePlan(): %v", err)
	}
	customer, err := svc.SaveCustomer(ctx, "", CustomerRequest{Name: "Customer A", GroupID: group.ID, PlanID: plan.ID, Status: StatusActive})
	if err != nil {
		t.Fatalf("SaveCustomer(): %v", err)
	}
	entry, err := svc.ApplyBalanceEntry(ctx, "tester", BalanceEntryRequest{CustomerID: customer.ID, Kind: "recharge", AmountCents: 1000})
	if err != nil {
		t.Fatalf("ApplyBalanceEntry(): %v", err)
	}
	if entry.BalanceAfter != 1000 {
		t.Fatalf("balance after = %d", entry.BalanceAfter)
	}
	updated, err := svc.SaveCustomer(ctx, customer.ID, CustomerRequest{Name: customer.Name, GroupID: group.ID, PlanID: plan.ID, Status: StatusActive})
	if err != nil {
		t.Fatalf("SaveCustomer update(): %v", err)
	}
	if updated.BalanceCents != 1000 {
		t.Fatalf("customer edit changed balance: %+v", updated)
	}
	if err := svc.DeleteGroup(ctx, group.ID); err == nil {
		t.Fatal("DeleteGroup accepted referenced group")
	}
	if err := svc.DeletePlan(ctx, plan.ID); err == nil {
		t.Fatal("DeletePlan accepted referenced plan")
	}

	key, err := svc.CreateCustomerKey(ctx, "tester", customer.ID, controlplane.APIKeyCreateRequest{Name: "Customer key", ModelAllowlist: []string{"gpt-5"}})
	if err != nil {
		t.Fatalf("CreateCustomerKey(): %v", err)
	}
	if key.Record.KeyType != controlplane.APIKeyTypeCustomer || key.Record.CustomerID != customer.ID {
		t.Fatalf("customer key ownership mismatch: %+v", key.Record)
	}
	if err := svc.DeleteCustomer(ctx, customer.ID); err == nil {
		t.Fatal("DeleteCustomer accepted customer with keys")
	}
}

func TestOperatorUsageObserverChargesCustomerIdempotently(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	control := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	svc := NewService(repo, control)
	plan, err := svc.SavePlan(ctx, "", PlanRequest{Name: "Standard", Status: StatusActive, RateMultiplier: 1})
	if err != nil {
		t.Fatalf("SavePlan(): %v", err)
	}
	customer, err := svc.SaveCustomer(ctx, "", CustomerRequest{Name: "Customer A", PlanID: plan.ID, Status: StatusActive})
	if err != nil {
		t.Fatalf("SaveCustomer(): %v", err)
	}
	if _, err := svc.SavePricingRule(ctx, "", PricingRuleRequest{
		Name: "Exact model", PlanID: plan.ID, Model: "model-a", Status: StatusActive,
		InputPrice: 1000, OutputPrice: 2000, RateMultiplier: 1,
	}); err != nil {
		t.Fatalf("SavePricingRule(): %v", err)
	}
	record := controlplane.UsageRecord{
		ID: "usage_test_1", CustomerID: customer.ID, Model: "model-a", Status: "forwarded",
		InputTokens: 1000, OutputTokens: 500, CreatedAt: time.Now().UTC(),
	}
	if err := svc.OnGatewayUsage(ctx, record); err != nil {
		t.Fatalf("OnGatewayUsage(): %v", err)
	}
	if err := svc.OnGatewayUsage(ctx, record); err != nil {
		t.Fatalf("OnGatewayUsage(retry): %v", err)
	}
	customers, err := repo.ListCustomers(ctx)
	if err != nil {
		t.Fatalf("ListCustomers(): %v", err)
	}
	// 1000 input tokens at 1000 cents/M + 500 output tokens at 2000 cents/M
	// equals two cents, and the retry must not deduct it a second time.
	if len(customers) != 1 || customers[0].BalanceCents != -2 {
		t.Fatalf("customer balance = %+v", customers)
	}
	entries, err := repo.ListBalanceEntries(ctx)
	if err != nil {
		t.Fatalf("ListBalanceEntries(): %v", err)
	}
	if len(entries) != 1 || entries[0].Reference != record.ID {
		t.Fatalf("balance entries = %+v", entries)
	}
}
