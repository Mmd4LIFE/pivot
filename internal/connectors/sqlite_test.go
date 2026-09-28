package connectors_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
)

/*
The SQLite connector.

What the conformance suite cannot cover, because it is true of this connector
and no other: that the file is opened read-only, that the fields a network
source needs are refused rather than ignored, and that an interrupted query
really stops rather than merely returning.
*/

func sqliteConfig(t *testing.T) connectors.Config {
	t.Helper()

	return connectors.Config{Kind: connectors.KindSQLite, Database: seedSQLite(t)}
}

// --- read-only --------------------------------------------------------------

/*
The file cannot be written through this connector.

A BI source is something Pivot reads. A connector able to write to the file it
was pointed at is one stray statement away from changing somebody's data, and
the statement does not have to be malicious -- an UPDATE pasted into the query
editor is enough.

Caused rather than asserted from the DSN: the check is what SQLite does, not
what this package believes it asked for.
*/
func TestASQLiteSourceCannotBeWrittenTo(t *testing.T) {
	t.Parallel()

	connector := open(t, sqliteConfig(t))

	for name, statement := range map[string]string{
		"insert": "INSERT INTO " + sqliteFixtureTable +
			" (id, name, flag, ratio, created_utc, created_naive)" +
			" VALUES (99, 'x', 1, 0, '2024-01-01 00:00:00+00:00', '2024-01-01 00:00:00')",
		"update": "UPDATE " + sqliteFixtureTable + " SET name = 'changed'",
		"delete": "DELETE FROM " + sqliteFixtureTable,
		"drop":   "DROP TABLE " + sqliteFixtureTable,
		"create": "CREATE TABLE trespass (x INTEGER)",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := connector.Query(t.Context(), statement)
			if err == nil {
				t.Fatalf("a %s succeeded against a read-only connection", name)
			}

			if !errors.Is(err, &connectors.Error{Reason: connectors.ReasonPermission}) {
				t.Errorf("error = %v, want it classified as a permission failure", err)
			}
		})
	}
}

// And the rows are still what they were, which is the thing the test above is
// actually protecting -- an error that arrived after the write would satisfy
// every assertion up there and none of the intent.
func TestASQLiteSourceIsUnchangedAfterARefusedWrite(t *testing.T) {
	t.Parallel()

	cfg := sqliteConfig(t)
	connector := open(t, cfg)

	if _, err := connector.Query(t.Context(),
		"UPDATE "+sqliteFixtureTable+" SET name = 'changed'"); err == nil {
		t.Fatal("the update succeeded")
	}

	// Read back through a handle of our own, so this is the file rather than
	// the connector's opinion of it.
	db, err := sql.Open("sqlite", "file:"+cfg.Database)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer func() { _ = db.Close() }()

	var changed int64

	if qerr := db.QueryRowContext(t.Context(),
		"SELECT count(*) FROM "+sqliteFixtureTable+" WHERE name = 'changed'").Scan(&changed); qerr != nil {
		t.Fatalf("read back: %v", qerr)
	}

	if changed != 0 {
		t.Errorf("%d rows were written despite the connection being read-only", changed)
	}
}

// An option cannot turn the read-only mode off, because the connector sets it
// after everything a configuration supplied.
func TestSQLiteReadOnlyCannotBeOverridden(t *testing.T) {
	t.Parallel()

	cfg := sqliteConfig(t)
	cfg.Options = map[string]string{"mode": "rwc", "_pragma": "busy_timeout(1)"}

	connector := open(t, cfg)

	_, err := connector.Query(t.Context(), "CREATE TABLE trespass (x INTEGER)")
	if err == nil {
		t.Fatal("mode=rwc in Options reopened the file for writing")
	}

	if !errors.Is(err, &connectors.Error{Reason: connectors.ReasonPermission}) {
		t.Errorf("error = %v, want a permission failure", err)
	}
}

// --- the fields this source does not have -----------------------------------

/*
A host, a port or a credential is refused rather than quietly dropped.

Dropping them would leave a connection that looks authenticated in every
listing and is not: a SQLite file is protected by its filesystem permissions
and nothing else. Saying so is the difference between a misunderstanding
corrected now and one discovered during an audit.
*/
func TestSQLiteRefusesTheFieldsItCannotUse(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*connectors.Config){
		"a host":     func(c *connectors.Config) { c.Host = "db.internal" },
		"a port":     func(c *connectors.Config) { c.Port = 5432 },
		"a username": func(c *connectors.Config) { c.Username = "pivot" },
		"a password": func(c *connectors.Config) { c.Password = "hunter2" },
		"an SSL mode": func(c *connectors.Config) {
			c.SSLMode = "require"
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := connectors.Config{Kind: connectors.KindSQLite, Database: "/tmp/x.db"}
			mutate(&cfg)

			_, err := connectors.Open(cfg)
			if err == nil {
				t.Fatalf("%s was accepted", name)
			}

			if !strings.Contains(err.Error(), "SQLite connection has no") {
				t.Errorf("error = %v, want it to say which field", err)
			}
		})
	}
}

// Every unusable field is named at once, and in a stable order -- ranging a
// map is not, and the same mistake has to read the same way twice running.
func TestSQLiteNamesEveryUnusableFieldInOrder(t *testing.T) {
	t.Parallel()

	cfg := connectors.Config{
		Kind: connectors.KindSQLite, Database: "/tmp/x.db",
		Host: "h", Port: 1, Username: "u", Password: "p", SSLMode: "require",
	}

	first, err := connectors.Open(cfg)
	if err == nil {
		_ = first.Close()
		t.Fatal("a fully populated network config was accepted for SQLite")
	}

	want := "a SQLite connection has no SSL mode, host, password, port, username"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v,\nwant it to contain %q", err, want)
	}

	// And the password is not in it.
	if strings.Contains(err.Error(), "hunter2") {
		t.Error("the error repeated the password back")
	}
}

func TestSQLiteNeedsAPath(t *testing.T) {
	t.Parallel()

	for name, path := range map[string]string{
		"empty":       "",
		"only spaces": "   ",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := connectors.Open(connectors.Config{
				Kind: connectors.KindSQLite, Database: path,
			})

			if err == nil {
				t.Fatal("a connection with no path was accepted")
			}

			if !strings.Contains(err.Error(), "path to a database file") {
				t.Errorf("error = %v", err)
			}
		})
	}
}

/*
A URI is refused, because it would smuggle in the parameters this connector
sets itself.

`file:/data/x.db?mode=rwc` as a path would otherwise reach the driver with a
mode of its own, which is the read-only guarantee undone by a configuration
field.
*/
func TestSQLiteRefusesAURIAsAPath(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"file:/data/x.db",
		"/data/x.db?mode=rwc",
	} {
		_, err := connectors.Open(connectors.Config{
			Kind: connectors.KindSQLite, Database: path,
		})

		if err == nil {
			t.Fatalf("%q was accepted as a path", path)
		}

		if !strings.Contains(err.Error(), "may not be a URI") {
			t.Errorf("%q: error = %v", path, err)
		}
	}
}

// --- failures, caused rather than mocked ------------------------------------

func TestSQLiteFailuresAreClassified(t *testing.T) {
	t.Parallel()

	t.Run("a file that is not there", func(t *testing.T) {
		t.Parallel()

		connector := open(t, connectors.Config{
			Kind:     connectors.KindSQLite,
			Database: filepath.Join(t.TempDir(), "absent.db"),
		})

		// Read-only, so SQLite will not create it -- which is the behavior
		// that turns a typo into an error rather than into an empty database
		// somebody spends an afternoon wondering about.
		err := connector.Test(t.Context())
		if err == nil {
			t.Fatal("opening a file that does not exist succeeded")
		}

		if !errors.Is(err, &connectors.Error{Reason: connectors.ReasonNoDatabase}) {
			t.Errorf("error = %v, want it classified as a missing database", err)
		}
	})

	t.Run("a file that is not a database", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "notadb.db")
		if err := os.WriteFile(path, []byte("this is not a SQLite database"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		err := open(t, connectors.Config{
			Kind: connectors.KindSQLite, Database: path,
		}).Test(t.Context())

		if err == nil {
			t.Fatal("opening a text file as a database succeeded")
		}

		if !errors.Is(err, &connectors.Error{Reason: connectors.ReasonNoDatabase}) {
			t.Errorf("error = %v, want it classified as a missing database", err)
		}
	})

	t.Run("a file that cannot be read", func(t *testing.T) {
		t.Parallel()

		if os.Geteuid() == 0 {
			t.Skip("running as root, which can read a file with no permissions")
		}

		path := seedSQLite(t)
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatalf("chmod: %v", err)
		}

		err := open(t, connectors.Config{
			Kind: connectors.KindSQLite, Database: path,
		}).Test(t.Context())

		if err == nil {
			t.Fatal("opening an unreadable file succeeded")
		}

		// Either classification is honest here: SQLite reports CANTOPEN for a
		// file it may not open, and PERM where the platform distinguishes.
		// What matters is that it is not "unknown" and says what to check.
		if !strings.Contains(err.Error(), "readable") &&
			!strings.Contains(err.Error(), "permissions") {
			t.Errorf("error = %v, want it to point at the file permissions", err)
		}
	})
}

// --- stopping ---------------------------------------------------------------

/*
An interrupted query really stops, rather than returning while it runs on.

For a network database this is proven from a second connection. An embedded one
has no second place to look: the client and the server are the same goroutine.
So it is proven from the pool instead -- one connection, and a query issued
straight after the cancellation. If the interrupt had not taken, that second
query would queue behind a recursive CTE counting to six hundred million and
this would time out rather than return in milliseconds.
*/
func TestSQLiteCancellationReallyStops(t *testing.T) {
	t.Parallel()

	cfg := sqliteConfig(t)

	// One connection, so the follow-up query has nowhere else to go.
	cfg.MaxOpenConns = 1
	cfg.QueryTimeoutSeconds = 60

	connector := open(t, cfg)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	timer := time.AfterFunc(300*time.Millisecond, cancel)
	defer timer.Stop()

	if _, err := connector.Query(ctx, sqliteSleep(30)); err == nil {
		t.Fatal("a canceled query returned successfully")
	}

	// The same connection, immediately.
	started := time.Now()

	if _, err := connector.Query(t.Context(), "SELECT 1"); err != nil {
		t.Fatalf("the connector stopped working after a cancellation: %v", err)
	}

	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("the next query waited %s for the pool, so the canceled one was still running",
			elapsed.Round(time.Millisecond))
	}
}
