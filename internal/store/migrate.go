package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"

	"github.com/pressly/goose/v3"
)

// migrationsFS holds the SQL for every supported engine. Embedding them means
// a released binary carries its own schema — no sidecar files to deploy, which
// is what keeps the single-binary install honest.
//
//go:embed migrations/postgres/*.sql migrations/sqlite/*.sql
var migrationsFS embed.FS

// MigrationsDir returns the embedded directory for an engine.
func MigrationsDir(e Engine) string { return "migrations/" + e.String() }

// MigrationsFS exposes the embedded migrations, for tests and tooling.
func MigrationsFS() fs.FS { return migrationsFS }

// MigrationStatus describes one migration.
type MigrationStatus struct {
	Version int64
	Source  string
	Applied bool
}

// newProvider builds a goose provider bound to an engine's migration set.
func newProvider(db *DB) (*goose.Provider, error) {
	dialect := goose.DialectPostgres
	if db.IsSQLite() {
		dialect = goose.DialectSQLite3
	}

	sub, err := fs.Sub(migrationsFS, MigrationsDir(db.Engine()))
	if err != nil {
		return nil, fmt.Errorf("locate migrations for %s: %w", db.Engine(), err)
	}

	p, err := goose.NewProvider(dialect, db.DB, sub)
	if err != nil {
		return nil, fmt.Errorf("build migration provider: %w", err)
	}

	return p, nil
}

// Migrate applies every pending migration.
//
// It is safe to run repeatedly: goose records applied versions, so a second
// run is a no-op. Migrations are forward-only in production per architectural
// rule 7; Down exists for tests and local iteration only.
func Migrate(ctx context.Context, db *DB, log *slog.Logger) error {
	p, err := newProvider(db)
	if err != nil {
		return err
	}

	results, err := p.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	if len(results) == 0 {
		log.Debug("database schema already current", slog.String("engine", db.Engine().String()))

		return nil
	}

	for _, r := range results {
		log.Info("migration applied",
			slog.Int64("version", r.Source.Version),
			slog.String("source", filepath.Base(r.Source.Path)),
			slog.String("duration", r.Duration.String()),
		)
	}

	log.Info("migrations complete",
		slog.Int("applied", len(results)),
		slog.String("engine", db.Engine().String()),
	)

	return nil
}

// MigrateDown rolls back the most recent migration.
//
// Production is forward-only; this exists so tests and local development can
// exercise the Down blocks, which otherwise rot untested.
func MigrateDown(ctx context.Context, db *DB) error {
	p, err := newProvider(db)
	if err != nil {
		return err
	}

	if _, err := p.Down(ctx); err != nil {
		return fmt.Errorf("roll back migration: %w", err)
	}

	return nil
}

// Status lists every known migration and whether it has been applied.
func Status(ctx context.Context, db *DB) ([]MigrationStatus, error) {
	p, err := newProvider(db)
	if err != nil {
		return nil, err
	}

	sources, err := p.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("read migration status: %w", err)
	}

	out := make([]MigrationStatus, 0, len(sources))

	for _, s := range sources {
		out = append(out, MigrationStatus{
			Version: s.Source.Version,
			Source:  s.Source.Path,
			Applied: s.State == goose.StateApplied,
		})
	}

	return out, nil
}

// CurrentVersion reports the highest applied migration version, or 0 when the
// database is empty.
func CurrentVersion(ctx context.Context, db *DB) (int64, error) {
	p, err := newProvider(db)
	if err != nil {
		return 0, err
	}

	v, err := p.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}

	return v, nil
}
