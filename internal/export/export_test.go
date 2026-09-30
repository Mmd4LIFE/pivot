package export_test

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/datatype"
	"github.com/Mmd4LIFE/pivot/internal/export"
)

/*
The writers, checked against the decisions in the package doc rather than
against a golden file of convenience.

NULL against empty, DATE without a midnight, Decimal as a JSON string, and a
hand-written escapers agreement with encoding/json: each is a place the file
would be wrong while still looking fine.
*/

func TestParseFormat(t *testing.T) {
	t.Parallel()

	cases := map[string]export.Format{
		"csv":  export.CSV,
		"CSV":  export.CSV,
		" tsv": export.TSV,
		"json": export.JSON,
	}

	for name, want := range cases {
		got, err := export.ParseFormat(name)
		if err != nil {
			t.Errorf("ParseFormat(%q): %v", name, err)
			continue
		}

		if got != want {
			t.Errorf("ParseFormat(%q) = %q, want %q", name, got, want)
		}
	}

	_, err := export.ParseFormat("xlsx")
	if err == nil {
		t.Fatal("ParseFormat(xlsx) succeeded; Excel is 24-b's and must refuse for now")
	}

	if !strings.Contains(err.Error(), "csv") {
		t.Errorf("refusal = %q, want it to name the formats that do exist", err)
	}
}

func TestCSVKeepsNullAndEmptyApart(t *testing.T) {
	t.Parallel()

	columns := []export.Column{{Name: "id", Kind: datatype.Integer}, {Name: "note", Kind: datatype.String}}
	rows := &sliceRows{rows: [][]any{
		{int64(1), nil},
		{int64(2), ""},
		{int64(3), "hello"},
	}}

	var buf bytes.Buffer
	n, err := export.Write(&buf, export.CSV, columns, rows)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if n != 3 {
		t.Errorf("wrote %d rows, want 3", n)
	}

	// CRLF, RFC 4180. An unquoted empty field is NULL; a quoted one is "".
	want := "id,note\r\n1,\r\n2,\"\"\r\n3,hello\r\n"
	if got := buf.String(); got != want {
		t.Errorf("csv =\n%q\nwant\n%q", got, want)
	}
}

func TestTSVQuotesFieldsThatNeedIt(t *testing.T) {
	t.Parallel()

	columns := []export.Column{{Name: "a", Kind: datatype.String}, {Name: "b", Kind: datatype.String}}
	rows := &sliceRows{rows: [][]any{
		{"plain", "has\ttab"},
		{"has\"quote", "line\nbreak"},
	}}

	var buf bytes.Buffer
	if _, err := export.Write(&buf, export.TSV, columns, rows); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "\"has\ttab\"") {
		t.Errorf("tab-bearing field was not quoted: %q", got)
	}

	if !strings.Contains(got, "\"has\"\"quote\"") {
		t.Errorf("quote was not doubled: %q", got)
	}
}

func TestDatesFormatFromTheKind(t *testing.T) {
	t.Parallel()

	instant := time.Date(2026, 9, 26, 15, 4, 5, 0, time.FixedZone("UTC+3:30", 3*3600+1800))

	columns := []export.Column{
		{Name: "d", Kind: datatype.Date},
		{Name: "ts", Kind: datatype.Timestamp},
		{Name: "tz", Kind: datatype.TimestampTZ},
	}
	rows := &sliceRows{rows: [][]any{{instant, instant, instant}}}

	var buf bytes.Buffer
	if _, err := export.Write(&buf, export.CSV, columns, rows); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := buf.String()
	lines := strings.Split(strings.TrimSuffix(got, "\r\n"), "\r\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %v", lines)
	}

	cells := strings.Split(lines[1], ",")
	if len(cells) != 3 {
		t.Fatalf("cells = %v", cells)
	}

	if cells[0] != "2026-09-26" {
		t.Errorf("DATE = %q, want a calendar day with no time", cells[0])
	}

	// A Timestamp has no zone. A trailing offset or a T would invent one.
	if strings.Contains(cells[1], "T") || strings.Contains(cells[1], "Z") ||
		strings.Contains(cells[1], "+") {
		t.Errorf("Timestamp = %q, want a wall-clock reading without a zone", cells[1])
	}

	if !strings.Contains(cells[2], "+03:30") {
		t.Errorf("TimestampTZ = %q, want the offset kept", cells[2])
	}
}

func TestJSONWritesDecimalAsString(t *testing.T) {
	t.Parallel()

	columns := []export.Column{
		{Name: "total", Kind: datatype.Decimal},
		{Name: "ok", Kind: datatype.Boolean},
		{Name: "missing", Kind: datatype.String},
	}
	rows := &sliceRows{rows: [][]any{
		{"19.9900", true, nil},
	}}

	var buf bytes.Buffer
	if _, err := export.Write(&buf, export.JSON, columns, rows); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var docs []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &docs); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}

	if len(docs) != 1 {
		t.Fatalf("docs = %d, want 1", len(docs))
	}

	total, ok := docs[0]["total"].(string)
	if !ok || total != "19.9900" {
		t.Errorf("total = %#v, want the string \"19.9900\" (a JSON number would be a float64)", docs[0]["total"])
	}

	if docs[0]["ok"] != true {
		t.Errorf("ok = %#v, want true", docs[0]["ok"])
	}

	if docs[0]["missing"] != nil {
		t.Errorf("missing = %#v, want null", docs[0]["missing"])
	}
}

func TestJSONEscapingAgreesWithEncodingJSON(t *testing.T) {
	t.Parallel()

	// The corpus that would catch a hand-rolled escaper lying: controls, a
	// quote, a backslash, emoji, and a line/paragraph separator that is legal
	// in JSON and illegal in a JavaScript string.
	inputs := []string{
		"",
		"plain",
		`say "hello"`,
		"back\\slash",
		"line\nbreak\tand\rreturn",
		"emoji 🦆",
		"sep\u2028here\u2029",
		string([]byte{0x01, 0x1f}),
	}

	columns := []export.Column{{Name: "s", Kind: datatype.String}}

	for _, in := range inputs {
		rows := &sliceRows{rows: [][]any{{in}}}

		var buf bytes.Buffer
		if _, err := export.Write(&buf, export.JSON, columns, rows); err != nil {
			t.Fatalf("Write(%q): %v", in, err)
		}

		var docs []map[string]any
		if err := json.Unmarshal(buf.Bytes(), &docs); err != nil {
			t.Fatalf("our output for %q is not JSON: %v\n%s", in, err, buf.String())
		}

		want, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshal reference: %v", err)
		}

		got, err := json.Marshal(docs[0]["s"])
		if err != nil {
			t.Fatalf("remarshal: %v", err)
		}

		if !bytes.Equal(got, want) {
			t.Errorf("round-trip of %q: got %s, want %s", in, got, want)
		}
	}
}

func TestJSONRefusesNaN(t *testing.T) {
	t.Parallel()

	columns := []export.Column{{Name: "x", Kind: datatype.Float}}
	rows := &sliceRows{rows: [][]any{{math.NaN()}, {math.Inf(1)}}}

	var buf bytes.Buffer
	if _, err := export.Write(&buf, export.JSON, columns, rows); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if !bytes.Contains(buf.Bytes(), []byte(`"x":null`)) {
		t.Errorf("NaN/Inf should become null, got %s", buf.String())
	}

	var docs []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &docs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}

func TestContentTypeAndExtension(t *testing.T) {
	t.Parallel()

	if export.CSV.ContentType() != "text/csv; charset=utf-8" {
		t.Errorf("CSV content type = %q", export.CSV.ContentType())
	}

	if export.JSON.Extension() != "json" {
		t.Errorf("JSON extension = %q", export.JSON.Extension())
	}
}

// sliceRows is a finite Rows for the correctness tests.
type sliceRows struct {
	rows [][]any
	i    int
}

func (s *sliceRows) Next() bool {
	if s.i >= len(s.rows) {
		return false
	}

	s.i++

	return true
}

func (s *sliceRows) Row() []any { return s.rows[s.i-1] }

func (s *sliceRows) Err() error { return nil }
