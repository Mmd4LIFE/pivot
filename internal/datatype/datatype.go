/*
Package datatype is what Pivot means by a type, independent of who said it.

Four connectors describe the same column four ways, and two of them describe it
two ways each: PostgreSQL's catalog says "timestamp with time zone" where its
driver says "TIMESTAMPTZ", and for `timetz` the driver says "1266" because it
has no name for it at all. Everything downstream needs one answer.

# What reads this

Phase 2 picks a chart from it -- a temporal column gets a line, a categorical
one gets bars. Phase 3's semantic layer decides what can be summed. Phase 7
grounds the AI in it. Part 24 formats a cell from it. Four consumers, none of
which should be parsing "double precision" for itself.

That is also the discipline: every distinction here has a consumer that would
be wrong without it. A type system with forty kinds nobody reads is as useless
as one that says "string" for everything, and it is harder to delete.

# The two that are not negotiable

**Exact and approximate numbers are different kinds.** DECIMAL(10,2) is money
and DOUBLE is not. A system that conflates them is how a total of 0.1 and 0.2
renders as 0.30000000000000004 in front of somebody doing the accounts.

**Zoned and naive timestamps are different kinds.** The conformance suite
already treats them as separate properties -- one keeps an instant, the other
keeps a clock reading -- and a type system that disagreed with it would be
wrong somewhere.

# Unknown is a real answer

A type nobody has mapped becomes [Unknown], keeping the source's own spelling.
Not a guess at String: a guess is indistinguishable from knowledge at the point
where it matters, which is the point where somebody charts it.

Unknown means Pivot has no canonical meaning for this and consumers should show
it as text and not compute with it. That is a useful thing to be able to say,
and it is also how the gaps stay findable -- [Type.Source] is the search term
for whoever adds the mapping.
*/
package datatype

import "strings"

// Kind is a canonical type.
//
// The string values are stored in the catalog and cross the API, so a value
// here is permanent: renaming one orphans every row that used it.
type Kind string

const (
	// Unknown is a source type Pivot has no canonical meaning for. It carries
	// the source's spelling in [Type.Source]; show it as text, do not compute
	// with it.
	Unknown Kind = "unknown"

	// Boolean is true or false.
	Boolean Kind = "boolean"

	// Integer is a whole number. [Type.Bits] says how wide, where the source
	// says.
	Integer Kind = "integer"

	// Decimal is an *exact* number -- NUMERIC, DECIMAL, money. Summing these
	// is safe; summing Floats is not.
	Decimal Kind = "decimal"

	// Float is an approximate number: REAL, DOUBLE PRECISION. Fine for a
	// ratio, wrong for a currency total.
	Float Kind = "float"

	// String is text.
	String Kind = "string"

	// Binary is bytes with no encoding.
	Binary Kind = "binary"

	// Date is a calendar day with no time.
	Date Kind = "date"

	// Time is a time of day with no date.
	Time Kind = "time"

	// Timestamp is a wall-clock reading with no zone. It is a moment on a
	// calendar, not a moment in history.
	Timestamp Kind = "timestamp"

	// TimestampTZ is an instant, which is a different thing from Timestamp and
	// is the distinction most often got wrong.
	TimestampTZ Kind = "timestamp_tz"

	// Interval is a duration.
	Interval Kind = "interval"

	// JSON is a document.
	JSON Kind = "json"

	// UUID is a UUID, which is worth distinguishing from String because
	// nothing sensible aggregates it and everything sensible joins on it.
	UUID Kind = "uuid"

	// Array is a repeated value.
	Array Kind = "array"

	// Struct is a nested record.
	Struct Kind = "struct"
)

/*
Type is a canonical kind plus what the source called it.

Source is kept always, including when the kind is known. Part 19's
normalization will be wrong about something, and the original spelling is the
only way to find out what -- the same reason [connectors.Column] keeps it.
*/
type Type struct {
	Kind Kind

	// Source is the source's own spelling, verbatim.
	Source string

	// Bits is the width of an Integer or a Float where the source says --
	// 16, 32, 64. Zero means it did not.
	Bits int
}

// String renders the type for a log or an error.
func (t Type) String() string {
	if t.Source == "" {
		return string(t.Kind)
	}

	return string(t.Kind) + " (" + t.Source + ")"
}

// IsNumeric says whether arithmetic on this means anything.
//
// Deliberately includes Decimal and Float and excludes everything else: a
// timestamp subtracts to an interval rather than to a number, and a UUID does
// not average.
func (t Type) IsNumeric() bool {
	switch t.Kind {
	case Integer, Decimal, Float:
		return true
	default:
		return false
	}
}

// IsExact says whether arithmetic on this is exact.
//
// The check anything totalling money should make. Integer and Decimal are
// exact; Float is not, and the difference shows up in the last two digits of a
// number somebody is going to reconcile.
func (t Type) IsExact() bool {
	switch t.Kind {
	case Integer, Decimal:
		return true
	default:
		return false
	}
}

// IsTemporal says whether this sits on a timeline. What Phase 2 reads when it
// reaches for a line chart.
func (t Type) IsTemporal() bool {
	switch t.Kind {
	case Date, Time, Timestamp, TimestampTZ:
		return true
	default:
		return false
	}
}

// IsText says whether this is characters.
func (t Type) IsText() bool {
	return t.Kind == String
}

// Known says whether Pivot has a canonical meaning for this.
func (t Type) Known() bool {
	return t.Kind != Unknown && t.Kind != ""
}

// --- reading a source's spelling --------------------------------------------

/*
Base maps the type names the SQL standard gave everybody.

Every dialect starts here and overrides what it spells differently, because
four copies of "bigint is an integer" is three copies too many -- and because a
name this misses in one dialect is a name it probably misses in all of them.

The match is on the bare name: parameters, array suffixes and modifiers are
stripped by [Normalize] before this is consulted.
*/
func Base(name string) (Type, bool) {
	entry, ok := baseKinds[name]
	if !ok {
		return Type{}, false
	}

	return Type{Kind: entry.kind, Bits: entry.bits}, true
}

// baseKinds is the shared vocabulary, keyed by the bare lower-cased name.
var baseKinds = map[string]struct {
	kind Kind
	bits int
}{
	// Whole numbers, in both the standard spellings and the ones PostgreSQL's
	// driver uses.
	"smallint": {Integer, 16}, "int2": {Integer, 16}, "tinyint": {Integer, 8},
	"integer": {Integer, 32}, "int": {Integer, 32}, "int4": {Integer, 32},
	"mediumint": {Integer, 32},
	"bigint":    {Integer, 64}, "int8": {Integer, 64},

	// Exact.
	"decimal": {Decimal, 0}, "numeric": {Decimal, 0},
	"money": {Decimal, 0}, "number": {Decimal, 0},

	// Approximate.
	"real": {Float, 32}, "float4": {Float, 32},
	"double": {Float, 64}, "double precision": {Float, 64}, "float8": {Float, 64},
	"float": {Float, 64},

	"boolean": {Boolean, 0}, "bool": {Boolean, 0},

	"text": {String, 0}, "varchar": {String, 0}, "character varying": {String, 0},
	"char": {String, 0}, "character": {String, 0}, "bpchar": {String, 0},
	"nvarchar": {String, 0}, "nchar": {String, 0}, "string": {String, 0},
	"clob": {String, 0}, "longtext": {String, 0}, "mediumtext": {String, 0},
	"tinytext": {String, 0},

	"bytea": {Binary, 0}, "blob": {Binary, 0}, "binary": {Binary, 0},
	"varbinary": {Binary, 0}, "longblob": {Binary, 0}, "mediumblob": {Binary, 0},
	"tinyblob": {Binary, 0},

	"date": {Date, 0},
	"time": {Time, 0}, "time without time zone": {Time, 0},

	// The distinction this package exists to keep.
	"timestamp": {Timestamp, 0}, "timestamp without time zone": {Timestamp, 0},
	"datetime":    {Timestamp, 0},
	"timestamptz": {TimestampTZ, 0}, "timestamp with time zone": {TimestampTZ, 0},

	"interval": {Interval, 0},
	"json":     {JSON, 0}, "jsonb": {JSON, 0},
	"uuid": {UUID, 0},
}

/*
Normalize reads a source type through a dialect's own table, then the shared
one.

The tidying before the lookup is the part that earns its keep. Sources spell
the same type as "VARCHAR(50)", "varchar", "character varying(50)" and
"NUMERIC(10, 2)", and a table with an entry per length is a table that is
missing the length somebody used.

A name nothing recognizes comes back [Unknown] with its spelling intact.
*/
func Normalize(source string, dialect func(string) (Type, bool)) Type {
	bare := bareName(source)

	// PostgreSQL's driver names an array by prefixing the element type with an
	// underscore -- _int4 is an integer array -- and its catalog just says
	// ARRAY. Both are arrays, and neither says so in a way a table can hold.
	if bare == "array" || strings.HasPrefix(bare, "_") {
		return Type{Kind: Array, Source: source}
	}

	if dialect != nil {
		if t, ok := dialect(bare); ok {
			t.Source = source

			return t
		}
	}

	if t, ok := Base(bare); ok {
		t.Source = source

		return t
	}

	return Type{Kind: Unknown, Source: source}
}

/*
bareName reduces a source's spelling to the name a table can be keyed on.

Lower-cased, parameters dropped, and the modifiers that follow a name in some
dialects -- `unsigned`, `zerofill` in MySQL, a trailing `[]` in PostgreSQL --
removed. What is left is the type, which is the only part a canonical kind
depends on.
*/
func bareName(source string) string {
	name := strings.ToLower(strings.TrimSpace(source))

	// A trailing [] is PostgreSQL's other way of writing an array.
	if strings.HasSuffix(name, "[]") {
		return "_" + strings.TrimSpace(strings.TrimSuffix(name, "[]"))
	}

	if open := strings.IndexByte(name, '('); open >= 0 {
		closing := strings.LastIndexByte(name, ')')
		if closing > open {
			// Keep whatever followed the parentheses -- "double precision" has
			// none, but "bigint(20) unsigned" does and the modifier is dropped
			// below rather than here.
			name = strings.TrimSpace(name[:open]) + " " + strings.TrimSpace(name[closing+1:])
		} else {
			name = strings.TrimSpace(name[:open])
		}
	}

	// MySQL writes width and sign as part of the type: "bigint(20) unsigned".
	// The sign is dropped because Pivot has no unsigned kind -- a uint64 that
	// does not fit an int64 is refused rather than wrapped, which the
	// conformance readers already do.
	//
	// "precision" is deliberately not in this list: "double precision" is a
	// name, not a modifier.
	for _, modifier := range []string{" unsigned", " signed", " zerofill"} {
		name = strings.ReplaceAll(name, modifier, "")
	}

	return strings.Join(strings.Fields(name), " ")
}
