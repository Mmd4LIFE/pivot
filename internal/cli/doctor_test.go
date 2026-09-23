package cli_test

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

/*
`pivot doctor`.

A diagnostic that only passes on a working machine is worth nothing: everybody
who runs it is running it because something is wrong. So the tests here mostly
break things on purpose and check that the output names the thing that is
broken, rather than naming a symptom of it.
*/

// doctorEnv points a command at a database in a fresh directory.
func doctorEnv(t *testing.T, dir string, port int) map[string]string {
	t.Helper()

	return map[string]string{
		"PIVOT_DATABASE_URL": "sqlite://" + filepath.Join(dir, "pivot.db"),
		"PIVOT_SERVER_PORT":  strconv.Itoa(port),
		// Off, so a missing migration is a finding rather than something the
		// command quietly fixes on the way past.
		"PIVOT_DATABASE_AUTO_MIGRATE": "false",
	}
}

// findingFor returns the line describing a named check, or "".
//
// Matching on the padded name rather than the bare one, so that looking for
// "database" does not return the line for "database directory".
func findingFor(out, name string) string {
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, " "+name+" ") {
			return line
		}
	}

	return ""
}

// A healthy install: every check passes and the command exits zero. The easy
// half, and the one that would still pass if every check were a no-op -- which
// is why it is the shortest test here.
func TestDoctorPassesOnAWorkingInstall(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	env := doctorEnv(t, dir, freePort(t))

	if _, _, err := run(t, env, "migrate", "up"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	stdout, _, err := run(t, env, "doctor")
	if err != nil {
		t.Fatalf("doctor on a working install: %v\n%s", err, stdout)
	}

	if !strings.Contains(stdout, "No problems found") {
		t.Errorf("doctor did not say it was happy:\n%s", stdout)
	}

	for _, check := range []string{"database", "schema", "listener"} {
		if line := findingFor(stdout, check); !strings.HasPrefix(strings.TrimSpace(line), "ok") {
			t.Errorf("%s = %q, want ok", check, strings.TrimSpace(line))
		}
	}
}

/*
A schema older than the binary.

This is what a deployment that skipped its migration step looks like, and its
symptom without `doctor` is an error from deep inside a query about a column
that does not exist -- which sends people to read the query.
*/
func TestDoctorReportsAPendingMigration(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	env := doctorEnv(t, dir, freePort(t))

	// A database that exists but has never been migrated.
	if _, _, err := run(t, env, "migrate", "status"); err != nil {
		t.Fatalf("migrate status: %v", err)
	}

	stdout, _, err := run(t, env, "doctor")

	if err == nil {
		t.Fatalf("doctor exited zero with the schema behind:\n%s", stdout)
	}

	line := findingFor(stdout, "schema")

	if !strings.Contains(line, "FAIL") {
		t.Errorf("schema = %q, want a failure", strings.TrimSpace(line))
	}

	if !strings.Contains(stdout, "pivot migrate up") {
		t.Error("the report does not say how to fix it")
	}
}

/*
A directory that cannot be written to.

The check writes a file rather than reading the mode bits, because the mode is
not the question: a directory can be 0777 and still refuse a write on a
read-only mount, a full disk, or under SELinux. And it checks the *directory*
rather than the database, because SQLite writes a WAL beside the file -- so a
perfectly writable database in a read-only directory fails on the first write
with a message about the database being read-only, which is the wrong place to
look.
*/
func TestDoctorReportsAnUnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can write to anything")
	}

	t.Parallel()

	dir := t.TempDir()
	env := doctorEnv(t, dir, freePort(t))

	if _, _, err := run(t, env, "migrate", "up"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	// Put it back, or the test framework cannot clean up after itself.
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	stdout, _, err := run(t, env, "doctor")

	if err == nil {
		t.Fatalf("doctor exited zero with an unwritable directory:\n%s", stdout)
	}

	if line := findingFor(stdout, "database directory"); !strings.Contains(line, "FAIL") {
		t.Errorf("database directory = %q, want a failure\n%s", strings.TrimSpace(line), stdout)
	}
}

// A port somebody else already has. The most common reason `pivot serve` exits
// immediately under a service manager, where the error scrolls past.
func TestDoctorReportsAPortAlreadyInUse(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig

	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	defer func() { _ = listener.Close() }()

	port := listener.Addr().(*net.TCPAddr).Port

	dir := t.TempDir()
	env := doctorEnv(t, dir, port)
	env["PIVOT_SERVER_HOST"] = "127.0.0.1"

	if _, _, merr := run(t, env, "migrate", "up"); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}

	stdout, _, derr := run(t, env, "doctor")

	if derr == nil {
		t.Fatalf("doctor exited zero with the port taken:\n%s", stdout)
	}

	if line := findingFor(stdout, "listener"); !strings.Contains(line, "FAIL") {
		t.Errorf("listener = %q, want a failure\n%s", strings.TrimSpace(line), stdout)
	}
}

// A database URL that is not one. The check runs before anything tries to
// connect, so the report says what is wrong with the setting rather than what
// the driver made of it.
func TestDoctorReportsAnUnusableDatabaseURL(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"PIVOT_DATABASE_URL": "mysql://user:pass@localhost/pivot",
		"PIVOT_SERVER_PORT":  strconv.Itoa(freePort(t)),
	}

	stdout, _, err := run(t, env, "doctor")

	if err == nil {
		t.Fatalf("doctor exited zero with an unsupported database:\n%s", stdout)
	}

	if !strings.Contains(stdout, "database url") || !strings.Contains(stdout, "FAIL") {
		t.Errorf("the report does not name the URL:\n%s", stdout)
	}
}

/*
A warning does not fail the run.

An operator who puts `pivot doctor` in a provisioning script needs it to fail
only on things that are actually broken. One that exits non-zero over a file
mode is one nobody runs twice.
*/
func TestDoctorWarningsDoNotFailTheRun(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	env := doctorEnv(t, dir, freePort(t))

	if _, _, err := run(t, env, "migrate", "up"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// The permission warning: a database anybody on the machine can read.
	if err := os.Chmod(filepath.Join(dir, "pivot.db"), 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	stdout, _, err := run(t, env, "doctor")
	if err != nil {
		t.Fatalf("a warning failed the run: %v\n%s", err, stdout)
	}

	if !strings.Contains(stdout, "warn") {
		t.Errorf("the loose file mode was not reported at all:\n%s", stdout)
	}

	if !strings.Contains(stdout, "No problems found") {
		t.Errorf("a warning was counted as a problem:\n%s", stdout)
	}
}

// A database Pivot creates is readable by its owner and nobody else. It holds
// session token hashes and stored secrets, and SQLite's own default leaves it
// readable by every account on the machine.
func TestANewDatabaseIsNotWorldReadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where file modes are advisory")
	}

	t.Parallel()

	dir := t.TempDir()
	env := doctorEnv(t, dir, freePort(t))

	if _, _, err := run(t, env, "migrate", "up"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "pivot.db"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("a new database is mode %o; it holds session tokens and secrets", perm)
	}
}
