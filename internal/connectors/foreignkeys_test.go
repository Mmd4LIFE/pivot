package connectors_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
)

/*
Foreign key discovery, on every connector this run can reach.

The fixture is a **composite** key on purpose. Every one of these sources
exposes a relationship as two column lists, and the obvious way to read them
back pairs every column of one with every column of the other -- which for a
two-column key yields four rows rather than two, and a relationship that joins
on columns never related to each other. It returns rows, so nothing looks
wrong.

That is not hypothetical. The standard information_schema query, the one in
every blog post on the subject, does exactly this on PostgreSQL. Measured
before the connector was written:

	zz_child_..._fkey  child.tenant_id -> parent.tenant_id  ord 1
	zz_child_..._fkey  child.tenant_id -> parent.code       ord 1   <- wrong
	zz_child_..._fkey  child.code      -> parent.tenant_id  ord 2   <- wrong
	zz_child_..._fkey  child.code      -> parent.code       ord 2

A single-column fixture would have passed against that.
*/

const (
	fkParentDDL = `CREATE TABLE fk_parent (
		tenant_id INTEGER NOT NULL,
		code      INTEGER NOT NULL,
		PRIMARY KEY (tenant_id, code)
	)`

	fkChildDDL = `CREATE TABLE fk_child (
		id        INTEGER NOT NULL PRIMARY KEY,
		tenant_id INTEGER NOT NULL,
		code      INTEGER NOT NULL,
		CONSTRAINT fk_child_parent FOREIGN KEY (tenant_id, code)
			REFERENCES fk_parent (tenant_id, code)
	)`
)

// fkTarget is a connector to look for relationships in, and how to put them
// there -- which differs because a read-only connector cannot.
type fkTarget struct {
	connector func(t *testing.T) connectors.Connector
}

func fkTargets(t *testing.T) map[string]fkTarget {
	t.Helper()

	targets := map[string]fkTarget{
		// Seeded through a separate read-write handle, because the connector
		// opens every file read-only.
		"sqlite": {connector: func(t *testing.T) connectors.Connector {
			path := filepath.Join(t.TempDir(), "fk.db")

			db, err := sql.Open("sqlite", "file:"+path)
			if err != nil {
				t.Fatalf("create: %v", err)
			}

			defer func() { _ = db.Close() }()

			for _, statement := range []string{fkParentDDL, fkChildDDL} {
				if _, err = db.ExecContext(t.Context(), statement); err != nil {
					t.Fatalf("%s: %v", statement, err)
				}
			}

			return open(t, connectors.Config{
				Kind: connectors.KindSQLite, Database: path,
			})
		}},
	}

	if os.Getenv(postgresURLEnv) != "" {
		targets["postgres"] = fkTarget{connector: func(t *testing.T) connectors.Connector {
			return seedServerFK(t, liveTarget(t).config())
		}}
	}

	if os.Getenv(mysqlURLEnv) != "" {
		targets["mysql"] = fkTarget{connector: func(t *testing.T) connectors.Connector {
			return seedServerFK(t, mysqlConfig(t))
		}}
	}

	return targets
}

// seedServerFK builds the fixture on a server the connector can write to, and
// takes it down afterwards.
func seedServerFK(t *testing.T, cfg connectors.Config) connectors.Connector {
	t.Helper()

	c := open(t, cfg)

	drop := func() {
		_, _ = c.Query(t.Context(), "DROP TABLE IF EXISTS fk_child")
		_, _ = c.Query(t.Context(), "DROP TABLE IF EXISTS fk_parent")
	}

	drop()

	for _, statement := range []string{fkParentDDL, fkChildDDL} {
		if _, err := c.Query(t.Context(), statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}

	t.Cleanup(drop)

	return c
}

/*
A composite key is one relationship with its columns paired in order.

Two rows, not four, and tenant_id opposite tenant_id rather than opposite code.
*/
func TestACompositeForeignKeyIsPairedInOrder(t *testing.T) {
	for name, target := range fkTargets(t) {
		t.Run(name, func(t *testing.T) {
			connector := target.connector(t)

			keys, err := connector.ForeignKeys(t.Context())
			if err != nil {
				t.Fatalf("ForeignKeys: %v", err)
			}

			var ours []connectors.ForeignKey

			for _, key := range keys {
				if key.FromTable == "fk_child" {
					ours = append(ours, key)
				}
			}

			if len(ours) != 2 {
				t.Fatalf("a two-column key came back as %d rows, want 2 -- "+
					"the column lists were crossed rather than paired:\n%+v", len(ours), ours)
			}

			// In key order, and each local column against its own target.
			want := []struct{ from, to string }{
				{"tenant_id", "tenant_id"},
				{"code", "code"},
			}

			for i, expect := range want {
				got := ours[i]

				if got.Ordinal != i+1 {
					t.Errorf("row %d has ordinal %d, want %d", i, got.Ordinal, i+1)
				}

				if got.FromColumn != expect.from || got.ToColumn != expect.to {
					t.Errorf("row %d pairs %s -> %s, want %s -> %s",
						i, got.FromColumn, got.ToColumn, expect.from, expect.to)
				}

				if got.ToTable != "fk_parent" {
					t.Errorf("row %d points at %q, want fk_parent", i, got.ToTable)
				}
			}

			// One constraint, not two.
			relationships := connectors.GroupForeignKeys(ours)
			if len(relationships) != 1 {
				t.Fatalf("%d relationships, want 1: %+v", len(relationships), relationships)
			}

			rel := relationships[0]
			if len(rel.From) != 2 || len(rel.To) != 2 {
				t.Errorf("grouped into %d and %d columns, want 2 and 2", len(rel.From), len(rel.To))
			}
		})
	}
}

// The schema and the constraint name come back populated, because a
// relationship nothing can name is one nothing can report a change to.
func TestAForeignKeyCarriesItsNameAndSchema(t *testing.T) {
	for name, target := range fkTargets(t) {
		t.Run(name, func(t *testing.T) {
			keys, err := target.connector(t).ForeignKeys(t.Context())
			if err != nil {
				t.Fatalf("ForeignKeys: %v", err)
			}

			for _, key := range keys {
				if key.FromTable != "fk_child" {
					continue
				}

				if key.Name == "" {
					t.Error("a foreign key came back with no constraint name")
				}

				if key.FromSchema == "" || key.ToSchema == "" {
					t.Errorf("a foreign key came back with an empty schema: %+v", key)
				}

				return
			}

			t.Error("the fixture's foreign key was not reported at all")
		})
	}
}

// Grouping keys on the name alone would merge two tables' constraints into one
// wrong relationship, which MySQL makes possible by allowing the same name on
// two tables.
func TestGroupingKeepsSameNamedKeysOnDifferentTablesApart(t *testing.T) {
	t.Parallel()

	keys := []connectors.ForeignKey{
		{Name: "fk", FromSchema: "s", FromTable: "a", FromColumn: "x",
			ToSchema: "s", ToTable: "p", ToColumn: "x", Ordinal: 1},
		{Name: "fk", FromSchema: "s", FromTable: "b", FromColumn: "y",
			ToSchema: "s", ToTable: "q", ToColumn: "y", Ordinal: 1},
	}

	grouped := connectors.GroupForeignKeys(keys)

	if len(grouped) != 2 {
		t.Fatalf("%d relationships, want 2 -- two tables' keys were merged", len(grouped))
	}

	for _, rel := range grouped {
		if len(rel.From) != 1 {
			t.Errorf("%s.%s has %d columns, want 1", rel.FromTable, rel.Name, len(rel.From))
		}
	}
}

// Grouping preserves the order the rows arrived in, because the query orders
// by ordinal and a map would not.
func TestGroupingPreservesColumnOrder(t *testing.T) {
	t.Parallel()

	keys := []connectors.ForeignKey{
		{Name: "fk", FromTable: "a", FromColumn: "first", ToTable: "p", ToColumn: "one", Ordinal: 1},
		{Name: "fk", FromTable: "a", FromColumn: "second", ToTable: "p", ToColumn: "two", Ordinal: 2},
		{Name: "fk", FromTable: "a", FromColumn: "third", ToTable: "p", ToColumn: "three", Ordinal: 3},
	}

	grouped := connectors.GroupForeignKeys(keys)
	if len(grouped) != 1 {
		t.Fatalf("%d relationships, want 1", len(grouped))
	}

	wantFrom := []string{"first", "second", "third"}
	wantTo := []string{"one", "two", "three"}

	for i := range wantFrom {
		if grouped[0].From[i] != wantFrom[i] || grouped[0].To[i] != wantTo[i] {
			t.Errorf("column %d is %s -> %s, want %s -> %s",
				i, grouped[0].From[i], grouped[0].To[i], wantFrom[i], wantTo[i])
		}
	}
}
