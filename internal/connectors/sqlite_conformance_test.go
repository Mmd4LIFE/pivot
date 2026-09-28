package connectors_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/connectors/conformance"
)

/*
SQLite against the conformance suite.

The third connector, and the one that does not fit the shape: no host, no port,
no credentials, no TLS. A path is the whole address.

It needs no environment variable and no container, so unlike the PostgreSQL and
MySQL suites this one always runs -- including on a laptop with no Docker and
in `go test ./...` with nothing set up. That is worth having: it means the
conformance suite itself is exercised against a real database on every run
rather than only when somebody remembers to start one.
*/

const sqliteFixtureTable = "pivot_conformance"

func TestSQLiteConformance(t *testing.T) {
	path := seedSQLite(t)

	cfg := connectors.Config{
		Kind:     connectors.KindSQLite,
		Database: path,

		// Small, so the truncation check moves a few hundred rows and the
		// timeout check waits three seconds rather than thirty.
		MaxRows:             200,
		QueryTimeoutSeconds: 3,
	}

	conformance.Run(t, conformance.Subject{
		Name:         "sqlite",
		Connector:    open(t, cfg),
		MaxRows:      cfg.MaxRows,
		QueryTimeout: time.Duration(cfg.QueryTimeoutSeconds) * time.Second,

		Fixture: conformance.Fixture{
			Table: sqliteFixtureTable,
			// SQLite's attached database is called "main", which the catalog
			// query reports so that every connector has the same shape.
			Schema: "main",
			Name:   sqliteFixtureTable,

			// No Create, Insert or Drop. The connector opens the file
			// read-only, so the suite cannot build its own fixture -- see
			// seedSQLite, which does it through a separate read-write handle
			// before the connector exists.
		},

		SQL: conformance.Expressions{
			Sleep:             sqliteSleep,
			Series:            sqliteSeries,
			SyntaxError:       "SELEKT 1",
			MissingTable:      "SELECT * FROM pivot_no_such_table_9f2c",
			AwkwardIdentifier: `a "quoted" name`,

			// No LateralJoin, because SQLite has no LATERAL and the dialect
			// declares the capability false. The suite refuses a capability
			// claimed without proof; declining to claim it is the other half
			// of that bargain.
		},
	})
}

/*
sqliteSleep returns a query that keeps SQLite busy for roughly this long.

SQLite has no sleep function, and there is nothing to wait for anyway: an
embedded database has no server that could be asleep. So time is spent rather
than waited, by counting through a recursive CTE.

Deliberately generous -- twenty million rows per second against a measured
three million -- because every caller of this either cancels the query or times
it out, so running too long is correct and running too short is a test that
passes for the wrong reason.
*/
func sqliteSleep(seconds int) string {
	if seconds <= 0 {
		// The cancellation check runs Sleep(0) afterwards to prove the pool
		// still works, and that one has to come back immediately.
		return "SELECT 0"
	}

	return fmt.Sprintf(
		"WITH RECURSIVE burn(x) AS ("+
			"SELECT 1 UNION ALL SELECT x + 1 FROM burn WHERE x < %d"+
			") SELECT count(*) FROM burn", seconds*20_000_000)
}

func sqliteSeries(n int) string {
	return fmt.Sprintf(
		"WITH RECURSIVE s(i) AS ("+
			"SELECT 1 UNION ALL SELECT i + 1 FROM s WHERE i < %d"+
			") SELECT i FROM s", n)
}

/*
seedSQLite writes the suite's fixture to a temporary file and returns its path.

Through a read-write handle of its own, opened and closed before the connector
exists. The connector cannot do this and should not be able to: it opens every
file read-only, and this is what that costs.

The file lives in the test's temporary directory, so removing it is the
testing package's problem rather than a cleanup this has to get right.
*/
func seedSQLite(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "conformance.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("create the fixture file: %v", err)
	}

	defer func() { _ = db.Close() }()

	// id is NOT NULL explicitly. INTEGER PRIMARY KEY on its own is an alias
	// for the rowid, which SQLite reports as nullable -- true, surprising, and
	// the sort of thing the catalog check exists to surface.
	create := `CREATE TABLE ` + sqliteFixtureTable + ` (
		id            INTEGER NOT NULL PRIMARY KEY,
		name          TEXT NOT NULL,
		notes         TEXT,
		flag          BOOLEAN NOT NULL,
		ratio         REAL NOT NULL,
		created_utc   TIMESTAMP NOT NULL,
		created_naive DATETIME NOT NULL
	)`

	if _, err = db.ExecContext(t.Context(), create); err != nil {
		t.Fatalf("create the fixture table: %v", err)
	}

	for _, statement := range sqliteInserts() {
		if _, err = db.ExecContext(t.Context(), statement); err != nil {
			t.Fatalf("seed the fixture: %v", err)
		}
	}

	return path
}

/*
The fixture's INSERTs, rendered from the suite's own row contract.

SQLite has no date type at all: a timestamp is text, and what it means is
whatever the reader decides. So created_utc is written with an explicit +00:00
offset and created_naive without one, which is the only thing that makes the
two columns different -- and the only reason the driver can hand back an
instant for one and a wall-clock reading for the other.
*/
func sqliteInserts() []string {
	rows := conformance.Rows()
	out := make([]string, 0, len(rows))

	for _, row := range rows {
		notes := "NULL"
		if row.Notes != nil {
			notes = sqliteLiteral(*row.Notes)
		}

		out = append(out, fmt.Sprintf(
			"INSERT INTO %s (id, name, notes, flag, ratio, created_utc, created_naive) "+
				"VALUES (%d, %s, %s, %d, %v, %s, %s)",
			sqliteFixtureTable,
			row.ID,
			sqliteLiteral(row.Name),
			notes,
			// SQLite has no boolean either. 1 and 0, which is what BOOLEAN
			// means here and what the driver gives back.
			boolAsInt(row.Flag),
			row.Ratio,
			sqliteLiteral(row.CreatedUTC.Format("2006-01-02 15:04:05-07:00")),
			sqliteLiteral(row.CreatedNaive.Format("2006-01-02 15:04:05")),
		))
	}

	return out
}

func boolAsInt(b bool) int {
	if b {
		return 1
	}

	return 0
}

// sqliteLiteral quotes a string for SQLite. Enough for a fixture, and
// deliberately not exported: production SQL binds parameters.
//
// Only the quote is doubled. Unlike MySQL, SQLite gives backslash no special
// meaning inside a string literal.
func sqliteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
