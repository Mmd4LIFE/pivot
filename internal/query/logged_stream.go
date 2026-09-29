package query

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

/*
loggedStream is a result stream that writes down what it delivered.

It counts as it goes rather than at the end, because it never holds the result
-- that is the whole point of streaming, and a count taken from a materialized
slice would undo it. Closing completes the log row and releases the connector,
so a caller that does the one thing a stream already requires gets the
accounting for free.
*/
type loggedStream struct {
	connectors.Stream

	executor  *Executor
	entry     model.QueryLogEntry
	started   time.Time
	connector connectors.Connector
	ctx       context.Context //nolint:containedctx // the query's context, kept so the completing write can outlive its cancellation

	rows  int64
	bytes int64

	/*
		The tee into the cache.

		Rows are copied aside as they stream past, and the copy is abandoned the
		moment it outgrows the per-entry budget. That is what lets a cache and
		Part 20-a's constant-memory streaming coexist: a result small enough to
		be worth caching is held, a result too large to hold is not, and peak
		memory is bounded by the budget rather than by the answer.

		Abandoned rather than truncated, and abandoned for good -- `caching`
		goes false and never comes back, so a result that crossed the line is
		not partially cached and cannot be completed by a later read.
	*/
	caching     bool
	cacheKey    string
	cacheStatus string
	pending     [][]any

	/*
		release hands the governor's slot back.

		Called from Close, which is sync.Once-guarded, so the slot is returned
		exactly once however many times a caller closes. A caller who never
		closes holds the slot until their query's timeout fires -- which is why
		the organization-wide ceiling and this live in the same part.
	*/
	release func()

	/*
		unwatch takes this query out of the monitor, and cancel releases the
		context derived for it.

		Both are called from Close, which is sync.Once-guarded. Leaving a
		cancel un-called leaks it; leaving the registration in place means the
		monitor claims to be running a query that finished, and an
		administrator killing it would be told it worked.
	*/
	unwatch func()
	cancel  context.CancelCauseFunc

	// exhausted distinguishes a stream that ended from one the caller walked
	// away from. Only the first may be cached: a partial read stored whole
	// would be served as a complete answer, with nothing on it to say it was
	// not.
	exhausted bool

	once sync.Once
}

// Next advances the stream, counts what it produced and tees it into the
// pending cache entry.
func (s *loggedStream) Next() bool {
	if !s.Stream.Next() {
		s.exhausted = s.Err() == nil

		return false
	}

	row := s.Row()

	s.rows++
	s.bytes += estimate(row)

	s.tee(row)

	return true
}

/*
tee keeps a copy of the row for the cache, while the copy is still small enough
to be worth keeping.

The copy is the point. [connectors.Stream] says a row is valid only until the
next Next, because the driver reuses the backing array -- storing the slice
itself would give a cached entry whose every row is the last one read.
*/
func (s *loggedStream) tee(row []any) {
	if !s.caching {
		return
	}

	if s.bytes > s.executor.cache.MaxEntryBytes() {
		// Over budget. Drop what was accumulated and stop: holding it any
		// longer is memory spent on an entry that will never be stored.
		s.caching = false
		s.cacheStatus = CacheUncached
		s.pending = nil

		return
	}

	s.pending = append(s.pending, slices.Clone(row))
}

/*
Close completes the log row, closes the underlying stream and releases the
connector. Idempotent, and safe to call before the stream is exhausted.

The order matters: the stream is closed before the connector, because closing
the connector under an open stream returns a pooled connection that is still
reading.
*/
func (s *loggedStream) Close() error {
	var err error

	s.once.Do(func() {
		err = s.Stream.Close()

		// The stream's own error, not Close's: a stream that failed mid-read
		// reports it through Err, and that is the outcome worth recording.
		cause := s.Err()
		if cause == nil {
			cause = err
		}

		s.store(cause)
		s.executor.finish(s.ctx, s.entry, s.started, s, cause)

		if s.unwatch != nil {
			s.unwatch()
		}

		if s.release != nil {
			s.release()
		}

		// After the stream is closed and the slot returned, because canceling
		// a context whose work is already finished is free and canceling one
		// whose work is not would be racing the thing we just closed.
		if s.cancel != nil {
			s.cancel(nil)
		}

		// Nil on a cache hit, which opened nothing. Guarded rather than
		// papered over with a no-op connector, because "there is no connector"
		// is the truth about that path and a stub would hide it.
		if s.connector != nil {
			if cerr := s.connector.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}
	})

	return err
}

/*
store puts the teed result in the cache, if it earned the right to be there.

Four conditions, and each one of them is a way a cached entry could be wrong
rather than merely useless: the result must have been cached in the first
place, it must have ended rather than been abandoned, it must not have failed,
and it must still fit. A result that fails any of them is dropped, because the
cost of not caching is one slow query and the cost of caching a partial or
failed result is a wrong answer served quickly for the next minute.
*/
func (s *loggedStream) store(cause error) {
	// A hit has nothing to store and nothing to reconsider; an execution that
	// was never eligible has already said so. Only a miss is still deciding.
	if s.cacheStatus != CacheMiss {
		return
	}

	if !s.caching || !s.exhausted || cause != nil {
		s.cacheStatus = CacheUncached

		return
	}

	if !s.executor.cache.Put(
		s.ctx, s.cacheKey, s.Columns(), s.pending, s.Truncated(), s.bytes,
	) {
		s.cacheStatus = CacheUncached
	}

	s.pending = nil
}

/*
estimate sizes one row as Pivot holds it, in bytes.

An estimate, and named one. It is what the row costs in memory after the
driver decoded it, which is not what crossed the wire and not what the source
stored -- those differ by compression, by protocol framing and by the source's
own encoding. The number is here to answer "is this result enormous?", and for
that question a figure that is right to within a small factor and costs one
pass over the row beats an exact one that costs a second copy.
*/
func estimate(row []any) int64 {
	var total int64

	for _, cell := range row {
		total += estimateCell(cell)
	}

	return total
}

func estimateCell(cell any) int64 {
	switch v := cell.(type) {
	case nil:
		return 0
	case string:
		return int64(len(v))
	case []byte:
		return int64(len(v))
	case time.Time:
		// A wall clock, a monotonic reading and a location pointer.
		return 24
	case bool:
		return 1
	default:
		// Numbers and anything else decoded into a fixed-width value. Eight
		// bytes is what the driver's int64 and float64 actually occupy, and
		// an unrecognized type is more likely one of those than not.
		return 8
	}
}
