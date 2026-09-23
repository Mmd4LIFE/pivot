package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/config"

	// Database drivers. Both are pure Go, which is what keeps cross-compilation
	// for six platforms (Part 13) a single build matrix rather than a
	// cross-toolchain problem.
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Engine identifies a metadata database backend.
type Engine string

const (
	EngineSQLite   Engine = "sqlite"
	EnginePostgres Engine = "postgres"
)

// String implements [fmt.Stringer].
func (e Engine) String() string { return string(e) }

// Default pool sizes, chosen per engine.
//
// SQLite allows exactly one writer at a time. A larger pool converts that into
// intermittent "database is locked" failures under write contention, which is
// a worse trade than serializing the small, fast queries a metadata store
// makes. ADR-0003 scopes SQLite to small deployments; PostgreSQL is the answer
// for concurrency.
const (
	DefaultMaxOpenConnsSQLite   = 1
	DefaultMaxIdleConnsSQLite   = 1
	DefaultMaxOpenConnsPostgres = 25
	DefaultMaxIdleConnsPostgres = 5
)

// DB is a metadata database handle that knows which engine it is talking to.
//
// The engine is exposed because a few places legitimately need it — migrations
// pick a dialect, and Part 3-b picks a generated query set. It is not a license
// to scatter engine conditionals through business logic.
type DB struct {
	*sql.DB

	engine Engine
	dsn    string
}

// Engine reports the backend in use.
func (db *DB) Engine() Engine { return db.engine }

// IsSQLite reports whether the backend is SQLite.
func (db *DB) IsSQLite() bool { return db.engine == EngineSQLite }

// IsPostgres reports whether the backend is PostgreSQL.
func (db *DB) IsPostgres() bool { return db.engine == EnginePostgres }

// Open resolves the configured URL, connects, applies pool settings, and
// verifies the connection with a ping.
//
// It does not run migrations; that is [Migrate], so the decision to change
// schema is always explicit.
func Open(ctx context.Context, cfg config.DatabaseConfig, log *slog.Logger) (*DB, error) {
	engine, dsn, err := ParseURL(cfg.URL)
	if err != nil {
		return nil, err
	}

	// Whether the file exists *before* the driver touches it, so a database
	// Pivot creates can be given a sensible mode and one the operator created
	// is left exactly as they left it.
	path, isFile := SQLitePath(cfg.URL)
	_, statErr := os.Stat(path)
	fresh := isFile && errors.Is(statErr, os.ErrNotExist)

	sqlDB, err := sql.Open(driverName(engine), dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s database: %w", engine, err)
	}

	applyPoolSettings(sqlDB, engine, cfg)

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()

		return nil, fmt.Errorf("connect to %s database: %w", engine, redactError(err, dsn))
	}

	// A database Pivot just created is readable by its owner and nobody else.
	//
	// SQLite creates it 0644 minus the umask, which on a default machine
	// leaves every session token hash and every stored secret readable by any
	// account on the box. Only on creation: an existing file's mode is the
	// operator's decision, and `pivot doctor` warns about it rather than
	// changing it underneath them.
	if fresh {
		if cherr := os.Chmod(path, 0o600); cherr != nil {
			log.Warn("could not restrict permissions on the new database file",
				slog.String("path", path), slog.String("error", cherr.Error()))
		}
	}

	log.Info("database connected",
		slog.String("engine", engine.String()),
		slog.Int("max_open_conns", effectiveMaxOpen(engine, cfg)),
	)

	return &DB{DB: sqlDB, engine: engine, dsn: dsn}, nil
}

// driverName maps an engine to its registered database/sql driver.
func driverName(e Engine) string {
	if e == EnginePostgres {
		return "pgx"
	}

	return "sqlite"
}

func effectiveMaxOpen(e Engine, cfg config.DatabaseConfig) int {
	if cfg.MaxOpenConns > 0 {
		return cfg.MaxOpenConns
	}

	if e == EnginePostgres {
		return DefaultMaxOpenConnsPostgres
	}

	return DefaultMaxOpenConnsSQLite
}

func applyPoolSettings(db *sql.DB, e Engine, cfg config.DatabaseConfig) {
	db.SetMaxOpenConns(effectiveMaxOpen(e, cfg))

	idle := cfg.MaxIdleConns
	if idle <= 0 {
		idle = DefaultMaxIdleConnsSQLite
		if e == EnginePostgres {
			idle = DefaultMaxIdleConnsPostgres
		}
	}

	db.SetMaxIdleConns(idle)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime.Duration())
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime.Duration())
}

// ParseURL determines the engine from a configured URL and returns the DSN the
// driver expects.
//
// Accepted forms:
//
//	pivot.db                    SQLite file (no scheme)
//	./data/pivot.db             SQLite file
//	sqlite://data/pivot.db      SQLite file, explicit
//	file:pivot.db               SQLite file, driver-native
//	:memory:                    SQLite, in-memory
//	postgres://...              PostgreSQL
//	postgresql://...            PostgreSQL
func ParseURL(raw string) (Engine, string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", errors.New("database url is empty")
	}

	lower := strings.ToLower(trimmed)

	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		return EnginePostgres, trimmed, nil

	case strings.HasPrefix(lower, "sqlite://"), strings.HasPrefix(lower, "sqlite3://"):
		path := trimmed[strings.Index(trimmed, "://")+len("://"):]
		if path == "" {
			return "", "", fmt.Errorf("sqlite url %q has no path", raw)
		}

		return EngineSQLite, sqliteDSN(path), nil

	case strings.HasPrefix(lower, "file:"):
		// Already driver-native; pass through but still ensure our pragmas.
		return EngineSQLite, withSQLitePragmas(trimmed), nil

	case trimmed == ":memory:":
		return EngineSQLite, sqliteDSN(trimmed), nil

	default:
		// A bare path. This is what makes the zero-config first run work.
		if looksLikeURL(trimmed) {
			return "", "", fmt.Errorf(
				"unsupported database url %q (want a SQLite path, sqlite://, :memory:, or postgres://)",
				raw,
			)
		}

		return EngineSQLite, sqliteDSN(trimmed), nil
	}
}

// looksLikeURL reports whether s has a scheme we did not recognize, so an
// unsupported backend fails loudly instead of being silently treated as a
// SQLite filename.
func looksLikeURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}

	return u.Scheme != "" && strings.Contains(s, "://")
}

// sqliteDSN builds a driver DSN with the pragmas correctness depends on.
func sqliteDSN(path string) string {
	if path == ":memory:" {
		return withSQLitePragmas("file::memory:?cache=shared")
	}

	return withSQLitePragmas("file:" + filepath.Clean(path))
}

// withSQLitePragmas appends the pragmas Pivot requires.
//
// foreign_keys is the important one: SQLite disables foreign key enforcement
// by default, so without it every REFERENCES clause in the schema is decorative
// on SQLite while being enforced on PostgreSQL. That is exactly the kind of
// silent divergence the dual-engine support exists to catch, not to create.
//
// journal_mode=WAL lets readers proceed during a write, and busy_timeout makes
// brief write contention wait rather than fail immediately.
func withSQLitePragmas(dsn string) string {
	pragmas := []string{
		"_pragma=foreign_keys(1)",
		"_pragma=journal_mode(WAL)",
		"_pragma=busy_timeout(5000)",
	}

	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}

	return dsn + sep + strings.Join(pragmas, "&")
}

// redactError strips credentials from a DSN that a driver echoed into an error.
func redactError(err error, dsn string) error {
	msg := err.Error()

	if u, perr := url.Parse(dsn); perr == nil && u.User != nil {
		if pw, ok := u.User.Password(); ok && pw != "" {
			msg = strings.ReplaceAll(msg, pw, "xxxxx")
		}
	}

	return errors.New(msg)
}

// HealthCheck verifies the database answers, for the readiness probe.
func (db *DB) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping: %w", redactError(err, db.dsn))
	}

	return nil
}
