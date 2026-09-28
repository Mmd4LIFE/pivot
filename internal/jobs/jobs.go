/*
Package jobs is Pivot's background work: the things that have to happen without
anybody asking.

# Why River, and why it works on SQLite

ADR-0007 chose River and rejected a homegrown queue, on the grounds that
visibility timeouts, poison messages and leader election are exactly the
failure modes that are subtle and expensive to get wrong. That reasoning holds.

What ADR-0007 never confronted is ADR-0003: SQLite is the zero-config default,
and River is described everywhere as a *Postgres* job queue. Taken at face
value that means the quickstart path silently loses catalog syncs, alerts and
flows -- two products wearing one name.

It turns out not to be so. River publishes a `riversqlite` driver that takes a
plain *sql.DB, which is what modernc.org/sqlite gives us, and it was measured
working end to end on both engines -- ordinary jobs and the periodic scheduler
-- before any of this was written. ADR-0011 records the measurement and the
caveats.

# Why the runner gets its own pool

On SQLite the store's pool is one connection by design, and a job runner
polling on it would sit between every request and the database. So the runner
opens its own, and the store's [store.DB.OpenSibling] exists for that.

# What is deliberately not here

Transactional enqueueing, which ADR-0007 called the decisive feature: a job
inserted in the same transaction as the change that triggered it. River
supports it through InsertTx, and nothing yet has a transaction to join --
the catalog sync is scheduled rather than triggered. The first caller that
needs it gets it; building the plumbing now would be guessing at its shape.
*/
package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/riverdriver/riversqlite"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/Mmd4LIFE/pivot/internal/store"
)

// Pool sizes for the runner's own connections.
//
// One on SQLite because the riversqlite driver's own documentation says to:
// River runs internal operations in parallel and SQLite allows one at a time,
// so anything more produces SQLITE_BUSY rather than throughput. Four on
// PostgreSQL, which is enough for the poller, the leader election and a couple
// of workers without taking a meaningful share of a 25-connection pool.
const (
	runnerConnsSQLite   = 1
	runnerConnsPostgres = 4
)

// Runner owns the background job client and the pool underneath it.
type Runner struct {
	client *river.Client[*sql.Tx]
	pool   *sql.DB
	log    *slog.Logger
}

// Options configures a runner.
type Options struct {
	// Workers is what this process can execute. A job whose kind is not
	// registered stays queued rather than failing, which is what lets a
	// rolling upgrade introduce a worker before the job that needs it.
	Workers *river.Workers

	// Periodic is the work that happens on a timer. River elects a leader
	// across every process sharing the database, and only the leader inserts
	// these -- which is what keeps two Pivots from both syncing the same
	// connection.
	Periodic []*river.PeriodicJob

	// MaxWorkers caps concurrent execution in this process.
	MaxWorkers int

	Logger *slog.Logger
}

/*
New builds a runner over the metadata database.

It does not start it: [Runner.Start] does, so that a caller can construct the
runner, decide the process is not going to serve, and close it again without
having briefly claimed leadership.
*/
func New(db *store.DB, opts Options) (*Runner, error) {
	if opts.Workers == nil {
		return nil, errors.New("jobs: a runner needs workers, even if none are registered")
	}

	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	pool, err := db.OpenSibling()
	if err != nil {
		return nil, err
	}

	conns := runnerConnsPostgres
	if db.IsSQLite() {
		conns = runnerConnsSQLite
	}

	pool.SetMaxOpenConns(conns)
	pool.SetMaxIdleConns(conns)

	maxWorkers := opts.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = conns
	}

	client, err := river.NewClient(driverFor(db, pool), &river.Config{
		Queues:       map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: maxWorkers}},
		Workers:      opts.Workers,
		PeriodicJobs: opts.Periodic,
		Logger:       log,
	})
	if err != nil {
		closePool(pool, log)

		return nil, fmt.Errorf("jobs: build the client: %w", err)
	}

	return &Runner{client: client, pool: pool, log: log}, nil
}

// driverFor picks River's driver for the engine in use.
//
// riverdatabasesql rather than riverpgxv5 for PostgreSQL: pgx's own driver
// adds LISTEN/NOTIFY, and taking it would mean a second connection type and a
// second code path for a latency improvement on work that runs on a timer.
func driverFor(db *store.DB, pool *sql.DB) riverdriver.Driver[*sql.Tx] {
	if db.IsSQLite() {
		return riversqlite.New(pool)
	}

	return riverdatabasesql.New(pool)
}

// Start begins polling and, if this process wins the election, scheduling.
func (r *Runner) Start(ctx context.Context) error {
	if err := r.client.Start(ctx); err != nil {
		return fmt.Errorf("jobs: start: %w", err)
	}

	return nil
}

/*
Stop drains and closes.

Given a context with a deadline, River lets running jobs finish and then
returns; jobs still running when it expires are left in the queue for another
process, or for this one after a restart. That is the behavior worth having:
a job killed mid-flight is one that has to be safe to run twice, and
guaranteeing that for every job ever written is not something a shutdown path
can promise on their behalf.
*/
func (r *Runner) Stop(ctx context.Context) error {
	err := r.client.Stop(ctx)

	closePool(r.pool, r.log)

	if err != nil {
		return fmt.Errorf("jobs: stop: %w", err)
	}

	return nil
}

// Client exposes River for inserting work.
func (r *Runner) Client() *river.Client[*sql.Tx] { return r.client }

/*
Migrate brings River's own tables up to date.

Separate from Pivot's goose migrations because they are not Pivot's schema:
River owns five `river_*` tables, versions them itself, and changes them on its
own release cadence. Copying them into internal/store/migrations would mean
hand-porting somebody else's schema to two dialects and re-doing it on every
upgrade.

Run from `pivot migrate up`, so the decision to change schema stays explicit --
the same reason [store.Migrate] is not called by Open.
*/
func Migrate(ctx context.Context, db *store.DB) (int, error) {
	pool, err := db.OpenSibling()
	if err != nil {
		return 0, err
	}

	defer closePool(pool, slog.Default())

	if db.IsSQLite() {
		pool.SetMaxOpenConns(1)
	}

	migrator, err := rivermigrate.New(driverFor(db, pool), nil)
	if err != nil {
		return 0, fmt.Errorf("jobs: build the migrator: %w", err)
	}

	result, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return 0, fmt.Errorf("jobs: migrate: %w", err)
	}

	return len(result.Versions), nil
}

func closePool(pool *sql.DB, log *slog.Logger) {
	if err := pool.Close(); err != nil {
		log.Warn("could not close the job runner's connection pool", "error", err)
	}
}

// DefaultStopTimeout is how long a shutdown waits for running jobs.
const DefaultStopTimeout = 15 * time.Second
