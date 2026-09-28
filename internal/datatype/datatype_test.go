package datatype_test

import (
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/datatype"
)

/*
The canonical type system.

The conformance suite checks these against four real databases, which is the
check that matters. What is here is the reasoning the databases cannot reach:
the spellings nobody has a server for, and the promises the kinds make to
whatever reads them.
*/

// The tidying before the lookup is where most source spellings are actually
// handled, and every entry below is one a real source produces.
func TestSourceSpellingsAreReducedToATypeName(t *testing.T) {
	t.Parallel()

	for source, want := range map[string]datatype.Kind{
		// Parameters, which a table keyed on full names would miss.
		"VARCHAR(255)":   datatype.String,
		"varchar(50)":    datatype.String,
		"NUMERIC(10, 2)": datatype.Decimal,
		"decimal(18,4)":  datatype.Decimal,
		"CHAR(3)":        datatype.String,
		"bigint(20)":     datatype.Integer,
		"timestamp(6)":   datatype.Timestamp,

		// Case, which differs between a catalog and a driver.
		"TEXT":             datatype.String,
		"text":             datatype.String,
		"Double Precision": datatype.Float,

		// MySQL writes the sign as part of the type.
		"bigint(20) unsigned": datatype.Integer,
		"INT UNSIGNED":        datatype.Integer,

		// Whitespace, which information_schema is generous with.
		"  timestamp without time zone  ": datatype.Timestamp,
	} {
		got := datatype.Normalize(source, nil)
		if got.Kind != want {
			t.Errorf("Normalize(%q) = %q, want %q", source, got.Kind, want)
		}

		if got.Source != source {
			t.Errorf("Normalize(%q) lost the source spelling: %q", source, got.Source)
		}
	}
}

/*
An array is an array however the source spells it.

PostgreSQL alone has three ways: its catalog says ARRAY, its driver prefixes
the element type with an underscore, and its own DDL writes a trailing [].
*/
func TestArraysAreRecognizedInEverySpelling(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"ARRAY", "_int4", "_text", "integer[]", "TEXT[]"} {
		if got := datatype.Normalize(source, nil); got.Kind != datatype.Array {
			t.Errorf("Normalize(%q) = %q, want array", source, got.Kind)
		}
	}
}

/*
A name nobody has mapped is Unknown, and keeps its spelling.

The spelling is the whole point: it is the search term for whoever adds the
mapping, and without it a gap in the type system is invisible.
*/
func TestAnUnmappedNameKeepsItsSpelling(t *testing.T) {
	t.Parallel()

	got := datatype.Normalize("st_geography", nil)

	if got.Kind != datatype.Unknown {
		t.Errorf("Normalize(st_geography) = %q, want unknown", got.Kind)
	}

	if got.Source != "st_geography" {
		t.Errorf("Source = %q, want the spelling back", got.Source)
	}

	if got.Known() {
		t.Error("Known() is true for an unmapped type")
	}
}

// A dialect's own table wins over the shared one, which is what lets MySQL say
// that TIMESTAMP is an instant where everybody else says it is not.
func TestADialectOverridesTheSharedTable(t *testing.T) {
	t.Parallel()

	inverted := func(name string) (datatype.Type, bool) {
		if name == "timestamp" {
			return datatype.Type{Kind: datatype.TimestampTZ}, true
		}

		return datatype.Type{}, false
	}

	if got := datatype.Normalize("TIMESTAMP", inverted); got.Kind != datatype.TimestampTZ {
		t.Errorf("a dialect override was ignored: got %q", got.Kind)
	}

	// And a name it does not claim still falls through.
	if got := datatype.Normalize("bigint", inverted); got.Kind != datatype.Integer {
		t.Errorf("fallthrough to the shared table failed: got %q", got.Kind)
	}
}

/*
Exactness is the distinction a total depends on.

Integer and Decimal are exact; Float is not. Anything summing a currency
column should be asking [Type.IsExact], and it has to get a different answer
for DECIMAL than for DOUBLE or the whole type system was pointless.
*/
func TestExactAndApproximateAreDifferentAnswers(t *testing.T) {
	t.Parallel()

	exact := datatype.Normalize("NUMERIC(10,2)", nil)
	approximate := datatype.Normalize("double precision", nil)

	if !exact.IsExact() {
		t.Error("NUMERIC(10,2) is not exact, so nothing can safely total money")
	}

	if approximate.IsExact() {
		t.Error("double precision reports itself exact, which is how 0.1 + 0.2 " +
			"becomes 0.30000000000000004 on somebody's invoice")
	}

	// Both are numeric, which is a weaker and separate question.
	if !exact.IsNumeric() || !approximate.IsNumeric() {
		t.Error("a decimal or a float is not numeric")
	}
}

// The zoned/naive distinction, which the conformance suite treats as two
// separate properties and this has to agree with.
func TestZonedAndNaiveTimestampsAreDifferentKinds(t *testing.T) {
	t.Parallel()

	naive := datatype.Normalize("timestamp without time zone", nil)
	zoned := datatype.Normalize("timestamp with time zone", nil)

	if naive.Kind == zoned.Kind {
		t.Fatalf("both normalized to %q, so an instant and a clock reading are "+
			"indistinguishable", naive.Kind)
	}

	for _, ty := range []datatype.Type{naive, zoned, datatype.Normalize("date", nil)} {
		if !ty.IsTemporal() {
			t.Errorf("%s is not temporal", ty)
		}
	}
}

func TestWidthIsCarriedWhereTheSourceSaysIt(t *testing.T) {
	t.Parallel()

	for source, want := range map[string]int{
		"smallint": 16,
		"int4":     32,
		"bigint":   64,
		"real":     32,
		"float8":   64,
	} {
		if got := datatype.Normalize(source, nil); got.Bits != want {
			t.Errorf("Normalize(%q).Bits = %d, want %d", source, got.Bits, want)
		}
	}

	// And left at zero where it does not, rather than guessed.
	if got := datatype.Normalize("numeric", nil); got.Bits != 0 {
		t.Errorf("numeric claims %d bits", got.Bits)
	}
}

// The predicates are what Phase 2 reads to pick a chart, so their edges are
// worth pinning: nothing that is not a number is numeric, and Unknown is not
// quietly text.
func TestThePredicatesDoNotOverreach(t *testing.T) {
	t.Parallel()

	unknown := datatype.Normalize("st_geography", nil)

	for name, got := range map[string]bool{
		"numeric":  unknown.IsNumeric(),
		"temporal": unknown.IsTemporal(),
		"text":     unknown.IsText(),
		"exact":    unknown.IsExact(),
	} {
		if got {
			t.Errorf("an unknown type reports itself %s", name)
		}
	}

	uuid := datatype.Normalize("uuid", nil)
	if uuid.IsNumeric() || uuid.IsText() {
		t.Error("a UUID is neither a number nor text: nothing aggregates it " +
			"and everything joins on it")
	}

	ts := datatype.Normalize("timestamptz", nil)
	if ts.IsNumeric() {
		t.Error("a timestamp is not a number; it subtracts to an interval")
	}
}

// A type renders readably, because it ends up in errors and logs.
func TestATypeSaysWhatItIsAndWhatItWas(t *testing.T) {
	t.Parallel()

	if got := datatype.Normalize("DOUBLE PRECISION", nil).String(); got != "float (DOUBLE PRECISION)" {
		t.Errorf("String() = %q", got)
	}

	if got := (datatype.Type{Kind: datatype.Integer}).String(); got != "integer" {
		t.Errorf("String() with no source = %q", got)
	}
}
