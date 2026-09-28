package query

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

/*
Putting one driver row into Arrow's column builders.

The awkward part of the edge, and the reason it is one function rather than
scattered: drivers disagree about what Go type a column arrives as. pgx gives
an int64 where another gives a string; MySQL gives uint64 for an unsigned
column; SQLite gives whatever was stored. The conformance suite's readers made
the same discovery on the test side, and this is the production half of it --
permissive about the container, strict about the value.

A value that will not convert is an error rather than a null. A null here is
indistinguishable from a NULL in the source, which means a conversion Pivot got
wrong would show up as missing data in a chart and nowhere else.
*/

// appendRow appends one row to the builders, column by column.
func appendRow(builder *array.RecordBuilder, schema *arrow.Schema, row []any) error {
	if len(row) != len(builder.Fields()) {
		return fmt.Errorf("the row has %d cells and the schema %d columns",
			len(row), len(builder.Fields()))
	}

	for i, cell := range row {
		field := builder.Field(i)

		if cell == nil {
			field.AppendNull()

			continue
		}

		if err := appendCell(field, cell); err != nil {
			return fmt.Errorf("column %q: %w", schema.Field(i).Name, err)
		}
	}

	return nil
}

// appendCell appends one value to one builder.
func appendCell(field array.Builder, cell any) error {
	switch b := field.(type) {
	case *array.BooleanBuilder:
		value, err := asBool(cell)
		if err != nil {
			return err
		}

		b.Append(value)

	case *array.Int16Builder:
		value, err := asInt(cell, math.MinInt16, math.MaxInt16)
		if err != nil {
			return err
		}

		// asInt refused anything outside the range just passed to it, which
		// gosec cannot see from here.
		b.Append(int16(value)) //nolint:gosec // G115: bounds checked by asInt

	case *array.Int32Builder:
		value, err := asInt(cell, math.MinInt32, math.MaxInt32)
		if err != nil {
			return err
		}

		b.Append(int32(value)) //nolint:gosec // G115: bounds checked by asInt

	case *array.Int64Builder:
		value, err := asInt(cell, math.MinInt64, math.MaxInt64)
		if err != nil {
			return err
		}

		b.Append(value)

	case *array.Float32Builder:
		value, err := asFloat(cell)
		if err != nil {
			return err
		}

		b.Append(float32(value))

	case *array.Float64Builder:
		value, err := asFloat(cell)
		if err != nil {
			return err
		}

		b.Append(value)

	case *array.Date32Builder:
		value, err := asTime(cell)
		if err != nil {
			return err
		}

		b.Append(arrow.Date32FromTime(value))

	case *array.TimestampBuilder:
		value, err := asTime(cell)
		if err != nil {
			return err
		}

		b.Append(arrow.Timestamp(value.UnixMicro()))

	case *array.BinaryBuilder:
		switch raw := cell.(type) {
		case []byte:
			b.Append(raw)
		case string:
			b.Append([]byte(raw))
		default:
			return fmt.Errorf("want bytes, got %T", cell)
		}

	case *array.StringBuilder:
		b.Append(asString(cell))

	default:
		return fmt.Errorf("no rule for an Arrow %T", field)
	}

	return nil
}

// asBool reads a truth value. The spellings are the ones real drivers produce:
// Go's bool, MySQL's TINYINT, and PostgreSQL's "t"/"f" as text.
func asBool(cell any) (bool, error) {
	switch v := cell.(type) {
	case bool:
		return v, nil
	case int64:
		return v != 0, nil
	case uint64:
		return v != 0, nil
	case []byte:
		return asBool(string(v))
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "t", "true", "1", "y", "yes":
			return true, nil
		case "f", "false", "0", "n", "no":
			return false, nil
		}
	}

	return false, fmt.Errorf("want a boolean, got %T (%v)", cell, cell)
}

/*
asInt reads a whole number, refusing one that will not fit.

The bounds are not decoration. A column declared SMALLINT whose driver hands
back an int64 outside that range means the schema and the data disagree, and
silently truncating produces a number that is wrong by 65,536 -- which looks
like data. Refusing produces an error somebody can act on.
*/
func asInt(cell any, low, high int64) (int64, error) {
	var value int64

	switch v := cell.(type) {
	case int64:
		value = v
	case int32:
		value = int64(v)
	case int16:
		value = int64(v)
	case int8:
		value = int64(v)
	case int:
		value = int64(v)
	case uint64:
		if v > math.MaxInt64 {
			return 0, fmt.Errorf("%d does not fit in a signed 64-bit integer", v)
		}

		value = int64(v)
	case uint32:
		value = int64(v)
	case float64:
		if v != math.Trunc(v) {
			return 0, fmt.Errorf("%v is not a whole number", v)
		}

		value = int64(v)
	case []byte:
		return asInt(string(v), low, high)
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("want a whole number, got %q", v)
		}

		value = parsed
	default:
		return 0, fmt.Errorf("want a whole number, got %T (%v)", cell, cell)
	}

	if value < low || value > high {
		return 0, fmt.Errorf("%d does not fit the column's declared width", value)
	}

	return value, nil
}

func asFloat(cell any) (float64, error) {
	switch v := cell.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case []byte:
		return asFloat(string(v))
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, fmt.Errorf("want a number, got %q", v)
		}

		return parsed, nil
	}

	return 0, fmt.Errorf("want a number, got %T (%v)", cell, cell)
}

// timeLayouts are the spellings a driver that returns timestamps as text uses.
var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func asTime(cell any) (time.Time, error) {
	switch v := cell.(type) {
	case time.Time:
		return v, nil
	case []byte:
		return asTime(string(v))
	case string:
		text := strings.TrimSpace(v)

		for _, layout := range timeLayouts {
			if parsed, err := time.Parse(layout, text); err == nil {
				return parsed, nil
			}
		}

		return time.Time{}, fmt.Errorf("want a timestamp, got %q", v)
	}

	return time.Time{}, fmt.Errorf("want a timestamp, got %T (%v)", cell, cell)
}

/*
asString renders anything as text, and never fails.

The fallback column type, so it has to accept whatever arrives: a decimal a
driver handed back as a string, a JSON document, a UUID, an array Pivot has no
richer representation for. Refusing here would mean a column Pivot could not
map became a column Pivot could not read, which is a worse answer than the
source's own spelling.
*/
func asString(cell any) string {
	switch v := cell.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case time.Time:
		return v.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(v)
	}
}
