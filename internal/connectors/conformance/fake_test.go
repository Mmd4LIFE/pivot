package conformance

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/datatype"
)

/*
A connector that is correct, and can be broken one way at a time.

The suite's own test needs something to point at. A mock that records calls
would prove nothing -- the question is not whether the checks run, it is
whether they notice. So this is a real, working connector over an in-memory
copy of the fixture, with a single [defect] field that makes exactly one thing
wrong.

Its dialect is deliberately nothing like PostgreSQL: a toy vocabulary of
"series 40" and "sleep 3". If the suite only passes against something that
looks like Postgres, then it is a PostgreSQL regression test wearing a
conformance suite's clothes, and this is how that gets caught.
*/

// defect is the one thing a fake connector gets wrong.
type defect uint8

const (
	// defectNone is a correct connector. It has to pass everything.
	defectNone defect = iota

	defectNullBecomesEmpty
	defectUnicodeNormalized
	defectInstantShifted
	defectNaiveShifted
	defectNoSourceType
	defectUncataloged
	defectNullableID
	defectPositionalBinding
	defectLyingCTEs
	defectUnprovenLateral
	defectUnescapedQuoting
	defectDropsRows
	defectSilentTruncation
	defectIgnoresCancel
	defectWrongCancelReason
	defectWrongTimeoutReason
	defectRawErrors
	defectGuessesTypes
	defectStreamStopsEarly
)

// fakeTimeout is short, so the timeout check costs milliseconds here.
const fakeTimeout = time.Second

type fake struct {
	defect defect
}

func (f *fake) Kind() connectors.Kind { return connectors.Kind("fake") }

func (f *fake) Capabilities() connectors.Capabilities {
	quote := quoteFake
	if f.defect == defectUnescapedQuoting {
		// Glues quotes on either end without escaping the ones inside, which
		// is the bug that turns an identifier into an injection.
		quote = func(name string) string { return `"` + name + `"` }
	}

	return connectors.Capabilities{
		WindowFunctions:     true,
		CTEs:                true,
		LateralJoins:        f.defect == defectUnprovenLateral,
		Placeholder:         connectors.PlaceholderDollar,
		QuoteIdentifier:     quote,
		MaxIdentifierLength: 63,
		SupportsCancel:      true,
	}
}

func (f *fake) Test(context.Context) error { return nil }

// NormalizeType knows only what the fixture needs, so that an invented name
// still falls through to Unknown -- which is the property being checked.
func (f *fake) NormalizeType(sourceType string) datatype.Type {
	if f.defect == defectGuessesTypes {
		// Everything is text, which is the type system failure this suite
		// exists to catch: indistinguishable from knowledge until somebody
		// charts it.
		return datatype.Type{Kind: datatype.String, Source: sourceType}
	}

	return datatype.Normalize(sourceType, nil)
}

func (f *fake) Close() error { return nil }

// Stream answers the same statements Query does, over the result it would
// have returned.
func (f *fake) Stream(
	ctx context.Context, query string, args ...any,
) (connectors.Stream, error) {
	result, err := f.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	stream := newSliceStream(result)

	if f.defect == defectStreamStopsEarly {
		// Ends after one row and says nothing, which is the streaming
		// equivalent of a silently truncated result.
		stream.result = &connectors.Result{
			Columns: result.Columns,
			Rows:    result.Rows[:min(1, len(result.Rows))],
		}
	}

	return stream, nil
}

// ForeignKeys: the fake declares one composite relationship, so a subject
// built on it exercises the grouping rather than the trivial single-column
// case.
func (f *fake) ForeignKeys(context.Context) ([]connectors.ForeignKey, error) {
	return []connectors.ForeignKey{
		{
			Name: "fixture_parent_fk", FromSchema: "main", FromTable: "fixture",
			FromColumn: "id", ToSchema: "main", ToTable: "parent",
			ToColumn: "id", Ordinal: 1,
		},
		{
			Name: "fixture_parent_fk", FromSchema: "main", FromTable: "fixture",
			FromColumn: "name", ToSchema: "main", ToTable: "parent",
			ToColumn: "name", Ordinal: 2,
		},
	}, nil
}

func (f *fake) Introspect(context.Context) ([]connectors.Table, error) {
	if f.defect == defectUncataloged {
		return nil, nil
	}

	table := connectors.Table{Schema: "main", Name: "fixture", Type: connectors.TableTypeTable}

	for i, name := range Columns() {
		table.Columns = append(table.Columns, connectors.Column{
			Name:       name,
			SourceType: fakeSourceTypes[name],
			Type:       f.NormalizeType(fakeSourceTypes[name]),
			Nullable:   name == "notes" || (name == "id" && f.defect == defectNullableID),
			Position:   i + 1,
		})
	}

	return []connectors.Table{table}, nil
}

/*
Query answers the handful of statements the suite issues.

Matched by shape rather than parsed. A parser here would be a second
implementation of SQL to get wrong, and what is being tested is the suite.
*/
func (f *fake) Query(ctx context.Context, query string, args ...any) (*connectors.Result, error) {
	switch {
	case query == "create", query == "insert", query == "drop":
		return &connectors.Result{}, nil

	case query == "boom":
		return nil, f.classified(connectors.ReasonSyntax, "the fake could not parse that")

	case query == "missing":
		return nil, f.classified(connectors.ReasonSyntax, "there is no table called nothing_here")

	case strings.HasPrefix(query, "sleep "):
		return f.sleep(ctx, query)

	case strings.HasPrefix(query, "series "):
		return f.series(query)

	case strings.HasPrefix(query, "SELECT id AS "):
		return f.aliased(strings.TrimPrefix(query, "SELECT id AS "))

	case strings.HasPrefix(query, "SELECT id FROM fixture WHERE"):
		return f.bound(args)

	case strings.HasPrefix(query, "WITH "), strings.Contains(query, "ROW_NUMBER"), strings.Contains(query, "LATERAL"):
		return f.ids(), nil

	case strings.HasPrefix(query, "SELECT id, name,"):
		return f.fixture(), nil
	}

	return nil, f.classified(connectors.ReasonSyntax, "the fake does not know "+query)
}

// fixture is the seeded table as this connector would return it.
func (f *fake) fixture() *connectors.Result {
	result := &connectors.Result{}

	for i, name := range Columns() {
		sourceType := fakeSourceTypes[name]
		if f.defect == defectNoSourceType {
			sourceType = ""
		}

		result.Columns = append(result.Columns, connectors.Column{
			Name:       name,
			SourceType: sourceType,
			Type:       f.NormalizeType(sourceType),
			Nullable:   name == "notes",
			Position:   i + 1,
		})
	}

	for _, row := range Rows() {
		var notes any
		if row.Notes != nil {
			notes = *row.Notes
		}

		if notes == nil && f.defect == defectNullBecomesEmpty {
			notes = ""
		}

		name := row.Name
		if f.defect == defectUnicodeNormalized {
			// Combining acute to the precomposed character: renders
			// identically, compares unequal.
			name = strings.ReplaceAll(name, "é", "é")
		}

		zoned := row.CreatedUTC
		if f.defect == defectInstantShifted {
			zoned = zoned.Add(time.Hour)
		}

		naive := row.CreatedNaive
		if f.defect == defectNaiveShifted {
			naive = naive.Add(90 * time.Minute)
		}

		result.Rows = append(result.Rows, []any{
			row.ID, name, notes, row.Flag, row.Ratio, zoned, naive,
		})
	}

	return result
}

func (f *fake) ids() *connectors.Result {
	result := &connectors.Result{
		Columns: []connectors.Column{{Name: "id", SourceType: "FAKE", Position: 1}},
	}

	if f.defect == defectLyingCTEs {
		return &connectors.Result{Columns: result.Columns}
	}

	for _, row := range Rows() {
		result.Rows = append(result.Rows, []any{row.ID})
	}

	return result
}

// bound answers the placeholder check: the row whose id is the second argument
// and whose name is not the first.
func (f *fake) bound(args []any) (*connectors.Result, error) {
	id, name := args[1], args[0]

	if f.defect == defectPositionalBinding {
		// Declares a numbered style and binds in the order the arguments
		// arrived, so $2 gets the first one.
		id, name = args[0], args[1]
	}

	result := &connectors.Result{
		Columns: []connectors.Column{{Name: "id", SourceType: "FAKE", Position: 1}},
	}

	for _, row := range Rows() {
		if id == any(row.ID) && name != any(row.Name) {
			result.Rows = append(result.Rows, []any{row.ID})
		}
	}

	return result, nil
}

// aliased answers the quoting check, rejecting an identifier this dialect
// cannot parse the way a real source would.
func (f *fake) aliased(rest string) (*connectors.Result, error) {
	quoted, _, _ := strings.Cut(rest, " FROM ")

	name, err := unquoteFake(quoted)
	if err != nil {
		return nil, f.classified(connectors.ReasonSyntax, err.Error())
	}

	return &connectors.Result{
		Columns: []connectors.Column{{Name: name, SourceType: "FAKE", Position: 1}},
	}, nil
}

func (f *fake) series(query string) (*connectors.Result, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(query, "series "))
	if err != nil {
		return nil, f.classified(connectors.ReasonSyntax, "series wants a number")
	}

	result := &connectors.Result{
		Columns: []connectors.Column{{Name: "i", SourceType: "FAKE", Position: 1}},
	}

	for i := 1; i <= n; i++ {
		if int64(len(result.Rows)) >= fakeMaxRows {
			result.Truncated = f.defect != defectSilentTruncation

			break
		}

		value := int64(i)

		// One row repeated in the middle of the stream, in place of the one
		// that should be there. The count still comes out right, which is why
		// a row count alone cannot see it.
		if f.defect == defectDropsRows && i == 3 {
			value = 2
		}

		result.Rows = append(result.Rows, []any{value})
	}

	return result, nil
}

/*
fakeSourceTypes is what this dialect calls its own types.

Ordinary SQL names rather than the toy vocabulary the rest of the fake uses,
because the canonical-type check reads them through the shared table in
[datatype] -- and a source whose type names were "FAKE" would make that check
pass by being unmapped rather than by being right.
*/
var fakeSourceTypes = map[string]string{
	"id":            "BIGINT",
	"name":          "TEXT",
	"notes":         "TEXT",
	"flag":          "BOOLEAN",
	"ratio":         "DOUBLE PRECISION",
	"created_utc":   "TIMESTAMP WITH TIME ZONE",
	"created_naive": "TIMESTAMP WITHOUT TIME ZONE",
}

// fakeMaxRows is the fake's row cap, small enough that the truncation check
// moves a handful of rows.
const fakeMaxRows = 50

func (f *fake) sleep(ctx context.Context, query string) (*connectors.Result, error) {
	seconds, err := strconv.Atoi(strings.TrimPrefix(query, "sleep "))
	if err != nil {
		return nil, f.classified(connectors.ReasonSyntax, "sleep wants a number")
	}

	if seconds == 0 {
		return &connectors.Result{}, nil
	}

	timer := time.NewTimer(fakeTimeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		if f.defect == defectIgnoresCancel {
			return &connectors.Result{}, nil
		}

		if f.defect == defectWrongCancelReason {
			return nil, f.classified(connectors.ReasonUnknown, "something went wrong")
		}

		return nil, f.classified(connectors.ReasonCanceled, "the query was canceled")

	case <-timer.C:
		if f.defect == defectWrongTimeoutReason {
			return nil, f.classified(connectors.ReasonCanceled, "the query was canceled")
		}

		return nil, f.classified(connectors.ReasonTimeout, "the query ran longer than "+fakeTimeout.String())
	}
}

// classified returns a proper connector error, or a raw one when that is the
// defect under test.
func (f *fake) classified(reason connectors.Reason, message string) error {
	if f.defect == defectRawErrors {
		return errors.New(message)
	}

	return connectors.Errorf(reason, nil, "", "%s", message)
}

// quoteFake is a correct quoting rule: wrap in double quotes, double any
// inside.
func quoteFake(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// unquoteFake parses an identifier the way a real source would, rejecting one
// whose inner quotes were not escaped.
func unquoteFake(quoted string) (string, error) {
	if len(quoted) < 2 || !strings.HasPrefix(quoted, `"`) || !strings.HasSuffix(quoted, `"`) {
		return "", fmt.Errorf("%s is not a quoted identifier", quoted)
	}

	body := quoted[1 : len(quoted)-1]

	var name strings.Builder

	for i := 0; i < len(body); i++ {
		if body[i] != '"' {
			name.WriteByte(body[i])

			continue
		}

		if i+1 >= len(body) || body[i+1] != '"' {
			return "", fmt.Errorf("unterminated identifier near %q", body[i:])
		}

		name.WriteByte('"')
		i++
	}

	return name.String(), nil
}

// fakeSubject is the whole integration for the fake connector -- the same one
// file a real connector writes.
func fakeSubject(d defect) Subject {
	return Subject{
		Name:         "fake",
		Connector:    &fake{defect: d},
		MaxRows:      fakeMaxRows,
		QueryTimeout: fakeTimeout,

		Fixture: Fixture{
			Table:  "fixture",
			Schema: "main",
			Name:   "fixture",
			Create: []string{"create"},
			Insert: []string{"insert"},
			Drop:   []string{"drop"},
		},

		SQL: Expressions{
			Sleep:             func(seconds int) string { return fmt.Sprintf("sleep %d", seconds) },
			Series:            func(n int) string { return fmt.Sprintf("series %d", n) },
			SyntaxError:       "boom",
			MissingTable:      "missing",
			AwkwardIdentifier: `a "quoted" name`,
		},
	}
}
