package query

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

/*
The monitor.

A kill is not a new way of stopping a query. It is the existing one -- the
cancellation Part 20-b proved reaches the source -- reached by somebody who is
not the caller. These tests check that the reaching works and that the log can
tell the two apart afterwards.
*/

// A query this process is running can be stopped by somebody who is not its
// caller, and the log says an administrator did it.
func TestAnAdministratorCanStopARunningQuery(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	monitor := NewMonitor()
	f.executor = NewExecutor(f.repos, f.checker,
		withOpener(f.countingOpener()), WithMonitor(monitor))

	ctx := f.as(t, f.analyst)

	ex, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: slowQuery})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if !monitor.Watching(ex.LogID) {
		t.Fatal("a running query is not being watched, so nothing could kill it")
	}

	// Read one row first, so the query is demonstrably under way rather than
	// still being planned -- and so the kill has something to interrupt.
	if !ex.Stream.Next() {
		t.Fatalf("the query produced nothing to interrupt: %v", ex.Stream.Err())
	}

	if !monitor.Kill(ex.LogID) {
		t.Fatal("the monitor did not find the query it is watching")
	}

	// Draining now meets the cancellation.
	for ex.Stream.Next() { //nolint:revive // draining is the point
	}

	_ = ex.Stream.Close()

	entry, err := f.repos.QueryLog.Get(ctx, ex.LogID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if entry.State != repo.StateCanceled {
		t.Errorf("State = %q, want canceled", entry.State)
	}

	// And it says *who*: an administrator's decision, not a closed tab.
	if entry.ErrorMessage != ErrKilled.Error() {
		t.Errorf("ErrorMessage = %q, want %q", entry.ErrorMessage, ErrKilled.Error())
	}
}

// A query that finished is no longer watched, so killing it reports that
// honestly rather than claiming success.
func TestKillingAFinishedQueryFindsNothing(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	monitor := NewMonitor()
	f.executor = NewExecutor(f.repos, f.checker,
		withOpener(f.countingOpener()), WithMonitor(monitor))

	ctx := f.as(t, f.analyst)

	ex, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	drain(t, ex)

	if monitor.Watching(ex.LogID) {
		t.Error("a finished query is still being watched")
	}

	if monitor.Kill(ex.LogID) {
		t.Error("killing a finished query reported success")
	}

	if n := monitor.Running(); n != 0 {
		t.Errorf("the monitor holds %d queries after everything finished", n)
	}
}

// A query nobody has ever heard of is not found, rather than being an error.
func TestKillingAnUnknownQueryFindsNothing(t *testing.T) {
	t.Parallel()

	if NewMonitor().Kill(uuid.New()) {
		t.Error("killing an unknown id reported success")
	}
}

/*
A caller who walks away is not recorded as having been killed.

The distinction is the reason the cause exists. Both arrive as a canceled
context through the same channel, and an operator reading the log needs to tell
"somebody stopped this" from "the browser tab closed" -- they lead to
completely different next questions.
*/
func TestACallerWhoLeavesIsNotRecordedAsKilled(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	f.executor = NewExecutor(f.repos, f.checker,
		withOpener(f.countingOpener()), WithMonitor(NewMonitor()))

	ctx := f.as(t, f.analyst)

	ex, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	drain(t, ex)

	entry, err := f.repos.QueryLog.Get(ctx, ex.LogID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if entry.ErrorMessage == ErrKilled.Error() {
		t.Error("a query that simply finished was recorded as killed")
	}
}

// The monitor is read and written from many goroutines, which is how a kill
// arrives: on some other goroutine entirely.
func TestTheMonitorSurvivesConcurrentUse(t *testing.T) {
	t.Parallel()

	monitor := NewMonitor()

	var wg sync.WaitGroup

	for range 16 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			id := uuid.New()
			done := make(chan struct{})

			unwatch := monitor.watch(id, func(error) { close(done) })

			if !monitor.Kill(id) {
				t.Errorf("a watched query was not found")
			}

			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("the cancel was never called")
			}

			unwatch()
		}()
	}

	wg.Wait()

	if n := monitor.Running(); n != 0 {
		t.Errorf("%d queries are still watched after all were released", n)
	}
}

// A nil monitor is usable, because an executor built without one must still
// run queries rather than panic on the first.
func TestANilMonitorIsHarmless(t *testing.T) {
	t.Parallel()

	var m *Monitor

	if m.Kill(uuid.New()) || m.Running() != 0 || m.Watching(uuid.New()) {
		t.Error("a nil monitor claimed to be doing something")
	}

	release := m.watch(uuid.New(), func(error) {})
	release()
}

/*
slowQuery runs long enough to be killed.

The first version of this test used the fixture's three-row table and failed
about one run in four: SELECT over three rows finishes before a kill issued
microseconds later can land, and the log then honestly said "succeeded". A test
for stopping something has to be given something that is still going.

A recursive CTE rather than a sleep, because SQLite has no sleep -- and this is
a source query, so it must be something the source can actually be asked.
*/
const slowQuery = `WITH RECURSIVE counter(n) AS (
	SELECT 1 UNION ALL SELECT n + 1 FROM counter WHERE n < 40000000
) SELECT n FROM counter`
