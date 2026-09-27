package conformance

import "time"

/*
The fixture's expected contents.

The suite owns what the rows mean; a subject owns how to spell them in its
dialect. That split is what keeps the suite from knowing about any particular
connector, and the two cannot drift apart silently: a subject whose INSERT
disagrees with this fails values_survive_the_round_trip, which is the check's
whole job.

Every value here is chosen to break something that has broken before:

  - Row 2's name carries a combining accent, a precomposed umlaut, a
    four-byte emoji and a right-to-left script, so a connector that
    round-trips through Latin-1, normalizes unicode, or counts bytes as
    characters produces a different string.

  - Row 2's notes is NULL and row 3's is the empty string, which a driver
    that maps one to the other cannot satisfy both of.

  - created_utc in row 1 is late enough in the day that any session timezone
    east of UTC puts it on the next date -- the off-by-one-day bug, made to
    happen on purpose.

  - Row 3's timestamps sit on the 32-bit epoch boundaries, where a driver
    that narrows to int32 seconds wraps.

  - The ratios are exactly representable as binary floats, so a mismatch is a
    real loss rather than the usual decimal-printing noise.
*/

// Row is one row of the suite's fixture table.
type Row struct {
	ID   int64
	Name string

	// Notes is nil for SQL NULL. A pointer rather than a string plus a bool,
	// because the distinction being tested is exactly nil versus empty.
	Notes *string

	Flag  bool
	Ratio float64

	// CreatedUTC is an instant. Whatever zone the source stores or returns it
	// in, it has to be this moment.
	CreatedUTC time.Time

	// CreatedNaive is a wall-clock reading with no zone. It has to come back
	// with these calendar fields, and a connector that attaches a zone and
	// shifts the clock has changed the value.
	CreatedNaive time.Time
}

// Columns is the fixture's columns, in the order the suite selects them.
//
// Exported so a subject's DDL can be checked against it by eye, and so the
// column-metadata check has one list rather than a literal in two places.
func Columns() []string {
	return []string{"id", "name", "notes", "flag", "ratio", "created_utc", "created_naive"}
}

// Rows is what the fixture must contain, in id order.
func Rows() []Row {
	present, empty := "present", ""

	return []Row{
		{
			ID:    1,
			Name:  "plain ascii",
			Notes: &present,
			Flag:  true,
			Ratio: 1.5,
			// 23:30 UTC is already tomorrow anywhere east of UTC+00:30.
			CreatedUTC:   time.Date(2024, time.March, 15, 23, 30, 0, 0, time.UTC),
			CreatedNaive: time.Date(2024, time.March, 15, 1, 15, 0, 0, time.UTC),
		},
		{
			ID: 2,
			// A combining acute, a precomposed umlaut, an emoji outside the
			// basic plane, and Arabic.
			Name:         "héllo wörld \U0001F30D مرحبا",
			Notes:        nil,
			Flag:         false,
			Ratio:        -2.25,
			CreatedUTC:   time.Date(1999, time.December, 31, 23, 59, 59, 0, time.UTC),
			CreatedNaive: time.Date(2000, time.January, 1, 0, 0, 1, 0, time.UTC),
		},
		{
			ID: 3,
			// Empty, and it has to stay distinct from row 2's NULL.
			Name:         "",
			Notes:        &empty,
			Flag:         true,
			Ratio:        0,
			CreatedUTC:   time.Date(2038, time.January, 19, 3, 14, 7, 0, time.UTC),
			CreatedNaive: time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
	}
}
