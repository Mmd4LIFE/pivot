package query

import (
	"context"
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

	once sync.Once
}

// Next advances the stream and counts what it produced.
func (s *loggedStream) Next() bool {
	if !s.Stream.Next() {
		return false
	}

	s.rows++
	s.bytes += estimate(s.Row())

	return true
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

		s.executor.finish(s.ctx, s.entry, s.started, s, cause)

		if cerr := s.connector.Close(); cerr != nil && err == nil {
			err = cerr
		}
	})

	return err
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
