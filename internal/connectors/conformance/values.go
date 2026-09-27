package conformance

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

/*
Reading a driver's idea of a value.

Two drivers reading the same column give back different Go types, and both are
right: pgx returns an int64 for a BIGINT while another driver returns a string
for a NUMERIC, and a third hands back []byte for everything. Part 19 is where
normalization becomes a contract the whole product depends on. Until then the
suite asserts the property that actually matters today -- the value is
faithful -- and is deliberately permissive about the Go type it arrives in.

Permissive about the container, strict about the contents. A driver may return
"1.5" or 1.5; it may not return 1.4999. NULL is nil and nothing else, because
that is the one conversion no dialect has an excuse for.
*/

// asString reads a text value.
func asString(v any) (string, error) {
	switch typed := v.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	default:
		return "", fmt.Errorf("want text, got %T (%v)", v, v)
	}
}

// text renders a value already known to be a string or a byte slice.
//
// Separate from [asString] because the callers below have matched on the type
// already, and threading an error back from a conversion that cannot fail
// obscures the one that can.
func text(v any) string {
	switch typed := v.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(v)
	}
}

// asInt64 reads a whole number.
func asInt64(v any) (int64, error) {
	switch typed := v.(type) {
	case int64:
		return typed, nil
	case int32:
		return int64(typed), nil
	case int:
		return int64(typed), nil
	case float64:
		// A driver that routes integers through a float is not wrong until the
		// value stops being exact, which is where this refuses.
		if typed != float64(int64(typed)) {
			return 0, fmt.Errorf("%v is not a whole number", typed)
		}

		return int64(typed), nil
	case string, []byte:
		raw := text(v)

		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("want a whole number, got %q", raw)
		}

		return n, nil
	default:
		return 0, fmt.Errorf("want a whole number, got %T (%v)", v, v)
	}
}

// asFloat64 reads a real number.
func asFloat64(v any) (float64, error) {
	switch typed := v.(type) {
	case float64:
		return typed, nil
	case float32:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	case string, []byte:
		raw := text(v)

		f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return 0, fmt.Errorf("want a number, got %q", raw)
		}

		return f, nil
	default:
		return 0, fmt.Errorf("want a number, got %T (%v)", v, v)
	}
}

// asBool reads a truth value.
//
// The spellings below are the ones real drivers produce: Go's bool, MySQL's
// TINYINT, and PostgreSQL's "t"/"f" when a value arrives as text.
func asBool(v any) (bool, error) {
	switch typed := v.(type) {
	case bool:
		return typed, nil
	case int64:
		return typed != 0, nil
	case string, []byte:
		raw := text(v)

		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "t", "true", "1", "y", "yes":
			return true, nil
		case "f", "false", "0", "n", "no":
			return false, nil
		}

		return false, fmt.Errorf("want a boolean, got %q", raw)
	default:
		return false, fmt.Errorf("want a boolean, got %T (%v)", v, v)
	}
}

// timeLayouts are the spellings a driver that returns timestamps as text uses.
var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// asTime reads a timestamp.
func asTime(v any) (time.Time, error) {
	switch typed := v.(type) {
	case time.Time:
		return typed, nil
	case string, []byte:
		raw := strings.TrimSpace(text(v))

		for _, layout := range timeLayouts {
			if parsed, err := time.Parse(layout, raw); err == nil {
				return parsed, nil
			}
		}

		return time.Time{}, fmt.Errorf("want a timestamp, got %q", raw)
	default:
		return time.Time{}, fmt.Errorf("want a timestamp, got %T (%v)", v, v)
	}
}

// wallClock is a timestamp's calendar reading with its zone discarded.
//
// What a naive timestamp means: the source stored "2000-01-01 00:00:01" with
// no zone, and whatever location the driver decided to attach on the way out,
// those are the numbers that have to come back.
func wallClock(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}
