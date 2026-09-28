package query

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/datatype"
)

/*
Turning a source's rows into Arrow record batches.

ADR-0004 makes Arrow the internal contract so that Phase 2's charts, Phase 6's
flows, Part 24's exports and the Python AI service do not each invent a result
format -- and says row-oriented conversion happens **once, at the edge**. This
is that edge. `database/sql` is row-oriented and nothing changes that, so the
rows arrive one at a time and leave in columns.

# Why batches rather than a whole table

A batch is the unit that makes the memory claim true. Rows accumulate into
column builders until the batch is full, the batch is handed to the caller, and
the builders are reset. Peak memory is one batch plus whatever the caller is
holding -- not one row more for a ten-million-row answer than for a thousand.

The caller releases each batch. Arrow buffers are reference counted and come
from a pool, so a batch nobody releases is memory nobody reclaims: the
allocation lives as long as the process rather than as long as the scope.
*/

// DefaultBatchRows is how many rows go in a record batch.
//
// Big enough that per-batch overhead disappears, small enough that one batch of
// wide rows is megabytes rather than hundreds. The number matters less than
// that there is one: a batch sized by the result rather than by a constant is
// a materialized result wearing a different name.
const DefaultBatchRows = 4096

/*
Batches converts a stream of rows into Arrow record batches.

The stream is not closed here. Whoever opened it owns it, and a converter that
closed somebody else's stream would make the obvious `defer stream.Close()` a
double close -- which is legal but hides the question of who is responsible.
*/
type Batches struct {
	stream connectors.Stream
	schema *arrow.Schema
	// builder holds the column builders between batches, reset rather than
	// reallocated so a long stream does not churn.
	builder *array.RecordBuilder
	size    int
	err     error
	done    bool
}

// NewBatches prepares a conversion over an open stream.
func NewBatches(stream connectors.Stream, size int) *Batches {
	if size <= 0 {
		size = DefaultBatchRows
	}

	schema := SchemaOf(stream.Columns())

	return &Batches{
		stream:  stream,
		schema:  schema,
		builder: array.NewRecordBuilder(memory.DefaultAllocator, schema),
		size:    size,
	}
}

// Schema is the Arrow schema of every batch this produces.
func (b *Batches) Schema() *arrow.Schema { return b.schema }

/*
Next returns the next batch, or nil when the stream is finished.

The caller releases what it gets. A batch is reference counted and the
allocation outlives the scope otherwise, which on a ten-million-row export is
the difference between constant memory and none left.
*/
func (b *Batches) Next() arrow.RecordBatch {
	if b.done || b.err != nil {
		return nil
	}

	rows := 0

	for rows < b.size && b.stream.Next() {
		if err := appendRow(b.builder, b.schema, b.stream.Row()); err != nil {
			b.err = err

			return nil
		}

		rows++
	}

	if rows < b.size {
		// The stream ran out inside this batch, so there will not be another.
		b.done = true

		if err := b.stream.Err(); err != nil {
			b.err = err

			return nil
		}
	}

	if rows == 0 {
		return nil
	}

	// NewRecordBatch resets the builders, which is what keeps one batch's
	// worth of memory in play rather than the whole result's.
	return b.builder.NewRecordBatch()
}

// Err reports what stopped the conversion.
func (b *Batches) Err() error { return b.err }

// Truncated says the underlying stream stopped at the connection's row cap.
func (b *Batches) Truncated() bool { return b.stream.Truncated() }

// Release frees the builders. Called when the caller is finished, whether or
// not the stream ran to the end.
func (b *Batches) Release() { b.builder.Release() }

/*
SchemaOf maps Pivot's canonical types onto Arrow's.

Every column is nullable in the Arrow schema regardless of what the source
said. A source that reports NOT NULL and then returns a NULL -- which happens
on an outer join, on a view, and on any dialect whose driver declines to say --
would otherwise produce a batch that violates its own schema, and Arrow is
within its rights to panic on that. Nullable is the safe direction, and
[connectors.Column.Nullable] is still carried for anything that wants the
source's opinion.
*/
func SchemaOf(columns []connectors.Column) *arrow.Schema {
	fields := make([]arrow.Field, 0, len(columns))

	for _, col := range columns {
		fields = append(fields, arrow.Field{
			Name:     col.Name,
			Type:     arrowType(col.Type),
			Nullable: true,
			Metadata: arrow.NewMetadata(
				[]string{"pivot.kind", "pivot.source_type"},
				[]string{string(col.Type.Kind), col.SourceType},
			),
		})
	}

	return arrow.NewSchema(fields, nil)
}

/*
arrowType picks the Arrow type for a canonical one.

Deliberately conservative. A canonical kind Pivot cannot represent losslessly
becomes a string rather than a guess at something numeric -- the same principle
[datatype.Unknown] follows, for the same reason: a wrong type is
indistinguishable from a right one until somebody computes with it.

Decimal is the one that looks like a compromise and is not. Arrow's decimal
types need a precision and scale, and the catalog does not carry them yet
(Part 19-b left that to whatever needs it first). A decimal rendered as text
keeps every digit; a decimal guessed into a float128 with the wrong scale does
not, and the whole reason [datatype.Decimal] exists is that those digits matter.
*/
func arrowType(t datatype.Type) arrow.DataType {
	switch t.Kind {
	case datatype.Boolean:
		return arrow.FixedWidthTypes.Boolean

	case datatype.Integer:
		switch t.Bits {
		case 8, 16:
			return arrow.PrimitiveTypes.Int16
		case 32:
			return arrow.PrimitiveTypes.Int32
		default:
			return arrow.PrimitiveTypes.Int64
		}

	case datatype.Float:
		if t.Bits == 32 {
			return arrow.PrimitiveTypes.Float32
		}

		return arrow.PrimitiveTypes.Float64

	case datatype.Date:
		return arrow.FixedWidthTypes.Date32

	case datatype.Timestamp:
		// No zone: a wall-clock reading, which Arrow spells as a timestamp
		// with an empty timezone.
		return &arrow.TimestampType{Unit: arrow.Microsecond}

	case datatype.TimestampTZ:
		// An instant. UTC rather than the source's zone, because the instant
		// is what was stored and the zone it was displayed in is not part of
		// the value.
		return &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}

	case datatype.Binary:
		return arrow.BinaryTypes.Binary

	default:
		// String, Decimal, Time, Interval, JSON, UUID, Array, Struct and
		// Unknown. Text loses nothing that Pivot currently knows how to keep.
		return arrow.BinaryTypes.String
	}
}

// Collect reads a stream to the end and returns every batch.
//
// For tests and for callers that genuinely want the whole thing. Named so that
// using it is a decision: everything else in this package streams, and this is
// the one function that does not.
func Collect(ctx context.Context, stream connectors.Stream, size int) ([]arrow.RecordBatch, error) {
	batches := NewBatches(stream, size)
	defer batches.Release()

	var out []arrow.RecordBatch

	for {
		if err := ctx.Err(); err != nil {
			releaseAll(out)

			return nil, err
		}

		record := batches.Next()
		if record == nil {
			break
		}

		out = append(out, record)
	}

	if err := batches.Err(); err != nil {
		releaseAll(out)

		return nil, fmt.Errorf("read the result: %w", err)
	}

	return out, nil
}

func releaseAll(records []arrow.RecordBatch) {
	for _, record := range records {
		record.Release()
	}
}
