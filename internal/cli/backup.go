package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/store"
)

/*
Backup and restore, for SQLite.

Postgres is deliberately not covered: `pg_dump` exists, every operator running
Postgres already has it, and a worse reimplementation inside this binary would
produce something that looks like a backup. Asked to back up a Postgres
instance, these commands say so and point at the right tool.

Restore is a separate command rather than a documented `cp` because the thing
that goes wrong is not the copy. It is copying a database file while the server
holds it open, or copying it without the WAL beside it, and getting a file that
restores months later as "database disk image is malformed".
*/

// newBackupCmd builds `pivot backup`.
func newBackupCmd(env Env, flags *globalFlags) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Write a consistent copy of a SQLite database",
		Long: `Write a consistent copy of the metadata database.

Safe to run while Pivot is serving: it uses SQLite's own VACUUM INTO, which
takes its copy inside a read transaction, so writers are not blocked and the
result is the database as of one instant.

Copying the file by hand is not equivalent. With WAL enabled there are three
files, a copy is not atomic across them, and the result is a snapshot of a
moving target -- which restores as a corrupt database, at the worst possible
moment.

The output must not already exist. Overwriting last night's backup with a
broken one is worse than failing.

PostgreSQL is not supported here on purpose: use pg_dump.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := loadConfig(cmd, env, flags)
			if err != nil {
				return err
			}

			dst := output
			if dst == "" {
				dst = defaultBackupName(res.Config.Database.URL, time.Now())
			}

			log := logging.New(res.Config.Log, env.Stderr)

			db, err := store.Open(cmd.Context(), res.Config.Database, log)
			if err != nil {
				return err
			}

			defer func() { _ = db.Close() }()

			if !db.IsSQLite() {
				return fmt.Errorf(
					"%w: this instance uses %s, so use pg_dump", store.ErrNotSQLite, db.Engine())
			}

			if berr := store.Backup(cmd.Context(), db, dst); berr != nil {
				return berr
			}

			info, err := os.Stat(dst)
			if err != nil {
				return fmt.Errorf("stat %s: %w", dst, err)
			}

			fmt.Fprintf(env.Stdout, "Backed up to %s (%d bytes).\n", dst, info.Size())
			fmt.Fprintf(env.Stdout, "Restore with: pivot restore %s\n", dst)

			return nil
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "",
		"Where to write the backup (default: the database's name with a timestamp)")

	return cmd
}

// newRestoreCmd builds `pivot restore`.
func newRestoreCmd(env Env, flags *globalFlags) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "restore <backup>",
		Short: "Replace the SQLite database with a backup",
		Long: `Replace the metadata database with a backup taken by ` + "`pivot backup`" + `.

Stop Pivot first. Restoring underneath a running server leaves it holding a
descriptor on the file that was replaced -- it keeps reading the old contents
and writing them back, and the restore silently does nothing.

The existing database is renamed aside rather than deleted, so a restore of the
wrong file is recoverable. The WAL and shared-memory files are removed with it:
left behind, they belong to the database that was replaced, and SQLite will
apply them to the new one.

PostgreSQL is not supported here on purpose: use pg_restore.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]

			res, err := loadConfig(cmd, env, flags)
			if err != nil {
				return err
			}

			target, isFile := store.SQLitePath(res.Config.Database.URL)
			if !isFile {
				return fmt.Errorf(
					"%w: this instance's database is %q, so use pg_restore",
					store.ErrNotSQLite, res.Config.Database.URL)
			}

			if _, serr := os.Stat(source); serr != nil {
				return fmt.Errorf("read the backup: %w", serr)
			}

			// Opened and checked before anything is moved. A file that is not
			// a SQLite database, or one that is corrupt, must be found out
			// about now rather than after the live database has been renamed.
			if verr := verifyBackup(cmd, env, source); verr != nil {
				return verr
			}

			replaced, err := moveAside(target, force)
			if err != nil {
				return err
			}

			if cerr := copyFile(source, target); cerr != nil {
				return fmt.Errorf("restore %s: %w", source, cerr)
			}

			// The WAL and shm belong to the database that was just replaced.
			// Leaving them means SQLite replays another database's journal
			// into this one, which corrupts it.
			for _, sidecar := range []string{target + "-wal", target + "-shm"} {
				_ = os.Remove(sidecar)
			}

			fmt.Fprintf(env.Stdout, "Restored %s to %s.\n", source, target)

			if replaced != "" {
				fmt.Fprintf(env.Stdout, "The previous database is at %s; delete it once you are sure.\n",
					replaced)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false,
		"Delete the existing database instead of renaming it aside")

	return cmd
}

// verifyBackup opens the backup and asks it a question only a real database
// can answer.
func verifyBackup(cmd *cobra.Command, env Env, source string) error {
	cfg := configForFile(source)

	log := logging.New(cfg.Log, env.Stderr)

	db, err := store.Open(cmd.Context(), cfg.Database, log)
	if err != nil {
		return fmt.Errorf("%s is not a database this can restore: %w", source, err)
	}

	defer func() { _ = db.Close() }()

	version, err := store.CurrentVersion(cmd.Context(), db)
	if err != nil {
		return fmt.Errorf("%s does not look like a Pivot database: %w", source, err)
	}

	fmt.Fprintf(env.Stdout, "%s is a Pivot database at schema version %d.\n", source, version)

	return nil
}

// moveAside renames the existing database out of the way, returning where it
// went. Returns "" when there was nothing to move.
func moveAside(target string, force bool) (string, error) {
	if _, err := os.Stat(target); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}

		return "", fmt.Errorf("check %s: %w", target, err)
	}

	if force {
		if err := os.Remove(target); err != nil {
			return "", fmt.Errorf("remove %s: %w", target, err)
		}

		return "", nil
	}

	aside := fmt.Sprintf("%s.replaced-%s", target, time.Now().UTC().Format("20060102T150405Z"))

	if err := os.Rename(target, aside); err != nil {
		return "", fmt.Errorf("move %s aside: %w", target, err)
	}

	return aside, nil
}

// copyFile writes src to dst with an owner-only mode.
//
// Both paths come from this machine's own configuration and this machine's own
// command line, not from a request -- so the traversal gosec warns about would
// mean an operator writing a path they typed. Cleaned anyway, because it costs
// nothing and the annotation should not be the only thing standing there.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(filepath.Clean(src))
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Clean(dst), data, 0o600) //nolint:gosec // G703: operator-supplied path, not request input
}

// configForFile builds a configuration pointed at one SQLite file, for
// inspecting a backup without touching the instance's own configuration.
func configForFile(path string) *config.Config {
	cfg := config.Default()
	cfg.Database.URL = "sqlite://" + path
	cfg.Database.AutoMigrate = false
	cfg.Log.Level = "error"

	return cfg
}

// defaultBackupName derives a name from the database and the time.
//
// UTC and sortable, because backups end up in one directory and the only
// ordering anybody can rely on is the filename's.
func defaultBackupName(databaseURL string, now time.Time) string {
	base := "pivot"

	if path, isFile := store.SQLitePath(databaseURL); isFile {
		base = filepath.Base(path)
		base = base[:len(base)-len(filepath.Ext(base))]
	}

	return fmt.Sprintf("%s-%s.db", base, now.UTC().Format("20060102T150405Z"))
}
