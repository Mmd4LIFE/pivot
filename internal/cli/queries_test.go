package cli_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	_ "modernc.org/sqlite"
)

/*
`pivot admin queries`.

The monitor's read half. It exists because "what is running right now" is asked
while something is on fire, and the answer comes from the query log rather than
from any process's memory -- which is what makes it answerable from a different
instance than the one running the query, and what makes it survive a restart.

Tested for the same reason `pivot admin jobs` is: a command whose whole purpose
is making something visible, with no test, can show nothing and look fine.
*/
func TestQueriesSaysSoWhenNothingIsRunning(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)

	stdout, _, err := run(t, env, "admin", "queries")
	if err != nil {
		t.Fatalf("queries: %v", err)
	}

	if !strings.Contains(stdout, "Nothing is running") {
		t.Errorf("an idle instance did not say so:\n%s", stdout)
	}
}

// And usage over a window with no queries says so rather than printing an
// empty table, which reads as a broken command.
func TestUsageSaysSoWhenThereIsNone(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)

	stdout, _, err := run(t, env, "admin", "queries", "--usage")
	if err != nil {
		t.Fatalf("queries --usage: %v", err)
	}

	if !strings.Contains(stdout, "No queries") {
		t.Errorf("an empty window did not say so:\n%s", stdout)
	}
}

/*
A running query is shown, with whether anything is still running it.

Seeded with SQL rather than by running a query, because what is under test is
the display: the command reads the log, and a row in the log is exactly what it
would see if another instance had written it. That is also the case that
matters -- the monitor exists to be useful about queries this process is not
running.
*/
func TestQueriesShowsARunningQueryAndItsHealth(t *testing.T) {
	t.Parallel()

	dir, env := withOrg(t)
	env["PIVOT_CONNECTION_PASSWORD"] = warehousePassword

	if _, _, err := addWarehouse(t, env); err != nil {
		t.Fatalf("add-connection: %v", err)
	}

	cases := map[string]struct {
		owner     string
		heartbeat string
		want      string
	}{
		"beating":       {"instance-1", nowISO(), "running"},
		"gone quiet":    {"instance-1", agoISO(5 * time.Minute), "abandoned"},
		"never claimed": {"", "", "unknown owner"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			id := seedRunningQuery(t, dir, tc.owner, tc.heartbeat)

			stdout, _, err := run(t, env, "admin", "queries")
			if err != nil {
				t.Fatalf("queries: %v", err)
			}

			if !strings.Contains(stdout, id) {
				t.Errorf("the running query is not listed:\n%s", stdout)
			}

			if !strings.Contains(stdout, tc.want) {
				t.Errorf("health reads as something other than %q:\n%s", tc.want, stdout)
			}

			clearQueries(t, dir)
		})
	}
}

// Asking to kill a running query records the request and says what happens
// next -- which depends on whether anything is there to act on it.
func TestKillAsksAndSaysWhatWillHappen(t *testing.T) {
	t.Parallel()

	dir, env := withOrg(t)
	env["PIVOT_CONNECTION_PASSWORD"] = warehousePassword

	if _, _, err := addWarehouse(t, env); err != nil {
		t.Fatalf("add-connection: %v", err)
	}

	// A live owner: the request will be delivered.
	id := seedRunningQuery(t, dir, "instance-1", nowISO())

	stdout, _, err := run(t, env, "admin", "queries", "--kill", id)
	if err != nil {
		t.Fatalf("kill: %v", err)
	}

	if !strings.Contains(stdout, "Asked query") {
		t.Errorf("the kill was not acknowledged:\n%s", stdout)
	}

	if !strings.Contains(stdout, "will stop it") {
		t.Errorf("a live owner was not reported as able to act:\n%s", stdout)
	}

	// And the request is on the row, so another instance can find it.
	if got := cancelRequestedAt(t, dir, id); got == "" {
		t.Error("the kill request was not recorded on the row")
	}

	clearQueries(t, dir)

	// An abandoned owner: the request may never be delivered, and saying
	// "stopped" here would be a claim this process cannot make.
	stale := seedRunningQuery(t, dir, "instance-9", agoISO(10*time.Minute))

	stdout, _, err = run(t, env, "admin", "queries", "--kill", stale)
	if err != nil {
		t.Fatalf("kill: %v", err)
	}

	if !strings.Contains(stdout, "may never be delivered") {
		t.Errorf("an abandoned owner was not flagged:\n%s", stdout)
	}
}

// Killing something that is not a query, or not running, fails with a message
// that says which.
func TestKillRefusesWhatItCannotStop(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)

	if _, _, err := run(t, env, "admin", "queries", "--kill", "not-a-uuid"); err == nil {
		t.Error("a malformed id was accepted")
	}

	if _, _, err := run(t, env, "admin", "queries", "--kill", uuid.NewString()); err == nil {
		t.Error("an unknown id was accepted")
	}
}

// --- helpers -----------------------------------------------------------------

func nowISO() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

func agoISO(d time.Duration) string {
	return time.Now().UTC().Add(-d).Format("2006-01-02T15:04:05.000Z")
}

// seedRunningQuery writes a query_log row directly, standing in for a query
// another instance is running.
func seedRunningQuery(t *testing.T, dir, owner, heartbeat string) string {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "pivot.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer func() { _ = db.Close() }()

	var orgID, connID string
	if err := db.QueryRowContext(t.Context(), "SELECT org_id, id FROM connections").Scan(&orgID, &connID); err != nil {
		t.Fatalf("read the connection: %v", err)
	}

	id := uuid.NewString()

	var beat any
	if heartbeat != "" {
		beat = heartbeat
	}

	if _, err := db.ExecContext(t.Context(), `INSERT INTO query_log
		(id, org_id, connection_id, sql_text, state, started_at, owner, heartbeat_at)
		VALUES (?, ?, ?, ?, 'running', ?, ?, ?)`,
		id, orgID, connID, "SELECT 1 FROM orders", agoISO(time.Minute), owner, beat,
	); err != nil {
		t.Fatalf("seed the query: %v", err)
	}

	return id
}

func clearQueries(t *testing.T, dir string) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "pivot.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(t.Context(), "DELETE FROM query_log"); err != nil {
		t.Fatalf("clear: %v", err)
	}
}

func cancelRequestedAt(t *testing.T, dir, id string) string {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "pivot.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer func() { _ = db.Close() }()

	var at sql.NullString
	if err := db.QueryRowContext(t.Context(), "SELECT cancel_requested_at FROM query_log WHERE id = ?", id).
		Scan(&at); err != nil {
		t.Fatalf("read: %v", err)
	}

	return at.String
}
