package authz_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

const postgresURLEnv = "PIVOT_TEST_POSTGRES_URL"

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func testDBConfig(url string) config.DatabaseConfig {
	cfg := config.Default().Database
	cfg.URL = url
	cfg.AutoMigrate = false

	return cfg
}

func openSQLite(t *testing.T) *store.DB {
	t.Helper()

	db, err := store.Open(context.Background(),
		testDBConfig(filepath.Join(t.TempDir(), "authz-test.db")), discardLogger())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}

func openPostgres(t *testing.T) *store.DB {
	t.Helper()

	url := os.Getenv(postgresURLEnv)
	if url == "" {
		t.Skipf("%s not set; run `make test-all` to include PostgreSQL", postgresURLEnv)
	}

	schema := "authz_" + sanitize(t.Name())

	admin, err := store.Open(context.Background(), testDBConfig(url), discardLogger())
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

	db, err := store.Open(ctx, testDBConfig(url+sep+"search_path="+schema), discardLogger())
	if err != nil {
		t.Fatalf("open postgres schema: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()

		cleanup, cerr := store.Open(context.Background(), testDBConfig(url), discardLogger())
		if cerr != nil {
			return
		}
		defer func() { _ = cleanup.Close() }()

		_, _ = cleanup.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	return db
}

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

// bothEngines runs a test against SQLite and, when configured, PostgreSQL.
//
// A permission model that behaves differently per engine would be a security
// bug rather than a portability annoyance, so every assertion runs on both.
func bothEngines(t *testing.T, run func(t *testing.T, db *store.DB)) {
	t.Helper()

	t.Run("sqlite", func(t *testing.T) { run(t, openSQLite(t)) })
	t.Run("postgres", func(t *testing.T) { run(t, openPostgres(t)) })
}

// newOrg migrates a database and creates one organization, returning a context
// already scoped to it.
func newOrg(t *testing.T, db *store.DB) (*repo.Repositories, context.Context, model.Organization) {
	t.Helper()

	if err := store.Migrate(context.Background(), db, discardLogger()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repos := repo.New(db)

	org, err := repos.System().CreateOrganization(context.Background(),
		repo.CreateOrganization{Name: "Acme", Slug: "acme"})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	ctx := tenant.WithScope(context.Background(),
		tenant.MustNewScope(org.ID, uuid.NullUUID{}))

	return repos, ctx, org
}
