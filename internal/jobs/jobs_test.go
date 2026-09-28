package jobs_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/jobs"
	"github.com/Mmd4LIFE/pivot/internal/store"
)

/*
The background job runner, on both engines.

Run against real databases rather than a fake, because what is being checked is
not "does the code call River" but the three things River is here to provide:
that scheduled work happens, that two processes do not do it twice, and that a
failure is recoverable rather than silent. None of those can be observed
through a mock.

SQLite always runs. PostgreSQL runs when its URL is set -- and matters, because
riversqlite is the newer of the two drivers and its own documentation calls it
early: a property that holds on one engine and not the other is exactly what
this is here to catch.
*/

// --- the databases these run against -----------------------------------------

type engine struct {
	name string
	open func(t *testing.T) *store.DB
}

func engines(t *testing.T) []engine {
	t.Helper()

	out := []engine{{
		name: "sqlite",
		open: func(t *testing.T) *store.DB {
			path := filepath.Join(t.TempDir(), "pivot.db")

			return openStore(t, "sqlite://"+path)
		},
	}}

	if url := os.Getenv("PIVOT_TEST_POSTGRES_URL"); url != "" {
		out = append(out, engine{
			name: "postgres",
			open: func(t *testing.T) *store.DB { return openStore(t, url) },
		})
	}

	return out
}

func openStore(t *testing.T, url string) *store.DB {
	t.Helper()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	db, err := store.Open(t.Context(), config.DatabaseConfig{URL: url}, log)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	if _, err = jobs.Migrate(t.Context(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	/*
		An empty queue to start from.

		SQLite gets a fresh file per test and needs none of this. PostgreSQL
		does not: every test in this file shares one database, and without this
		a job discarded by an earlier test is still sitting there when a later
		one asks "did anything get discarded" -- which is exactly the false
		failure that sent me looking for an engine difference that was not
		there.
	*/
	if _, err = db.ExecContext(t.Context(), "DELETE FROM river_job"); err != nil {
		t.Fatalf("clear the queue: %v", err)
	}

	return db
}

// --- a job that counts, and one that fails -----------------------------------

type countArgs struct {
	Note string `json:"note"`
}

func (countArgs) Kind() string { return "test.count" }

type countWorker struct {
	river.WorkerDefaults[countArgs]

	ran *atomic.Int64
}

func (w *countWorker) Work(context.Context, *river.Job[countArgs]) error {
	w.ran.Add(1)

	return nil
}

type failArgs struct{}

func (failArgs) Kind() string { return "test.fail" }

type failWorker struct {
	river.WorkerDefaults[failArgs]

	attempts *atomic.Int64
}

func (w *failWorker) Work(context.Context, *river.Job[failArgs]) error {
	w.attempts.Add(1)

	return errors.New("the warehouse is unreachable")
}

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// --- scheduled work happens ---------------------------------------------------

/*
A periodic job runs without anybody asking.

The property the whole part exists for. Checked on a one-second interval
rather than the catalog's fifteen minutes, because what is being tested is that
the scheduler runs at all -- the interval is a constant somewhere else.
*/
func TestAPeriodicJobRunsWithoutAnybodyAsking(t *testing.T) {
	for _, e := range engines(t) {
		t.Run(e.name, func(t *testing.T) {
			db := e.open(t)

			var ran atomic.Int64

			workers := river.NewWorkers()
			river.AddWorker(workers, &countWorker{ran: &ran})

			runner, err := jobs.New(db, jobs.Options{
				Workers: workers,
				Periodic: []*river.PeriodicJob{
					river.NewPeriodicJob(
						river.PeriodicInterval(time.Second),
						func() (river.JobArgs, *river.InsertOpts) {
							return countArgs{Note: "periodic"}, nil
						},
						&river.PeriodicJobOpts{RunOnStart: true},
					),
				},
				Logger: quiet(),
			})
			if err != nil {
				t.Fatalf("new: %v", err)
			}

			if err = runner.Start(t.Context()); err != nil {
				t.Fatalf("start: %v", err)
			}

			defer stop(t, runner)

			waitFor(t, &ran, 1, 20*time.Second, "the periodic job never ran")
		})
	}
}

/*
Two Pivots against one database do not both do the work.

Proven by running two, which is the only way to prove it. River elects a leader
across every process sharing the metadata database and only the leader inserts
periodic jobs; without that, every instance in a deployment would sync every
warehouse on every interval, and the load would scale with the number of
replicas rather than the number of connections.

The interval is long enough that the count is the leader's doing rather than
the race being too fast to see: RunOnStart fires once per leader, and two
leaders would show as two.
*/
func TestTwoRunnersDoNotBothScheduleTheSameWork(t *testing.T) {
	for _, e := range engines(t) {
		t.Run(e.name, func(t *testing.T) {
			db := e.open(t)

			var ran atomic.Int64

			periodic := []*river.PeriodicJob{
				river.NewPeriodicJob(
					river.PeriodicInterval(time.Hour),
					func() (river.JobArgs, *river.InsertOpts) {
						return countArgs{Note: "once"}, nil
					},
					&river.PeriodicJobOpts{RunOnStart: true},
				),
			}

			// Two processes, as far as River is concerned: separate clients,
			// separate pools, one database.
			running := make([]*jobs.Runner, 0, 2)

			defer func() {
				for _, runner := range running {
					stop(t, runner)
				}
			}()

			for i := range 2 {
				workers := river.NewWorkers()
				river.AddWorker(workers, &countWorker{ran: &ran})

				runner, err := jobs.New(db, jobs.Options{
					Workers: workers, Periodic: periodic, Logger: quiet(),
				})
				if err != nil {
					t.Fatalf("runner %d: %v", i, err)
				}

				running = append(running, runner)

				if err = runner.Start(t.Context()); err != nil {
					t.Fatalf("runner %d start: %v", i, err)
				}
			}

			waitFor(t, &ran, 1, 20*time.Second, "neither runner scheduled the work")

			// And then stays at one. Long enough for a second leader to have
			// shown itself, short enough not to pad the suite.
			time.Sleep(3 * time.Second)

			if got := ran.Load(); got != 1 {
				t.Errorf("the work ran %d times with two runners up, want 1 -- "+
					"both processes are scheduling", got)
			}
		})
	}
}

// --- failure is visible --------------------------------------------------------

/*
A job that fails is retried, and then recorded rather than forgotten.

The failure mode of every job system is that work stops and nothing says so.
River retries with backoff and files what is left as discarded, carrying the
error -- which is what `pivot admin jobs --failed` reads.
*/
func TestAFailedJobIsRetriedAndThenRecorded(t *testing.T) {
	for _, e := range engines(t) {
		t.Run(e.name, func(t *testing.T) {
			db := e.open(t)

			var attempts atomic.Int64

			workers := river.NewWorkers()
			river.AddWorker(workers, &failWorker{attempts: &attempts})

			runner, err := jobs.New(db, jobs.Options{Workers: workers, Logger: quiet()})
			if err != nil {
				t.Fatalf("new: %v", err)
			}

			if err = runner.Start(t.Context()); err != nil {
				t.Fatalf("start: %v", err)
			}

			defer stop(t, runner)

			// One attempt, so the test does not wait out River's backoff.
			if _, err = runner.Client().Insert(t.Context(), failArgs{},
				&river.InsertOpts{MaxAttempts: 1}); err != nil {
				t.Fatalf("insert: %v", err)
			}

			waitFor(t, &attempts, 1, 20*time.Second, "the failing job never ran")

			// It is findable afterwards, with the reason attached.
			deadline := time.Now().Add(20 * time.Second)

			for {
				list, lerr := runner.Client().JobList(t.Context(),
					river.NewJobListParams().
						States(rivertype.JobStateDiscarded).
						First(10))
				if lerr != nil {
					t.Fatalf("list: %v", lerr)
				}

				if len(list.Jobs) > 0 {
					job := list.Jobs[0]

					if len(job.Errors) == 0 {
						t.Fatal("a discarded job carries no error, so nothing can say why")
					}

					if !strings.Contains(job.Errors[0].Error, "warehouse is unreachable") {
						t.Errorf("the recorded error is %q, want the worker's own",
							job.Errors[0].Error)
					}

					return
				}

				if time.Now().After(deadline) {
					t.Fatal("the failed job was never recorded as discarded; " +
						"a failure nobody can find is a failure nobody fixes")
				}

				time.Sleep(200 * time.Millisecond)
			}
		})
	}
}

/*
A process that does not know a job's kind does not destroy it.

The rolling-upgrade question. A new version enqueues work an old process has
never heard of, and what the old one does with it decides whether the upgrade
is safe.

Two things were measured here, and the first corrected an assumption. River
**refuses to insert** a kind the inserting client has no worker for -- a
client-side guard, so a typo cannot put an unrunnable job in the queue. What it
does *not* guard is the other direction: a process can fetch a job whose kind it
lacks, and it fails that attempt with "unhandled job kind".

Which is the right behavior, and the reason this test asserts the job survives
rather than that it is never touched: the attempt is spent, the job goes back to
retryable, and the next process that understands it runs it. Nothing is lost.
*/
func TestAProcessThatDoesNotKnowAKindDoesNotDestroyIt(t *testing.T) {
	for _, e := range engines(t) {
		t.Run(e.name, func(t *testing.T) {
			db := e.open(t)

			// The process that knows the kind, used only to enqueue.
			var ignored atomic.Int64

			knowing := river.NewWorkers()
			river.AddWorker(knowing, &failWorker{attempts: &ignored})

			inserter, err := jobs.New(db, jobs.Options{Workers: knowing, Logger: quiet()})
			if err != nil {
				t.Fatalf("inserter: %v", err)
			}

			// Never started: it may not run what it enqueues.
			if _, err = inserter.Client().Insert(t.Context(), failArgs{},
				&river.InsertOpts{MaxAttempts: 5}); err != nil {
				t.Fatalf("insert: %v", err)
			}

			stop(t, inserter)

			// The old process, which has never heard of test.fail.
			var ran atomic.Int64

			unknowing := river.NewWorkers()
			river.AddWorker(unknowing, &countWorker{ran: &ran})

			runner, err := jobs.New(db, jobs.Options{Workers: unknowing, Logger: quiet()})
			if err != nil {
				t.Fatalf("runner: %v", err)
			}

			if err = runner.Start(t.Context()); err != nil {
				t.Fatalf("start: %v", err)
			}

			defer stop(t, runner)

			time.Sleep(3 * time.Second)

			discarded, err := runner.Client().JobList(t.Context(),
				river.NewJobListParams().States(rivertype.JobStateDiscarded).First(10))
			if err != nil {
				t.Fatalf("list: %v", err)
			}

			if len(discarded.Jobs) != 0 {
				t.Errorf("a process without the worker discarded the job outright, "+
					"so a rolling upgrade would lose it: %s", discarded.Jobs[0].Kind)
			}

			// And it is still there, waiting for a process that understands it.
			alive, err := runner.Client().JobList(t.Context(),
				river.NewJobListParams().
					States(rivertype.JobStateAvailable, rivertype.JobStateRetryable,
						rivertype.JobStateScheduled).
					First(10))
			if err != nil {
				t.Fatalf("list: %v", err)
			}

			if len(alive.Jobs) == 0 {
				t.Fatal("the job is neither discarded nor waiting; it has vanished")
			}
		})
	}
}

// River will not enqueue work nothing can run, which turns a typo in a job
// kind into an error at the call site rather than a row that sits in the queue
// forever.
func TestInsertingAnUnregisteredKindIsRefused(t *testing.T) {
	t.Parallel()

	db := engines(t)[0].open(t)

	var ran atomic.Int64

	workers := river.NewWorkers()
	river.AddWorker(workers, &countWorker{ran: &ran})

	runner, err := jobs.New(db, jobs.Options{Workers: workers, Logger: quiet()})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	defer stop(t, runner)

	_, err = runner.Client().Insert(t.Context(), failArgs{}, nil)
	if err == nil {
		t.Fatal("a job kind with no worker was accepted")
	}

	if !strings.Contains(err.Error(), "test.fail") {
		t.Errorf("error = %v, want it to name the kind", err)
	}
}

// Migrating twice is not an error, because `pivot migrate up` is run more than
// once and a second run must be a no-op rather than a failure.
func TestMigratingTwiceIsANoOp(t *testing.T) {
	for _, e := range engines(t) {
		t.Run(e.name, func(t *testing.T) {
			db := e.open(t)

			applied, err := jobs.Migrate(t.Context(), db)
			if err != nil {
				t.Fatalf("second migrate: %v", err)
			}

			if applied != 0 {
				t.Errorf("a second migration applied %d versions, want 0", applied)
			}
		})
	}
}

// --- helpers -------------------------------------------------------------------

func stop(t *testing.T, runner *jobs.Runner) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := runner.Stop(ctx); err != nil {
		t.Errorf("stop: %v", err)
	}
}

func waitFor(t *testing.T, counter *atomic.Int64, want int64, within time.Duration, complaint string) {
	t.Helper()

	deadline := time.Now().Add(within)

	for counter.Load() < want {
		if time.Now().After(deadline) {
			t.Fatalf("%s (reached %d of %d in %s)", complaint, counter.Load(), want, within)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// --- the refusals ---------------------------------------------------------------

/*
A runner with no workers is refused rather than built.

River's own client would accept it and quietly execute nothing, which is the
failure this whole part exists to prevent: work that is not happening and
nothing saying so. A nil bundle is a wiring mistake, and the place to report a
wiring mistake is where it was made.
*/
func TestARunnerWithoutWorkersIsRefused(t *testing.T) {
	t.Parallel()

	db := engines(t)[0].open(t)

	_, err := jobs.New(db, jobs.Options{Logger: quiet()})
	if err == nil {
		t.Fatal("a runner with no workers was built")
	}

	if !strings.Contains(err.Error(), "workers") {
		t.Errorf("error = %v, want it to say what is missing", err)
	}
}

// A runner with no logger takes the default rather than panicking, because a
// caller wiring one up in a test or a script should not have to supply one.
func TestARunnerWithoutALoggerStillWorks(t *testing.T) {
	t.Parallel()

	db := engines(t)[0].open(t)

	var ran atomic.Int64

	workers := river.NewWorkers()
	river.AddWorker(workers, &countWorker{ran: &ran})

	runner, err := jobs.New(db, jobs.Options{Workers: workers})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	stop(t, runner)
}

/*
Stopping twice does not panic, and says so the second time.

A shutdown path runs under a signal handler and a defer, and the two have met
before. Whatever this does, it must not be a panic during shutdown -- which is
how a clean drain becomes a crash in the logs.
*/
func TestStoppingTwiceIsSafe(t *testing.T) {
	t.Parallel()

	db := engines(t)[0].open(t)

	workers := river.NewWorkers()
	river.AddWorker(workers, &countWorker{ran: new(atomic.Int64)})

	runner, err := jobs.New(db, jobs.Options{Workers: workers, Logger: quiet()})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	if err = runner.Start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err = runner.Stop(ctx); err != nil {
		t.Fatalf("first stop: %v", err)
	}

	// The second is expected to complain about the closed pool rather than
	// to crash. Either outcome is fine; panicking is not.
	_ = runner.Stop(ctx)
}

/*
The runner's pool is independent of the store's.

Found by asserting the opposite and being wrong: closing the store does not
close the runner's connections, because OpenSibling dials its own from the
same DSN rather than sharing a handle.

Worth a test now that it is known, because two things depend on it. The runner
closes its own pool in Stop -- if it shared the store's, that would take the
application's database with it. And `pivot admin jobs` opens a runner over a
store it also closes on the way out, in whichever order the defers happen to
run.
*/
func TestTheRunnersPoolIsIndependentOfTheStores(t *testing.T) {
	t.Parallel()

	db := engines(t)[0].open(t)

	workers := river.NewWorkers()
	river.AddWorker(workers, &countWorker{ran: new(atomic.Int64)})

	runner, err := jobs.New(db, jobs.Options{Workers: workers, Logger: quiet()})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	if err = db.Close(); err != nil {
		t.Fatalf("close the store: %v", err)
	}

	// The runner can still reach the database.
	if _, err = runner.Client().JobList(t.Context(), river.NewJobListParams().First(1)); err != nil {
		t.Errorf("the runner lost its connection when the store closed: %v", err)
	}

	stop(t, runner)
}

/*
Starting against a database River has not migrated fails, and says so.

The regression test for a bug this part shipped and the CLI suite caught:
`pivot serve` started the runner on a database where only Pivot's own
migrations had run, and River's tables did not exist. Worse, the failure was
fatal -- an instance refusing to serve because a *background* feature could not
start.

Both halves are fixed elsewhere: `serve` auto-migrates River's schema too, and
treats a runner that will not start as a warning. What is pinned here is that
the failure is an error at all, rather than a runner that starts and silently
does nothing.
*/
func TestStartingWithoutRiversTablesFails(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Pivot's migrations only: jobs.Migrate is deliberately not run.
	db, err := store.Open(t.Context(), config.DatabaseConfig{
		URL: "sqlite://" + filepath.Join(t.TempDir(), "pivot.db"),
	}, log)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer func() { _ = db.Close() }()

	if merr := store.Migrate(t.Context(), db, log); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, &countWorker{ran: new(atomic.Int64)})

	runner, err := jobs.New(db, jobs.Options{Workers: workers, Logger: quiet()})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = runner.Stop(ctx)
	}()

	if err = runner.Start(t.Context()); err == nil {
		t.Fatal("the runner started against a database with no river tables, " +
			"so nothing would have said the schedule was dead")
	}

	if !strings.Contains(err.Error(), "jobs:") {
		t.Errorf("error = %v, want it attributed to the job runner", err)
	}
}

/*
A worker count River will not accept is refused at construction.

River caps concurrency per queue, and a configuration over that cap is a
misconfiguration rather than a request to be clamped. Refusing it at
construction means the operator hears about it when the process starts, not
when the queue quietly runs at a different concurrency than the one written
down.

The pool opened for the client is closed on the way out of that failure, which
is the part worth a test: a constructor that returns an error *and* a live
connection is a leak nobody is holding a handle to.
*/
func TestAnImpossibleWorkerCountIsRefused(t *testing.T) {
	t.Parallel()

	db := engines(t)[0].open(t)

	workers := river.NewWorkers()
	river.AddWorker(workers, &countWorker{ran: new(atomic.Int64)})

	_, err := jobs.New(db, jobs.Options{
		Workers:    workers,
		MaxWorkers: 1_000_000,
		Logger:     quiet(),
	})

	if err == nil {
		t.Fatal("a million workers per queue was accepted")
	}

	if !strings.Contains(err.Error(), "jobs:") {
		t.Errorf("error = %v, want it attributed to the job runner", err)
	}
}

/*
Migrating a database that cannot be written fails, and says so.

The realistic shape of this is a SQLite file somebody made read-only, or one on
a volume mounted read-only -- which is a thing people do to a database they
believe is a backup. River's migration creates tables, so it is the first thing
to notice, and it has to notice rather than start a runner against a schema
that was never applied.
*/
func TestMigratingAnUnwritableDatabaseFails(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("running as root, which can write a file with no write permission")
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	dir := t.TempDir()
	path := filepath.Join(dir, "pivot.db")

	db, err := store.Open(t.Context(), config.DatabaseConfig{URL: "sqlite://" + path}, log)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer func() { _ = db.Close() }()

	// The directory as well as the file: SQLite writes a journal beside it, so
	// a writable directory would let it proceed.
	if err = os.Chmod(path, 0o400); err != nil {
		t.Fatalf("chmod the file: %v", err)
	}

	if err = os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod the directory: %v", err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if _, err = jobs.Migrate(t.Context(), db); err == nil {
		t.Error("migrating a read-only database succeeded")
	}
}
