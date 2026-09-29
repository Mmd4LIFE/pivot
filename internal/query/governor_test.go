package query

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

/*
Admission.

The property worth protecting is the second one: one person cannot take a
connection away from everybody else. It is easy to write a semaphore that
enforces a total and calls it governance, and such a thing does nothing at all
about the case it was built for -- one caller running four exports.
*/

/*
One user cannot exhaust a connection's capacity for everybody else.

Alice takes her whole per-user allowance and holds it. Bob, who has run
nothing, is served immediately. Without the per-user limit Alice's queries
would have taken the connection's slots and Bob would be waiting behind them.
*/
func TestOneUserCannotStarveAnother(t *testing.T) {
	t.Parallel()

	var (
		g     = NewGovernor(WithPerUser(2), WithPerConnection(4), WithQueueWait(2*time.Second))
		org   = uuid.New()
		conn  = uuid.New()
		alice = uuid.New()
		bob   = uuid.New()
	)

	// Alice fills her allowance and keeps it.
	var held []func()

	defer func() {
		for _, release := range held {
			release()
		}
	}()

	for i := range 2 {
		release, err := g.Admit(t.Context(), org, alice, conn)
		if err != nil {
			t.Fatalf("alice's query %d was refused: %v", i, err)
		}

		held = append(held, release)
	}

	// Her third waits and is refused, because the limit is hers.
	short, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	if _, err := g.Admit(short, org, alice, conn); err == nil {
		t.Fatal("alice ran a third query against a per-user limit of two")
	}

	// Bob is served straight away. This is the whole point.
	done := make(chan error, 1)

	go func() {
		release, err := g.Admit(t.Context(), org, bob, conn)
		if release != nil {
			defer release()
		}

		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("bob was refused while alice held her own allowance: %v", err)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("bob waited behind alice's queries; the per-user limit is not doing its job")
	}
}

// The connection limit holds across users, so the source is protected too.
func TestAConnectionHasATotalCapacity(t *testing.T) {
	t.Parallel()

	var (
		g    = NewGovernor(WithPerUser(1), WithPerConnection(2), WithQueueWait(200*time.Millisecond))
		org  = uuid.New()
		conn = uuid.New()
	)

	var held []func()

	defer func() {
		for _, release := range held {
			release()
		}
	}()

	for i := range 2 {
		release, err := g.Admit(t.Context(), org, uuid.New(), conn)
		if err != nil {
			t.Fatalf("query %d was refused: %v", i, err)
		}

		held = append(held, release)
	}

	// A third user, well within their own allowance, meets the connection's.
	_, err := g.Admit(t.Context(), org, uuid.New(), conn)
	if !errors.Is(err, ErrTooBusy) {
		t.Errorf("err = %v, want ErrTooBusy", err)
	}
}

/*
Waiting and being refused are different outcomes, and so is giving up.

Three ways an admission does not succeed immediately, and an operator needs to
tell them apart: a query that waited and then ran means the limits are about
right, a query refused means they are too low or the source is overloaded, and
a caller who left means their browser tab closed. Collapsing them into one
"rejected" number loses the distinction exactly when somebody is trying to
decide whether to raise a limit.
*/
func TestWaitingIsNotTheSameAsBeingRefused(t *testing.T) {
	t.Parallel()

	var (
		g    = NewGovernor(WithPerUser(1), WithPerConnection(1), WithQueueWait(2*time.Second))
		org  = uuid.New()
		user = uuid.New()
		conn = uuid.New()
	)

	release, err := g.Admit(t.Context(), org, user, conn)
	if err != nil {
		t.Fatalf("the first query was refused: %v", err)
	}

	// A second caller waits, and is admitted once the first finishes -- so
	// waiting is a real outcome and not a disguised refusal.
	admitted := make(chan error, 1)

	go func() {
		r, aerr := g.Admit(t.Context(), org, user, conn)
		if r != nil {
			r()
		}

		admitted <- aerr
	}()

	time.Sleep(100 * time.Millisecond)
	release()

	select {
	case aerr := <-admitted:
		if aerr != nil {
			t.Errorf("a query that waited for a freed slot was refused: %v", aerr)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("a freed slot was never handed to the waiting query")
	}
}

// A caller who stops waiting gets their own context's error, not ErrTooBusy.
// Nothing is wrong with the system; they left.
func TestACallerWhoGivesUpIsNotTooBusy(t *testing.T) {
	t.Parallel()

	var (
		g    = NewGovernor(WithPerUser(1), WithPerConnection(1), WithQueueWait(time.Minute))
		org  = uuid.New()
		user = uuid.New()
		conn = uuid.New()
	)

	release, err := g.Admit(t.Context(), org, user, conn)
	if err != nil {
		t.Fatalf("the first query was refused: %v", err)
	}

	defer release()

	gone, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = g.Admit(gone, org, user, conn)

	if errors.Is(err, ErrTooBusy) {
		t.Error("a caller who gave up was reported as the system being full")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

/*
A released slot is released once, however many times release is called.

The release is wired to a stream's Close, which callers are entitled to call
more than once -- the Stream contract says so. A second release would return a
slot that was never taken, and the capacity would grow every time somebody
closed twice.
*/
func TestReleasingTwiceDoesNotInventCapacity(t *testing.T) {
	t.Parallel()

	var (
		g    = NewGovernor(WithPerUser(1), WithPerConnection(1), WithQueueWait(100*time.Millisecond))
		org  = uuid.New()
		user = uuid.New()
		conn = uuid.New()
	)

	release, err := g.Admit(t.Context(), org, user, conn)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}

	release()
	release()
	release()

	// One slot, so exactly one query may run.
	first, err := g.Admit(t.Context(), org, user, conn)
	if err != nil {
		t.Fatalf("the slot was not returned: %v", err)
	}

	defer first()

	if _, err := g.Admit(t.Context(), org, user, conn); !errors.Is(err, ErrTooBusy) {
		t.Errorf("a second query ran against a limit of one: err = %v", err)
	}
}

// Limits apply per connection and per tenant, not globally.
func TestLimitsDoNotLeakAcrossConnectionsOrTenants(t *testing.T) {
	t.Parallel()

	var (
		g    = NewGovernor(WithPerUser(1), WithPerConnection(1), WithQueueWait(100*time.Millisecond))
		org  = uuid.New()
		user = uuid.New()
		conn = uuid.New()
	)

	held, err := g.Admit(t.Context(), org, user, conn)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}

	defer held()

	// Same user, different connection.
	other, err := g.Admit(t.Context(), org, user, uuid.New())
	if err != nil {
		t.Errorf("a different connection was blocked by this one: %v", err)
	} else {
		other()
	}

	// Same connection id, different tenant. Ids are unique in practice; the
	// tenant is in the key anyway, because a limit that could be consumed by
	// another organization is a tenant boundary in the wrong place.
	elsewhere, err := g.Admit(t.Context(), uuid.New(), user, conn)
	if err != nil {
		t.Errorf("another tenant's query was blocked by ours: %v", err)
	} else {
		elsewhere()
	}
}

// The governor is used from many goroutines at once, which is the only way it
// is ever used.
func TestTheGovernorSurvivesConcurrentCallers(t *testing.T) {
	t.Parallel()

	var (
		g       = NewGovernor(WithPerUser(4), WithPerConnection(8), WithQueueWait(5*time.Second))
		org     = uuid.New()
		conn    = uuid.New()
		admits  atomic.Int64
		wg      sync.WaitGroup
		holders = 32
	)

	for i := range holders {
		wg.Add(1)

		go func() {
			defer wg.Done()

			// Four users, so the per-user limit is exercised as well as the
			// connection's.
			user := uuid.NewSHA1(uuid.Nil, []byte{byte(i % 4)})

			release, err := g.Admit(t.Context(), org, user, conn)
			if err != nil {
				return
			}

			admits.Add(1)

			time.Sleep(time.Millisecond)
			release()
		}()
	}

	wg.Wait()

	if admits.Load() != int64(holders) {
		t.Errorf("%d of %d queries were admitted; the rest were refused under contention",
			admits.Load(), holders)
	}

	// And everything was handed back, so the next query is not queueing behind
	// slots nobody holds.
	if left := g.InFlight(org, conn); left != 0 {
		t.Errorf("%d slots are still held after everything finished", left)
	}
}
