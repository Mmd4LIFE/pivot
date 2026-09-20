package store_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/store"
)

// PostgresURLEnv names the environment variable that points the suite at a
// PostgreSQL instance. `make test-all` sets it from the development container.
//
// Without it the PostgreSQL cases skip rather than fail, so `make test` stays
// runnable with no Docker. CI always sets it — a green run that silently
// skipped half the matrix would defeat the point of dual-engine support.
const PostgresURLEnv = "PIVOT_TEST_POSTGRES_URL"

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func testDBConfig(url string) config.DatabaseConfig {
	cfg := config.Default().Database
	cfg.URL = url
	cfg.AutoMigrate = false

	return cfg
}

// openSQLite returns a migrated, isolated SQLite database backed by a temp file.
//
// A file rather than :memory: because in-memory databases and connection pools
// interact badly, and because the file path is what production actually uses.
func openSQLite(t *testing.T) *store.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "pivot-test.db")

	db, err := store.Open(context.Background(), testDBConfig(path), discardLogger())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}

// openPostgres returns a PostgreSQL database in its own schema, or skips.
//
// Each test gets a private schema with search_path pointed at it, so the whole
// suite can run in parallel against one container without tests colliding.
func openPostgres(t *testing.T) *store.DB {
	t.Helper()

	url := os.Getenv(PostgresURLEnv)
	if url == "" {
		t.Skipf("%s not set; run `make test-all` to include PostgreSQL", PostgresURLEnv)
	}

	schema := "test_" + sanitize(t.Name())

	admin, err := store.Open(context.Background(), testDBConfig(url), discardLogger())
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = admin.Close() }()

	ctx := context.Background()
	if _, dropErr := admin.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); dropErr != nil {
		t.Fatalf("drop schema %s: %v", schema, dropErr)
	}

	if _, createErr := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); createErr != nil {
		t.Fatalf("create schema %s: %v", schema, createErr)
	}

	scoped := url
	if filepath.Ext(scoped) == "" { // always true; keeps the intent explicit
		sep := "?"
		if containsRune(scoped, '?') {
			sep = "&"
		}

		scoped += sep + "search_path=" + schema
	}

	db, err := store.Open(ctx, testDBConfig(scoped), discardLogger())
	if err != nil {
		t.Fatalf("open postgres schema %s: %v", schema, err)
	}

	t.Cleanup(func() {
		_ = db.Close()

		cleanup, err := store.Open(context.Background(), testDBConfig(url), discardLogger())
		if err != nil {
			return
		}
		defer func() { _ = cleanup.Close() }()

		_, _ = cleanup.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	return db
}

// eachEngine runs fn against every available engine.
//
// This is the shape the portability requirement takes in practice: a store
// test written once and executed on both backends, so a divergence surfaces as
// a failing test rather than as a production surprise.
func eachEngine(t *testing.T, fn func(t *testing.T, db *store.DB)) {
	t.Helper()

	t.Run("sqlite", func(t *testing.T) {
		t.Parallel()
		fn(t, openSQLite(t))
	})

	t.Run("postgres", func(t *testing.T) {
		t.Parallel()
		fn(t, openPostgres(t))
	})
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}

	return false
}

// sanitize reduces a test name to a safe SQL identifier.
func sanitize(in string) string {
	out := make([]rune, 0, len(in))

	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		default:
			out = append(out, '_')
		}
	}

	if len(out) > 40 {
		out = out[:40]
	}

	return string(out)
}

// mustExec fails the test if a statement errors.
func mustExec(t *testing.T, db *store.DB, query string, args ...any) {
	t.Helper()

	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// rebind converts ? placeholders to $N for PostgreSQL.
//
// Part 3-b replaces this with sqlc-generated queries per dialect; it exists so
// the Part 3-a tests can be written once.
func rebind(db *store.DB, query string) string {
	if db.IsSQLite() {
		return query
	}

	out := make([]rune, 0, len(query)+8)
	n := 0

	for _, r := range query {
		if r == '?' {
			n++
			out = append(out, '$')
			out = append(out, []rune(itoa(n))...)

			continue
		}

		out = append(out, r)
	}

	return string(out)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var buf [8]byte

	i := len(buf)

	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}

	return string(buf[i:])
}
