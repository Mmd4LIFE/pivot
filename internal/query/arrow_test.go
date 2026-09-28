package query_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"math"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	_ "modernc.org/sqlite"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/query"
)

/*
Rows into Arrow, in bounded memory.

The source is SQLite, so this runs with no container and no environment
variable -- which matters for the memory measurement more than anywhere else:
a benchmark that only runs when somebody remembers to start a database is a
benchmark nobody runs.
*/

// source opens a read-only connector over a SQLite file seeded with DDL.
func source(t *testing.T, statements ...string) connectors.Connector {
	t.Helper()

	path := filepath.Join(t.TempDir(), "source.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Something that touches the file, so it exists even when a caller wants
	// no tables: the connector opens read-only and will not create one.
	statements = append([]string{"PRAGMA user_version = 1"}, statements...)

	for _, statement := range statements {
		if _, err = db.ExecContext(t.Context(), statement); err != nil {
			_ = db.Close()
			t.Fatalf("%s: %v", statement, err)
		}
	}

	if err = db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	connector, err := connectors.Open(connectors.Config{
		Kind: connectors.KindSQLite, Database: path,
		// Above anything these tests ask for, so the cap is never what stops
		// a stream and the measurements are of streaming rather than of
		// truncation.
		MaxRows:             10_000_000,
		QueryTimeoutSeconds: 120,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = connector.Close() })

	return connector
}

// rows generates n rows of a few columns, wide enough that a batch is a
// meaningful amount of memory.
func rows(n int) string {
	return fmt.Sprintf(`
WITH RECURSIVE s(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM s WHERE i < %d)
SELECT i                              AS id,
       'row number ' || i             AS label,
       i * 1.5                        AS ratio,
       i %% 2                          AS flag,
       '2024-03-15 12:00:00+00:00'    AS seen_at
FROM s`, n)
}

/*
A result far larger than one batch is read in constant memory.

The property the whole part exists for, and the Done when is explicit that it
has to be *measured*: a test that reads ten million rows and asserts it did not
crash would pass against a materializing implementation on a machine with
enough memory.

So this measures peak heap while streaming ten times as many rows, and requires
that it does not scale with the result. A materializing read of 200,000 wide
rows is tens of megabytes; one batch of 4,096 is well under one.
*/
func TestALargeResultStreamsInConstantMemory(t *testing.T) {
	connector := source(t)

	const (
		small = 20_000
		large = 200_000
	)

	smallPeak := peakHeapWhileStreaming(t, connector, small)
	largePeak := peakHeapWhileStreaming(t, connector, large)

	t.Logf("peak heap: %d rows -> %.1f MB, %d rows -> %.1f MB",
		small, float64(smallPeak)/(1<<20), large, float64(largePeak)/(1<<20))

	// Ten times the rows must not be anything like ten times the memory. Two
	// times is a generous ceiling that a materializing implementation cannot
	// meet and a streaming one clears with room to spare.
	if largePeak > smallPeak*2 {
		t.Errorf("peak heap went from %.1f MB to %.1f MB for 10x the rows, "+
			"so the result is being assembled rather than streamed",
			float64(smallPeak)/(1<<20), float64(largePeak)/(1<<20))
	}

	// And an absolute ceiling, so that "constant" cannot mean "constantly
	// enormous". One batch of five columns is orders of magnitude under this.
	const ceiling = 64 << 20

	if largePeak > ceiling {
		t.Errorf("streaming %d rows peaked at %.1f MB, over the %d MB ceiling",
			large, float64(largePeak)/(1<<20), ceiling>>20)
	}
}

/*
peakHeapWhileStreaming reads n rows as Arrow batches and returns the peak heap.

Measured as the high-water mark across batches rather than the total allocated:
a stream that allocates and frees a batch ten thousand times has a large total
and a small footprint, and the footprint is the thing that decides whether a
container survives.
*/
func peakHeapWhileStreaming(t *testing.T, connector connectors.Connector, n int) uint64 {
	t.Helper()

	runtime.GC()

	var before runtime.MemStats

	runtime.ReadMemStats(&before)

	stream, err := connector.Stream(t.Context(), rows(n))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	defer func() { _ = stream.Close() }()

	batches := query.NewBatches(stream, query.DefaultBatchRows)
	defer batches.Release()

	var (
		peak uint64
		read int64
	)

	for {
		record := batches.Next()
		if record == nil {
			break
		}

		read += record.NumRows()

		var now runtime.MemStats

		runtime.ReadMemStats(&now)

		if inUse := now.HeapAlloc; inUse > peak {
			peak = inUse
		}

		// Released as soon as it has been counted, which is the contract: a
		// caller that keeps every batch has materialized the result by hand.
		record.Release()
	}

	if err := batches.Err(); err != nil {
		t.Fatalf("batches: %v", err)
	}

	if read != int64(n) {
		t.Fatalf("read %d rows, want %d", read, n)
	}

	if peak < before.HeapAlloc {
		return 0
	}

	return peak - before.HeapAlloc
}

// Every column arrives with the Arrow type its canonical kind maps to, and the
// source's own spelling is carried in the field metadata -- which is what lets
// an export say what the warehouse called a column.
func TestTheSchemaCarriesBothTypeAndSourceSpelling(t *testing.T) {
	connector := source(t, `CREATE TABLE things (
		id      INTEGER NOT NULL PRIMARY KEY,
		name    TEXT NOT NULL,
		ratio   REAL NOT NULL,
		flag    BOOLEAN NOT NULL,
		seen    TIMESTAMPTZ NOT NULL,
		naive   DATETIME NOT NULL
	)`)

	stream, err := connector.Stream(t.Context(), "SELECT * FROM things")
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	defer func() { _ = stream.Close() }()

	schema := query.SchemaOf(stream.Columns())

	want := map[string]arrow.Type{
		"id":    arrow.INT64,
		"name":  arrow.STRING,
		"ratio": arrow.FLOAT64,
		"flag":  arrow.BOOL,
		"seen":  arrow.TIMESTAMP,
		"naive": arrow.TIMESTAMP,
	}

	for name, kind := range want {
		idx := schema.FieldIndices(name)
		if len(idx) != 1 {
			t.Fatalf("no field named %q", name)
		}

		field := schema.Field(idx[0])

		if field.Type.ID() != kind {
			t.Errorf("%s is %s, want %s", name, field.Type.ID(), kind)
		}

		if got, ok := field.Metadata.GetValue("pivot.source_type"); !ok || got == "" {
			t.Errorf("%s carries no source type in its metadata", name)
		}
	}

	// The two timestamps differ in their zone, which is the distinction
	// internal/datatype exists to keep and which must survive into Arrow.
	zoned := schema.Field(schema.FieldIndices("seen")[0]).Type.(*arrow.TimestampType)
	naive := schema.Field(schema.FieldIndices("naive")[0]).Type.(*arrow.TimestampType)

	if zoned.TimeZone == naive.TimeZone {
		t.Errorf("an instant and a clock reading both became %q; the distinction was lost",
			zoned.TimeZone)
	}
}

/*
Every column is nullable in the schema, whatever the source claimed.

A source that reports NOT NULL and then returns a NULL is ordinary -- an outer
join, a view, a driver that declines to say. Arrow is within its rights to
panic on a null in a non-nullable column, and a panic inside a streaming export
is the worst place to discover the source was optimistic.
*/
func TestEveryColumnIsNullableInTheSchema(t *testing.T) {
	connector := source(t,
		"CREATE TABLE strict (id INTEGER NOT NULL PRIMARY KEY, name TEXT NOT NULL)")

	stream, err := connector.Stream(t.Context(), "SELECT * FROM strict")
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	defer func() { _ = stream.Close() }()

	schema := query.SchemaOf(stream.Columns())

	for i := range schema.NumFields() {
		if !schema.Field(i).Nullable {
			t.Errorf("%s is not nullable in the Arrow schema", schema.Field(i).Name)
		}
	}
}

// A NULL becomes an Arrow null rather than a zero, because a count over a
// column where a missing value became 0 is wrong by however many rows had no
// value and nothing says so.
func TestANullBecomesANullAndNotAZero(t *testing.T) {
	connector := source(t,
		"CREATE TABLE maybe (id INTEGER NOT NULL PRIMARY KEY, note TEXT, amount REAL)",
		"INSERT INTO maybe (id, note, amount) VALUES (1, 'here', 1.5)",
		"INSERT INTO maybe (id, note, amount) VALUES (2, NULL, NULL)",
	)

	stream, err := connector.Stream(t.Context(), "SELECT * FROM maybe ORDER BY id")
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	defer func() { _ = stream.Close() }()

	records, err := query.Collect(t.Context(), stream, 0)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()

	if len(records) != 1 {
		t.Fatalf("%d batches, want 1", len(records))
	}

	record := records[0]

	for _, name := range []string{"note", "amount"} {
		col := record.Column(record.Schema().FieldIndices(name)[0])

		if col.IsNull(0) {
			t.Errorf("%s row 0 is null, want a value", name)
		}

		if !col.IsNull(1) {
			t.Errorf("%s row 1 is not null; a missing value became a zero", name)
		}
	}
}

// A batch is at most the size it was asked for, which is the whole mechanism
// behind the memory claim.
func TestBatchesAreBounded(t *testing.T) {
	connector := source(t)

	stream, err := connector.Stream(t.Context(), rows(1000))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	defer func() { _ = stream.Close() }()

	batches := query.NewBatches(stream, 128)
	defer batches.Release()

	var total, count int64

	for {
		record := batches.Next()
		if record == nil {
			break
		}

		if record.NumRows() > 128 {
			t.Errorf("a batch carried %d rows, over the 128 asked for", record.NumRows())
		}

		total += record.NumRows()
		count++

		record.Release()
	}

	if err := batches.Err(); err != nil {
		t.Fatalf("batches: %v", err)
	}

	if total != 1000 {
		t.Errorf("read %d rows, want 1000", total)
	}

	if count < 2 {
		t.Errorf("1000 rows came back in %d batch(es); the size was not applied", count)
	}
}

// Conversion stops at the first value it cannot represent rather than writing
// a null, because a null here is indistinguishable from a NULL in the source.
func TestAValueThatWillNotConvertIsAnError(t *testing.T) {
	connector := source(t,
		// Declared INTEGER, holding text: SQLite allows it, and it is exactly
		// the shape of a source whose declared types are aspirational.
		"CREATE TABLE loose (id INTEGER NOT NULL PRIMARY KEY, n INTEGER)",
		"INSERT INTO loose (id, n) VALUES (1, 'not a number')",
	)

	stream, err := connector.Stream(t.Context(), "SELECT * FROM loose")
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	defer func() { _ = stream.Close() }()

	_, err = query.Collect(t.Context(), stream, 0)
	if err == nil {
		t.Fatal("a value that is not a number was accepted into an integer column")
	}

	t.Logf("refused, as it must be: %v", err)
}

// The truncation flag survives the conversion, because a chart drawn from a
// partial answer is wrong in a way nobody can see.
func TestTruncationSurvivesTheConversion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capped.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err = db.ExecContext(t.Context(), "CREATE TABLE t (i INTEGER)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	_ = db.Close()

	connector, err := connectors.Open(connectors.Config{
		Kind: connectors.KindSQLite, Database: path,
		MaxRows: 50, QueryTimeoutSeconds: 30,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer func() { _ = connector.Close() }()

	stream, err := connector.Stream(t.Context(), rows(500))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	defer func() { _ = stream.Close() }()

	batches := query.NewBatches(stream, 16)
	defer batches.Release()

	var total int64

	for {
		record := batches.Next()
		if record == nil {
			break
		}

		total += record.NumRows()

		record.Release()
	}

	if total != 50 {
		t.Errorf("read %d rows against a cap of 50", total)
	}

	if !batches.Truncated() {
		t.Error("the result was cut at the cap and the batches do not say so")
	}
}

/*
A SQLite integer larger than four bytes survives.

The regression test for a bug this part found in Part 19-a's type mapping.
SQLite's INTEGER is a variable-width storage class holding up to eight bytes,
and REAL is always an eight-byte double -- but the shared type table's widths
are PostgreSQL's, where `integer` is four bytes and `real` is four.

Left alone, a SQLite id above two billion mapped to an Arrow int32. The
conversion refuses a value that does not fit rather than truncating it, which
is what turned a silent wrong number into a loud failure and is how this was
found at all. A mapping that had quietly truncated would have produced an id
wrong by four billion and nothing to notice it.
*/
func TestALargeSQLiteIntegerSurvives(t *testing.T) {
	const big = int64(9_000_000_000) // Comfortably past a signed 32-bit int.

	connector := source(t,
		"CREATE TABLE wide (id INTEGER NOT NULL PRIMARY KEY, amount REAL NOT NULL)",
		fmt.Sprintf("INSERT INTO wide (id, amount) VALUES (%d, 1.7976931348623157e308)", big),
	)

	stream, err := connector.Stream(t.Context(), "SELECT * FROM wide")
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	defer func() { _ = stream.Close() }()

	records, err := query.Collect(t.Context(), stream, 0)
	if err != nil {
		t.Fatalf("a large SQLite integer could not be converted: %v", err)
	}

	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()

	if len(records) != 1 || records[0].NumRows() != 1 {
		t.Fatalf("got %d batches", len(records))
	}

	record := records[0]

	ids, ok := record.Column(record.Schema().FieldIndices("id")[0]).(*array.Int64)
	if !ok {
		t.Fatalf("id is %T, want an Arrow int64", record.Column(0))
	}

	if ids.Value(0) != big {
		t.Errorf("id came back as %d, want %d", ids.Value(0), big)
	}

	// And a double that only fits in eight bytes.
	amounts, ok := record.Column(record.Schema().FieldIndices("amount")[0]).(*array.Float64)
	if !ok {
		t.Fatalf("amount is %T, want an Arrow float64",
			record.Column(record.Schema().FieldIndices("amount")[0]))
	}

	if amounts.Value(0) == 0 || math.IsInf(amounts.Value(0), 0) {
		t.Errorf("amount came back as %v; an eight-byte double did not survive",
			amounts.Value(0))
	}
}
