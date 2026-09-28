package query

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/store"
)

/*
The cache status reaches both engines' query_log.

The rest of this package's tests run the pipeline over SQLite, which is the
right default -- they need no container and they exercise the logic. But
`cache_status` is a column on two schemas written through two generated
statements, and 00011 constrains it with a CHECK on each. A value the pipeline
produces and PostgreSQL rejects would pass every other test here.

So this one runs the whole pipeline against a PostgreSQL metadata database and
reads the column back. The source stays SQLite: what is under test is where
Pivot writes its own row, not where the rows came from.
*/
func TestTheCacheStatusIsWrittenOnBothEngines(t *testing.T) {
	t.Parallel()

	for name, open := range map[string]func(*testing.T) *store.DB{
		"sqlite":   openPivotDB,
		"postgres": openPivotPostgres,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newCacheFixtureOn(t, open(t))
			ctx := f.as(t, f.analyst)

			first, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
			if err != nil {
				t.Fatalf("first: %v", err)
			}

			drain(t, first)

			second, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
			if err != nil {
				t.Fatalf("second: %v", err)
			}

			drain(t, second)

			entries, err := f.repos.QueryLog.List(ctx, 10)
			if err != nil {
				t.Fatalf("list: %v", err)
			}

			if len(entries) != 2 {
				t.Fatalf("two executions left %d log entries", len(entries))
			}

			if entries[0].CacheStatus != CacheHit {
				t.Errorf("the cached execution logged %q, want hit", entries[0].CacheStatus)
			}

			if entries[1].CacheStatus != CacheMiss {
				t.Errorf("the first execution logged %q, want miss", entries[1].CacheStatus)
			}
		})
	}
}

/*
And the CHECK added in 00011 actually refuses a value outside the vocabulary.

Written with SQL rather than through the repository, because the repository is
what the constraint is defending against: a future code path that writes a
typo. Asking the repository to prove the database would refuse it is asking the
wrong layer.
*/
func TestTheDatabaseRefusesAnUnknownCacheStatus(t *testing.T) {
	t.Parallel()

	for name, open := range map[string]func(*testing.T) *store.DB{
		"sqlite":   openPivotDB,
		"postgres": openPivotPostgres,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newCacheFixtureOn(t, open(t))
			ctx := f.as(t, f.analyst)

			ex, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}

			drain(t, ex)

			_, err = f.db.ExecContext(context.Background(),
				"UPDATE query_log SET cache_status = 'warm'")
			if err == nil {
				t.Fatal("the database accepted a cache_status outside the vocabulary")
			}

			if !strings.Contains(strings.ToLower(err.Error()), "constraint") &&
				!strings.Contains(strings.ToLower(err.Error()), "check") {
				t.Errorf("refused for the wrong reason: %v", err)
			}
		})
	}
}

// openPivotPostgres opens a migrated PostgreSQL metadata database in a schema
// of its own, the way internal/store/repo's harness does.
func openPivotPostgres(t *testing.T) *store.DB {
	t.Helper()

	url := os.Getenv(PostgresURLEnv)
	if url == "" {
		t.Skipf("%s not set; run `make test-all` to include PostgreSQL", PostgresURLEnv)
	}

	schema := "query_" + strings.NewReplacer("/", "_", "-", "_").Replace(strings.ToLower(t.Name()))

	admin, err := store.Open(t.Context(), observerConfig(url), discardLogger())
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}

	defer func() { _ = admin.Close() }()

	ctx := context.Background()
	if _, derr := admin.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); derr != nil {
		t.Fatalf("drop schema: %v", derr)
	}

	if _, cerr := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); cerr != nil {
		t.Fatalf("create schema: %v", cerr)
	}

	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}

	cfg := observerConfig(url + sep + "search_path=" + schema)

	db, err := store.Open(ctx, cfg, discardLogger())
	if err != nil {
		t.Fatalf("open the schema: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()

		cleanup, cerr := store.Open(context.Background(), observerConfig(url), discardLogger())
		if cerr != nil {
			return
		}

		defer func() { _ = cleanup.Close() }()

		_, _ = cleanup.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	if merr := store.Migrate(ctx, db, discardLogger()); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}

	return db
}
