package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotSQLite is returned when a SQLite-only operation is asked of Postgres.
//
// Backing up Postgres is `pg_dump`'s job, and reimplementing it badly inside
// this binary would produce something that looks like a backup and is not.
var ErrNotSQLite = errors.New("store: this operation is only available on SQLite")

// SQLitePath returns the filesystem path behind a database URL.
//
// The DSN the driver is given carries pragmas and a `file:` prefix, neither of
// which anybody can pass to `cp`. This is the plain path: what to check
// permissions on, what to back up, and what to tell somebody to keep.
//
// Reports false for Postgres and for an in-memory database, which have no file.
func SQLitePath(raw string) (string, bool) {
	engine, dsn, err := ParseURL(raw)
	if err != nil || engine != EngineSQLite {
		return "", false
	}

	// Strip the scheme and everything the driver added.
	path := strings.TrimPrefix(dsn, "file:")
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}

	if path == "" || strings.HasPrefix(path, ":memory:") {
		return "", false
	}

	return path, true
}

// Backup writes a consistent copy of a SQLite database to dst.
//
// `VACUUM INTO`, which is SQLite's own answer and the only correct one while
// the database is being served. Copying the file is not: with WAL enabled
// there are three files, the copy is not atomic across them, and what you get
// is a snapshot of a moving target that restores as "database disk image is
// malformed" -- months later, when somebody needs it.
//
// It runs inside a read transaction, so writers are not blocked and the result
// is the database as of one instant. It also rebuilds the file, so the copy is
// compact: the same reason `VACUUM` exists.
//
// The destination must not already exist. SQLite refuses to overwrite, and
// that refusal is worth keeping rather than working around -- a backup command
// that silently replaces last night's backup with a broken one is worse than
// no backup command.
func Backup(ctx context.Context, db *DB, dst string) error {
	if !db.IsSQLite() {
		return ErrNotSQLite
	}

	abs, err := filepath.Abs(dst)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", dst, err)
	}

	if _, serr := os.Stat(abs); serr == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite a backup", dst)
	} else if !errors.Is(serr, os.ErrNotExist) {
		return fmt.Errorf("check %s: %w", dst, serr)
	}

	if mkerr := os.MkdirAll(filepath.Dir(abs), 0o750); mkerr != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(abs), mkerr)
	}

	// Quoted as a SQL string literal rather than bound as a parameter: SQLite
	// does not accept a parameter for VACUUM INTO's target. Single quotes are
	// doubled, which is the only escape this grammar has.
	quoted := "'" + strings.ReplaceAll(abs, "'", "''") + "'"

	if _, execErr := db.ExecContext(ctx, "VACUUM INTO "+quoted); execErr != nil {
		return fmt.Errorf("back up to %s: %w", dst, execErr)
	}

	// Readable by the owner only. A backup holds every session token hash and
	// every stored secret in the instance; inheriting the process umask means
	// that ends up world-readable on a default-configured machine.
	if cherr := os.Chmod(abs, 0o600); cherr != nil {
		return fmt.Errorf("restrict permissions on %s: %w", dst, cherr)
	}

	return nil
}
