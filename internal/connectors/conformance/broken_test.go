package conformance

import (
	"strings"
	"testing"
)

/*
The suite's own test: can it fail?

A conformance suite that passes is worth exactly as much as the confidence that
it would have failed, and that confidence cannot come from reading it. So every
defect below is a connector bug that has really happened to somebody, wired
into a working connector one at a time, with the property it must break named
next to it.

If a row here stops failing, the corresponding check has quietly stopped
checking -- which is the failure mode this file exists to catch.
*/

// defects maps a deliberate bug to the properties it must break.
var defects = map[string]struct {
	defect defect
	breaks []string
}{
	"a NULL arrives as an empty string": {
		defectNullBecomesEmpty, []string{"null_and_empty_are_distinct"},
	},
	"unicode is normalized on the way out": {
		defectUnicodeNormalized, []string{"unicode_is_unchanged"},
	},
	"a zoned timestamp is shifted by an hour": {
		defectInstantShifted, []string{"timestamps_keep_their_instant"},
	},
	"a zone is applied to a naive timestamp": {
		defectNaiveShifted, []string{"timestamps_keep_their_instant"},
	},
	"columns come back with no source type": {
		defectNoSourceType, []string{"columns_describe_themselves"},
	},
	"a table that exists is not cataloged": {
		defectUncataloged, []string{"introspection_finds_the_fixture"},
	},
	"a NOT NULL column is cataloged as nullable": {
		defectNullableID, []string{"introspection_finds_the_fixture"},
	},
	"a numbered placeholder style binds positionally": {
		defectPositionalBinding, []string{"declared_placeholder_is_the_real_one"},
	},
	"CTEs are declared and do not work": {
		defectLyingCTEs, []string{"declared_capabilities_are_true"},
	},
	"lateral joins are declared with nothing to prove them": {
		defectUnprovenLateral, []string{"declared_capabilities_are_true"},
	},
	"quoting does not escape the quote character": {
		defectUnescapedQuoting, []string{"quoted_identifiers_round_trip"},
	},
	"a row is repeated mid-stream": {
		defectDropsRows, []string{"a_large_result_arrives_whole"},
	},
	"a result is cut at the cap without the flag": {
		defectSilentTruncation, []string{"row_limit_truncates_with_a_signal"},
	},
	"a canceled query finishes anyway": {
		defectIgnoresCancel, []string{"cancellation_is_prompt_and_says_so"},
	},
	"a cancellation is reported as an unknown failure": {
		defectWrongCancelReason, []string{"cancellation_is_prompt_and_says_so"},
	},
	"a timeout is reported as a cancellation": {
		defectWrongTimeoutReason, []string{"a_timeout_is_reported_as_one"},
	},
	"errors are not classified at all": {
		defectRawErrors, []string{"a_syntax_error_says_so", "a_missing_table_says_so"},
	},
}

/*
Each defect fails its property, and the failure names it.

The second half is the part that makes the suite usable. A connector author
reads "timestamps_keep_their_instant: id 1: created_utc is ... (off by 1h0m0s)"
and knows what to go and look at; a line number in a file they have never
opened tells them to go and read this package first.
*/
func TestEachDefectFailsItsOwnProperty(t *testing.T) {
	t.Parallel()

	for name, tc := range defects {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			subject := fakeSubject(tc.defect)

			if err := subject.createFixture(t.Context()); err != nil {
				t.Fatalf("fixture: %v", err)
			}

			for _, property := range tc.breaks {
				check := checkNamed(t, property)

				err := check.Run(t.Context(), subject)
				if err == nil {
					t.Errorf("%s passed against a connector where %s", property, name)

					continue
				}

				failure := check.Failure(err)

				if !strings.HasPrefix(failure, property+": ") {
					t.Errorf("the failure does not open with the property:\n%s", failure)
				}

				if strings.TrimSpace(strings.TrimPrefix(failure, property+":")) == "" {
					t.Errorf("%s failed with nothing after the property name", property)
				}

				// Read it once by eye: this is what a connector author sees.
				t.Log(failure)
			}
		})
	}
}

/*
A correct connector passes every check, and every check runs.

Two claims at once. The first is that the suite is satisfiable by something
that is not PostgreSQL -- the fake's dialect is "series 40" and "sleep 3", so a
suite shaped around Postgres would not survive it. The second is that nothing
is skipped: a check whose requirement no subject can meet is a check that never
runs, which is the quietest way for a suite to stop testing anything.
*/
func TestACorrectConnectorPassesEverything(t *testing.T) {
	t.Parallel()

	subject := fakeSubject(defectNone)

	if err := subject.createFixture(t.Context()); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	ran := 0

	for _, check := range Checks() {
		// LateralJoins is the one capability the fake does not declare, so the
		// subject legitimately supplies no lateral query. Everything else has
		// to run.
		if missing := subject.cannot(check.Needs); missing != "" {
			t.Errorf("%s was skipped: the fake does not supply %s", check.Property, missing)

			continue
		}

		if err := check.Run(t.Context(), subject); err != nil {
			t.Error(check.Failure(err))
		}

		ran++
	}

	if ran != len(Checks()) {
		t.Errorf("%d of %d checks ran", ran, len(Checks()))
	}
}

// Run is the entry point a connector actually calls, so it is exercised end to
// end rather than only through its parts -- including that it creates the
// fixture, names each subtest after its property, and cleans up afterwards.
func TestRunAgainstACorrectConnector(t *testing.T) {
	t.Parallel()

	Run(t, fakeSubject(defectNone))
}

/*
Every property is named once, in snake_case.

Names are the suite's interface: they appear in CI output, in `go test -run`
arguments and in the bug reports people file against connectors. A duplicate
makes two different failures indistinguishable, and a rename breaks somebody's
command line, so both are worth a test rather than a habit.
*/
func TestPropertyNamesAreUsable(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}

	for _, check := range Checks() {
		switch {
		case check.Property == "":
			t.Error("a check has no property name")

		case seen[check.Property]:
			t.Errorf("%q names two checks", check.Property)

		case strings.ToLower(check.Property) != check.Property,
			strings.Contains(check.Property, " "),
			strings.Contains(check.Property, "-"):
			t.Errorf("%q is not snake_case", check.Property)
		}

		if check.Run == nil {
			t.Errorf("%q has no implementation", check.Property)
		}

		seen[check.Property] = true
	}
}

func checkNamed(t *testing.T, property string) Check {
	t.Helper()

	for _, check := range Checks() {
		if check.Property == property {
			return check
		}
	}

	t.Fatalf("there is no check called %q", property)

	return Check{}
}
