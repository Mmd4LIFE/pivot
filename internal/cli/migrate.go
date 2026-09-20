package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/store"
)

func newMigrateCmd(env Env, flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Manage the metadata database schema",
		Long: `Manage the metadata database schema.

Migrations are forward-only in production and embedded in the binary, so a
release carries its own schema. Running "migrate up" twice is a no-op.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newMigrateUpCmd(env, flags),
		newMigrateStatusCmd(env, flags),
		newMigrateVersionCmd(env, flags),
		newMigrateCreateCmd(env),
	)

	return cmd
}

// openDB loads configuration and connects, for commands that need the database.
func openDB(cmd *cobra.Command, env Env, flags *globalFlags) (*store.DB, error) {
	res, err := loadConfig(cmd, env, flags)
	if err != nil {
		return nil, err
	}

	log := logging.New(res.Config.Log, env.Stderr)

	db, err := store.Open(cmd.Context(), res.Config.Database, log)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func newMigrateUpCmd(env Env, flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Apply pending migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := loadConfig(cmd, env, flags)
			if err != nil {
				return err
			}

			log := logging.New(res.Config.Log, env.Stderr)

			db, err := store.Open(cmd.Context(), res.Config.Database, log)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			before, err := store.CurrentVersion(cmd.Context(), db)
			if err != nil {
				return err
			}

			if migrateErr := store.Migrate(cmd.Context(), db, log); migrateErr != nil {
				return migrateErr
			}

			after, err := store.CurrentVersion(cmd.Context(), db)
			if err != nil {
				return err
			}

			if before == after {
				fmt.Fprintf(env.Stdout, "Already up to date (version %d, %s).\n", after, db.Engine())

				return nil
			}

			fmt.Fprintf(env.Stdout, "Migrated %s from version %d to %d.\n", db.Engine(), before, after)

			return nil
		},
	}
}

func newMigrateStatusCmd(env Env, flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show which migrations have been applied",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := openDB(cmd, env, flags)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			statuses, err := store.Status(cmd.Context(), db)
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "ENGINE\t%s\n\n", db.Engine())
			fmt.Fprintln(w, "VERSION\tSTATUS\tSOURCE")

			for _, s := range statuses {
				state := "pending"
				if s.Applied {
					state = "applied"
				}

				fmt.Fprintf(w, "%d\t%s\t%s\n", s.Version, state, filepath.Base(s.Source))
			}

			return w.Flush()
		},
	}
}

func newMigrateVersionCmd(env Env, flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the current schema version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := openDB(cmd, env, flags)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			v, err := store.CurrentVersion(cmd.Context(), db)
			if err != nil {
				return err
			}

			_, err = fmt.Fprintf(env.Stdout, "%d\n", v)

			return err
		},
	}
}

// newMigrateCreateCmd scaffolds a migration for every engine at once.
//
// Creating both files together is deliberate: a migration that exists for only
// one engine is the single most likely way the two schemas drift apart.
func newMigrateCreateCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Scaffold a new migration for every engine",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := sanitizeMigrationName(args[0])
			if name == "" {
				return fmt.Errorf("migration name %q has no usable characters", args[0])
			}

			version := time.Now().UTC().Format("20060102150405")

			for _, engine := range []store.Engine{store.EnginePostgres, store.EngineSQLite} {
				dir := filepath.Join("internal", "store", "migrations", engine.String())
				if err := os.MkdirAll(dir, 0o750); err != nil {
					return fmt.Errorf("create %s: %w", dir, err)
				}

				path := filepath.Join(dir, fmt.Sprintf("%s_%s.sql", version, name))
				if _, err := os.Stat(path); err == nil {
					return fmt.Errorf("%s already exists", path)
				}

				body := fmt.Sprintf(migrationTemplate, engine, name)
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					return fmt.Errorf("write %s: %w", path, err)
				}

				fmt.Fprintf(env.Stdout, "created %s\n", path)
			}

			fmt.Fprintln(env.Stdout,
				"\nBoth files must stay in step — the portability test compares the "+
					"resulting schemas and fails if they diverge.")

			return nil
		},
	}
}

const migrationTemplate = `-- +goose Up
-- %s: %s

-- +goose Down
`

// sanitizeMigrationName reduces a name to lowercase words joined by underscores.
func sanitizeMigrationName(in string) string {
	out := make([]rune, 0, len(in))
	lastUnderscore := true // suppress a leading underscore

	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
			lastUnderscore = false
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
			lastUnderscore = false
		default:
			if !lastUnderscore {
				out = append(out, '_')
				lastUnderscore = true
			}
		}
	}

	// Trim a trailing underscore.
	if n := len(out); n > 0 && out[n-1] == '_' {
		out = out[:n-1]
	}

	return string(out)
}
