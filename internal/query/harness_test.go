package query

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	_ "modernc.org/sqlite"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/secrets"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// An internal test, not an external one: these tests substitute the connector
// factory, and the whole point of [Executor] is that nothing outside this
// package can.

// PostgresURLEnv matches the rest of the project. Without it the tests that
// need a real cancellable source skip.
const PostgresURLEnv = "PIVOT_TEST_POSTGRES_URL"

// fixture is a pipeline over a real Pivot database, with a real SQLite source
// to run against.
type fixture struct {
	executor *Executor
	repos    *repo.Repositories
	ctx      context.Context
	orgID    uuid.UUID
	userID   uuid.UUID
	connID   uuid.UUID

	// opens counts how many times the pipeline asked for a connector, which
	// is how a test observes that authorization ran *before* anything opened.
	opens *atomic.Int64

	checker *fakeChecker
}

type fixtureOptions struct {
	// source is the SQLite file the stored connection points at. Empty means
	// a fresh one with a small table in it.
	ddl []string

	// connection overrides what is stored, for a source the pipeline cannot
	// actually open.
	kind string
	host string
}

func newFixture(t *testing.T, opts fixtureOptions) *fixture {
	t.Helper()

	path := filepath.Join(t.TempDir(), "source.db")

	if opts.kind == "" {
		opts.kind = string(connectors.KindSQLite)
		applyDDL(t, path, opts.ddl...)
	}

	db := openPivotDB(t)
	repos := repo.New(db, repo.WithSecrets(testCipher(t)))

	org, err := repos.System().CreateOrganization(t.Context(),
		repo.CreateOrganization{Name: "Acme", Slug: "acme"})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	userID := uuid.New()
	scope := tenant.MustNewScope(org.ID, uuid.NullUUID{UUID: userID, Valid: true})
	ctx := tenant.WithScope(t.Context(), scope)

	conn, err := repos.Connections.Create(ctx, repo.CreateConnection{
		Slug: "source", Name: "The source", Kind: opts.kind,
		Host: opts.host, Database: path, IsEnabled: true,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	var (
		opens   atomic.Int64
		checker = &fakeChecker{allow: true}
	)

	counting := func(cfg connectors.Config) (connectors.Connector, error) {
		opens.Add(1)

		return connectors.Open(cfg)
	}

	return &fixture{
		executor: NewExecutor(repos, checker, withOpener(counting)),
		repos:    repos,
		ctx:      ctx,
		orgID:    org.ID,
		userID:   userID,
		connID:   conn.ID,
		opens:    &opens,
		checker:  checker,
	}
}

// applyDDL builds the source file. The connector opens every SQLite file
// read-only, so the fixture cannot use it to create anything.
func applyDDL(t *testing.T, path string, statements ...string) {
	t.Helper()

	if len(statements) == 0 {
		statements = []string{
			`CREATE TABLE orders (id INTEGER PRIMARY KEY, region TEXT, total REAL)`,
			`INSERT INTO orders VALUES (1, 'emea', 10.5), (2, 'apac', 20.25), (3, 'emea', 30.0)`,
		}
	}

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open the source: %v", err)
	}

	defer func() { _ = db.Close() }()

	for _, statement := range statements {
		if _, err := db.ExecContext(t.Context(), statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

// observerConfig opens a database for reading only, with no migration.
func observerConfig(url string) config.DatabaseConfig {
	cfg := config.Default().Database
	cfg.URL = url
	cfg.AutoMigrate = false

	return cfg
}

func openPivotDB(t *testing.T) *store.DB {
	t.Helper()

	cfg := observerConfig(filepath.Join(t.TempDir(), "pivot.db"))

	db, err := store.Open(t.Context(), cfg, discardLogger())
	if err != nil {
		t.Fatalf("open pivot db: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	if err := store.Migrate(t.Context(), db, discardLogger()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return db
}

func testCipher(t *testing.T) secrets.Cipher {
	t.Helper()

	key, err := secrets.ParseKey("c2l4dGVlbi1ieXRlcy10aW1lcy10d28tZXhhY3RseSE=")
	if err != nil {
		t.Fatalf("parse test key: %v", err)
	}

	ring, err := secrets.NewKeyring(key)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}

	return ring
}

// drain reads an execution to the end and closes it, which is what a caller
// is supposed to do and what completes the log row.
func drain(t *testing.T, ex *Execution) int {
	t.Helper()

	n := 0
	for ex.Stream.Next() {
		n++
	}

	if err := ex.Stream.Err(); err != nil {
		t.Fatalf("stream: %v", err)
	}

	if err := ex.Stream.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	return n
}

// --- a checker that answers what the test tells it to ------------------------

type fakeChecker struct {
	allow bool
	asked atomic.Int64
	last  authz.Request
}

func (f *fakeChecker) Check(_ context.Context, req authz.Request) (authz.Decision, error) {
	f.asked.Add(1)
	f.last = req

	return authz.Decision{Allowed: f.allow}, nil
}

func (f *fakeChecker) Explain(ctx context.Context, req authz.Request) (authz.Explanation, error) {
	decision, err := f.Check(ctx, req)

	return authz.Explanation{Decision: decision, Request: req}, err
}

func discardLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }
