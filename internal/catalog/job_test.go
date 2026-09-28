package catalog_test

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	_ "modernc.org/sqlite"

	"github.com/Mmd4LIFE/pivot/internal/catalog"
	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/jobs"
	"github.com/Mmd4LIFE/pivot/internal/secrets"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

/*
The catalog sync as scheduled work, end to end.

A real store, a real source, and River in between. The reconciliation is
covered exhaustively elsewhere against a fake; what is here is the wiring, and
wiring is precisely what a fake cannot vouch for -- that the sweep finds the
connections, that the job it enqueues reaches a worker, and that the worker
scopes itself to the right tenant before touching anything.

The last of those is the one worth a real database. Background work has no
user, so it runs under a system scope, and a system scope that resolved to the
wrong organization would read one tenant's connections and write another's
catalog. The isolation harness cannot see this path because nothing here goes
through a request.
*/

func TestTheSweepSyncsEveryConnection(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	dir := t.TempDir()

	// Pivot's own store.
	db, err := store.Open(t.Context(), config.DatabaseConfig{
		URL: "sqlite://" + filepath.Join(dir, "pivot.db"),
	}, quiet)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}

	defer func() { _ = db.Close() }()

	if merr := store.Migrate(t.Context(), db, quiet); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}

	if _, merr := jobs.Migrate(t.Context(), db); merr != nil {
		t.Fatalf("migrate jobs: %v", merr)
	}

	repos := repo.New(db, repo.WithSecrets(testCipher(t)))

	org, err := repos.System().CreateOrganization(t.Context(), repo.CreateOrganization{
		Name: "Acme", Slug: "acme",
	})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	ctx := tenant.WithScope(t.Context(), tenant.MustNewScope(org.ID, uuid.NullUUID{}))

	// A source with something in it.
	sourcePath := filepath.Join(dir, "warehouse.db")
	newSourceAt(t, sourcePath, createParent, createChild)

	conn, err := repos.Connections.Create(ctx, repo.CreateConnection{
		Slug: "warehouse", Name: "Warehouse", Kind: "sqlite",
		Database: sourcePath, IsEnabled: true,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	// The runner, with both workers, exactly as `pivot serve` builds it.
	workers := river.NewWorkers()
	river.AddWorker(workers, &catalog.SweepWorker{Repos: repos, Log: quiet})
	river.AddWorker(workers, &catalog.SyncWorker{Repos: repos, Log: quiet})

	runner, err := jobs.New(db, jobs.Options{Workers: workers, Logger: quiet})
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	if err = runner.Start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}

	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		if serr := runner.Stop(stop); serr != nil {
			t.Errorf("stop: %v", serr)
		}
	}()

	if _, err = runner.Client().Insert(t.Context(), catalog.SweepArgs{}, nil); err != nil {
		t.Fatalf("enqueue the sweep: %v", err)
	}

	// The catalog fills in, without anybody calling the syncer.
	deadline := time.Now().Add(30 * time.Second)

	for {
		tables, terr := repos.Catalog.Tables(ctx, conn.ID)
		if terr != nil {
			t.Fatalf("read the catalog: %v", terr)
		}

		if len(tables) >= 2 {
			// And the relationship came with it, which means the whole sync
			// ran rather than just the table half.
			keys, kerr := repos.Catalog.ForeignKeys(ctx, conn.ID)
			if kerr != nil {
				t.Fatalf("read the relationships: %v", kerr)
			}

			if len(keys) != 2 {
				t.Errorf("%d foreign key columns, want 2", len(keys))
			}

			return
		}

		if time.Now().After(deadline) {
			failed := listFailures(t, runner)

			t.Fatalf("the catalog is still empty after 30s; %d tables%s",
				len(tables), failed)
		}

		time.Sleep(200 * time.Millisecond)
	}
}

/*
A connection that is disabled is not synced.

Disabling one is how somebody stops Pivot touching a warehouse -- during an
incident, or while a credential is rotated. Background work that kept reaching
it would make the switch useless exactly when it matters.
*/
func TestTheSweepSkipsDisabledConnections(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	dir := t.TempDir()

	db, err := store.Open(t.Context(), config.DatabaseConfig{
		URL: "sqlite://" + filepath.Join(dir, "pivot.db"),
	}, quiet)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer func() { _ = db.Close() }()

	if merr := store.Migrate(t.Context(), db, quiet); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}

	if _, merr := jobs.Migrate(t.Context(), db); merr != nil {
		t.Fatalf("migrate jobs: %v", merr)
	}

	repos := repo.New(db, repo.WithSecrets(testCipher(t)))

	org, err := repos.System().CreateOrganization(t.Context(), repo.CreateOrganization{
		Name: "Acme", Slug: "acme",
	})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	ctx := tenant.WithScope(t.Context(), tenant.MustNewScope(org.ID, uuid.NullUUID{}))

	sourcePath := filepath.Join(dir, "warehouse.db")
	newSourceAt(t, sourcePath, createParent, createChild)

	conn, err := repos.Connections.Create(ctx, repo.CreateConnection{
		Slug: "warehouse", Name: "Warehouse", Kind: "sqlite",
		Database: sourcePath, IsEnabled: false,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, &catalog.SweepWorker{Repos: repos, Log: quiet})
	river.AddWorker(workers, &catalog.SyncWorker{Repos: repos, Log: quiet})

	runner, err := jobs.New(db, jobs.Options{Workers: workers, Logger: quiet})
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	if err = runner.Start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}

	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = runner.Stop(stop)
	}()

	if _, err = runner.Client().Insert(t.Context(), catalog.SweepArgs{}, nil); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Long enough for the sweep to have run and fanned out if it were going to.
	time.Sleep(4 * time.Second)

	tables, err := repos.Catalog.Tables(ctx, conn.ID)
	if err != nil {
		t.Fatalf("read the catalog: %v", err)
	}

	if len(tables) != 0 {
		t.Errorf("a disabled connection was synced: %d tables", len(tables))
	}
}

// newSourceAt writes a source database at a chosen path.
func newSourceAt(t *testing.T, path string, statements ...string) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("create the source: %v", err)
	}

	defer func() { _ = db.Close() }()

	for _, statement := range statements {
		if _, err = db.ExecContext(t.Context(), statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

// listFailures renders any failed jobs, so a timeout says why rather than
// only that it timed out.
func listFailures(t *testing.T, runner *jobs.Runner) string {
	t.Helper()

	list, err := runner.Client().JobList(t.Context(),
		river.NewJobListParams().
			States(rivertype.JobStateRetryable, rivertype.JobStateDiscarded).
			First(5))
	if err != nil || len(list.Jobs) == 0 {
		return ""
	}

	out := "; failed jobs:"
	for _, job := range list.Jobs {
		reason := "(no error recorded)"
		if len(job.Errors) > 0 {
			reason = job.Errors[len(job.Errors)-1].Error
		}

		out += "\n  " + job.Kind + ": " + reason
	}

	return out
}

// testCipher builds a cipher for sealing connection passwords. A fixed key,
// because what is under test is the scheduling rather than the cryptography.
func testCipher(t *testing.T) secrets.Cipher {
	t.Helper()

	key, err := secrets.ParseKey("c2l4dGVlbi1ieXRlcy10aW1lcy10d28tZXhhY3RseSE=")
	if err != nil {
		t.Fatalf("parse the test key: %v", err)
	}

	ring, err := secrets.NewKeyring(key)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}

	return ring
}
