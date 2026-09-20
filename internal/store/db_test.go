package store_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/store"
)

func TestParseURL(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in         string
		wantEngine store.Engine
		wantInDSN  string
		wantErr    bool
	}{
		"bare filename":     {"pivot.db", store.EngineSQLite, "pivot.db", false},
		"relative path":     {"./data/pivot.db", store.EngineSQLite, "data/pivot.db", false},
		"absolute path":     {"/var/lib/pivot/pivot.db", store.EngineSQLite, "/var/lib/pivot/pivot.db", false},
		"sqlite scheme":     {"sqlite://data/pivot.db", store.EngineSQLite, "data/pivot.db", false},
		"sqlite3 scheme":    {"sqlite3://x.db", store.EngineSQLite, "x.db", false},
		"file scheme":       {"file:pivot.db", store.EngineSQLite, "pivot.db", false},
		"memory":            {":memory:", store.EngineSQLite, ":memory:", false},
		"postgres":          {"postgres://u:p@localhost/pivot", store.EnginePostgres, "postgres://", false},
		"postgresql":        {"postgresql://u:p@localhost/pivot", store.EnginePostgres, "postgresql://", false},
		"uppercase scheme":  {"POSTGRES://u@h/db", store.EnginePostgres, "POSTGRES://", false},
		"empty":             {"", "", "", true},
		"whitespace":        {"   ", "", "", true},
		"unsupported":       {"mysql://u@h/db", "", "", true},
		"unsupported mongo": {"mongodb://h/db", "", "", true},
		"sqlite no path":    {"sqlite://", "", "", true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			engine, dsn, err := store.ParseURL(tc.in)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseURL(%q) = (%q, %q, nil); want an error", tc.in, engine, dsn)
				}

				return
			}

			if err != nil {
				t.Fatalf("ParseURL(%q): %v", tc.in, err)
			}

			if engine != tc.wantEngine {
				t.Errorf("engine = %q, want %q", engine, tc.wantEngine)
			}

			if !strings.Contains(dsn, tc.wantInDSN) {
				t.Errorf("dsn = %q, want it to contain %q", dsn, tc.wantInDSN)
			}
		})
	}
}

// Foreign key enforcement is off by default in SQLite, so the pragma must be
// in every DSN we build. Asserting on the string is crude but it is the layer
// where an omission would go unnoticed.
func TestSQLiteDSNCarriesRequiredPragmas(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"pivot.db", "sqlite://x.db", ":memory:", "file:y.db"} {
		_, dsn, err := store.ParseURL(in)
		if err != nil {
			t.Fatalf("ParseURL(%q): %v", in, err)
		}

		for _, pragma := range []string{"foreign_keys(1)", "journal_mode(WAL)", "busy_timeout"} {
			if !strings.Contains(dsn, pragma) {
				t.Errorf("dsn for %q = %q, missing pragma %q", in, dsn, pragma)
			}
		}
	}
}

func TestOpenSQLiteCreatesUsableHandle(t *testing.T) {
	t.Parallel()

	db := openSQLite(t)

	if db.Engine() != store.EngineSQLite {
		t.Errorf("engine = %q, want sqlite", db.Engine())
	}

	if !db.IsSQLite() || db.IsPostgres() {
		t.Error("engine predicates disagree with Engine()")
	}

	if err := db.HealthCheck(context.Background()); err != nil {
		t.Errorf("HealthCheck: %v", err)
	}
}

func TestOpenPostgresCreatesUsableHandle(t *testing.T) {
	t.Parallel()

	db := openPostgres(t)

	if db.Engine() != store.EnginePostgres {
		t.Errorf("engine = %q, want postgres", db.Engine())
	}

	if !db.IsPostgres() || db.IsSQLite() {
		t.Error("engine predicates disagree with Engine()")
	}

	if err := db.HealthCheck(context.Background()); err != nil {
		t.Errorf("HealthCheck: %v", err)
	}
}

func TestOpenRejectsUnreachableDatabase(t *testing.T) {
	t.Parallel()

	cfg := testDBConfig("postgres://nobody:secret-password@127.0.0.1:1/nope?sslmode=disable&connect_timeout=2")

	_, err := store.Open(context.Background(), cfg, discardLogger())
	if err == nil {
		t.Fatal("Open succeeded against an unreachable database")
	}

	// A connection error must not leak the password into logs or output.
	if strings.Contains(err.Error(), "secret-password") {
		t.Errorf("error leaks the password: %v", err)
	}
}

func TestOpenRejectsInvalidURL(t *testing.T) {
	t.Parallel()

	_, err := store.Open(context.Background(), testDBConfig("mysql://u@h/db"), discardLogger())
	if err == nil {
		t.Fatal("Open accepted an unsupported database URL")
	}
}

// The default configuration must work with no setup at all — that is the
// 30-second-install commitment.
func TestDefaultConfigOpensWithoutSetup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	cfg := testDBConfig(filepath.Join(dir, "pivot.db"))

	db, err := store.Open(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatalf("Open with defaults: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := store.Migrate(context.Background(), db, discardLogger()); err != nil {
		t.Fatalf("Migrate with defaults: %v", err)
	}
}
