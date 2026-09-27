package connectors_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/connectors/conformance"
)

/*
PostgreSQL against the conformance suite.

This file is the whole integration, and it is the template for every connector
after it: describe the fixture in this dialect, hand over four snippets of SQL
the suite cannot write portably, and call Run. Nothing in the suite knows this
file exists.

It is also the reference answer. When Redshift or ClickHouse fails a property,
the useful question is what this file does differently -- so it is written to be
read, not to be clever.
*/

const fixtureTable = "pivot_conformance"

// The session runs on a clock that is not UTC, deliberately.
//
// A connector that confuses a zoned timestamp for a naive one, or applies the
// session timezone to a value that already has one, produces the right answer
// under a UTC session and the wrong day under this one. Asia/Tehran is +03:30:
// a half-hour offset also catches anything that assumes whole hours.
const fixtureTimeZone = "Asia/Tehran"

func TestPostgresConformance(t *testing.T) {
	cfg := liveTarget(t).config()

	// A small row cap and a short timeout, so the truncation check moves a few
	// hundred rows instead of a hundred thousand and the timeout check waits
	// three seconds instead of thirty.
	cfg.MaxRows = 200
	cfg.QueryTimeoutSeconds = 3
	cfg.Options = map[string]string{"timezone": fixtureTimeZone}

	connector := open(t, cfg)

	conformance.Run(t, conformance.Subject{
		Name:         "postgres",
		Connector:    connector,
		MaxRows:      cfg.MaxRows,
		QueryTimeout: time.Duration(cfg.QueryTimeoutSeconds) * time.Second,

		Fixture: conformance.Fixture{
			Table:  fixtureTable,
			Schema: "public",
			Name:   fixtureTable,

			Create: []string{`CREATE TABLE ` + fixtureTable + ` (
				id            BIGINT PRIMARY KEY,
				name          TEXT NOT NULL,
				notes         TEXT,
				flag          BOOLEAN NOT NULL,
				ratio         DOUBLE PRECISION NOT NULL,
				created_utc   TIMESTAMP WITH TIME ZONE NOT NULL,
				created_naive TIMESTAMP WITHOUT TIME ZONE NOT NULL
			)`},

			Insert: postgresInserts(),

			// IF EXISTS because the suite drops before it creates, to clean up
			// after a run that died partway through.
			Drop: []string{`DROP TABLE IF EXISTS ` + fixtureTable},
		},

		SQL: conformance.Expressions{
			Sleep: func(seconds int) string {
				return fmt.Sprintf("SELECT pg_sleep(%d)", seconds)
			},

			Series: func(n int) string {
				return fmt.Sprintf("SELECT i FROM generate_series(1, %d) AS i", n)
			},

			LateralJoin: fmt.Sprintf(
				"SELECT f.id FROM %s f, LATERAL (SELECT f.id AS echoed) s ORDER BY f.id",
				fixtureTable),

			SyntaxError:  "SELEKT 1",
			MissingTable: "SELECT * FROM pivot_no_such_table_9f2c",

			// A space and the quote character itself, which is the pair that
			// tells a real quoting rule from a pair of double quotes glued on
			// either end of the name.
			AwkwardIdentifier: `a "quoted" name`,
		},
	})
}

/*
The fixture's INSERTs, rendered from the suite's own row contract.

Generated rather than typed out, because a subject whose literals drift from
[conformance.Rows] fails with a value mismatch that reads like a driver bug and
is not one. The round trip is still real: these become SQL text, PostgreSQL
parses and stores them, and the driver hands back whatever it hands back.
*/
func postgresInserts() []string {
	out := make([]string, 0, len(conformance.Rows()))

	for _, row := range conformance.Rows() {
		notes := "NULL"
		if row.Notes != nil {
			notes = pgLiteral(*row.Notes)
		}

		out = append(out, fmt.Sprintf(
			"INSERT INTO %s (id, name, notes, flag, ratio, created_utc, created_naive) "+
				"VALUES (%d, %s, %s, %t, %v, TIMESTAMPTZ %s, TIMESTAMP %s)",
			fixtureTable,
			row.ID,
			pgLiteral(row.Name),
			notes,
			row.Flag,
			row.Ratio,
			// Written with an explicit offset, so the stored instant does not
			// depend on the session timezone this connection happens to use.
			pgLiteral(row.CreatedUTC.Format(time.RFC3339)),
			pgLiteral(row.CreatedNaive.Format("2006-01-02 15:04:05")),
		))
	}

	return out
}

// pgLiteral quotes a string for PostgreSQL. Enough for a fixture, and
// deliberately not exported: production SQL binds parameters.
func pgLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
