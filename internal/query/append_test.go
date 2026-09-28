package query

import (
	"math"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/datatype"
)

/*
The type matrix at the edge.

Every driver disagrees about what Go type a column arrives as, and this is the
one place that has to know all of them. The conformance suite proves the four
connectors Pivot ships behave; this proves the *conversion* handles the shapes
those drivers and the next ones produce -- an int64 where another gives a
string, MySQL's uint64 for an unsigned column, a timestamp a driver handed back
as text.

Permissive about the container, strict about the value. Every accepted spelling
below is one a real driver produces, and every refusal is a value that must not
quietly become something else.
*/

// build makes a one-column builder of a given Arrow type.
func build(t *testing.T, kind datatype.Kind, bits int) (*array.RecordBuilder, *arrow.Schema) {
	t.Helper()

	schema := SchemaOf([]connectors.Column{{
		Name: "c", Type: datatype.Type{Kind: kind, Bits: bits},
	}})

	return array.NewRecordBuilder(memory.DefaultAllocator, schema), schema
}

// appendOne converts one value and returns the error, if any.
func appendOne(t *testing.T, kind datatype.Kind, bits int, value any) error {
	t.Helper()

	builder, schema := build(t, kind, bits)
	defer builder.Release()

	return appendRow(builder, schema, []any{value})
}

func TestBooleansArriveInEverySpellingADriverUses(t *testing.T) {
	t.Parallel()

	// bool from most drivers, int64 from MySQL's TINYINT, text from a driver
	// handing back PostgreSQL's "t"/"f".
	for _, value := range []any{
		true, false, int64(1), int64(0), uint64(1),
		"t", "f", "TRUE", "no", "Yes", []byte("true"),
	} {
		if err := appendOne(t, datatype.Boolean, 0, value); err != nil {
			t.Errorf("a boolean as %T (%v): %v", value, value, err)
		}
	}

	if err := appendOne(t, datatype.Boolean, 0, "maybe"); err == nil {
		t.Error("\"maybe\" was accepted as a boolean")
	}

	if err := appendOne(t, datatype.Boolean, 0, 1.5); err == nil {
		t.Error("a float was accepted as a boolean")
	}
}

func TestWholeNumbersArriveInEverySpelling(t *testing.T) {
	t.Parallel()

	for _, value := range []any{
		int64(7), int32(7), int16(7), int8(7), int(7),
		uint64(7), uint32(7), float64(7), "7", []byte(" 7 "),
	} {
		if err := appendOne(t, datatype.Integer, 64, value); err != nil {
			t.Errorf("an integer as %T (%v): %v", value, value, err)
		}
	}

	for name, value := range map[string]any{
		"prose":              "seven",
		"a fractional float": 7.5,
		"a struct":           struct{}{},
	} {
		if err := appendOne(t, datatype.Integer, 64, value); err == nil {
			t.Errorf("%s was accepted as a whole number", name)
		}
	}

	// An unsigned value past the signed ceiling is refused rather than
	// wrapping to a negative, which would look like data.
	if err := appendOne(t, datatype.Integer, 64, uint64(math.MaxUint64)); err == nil {
		t.Error("a uint64 past MaxInt64 was accepted")
	}
}

/*
A value outside a column's declared width is refused, not truncated.

The column says SMALLINT and the driver hands back something bigger: the schema
and the data disagree. Truncating produces a number wrong by 65,536, which
looks like data; refusing produces an error somebody can act on.
*/
func TestAValueTooWideForItsColumnIsRefused(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		bits  int
		value int64
	}{
		"past int16":  {16, math.MaxInt16 + 1},
		"past int32":  {32, math.MaxInt32 + 1},
		"under int16": {16, math.MinInt16 - 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := appendOne(t, datatype.Integer, tc.bits, tc.value); err == nil {
				t.Errorf("%d was accepted into a %d-bit column", tc.value, tc.bits)
			}
		})
	}

	// And one that fits is not.
	if err := appendOne(t, datatype.Integer, 16, int64(100)); err != nil {
		t.Errorf("100 was refused by a 16-bit column: %v", err)
	}
}

func TestRealNumbersArriveInEverySpelling(t *testing.T) {
	t.Parallel()

	for _, bits := range []int{32, 64} {
		for _, value := range []any{
			float64(1.5), float32(1.5), int64(2), uint64(2), "1.5", []byte("1.5"),
		} {
			if err := appendOne(t, datatype.Float, bits, value); err != nil {
				t.Errorf("a %d-bit float as %T (%v): %v", bits, value, value, err)
			}
		}
	}

	if err := appendOne(t, datatype.Float, 64, "one point five"); err == nil {
		t.Error("prose was accepted as a number")
	}

	if err := appendOne(t, datatype.Float, 64, struct{}{}); err == nil {
		t.Error("a struct was accepted as a number")
	}
}

func TestTimestampsArriveAsValuesOrAsText(t *testing.T) {
	t.Parallel()

	instant := time.Date(2024, time.March, 15, 23, 30, 0, 0, time.UTC)

	for _, kind := range []datatype.Kind{datatype.Timestamp, datatype.TimestampTZ, datatype.Date} {
		for _, value := range []any{
			instant,
			"2024-03-15T23:30:00Z",
			"2024-03-15 23:30:00+00:00",
			"2024-03-15 23:30:00",
			"2024-03-15",
			[]byte("2024-03-15 23:30:00"),
		} {
			if err := appendOne(t, kind, 0, value); err != nil {
				t.Errorf("%s as %T (%v): %v", kind, value, value, err)
			}
		}

		if err := appendOne(t, kind, 0, "the fifteenth"); err == nil {
			t.Errorf("%s accepted prose", kind)
		}

		// A unix epoch has no zone and no agreed unit, so it is refused
		// rather than guessed at.
		if err := appendOne(t, kind, 0, int64(1710545400)); err == nil {
			t.Errorf("%s accepted a bare number", kind)
		}
	}
}

// Binary takes bytes or text, because a driver may hand back either for the
// same column.
func TestBinaryTakesBytesOrText(t *testing.T) {
	t.Parallel()

	for _, value := range []any{[]byte{1, 2, 3}, "abc"} {
		if err := appendOne(t, datatype.Binary, 0, value); err != nil {
			t.Errorf("binary as %T: %v", value, err)
		}
	}

	if err := appendOne(t, datatype.Binary, 0, 42); err == nil {
		t.Error("a number was accepted as binary")
	}
}

/*
The text fallback accepts anything, and never fails.

It is what every kind Pivot cannot represent richly becomes -- a decimal a
driver returned as a string, a JSON document, a UUID, an array. Refusing here
would turn a column Pivot could not map into a column Pivot could not read,
which is a worse answer than the source's own spelling.
*/
func TestTheTextFallbackAcceptsAnything(t *testing.T) {
	t.Parallel()

	for _, kind := range []datatype.Kind{
		datatype.String, datatype.Decimal, datatype.JSON,
		datatype.UUID, datatype.Array, datatype.Struct,
		datatype.Interval, datatype.Time, datatype.Unknown,
	} {
		for _, value := range []any{
			"text", []byte("bytes"), 42, 1.5, true,
			time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC),
			struct{ A int }{1},
		} {
			if err := appendOne(t, kind, 0, value); err != nil {
				t.Errorf("%s as %T: %v", kind, value, err)
			}
		}
	}
}

// A NULL is a null in every kind, rather than a zero in the numeric ones.
func TestANullIsANullInEveryKind(t *testing.T) {
	t.Parallel()

	for _, kind := range []datatype.Kind{
		datatype.Boolean, datatype.Integer, datatype.Float, datatype.String,
		datatype.Binary, datatype.Date, datatype.Timestamp, datatype.TimestampTZ,
		datatype.Decimal, datatype.Unknown,
	} {
		builder, schema := build(t, kind, 64)

		if err := appendRow(builder, schema, []any{nil}); err != nil {
			builder.Release()
			t.Fatalf("a null in a %s column: %v", kind, err)
		}

		record := builder.NewRecordBatch()

		if !record.Column(0).IsNull(0) {
			t.Errorf("a NULL in a %s column did not become a null", kind)
		}

		record.Release()
		builder.Release()
	}
}

// A row of the wrong width is an error rather than a partial batch, because a
// batch with a short column is one Arrow will reject later and further away.
func TestARowOfTheWrongWidthIsRefused(t *testing.T) {
	t.Parallel()

	builder, schema := build(t, datatype.Integer, 64)
	defer builder.Release()

	if err := appendRow(builder, schema, []any{int64(1), int64(2)}); err == nil {
		t.Error("a two-cell row was accepted into a one-column schema")
	}
}

// Every canonical kind maps to some Arrow type, including ones added later:
// the fallback is text rather than a panic.
func TestEveryCanonicalKindHasAnArrowType(t *testing.T) {
	t.Parallel()

	for _, kind := range []datatype.Kind{
		datatype.Unknown, datatype.Boolean, datatype.Integer, datatype.Decimal,
		datatype.Float, datatype.String, datatype.Binary, datatype.Date,
		datatype.Time, datatype.Timestamp, datatype.TimestampTZ,
		datatype.Interval, datatype.JSON, datatype.UUID, datatype.Array,
		datatype.Struct, datatype.Kind("something invented later"),
	} {
		if got := arrowType(datatype.Type{Kind: kind}); got == nil {
			t.Errorf("%s has no Arrow type", kind)
		}
	}

	// Widths pick the matching Arrow width rather than always the widest.
	for bits, want := range map[int]arrow.Type{
		8: arrow.INT16, 16: arrow.INT16, 32: arrow.INT32, 64: arrow.INT64, 0: arrow.INT64,
	} {
		got := arrowType(datatype.Type{Kind: datatype.Integer, Bits: bits})
		if got.ID() != want {
			t.Errorf("a %d-bit integer became %s, want %s", bits, got.ID(), want)
		}
	}

	if got := arrowType(datatype.Type{Kind: datatype.Float, Bits: 32}); got.ID() != arrow.FLOAT32 {
		t.Errorf("a 32-bit float became %s", got.ID())
	}
}
