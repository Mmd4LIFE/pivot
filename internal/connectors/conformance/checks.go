package conformance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
)

// --- reaching the source ----------------------------------------------------

func checkConnects(ctx context.Context, s Subject) error {
	if err := s.Connector.Test(ctx); err != nil {
		return fmt.Errorf("Test: %w", err)
	}

	return nil
}

// --- values -----------------------------------------------------------------

/*
Everything that is not a string, a NULL or a timestamp survives the trip.

Ordering is part of it: a driver that returns the right three rows in the wrong
order has broken every query with an ORDER BY, which is most of them.
*/
func checkValues(ctx context.Context, s Subject) error {
	rows, err := s.fetch(ctx)
	if err != nil {
		return err
	}

	for i, want := range Rows() {
		id, err := rows.int64At(i, "id")
		if err != nil {
			return err
		}

		if id != want.ID {
			return fmt.Errorf("row %d has id %d, want %d -- the rows are not in id order", i, id, want.ID)
		}

		flag, err := rows.boolAt(i, "flag")
		if err != nil {
			return err
		}

		if flag != want.Flag {
			return fmt.Errorf("id %d: flag is %t, want %t", want.ID, flag, want.Flag)
		}

		ratio, err := rows.float64At(i, "ratio")
		if err != nil {
			return err
		}

		// Exact. Every value in the fixture is representable in binary
		// floating point, so a difference here is a real loss of precision
		// rather than the usual decimal-printing noise.
		if ratio != want.Ratio {
			return fmt.Errorf("id %d: ratio is %v, want %v", want.ID, ratio, want.Ratio)
		}
	}

	return nil
}

/*
NULL is nil, and the empty string is not.

The single most common connector bug, and the one that does the quietest
damage: a COUNT over a column where a missing value became "" is wrong by
however many rows had no value, and nothing anywhere says so.
*/
func checkNullVersusEmpty(ctx context.Context, s Subject) error {
	rows, err := s.fetch(ctx)
	if err != nil {
		return err
	}

	for i, want := range Rows() {
		cell, err := rows.at(i, "notes")
		if err != nil {
			return err
		}

		switch {
		case want.Notes == nil && cell != nil:
			return fmt.Errorf("id %d: notes is NULL in the source and came back as %#v (%T)",
				want.ID, cell, cell)

		case want.Notes != nil && cell == nil:
			return fmt.Errorf("id %d: notes is %q in the source and came back as nil", want.ID, *want.Notes)

		case want.Notes != nil:
			got, serr := asString(cell)
			if serr != nil {
				return fmt.Errorf("id %d: notes: %w", want.ID, serr)
			}

			if got != *want.Notes {
				return fmt.Errorf("id %d: notes is %q, want %q", want.ID, got, *want.Notes)
			}
		}
	}

	return nil
}

/*
Text comes back as the bytes that went in.

Not "looks the same": byte-identical. A connector that normalizes NFD to NFC
produces a string that renders identically and compares unequal, which turns
into a join that silently matches nothing.
*/
func checkUnicode(ctx context.Context, s Subject) error {
	rows, err := s.fetch(ctx)
	if err != nil {
		return err
	}

	for i, want := range Rows() {
		got, err := rows.stringAt(i, "name")
		if err != nil {
			return err
		}

		if got == want.Name {
			continue
		}

		if len(got) != len(want.Name) {
			return fmt.Errorf("id %d: name came back as %d bytes, want %d -- %q, not %q",
				want.ID, len(got), len(want.Name), got, want.Name)
		}

		return fmt.Errorf("id %d: name is %q (% x), want %q (% x)",
			want.ID, got, got, want.Name, want.Name)
	}

	return nil
}

/*
A timestamp with a zone keeps its instant; one without keeps its clock face.

Two different promises, and confusing them is how "dates are off by one"
happens. The fixture's row 1 sits at 23:30 UTC precisely so that a connector
which applies a session timezone to a zoned value lands on the wrong date
rather than merely the wrong hour.
*/
func checkTimestamps(ctx context.Context, s Subject) error {
	rows, err := s.fetch(ctx)
	if err != nil {
		return err
	}

	for i, want := range Rows() {
		zoned, err := rows.timeAt(i, "created_utc")
		if err != nil {
			return err
		}

		if !zoned.Equal(want.CreatedUTC) {
			return fmt.Errorf("id %d: created_utc is %s, want the instant %s (off by %s)",
				want.ID, zoned.Format(time.RFC3339Nano), want.CreatedUTC.Format(time.RFC3339Nano),
				zoned.Sub(want.CreatedUTC))
		}

		naive, err := rows.timeAt(i, "created_naive")
		if err != nil {
			return err
		}

		// Compared as a clock reading with the zone discarded, because a value
		// stored without one does not have an instant to be equal to. Whatever
		// location the driver decided to attach on the way out, these are the
		// numbers that have to come back.
		if wallClock(naive) != wallClock(want.CreatedNaive) {
			return fmt.Errorf("id %d: created_naive reads %s, want %s -- a zone was applied to a value that has none",
				want.ID, wallClock(naive), wallClock(want.CreatedNaive))
		}
	}

	return nil
}

// --- metadata ---------------------------------------------------------------

/*
A result describes its own columns.

Everything downstream -- the catalog, the compiler, the grid -- reads these
rather than guessing from the values, and a column with no name or no source
type is a column nothing above can render or reason about.
*/
func checkColumnMetadata(ctx context.Context, s Subject) error {
	rows, err := s.fetch(ctx)
	if err != nil {
		return err
	}

	want := Columns()

	if len(rows.result.Columns) != len(want) {
		return fmt.Errorf("the result describes %d columns, want %d",
			len(rows.result.Columns), len(want))
	}

	for i, col := range rows.result.Columns {
		if !strings.EqualFold(col.Name, want[i]) {
			return fmt.Errorf("column %d is named %q, want %q -- the order or the names are wrong",
				i, col.Name, want[i])
		}

		if col.SourceType == "" {
			return fmt.Errorf("column %q reports no source type, so nothing above this can tell a date from a number",
				col.Name)
		}

		if col.Position != i+1 {
			return fmt.Errorf("column %q reports position %d, want %d", col.Name, col.Position, i+1)
		}
	}

	return nil
}

/*
Introspection finds the fixture, with its columns and their nullability.

Nullability is checked rather than merely present: it is the one piece of
catalog metadata that changes what generated SQL is correct, and a source that
reports everything as nullable is indistinguishable from one that does not
report it at all.
*/
func checkIntrospection(ctx context.Context, s Subject) error {
	tables, err := s.Connector.Introspect(ctx)
	if err != nil {
		return fmt.Errorf("Introspect: %w", err)
	}

	var found *connectors.Table

	for i := range tables {
		t := &tables[i]
		if t.Name == s.Fixture.Name && (s.Fixture.Schema == "" || t.Schema == s.Fixture.Schema) {
			found = t

			break
		}
	}

	if found == nil {
		return fmt.Errorf("introspection returned %d tables and none of them is %s -- a table that exists and cannot be cataloged is invisible to everything above",
			len(tables), s.qualifiedFixture())
	}

	byName := map[string]connectors.Column{}
	for _, col := range found.Columns {
		byName[strings.ToLower(col.Name)] = col
	}

	for _, want := range Columns() {
		col, ok := byName[want]
		if !ok {
			return fmt.Errorf("%s: introspection does not report a column %q", s.qualifiedFixture(), want)
		}

		if col.SourceType == "" {
			return fmt.Errorf("%s: column %q is cataloged with no source type", s.qualifiedFixture(), want)
		}
	}

	if !byName["notes"].Nullable {
		return fmt.Errorf("%s: notes is declared NULL in the fixture and is cataloged as NOT NULL", s.qualifiedFixture())
	}

	if byName["id"].Nullable {
		return fmt.Errorf("%s: id is declared NOT NULL in the fixture and is cataloged as nullable", s.qualifiedFixture())
	}

	return nil
}

// --- declarations the compiler will trust -----------------------------------

/*
The placeholder style a connector declares is the one the source accepts.

[connectors.Capabilities.Placeholder] is what the query compiler will read to
decide how to write a bound parameter, so a wrong answer here is not a bug in
this connector -- it is SQL the source rejects, emitted by a component that
never touched this driver.
*/
func checkPlaceholder(ctx context.Context, s Subject) error {
	style := s.Connector.Capabilities().Placeholder

	var (
		query string
		args  []any
	)

	switch style {
	case connectors.PlaceholderDollar:
		// The arguments are referenced deliberately out of order. A driver
		// that declares a numbered style but binds positionally gets them
		// swapped, which this notices and a same-order query would not.
		query = fmt.Sprintf("SELECT id FROM %s WHERE id = %s AND name <> %s",
			s.Fixture.Table, style.Format(2), style.Format(1))
		args = []any{"no row has this name", int64(2)}

	case connectors.PlaceholderQuestion:
		// Nothing to get wrong about ordering, so it is written the only way
		// a positional style can be written.
		query = fmt.Sprintf("SELECT id FROM %s WHERE id = %s AND name <> %s",
			s.Fixture.Table, style.Format(1), style.Format(2))
		args = []any{int64(2), "no row has this name"}

	default:
		return fmt.Errorf("the declared placeholder style is %q, which is not one this build knows how to write", style)
	}

	result, err := s.Connector.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("the declared placeholder style %q produced SQL the source rejected: %w", style, err)
	}

	if len(result.Rows) != 1 {
		return fmt.Errorf("binding id = 2 returned %d rows, want 1 -- the arguments did not land where the query put them",
			len(result.Rows))
	}

	got, err := asInt64(result.Rows[0][0])
	if err != nil {
		return fmt.Errorf("id: %w", err)
	}

	if got != 2 {
		return fmt.Errorf("binding id = 2 returned id %d -- the arguments were bound in the wrong order", got)
	}

	return nil
}

/*
Every declared capability is demonstrated rather than asserted.

Capabilities are declared by hand because discovery would mean probe queries on
every connect. The cost of that choice is that a field can be wrong, and a
wrong one is not discovered here -- it is discovered when the query compiler
emits a CTE against a source that has none, in front of whoever built the
dashboard.

A capability the suite cannot demonstrate is a failure, not a pass. The subject
supplies the SQL or the connector stops claiming the capability; quietly
declaring something unverifiable is the one outcome worth refusing.
*/
func checkCapabilities(ctx context.Context, s Subject) error {
	caps := s.Connector.Capabilities()

	if caps.QuoteIdentifier == nil {
		return errors.New("QuoteIdentifier is nil, so anything generating SQL for this source has to invent its own quoting")
	}

	if caps.MaxIdentifierLength <= 0 {
		return errors.New("MaxIdentifierLength is not set, so generated aliases have no length to respect and two columns can collide into one name")
	}

	if caps.CTEs {
		query := fmt.Sprintf("WITH c AS (SELECT id FROM %s) SELECT id FROM c ORDER BY id", s.Fixture.Table)
		if err := s.expectIDs(ctx, query, []int64{1, 2, 3}); err != nil {
			return fmt.Errorf("CTEs is declared true: %w", err)
		}
	}

	if caps.WindowFunctions {
		query := fmt.Sprintf("SELECT ROW_NUMBER() OVER (ORDER BY id) AS rn FROM %s", s.Fixture.Table)
		if err := s.expectIDs(ctx, query, []int64{1, 2, 3}); err != nil {
			return fmt.Errorf("WindowFunctions is declared true: %w", err)
		}
	}

	if caps.LateralJoins {
		if s.SQL.LateralJoin == "" {
			return errors.New("LateralJoins is declared true and the subject supplies no lateral query to prove it -- lateral syntax varies too much to write here, so an unproven claim is refused")
		}

		if err := s.expectIDs(ctx, s.SQL.LateralJoin, []int64{1, 2, 3}); err != nil {
			return fmt.Errorf("LateralJoins is declared true: %w", err)
		}
	}

	return nil
}

/*
The declared quoting rule is the one the source applies.

Checked through a column alias rather than a table name, because an alias needs
no DDL and works on a read-only source. The source parses the quoted
identifier and hands the name straight back, so a rule that fails to escape an
embedded quote produces a syntax error and one that mangles the name produces a
mismatch. Both are the injection this function exists to prevent.
*/
func checkQuoting(ctx context.Context, s Subject) error {
	raw := s.SQL.AwkwardIdentifier
	quoted := s.Connector.Capabilities().QuoteIdentifier(raw)

	if quoted == raw {
		return fmt.Errorf("QuoteIdentifier left %q unchanged, which cannot be right for a name that is only legal quoted", raw)
	}

	query := fmt.Sprintf("SELECT id AS %s FROM %s ORDER BY id", quoted, s.Fixture.Table)

	result, err := s.Connector.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("QuoteIdentifier turned %q into %s, which the source rejected: %w", raw, quoted, err)
	}

	if len(result.Columns) != 1 {
		return fmt.Errorf("aliasing to %s returned %d columns, want 1", quoted, len(result.Columns))
	}

	if got := result.Columns[0].Name; got != raw {
		return fmt.Errorf("aliased to %q via %s and the source called it %q -- the quoting rule does not survive a round trip",
			raw, quoted, got)
	}

	return nil
}

// --- size -------------------------------------------------------------------

/*
A result under the cap arrives whole and in order.

The values are checked rather than only the count. A driver that streams in
batches can drop or repeat a row at a batch boundary, and a row count alone
cannot tell the difference between the right ten thousand rows and ten
thousand copies of the first one.
*/
func checkLargeResult(ctx context.Context, s Subject) error {
	n := s.maxRows() - 1
	if n > largeResultRows {
		n = largeResultRows
	}

	if n < 1 {
		return fmt.Errorf("the connector's row cap is %d, which leaves no room to stream a result under it", s.maxRows())
	}

	result, err := s.Connector.Query(ctx, s.SQL.Series(int(n)))
	if err != nil {
		return fmt.Errorf("selecting %d rows: %w", n, err)
	}

	if result.Truncated {
		return fmt.Errorf("%d rows is under the cap of %d and the result is flagged truncated", n, s.maxRows())
	}

	if int64(len(result.Rows)) != n {
		return fmt.Errorf("asked for %d rows and got %d", n, len(result.Rows))
	}

	for i, row := range result.Rows {
		got, cerr := asInt64(row[0])
		if cerr != nil {
			return fmt.Errorf("row %d: %w", i, cerr)
		}

		if got != int64(i+1) {
			return fmt.Errorf("row %d is %d, want %d -- rows were dropped, repeated or reordered somewhere in the stream",
				i, got, i+1)
		}
	}

	return nil
}

// largeResultRows caps how big "large" gets, so a subject with a hundred
// thousand row limit does not make the suite slow enough that people stop
// running it.
const largeResultRows = 5_000

/*
A result at the cap is cut and says so.

Truncation is the failure mode with no visible symptom: a chart drawn from the
first ten thousand of two million rows is a wrong chart that looks exactly like
a right one. The flag is the whole point, and a connector that stops at the
limit without setting it has done the more dangerous half of the job.
*/
func checkTruncation(ctx context.Context, s Subject) error {
	limit := s.maxRows()

	result, err := s.Connector.Query(ctx, s.SQL.Series(int(limit+10)))
	if err != nil {
		return fmt.Errorf("selecting %d rows: %w", limit+10, err)
	}

	if int64(len(result.Rows)) != limit {
		return fmt.Errorf("asked for %d rows against a cap of %d and got %d",
			limit+10, limit, len(result.Rows))
	}

	if !result.Truncated {
		return fmt.Errorf("the result was cut to the cap of %d rows and is not flagged truncated, so everything above this will treat a partial answer as the whole one",
			limit)
	}

	return nil
}

/*
The connector is safe to use from several goroutines at once.

[connectors.Connector] promises it -- the pool underneath it is the point --
and everything above depends on it: one stored connection serves every person
in an organization, concurrently, forever.

Worth proving rather than assuming since cancellation grew a per-query
connection and a watcher goroutine. A pool that hands one connection to two
queries, a watcher that outlives what it was watching, or a driver that is
simply not concurrent shows up here and in none of the checks above, all of
which run one query at a time.
*/
func checkConcurrency(ctx context.Context, s Subject) error {
	const goroutines = 16

	failures := make(chan error, goroutines)

	for range goroutines {
		go func() {
			// The whole fixture, because it is the one query this suite knows
			// every subject can answer -- and reading seven columns of three
			// rows gives cross-talk between connections somewhere to show.
			rows, err := s.fetch(ctx)
			if err != nil {
				failures <- err

				return
			}

			for i, want := range Rows() {
				id, ierr := rows.int64At(i, "id")
				if ierr != nil {
					failures <- ierr

					return
				}

				if id != want.ID {
					failures <- fmt.Errorf("row %d came back as id %d, want %d", i, id, want.ID)

					return
				}
			}

			failures <- nil
		}()
	}

	for range goroutines {
		if err := <-failures; err != nil {
			return fmt.Errorf("%d queries at once: %w", goroutines, err)
		}
	}

	return nil
}

// --- stopping ---------------------------------------------------------------

/*
A canceled query stops soon, says it was canceled, and leaves the pool usable.

All three matter. A cancellation that is not prompt means a closed browser tab
leaves a query running; one reported as an unknown failure sends whoever reads
the log looking for a bug that is not there; and one that poisons the pool
turns a single abandoned query into an outage.
*/
func checkCancellation(ctx context.Context, s Subject) error {
	canceling, cancel := context.WithCancel(ctx)
	defer cancel()

	timer := time.AfterFunc(cancelAfter, cancel)
	defer timer.Stop()

	started := time.Now()

	_, err := s.Connector.Query(canceling, s.SQL.Sleep(30))
	elapsed := time.Since(started)

	if err == nil {
		return errors.New("a query canceled while it was sleeping returned successfully")
	}

	if elapsed > cancelDeadline {
		return fmt.Errorf("canceled after %s and the query took %s to come back -- cancellation is not reaching the source",
			cancelAfter, elapsed.Round(time.Millisecond))
	}

	if rerr := wantReason(err, connectors.ReasonCanceled); rerr != nil {
		return rerr
	}

	// The pool has to survive it. A driver that abandons the connection
	// without returning it turns one canceled query into a leak.
	if _, aerr := s.Connector.Query(ctx, s.SQL.Sleep(0)); aerr != nil {
		return fmt.Errorf("the connector stopped working after a cancellation: %w", aerr)
	}

	return nil
}

const (
	// cancelAfter is how long the suite lets the query run before pulling the
	// context.
	cancelAfter = 250 * time.Millisecond

	// cancelDeadline is how long after that a prompt cancellation may take.
	// Generous: a loaded CI runner is slow, and a flaky conformance suite is
	// worse than a lenient one.
	cancelDeadline = 5 * time.Second
)

/*
A query that runs out of time is reported as a timeout and not as something
else.

The distinction is the operator's: a timeout means raise the limit or make the
query cheaper, and a cancellation means somebody walked away. Reporting one as
the other sends whoever is on call in the wrong direction.
*/
func checkTimeout(ctx context.Context, s Subject) error {
	// Well past the connector's own timeout, so the timeout is what ends this
	// and not the sleep finishing.
	sleep := int(s.QueryTimeout.Seconds()) + 5

	started := time.Now()

	_, err := s.Connector.Query(ctx, s.SQL.Sleep(sleep))
	elapsed := time.Since(started)

	if err == nil {
		return fmt.Errorf("a %ds query returned successfully against a %s timeout", sleep, s.QueryTimeout)
	}

	if elapsed > s.QueryTimeout+cancelDeadline {
		return fmt.Errorf("the timeout is %s and the query took %s to come back",
			s.QueryTimeout, elapsed.Round(time.Millisecond))
	}

	return wantReason(err, connectors.ReasonTimeout)
}

// --- errors -----------------------------------------------------------------

/*
SQL the source will not parse is reported as a syntax error.

Classification is what turns a driver's own text -- written for whoever wrote
the driver -- into something the person who typed the query can act on. An
unrecognized error is not a small failure: "the postgres connection failed" in
front of somebody with a missing comma is the product refusing to help.
*/
func checkSyntaxError(ctx context.Context, s Subject) error {
	_, err := s.Connector.Query(ctx, s.SQL.SyntaxError)
	if err == nil {
		return fmt.Errorf("%q was accepted, so the subject's SyntaxError is not one", s.SQL.SyntaxError)
	}

	return wantReason(err, connectors.ReasonSyntax)
}

// A statement naming a table that is not there is reported the same way, for
// the same reason: it is a mistake in the query rather than a fault in the
// connection, and telling the two apart is the whole point of classification.
func checkMissingTable(ctx context.Context, s Subject) error {
	_, err := s.Connector.Query(ctx, s.SQL.MissingTable)
	if err == nil {
		return fmt.Errorf("%q was accepted, so the subject's MissingTable exists", s.SQL.MissingTable)
	}

	return wantReason(err, connectors.ReasonSyntax)
}

// wantReason checks that an error was classified, and classified as expected.
func wantReason(err error, want connectors.Reason) error {
	var classified *connectors.Error

	if !errors.As(err, &classified) {
		return fmt.Errorf("the failure came back as a raw %T rather than a classified connectors.Error, so nothing above this can tell one kind of failure from another: %w",
			err, err)
	}

	if classified.Reason != want {
		return fmt.Errorf("the failure is classified %q, want %q: %w", classified.Reason, want, err)
	}

	if classified.Message == "" {
		return fmt.Errorf("the failure is classified %q with no message", want)
	}

	return nil
}

// --- reading results --------------------------------------------------------

// expectIDs runs a query returning one whole-number column and compares it.
func (s Subject) expectIDs(ctx context.Context, query string, want []int64) error {
	result, err := s.Connector.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("%s: %w", query, err)
	}

	if len(result.Rows) != len(want) {
		return fmt.Errorf("%s returned %d rows, want %d", query, len(result.Rows), len(want))
	}

	for i, expected := range want {
		got, cerr := asInt64(result.Rows[i][0])
		if cerr != nil {
			return fmt.Errorf("%s, row %d: %w", query, i, cerr)
		}

		if got != expected {
			return fmt.Errorf("%s, row %d is %d, want %d", query, i, got, expected)
		}
	}

	return nil
}

// fetch reads the whole fixture in id order.
type fixtureRows struct {
	result *connectors.Result
	index  map[string]int
}

func (s Subject) fetch(ctx context.Context) (fixtureRows, error) {
	query := fmt.Sprintf("SELECT %s FROM %s ORDER BY id",
		strings.Join(Columns(), ", "), s.Fixture.Table)

	result, err := s.Connector.Query(ctx, query)
	if err != nil {
		return fixtureRows{}, fmt.Errorf("%s: %w", query, err)
	}

	if want := len(Rows()); len(result.Rows) != want {
		return fixtureRows{}, fmt.Errorf("the fixture has %d rows, want %d -- the subject's INSERT does not match conformance.Rows()",
			len(result.Rows), want)
	}

	index := make(map[string]int, len(result.Columns))
	for i, col := range result.Columns {
		index[strings.ToLower(col.Name)] = i
	}

	return fixtureRows{result: result, index: index}, nil
}

func (f fixtureRows) at(row int, column string) (any, error) {
	i, ok := f.index[column]
	if !ok {
		return nil, fmt.Errorf("the result has no column %q", column)
	}

	cells := f.result.Rows[row]
	if i >= len(cells) {
		return nil, fmt.Errorf("row %d has %d cells and %q is at %d", row, len(cells), column, i)
	}

	return cells[i], nil
}

func (f fixtureRows) stringAt(row int, column string) (string, error) {
	cell, err := f.at(row, column)
	if err != nil {
		return "", err
	}

	got, err := asString(cell)
	if err != nil {
		return "", fmt.Errorf("row %d, %s: %w", row, column, err)
	}

	return got, nil
}

func (f fixtureRows) int64At(row int, column string) (int64, error) {
	cell, err := f.at(row, column)
	if err != nil {
		return 0, err
	}

	got, err := asInt64(cell)
	if err != nil {
		return 0, fmt.Errorf("row %d, %s: %w", row, column, err)
	}

	return got, nil
}

func (f fixtureRows) float64At(row int, column string) (float64, error) {
	cell, err := f.at(row, column)
	if err != nil {
		return 0, err
	}

	got, err := asFloat64(cell)
	if err != nil {
		return 0, fmt.Errorf("row %d, %s: %w", row, column, err)
	}

	return got, nil
}

func (f fixtureRows) boolAt(row int, column string) (bool, error) {
	cell, err := f.at(row, column)
	if err != nil {
		return false, err
	}

	got, err := asBool(cell)
	if err != nil {
		return false, fmt.Errorf("row %d, %s: %w", row, column, err)
	}

	return got, nil
}

func (f fixtureRows) timeAt(row int, column string) (time.Time, error) {
	cell, err := f.at(row, column)
	if err != nil {
		return time.Time{}, err
	}

	got, err := asTime(cell)
	if err != nil {
		return time.Time{}, fmt.Errorf("row %d, %s: %w", row, column, err)
	}

	return got, nil
}

func (s Subject) qualifiedFixture() string {
	if s.Fixture.Schema == "" {
		return s.Fixture.Name
	}

	return s.Fixture.Schema + "." + s.Fixture.Name
}
