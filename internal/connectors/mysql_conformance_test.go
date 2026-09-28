package connectors_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/connectors/conformance"
)

/*
MySQL against the conformance suite.

The claim Part 17 made was that adding a connector means writing one file. This
is that file, and it is the first time the claim has been tested by something
other than the connector it was written against.

Opt-in on PIVOT_TEST_MYSQL_URL, the same way the PostgreSQL tests are:

	make dev-db
	PIVOT_TEST_MYSQL_URL='mysql://pivot:pivot@localhost:3307/pivot' \
	  go test ./internal/connectors/
*/

const mysqlURLEnv = "PIVOT_TEST_MYSQL_URL"

func mysqlTarget(t *testing.T) target {
	t.Helper()

	raw := os.Getenv(mysqlURLEnv)
	if raw == "" {
		t.Skipf("%s not set; run `make test-all` to include MySQL", mysqlURLEnv)
	}

	// Parsed by hand, like the PostgreSQL one, so a malformed value in the
	// environment fails readably rather than producing a config that dials
	// somewhere unexpected.
	rest := strings.TrimPrefix(raw, "mysql://")

	credentials, hostAndPath, found := strings.Cut(rest, "@")
	if !found {
		t.Fatalf("%s is not a mysql URL: %q", mysqlURLEnv, raw)
	}

	user, password, _ := strings.Cut(credentials, ":")
	hostPort, path, _ := strings.Cut(hostAndPath, "/")
	host, portText, _ := strings.Cut(hostPort, ":")
	database, _, _ := strings.Cut(path, "?")

	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("%s has no usable port: %q", mysqlURLEnv, raw)
	}

	return target{
		host: host, port: port, database: database,
		username: user, password: password,
	}
}

// mysqlConfig is the connection every MySQL test opens.
func mysqlConfig(t *testing.T) connectors.Config {
	tg := mysqlTarget(t)

	return connectors.Config{
		Kind: connectors.KindMySQL,
		Host: tg.host, Port: tg.port, Database: tg.database,
		Username: tg.username, Password: tg.password,
		SSLMode: "disable",

		// Small, so the truncation check moves a few hundred rows and the
		// timeout check waits three seconds rather than thirty.
		MaxRows:             200,
		QueryTimeoutSeconds: 3,

		Options: map[string]string{
			// MySQL has no generate_series, so the suite's rows come from a
			// recursive CTE -- and MySQL stops one at 1000 iterations by
			// default, which is fewer than the truncation check asks for.
			//
			// Set here rather than on the server because a GitHub Actions
			// service container cannot be given a command line, and a dev
			// database configured differently from CI's produces failures
			// that only reproduce where you cannot debug them.
			"cte_max_recursion_depth": "100000",
		},
	}
}

const mysqlFixtureTable = "pivot_conformance_mysql"

func TestMySQLConformance(t *testing.T) {
	cfg := mysqlConfig(t)
	connector := open(t, cfg)

	conformance.Run(t, conformance.Subject{
		Name:         "mysql",
		Connector:    connector,
		MaxRows:      cfg.MaxRows,
		QueryTimeout: time.Duration(cfg.QueryTimeoutSeconds) * time.Second,

		Fixture: conformance.Fixture{
			Table: mysqlFixtureTable,
			// In MySQL a schema is a database, so the fixture is cataloged
			// under the one in the DSN.
			Schema: cfg.Database,
			Name:   mysqlFixtureTable,

			Create: []string{`CREATE TABLE ` + mysqlFixtureTable + ` (
				id            BIGINT PRIMARY KEY,
				name          TEXT NOT NULL,
				notes         TEXT NULL,
				flag          BOOLEAN NOT NULL,
				ratio         DOUBLE NOT NULL,
				created_utc   TIMESTAMP NOT NULL,
				created_naive DATETIME NOT NULL
			) CHARACTER SET utf8mb4`},

			Insert: mysqlInserts(),

			Drop: []string{`DROP TABLE IF EXISTS ` + mysqlFixtureTable},
		},

		SQL: conformance.Expressions{
			Sleep: func(seconds int) string {
				return fmt.Sprintf("SELECT SLEEP(%d)", seconds)
			},

			// No generate_series in MySQL. A recursive CTE is the portable
			// answer, and it doubles as proof that the CTE capability this
			// dialect declares is real.
			Series: func(n int) string {
				return fmt.Sprintf(
					"WITH RECURSIVE s (i) AS ("+
						"SELECT 1 UNION ALL SELECT i + 1 FROM s WHERE i < %d"+
						") SELECT i FROM s", n)
			},

			LateralJoin: fmt.Sprintf(
				"SELECT f.id FROM %s f, LATERAL (SELECT f.id AS echoed) s ORDER BY f.id",
				mysqlFixtureTable),

			SyntaxError:  "SELEKT 1",
			MissingTable: "SELECT * FROM pivot_no_such_table_9f2c",

			// A space and the quote character itself -- which for MySQL is the
			// backtick, not the double quote. The suite never needs to know
			// that: it asks the dialect to quote this and checks what comes
			// back.
			AwkwardIdentifier: "a `quoted` name",
		},
	})
}

/*
The fixture's INSERTs, rendered from the suite's own row contract.

Generated rather than typed out, for the same reason PostgreSQL's are: a
subject whose literals drift from [conformance.Rows] fails with a value
mismatch that reads like a driver bug and is not one.

The timestamps are written as plain wall-clock strings with no offset, which is
correct *because* the connector pins the session to UTC — MySQL interprets an
unqualified literal in the session's zone. That is the coupling this connector
exists to guarantee, and the server this runs against is deliberately set to
Asia/Kathmandu (+05:45) so that a connector which did not pin it would store
every instant 5h45m out.
*/
func mysqlInserts() []string {
	rows := conformance.Rows()
	out := make([]string, 0, len(rows))

	for _, row := range rows {
		notes := "NULL"
		if row.Notes != nil {
			notes = mysqlLiteral(*row.Notes)
		}

		out = append(out, fmt.Sprintf(
			"INSERT INTO %s (id, name, notes, flag, ratio, created_utc, created_naive) "+
				"VALUES (%d, %s, %s, %t, %v, %s, %s)",
			mysqlFixtureTable,
			row.ID,
			mysqlLiteral(row.Name),
			notes,
			row.Flag,
			row.Ratio,
			mysqlLiteral(row.CreatedUTC.Format("2006-01-02 15:04:05")),
			mysqlLiteral(row.CreatedNaive.Format("2006-01-02 15:04:05")),
		))
	}

	return out
}

// mysqlLiteral quotes a string for MySQL. Enough for a fixture, and
// deliberately not exported: production SQL binds parameters.
//
// The backslash is escaped as well as the quote, because MySQL treats
// backslash as an escape character inside a string literal and PostgreSQL does
// not — one of the small differences that makes a shared literal-writer a bad
// idea.
func mysqlLiteral(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)

	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
