package conformance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
)

/*
What a subject must supply, and what happens when it does not.

The skip logic is worth testing on its own. A check that skips is a check that
is not running, and the difference between "this source has no row generator"
and "somebody forgot a field" is invisible in a test log unless the skip says
which. So each requirement is asked what it is missing, and the answer has to
be a phrase a person can act on rather than a boolean.
*/

func TestASubjectIsAskedWhatItIsMissing(t *testing.T) {
	t.Parallel()

	full := fakeSubject(defectNone)
	full.SQL.LateralJoin = "lateral"

	// Every requirement, against a subject that has everything.
	for _, need := range []Requirement{
		NeedsNothing, NeedsFixture, NeedsIntrospection, NeedsSeries,
		NeedsSleep, NeedsTimeout, NeedsQuoting, NeedsBadSQL,
	} {
		if missing := full.cannot(need); missing != "" {
			t.Errorf("a complete subject was told it lacks %q", missing)
		}
	}

	bare := Subject{Connector: &fake{}}

	for name, tc := range map[string]struct {
		need Requirement
		want string
	}{
		"no fixture":      {NeedsFixture, "fixture table"},
		"no catalog name": {NeedsIntrospection, "fixture table"},
		"no generator":    {NeedsSeries, "row generator"},
		"no sleep":        {NeedsSleep, "sleep expression"},
		"no timeout":      {NeedsTimeout, "sleep expression"},
		"no quoting":      {NeedsQuoting, "fixture table"},
		"no bad sql":      {NeedsBadSQL, "statements it rejects"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			missing := bare.cannot(tc.need)
			if !strings.Contains(missing, tc.want) {
				t.Errorf("cannot() = %q, want it to mention %q", missing, tc.want)
			}
		})
	}
}

// A subject with a fixture but no catalog name skips introspection rather
// than failing it: a source Pivot can query and cannot introspect is a real
// situation, and it is not this suite's job to call it a bug.
func TestIntrospectionSkipsWithoutACatalogueName(t *testing.T) {
	t.Parallel()

	s := fakeSubject(defectNone)
	s.Fixture.Name = ""

	if missing := s.cannot(NeedsIntrospection); !strings.Contains(missing, "cataloged") {
		t.Errorf("cannot() = %q", missing)
	}
}

// Quoting skips when there is no awkward identifier to try, because some
// sources have no quoting worth the name.
func TestQuotingSkipsWithoutAnAwkwardIdentifier(t *testing.T) {
	t.Parallel()

	s := fakeSubject(defectNone)
	s.SQL.AwkwardIdentifier = ""

	if missing := s.cannot(NeedsQuoting); !strings.Contains(missing, "quoting") {
		t.Errorf("cannot() = %q", missing)
	}
}

/*
A timeout longer than the suite will wait is a skip with the number in it.

The alternative is a conformance run that appears to hang. A connector opened
with the thirty-second default would make one check sit still for half a
minute, and the honest answer is to say so and move on rather than to spend it.
*/
func TestATimeoutTooLongToWaitForIsSkippedWithItsLength(t *testing.T) {
	t.Parallel()

	s := fakeSubject(defectNone)
	s.QueryTimeout = maxWaitForTimeout + time.Second

	missing := s.cannot(NeedsTimeout)

	if !strings.Contains(missing, s.QueryTimeout.String()) {
		t.Errorf("cannot() = %q, want it to name the connector's timeout", missing)
	}

	s.QueryTimeout = 0

	if missing := s.cannot(NeedsTimeout); !strings.Contains(missing, "timeout") {
		t.Errorf("cannot() = %q", missing)
	}
}

// A subject that does not name itself is named after its connector, so a skip
// message says something rather than nothing.
func TestASubjectFallsBackToItsKind(t *testing.T) {
	t.Parallel()

	s := Subject{Connector: &fake{}}

	if got := s.name(); got != "fake" {
		t.Errorf("name() = %q, want the connector's kind", got)
	}

	s.Name = "the warehouse"

	if got := s.name(); got != "the warehouse" {
		t.Errorf("name() = %q", got)
	}
}

// An unset row cap means the connector's default, which is what a subject that
// says nothing was actually opened with.
func TestAnUnsetRowCapMeansTheDefault(t *testing.T) {
	t.Parallel()

	if got := (Subject{}).maxRows(); got != connectors.DefaultMaxRows {
		t.Errorf("maxRows() = %d, want %d", got, connectors.DefaultMaxRows)
	}

	if got := (Subject{MaxRows: 12}).maxRows(); got != 12 {
		t.Errorf("maxRows() = %d, want 12", got)
	}
}

func TestAFixtureIsNamedWithItsSchemaWhenItHasOne(t *testing.T) {
	t.Parallel()

	s := Subject{Fixture: Fixture{Name: "fixture"}}

	if got := s.qualifiedFixture(); got != "fixture" {
		t.Errorf("qualifiedFixture() = %q", got)
	}

	s.Fixture.Schema = "main"

	if got := s.qualifiedFixture(); got != "main.fixture" {
		t.Errorf("qualifiedFixture() = %q", got)
	}
}

// --- a connector that fails on demand ---------------------------------------

// stub answers every query the same way, so the suite's own error paths can be
// reached without inventing a new defect for each one.
type stub struct {
	result *connectors.Result
	err    error
}

func (s stub) Kind() connectors.Kind                 { return connectors.Kind("stub") }
func (s stub) Capabilities() connectors.Capabilities { return connectors.Capabilities{} }
func (s stub) Test(context.Context) error            { return s.err }
func (s stub) Close() error                          { return nil }
func (s stub) Introspect(context.Context) ([]connectors.Table, error) {
	return nil, s.err
}

func (s stub) Query(context.Context, string, ...any) (*connectors.Result, error) {
	if s.err != nil {
		return nil, s.err
	}

	return s.result, nil
}

func stubSubject(c connectors.Connector) Subject {
	s := fakeSubject(defectNone)
	s.Connector = c

	return s
}

// A source that cannot be reached fails the first check rather than producing
// fourteen confusing ones.
func TestAConnectorThatCannotConnectFailsConnects(t *testing.T) {
	t.Parallel()

	broken := stub{err: errors.New("no route to host")}

	if err := checkConnects(t.Context(), stubSubject(broken)); err == nil {
		t.Error("connects passed against a connector whose Test fails")
	}

	if err := checkIntrospection(t.Context(), stubSubject(broken)); err == nil {
		t.Error("introspection passed against a connector whose Introspect fails")
	}
}

// A fixture that will not build stops the run, because every check after it
// would fail for the same reason and none of them would say so.
func TestAFixtureThatWillNotBuildIsReported(t *testing.T) {
	t.Parallel()

	broken := stubSubject(stub{err: errors.New("permission denied")})

	err := broken.createFixture(t.Context())
	if err == nil {
		t.Fatal("createFixture succeeded against a connector that refuses everything")
	}

	// The statement is in the message: "permission denied" alone does not say
	// whether it was the CREATE or one of the INSERTs.
	if !strings.Contains(err.Error(), "create") {
		t.Errorf("createFixture error = %v, want it to name the statement", err)
	}

	if derr := broken.dropFixture(t.Context()); derr == nil {
		t.Error("dropFixture succeeded against a connector that refuses everything")
	}
}

// An INSERT that fails is reported as distinctly as a CREATE that does.
func TestAFailedInsertNamesItself(t *testing.T) {
	t.Parallel()

	s := stubSubject(refuseInsert{&fake{}})

	err := s.createFixture(t.Context())
	if err == nil {
		t.Fatal("createFixture succeeded")
	}

	if !strings.Contains(err.Error(), "insert") {
		t.Errorf("error = %v, want it to name the INSERT", err)
	}
}

type refuseInsert struct{ *fake }

func (r refuseInsert) Query(ctx context.Context, query string, args ...any) (*connectors.Result, error) {
	if query == "insert" {
		return nil, errors.New("the fixture would not seed")
	}

	return r.fake.Query(ctx, query, args...)
}

/*
A fixture with the wrong number of rows says which side is wrong.

The subject's INSERT and [Rows] are two halves of one contract written in two
files, and when they disagree every value check fails at once with what looks
like a driver bug. Saying it plainly here saves the next connector author an
afternoon.
*/
func TestAFixtureWithTheWrongRowCountBlamesTheSubject(t *testing.T) {
	t.Parallel()

	short := stubSubject(stub{result: &connectors.Result{
		Columns: []connectors.Column{{Name: "id"}},
		Rows:    [][]any{{int64(1)}},
	}})

	_, err := short.fetch(t.Context())
	if err == nil {
		t.Fatal("a one-row fixture was accepted")
	}

	if !strings.Contains(err.Error(), "conformance.Rows()") {
		t.Errorf("error = %v, want it to point at the row contract", err)
	}
}

// A column the suite selected and did not get back is named, rather than
// becoming an index out of range.
func TestAMissingColumnIsNamed(t *testing.T) {
	t.Parallel()

	rows := fixtureRows{
		result: &connectors.Result{Rows: [][]any{{1}, {2}, {3}}},
		index:  map[string]int{},
	}

	if _, err := rows.at(0, "notes"); err == nil || !strings.Contains(err.Error(), "notes") {
		t.Errorf("at() = %v, want it to name the column", err)
	}

	// An index that points past the end of a short row, which a driver
	// returning ragged rows would produce.
	rows.index["notes"] = 9

	if _, err := rows.at(0, "notes"); err == nil || !strings.Contains(err.Error(), "cells") {
		t.Errorf("at() = %v, want it to say the row is short", err)
	}
}

// Every typed reader reports the column and row it was reading, so a
// conversion failure points at a cell rather than at the suite.
func TestATypedReaderNamesTheCell(t *testing.T) {
	t.Parallel()

	rows := fixtureRows{
		result: &connectors.Result{Rows: [][]any{{struct{}{}}}},
		index:  map[string]int{"ratio": 0},
	}

	readers := map[string]func(int, string) error{
		"string": func(r int, c string) error { _, err := rows.stringAt(r, c); return err },
		"int64":  func(r int, c string) error { _, err := rows.int64At(r, c); return err },
		"float":  func(r int, c string) error { _, err := rows.float64At(r, c); return err },
		"bool":   func(r int, c string) error { _, err := rows.boolAt(r, c); return err },
		"time":   func(r int, c string) error { _, err := rows.timeAt(r, c); return err },
	}

	for name, read := range readers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := read(0, "ratio")
			if err == nil {
				t.Fatalf("%s accepted a struct", name)
			}

			if !strings.Contains(err.Error(), "ratio") {
				t.Errorf("error = %v, want it to name the column", err)
			}

			// And a column that is not there at all.
			if merr := read(0, "nothing"); merr == nil {
				t.Errorf("%s accepted a column that does not exist", name)
			}
		})
	}
}

// A placeholder style this build cannot write is refused rather than guessed
// at, because guessing produces SQL the source rejects somewhere else.
func TestAnUnknownPlaceholderStyleIsRefused(t *testing.T) {
	t.Parallel()

	s := stubSubject(stub{})

	err := checkPlaceholder(t.Context(), s)
	if err == nil {
		t.Fatal("an empty placeholder style was accepted")
	}

	if !strings.Contains(err.Error(), "placeholder") {
		t.Errorf("error = %v", err)
	}
}

// A capability set with no quoting rule and no identifier length is refused:
// both are things the query compiler will read and neither has a safe default.
func TestUndeclaredCapabilitiesAreRefused(t *testing.T) {
	t.Parallel()

	err := checkCapabilities(t.Context(), stubSubject(stub{}))
	if err == nil || !strings.Contains(err.Error(), "QuoteIdentifier") {
		t.Errorf("error = %v, want the missing quoting rule", err)
	}

	noLength := fakeSubject(defectNone)
	noLength.Connector = shortIdentifiers{&fake{}}

	err = checkCapabilities(t.Context(), noLength)
	if err == nil || !strings.Contains(err.Error(), "MaxIdentifierLength") {
		t.Errorf("error = %v, want the missing identifier length", err)
	}
}

type shortIdentifiers struct{ *fake }

func (s shortIdentifiers) Capabilities() connectors.Capabilities {
	caps := s.fake.Capabilities()
	caps.MaxIdentifierLength = 0

	return caps
}

// A quoting rule that leaves an awkward name untouched cannot be right, and is
// caught before the source is asked to parse it.
func TestQuotingThatChangesNothingIsRefused(t *testing.T) {
	t.Parallel()

	err := checkQuoting(t.Context(), stubSubject(identityQuoting{&fake{}}))
	if err == nil || !strings.Contains(err.Error(), "unchanged") {
		t.Errorf("error = %v", err)
	}
}

type identityQuoting struct{ *fake }

func (i identityQuoting) Capabilities() connectors.Capabilities {
	caps := i.fake.Capabilities()
	caps.QuoteIdentifier = func(name string) string { return name }

	return caps
}

// A row cap of one leaves nothing to stream, and the suite says so rather than
// passing on an empty result.
func TestARowCapTooSmallToStreamIsReported(t *testing.T) {
	t.Parallel()

	s := fakeSubject(defectNone)
	s.MaxRows = 1

	err := checkLargeResult(t.Context(), s)
	if err == nil || !strings.Contains(err.Error(), "row cap") {
		t.Errorf("error = %v", err)
	}
}

// SQL the subject nominated as invalid, which the source accepts, is the
// subject's mistake and is reported as one.
func TestBadSQLThatIsNotBadIsBlamedOnTheSubject(t *testing.T) {
	t.Parallel()

	accepting := stubSubject(stub{result: &connectors.Result{}})

	if err := checkSyntaxError(t.Context(), accepting); err == nil ||
		!strings.Contains(err.Error(), "SyntaxError") {
		t.Errorf("error = %v", err)
	}

	if err := checkMissingTable(t.Context(), accepting); err == nil ||
		!strings.Contains(err.Error(), "MissingTable") {
		t.Errorf("error = %v", err)
	}
}

// A classified error with no message is as useless as an unclassified one: the
// reason goes in a log and the message goes in front of a person.
func TestAClassifiedErrorWithNoMessageIsRefused(t *testing.T) {
	t.Parallel()

	err := wantReason(&connectors.Error{Reason: connectors.ReasonSyntax}, connectors.ReasonSyntax)
	if err == nil || !strings.Contains(err.Error(), "no message") {
		t.Errorf("wantReason = %v", err)
	}
}

func TestIgnoreDiscardsAnError(t *testing.T) {
	t.Parallel()

	// Exists so that a deliberate discard reads as one at the call site.
	ignore(errors.New("discarded"))
}
