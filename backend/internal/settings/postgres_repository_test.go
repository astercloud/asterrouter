package settings

import (
	"context"
	"errors"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestPostgresRepositoryPersistsSettingsAcrossRestart(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()

	repo, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("NewPostgresRepository(): %v", err)
	}
	if err := repo.SetMultiple(ctx, map[string]string{"site_name": "Test Router", "default_locale": "zh-CN"}); err != nil {
		t.Fatalf("SetMultiple(): %v", err)
	}
	if err := repo.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	reopened, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("reopen NewPostgresRepository(): %v", err)
	}
	defer reopened.Close()
	values, err := reopened.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll(): %v", err)
	}
	if values["site_name"] != "Test Router" || values["default_locale"] != "zh-CN" {
		t.Fatalf("persisted settings = %#v", values)
	}
}

func TestPostgresRepositorySerializesConcurrentSetup(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	repositories := make([]*PostgresRepository, 2)
	for index := range repositories {
		repo, err := NewPostgresRepository(ctx, schema.URL)
		if err != nil {
			t.Fatalf("NewPostgresRepository(%d): %v", index, err)
		}
		repositories[index] = repo
	}
	organizationNames := []string{"First Organization", "Second Organization"}
	start := make(chan struct{})
	results := make(chan error, len(repositories))
	for index, repo := range repositories {
		go func(repository *PostgresRepository, organizationName string) {
			<-start
			results <- repository.CompleteSetup(ctx, organizationName)
		}(repo, organizationNames[index])
	}
	close(start)

	succeeded := 0
	conflicted := 0
	for range repositories {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrSetupCompleted):
			conflicted++
		default:
			t.Fatalf("CompleteSetup() unexpected error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent initialization results: succeeded=%d conflicted=%d", succeeded, conflicted)
	}
	for index, repo := range repositories {
		if err := repo.Close(); err != nil {
			t.Fatalf("Close(%d): %v", index, err)
		}
	}

	reopened, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("reopen NewPostgresRepository(): %v", err)
	}
	defer reopened.Close()
	values, err := reopened.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll(): %v", err)
	}
	if !parseBool(values[KeySetupCompleted]) || (values[KeySiteName] != organizationNames[0] && values[KeySiteName] != organizationNames[1]) {
		t.Fatalf("persisted setup is inconsistent: %#v", values)
	}
}

func TestPostgresInvitationCodeConsumptionIsAtomic(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	repo, err := NewPostgresRepository(t.Context(), schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	testInvitationCodeConsumptionIsAtomic(t, repo)
}

func TestPostgresReplaceIfUnchangedIsAtomicAndPersistent(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	repo, err := NewPostgresRepository(t.Context(), schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMultiple(t.Context(), map[string]string{"first": "old-first", "second": "old-second"}); err != nil {
		t.Fatal(err)
	}
	err = repo.ReplaceIfUnchanged(t.Context(), map[string]ValueReplacement{
		"first":  {Expected: "old-first", Value: "new-first"},
		"second": {Expected: "stale-second", Value: "new-second"},
	})
	if !errors.Is(err, ErrSettingsChanged) {
		t.Fatalf("ReplaceIfUnchanged(stale) error = %v", err)
	}
	values, err := repo.GetAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if values["first"] != "old-first" || values["second"] != "old-second" {
		t.Fatalf("failed replacement partially changed settings: %#v", values)
	}
	if err := repo.ReplaceIfUnchanged(t.Context(), map[string]ValueReplacement{
		"first":  {Expected: "old-first", Value: "new-first"},
		"second": {Expected: "old-second", Value: "new-second"},
	}); err != nil {
		t.Fatalf("ReplaceIfUnchanged(valid): %v", err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewPostgresRepository(t.Context(), schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	values, err = reopened.GetAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if values["first"] != "new-first" || values["second"] != "new-second" {
		t.Fatalf("replacement did not persist: %#v", values)
	}
}

func TestPostgresSetIfAbsentDoesNotOverwrite(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	repo, err := NewPostgresRepository(t.Context(), schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	inserted, err := repo.SetIfAbsent(t.Context(), "key", "first")
	if err != nil || !inserted {
		t.Fatalf("SetIfAbsent(first) = %v, %v", inserted, err)
	}
	inserted, err = repo.SetIfAbsent(t.Context(), "key", "second")
	if err != nil || inserted {
		t.Fatalf("SetIfAbsent(second) = %v, %v", inserted, err)
	}
	values, err := repo.GetAll(t.Context())
	if err != nil || values["key"] != "first" {
		t.Fatalf("GetAll() = %#v, %v", values, err)
	}
}
