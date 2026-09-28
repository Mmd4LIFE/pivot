package connectors

import (
	"context"
	"database/sql"
	"sync"
)

/*
Streaming a result out of database/sql.

The materializing [SQLConnector.Query] is now this with a loop around it, which
is the arrangement worth having: one piece of code reads rows, applies the row
cap and copies byte slices, and there is no second implementation to disagree
with it about what a truncated result means.

What this owns that Query did not is the connection's lifetime. Query borrows,
reads and releases inside one call; a stream hands the connection to a caller
who may wander off. So the release is deferred to [sqlStream.Close], and every
path that can fail before the caller ever sees the stream closes it on the way
out -- a stream returned as an error would otherwise hold a connection that
nobody has a handle to.
*/

// sqlStream reads rows one at a time, releasing the connection when closed.
type sqlStream struct {
	rows    *sql.Rows
	columns []Column

	// release returns the borrowed connection and stops any cancellation
	// watcher bound to it.
	release func()

	// cancel ends the per-query context, which is what carries the timeout
	// and what a dialect's Canceler watches.
	cancel context.CancelFunc

	connector *SQLConnector
	ctx       context.Context

	limit int64
	read  int64

	row       []any
	cells     []any
	targets   []any
	truncated bool
	err       error

	// once guards Close, which is called from a defer and from the end of a
	// loop in the same code more often than not.
	once   sync.Once
	closed bool
}

/*
Stream runs SQL and returns the rows one at a time.

The context governs the whole read rather than the call that starts it: a
stream held open past its deadline is stopped, which is the behavior the
per-connection query timeout is supposed to have and would not if the deadline
ended at the first row.
*/
func (c *SQLConnector) Stream(ctx context.Context, query string, args ...any) (Stream, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout())

	conn, release, err := c.borrow(ctx)
	if err != nil {
		cancel()

		return nil, c.classify(ctx, err)
	}

	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		release()
		cancel()

		return nil, c.classify(ctx, err)
	}

	columns, err := describe(rows, c.dialect)
	if err != nil {
		// Not deferred: on the path below these rows belong to the stream and
		// are closed by its Close. sqlclosecheck cannot see a constructor that
		// hands ownership away.
		_ = rows.Close() //nolint:sqlclosecheck // closed here only when the stream is not returned

		release()
		cancel()

		return nil, c.classify(ctx, err)
	}

	width := len(columns)

	stream := &sqlStream{
		rows: rows, columns: columns,
		release: release, cancel: cancel,
		connector: c, ctx: ctx,
		limit:   c.maxRows(),
		cells:   make([]any, width),
		targets: make([]any, width),
	}

	for i := range stream.cells {
		stream.targets[i] = &stream.cells[i]
	}

	return stream, nil
}

// Columns describes the result.
func (s *sqlStream) Columns() []Column { return s.columns }

// Next advances to the next row.
func (s *sqlStream) Next() bool {
	if s.err != nil || s.closed {
		return false
	}

	if s.read >= s.limit {
		// Stopped *and* flagged, the same as the materializing read. A result
		// silently cut is a wrong answer presented as a right one.
		s.truncated = true

		return false
	}

	if !s.rows.Next() {
		if err := s.rows.Err(); err != nil {
			s.err = s.connector.classify(s.ctx, err)
		}

		return false
	}

	if err := s.rows.Scan(s.targets...); err != nil {
		s.err = s.connector.classify(s.ctx, err)

		return false
	}

	// Byte slices are copied to string here rather than handed out: the driver
	// may reuse the buffer on the next Next, which turns a retained []byte
	// into a value that changes underneath its owner.
	for i, cell := range s.cells {
		if raw, ok := cell.([]byte); ok {
			s.cells[i] = string(raw)
		}
	}

	s.row = s.cells
	s.read++

	return true
}

// Row is the current row, valid until the next call to Next.
func (s *sqlStream) Row() []any { return s.row }

// Err reports what stopped the stream.
func (s *sqlStream) Err() error { return s.err }

// Truncated says the stream stopped at the row cap.
func (s *sqlStream) Truncated() bool { return s.truncated }

/*
Close releases the connection and stops the query.

Idempotent, because it is called from a defer and from an error path in the
same function often enough that making the caller track it would just move the
bug. The cancel comes last: it is what a dialect's Canceler watches, and
ending the context before the connection is back would race the kill against
the pool.
*/
func (s *sqlStream) Close() error {
	var err error

	s.once.Do(func() {
		s.closed = true

		err = s.rows.Close()

		s.release()
		s.cancel()
	})

	if err != nil {
		return s.connector.classify(s.ctx, err)
	}

	return nil
}

// describe turns a driver's column metadata into Pivot's.
//
// Extracted so the streaming and materializing reads cannot drift about what a
// column is -- which they would, because the nullability rule below is the
// sort of thing that gets fixed in one place.
func describe(rows *sql.Rows, dialect Dialect) ([]Column, error) {
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}

	columns := make([]Column, 0, len(types))

	for i, t := range types {
		nullable, known := t.Nullable()

		columns = append(columns, Column{
			Name:       t.Name(),
			SourceType: t.DatabaseTypeName(),
			Type:       dialect.NormalizeType(t.DatabaseTypeName()),
			// A driver that will not say is reported as nullable, because
			// assuming NOT NULL and being wrong is a panic on a nil scan.
			Nullable: nullable || !known,
			Position: i + 1,
		})
	}

	return columns, nil
}
