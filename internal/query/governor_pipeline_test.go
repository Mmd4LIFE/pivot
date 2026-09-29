package query

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

/*
Governance through the pipeline.

The governor's own tests prove the semaphore. These prove it is wired where it
claims to be: after the cache, released on Close, and bounded by the
organization's timeout ceiling.
*/

// A query that cannot get a slot is refused as too busy, through Execute.
func TestThePipelineRefusesAQueryItCannotAdmit(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	f.executor = NewExecutor(f.repos, f.checker,
		withOpener(f.countingOpener()),
		WithGovernor(NewGovernor(WithPerUser(1), WithPerConnection(1),
			WithQueueWait(100*time.Millisecond))))

	ctx := f.as(t, f.analyst)

	held, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("the first query was refused: %v", err)
	}

	if _, berr := f.executor.Execute(ctx, Request{
		ConnectionID: f.connID, SQL: "SELECT 1",
	}); !errors.Is(berr, ErrTooBusy) {
		t.Errorf("err = %v, want ErrTooBusy", berr)
	}

	// And closing the first frees the slot, so the refusal was about capacity
	// rather than a slot that leaked.
	drain(t, held)

	after, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: "SELECT 1"})
	if err != nil {
		t.Fatalf("the slot was not returned when the stream closed: %v", err)
	}

	drain(t, after)
}

/*
A cache hit does not consume a connection's capacity.

Admission sits after the cache for this reason. A hit opens nothing and asks
the source for nothing, so making it queue behind queries that do would be a
limit that punishes exactly the requests the cache exists to make cheap.
*/
func TestACacheHitNeedsNoSlot(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	f.executor = NewExecutor(f.repos, f.checker,
		withOpener(f.countingOpener()),
		WithCache(f.cache, f.granter(t)),
		WithGovernor(NewGovernor(WithPerUser(1), WithPerConnection(1),
			WithQueueWait(100*time.Millisecond))))

	ctx := f.as(t, f.analyst)

	// Warm the cache and let go of the slot.
	warm, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("warm: %v", err)
	}

	drain(t, warm)

	// Now hold the only slot with a query that must reach the source...
	held, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: "SELECT 1"})
	if err != nil {
		t.Fatalf("holding query: %v", err)
	}

	defer func() { _ = held.Stream.Close() }()

	// ...and a cached query is served anyway.
	hit, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("a cached query was refused while a slot was held: %v", err)
	}

	if hit.CacheStatus != CacheHit {
		t.Fatalf("the second execution was a %q, so this test proved nothing", hit.CacheStatus)
	}

	drain(t, hit)
}

// A slot is returned when the stream closes, even if the caller abandons it
// part-read.
func TestAnAbandonedStreamReturnsItsSlot(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	f.executor = NewExecutor(f.repos, f.checker,
		withOpener(f.countingOpener()),
		WithGovernor(NewGovernor(WithPerUser(1), WithPerConnection(1),
			WithQueueWait(100*time.Millisecond))))

	ctx := f.as(t, f.analyst)

	ex, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: "SELECT id FROM orders"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	ex.Stream.Next()

	if cerr := ex.Stream.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}

	again, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: "SELECT 1"})
	if err != nil {
		t.Fatalf("the slot was not returned by an abandoned stream: %v", err)
	}

	drain(t, again)
}

/*
Two users, one connection, and the one who has run nothing is served.

The same property as the governor's own test, through the whole pipeline --
because the thing that could be wrong here is not the semaphore but what the
pipeline passes it. An executor that keyed admission on, say, the connection
alone would pass every governor test and fail this one.
*/
func TestOneUserCannotStarveAnotherThroughThePipeline(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	f.executor = NewExecutor(f.repos, f.checker,
		withOpener(f.countingOpener()),
		WithGovernor(NewGovernor(WithPerUser(1), WithPerConnection(4),
			WithQueueWait(2*time.Second))))

	// The analyst takes their whole allowance and keeps it.
	greedy, err := f.executor.Execute(f.as(t, f.analyst),
		Request{ConnectionID: f.connID, SQL: "SELECT id FROM orders"})
	if err != nil {
		t.Fatalf("the analyst's query was refused: %v", err)
	}

	defer func() { _ = greedy.Stream.Close() }()

	// The administrator, who has run nothing, is served.
	done := make(chan error, 1)

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		ex, aerr := f.executor.Execute(f.as(t, f.admin),
			Request{ConnectionID: f.connID, SQL: emeaQuery})
		if ex != nil {
			_ = ex.Stream.Close()
		}

		done <- aerr
	}()

	select {
	case aerr := <-done:
		if aerr != nil {
			t.Errorf("the administrator was refused while the analyst held their own allowance: %v", aerr)
		}

	case <-time.After(3 * time.Second):
		t.Fatal("the administrator waited behind the analyst's query")
	}

	wg.Wait()
}

/*
The organization's ceiling shortens a connection's timeout and never lengthens
it.

Checked on the configuration handed to the connector rather than by running a
slow query, because the claim is about which number wins and a test that
measured it would be timing a sleep.
*/
func TestTheTimeoutCeilingOnlyEverShortens(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		ceiling    time.Duration
		connection int64
		want       int
	}{
		"no ceiling leaves the connection alone":   {0, 30, 30},
		"a lower ceiling wins":                     {10 * time.Second, 30, 10},
		"a higher ceiling does not lengthen":       {60 * time.Second, 30, 30},
		"a ceiling applies where there is none":    {10 * time.Second, 0, 10},
		"neither set means no timeout":             {0, 0, 0},
		"a sub-second ceiling is one, not nothing": {500 * time.Millisecond, 0, 1},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e := NewExecutor(nil, nil, WithQueryTimeout(tc.ceiling))

			got := e.effectiveTimeout(modelConnectionWithTimeout(tc.connection))
			if got != tc.want {
				t.Errorf("effective timeout = %d, want %d", got, tc.want)
			}
		})
	}
}

// The limits an operator set are the limits reported back, so the command that
// shows them cannot drift from the thing enforcing them.
func TestAGovernorReportsTheLimitsItEnforces(t *testing.T) {
	t.Parallel()

	g := NewGovernor(WithPerUser(3), WithPerConnection(9), WithQueueWait(7*time.Second))

	perUser, perConnection, wait := g.Limits()
	if perUser != 3 || perConnection != 9 || wait != 7*time.Second {
		t.Errorf("Limits() = %d, %d, %v", perUser, perConnection, wait)
	}
}

// modelConnectionWithTimeout is a connection carrying only the field these
// cases are about.
func modelConnectionWithTimeout(seconds int64) model.Connection {
	return model.Connection{QueryTimeoutSeconds: seconds}
}
