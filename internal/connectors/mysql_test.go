package connectors_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
)

/*
The MySQL connector, against a real MySQL.

Error classification cannot be tested against a fake: the value of it is that a
particular failure at a particular server produces a particular sentence
somebody can act on, and the only way to know that is to cause the failure.

The conformance suite covers what every connector must do. What is here is the
part that is MySQL's alone -- chiefly that cancellation reaches the server,
which for this driver it does not do by itself.
*/

// --- cancellation, proven from somewhere else -------------------------------

/*
A canceled query stops at the server, not merely at the client.

This is the reason [connectors.Canceler] exists. Measured before it was
written: go-sql-driver returns to the caller in about 300ms and closes the
socket, and MySQL carries on running the statement -- `SELECT SLEEP(20)` was
still holding a server thread two seconds later and would have held it for the
full twenty.

The client returning promptly proves nothing about that, which is why this
watches from a *second* connection. Without the KILL QUERY this connector now
sends, the assertion at the bottom fails and everything above it still passes.
*/
func TestMySQLCancellationReachesTheServer(t *testing.T) {
	cfg := mysqlConfig(t)

	// Long enough that the query cannot end on its own inside the window this
	// test watches.
	cfg.QueryTimeoutSeconds = 60

	connector := open(t, cfg)

	// The observer is a second connector, so it is a genuinely different
	// connection to the server rather than another statement on the same one.
	observer := open(t, cfg)

	const sleeping = "SELECT SLEEP(30)"

	before := mysqlRunning(t, observer, sleeping)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	timer := time.AfterFunc(500*time.Millisecond, cancel)
	defer timer.Stop()

	started := time.Now()
	_, err := connector.Query(ctx, sleeping)
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("a canceled SELECT SLEEP(30) returned successfully")
	}

	if !errors.Is(err, &connectors.Error{Reason: connectors.ReasonCanceled}) {
		t.Errorf("error = %v, want it classified as canceled", err)
	}

	t.Logf("the client came back after %s", elapsed.Round(time.Millisecond))

	/*
		The server side, polled rather than sampled once.

		KILL QUERY is asynchronous: MySQL marks the thread and the statement
		unwinds when it next checks. For SLEEP that is prompt, but "prompt" on
		a loaded CI runner is not instant, and a single read immediately after
		the cancel would be flaky in the direction that hides the bug.
	*/
	deadline := time.Now().Add(10 * time.Second)

	for {
		running := mysqlRunning(t, observer, sleeping)
		if running <= before {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("the query is still running on the server 10s after it was canceled "+
				"(%d running, %d before the test) -- the client hung up and MySQL "+
				"never heard about it", running, before)
		}

		time.Sleep(200 * time.Millisecond)
	}
}

// mysqlRunning counts the sessions currently running a statement, seen from
// another connection.
//
// information_schema.processlist shows an account its own threads without
// needing the PROCESS privilege, which is what makes this work as the same
// unprivileged user the rest of the tests use.
func mysqlRunning(t *testing.T, observer connectors.Connector, statement string) int64 {
	t.Helper()

	result, err := observer.Query(t.Context(),
		"SELECT COUNT(*) FROM information_schema.processlist WHERE info = ?", statement)
	if err != nil {
		t.Fatalf("read the process list: %v", err)
	}

	var count int64

	switch n := result.Rows[0][0].(type) {
	case int64:
		count = n
	case uint64:
		count = int64(n)
	default:
		t.Fatalf("the process list returned a %T", n)
	}

	return count
}

// A cancellation must not poison the pool. KILL QUERY stops the statement and
// leaves the session alive precisely so the connection can be reused; if this
// connector had used KILL CONNECTION it would throw one away every time.
func TestMySQLSurvivesACancellation(t *testing.T) {
	connector := open(t, mysqlConfig(t))

	for range 3 {
		ctx, cancel := context.WithCancel(t.Context())
		timer := time.AfterFunc(200*time.Millisecond, cancel)

		if _, err := connector.Query(ctx, "SELECT SLEEP(10)"); err == nil {
			t.Fatal("a canceled query returned successfully")
		}

		timer.Stop()
		cancel()
	}

	result, err := connector.Query(t.Context(), "SELECT 1")
	if err != nil {
		t.Fatalf("the connector stopped working after three cancellations: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Errorf("got %d rows, want 1", len(result.Rows))
	}
}

// --- timestamps -------------------------------------------------------------

/*
The session is pinned to UTC, whatever the server thinks the time is.

The MySQL this runs against is deliberately set to Asia/Kathmandu, +05:45 --
not UTC and not a whole hour. MySQL converts a TIMESTAMP into the session's
zone on the way out, so a connector that inherits the server's zone returns an
instant 5h45m from the one that was stored, on every row, silently.

The conformance suite catches the consequence. This checks the cause, so that
a future change which unpins the zone fails with the reason rather than with
sixteen mysterious minutes.
*/
func TestMySQLPinsTheSessionToUTC(t *testing.T) {
	connector := open(t, mysqlConfig(t))

	result, err := connector.Query(t.Context(), "SELECT @@session.time_zone")
	if err != nil {
		t.Fatalf("read the session time zone: %v", err)
	}

	zone, _ := result.Rows[0][0].(string)
	if zone != "+00:00" {
		t.Errorf("the session time zone is %q, want +00:00", zone)
	}

	// And the server really is somewhere else, so the line above is doing work
	// rather than agreeing with a default.
	global, err := connector.Query(t.Context(), "SELECT @@global.time_zone")
	if err != nil {
		t.Fatalf("read the global time zone: %v", err)
	}

	if serverZone, _ := global.Rows[0][0].(string); serverZone == "+00:00" {
		t.Errorf("the server is already on UTC, so this test proves nothing; " +
			"the dev container sets TZ=Asia/Kathmandu for exactly this reason")
	}
}

/*
An option that would break that pinning is refused rather than ignored.

time_zone and the driver's parse location are two halves of one setting. A
configuration that moves one without the other produces values wrong by a fixed
offset, which is the kind of wrong that looks like data.
*/
func TestMySQLRefusesOptionsItSetsItself(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"time_zone", "TIME_ZONE", "loc", "parseTime"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			cfg := connectors.Config{
				Kind: connectors.KindMySQL, Host: "db.internal",
				Database: "analytics", Username: "pivot",
				Options: map[string]string{key: "whatever"},
			}

			_, err := connectors.Open(cfg)
			if err == nil {
				t.Fatalf("%s was accepted", key)
			}

			if !strings.Contains(err.Error(), key) {
				t.Errorf("error = %v, want it to name the option", err)
			}
		})
	}

	// An option the connector does not set itself is passed through, which is
	// what Options is for.
	cfg := connectors.Config{
		Kind: connectors.KindMySQL, Host: "db.internal",
		Database: "analytics", Username: "pivot",
		Options: map[string]string{"cte_max_recursion_depth": "100000"},
	}

	connector, err := connectors.Open(cfg)
	if err != nil {
		t.Fatalf("an ordinary option was refused: %v", err)
	}

	if cerr := connector.Close(); cerr != nil {
		t.Errorf("close: %v", cerr)
	}
}

// --- failures, caused rather than mocked ------------------------------------

/*
Each failure mode says what to do, and none of them contains the password.

Caused against the real server: a password MySQL refuses, a database that is
not there, a table that is not there, and SQL it will not parse. A mocked
version of this would be a test of the mock.
*/
func TestMySQLFailuresAreClassified(t *testing.T) {
	base := mysqlConfig(t)

	t.Run("a refused password", func(t *testing.T) {
		cfg := base
		cfg.Password = "not-the-password"

		err := open(t, cfg).Test(t.Context())
		requireReason(t, err, connectors.ReasonAuth)
		requireNoSecret(t, err, "not-the-password")
	})

	/*
		A database that does not exist, which MySQL reports as a permission
		failure rather than a missing one.

		Not what PostgreSQL does, and not a bug: MySQL will not tell an account
		without rights over a database whether it exists, because that is an
		information leak. The test asserts the classification *and* that the
		message says so -- an error that picked one of the two explanations
		would send half the people who hit it looking in the wrong place.

		Error 1049, the plain "no such database" an account with wider grants
		gets, is mapped but is not exercised here: causing it needs a
		privileged connection this suite deliberately does not hold.
	*/
	t.Run("a database that does not exist", func(t *testing.T) {
		cfg := base
		cfg.Database = "no_such_database_9f2c"

		err := open(t, cfg).Test(t.Context())
		requireReason(t, err, connectors.ReasonPermission)

		for _, want := range []string{"may not exist", "granted"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %v, want it to mention %q", err, want)
			}
		}
	})

	t.Run("nothing listening on the port", func(t *testing.T) {
		cfg := base
		cfg.Port = 3399

		err := open(t, cfg).Test(t.Context())
		requireReason(t, err, connectors.ReasonUnreachable)
	})

	t.Run("a host that does not resolve", func(t *testing.T) {
		cfg := base
		cfg.Host = "no-such-host.invalid"

		err := open(t, cfg).Test(t.Context())
		requireReason(t, err, connectors.ReasonUnreachable)
	})

	t.Run("a table that does not exist", func(t *testing.T) {
		_, err := open(t, base).Query(t.Context(), "SELECT * FROM pivot_no_such_table_9f2c")
		requireReason(t, err, connectors.ReasonSyntax)
	})

	t.Run("a column that does not exist", func(t *testing.T) {
		// Against information_schema, which is always there -- the
		// conformance fixture exists only while that test is running.
		_, err := open(t, base).Query(t.Context(),
			"SELECT no_such_column FROM information_schema.tables")
		requireReason(t, err, connectors.ReasonSyntax)
	})

	t.Run("SQL it will not parse", func(t *testing.T) {
		_, err := open(t, base).Query(t.Context(), "SELEKT 1")
		requireReason(t, err, connectors.ReasonSyntax)
	})
}

func requireReason(t *testing.T, err error, want connectors.Reason) {
	t.Helper()

	if err == nil {
		t.Fatalf("no error, want one classified %q", want)
	}

	if !errors.Is(err, &connectors.Error{Reason: want}) {
		t.Fatalf("error = %v, want it classified %q", err, want)
	}

	var classified *connectors.Error
	if errors.As(err, &classified) && classified.Message == "" {
		t.Error("the error is classified with nothing to show a person")
	}
}

func requireNoSecret(t *testing.T, err error, secret string) {
	t.Helper()

	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the error contains the password: %v", err)
	}
}
