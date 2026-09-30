package export

import (
	"fmt"
	"io"
	"strings"

	"github.com/Mmd4LIFE/pivot/internal/datatype"
)

/*
Format is a shape Pivot can write a result in.

A string rather than an integer because it arrives from a form field and is
echoed in a filename and a content type; an enum that has to be translated in
three places is an enum that will disagree with itself in one of them.
*/
type Format string

const (
	// CSV is comma-separated, quoted per RFC 4180.
	CSV Format = "csv"

	// TSV is the same rules with a tab. Not the backslash-escaping convention
	// PostgreSQL's COPY TEXT uses: `\N` for NULL is unambiguous and is also
	// what somebody sees in the cell after pasting into a spreadsheet, which
	// is what people do with a TSV.
	TSV Format = "tsv"

	// JSON is an array of objects, and the only format here that can carry
	// the difference between a NULL and an empty string without a convention.
	JSON Format = "json"
)

/*
ParseFormat reads a format name, or says what the choices are.

Named rather than defaulted: a request for `xlsx` before Part 24-b builds it
must fail saying so, not quietly hand somebody a CSV with the wrong extension
on it.
*/
func ParseFormat(name string) (Format, error) {
	switch Format(strings.ToLower(strings.TrimSpace(name))) {
	case CSV:
		return CSV, nil
	case TSV:
		return TSV, nil
	case JSON:
		return JSON, nil
	default:
		return "", fmt.Errorf("export: %q is not a format Pivot writes (csv, tsv, json)", name)
	}
}

// ContentType is what the format is served as.
func (f Format) ContentType() string {
	switch f {
	case CSV:
		return "text/csv; charset=utf-8"
	case TSV:
		return "text/tab-separated-values; charset=utf-8"
	case JSON:
		return "application/json; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

// Extension is the filename suffix, without the dot.
func (f Format) Extension() string { return string(f) }

/*
Column is a column's name and what it means.

Deliberately not [query.Column] and deliberately not the connector's: this
package formats values and needs exactly two facts to do it. Taking the
pipeline's struct would put a second importer on a type that belongs to the
pipeline, and taking the connector's would put this package on the wrong side
of the single-door test for no benefit at all.
*/
type Column struct {
	Name string
	Kind datatype.Kind
}

/*
Rows is a cursor over a result.

The subset of [connectors.Stream] this package actually uses, redeclared here
so that nothing in export imports the connector package -- see the single-door
test in internal/query. It is satisfied by a stream without an adapter.

Next reports whether a row was read. Row is valid only until the next call to
Next, which is why every writer here formats immediately rather than keeping
the slice. Err must be checked after the loop: a stream that ended because the
source failed and one that ended because the rows ran out look identical from
Next.
*/
type Rows interface {
	Next() bool
	Row() []any
	Err() error
}

/*
Write streams a result to w.

Returns the number of rows written along with any error, because a failure
partway through a ten-minute download is worth logging with the point it
reached -- "the export failed" and "the export failed after 9.4 million rows"
send somebody to different places.

A short write, a client that hung up, or a source that failed mid-stream all
come back as errors here. By then bytes are already on the wire and the status
line is long gone, so the caller cannot turn it into a clean HTTP error: what
it can do is truncate the download, which is what a dropped connection does,
and record what happened.
*/
func Write(w io.Writer, format Format, columns []Column, rows Rows) (int64, error) {
	switch format {
	case CSV:
		return writeDelimited(w, ',', columns, rows)
	case TSV:
		return writeDelimited(w, '\t', columns, rows)
	case JSON:
		return writeJSON(w, columns, rows)
	default:
		return 0, fmt.Errorf("export: no writer for %q", format)
	}
}
