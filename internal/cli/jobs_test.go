package cli_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/jobs"
	"github.com/Mmd4LIFE/pivot/internal/store"
)

/*
`pivot admin jobs`.

The command exists because background work fails quietly: nobody is waiting for
it, so a broken catalog sync looks exactly like a schema that has not changed.
Which means an untested version of it is worse than none -- somebody would look
here, see nothing, and conclude everything was fine.

So both halves are checked: that an empty queue says something useful rather
than printing an empty table, and that a job which really failed shows up with
the reason attached.
*/

func TestJobsSaysSoWhenThereAreNone(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)

	stdout, _, err := run(t, env, "admin", "jobs")
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}

	// Not an empty table. The useful thing to say is that nothing schedules
	// work unless a process is serving.
	for _, want := range []string{"No jobs yet", "serving"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want it to mention %q", stdout, want)
		}
	}
}

func TestJobsSaysSoWhenNoneHaveFailed(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)

	stdout, _, err := run(t, env, "admin", "jobs", "--failed")
	if err != nil {
		t.Fatalf("jobs --failed: %v", err)
	}

	if !strings.Contains(stdout, "No failed jobs") {
		t.Errorf("stdout = %q", stdout)
	}
}

/*
A job that really failed is listed, with the reason.

Caused rather than faked: a worker that returns an error, run by a real runner
against the same database the command reads. A test that inserted a row saying
"discarded" would pass against a command that cannot read River at all.
*/
func TestJobsShowsAFailureAndWhy(t *testing.T) {
	t.Parallel()

	_, env := withOrg(t)

	failOnce(t, env["PIVOT_DATABASE_URL"])

	stdout, _, err := run(t, env, "admin", "jobs")
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}

	for _, want := range []string{"test.doomed", "discarded", "the warehouse said no"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout does not mention %q:\n%s", want, stdout)
		}
	}

	// And --failed narrows to it rather than hiding it.
	narrowed, _, err := run(t, env, "admin", "jobs", "--failed")
	if err != nil {
		t.Fatalf("jobs --failed: %v", err)
	}

	if !strings.Contains(narrowed, "test.doomed") {
		t.Errorf("--failed does not show the failure:\n%s", narrowed)
	}
}

// --- a job that fails, run for real ------------------------------------------

type doomedArgs struct{}

func (doomedArgs) Kind() string { return "test.doomed" }

type doomedWorker struct {
	river.WorkerDefaults[doomedArgs]

	ran *atomic.Int64
}

func (w *doomedWorker) Work(context.Context, *river.Job[doomedArgs]) error {
	w.ran.Add(1)

	return errors.New("the warehouse said no")
}

// failOnce runs one job that fails, against the database the CLI will read.
func failOnce(t *testing.T, url string) {
	t.Helper()

	quiet := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	db, err := store.Open(t.Context(), config.DatabaseConfig{URL: url}, quiet)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer func() { _ = db.Close() }()

	var ran atomic.Int64

	workers := river.NewWorkers()
	river.AddWorker(workers, &doomedWorker{ran: &ran})

	runner, err := jobs.New(db, jobs.Options{Workers: workers, Logger: quiet})
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	if err = runner.Start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		if serr := runner.Stop(ctx); serr != nil {
			t.Errorf("stop: %v", serr)
		}
	}()

	// One attempt, so it is discarded rather than retried for the length of
	// this test.
	if _, err = runner.Client().Insert(t.Context(), doomedArgs{},
		&river.InsertOpts{MaxAttempts: 1}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)

	for ran.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the failing job never ran")
		}

		time.Sleep(100 * time.Millisecond)
	}

	// The discard is written after Work returns, so give it a moment to land
	// before the command reads it.
	time.Sleep(500 * time.Millisecond)
}
