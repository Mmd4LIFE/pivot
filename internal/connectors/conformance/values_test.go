package conformance

import (
	"math"
	"testing"
	"time"
)

/*
Reading a driver's idea of a value.

These are the conversions the suite is permissive about, and the line it draws.
A driver may hand back "1.5" or 1.5 or even an int64; it may not hand back
something that means a different number. Every accepted spelling below is one a
real driver produces, and every rejection is a value the suite must not quietly
paper over -- because papering over it here would make a connector bug look
like a pass.
*/

func TestReadingText(t *testing.T) {
	t.Parallel()

	if got, err := asString("hello"); err != nil || got != "hello" {
		t.Errorf("asString(string) = %q, %v", got, err)
	}

	// Drivers that hand back a reused buffer are the reason scanRow copies;
	// the reader accepts the copy either way.
	if got, err := asString([]byte("hello")); err != nil || got != "hello" {
		t.Errorf("asString([]byte) = %q, %v", got, err)
	}

	if _, err := asString(42); err == nil {
		t.Error("asString accepted a number, so a numeric column could pass a text check")
	}
}

func TestReadingWholeNumbers(t *testing.T) {
	t.Parallel()

	for name, in := range map[string]any{
		"int64":  int64(7),
		"int32":  int32(7),
		"int":    7,
		"float":  float64(7),
		"string": "7",
		"bytes":  []byte(" 7 "),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := asInt64(in)
			if err != nil {
				t.Fatalf("asInt64(%T) = %v", in, err)
			}

			if got != 7 {
				t.Errorf("asInt64(%T) = %d, want 7", in, got)
			}
		})
	}

	// A float that is not a whole number is refused rather than truncated: an
	// id of 7.5 silently becoming 7 is a wrong row, not a rounding.
	if _, err := asInt64(7.5); err == nil {
		t.Error("asInt64 truncated 7.5 instead of refusing it")
	}

	/*
		Unsigned, which MySQL returns for ROW_NUMBER() and for any UNSIGNED
		column. The suite met its first one when the second connector arrived;
		PostgreSQL has no unsigned integers, so a reader written against it
		alone had never seen one.
	*/
	for name, in := range map[string]any{
		"uint64": uint64(7),
		"uint32": uint32(7),
		"uint":   uint(7),
	} {
		got, err := asInt64(in)
		if err != nil || got != 7 {
			t.Errorf("asInt64(%s) = %d, %v", name, got, err)
		}
	}

	// And one that does not fit is refused rather than wrapped. Converting it
	// blindly comes back negative, and a count that reads -9223372036854775808
	// is the kind of wrong that looks like data rather than like a bug.
	for name, in := range map[string]any{
		"uint64": uint64(math.MaxUint64),
		"uint":   uint(math.MaxUint64),
	} {
		if got, err := asInt64(in); err == nil {
			t.Errorf("asInt64(%s max) = %d, want a refusal", name, got)
		}
	}

	if _, err := asInt64("seven"); err == nil {
		t.Error("asInt64 accepted \"seven\"")
	}

	if _, err := asInt64(struct{}{}); err == nil {
		t.Error("asInt64 accepted a struct")
	}
}

func TestReadingRealNumbers(t *testing.T) {
	t.Parallel()

	for name, in := range map[string]any{
		"float64": float64(1.5),
		"float32": float32(1.5),
		"string":  "1.5",
		"bytes":   []byte("1.5"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := asFloat64(in)
			if err != nil || got != 1.5 {
				t.Errorf("asFloat64(%T) = %v, %v", in, got, err)
			}
		})
	}

	// NUMERIC arrives as an integer from some drivers and as text from others.
	if got, err := asFloat64(int64(2)); err != nil || got != 2 {
		t.Errorf("asFloat64(int64) = %v, %v", got, err)
	}

	if got, err := asFloat64(uint64(2)); err != nil || got != 2 {
		t.Errorf("asFloat64(uint64) = %v, %v", got, err)
	}

	if _, err := asFloat64("one point five"); err == nil {
		t.Error("asFloat64 accepted prose")
	}

	if _, err := asFloat64(nil); err == nil {
		t.Error("asFloat64 accepted nil, which would make a NULL look like zero")
	}
}

func TestReadingBooleans(t *testing.T) {
	t.Parallel()

	// "t" and "f" are PostgreSQL's text form; 0 and 1 are MySQL's TINYINT.
	for in, want := range map[any]bool{
		true:          true,
		false:         false,
		int64(1):      true,
		int64(0):      false,
		uint64(1):     true,
		uint64(0):     false,
		"t":           true,
		"F":           false,
		"TRUE":        true,
		"no":          false,
		string("yes"): true,
	} {
		got, err := asBool(in)
		if err != nil {
			t.Errorf("asBool(%#v) = %v", in, err)

			continue
		}

		if got != want {
			t.Errorf("asBool(%#v) = %t, want %t", in, got, want)
		}
	}

	if _, err := asBool("maybe"); err == nil {
		t.Error("asBool accepted \"maybe\"")
	}

	if _, err := asBool(1.5); err == nil {
		t.Error("asBool accepted a float")
	}
}

func TestReadingTimestamps(t *testing.T) {
	t.Parallel()

	want := time.Date(2024, time.March, 15, 23, 30, 0, 0, time.UTC)

	if got, err := asTime(want); err != nil || !got.Equal(want) {
		t.Errorf("asTime(time.Time) = %v, %v", got, err)
	}

	// The spellings drivers that return timestamps as text actually use.
	for _, text := range []string{
		"2024-03-15T23:30:00Z",
		"2024-03-15 23:30:00+00:00",
		"2024-03-15 23:30:00",
	} {
		got, err := asTime(text)
		if err != nil {
			t.Errorf("asTime(%q) = %v", text, err)

			continue
		}

		if wallClock(got) != "2024-03-15 23:30:00" {
			t.Errorf("asTime(%q) reads %s", text, wallClock(got))
		}
	}

	if _, err := asTime("the fifteenth"); err == nil {
		t.Error("asTime accepted prose")
	}

	if _, err := asTime(int64(1710545400)); err == nil {
		t.Error("asTime accepted a unix epoch, which has no zone and no agreed unit")
	}
}

// A clock reading is compared with the zone discarded, so two values that are
// the same instant in different zones read differently -- which is the whole
// point for a column stored without one.
func TestWallClockIgnoresTheZone(t *testing.T) {
	t.Parallel()

	east := time.FixedZone("east", 3*60*60)
	instant := time.Date(2024, time.March, 15, 23, 30, 0, 0, time.UTC)

	if got := wallClock(instant.In(east)); got != "2024-03-16 02:30:00" {
		t.Errorf("wallClock = %s, want the local reading", got)
	}
}
