package store_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/store"
)

// This file is the P0-META-004 harness. ADR-0003 accepts a portability tax in
// exchange for the zero-config install; these tests are what stop that tax
// being paid in production incidents instead of in CI.

// expectedTables is the schema contract. Both engines must produce exactly
// this set — no more, no less.
var expectedTables = []string{
	"group_members",
	"groups",
	"organizations",
	"user_attributes",
	"users",
}

// expectedColumns lists the columns each table must expose on every engine.
// Types differ by design (UUID vs TEXT, TIMESTAMPTZ vs TEXT); names and
// nullability must not.
var expectedColumns = map[string][]string{
	"organizations": {
		"created_at", "created_by", "deleted_at", "id", "name", "plan",
		"settings", "slug", "updated_at", "updated_by", "version",
	},
	"users": {
		"avatar_url", "created_at", "created_by", "deleted_at", "email", "id",
		"is_active", "last_login_at", "locale", "name", "org_id",
		"password_hash", "timezone", "updated_at", "updated_by", "version",
	},
	"user_attributes": {
		"created_at", "id", "key", "org_id", "source", "updated_at", "user_id", "value",
	},
	"groups": {
		"created_at", "created_by", "deleted_at", "description", "external_id",
		"id", "name", "org_id", "parent_group_id", "updated_at", "updated_by",
		"version",
	},
	"group_members": {
		"added_at", "added_by", "group_id", "org_id", "user_id",
	},
}

func migrated(t *testing.T, db *store.DB) *store.DB {
	t.Helper()

	if err := store.Migrate(context.Background(), db, discardLogger()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return db
}

func TestMigrationsCreateExpectedTables(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		migrated(t, db)

		got := listTables(t, db)
		if !equalStrings(got, expectedTables) {
			t.Errorf("tables on %s =\n  %v\nwant\n  %v", db.Engine(), got, expectedTables)
		}
	})
}

func TestMigrationsCreateExpectedColumns(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		migrated(t, db)

		for table, want := range expectedColumns {
			got := listColumns(t, db, table)
			if !equalStrings(got, want) {
				t.Errorf("columns of %s on %s =\n  %v\nwant\n  %v",
					table, db.Engine(), got, want)
			}
		}
	})
}

// Running migrations twice must change nothing. An operator restarting a
// container should never wonder whether it is safe.
func TestMigrationsAreIdempotent(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()

		migrated(t, db)

		first, err := store.CurrentVersion(ctx, db)
		if err != nil {
			t.Fatalf("version: %v", err)
		}

		tablesBefore := listTables(t, db)

		if err = store.Migrate(ctx, db, discardLogger()); err != nil {
			t.Fatalf("second migrate: %v", err)
		}

		second, err := store.CurrentVersion(ctx, db)
		if err != nil {
			t.Fatalf("version: %v", err)
		}

		if first != second {
			t.Errorf("version changed on re-run: %d then %d", first, second)
		}

		if got := listTables(t, db); !equalStrings(got, tablesBefore) {
			t.Errorf("tables changed on re-run:\n  %v\nwas\n  %v", got, tablesBefore)
		}
	})
}

func TestStatusReportsAppliedMigrations(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()

		pending, err := store.Status(ctx, db)
		if err != nil {
			t.Fatalf("status before migrate: %v", err)
		}

		if len(pending) == 0 {
			t.Fatal("status reported no migrations at all")
		}

		for _, s := range pending {
			if s.Applied {
				t.Errorf("migration %d reported applied before migrating", s.Version)
			}
		}

		migrated(t, db)

		applied, err := store.Status(ctx, db)
		if err != nil {
			t.Fatalf("status after migrate: %v", err)
		}

		for _, s := range applied {
			if !s.Applied {
				t.Errorf("migration %d still pending after migrate", s.Version)
			}
		}
	})
}

func TestCurrentVersionStartsAtZero(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		v, err := store.CurrentVersion(context.Background(), db)
		if err != nil {
			t.Fatalf("version: %v", err)
		}

		if v != 0 {
			t.Errorf("fresh database reports version %d, want 0", v)
		}
	})
}

// Foreign keys are the portability trap this whole setup exists to catch:
// SQLite silently ignores REFERENCES unless foreign_keys is enabled, so
// without the pragma every constraint below would pass on PostgreSQL and be
// decorative on SQLite.
func TestForeignKeysAreEnforced(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		migrated(t, db)

		_, err := db.ExecContext(context.Background(),
			rebind(db, `INSERT INTO users (id, org_id, email) VALUES (?, ?, ?)`),
			"11111111-1111-1111-1111-111111111111",
			"22222222-2222-2222-2222-222222222222", // no such organization
			"orphan@example.com",
		)
		if err == nil {
			t.Fatalf("inserting a user with a nonexistent org_id succeeded on %s; "+
				"foreign keys are not being enforced", db.Engine())
		}
	})
}

// ON DELETE CASCADE must behave identically on both engines.
func TestCascadeDeleteRemovesChildren(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		migrated(t, db)

		const (
			orgID  = "33333333-3333-3333-3333-333333333333"
			userID = "44444444-4444-4444-4444-444444444444"
		)

		mustExec(t, db, rebind(db, `INSERT INTO organizations (id, name, slug) VALUES (?, ?, ?)`),
			orgID, "Acme", "acme")
		mustExec(t, db, rebind(db, `INSERT INTO users (id, org_id, email) VALUES (?, ?, ?)`),
			userID, orgID, "a@example.com")
		mustExec(t, db, rebind(db,
			`INSERT INTO user_attributes (id, org_id, user_id, key, value) VALUES (?, ?, ?, ?, ?)`),
			"55555555-5555-5555-5555-555555555555", orgID, userID, "region", "EU")

		mustExec(t, db, rebind(db, `DELETE FROM organizations WHERE id = ?`), orgID)

		for _, table := range []string{"users", "user_attributes"} {
			var n int

			row := db.QueryRowContext(context.Background(),
				rebind(db, `SELECT count(*) FROM `+table+` WHERE org_id = ?`), orgID)
			if err := row.Scan(&n); err != nil {
				t.Fatalf("count %s: %v", table, err)
			}

			if n != 0 {
				t.Errorf("%s still has %d rows on %s after the org was deleted",
					table, n, db.Engine())
			}
		}
	})
}

// A soft-deleted row must not block reuse of a unique value. Both engines
// implement this with a partial index, and both must honor it.
func TestPartialUniqueIndexIgnoresSoftDeletedRows(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		migrated(t, db)

		mustExec(t, db, rebind(db,
			`INSERT INTO organizations (id, name, slug, deleted_at) VALUES (?, ?, ?, ?)`),
			"66666666-6666-6666-6666-666666666666", "Old Acme", "acme", "2026-01-01T00:00:00.000Z")

		// The slug is free again because the first row is soft-deleted.
		mustExec(t, db, rebind(db, `INSERT INTO organizations (id, name, slug) VALUES (?, ?, ?)`),
			"77777777-7777-7777-7777-777777777777", "New Acme", "acme")

		// A second live row with the same slug must be rejected.
		_, err := db.ExecContext(context.Background(),
			rebind(db, `INSERT INTO organizations (id, name, slug) VALUES (?, ?, ?)`),
			"88888888-8888-8888-8888-888888888888", "Third Acme", "acme")
		if err == nil {
			t.Errorf("a duplicate live slug was accepted on %s", db.Engine())
		}
	})
}

// CHECK constraints must be enforced on both engines.
func TestCheckConstraintRejectsUnknownAttributeSource(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		migrated(t, db)

		const (
			orgID  = "99999999-9999-9999-9999-999999999999"
			userID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
		)

		mustExec(t, db, rebind(db, `INSERT INTO organizations (id, name, slug) VALUES (?, ?, ?)`),
			orgID, "Acme", "acme-check")
		mustExec(t, db, rebind(db, `INSERT INTO users (id, org_id, email) VALUES (?, ?, ?)`),
			userID, orgID, "check@example.com")

		_, err := db.ExecContext(context.Background(), rebind(db,
			`INSERT INTO user_attributes (id, org_id, user_id, key, value, source)
			 VALUES (?, ?, ?, ?, ?, ?)`),
			"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", orgID, userID, "region", "EU", "telepathy")
		if err == nil {
			t.Errorf("an invalid attribute source was accepted on %s", db.Engine())
		}
	})
}

// Defaults must produce equivalent values on both engines. A column that
// defaults on one and errors on the other is a latent outage.
func TestColumnDefaultsApplyOnBothEngines(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		migrated(t, db)

		const orgID = "cccccccc-cccc-cccc-cccc-cccccccccccc"

		mustExec(t, db, rebind(db, `INSERT INTO organizations (id, name, slug) VALUES (?, ?, ?)`),
			orgID, "Defaults", "defaults")

		var (
			plan      string
			settings  string
			version   int64
			createdAt string
		)

		row := db.QueryRowContext(context.Background(), rebind(db,
			`SELECT plan, settings, version, CAST(created_at AS TEXT)
			   FROM organizations WHERE id = ?`), orgID)
		if err := row.Scan(&plan, &settings, &version, &createdAt); err != nil {
			t.Fatalf("scan defaults: %v", err)
		}

		if plan != "free" {
			t.Errorf("plan default = %q, want %q", plan, "free")
		}

		if strings.TrimSpace(settings) != "{}" {
			t.Errorf("settings default = %q, want %q", settings, "{}")
		}

		if version != 1 {
			t.Errorf("version default = %d, want 1", version)
		}

		if createdAt == "" {
			t.Error("created_at default did not populate")
		}
	})
}

// --- schema introspection -------------------------------------------------

func listTables(t *testing.T, db *store.DB) []string {
	t.Helper()

	query := `SELECT name FROM sqlite_master
	           WHERE type = 'table'
	             AND name NOT LIKE 'sqlite_%'
	             AND name NOT LIKE 'goose_%'`

	if db.IsPostgres() {
		query = `SELECT table_name FROM information_schema.tables
		          WHERE table_schema = current_schema()
		            AND table_type = 'BASE TABLE'
		            AND table_name NOT LIKE 'goose_%'`
	}

	return queryStrings(t, db, query)
}

func listColumns(t *testing.T, db *store.DB, table string) []string {
	t.Helper()

	if db.IsPostgres() {
		return queryStrings(t, db,
			`SELECT column_name FROM information_schema.columns
			  WHERE table_schema = current_schema() AND table_name = $1`, table)
	}

	return queryStrings(t, db, `SELECT name FROM pragma_table_info(?)`, table)
}

func queryStrings(t *testing.T, db *store.DB, query string, args ...any) []string {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer func() { _ = rows.Close() }()

	var out []string

	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan: %v", err)
		}

		out = append(out, s)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	sort.Strings(out)

	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
