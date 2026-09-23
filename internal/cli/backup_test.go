package cli_test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

/*
Backup and restore.

The claim being tested is not "it copies a file". It is that the copy is
consistent while the database is being written to, that it is a database and
not a plausible-looking pile of bytes, and that restoring it leaves an instance
that works. A backup nobody has restored is a hypothesis.
*/

// emails reads the user list straight out of a SQLite file, without going
// through the application. What a restore produced has to be checkable
// independently of the thing that produced it.
func emails(t *testing.T, path string) []string {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}

	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(t.Context(), "SELECT email FROM users ORDER BY email")
	if err != nil {
		t.Fatalf("query %s: %v", path, err)
	}

	defer func() { _ = rows.Close() }()

	var out []string

	for rows.Next() {
		var email string
		if serr := rows.Scan(&email); serr != nil {
			t.Fatalf("scan: %v", serr)
		}

		out = append(out, email)
	}

	if rerr := rows.Err(); rerr != nil {
		t.Fatalf("rows: %v", rerr)
	}

	return out
}

// seeded builds a migrated database with one user, and returns its directory
// and the environment pointing at it.
func seeded(t *testing.T) (string, map[string]string) {
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

// The round trip, which is the only thing that proves a backup is a backup:
// take one, change the database, restore, and find the change gone.
func TestABackupCanBeRestored(t *testing.T) {
	t.Parallel()

	dir, env := seeded(t)
	backup := filepath.Join(dir, "snapshot.db")

	if _, _, err := run(t, env, "backup", "-o", backup); err != nil {
		t.Fatalf("backup: %v", err)
	}

	if _, _, err := run(t, env, "admin", "create-user",
		"--org", "acme", "--email", "grace@example.com", "--name", "Grace"); err != nil {
		t.Fatalf("create second user: %v", err)
	}

	live := filepath.Join(dir, "pivot.db")

	if got := emails(t, live); len(got) != 2 {
		t.Fatalf("before restore the database has %v, want two users", got)
	}

	if _, _, err := run(t, env, "restore", backup); err != nil {
		t.Fatalf("restore: %v", err)
	}

	got := emails(t, live)

	if len(got) != 1 || got[0] != "ada@example.com" {
		t.Errorf("after restore the database has %v, want only ada@example.com", got)
	}
}

/*
A backup taken while the database is being written to.

This is the property `VACUUM INTO` is used for and a file copy does not have.
With WAL enabled there are three files and no way to copy them atomically, so a
`cp` under load produces a snapshot of a moving target -- which restores as a
corrupt database, months later, when somebody needs it.
*/
func TestABackupTakenUnderWritesIsConsistent(t *testing.T) {
	t.Parallel()

	dir, env := seeded(t)
	backup := filepath.Join(dir, "under-load.db")
	live := filepath.Join(dir, "pivot.db")

	// A writer hammering the same database while the backup runs.
	db, err := sql.Open("sqlite", "file:"+live+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}

	defer func() { _ = db.Close() }()

	// The organization the seed created, because login_attempts references it.
	var orgID string

	if qerr := db.QueryRowContext(t.Context(),
		"SELECT id FROM organizations LIMIT 1").Scan(&orgID); qerr != nil {
		t.Fatalf("read org: %v", qerr)
	}

	ctx := t.Context()
	stop := make(chan struct{})

	// Closed after the first successful write, so the backup cannot start
	// before the writer has actually begun. Without it the goroutine may not
	// be scheduled until after the backup has finished, and the test then
	// measures nothing -- which is what it did until this comment existed.
	ready := make(chan struct{})

	var (
		wg      sync.WaitGroup
		once    sync.Once
		mu      sync.Mutex
		written int
		lastErr error
	)

	wg.Add(1)

	go func() {
		defer wg.Done()

		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}

			_, execErr := db.ExecContext(ctx,
				`INSERT INTO login_attempts (id, org_id, email, failed_count)
				 VALUES (?, ?, ?, 1)`,
				fmt.Sprintf("attempt-%d", i), orgID, fmt.Sprintf("churn-%d@example.com", i))

			mu.Lock()

			if execErr != nil {
				lastErr = execErr
			} else {
				written++

				once.Do(func() { close(ready) })
			}

			mu.Unlock()
		}
	}()

	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		close(stop)
		wg.Wait()
		t.Fatalf("the writer never completed a write: %v", lastErr)
	}

	_, _, berr := run(t, env, "backup", "-o", backup)

	close(stop)
	wg.Wait()

	if berr != nil {
		t.Fatalf("backup under writes: %v", berr)
	}

	// Without this the test is vacuous: a writer whose every statement fails
	// leaves the database perfectly still, and a plain file copy would pass.
	// The first version of this test inserted into a column that does not
	// exist and discarded the error, which is exactly that.
	mu.Lock()
	defer mu.Unlock()

	if written == 0 {
		t.Fatalf("the concurrent writer never wrote anything: %v", lastErr)
	}

	t.Logf("%d rows written while the backup ran", written)

	// The proof: the copy is a readable database with the seeded row in it.
	// A torn copy fails to open, or opens and then reports malformed pages.
	if got := emails(t, backup); len(got) != 1 || got[0] != "ada@example.com" {
		t.Errorf("the backup taken under load contains %v", got)
	}

	var integrity string

	check, err := sql.Open("sqlite", "file:"+backup)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}

	defer func() { _ = check.Close() }()

	if qerr := check.QueryRowContext(t.Context(), "PRAGMA integrity_check").Scan(&integrity); qerr != nil {
		t.Fatalf("integrity check: %v", qerr)
	}

	if integrity != "ok" {
		t.Errorf("integrity_check on the backup = %q, want ok", integrity)
	}
}

// Overwriting is refused. Replacing last night's backup with a broken one is
// worse than failing to write tonight's.
func TestBackupRefusesToOverwrite(t *testing.T) {
	t.Parallel()

	dir, env := seeded(t)
	backup := filepath.Join(dir, "snapshot.db")

	if _, _, err := run(t, env, "backup", "-o", backup); err != nil {
		t.Fatalf("first backup: %v", err)
	}

	_, _, err := run(t, env, "backup", "-o", backup)

	if err == nil {
		t.Fatal("a second backup overwrote the first")
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %v, want it to say the file exists", err)
	}
}

/*
The previous database is kept.

Restoring the wrong file is a mistake somebody makes at three in the morning
under pressure, and it must not be the end of the story. The live database is
renamed rather than deleted, and the command says where it went.
*/
func TestRestoreKeepsWhatItReplaced(t *testing.T) {
	t.Parallel()

	dir, env := seeded(t)
	backup := filepath.Join(dir, "snapshot.db")

	if _, _, err := run(t, env, "backup", "-o", backup); err != nil {
		t.Fatalf("backup: %v", err)
	}

	if _, _, err := run(t, env, "admin", "create-user",
		"--org", "acme", "--email", "grace@example.com", "--name", "Grace"); err != nil {
		t.Fatalf("create second user: %v", err)
	}

	stdout, _, err := run(t, env, "restore", backup)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	if !strings.Contains(stdout, "previous database is at") {
		t.Fatalf("the restore did not say where the old database went:\n%s", stdout)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	var replaced string

	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".replaced-") {
			replaced = filepath.Join(dir, entry.Name())
		}
	}

	if replaced == "" {
		t.Fatalf("nothing was kept: %v", entries)
	}

	// And what was kept is the database as it was, with both users.
	if got := emails(t, replaced); len(got) != 2 {
		t.Errorf("the replaced database has %v, want both users", got)
	}
}

/*
A file that is not a Pivot database is refused before anything is moved.

The dangerous version of this command notices the problem after renaming the
live database aside: the restore fails, the instance now has no database, and
the person running it is three commands further from where they started.
*/
func TestRestoreChecksTheBackupBeforeTouchingAnything(t *testing.T) {
	t.Parallel()

	dir, env := seeded(t)

	notADatabase := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notADatabase, []byte("this is not a database"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, _, err := run(t, env, "restore", notADatabase); err == nil {
		t.Fatal("restoring a text file succeeded")
	}

	// Untouched: still there, still with its user in it.
	if got := emails(t, filepath.Join(dir, "pivot.db")); len(got) != 1 {
		t.Errorf("the live database was disturbed: %v", got)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".replaced-") {
			t.Errorf("the live database was moved aside before the backup was checked: %s",
				entry.Name())
		}
	}
}

// A missing backup file is a clear error rather than a stack trace.
func TestRestoreRefusesAMissingFile(t *testing.T) {
	t.Parallel()

	dir, env := seeded(t)

	_, _, err := run(t, env, "restore", filepath.Join(dir, "nothing-here.db"))

	if err == nil {
		t.Fatal("restoring a file that does not exist succeeded")
	}
}

// Postgres is pointed at the right tool rather than given a worse one.
func TestBackupOnPostgresSaysToUsePgDump(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"PIVOT_DATABASE_URL": "postgres://pivot:pivot@127.0.0.1:1/pivot?sslmode=disable",
	}

	_, _, err := run(t, env, "restore", "anything.db")

	if err == nil {
		t.Fatal("restore accepted a Postgres instance")
	}

	if !strings.Contains(err.Error(), "pg_restore") {
		t.Errorf("error = %v, want it to name pg_restore", err)
	}
}
