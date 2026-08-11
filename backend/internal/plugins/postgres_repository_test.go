package plugins

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestPostgresRepositoryPersistsPluginAcrossRestart(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	repo, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("NewPostgresRepository(): %v", err)
	}
	plugin := Plugin{
		ID: "plugin-postgres", PluginID: "official.test", Name: "Postgres Plugin", Category: "testing",
		Type: "builtin", Tier: TierFreeCore, Version: "1.0.0", Vendor: "AsterRouter",
		Status: StatusEnabled, EntitlementStatus: EntitlementFree,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.SavePlugin(ctx, plugin); err != nil {
		t.Fatalf("SavePlugin(): %v", err)
	}
	if err := repo.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	reopened, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("reopen NewPostgresRepository(): %v", err)
	}
	defer reopened.Close()
	found, ok, err := reopened.FindPlugin(ctx, plugin.ID)
	if err != nil {
		t.Fatalf("FindPlugin(): %v", err)
	}
	if !ok || found.PluginID != plugin.PluginID || found.Status != StatusEnabled {
		t.Fatalf("persisted plugin ok=%t plugin=%#v", ok, found)
	}
}

func TestPostgresRepositorySeedsBuiltinCatalogConcurrently(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	first, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	barrier := newSeedListBarrier(2)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for _, repository := range []Repository{first, second} {
		service := NewService(&seedBarrierRepository{Repository: repository, barrier: barrier})
		wait.Add(1)
		go func(service *Service) {
			defer wait.Done()
			<-start
			errs <- service.EnsureSeedData(ctx)
		}(service)
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("EnsureSeedData() concurrent: %v", err)
		}
	}
	plugins, err := first.ListPlugins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != len(builtinPlugins(time.Now().UTC())) {
		t.Fatalf("seeded plugins=%d", len(plugins))
	}
}

type seedListBarrier struct {
	mu        sync.Mutex
	remaining int
	release   chan struct{}
}

func newSeedListBarrier(participants int) *seedListBarrier {
	return &seedListBarrier{remaining: participants, release: make(chan struct{})}
}

func (b *seedListBarrier) wait() {
	b.mu.Lock()
	b.remaining--
	if b.remaining == 0 {
		close(b.release)
	}
	b.mu.Unlock()
	<-b.release
}

type seedBarrierRepository struct {
	Repository
	barrier *seedListBarrier
}

func (r *seedBarrierRepository) ListPlugins(ctx context.Context) ([]Plugin, error) {
	plugins, err := r.Repository.ListPlugins(ctx)
	if err == nil {
		r.barrier.wait()
	}
	return plugins, err
}
