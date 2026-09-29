package connectors_test

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
)

/*
The PostgreSQL connector, against a real PostgreSQL.

Error classification cannot be tested against a fake. The whole value of it is
that a particular failure at a particular server produces a particular sentence
somebody can act on, and the only way to know that is to cause the failure.

Opt-in on the same variable the store's tests use, so `make test-all` includes
it and a bare `go test ./...` on a laptop with no database still passes:

	make dev-db
	PIVOT_TEST_POSTGRES_URL='postgres://pivot:pivot@localhost:5433/pivot?sslmode=disable' \
	  go test ./internal/connectors/
*/

const postgresURLEnv = "PIVOT_TEST_POSTGRES_URL"

// target is the running PostgreSQL, parsed out of the test URL.
type target struct {
	host     string
	port     int
	database string
	username string
	password string
}

func liveTarget(t *testing.T) target {
	t.Helper()

	raw := os.Getenv(postgresURLEnv)
	if raw == "" {
		t.Skipf("%s not set; run `make test-all` to include PostgreSQL", postgresURLEnv)
	}

	// Parsed by hand rather than with net/url, so that a malformed value in
	// the environment fails this test with something readable rather than
	// producing a config that dials somewhere unexpected.
	rest := strings.TrimPrefix(raw, "postgres://")
	credentials, hostAndPath, found := strings.Cut(rest, "@")

	if !found {
		t.Fatalf("%s is not a postgres URL: %q", postgresURLEnv, raw)
	}

	user, password, _ := strings.Cut(credentials, ":")
	hostPort, path, _ := strings.Cut(hostAndPath, "/")
	host, portText, _ := strings.Cut(hostPort, ":")
	database, _, _ := strings.Cut(path, "?")

	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("%s has no usable port: %q", postgresURLEnv, raw)
	}

	return target{host: host, port: port, database: database, username: user, password: password}
}

func (tg target) config() connectors.Config {
	return connectors.Config{
		Kind: connectors.KindPostgres, Host: tg.host, Port: tg.port,
		Database: tg.database, Username: tg.username, Password: tg.password,
		SSLMode: "disable",
	}
}

func open(t *testing.T, cfg connectors.Config) connectors.Connector {
	t.Helper()

	c, err := connectors.Open(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = c.Close() })

	return c
}

// The deliverable: reach a PostgreSQL somebody else owns, and come back.
func TestPostgresConnects(t *testing.T) {
	t.Parallel()

	c := open(t, liveTarget(t).config())

	if err := c.Test(t.Context()); err != nil {
		t.Fatalf("test: %v", err)
	}
}

func TestPostgresRunsAQuery(t *testing.T) {
	t.Parallel()

	c := open(t, liveTarget(t).config())

	result, err := c.Query(t.Context(),
		"SELECT 1 AS n, 'hello' AS greeting, NULL::text AS nothing")
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(result.Rows))
	}

	if got := len(result.Columns); got != 3 {
		t.Fatalf("got %d columns, want 3", got)
	}

	if result.Columns[0].Name != "n" || result.Columns[1].Name != "greeting" {
		t.Errorf("column names = %+v", result.Columns)
	}

	// The source's own type name, kept verbatim. Part 19 normalizes it, and
	// the normalization will be wrong about something -- this is how anybody
	// finds out what it was.
	if result.Columns[0].SourceType == "" {
		t.Error("the source type was not recorded")
	}

	// A NULL is a nil, not an empty string: the difference is the whole of
	// "no value" versus "the empty value", and collapsing it here would make
	// every later layer unable to tell them apart.
	if result.Rows[0][2] != nil {
		t.Errorf("NULL came back as %#v", result.Rows[0][2])
	}
}

// A row limit truncates *with a signal*. A result silently cut is a wrong
// answer presented as a right one, and a chart drawn from it is wrong in a way
// nobody can see.
func TestARowLimitTruncatesWithASignal(t *testing.T) {
	t.Parallel()

	cfg := liveTarget(t).config()
	cfg.MaxRows = 10

	c := open(t, cfg)

	result, err := c.Query(t.Context(), "SELECT generate_series(1, 1000) AS n")
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if int64(len(result.Rows)) != cfg.MaxRows {
		t.Errorf("got %d rows, want the limit of %d", len(result.Rows), cfg.MaxRows)
	}

	if !result.Truncated {
		t.Error("the result was cut and does not say so")
	}

	// And a result that fits is not flagged, or the signal means nothing.
	small, err := c.Query(t.Context(), "SELECT generate_series(1, 3) AS n")
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if small.Truncated {
		t.Error("a result inside the limit was reported as truncated")
	}
}

/*
Cancellation reaches the server.

Not merely "the client stopped waiting": a query abandoned by its client goes
on burning the source's CPU until it finishes, which is how a canceled
dashboard refresh takes a warehouse down. pg_sleep is the cheapest way to have
something worth canceling.
*/
func TestCancellationStopsTheQuery(t *testing.T) {
	t.Parallel()

	c := open(t, liveTarget(t).config())

	ctx, cancel := context.WithCancel(t.Context())

	var (
		wg       sync.WaitGroup
		queryErr error
	)

	wg.Add(1)

	go func() {
		defer wg.Done()

		_, queryErr = c.Query(ctx, "SELECT pg_sleep(30)")
	}()

	// Long enough for the query to have reached the server, short enough that
	// the test is not the slow one in the package.
	time.Sleep(300 * time.Millisecond)
	cancel()

	wg.Wait()

	if queryErr == nil {
		t.Fatal("a canceled 30-second query returned successfully")
	}

	// Reported as canceled rather than as an unknown failure, because the UI
	// shows the two differently: one is "you stopped it" and the other is
	// "something is wrong".
	if !errors.Is(queryErr, &connectors.Error{Reason: connectors.ReasonCanceled}) {
		t.Errorf("error = %v, want ReasonCanceled", queryErr)
	}
}

// A query that outlives its timeout is stopped and says why, with a hint that
// names the setting to change.
func TestAQueryTimeoutIsReportedAsOne(t *testing.T) {
	t.Parallel()

	cfg := liveTarget(t).config()
	cfg.QueryTimeoutSeconds = 1

	c := open(t, cfg)

	_, err := c.Query(t.Context(), "SELECT pg_sleep(10)")

	if err == nil {
		t.Fatal("a query that ran past its timeout returned successfully")
	}

	if !errors.Is(err, &connectors.Error{Reason: connectors.ReasonTimeout}) {
		t.Errorf("error = %v, want ReasonTimeout", err)
	}

	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("error = %q, want it to name the setting to change", err)
	}
}

/*
The four failures somebody actually hits when configuring a connection.

Each has to arrive as a sentence about what is wrong rather than the driver's
own text. "dial tcp: lookup db.internal: no such host" tells whoever wrote the
driver exactly what happened and tells everybody else nothing.
*/
func TestTheFailuresSomebodyActuallyHitsAreExplained(t *testing.T) {
	t.Parallel()

	live := liveTarget(t)

	cases := map[string]struct {
		mutate func(*connectors.Config)
		reason connectors.Reason
		says   string
	}{
		"the password is wrong": {
			mutate: func(c *connectors.Config) { c.Password = "definitely-not-the-password" },
			reason: connectors.ReasonAuth,
			says:   "credentials",
		},
		"the host does not resolve": {
			mutate: func(c *connectors.Config) { c.Host = "no-such-host.invalid" },
			reason: connectors.ReasonUnreachable,
			says:   "resolve",
		},
		"nothing is listening": {
			mutate: func(c *connectors.Config) { c.Port = 5999 },
			reason: connectors.ReasonUnreachable,
			says:   "refused",
		},
		"no such database": {
			mutate: func(c *connectors.Config) { c.Database = "there-is-no-such-database" },
			reason: connectors.ReasonNoDatabase,
			says:   "does not exist",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := live.config()
			tc.mutate(&cfg)

			err := open(t, cfg).Test(t.Context())

			if err == nil {
				t.Fatalf("%s was accepted", name)
			}

			if !errors.Is(err, &connectors.Error{Reason: tc.reason}) {
				t.Errorf("error = %v, want %s", err, tc.reason)
			}

			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("error = %q, want it to mention %q", err, tc.says)
			}

			// And never the password, however it failed. This text goes into
			// logs, error pages and support tickets.
			if strings.Contains(err.Error(), live.password) && live.password != "" {
				t.Errorf("the password is in the error: %q", err)
			}
		})
	}
}

// Introspection returns what is in the source, grouped by table and in
// declaration order -- which is what lets the catalog in Part 19 store it
// without buffering a whole schema.
func TestIntrospectionListsTablesAndColumns(t *testing.T) {
	t.Parallel()

	c := open(t, liveTarget(t).config())

	tables, err := c.Introspect(t.Context())
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}

	if len(tables) == 0 {
		t.Fatal("no tables; the test database should hold Pivot's own schema")
	}

	for _, table := range tables {
		if table.Schema == "pg_catalog" || table.Schema == "information_schema" {
			t.Errorf("system schema %q is in the catalog", table.Schema)
		}

		if len(table.Columns) == 0 {
			t.Errorf("%s.%s has no columns", table.Schema, table.Name)
		}

		for i, column := range table.Columns {
			if column.Position != i+1 {
				t.Errorf("%s.%s columns are out of declaration order at %d",
					table.Schema, table.Name, i)

				break
			}

			if column.SourceType == "" {
				t.Errorf("%s.%s.%s has no source type", table.Schema, table.Name, column.Name)
			}
		}
	}
}

// The pool is capped per connection, because the limit belongs to the database
// at the other end: a warehouse with twenty slots and a laptop Postgres do not
// want the same number.
func TestThePoolIsCappedPerConnection(t *testing.T) {
	t.Parallel()

	cfg := liveTarget(t).config()
	cfg.MaxOpenConns = 2

	c := open(t, cfg)

	sqlConn, ok := c.(*connectors.SQLConnector)
	if !ok {
		t.Fatalf("expected a SQLConnector, got %T", c)
	}

	// Twenty concurrent queries through a pool of two. What is being checked
	// is that they all succeed -- a cap that deadlocked or errored under
	// contention would be worse than no cap.
	var wg sync.WaitGroup

	errs := make([]error, 20)

	for i := range errs {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, errs[i] = c.Query(t.Context(), "SELECT 1")
		}()
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("query %d through a pool of 2 failed: %v", i, err)
		}
	}

	if got := sqlConn.DB().Stats().MaxOpenConnections; got != 2 {
		t.Errorf("MaxOpenConnections = %d, want 2", got)
	}
}

// Closing releases the pool. A connector left open when its connection is
// deleted is a pool held against somebody's warehouse until Pivot restarts.
func TestClosingReleasesThePool(t *testing.T) {
	t.Parallel()

	c, err := connectors.Open(liveTarget(t).config())
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if terr := c.Test(t.Context()); terr != nil {
		t.Fatalf("test: %v", terr)
	}

	if cerr := c.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}

	if _, qerr := c.Query(t.Context(), "SELECT 1"); qerr == nil {
		t.Error("a closed connector still runs queries")
	}
}

/*
PostgreSQL says where the problem is, and Pivot keeps it.

The editor underlines the offending word using this, and the underline is worth
more than the sentence above it. Three codes, because the first version of this
attached the position only to the two that looked like parse errors and missed
42703 -- an undefined column, which is the most common typo there is and
exactly the case an underline helps with. It was found by running a query, not
by a test, which is why there is now a test.
*/
func TestPostgresSaysWhereTheProblemIs(t *testing.T) {
	t.Parallel()

	c := open(t, liveTarget(t).config())

	cases := map[string]struct {
		sql  string
		word string
	}{
		"an undefined column": {"SELECT nope FROM information_schema.tables", "nope"},
		// PostgreSQL's parser reads past the empty target list and objects at
		// WHERE. The expectation here is what the server actually says, not
		// what a reader might predict -- the claim under test is that Pivot
		// carries the position faithfully, not that it can guess a parser.
		"a syntax error":     {"SELECT FROM WHERE", "WHERE"},
		"an undefined table": {"SELECT * FROM no_such_table_here", "no_such_table_here"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := c.Query(t.Context(), tc.sql)
			if err == nil {
				t.Fatalf("%q succeeded", tc.sql)
			}

			var connErr *connectors.Error
			if !errors.As(err, &connErr) {
				t.Fatalf("not a connectors.Error: %v", err)
			}

			if connErr.Position <= 0 {
				t.Fatalf("no position reported for %q: %v", tc.sql, connErr)
			}

			// The offset is 1-based and in bytes, and it must land on the word
			// PostgreSQL objected to -- a position that is merely non-zero
			// would pass a weaker test and still underline the wrong thing.
			if at := connErr.Position - 1; at >= len(tc.sql) ||
				!strings.HasPrefix(tc.sql[at:], tc.word) {
				t.Errorf("position %d points at %q, want the start of %q",
					connErr.Position, tc.sql[min(at, len(tc.sql)):], tc.word)
			}
		})
	}
}

/*
A failure with nothing to point at reports no position, rather than guessing.

Asserted against a real failure rather than a skip. A test that cannot run is
worse than no test -- Part 18-a shipped one of those for a whole phase and it
was green the entire time.
*/
func TestAFailureWithNothingToPointAtHasNoPosition(t *testing.T) {
	t.Parallel()

	c := open(t, liveTarget(t).config())

	// Division by zero is a runtime error, not a parse error: PostgreSQL
	// reports it without a position because there is no offending token.
	_, err := c.Query(t.Context(), "SELECT 1 / 0")
	if err == nil {
		t.Fatal("dividing by zero succeeded")
	}

	var connErr *connectors.Error
	if !errors.As(err, &connErr) {
		t.Fatalf("not a connectors.Error: %v", err)
	}

	if connErr.Position != 0 {
		t.Errorf("a runtime failure reported position %d", connErr.Position)
	}
}
