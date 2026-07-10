package plugins

import (
	"context"
	"errors"
	"testing"
)

func TestServiceSeedsBuiltinPluginCatalog(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}

	catalog, err := svc.Catalog(context.Background())
	if err != nil {
		t.Fatalf("Catalog(): %v", err)
	}

	if catalog.Summary.Total < 10 {
		t.Fatalf("expected built-in plugin catalog, got %+v", catalog.Summary)
	}
	if catalog.Summary.Enabled == 0 || catalog.Summary.PaidLocked == 0 {
		t.Fatalf("summary does not reflect enabled and locked plugins: %+v", catalog.Summary)
	}
}

func TestServiceEnablesFreeCorePlugin(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}

	plugin, err := svc.Enable(context.Background(), "com.asterrouter.notification.webhook")
	if err != nil {
		t.Fatalf("Enable(): %v", err)
	}
	if plugin.Status != StatusEnabled {
		t.Fatalf("status = %q", plugin.Status)
	}
}

func TestServiceRejectsLockedPaidPlugin(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}

	_, err := svc.Enable(context.Background(), "com.asterrouter.notification.slack")
	if !errors.Is(err, ErrPluginLocked) {
		t.Fatalf("err = %v, want ErrPluginLocked", err)
	}
}

func TestServiceRejectsDisablingCorePlugin(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}

	_, err := svc.Disable(context.Background(), "com.asterrouter.core.gateway")
	if !errors.Is(err, ErrPluginCoreRequired) {
		t.Fatalf("err = %v, want ErrPluginCoreRequired", err)
	}
}
