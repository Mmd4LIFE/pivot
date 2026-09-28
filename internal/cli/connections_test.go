package cli_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

/*
The connection commands.

Most of these run without a database to connect *to*, using --no-test: what
they check is the command's own behavior, and reaching a real PostgreSQL is
what internal/connectors tests do. The one thing checked against the file is
that the password is not in it.
*/

const warehousePassword = "the-warehouse-password"

// withOrg builds an instance with one organization and returns its directory
// and environment.
func withOrg(t *testing.T) (string, map[string]string) {
	t.Helper()

	dir := t.TempDir()
	env := map[string]string{
		"PIVOT_DATABASE_URL":   "sqlite://" + filepath.Join(dir, "pivot.db"),
		"PIVOT_ADMIN_PASSWORD": "a-long-enough-password",
	}

	if _, _, err := run(t, env, "migrate", "up"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, _, err := run(t, env, "admin", "create-user",
		"--create-org", "Acme", "--email", "ada@example.com", "--name", "Ada"); err != nil {
		t.Fatalf("create user: %v", err)
	}

	return dir, env
}

// addWarehouse stores a connection without reaching a database.
func addWarehouse(t *testing.T, env map[string]string, extra ...string) (string, string, error) {
	t.Helper()

	args := append([]string{
		"admin", "add-connection",
		"--slug", "warehouse", "--name", "Analytics warehouse",
		"--db-host", "db.internal", "--db-port", "5432",
		"--database", "analytics", "--username", "pivot",
		"--no-test",
	}, extra...)

	return run(t, env, args...)
}

// The password reaches the database as an envelope, and the command never took
// it as a flag -- a password passed as an argument lands in the shell history
// and in the process list.
func TestAConnectionPasswordIsNotStoredInTheClear(t *testing.T) {
	t.Parallel()

	dir, env := withOrg(t)
	env["PIVOT_CONNECTION_PASSWORD"] = warehousePassword

	if _, _, err := addWarehouse(t, env); err != nil {
		t.Fatalf("add-connection: %v", err)
	}

	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "pivot.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer func() { _ = db.Close() }()

	var stored string

	if qerr := db.QueryRowContext(t.Context(),
		"SELECT password FROM connections").Scan(&stored); qerr != nil {
		t.Fatalf("read the column: %v", qerr)
	}

	if strings.Contains(stored, warehousePassword) {
		t.Fatalf("the password is in the database in the clear: %s", stored)
	}

	if !strings.HasPrefix(stored, "pivot.v1.") {
		t.Errorf("the stored value is not an envelope: %s", stored)
	}
}

// A connection that cannot be reached is refused and nothing is stored, so a
// typo is reported to the person who made it rather than to whoever opens the
// query editor tomorrow.
func TestAnUnreachableConnectionIsRefusedAndStoresNothing(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)

	// No --no-test, and a host that does not resolve.
	_, _, err := run(t, env, "admin", "add-connection",
		"--slug", "warehouse", "--name", "Warehouse",
		"--db-host", "no-such-host.invalid", "--database", "analytics",
		"--username", "pivot")

	if err == nil {
		t.Fatal("a connection to a host that does not resolve was accepted")
	}

	if !strings.Contains(err.Error(), "resolve") {
		t.Errorf("error = %v, want it to say the host does not resolve", err)
	}

	if !strings.Contains(err.Error(), "Nothing was stored") {
		t.Errorf("error = %v, want it to say nothing was stored", err)
	}

	stdout, _, lerr := run(t, env, "admin", "list-connections")
	if lerr != nil {
		t.Fatalf("list: %v", lerr)
	}

	if !strings.Contains(stdout, "No connections") {
		t.Errorf("a refused connection was stored anyway:\n%s", stdout)
	}
}

// --no-test is the escape hatch for a source behind a firewall this machine
// cannot cross, and it has to actually store the row.
func TestNoTestStoresWithoutReachingTheDatabase(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)

	stdout, _, err := addWarehouse(t, env)
	if err != nil {
		t.Fatalf("add-connection --no-test: %v", err)
	}

	if !strings.Contains(stdout, "Configured") {
		t.Errorf("stdout = %q, want it to confirm", stdout)
	}

	// And it did not claim to have connected.
	if strings.Contains(stdout, "Connected") {
		t.Errorf("--no-test reported a connection it never made:\n%s", stdout)
	}
}

func TestAddConnectionRequiresWhatItNeeds(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)

	for name, args := range map[string][]string{
		"no slug":     {"--name", "W", "--db-host", "h", "--database", "d", "--username", "u"},
		"no name":     {"--slug", "w", "--db-host", "h", "--database", "d", "--username", "u"},
		"no host":     {"--slug", "w", "--name", "W", "--database", "d", "--username", "u"},
		"no database": {"--slug", "w", "--name", "W", "--db-host", "h", "--username", "u"},
		"no username": {"--slug", "w", "--name", "W", "--db-host", "h", "--database", "d"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			full := append([]string{"admin", "add-connection", "--no-test"}, args...)

			if _, _, err := run(t, env, full...); err == nil {
				t.Errorf("%s was accepted", name)
			}
		})
	}
}

// An unknown connector is refused by name, and the error lists what this build
// does have -- because the answer is almost always a typo.
func TestAnUnknownConnectorIsRefusedWithTheAlternatives(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)

	_, _, err := run(t, env, "admin", "add-connection", "--no-test",
		"--slug", "w", "--name", "W", "--kind", "oracle",
		"--db-host", "h", "--database", "d", "--username", "u")

	if err == nil {
		t.Fatal("an unknown connector kind was accepted")
	}

	if !strings.Contains(err.Error(), "oracle") || !strings.Contains(err.Error(), "postgres") {
		t.Errorf("error = %v, want it to name the kind and list the alternatives", err)
	}
}

// A duplicate slug is reported as one, naming the organization -- because on a
// multi-tenant instance "already exists" without it is a puzzle.
func TestADuplicateSlugIsReported(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)

	if _, _, err := addWarehouse(t, env); err != nil {
		t.Fatalf("first add: %v", err)
	}

	_, _, err := addWarehouse(t, env)

	if err == nil {
		t.Fatal("a duplicate slug was accepted")
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %v", err)
	}
}

/*
The list shows what an administrator needs and not the password.

This output ends up in issues and screenshots, so the check is that the
password is absent rather than that the columns are pretty.
*/
func TestListingConnectionsShowsNoSecret(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)
	env["PIVOT_CONNECTION_PASSWORD"] = warehousePassword

	if _, _, err := addWarehouse(t, env); err != nil {
		t.Fatalf("add: %v", err)
	}

	stdout, _, err := run(t, env, "admin", "list-connections")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if strings.Contains(stdout, warehousePassword) {
		t.Fatalf("the password is in the listing:\n%s", stdout)
	}

	for _, want := range []string{"warehouse", "Analytics warehouse", "postgres", "db.internal"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the listing does not show %q:\n%s", want, stdout)
		}
	}

	// Never tested, and it says so rather than implying it works.
	if !strings.Contains(stdout, "never") {
		t.Errorf("an untested connection does not say so:\n%s", stdout)
	}
}

func TestListingNothingSaysSo(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)

	stdout, _, err := run(t, env, "admin", "list-connections")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if !strings.Contains(stdout, "No connections") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestTestingAConnectionThatDoesNotExist(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)

	_, _, err := run(t, env, "admin", "test-connection", "nothing-here")

	if err == nil {
		t.Fatal("testing a connection that does not exist succeeded")
	}

	if !strings.Contains(err.Error(), "nothing-here") {
		t.Errorf("error = %v, want it to name the slug", err)
	}
}

/*
A failed test is recorded rather than only reported.

The result is what a list page shows without testing every row, and a failure
is the more useful of the two to have on record -- so the command has to store
it even though it is also returning an error.
*/
func TestAFailedTestIsRecordedOnTheConnection(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)

	if _, _, err := addWarehouse(t, env); err != nil {
		t.Fatalf("add: %v", err)
	}

	// db.internal does not resolve, so this fails -- which is the point.
	if _, _, err := run(t, env, "admin", "test-connection", "warehouse"); err == nil {
		t.Fatal("testing an unreachable connection succeeded")
	}

	stdout, _, err := run(t, env, "admin", "list-connections")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if !strings.Contains(stdout, "FAILED") {
		t.Errorf("the failed test was not recorded:\n%s", stdout)
	}
}

// The password is read from the environment and never from a flag.
func TestThePasswordIsNotAFlag(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)

	_, _, err := run(t, env, "admin", "add-connection", "--no-test",
		"--slug", "w", "--name", "W", "--db-host", "h",
		"--database", "d", "--username", "u", "--password", warehousePassword)

	if err == nil {
		t.Fatal("--password was accepted; a password in argv is readable by every process")
	}

	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("error = %v, want an unknown-flag error", err)
	}
}

// The help says where the password comes from, because a command whose
// required input is invisible is a command nobody can use.
func TestAddConnectionHelpNamesTheEnvironmentVariable(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, nil, "admin", "add-connection", "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}

	if !strings.Contains(stdout, "PIVOT_CONNECTION_PASSWORD") {
		t.Errorf("the help does not say where the password comes from:\n%s", stdout)
	}
}

func TestConnectionCommandsAreRegistered(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, nil, "admin", "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}

	for _, want := range []string{"add-connection", "test-connection", "list-connections"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("%s is not listed in `admin --help`:\n%s", want, stdout)
		}
	}
}

/*
A file-backed connection needs no host, no port and no credentials.

The command used to require --db-host and --username of its own accord, which
made a SQLite connection impossible to configure: it has neither. What a
connection needs is the connector's business now, and this is the test that
says so -- it passes only a path.
*/
func TestAFileBackedConnectionNeedsOnlyAPath(t *testing.T) {
	t.Parallel()

	dir, env := withOrg(t)
	source := filepath.Join(dir, "warehouse.db")

	// A real SQLite file, so the connection can be tested rather than stored
	// with --no-test. That is the point: a file-backed source is one this
	// machine can always reach.
	seedSource(t, source)

	stdout, _, err := run(t, env, "admin", "add-connection",
		"--kind", "sqlite", "--slug", "files", "--name", "Local files",
		"--database", source)

	if err != nil {
		t.Fatalf("add-connection for a file: %v", err)
	}

	if !strings.Contains(stdout, "Connected") {
		t.Errorf("stdout = %q, want it to confirm it reached the file", stdout)
	}

	listing, _, lerr := run(t, env, "admin", "list-connections")
	if lerr != nil {
		t.Fatalf("list: %v", lerr)
	}

	if !strings.Contains(listing, "sqlite") {
		t.Errorf("the connection is not listed:\n%s", listing)
	}
}

// And the fields it cannot use are refused by the connector rather than
// ignored by the command.
func TestAFileBackedConnectionRefusesCredentials(t *testing.T) {
	t.Parallel()

	dir, env := withOrg(t)
	source := filepath.Join(dir, "warehouse.db")
	seedSource(t, source)

	_, _, err := run(t, env, "admin", "add-connection",
		"--kind", "sqlite", "--slug", "files", "--name", "Local files",
		"--database", source, "--username", "pivot")

	if err == nil {
		t.Fatal("a username was accepted for a file-backed connection")
	}

	if !strings.Contains(err.Error(), "username") {
		t.Errorf("error = %v, want it to name the unusable field", err)
	}
}

/*
--no-test skips reaching the source, not validating the configuration.

The escape hatch is for a database behind a firewall this machine cannot
cross. A configuration the connector would refuse outright is a different
thing, and storing one only moves the failure to whoever opens the query
editor tomorrow.
*/
func TestNoTestStillValidatesTheConfiguration(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)

	_, _, err := run(t, env, "admin", "add-connection", "--no-test",
		"--kind", "sqlite", "--slug", "files", "--name", "Local files")

	if err == nil {
		t.Fatal("a SQLite connection with no path was stored")
	}

	if !strings.Contains(err.Error(), "path to a database file") {
		t.Errorf("error = %v, want the connector's own complaint", err)
	}
}

// seedSource writes a small SQLite database for a connection to point at.
func seedSource(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("create the source: %v", err)
	}

	defer func() { _ = db.Close() }()

	if _, err = db.ExecContext(t.Context(),
		"CREATE TABLE orders (id INTEGER NOT NULL PRIMARY KEY)"); err != nil {
		t.Fatalf("seed the source: %v", err)
	}
}
