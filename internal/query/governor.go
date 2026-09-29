package query

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ErrTooBusy means the query waited for a slot and did not get one. It is
// distinct from every other failure on purpose: nothing is wrong, the caller
// is simply not going to be served right now, and a retry is reasonable.
var ErrTooBusy = errors.New("query: too many queries are already running")

// Defaults for admission. Chosen to be generous enough that nobody meets them
// by accident and small enough that meeting them means something.
const (
	// DefaultMaxPerUser is how many queries one person may have running
	// against one connection.
	DefaultMaxPerUser = 4

	// DefaultMaxPerConnection is how many queries anybody may have running
	// against one connection.
	DefaultMaxPerConnection = 16

	// DefaultQueueWait is how long a query waits for a slot before it is
	// refused.
	//
	// Bounded rather than indefinite, because a caller waiting behind a
	// twenty-minute export has already given up -- their browser tab timed out
	// long ago -- and the queue would be holding a place for nobody.
	DefaultQueueWait = 10 * time.Second
)

/*
Governor decides who gets to run, and who waits.

Two limits, and they answer different questions. The per-connection limit
protects the *source*: a warehouse with twenty slots should not be sent a
hundred queries. The per-user limit protects everybody *else*: without it one
person running exports takes the whole connection and the fact that the source
was healthy the entire time is no comfort to anyone.

This is not the connection pool, and "the pool already queues" is not a defense.
A pool of four does block the fifth query -- inside the driver, invisibly, with
no way to tell the caller they are waiting. And it is not a queue: when a
connection frees, database/sql hands it to a waiter chosen with rand.IntN
(`connRequests.TakeRandom`, sql.go:1554), so the longest-waiting caller has no
better claim on it than the newest. That is the right trade for a driver, which
is managing a resource. It is the wrong one for a product deciding whose work
matters, which is what this is.

The zero value is not usable. Build one with [NewGovernor].
*/
type Governor struct {
	perUser       int
	perConnection int
	wait          time.Duration

	mu    sync.Mutex
	slots map[string]chan struct{}

	instruments *governorInstruments
}

// GovernorOption configures a Governor.
type GovernorOption func(*Governor)

// WithPerUser caps how many queries one person may run against one connection.
func WithPerUser(n int) GovernorOption {
	return func(g *Governor) {
		if n > 0 {
			g.perUser = n
		}
	}
}

// WithPerConnection caps how many queries anybody may run against one
// connection.
func WithPerConnection(n int) GovernorOption {
	return func(g *Governor) {
		if n > 0 {
			g.perConnection = n
		}
	}
}

// WithQueueWait bounds how long a query waits for a slot.
func WithQueueWait(d time.Duration) GovernorOption {
	return func(g *Governor) {
		if d > 0 {
			g.wait = d
		}
	}
}

// NewGovernor builds a governor with the given limits.
func NewGovernor(opts ...GovernorOption) *Governor {
	g := &Governor{
		perUser:       DefaultMaxPerUser,
		perConnection: DefaultMaxPerConnection,
		wait:          DefaultQueueWait,
		slots:         make(map[string]chan struct{}),
		instruments:   newGovernorInstruments(),
	}

	for _, opt := range opts {
		opt(g)
	}

	return g
}

// Limits reports what this governor enforces, for the command that shows an
// operator what is in force.
func (g *Governor) Limits() (perUser, perConnection int, wait time.Duration) {
	return g.perUser, g.perConnection, g.wait
}

/*
Admit takes a slot for this query, waiting up to the queue timeout, and returns
the release.

The user's slot is taken first and the connection's second, and that order is
the fairness property rather than an implementation detail. Taking the
connection slot first would mean a caller already at their own limit sits on
source capacity while they wait for themselves -- so one person queueing behind
their own exports would block everybody, which is the exact failure the
per-user limit exists to prevent.

The returned release is safe to call more than once and must be called exactly
where the query ends. A slot that is never released is capacity nobody can ever
use again, which is worse than a leaked log row: it degrades everyone.
*/
func (g *Governor) Admit(
	ctx context.Context, orgID, userID, connectionID uuid.UUID,
) (release func(), err error) {
	user := g.slot(fmt.Sprintf("u|%s|%s|%s", orgID, userID, connectionID), g.perUser)

	releaseUser, err := g.acquire(ctx, user)
	if err != nil {
		g.instruments.recordAdmission(ctx, outcomeOf(err, "user"))

		return nil, err
	}

	conn := g.slot(fmt.Sprintf("c|%s|%s", orgID, connectionID), g.perConnection)

	releaseConn, err := g.acquire(ctx, conn)
	if err != nil {
		releaseUser()
		g.instruments.recordAdmission(ctx, outcomeOf(err, "connection"))

		return nil, err
	}

	g.instruments.recordAdmission(ctx, "admitted")

	var once sync.Once

	return func() {
		once.Do(func() {
			releaseConn()
			releaseUser()
		})
	}, nil
}

// outcomeOf labels why an admission failed, distinguishing a full system from
// a caller who stopped waiting -- which are the same refusal and very
// different operational facts.
func outcomeOf(err error, limit string) string {
	if errors.Is(err, ErrTooBusy) {
		return "refused_" + limit
	}

	return "abandoned"
}

/*
acquire takes one slot, or explains which way it failed.

Three outcomes and they are deliberately not the same error. A slot taken
immediately is the ordinary case. A caller whose own context ended while
waiting was canceled -- that is their business, not the governor's. A caller
who waited out the queue timeout gets [ErrTooBusy], which is the only one of
the three that means the system is full.
*/
func (g *Governor) acquire(ctx context.Context, slot chan struct{}) (func(), error) {
	release := func() { <-slot }

	select {
	case slot <- struct{}{}:
		return release, nil
	default:
	}

	timer := time.NewTimer(g.wait)
	defer timer.Stop()

	select {
	case slot <- struct{}{}:
		return release, nil

	case <-ctx.Done():
		return nil, ctx.Err()

	case <-timer.C:
		return nil, fmt.Errorf("%w: waited %s for a slot", ErrTooBusy, g.wait)
	}
}

/*
slot returns the semaphore for a key, creating it on first use.

Created lazily and never removed. A map entry is a channel and two words, and
the alternative -- reference counting them so an idle one can be collected --
is a lock-ordering problem in exchange for memory that is measured in bytes
per (user, connection) pair an instance has ever served.
*/
func (g *Governor) slot(key string, size int) chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()

	if existing, ok := g.slots[key]; ok {
		return existing
	}

	created := make(chan struct{}, size)
	g.slots[key] = created

	return created
}

// InFlight reports how many queries hold a slot on a connection, for the
// command that shows an operator what is happening.
func (g *Governor) InFlight(orgID, connectionID uuid.UUID) int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return len(g.slots[fmt.Sprintf("c|%s|%s", orgID, connectionID)])
}
