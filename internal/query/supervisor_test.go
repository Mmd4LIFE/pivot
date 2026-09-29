package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

/*
A kill that crosses a process boundary.

The test is built so that it cannot accidentally prove the easy thing. The
killer and the owner are two separate Executors with two separate Monitors and
two different owner tokens, and they share no pointer -- the *only* thing
passing between them is a row in the database, which is exactly what passes
between two Pivots behind a load balancer.

`pivot admin` is already a different OS process from `pivot serve`, so this is
not a variant of the kill path. It is the only one the CLI has.
*/
func TestAKillIssuedElsewhereReachesTheQuery(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)

	// The owner: the instance actually running the query.
	ownerToken := NewOwner()
	ownerMonitor := NewMonitor()
	owning := NewExecutor(f.repos, f.checker,
		withOpener(f.countingOpener()),
		WithMonitor(ownerMonitor), WithOwner(ownerToken))

	// Its supervisor, on a sibling pool as it would be in production.
	sibling, err := f.db.SiblingStore()
	if err != nil {
		t.Fatalf("sibling store: %v", err)
	}

	t.Cleanup(func() { _ = sibling.Close() })

	siblingRepos := repo.New(sibling, repo.WithSecrets(testCipher(t)))

	supervisor := NewSupervisor(ownerToken, ownerMonitor, siblingRepos.System(), discardLogger(),
		WithIntervals(20*time.Millisecond, time.Hour))

	ctx := f.as(t, f.analyst)

	supervised, cancelSupervisor := context.WithCancel(t.Context())
	defer cancelSupervisor()

	go supervisor.Run(supervised)

	ex, err := owning.Execute(ctx, Request{ConnectionID: f.connID, SQL: slowQuery})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if !ex.Stream.Next() {
		t.Fatalf("the query produced nothing to interrupt: %v", ex.Stream.Err())
	}

	/*
		The killer: a different "instance" entirely. Its own monitor knows
		nothing about this query, which is the point -- if it could kill it
		directly, this test would prove nothing about crossing a boundary.
	*/
	killerMonitor := NewMonitor()
	if killerMonitor.Kill(ex.LogID) {
		t.Fatal("the other instance could kill the query directly; this proves nothing")
	}

	// All it can do is write the row.
	if cerr := f.repos.QueryLog.RequestCancel(ctx, ex.LogID); cerr != nil {
		t.Fatalf("request the cancel: %v", cerr)
	}

	// And the owner's supervisor delivers it.
	deadline := time.Now().Add(5 * time.Second)
	for ownerMonitor.Watching(ex.LogID) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	for ex.Stream.Next() { //nolint:revive // draining is the point
	}

	_ = ex.Stream.Close()

	entry, err := f.repos.QueryLog.Get(ctx, ex.LogID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if entry.State != repo.StateCanceled {
		t.Fatalf("State = %q, want canceled; the kill did not cross the boundary", entry.State)
	}

	if entry.ErrorMessage != ErrKilled.Error() {
		t.Errorf("ErrorMessage = %q, want %q", entry.ErrorMessage, ErrKilled.Error())
	}

	// And the log records who asked, which is what an audit of a stopped
	// query needs.
	if !entry.CancelRequestedBy.Valid || entry.CancelRequestedBy.UUID != f.analyst {
		t.Errorf("CancelRequestedBy = %v, want the analyst", entry.CancelRequestedBy)
	}
}

// Canceling a query in another tenant is not possible, whoever asks.
func TestAKillCannotCrossATenant(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	f.executor = NewExecutor(f.repos, f.checker,
		withOpener(f.countingOpener()), WithMonitor(NewMonitor()), WithOwner(NewOwner()))

	ctx := f.as(t, f.analyst)

	ex, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	defer func() { _ = ex.Stream.Close() }()

	other := tenant.WithScope(t.Context(),
		tenant.MustNewScope(uuid.New(), uuid.NullUUID{UUID: f.analyst, Valid: true}))

	if err := f.repos.QueryLog.RequestCancel(other, ex.LogID); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound; a kill reached another tenant", err)
	}
}

// Canceling something that is not running is not found, rather than silently
// writing nothing.
func TestCancelingAFinishedQueryIsNotFound(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	ctx := f.as(t, f.analyst)

	ex, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	drain(t, ex)

	if err := f.repos.QueryLog.RequestCancel(ctx, ex.LogID); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}

	if err := f.repos.QueryLog.RequestCancel(ctx, uuid.New()); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("canceling an unknown id: err = %v, want ErrNotFound", err)
	}
}

/*
A heartbeat is what separates a running query from an abandoned one.

The comparison is made in the database's clock on both sides -- the heartbeat
is written with now() rather than Go's time -- so an instance whose clock is
wrong can neither declare itself alive nor be declared dead by somebody else's
disagreement.
*/
func TestAHeartbeatMarksAQueryAsStillOwned(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	owner := NewOwner()
	monitor := NewMonitor()
	f.executor = NewExecutor(f.repos, f.checker,
		withOpener(f.countingOpener()), WithMonitor(monitor), WithOwner(owner))

	ctx := f.as(t, f.analyst)

	ex, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: slowQuery})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	defer func() { _ = ex.Stream.Close() }()

	before, err := f.repos.QueryLog.Get(ctx, ex.LogID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if before.HeartbeatAt.Valid {
		t.Error("a query has a heartbeat before anything beat for it")
	}

	n, err := f.repos.System().Heartbeat(t.Context(), owner)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	if n != 1 {
		t.Errorf("the heartbeat marked %d queries, want 1", n)
	}

	after, err := f.repos.QueryLog.Get(ctx, ex.LogID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if !after.HeartbeatAt.Valid {
		t.Fatal("the query has no heartbeat after one was recorded")
	}

	// And it does not touch another process's queries.
	if n, err := f.repos.System().Heartbeat(t.Context(), NewOwner()); err != nil || n != 0 {
		t.Errorf("a heartbeat for a different owner marked %d queries (err %v)", n, err)
	}
}

// An empty owner matches every unclaimed row, so it is refused rather than
// being allowed to beat for or cancel work it does not own.
func TestAnEmptyOwnerClaimsNothing(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)

	n, err := f.repos.System().Heartbeat(t.Context(), "")
	if err != nil || n != 0 {
		t.Errorf("an empty owner beat for %d queries (err %v)", n, err)
	}

	ids, err := f.repos.System().CancelRequestedFor(t.Context(), "")
	if err != nil || len(ids) != 0 {
		t.Errorf("an empty owner claimed %d kill requests (err %v)", len(ids), err)
	}
}

// Staleness is a judgement about time, so it is tested as one rather than by
// waiting thirty seconds.
func TestStalenessIsAboutTheLastHeartbeat(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	started := now.Add(-time.Hour)

	cases := map[string]struct {
		heartbeat time.Time
		want      bool
	}{
		"beating":                             {now.Add(-time.Second), false},
		"quiet for longer than the threshold": {now.Add(-StaleAfter - time.Second), true},
		"never beat, started long ago":        {time.Time{}, true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := IsStale(tc.heartbeat, started, now); got != tc.want {
				t.Errorf("IsStale = %v, want %v", got, tc.want)
			}
		})
	}

	// A query that started moments ago and has not beaten yet is not stale:
	// the fallback is its start, not zero.
	if IsStale(time.Time{}, now.Add(-time.Second), now) {
		t.Error("a query that just started was called stale")
	}
}
