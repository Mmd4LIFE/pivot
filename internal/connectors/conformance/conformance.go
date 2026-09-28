// Package conformance is the test suite every connector must pass.
//
// # Why this exists
//
// ADR-0001 chose Go over the JVM, which means no JDBC and a hand-written driver
// per source. The cost of that decision is paid here or in production. One
// shared suite, written once, is what makes the eighth connector a week instead
// of a month — and what stops the long tail of "dates are off by one in
// Redshift" bugs, each found by a different customer and fixed in a different
// place.
//
// It is written before the second connector on purpose. A suite written after
// two connectors exist gets shaped to what those two already happen to do,
// which is how a conformance suite becomes a regression test with delusions of
// generality.
//
// # How a connector uses it
//
// A connector's test file supplies a [Subject]: an open connector, the DDL for
// the suite's fixture table in that dialect, and the handful of SQL snippets
// the suite needs and cannot write portably. Then:
//
//	func TestConformance(t *testing.T) {
//		conformance.Run(t, conformance.Subject{...})
//	}
//
// That is the whole integration. Nothing in this package knows about any
// particular connector, so adding one means writing one file rather than
// editing this one — which is the difference between a suite that grows with
// the product and a suite that becomes a switch statement.
//
// # Why checks are values rather than test functions
//
// Each property is a [Check]: a name and a function returning an error, rather
// than a func(*testing.T). Two reasons. A failure then names the property —
// "null_and_empty_are_distinct: notes came back as an empty string" — rather
// than a line number in a file the reader does not have open. And the suite can
// be pointed at a deliberately broken connector by a test of the suite itself,
// which is the only way to know it can fail at all. See broken_test.go.
package conformance

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
)

// Subject is one connector under test.
type Subject struct {
	// Name appears in skips and failures. The connector's kind is usually
	// the right answer.
	Name string

	// Connector is open and ready. The caller owns closing it.
	Connector connectors.Connector

	// Fixture is the suite's table, expressed in this dialect.
	Fixture Fixture

	// SQL is what the suite needs and cannot write portably.
	SQL Expressions

	// MaxRows is the row cap the connector was opened with, which the
	// truncation check needs in order to ask for one row more than it.
	//
	// Zero means [connectors.DefaultMaxRows], which makes that check generate a
	// hundred thousand rows. A subject is much better off opening its connector
	// with a small cap and saying so here.
	MaxRows int64

	// QueryTimeout is the per-query timeout the connector was opened with.
	//
	// Needed because the only way to prove a timeout is reported as one is to
	// wait for it. A subject that leaves this at zero, or sets it longer than
	// the suite is willing to wait, skips that check rather than hanging a CI
	// run for half a minute.
	QueryTimeout time.Duration
}

// maxWaitForTimeout is how long the suite will sit still to watch a query time
// out. Beyond this the check skips: a suite nobody will run because it takes
// too long is a suite that catches nothing.
const maxWaitForTimeout = 10 * time.Second

/*
Fixture is the suite's table expressed in one dialect.

The suite selects a fixed set of columns by name, so the DDL below has to
produce them. Spelled out by each subject rather than generated, because the
point is to exercise the source's own types: a generated CREATE TABLE would
test the suite's idea of a timestamp rather than the source's.

The columns, in any physical order:

	id             a whole number, the sort key, not null
	name           text, not null
	notes          text, nullable
	flag           a boolean
	ratio          a double-precision float
	created_utc    a timestamp that carries its zone
	created_naive  a timestamp that does not

and exactly the three rows [Rows] describes.
*/
type Fixture struct {
	// Table is how the suite names the table in its queries, already
	// schema-qualified and quoted if this dialect needs that.
	Table string

	// Schema and Name are how introspection should report the same table.
	// An empty Name skips the introspection check.
	Schema string
	Name   string

	/*
		Create, Insert and Drop are run through the connector, in that order,
		statement by statement. Drop must tolerate a table that is not there,
		because a run that died before cleaning up would otherwise poison
		every run after it.

		All three may be empty, for a source the connector cannot write to.
		That is not an edge case: opening a BI source read-only is the correct
		thing to do, and an account granted SELECT and nothing else is how a
		careful warehouse administrator hands out access. A subject in that
		position puts the table there by its own means before calling [Run],
		and owns removing it -- the suite will not, because it cannot.
	*/
	Create []string
	Insert []string
	Drop   []string
}

// Expressions are the SQL snippets the suite needs from each dialect.
type Expressions struct {
	// Sleep blocks the source for roughly this many seconds, so that
	// cancellation and timeouts have something to interrupt.
	Sleep func(seconds int) string

	// Series returns n rows of one whole-number column counting from 1, for
	// the row-limit and large-result checks. A source with no generator can
	// select from a real table instead, as long as the numbers are 1..n.
	Series func(n int) string

	// LateralJoin is a query using a lateral join that returns the fixture's
	// three ids in order. Needed only when the connector declares
	// LateralJoins, because lateral syntax varies too much for the suite to
	// write one that works everywhere -- and a capability the suite cannot
	// demonstrate is refused rather than taken on trust.
	LateralJoin string

	// SyntaxError is SQL this source rejects while parsing.
	SyntaxError string

	// MissingTable is a syntactically valid statement naming a table that does
	// not exist.
	MissingTable string

	// AwkwardIdentifier is a name that is only legal quoted -- one containing
	// a space, or the quote character itself. The suite aliases a column to it
	// using the dialect's own [connectors.Capabilities.QuoteIdentifier] and
	// checks the name that comes back, which is the only way to know the
	// declared quoting rule is the one the source actually applies.
	//
	// Empty skips that check. Keep it shorter than MaxIdentifierLength, or the
	// source truncates it and the check fails for the wrong reason.
	AwkwardIdentifier string
}

// Check is one property a connector must have.
type Check struct {
	// Property is what is being checked, in snake_case. It names the subtest
	// and opens the failure, so it is the first thing a connector author
	// reads.
	Property string

	// Needs is what the check requires from the subject beyond an open
	// connector. A subject that cannot supply it skips rather than fails.
	Needs Requirement

	// Run returns nil when the property holds.
	Run func(ctx context.Context, s Subject) error
}

// Failure renders a check's failure the way [Run] reports it.
//
// A function rather than a format string at the call site, so that the promise
// this package makes -- a failure opens with the property that broke, not with
// the assertion that noticed -- is one testable thing rather than a convention.
func (c Check) Failure(err error) string {
	return c.Property + ": " + err.Error()
}

// Requirement is what a check needs from a subject beyond an open connector.
type Requirement uint8

const (
	// NeedsNothing runs against any connector.
	NeedsNothing Requirement = iota

	// NeedsFixture needs the seeded table.
	NeedsFixture

	// NeedsIntrospection needs the fixture and the name it is cataloged under.
	NeedsIntrospection

	// NeedsSeries needs a row generator.
	NeedsSeries

	// NeedsSleep needs a blocking expression.
	NeedsSleep

	// NeedsTimeout needs a blocking expression and a timeout short enough to
	// wait for.
	NeedsTimeout

	// NeedsQuoting needs an identifier that is only legal quoted.
	NeedsQuoting

	// NeedsBadSQL needs statements this source rejects.
	NeedsBadSQL
)

// Checks is every property, in the order they are most useful to read.
//
// Ordered so that a connector being brought up fails first on the most
// fundamental thing. A suite that reports fourteen failures when the real
// problem is that the connection is not working teaches people to ignore it.
func Checks() []Check {
	return []Check{
		{Property: "connects", Needs: NeedsNothing, Run: checkConnects},

		{Property: "values_survive_the_round_trip", Needs: NeedsFixture, Run: checkValues},
		{Property: "null_and_empty_are_distinct", Needs: NeedsFixture, Run: checkNullVersusEmpty},
		{Property: "unicode_is_unchanged", Needs: NeedsFixture, Run: checkUnicode},
		{Property: "timestamps_keep_their_instant", Needs: NeedsFixture, Run: checkTimestamps},

		{Property: "columns_describe_themselves", Needs: NeedsFixture, Run: checkColumnMetadata},
		{Property: "columns_carry_a_canonical_type", Needs: NeedsFixture, Run: checkCanonicalTypes},
		{Property: "an_unmapped_type_says_so", Needs: NeedsNothing, Run: checkUnmappedTypesSaySo},
		{Property: "introspection_finds_the_fixture", Needs: NeedsIntrospection, Run: checkIntrospection},

		{Property: "declared_placeholder_is_the_real_one", Needs: NeedsFixture, Run: checkPlaceholder},
		{Property: "declared_capabilities_are_true", Needs: NeedsFixture, Run: checkCapabilities},
		{Property: "quoted_identifiers_round_trip", Needs: NeedsQuoting, Run: checkQuoting},

		{Property: "a_large_result_arrives_whole", Needs: NeedsSeries, Run: checkLargeResult},
		{Property: "row_limit_truncates_with_a_signal", Needs: NeedsSeries, Run: checkTruncation},
		{Property: "concurrent_queries_all_succeed", Needs: NeedsFixture, Run: checkConcurrency},

		{Property: "cancellation_is_prompt_and_says_so", Needs: NeedsSleep, Run: checkCancellation},
		{Property: "a_timeout_is_reported_as_one", Needs: NeedsTimeout, Run: checkTimeout},

		{Property: "a_syntax_error_says_so", Needs: NeedsBadSQL, Run: checkSyntaxError},
		{Property: "a_missing_table_says_so", Needs: NeedsBadSQL, Run: checkMissingTable},
	}
}

/*
Run executes the suite against a subject.

Each property becomes a subtest named after itself, so `go test -run` can
select one and a failure reads as the property that broke.

The fixture is created once and dropped afterwards, including when a check
fails: a suite that leaves its table behind makes the next run fail for a
different reason than the one worth knowing about.
*/
func Run(t *testing.T, s Subject) {
	t.Helper()

	if s.Connector == nil {
		t.Fatal("conformance: the subject has no connector")
	}

	if s.hasFixture() {
		if err := s.createFixture(t.Context()); err != nil {
			t.Fatalf("conformance: could not create the fixture: %v", err)
		}

		t.Cleanup(func() {
			// A background context on purpose: the test's own may already be
			// canceled by the failure that brought us here, and the table
			// still has to go.
			if err := s.dropFixture(context.Background()); err != nil {
				t.Errorf("conformance: could not drop the fixture: %v", err)
			}
		})
	}

	for _, check := range Checks() {
		t.Run(check.Property, func(t *testing.T) {
			if missing := s.cannot(check.Needs); missing != "" {
				t.Skipf("%s does not supply %s", s.name(), missing)
			}

			if err := check.Run(t.Context(), s); err != nil {
				t.Error(check.Failure(err))
			}
		})
	}
}

// cannot reports what a subject is missing for a requirement, or "" when it
// has everything.
func (s Subject) cannot(need Requirement) string {
	switch need {
	case NeedsNothing:
		return ""

	case NeedsFixture:
		if !s.hasFixture() {
			return "a fixture table"
		}

	case NeedsIntrospection:
		if !s.hasFixture() {
			return "a fixture table"
		}

		if s.Fixture.Name == "" {
			return "the name its fixture is cataloged under"
		}

	case NeedsSeries:
		if s.SQL.Series == nil {
			return "a row generator"
		}

	case NeedsSleep:
		if s.SQL.Sleep == nil {
			return "a sleep expression"
		}

	case NeedsTimeout:
		if s.SQL.Sleep == nil {
			return "a sleep expression"
		}

		if s.QueryTimeout <= 0 {
			return "the query timeout its connector was opened with"
		}

		if s.QueryTimeout > maxWaitForTimeout {
			return fmt.Sprintf("a query timeout under %s (its connector uses %s, which is longer than this suite will wait)",
				maxWaitForTimeout, s.QueryTimeout)
		}

	case NeedsQuoting:
		if !s.hasFixture() {
			return "a fixture table"
		}

		if s.SQL.AwkwardIdentifier == "" {
			return "an identifier that needs quoting"
		}

	case NeedsBadSQL:
		if s.SQL.SyntaxError == "" || s.SQL.MissingTable == "" {
			return "statements it rejects"
		}
	}

	return ""
}

// hasFixture reports whether the suite has a table to query.
//
// The name alone, because a subject whose connector cannot write supplies no
// DDL and puts the table there itself. Requiring Create here meant a read-only
// connector could not be conformance-tested at all, which would have excluded
// exactly the connections most worth being careful with.
func (s Subject) hasFixture() bool {
	return s.Fixture.Table != ""
}

func (s Subject) name() string {
	if s.Name != "" {
		return s.Name
	}

	return string(s.Connector.Kind())
}

// maxRows is the cap the subject's connector was opened with.
func (s Subject) maxRows() int64 {
	if s.MaxRows > 0 {
		return s.MaxRows
	}

	return connectors.DefaultMaxRows
}

func (s Subject) createFixture(ctx context.Context) error {
	// Dropped first. A run that died before cleaning up would otherwise make
	// every later run fail with "already exists", which is a confusing way to
	// learn about a crash from last Tuesday. Whether this drop worked is not
	// interesting -- on a first run there is nothing to drop -- so the create
	// below is what decides whether the fixture is usable.
	ignore(s.dropFixture(ctx))

	for _, statement := range s.Fixture.Create {
		if _, err := s.Connector.Query(ctx, statement); err != nil {
			return fmt.Errorf("%s: %w", statement, err)
		}
	}

	for _, statement := range s.Fixture.Insert {
		if _, err := s.Connector.Query(ctx, statement); err != nil {
			return fmt.Errorf("%s: %w", statement, err)
		}
	}

	return nil
}

func (s Subject) dropFixture(ctx context.Context) error {
	for _, statement := range s.Fixture.Drop {
		if _, err := s.Connector.Query(ctx, statement); err != nil {
			return fmt.Errorf("%s: %w", statement, err)
		}
	}

	return nil
}

// ignore states in one word that an error is deliberately discarded.
//
// errcheck rejects `_ = f()` across this repository, which is the right
// default. A call to this is that exception made visible at the call site
// rather than hidden behind a nolint comment nobody reads.
func ignore(error) {}
