package api

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/query"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

/*
How a pipeline failure becomes an answer.

An internal test, because this is a pure function and the distinctions it draws
are the whole point of it -- reaching them through HTTP would mean contriving
an overloaded instance and a broken authorization store to check a switch.

The distinctions are the ones a person in a browser acts on, and getting any of
them wrong sends somebody somewhere useless: a denial dressed as a syntax error
has them rewriting correct SQL, and a busy instance dressed as a source failure
has them asking a DBA about a healthy database.
*/
func TestAPipelineFailureBecomesTheRightAnswer(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err  error
		want Code
	}{
		"a full instance is worth retrying": {
			fmt.Errorf("query: %w", query.ErrTooBusy), CodeQueryBusy,
		},
		"nothing to run is the caller's to fix": {
			query.ErrEmptySQL, CodeQueryRejected,
		},
		"no connection named": {
			query.ErrNoConnection, CodeQueryRejected,
		},
		"a connection that does not exist": {
			fmt.Errorf("resolve: %w", repo.ErrNotFound), CodeNotFound,
		},
		"a denial is a denial": {
			fmt.Errorf("query: %w", authz.ErrDenied), CodeForbidden,
		},
		// Not a denial. The caller may well be permitted and Pivot could not
		// find out; a 403 sends them to argue with an administrator about a
		// permission they already have.
		"an unreachable authorization store is not a denial": {
			fmt.Errorf("query: %w", authz.ErrUnavailable), CodeUnavailable,
		},
		"anything else is the source's": {
			errors.New("the warehouse fell over"), CodeQueryFailed,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var apiErr *APIError
			if !errors.As(queryError(tc.err), &apiErr) {
				t.Fatalf("queryError did not produce an APIError for %v", tc.err)
			}

			if apiErr.Code != tc.want {
				t.Errorf("code = %q, want %q", apiErr.Code, tc.want)
			}
		})
	}
}

/*
A rejected statement carries the source's own words.

"no such column: nope" is the entire answer. Anything that paraphrases it sends
somebody to check their connection, their permissions and their network before
they find the typo.
*/
func TestASourceRejectionKeepsTheSourcesWords(t *testing.T) {
	t.Parallel()

	sourceErr := &connectors.Error{
		Reason:  connectors.ReasonSyntax,
		Message: "no such column: nope",
	}

	var apiErr *APIError
	if !errors.As(queryError(fmt.Errorf("execute: %w", sourceErr)), &apiErr) {
		t.Fatal("not an APIError")
	}

	if apiErr.Message != "no such column: nope" {
		t.Errorf("message = %q; the source's own words were lost", apiErr.Message)
	}
}

/*
Cells cross into JSON as themselves.

Bytes as text rather than base64 noise, and a time as RFC 3339 so the zone
survives -- Part 19-a distinguished an instant from a wall-clock reading, and
the last step is the easiest place to throw that away.
*/
func TestCellsCrossIntoJSONAsThemselves(t *testing.T) {
	t.Parallel()

	moment := time.Date(2026, 9, 29, 10, 30, 0, 0, time.UTC)

	cases := map[string]struct {
		in   any
		want any
	}{
		"bytes become text":      {[]byte("hello"), "hello"},
		"a time keeps its zone":  {moment, "2026-09-29T10:30:00Z"},
		"a number is left alone": {int64(42), int64(42)},
		"a null stays null":      {nil, nil},
		"a string is untouched":  {"already text", "already text"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := jsonSafe(tc.in); got != tc.want {
				t.Errorf("jsonSafe(%#v) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}
