//go:build duckdb

package connectors_test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/connectors/conformance"
)

/*
DuckDB against the conformance suite.

Behind the same build tag as the connector, so `go test ./...` on a default
checkout neither builds nor runs it. `make test-duckdb` does both.

The fourth connector and the fourth confirmation that the suite is a library
rather than a runner: this file is the whole integration, and nothing in
internal/connectors/conformance knows it exists.
*/

const duckdbFixtureTable = "pivot_conformance"

func TestDuckDBConformance(t *testing.T) {
	path := seedDuckDB(t)

	cfg := connectors.Config{
		Kind:                connectors.KindDuckDB,
		Database:            path,
		MaxRows:             200,
		QueryTimeoutSeconds: 3,
	}

	conformance.Run(t, conformance.Subject{
		Name:         "duckdb",
		Connector:    open(t, cfg),
		MaxRows:      cfg.MaxRows,
		QueryTimeout: time.Duration(cfg.QueryTimeoutSeconds) * time.Second,

		Fixture: conformance.Fixture{
			Table:  duckdbFixtureTable,
			Schema: "main",
			Name:   duckdbFixtureTable,
			// No DDL: the connector opens the file read-only, so seedDuckDB
			// builds the fixture through a separate handle first.
		},

		SQL: conformance.Expressions{
			// DuckDB has no sleep function, so time is spent rather than
			// waited -- the same recursive CTE trick SQLite needs, and
			// generous for the same reason: every caller cancels or times it
			// out, so running too long is correct.
			Sleep: func(seconds int) string {
				if seconds <= 0 {
					return "SELECT 0"
				}

				return fmt.Sprintf(
					"WITH RECURSIVE burn(x) AS ("+
						"SELECT 1 UNION ALL SELECT x + 1 FROM burn WHERE x < %d"+
						") SELECT count(*) FROM burn", seconds*20_000_000)
			},

			Series: func(n int) string {
				return fmt.Sprintf("SELECT i FROM generate_series(1, %d) t(i)", n)
			},

			LateralJoin: fmt.Sprintf(
				"SELECT f.id FROM %s f, LATERAL (SELECT f.id AS echoed) s ORDER BY f.id",
				duckdbFixtureTable),

			SyntaxError:       "SELEKT 1",
			MissingTable:      "SELECT * FROM pivot_no_such_table_9f2c",
			AwkwardIdentifier: `a "quoted" name`,
		},
	})
}

// seedDuckDB writes the suite's fixture to a temporary file, through a
// read-write handle the connector will not give us.
func seedDuckDB(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "conformance.duckdb")

	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatalf("create the fixture file: %v", err)
	}

	defer func() { _ = db.Close() }()

	// UTC while seeding, so an unqualified literal means what it says. The
	// connector's own session setting is what the suite then exercises.
	if _, err = db.ExecContext(t.Context(), "SET TimeZone='UTC'"); err != nil {
		t.Fatalf("pin the seeding session to UTC: %v", err)
	}

	create := `CREATE TABLE ` + duckdbFixtureTable + ` (
		id            BIGINT NOT NULL PRIMARY KEY,
		name          VARCHAR NOT NULL,
		notes         VARCHAR,
		flag          BOOLEAN NOT NULL,
		ratio         DOUBLE NOT NULL,
		created_utc   TIMESTAMP WITH TIME ZONE NOT NULL,
		created_naive TIMESTAMP NOT NULL
	)`

	if _, err = db.ExecContext(t.Context(), create); err != nil {
		t.Fatalf("create the fixture table: %v", err)
	}

	for _, statement := range duckdbInserts() {
		if _, err = db.ExecContext(t.Context(), statement); err != nil {
			t.Fatalf("seed the fixture: %v", err)
		}
	}

	return path
}

func duckdbInserts() []string {
	rows := conformance.Rows()
	out := make([]string, 0, len(rows))

	for _, row := range rows {
		notes := "NULL"
		if row.Notes != nil {
			notes = duckdbLiteral(*row.Notes)
		}

		out = append(out, fmt.Sprintf(
			"INSERT INTO %s (id, name, notes, flag, ratio, created_utc, created_naive) "+
				"VALUES (%d, %s, %s, %t, %v, %s, %s)",
			duckdbFixtureTable,
			row.ID,
			duckdbLiteral(row.Name),
			notes,
			row.Flag,
			row.Ratio,
			// An explicit offset on the zoned column, so the stored instant
			// does not depend on the session that wrote it.
			duckdbLiteral(row.CreatedUTC.Format("2006-01-02 15:04:05-07:00")),
			duckdbLiteral(row.CreatedNaive.Format("2006-01-02 15:04:05")),
		))
	}

	return out
}

func duckdbLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

/*
DuckDB reads a Parquet or CSV file directly.

The reason DuckDB is worth its build cost at all, and the thing no other
connector here can do: a file on disk becomes a table without anybody loading
it anywhere first.
*/
func TestDuckDBReadsFilesDirectly(t *testing.T) {
	dir := t.TempDir()

	csvPath := filepath.Join(dir, "orders.csv")
	csv := "id,region,amount\n1,north,10.5\n2,south,20.25\n3,north,5.0\n"

	if err := os.WriteFile(csvPath, []byte(csv), 0o600); err != nil {
		t.Fatalf("write the csv: %v", err)
	}

	// In-memory, because there is no database here -- only files.
	connector := open(t, connectors.Config{
		Kind: connectors.KindDuckDB, Database: connectors.InMemory,
	})

	t.Run("csv", func(t *testing.T) {
		result, err := connector.Query(t.Context(), fmt.Sprintf(
			"SELECT region, sum(amount) AS total FROM read_csv_auto('%s') "+
				"GROUP BY region ORDER BY region", csvPath))
		if err != nil {
			t.Fatalf("read the csv: %v", err)
		}

		if len(result.Rows) != 2 {
			t.Fatalf("got %d rows, want 2", len(result.Rows))
		}

		// north is 10.5 + 5.0; the aggregate proves it parsed numbers rather
		// than strings that happen to look like them.
		if got := fmt.Sprint(result.Rows[0][1]); got != "15.5" {
			t.Errorf("north total = %s, want 15.5", got)
		}
	})

	t.Run("parquet", func(t *testing.T) {
		parquetPath := filepath.Join(dir, "orders.parquet")

		// Written by DuckDB itself, which is the only Parquet writer present.
		if _, err := connector.Query(t.Context(), fmt.Sprintf(
			"COPY (SELECT * FROM read_csv_auto('%s')) TO '%s' (FORMAT PARQUET)",
			csvPath, parquetPath)); err != nil {
			t.Fatalf("write the parquet: %v", err)
		}

		result, err := connector.Query(t.Context(),
			fmt.Sprintf("SELECT count(*) FROM read_parquet('%s')", parquetPath))
		if err != nil {
			t.Fatalf("read the parquet: %v", err)
		}

		if got := fmt.Sprint(result.Rows[0][0]); got != "3" {
			t.Errorf("parquet row count = %s, want 3", got)
		}
	})
}

// A DuckDB file is opened read-only, like a SQLite one and for the same
// reason: a BI source is something Pivot reads.
func TestADuckDBSourceCannotBeWrittenTo(t *testing.T) {
	connector := open(t, connectors.Config{
		Kind: connectors.KindDuckDB, Database: seedDuckDB(t),
	})

	_, err := connector.Query(t.Context(),
		"UPDATE "+duckdbFixtureTable+" SET name = 'changed'")

	if err == nil {
		t.Fatal("an update succeeded against a read-only DuckDB connection")
	}
}

/*
The per-query memory cap is enforced rather than advisory.

NFR 1.3 is explicit: "A query exceeding its budget is killed with a clear
error. The alternative -- DuckDB consuming all host memory and taking down
dashboards for everyone -- is unacceptable."

So this asks for a limit small enough to hit and a query greedy enough to hit
it, and requires the failure rather than the answer.
*/
func TestDuckDBEnforcesItsMemoryCap(t *testing.T) {
	connector := open(t, connectors.Config{
		Kind: connectors.KindDuckDB, Database: connectors.InMemory,
		QueryTimeoutSeconds: 60,
		Options:             map[string]string{"memory_limit": "128MB"},
	})

	// A wide sort over far more rows than fit in 128MB. Sorting is the
	// operator that cannot stream its way out of trouble.
	_, err := connector.Query(t.Context(),
		"SELECT i, repeat('x', 2000) AS padding FROM generate_series(1, 2000000) t(i) "+
			"ORDER BY padding, i")

	if err == nil {
		t.Fatal("a query far over its 128MB budget returned successfully, " +
			"so the cap is advisory and NFR 1.3 is not met")
	}

	t.Logf("refused, as it must be: %v", err)
}

// And the default cap is the NFR's, not DuckDB's own -- which is a fraction of
// host memory and would let one query take a dashboard down with it.
func TestDuckDBDefaultsToTheBudgetedMemoryCap(t *testing.T) {
	connector := open(t, connectors.Config{
		Kind: connectors.KindDuckDB, Database: connectors.InMemory,
	})

	result, err := connector.Query(t.Context(),
		"SELECT value FROM duckdb_settings() WHERE name = 'memory_limit'")
	if err != nil {
		t.Fatalf("read the setting: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Fatalf("duckdb_settings() returned %d rows for memory_limit", len(result.Rows))
	}

	/*
		Compared against a connection that asks for the default explicitly,
		rather than against a string.

		DuckDB reports the setting in its own units -- 1GB comes back as
		"953.6 MiB", which is the same number of bytes and looks like a bug to
		anybody reading it in a hurry. Asserting on that text would be
		asserting on DuckDB's formatting; asserting the two agree is asserting
		the thing that matters, which is that a connection saying nothing gets
		the budgeted cap rather than DuckDB's own default of most of the host.
	*/
	explicit := open(t, connectors.Config{
		Kind: connectors.KindDuckDB, Database: connectors.InMemory,
		Options: map[string]string{"memory_limit": connectors.DefaultDuckDBMemoryLimit},
	})

	asked, err := explicit.Query(t.Context(),
		"SELECT value FROM duckdb_settings() WHERE name = 'memory_limit'")
	if err != nil {
		t.Fatalf("read the setting: %v", err)
	}

	gotDefault := fmt.Sprint(result.Rows[0][0])
	gotExplicit := fmt.Sprint(asked.Rows[0][0])

	if gotDefault != gotExplicit {
		t.Errorf("a connection that said nothing got memory_limit %q, "+
			"but one asking for %s got %q",
			gotDefault, connectors.DefaultDuckDBMemoryLimit, gotExplicit)
	}

	t.Logf("%s reads back as %q", connectors.DefaultDuckDBMemoryLimit, gotDefault)
}

// The mirror of the default build's test: with the tag, DuckDB is a connector
// like any other and is offered where connectors are listed.
func TestADuckDBBuildOffersIt(t *testing.T) {
	kinds := connectors.Kinds()

	if !slices.Contains(kinds, connectors.KindDuckDB) {
		t.Errorf("a build with -tags duckdb does not list it: %v", kinds)
	}
}
