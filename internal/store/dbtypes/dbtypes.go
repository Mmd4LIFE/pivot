// Package dbtypes holds column types that behave identically on PostgreSQL and
// SQLite.
//
// The two engines disagree about representation. PostgreSQL has real uuid,
// timestamptz, boolean and jsonb types; SQLite stores all of them as TEXT or
// INTEGER. Left alone, sqlc generates structurally different models per engine
// — string where the other has time.Time — and every consumer would need two
// code paths.
//
// These types absorb that difference at the driver boundary. Both generated
// packages are configured to use them, so their models have identical field
// types and the repository layer in Part 4 maps one shape, not two.
//
// This is the concrete form of the portability tax ADR-0003 accepted.
package dbtypes

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// TimeFormat is the canonical wire format for timestamps on SQLite: RFC3339
// with milliseconds, always UTC. It matches the strftime default in the SQLite
// migrations, so a row written by Go and a row written by a column DEFAULT are
// indistinguishable.
const TimeFormat = "2006-01-02T15:04:05.000Z"

// timeLayouts are tried in order when parsing a timestamp read back as text.
//
// More than one is needed because values can arrive from three places: written
// by this package, written by a SQLite column DEFAULT, or written by an older
// build. Being lenient on read and strict on write is the right asymmetry.
var timeLayouts = []string{
	TimeFormat,
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
}

// Time is a timestamp that round-trips on both engines.
//
// It always carries UTC: mixing zones in a metadata store is a source of
// off-by-one-day bugs that only appear for users in the wrong hemisphere.
type Time struct {
	time.Time
}

// NewTime wraps a [time.Time], normalizing to UTC and millisecond precision so
// that what is written is exactly what comes back.
func NewTime(t time.Time) Time {
	return Time{t.UTC().Truncate(time.Millisecond)}
}

// Now returns the current time, normalized.
func Now() Time { return NewTime(time.Now()) }

// Scan implements [sql.Scanner], accepting what either driver produces.
func (t *Time) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		t.Time = time.Time{}

		return nil
	case time.Time:
		t.Time = v.UTC()

		return nil
	case string:
		return t.parse(v)
	case []byte:
		return t.parse(string(v))
	default:
		return fmt.Errorf("dbtypes.Time: cannot scan %T", src)
	}
}

func (t *Time) parse(s string) error {
	for _, layout := range timeLayouts {
		if parsed, err := time.Parse(layout, s); err == nil {
			t.Time = parsed.UTC()

			return nil
		}
	}

	return fmt.Errorf("dbtypes.Time: cannot parse %q", s)
}

// Value implements [driver.Valuer].
//
// It returns a formatted string rather than a [time.Time]. PostgreSQL casts the
// text to timestamptz; SQLite stores it verbatim in the format its own column
// defaults use. Returning a time.Time instead would let each driver choose its
// own serialization, and SQLite's choice does not match the schema default —
// so identical logical values would be stored two different ways.
func (t Time) Value() (driver.Value, error) {
	return t.UTC().Format(TimeFormat), nil
}

// String implements [fmt.Stringer].
func (t Time) String() string { return t.UTC().Format(TimeFormat) }

// NullTime is a nullable [Time].
type NullTime struct {
	Time  Time
	Valid bool
}

// NewNullTime wraps a non-zero time as valid.
func NewNullTime(t time.Time) NullTime {
	if t.IsZero() {
		return NullTime{}
	}

	return NullTime{Time: NewTime(t), Valid: true}
}

// Scan implements [sql.Scanner].
func (n *NullTime) Scan(src any) error {
	if src == nil {
		n.Time, n.Valid = Time{}, false

		return nil
	}

	if err := n.Time.Scan(src); err != nil {
		return err
	}

	n.Valid = true

	return nil
}

// Value implements [driver.Valuer].
func (n NullTime) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}

	return n.Time.Value()
}

// Bool is a boolean that round-trips on both engines.
//
// PostgreSQL has a real boolean; SQLite stores 0 or 1 in an INTEGER column and
// hands it back as int64.
type Bool bool

// Scan implements [sql.Scanner].
func (b *Bool) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*b = false

		return nil
	case bool:
		*b = Bool(v)

		return nil
	case int64:
		*b = v != 0

		return nil
	case float64:
		*b = v != 0

		return nil
	case []byte:
		return b.parse(string(v))
	case string:
		return b.parse(v)
	default:
		return fmt.Errorf("dbtypes.Bool: cannot scan %T", src)
	}
}

func (b *Bool) parse(s string) error {
	parsed, err := strconv.ParseBool(s)
	if err != nil {
		return fmt.Errorf("dbtypes.Bool: cannot parse %q: %w", s, err)
	}

	*b = Bool(parsed)

	return nil
}

// Value implements [driver.Valuer].
func (b Bool) Value() (driver.Value, error) { return bool(b), nil }

// Bool returns the underlying boolean.
func (b Bool) Bool() bool { return bool(b) }

// JSON is a JSON document that round-trips on both engines.
//
// PostgreSQL uses jsonb and returns []byte; SQLite stores TEXT and returns a
// string. The zero value marshals as an empty object rather than SQL NULL,
// which matches the NOT NULL DEFAULT '{}' in both schemas.
type JSON []byte

// Scan implements [sql.Scanner].
func (j *JSON) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*j = nil

		return nil
	case []byte:
		*j = append((*j)[:0], v...)

		return nil
	case string:
		*j = []byte(v)

		return nil
	default:
		return fmt.Errorf("dbtypes.JSON: cannot scan %T", src)
	}
}

// Value implements [driver.Valuer].
func (j JSON) Value() (driver.Value, error) {
	if len(j) == 0 {
		return "{}", nil
	}

	if !json.Valid(j) {
		return nil, errors.New("dbtypes.JSON: value is not valid JSON")
	}

	// A string, not []byte: SQLite would otherwise store it as a BLOB, which
	// a STRICT TEXT column rejects.
	return string(j), nil
}

// Unmarshal decodes the document into v.
func (j JSON) Unmarshal(v any) error {
	if len(j) == 0 {
		return nil
	}

	return json.Unmarshal(j, v)
}

// MarshalJSONValue encodes v into a [JSON].
func MarshalJSONValue(v any) (JSON, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("dbtypes.JSON: marshal: %w", err)
	}

	return JSON(b), nil
}
