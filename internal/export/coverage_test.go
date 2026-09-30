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
Coverage of the paths the happy-path tests do not walk: every numeric Go type
the drivers hand back, a Binary column that must be base64, a JSON column that
passes through or is quoted, and the formats' Content-Type strings.
*/

func TestTextRendersEveryDriverShape(t *testing.T) {
	t.Parallel()

	columns := []export.Column{
		{Name: "b", Kind: datatype.Boolean},
		{Name: "f64", Kind: datatype.Float},
		{Name: "f32", Kind: datatype.Float},
		{Name: "i64", Kind: datatype.Integer},
		{Name: "i", Kind: datatype.Integer},
		{Name: "i32", Kind: datatype.Integer},
		{Name: "u64", Kind: datatype.Integer},
		{Name: "bin", Kind: datatype.Binary},
		{Name: "bytes_as_text", Kind: datatype.String},
		{Name: "clock", Kind: datatype.Time},
		{Name: "other", Kind: datatype.Unknown},
	}

	clock := time.Date(2026, 9, 30, 15, 4, 5, 0, time.UTC)
	rows := &sliceRows{rows: [][]any{{
		true,
		float64(1.5),
		float32(2.25),
		int64(7),
		int(8),
		int32(9),
		uint64(10),
		[]byte{0xde, 0xad},
		[]byte("not-binary"),
		clock,
		struct{ X int }{X: 1},
	}}}

	var buf bytes.Buffer
	if _, err := export.Write(&buf, export.CSV, columns, rows); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := buf.String()
	for _, want := range []string{
		"true", "1.5", "2.25", "7", "8", "9", "10",
		"3q0=", // base64 of {0xde, 0xad}
		"not-binary",
		"15:04:05",
		"{1}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("csv missing %q:\n%s", want, got)
		}
	}
}

func TestJSONColumnPassesThroughOrQuotes(t *testing.T) {
	t.Parallel()

	columns := []export.Column{{Name: "doc", Kind: datatype.JSON}}
	rows := &sliceRows{rows: [][]any{
		{[]byte(`{"a":1}`)},
		{`{"b":2}`},
		{"not json"},
		{42},
	}}

	var buf bytes.Buffer
	if _, err := export.Write(&buf, export.JSON, columns, rows); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var docs []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &docs); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}

	if len(docs) != 4 {
		t.Fatalf("docs = %d, want 4", len(docs))
	}

	if _, ok := docs[0]["doc"].(map[string]any); !ok {
		t.Errorf("valid []byte JSON should pass through as an object, got %#v", docs[0]["doc"])
	}

	if _, ok := docs[1]["doc"].(map[string]any); !ok {
		t.Errorf("valid string JSON should pass through as an object, got %#v", docs[1]["doc"])
	}

	if docs[2]["doc"] != "not json" {
		t.Errorf("invalid JSON should be quoted as a string, got %#v", docs[2]["doc"])
	}

	// A non-string, non-[]byte falls back to text then a JSON string.
	if s, ok := docs[3]["doc"].(string); !ok || s != "42" {
		t.Errorf("other type = %#v, want the string \"42\"", docs[3]["doc"])
	}
}

func TestJSONWritesIntegersAndFloatsAsNumbers(t *testing.T) {
	t.Parallel()

	columns := []export.Column{
		{Name: "i64", Kind: datatype.Integer},
		{Name: "i", Kind: datatype.Integer},
		{Name: "i32", Kind: datatype.Integer},
		{Name: "u64", Kind: datatype.Integer},
		{Name: "f64", Kind: datatype.Float},
		{Name: "f32", Kind: datatype.Float},
		{Name: "ok", Kind: datatype.Boolean},
		{Name: "inf", Kind: datatype.Float},
	}
	rows := &sliceRows{rows: [][]any{{
		int64(1), int(2), int32(3), uint64(4),
		float64(1.25), float32(2.5), true, math.Inf(-1),
	}}}

	var buf bytes.Buffer
	if _, err := export.Write(&buf, export.JSON, columns, rows); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var docs []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &docs); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}

	want := map[string]any{
		"i64": float64(1), "i": float64(2), "i32": float64(3), "u64": float64(4),
		"f64": 1.25, "f32": 2.5, "ok": true, "inf": nil,
	}

	for key, expected := range want {
		if docs[0][key] != expected {
			t.Errorf("%s = %#v, want %#v", key, docs[0][key], expected)
		}
	}
}

func TestContentTypesForEveryFormat(t *testing.T) {
	t.Parallel()

	cases := map[export.Format]string{
		export.CSV:  "text/csv; charset=utf-8",
		export.TSV:  "text/tab-separated-values; charset=utf-8",
		export.JSON: "application/json; charset=utf-8",
		export.Format("nope"): "application/octet-stream",
	}

	for format, want := range cases {
		if got := format.ContentType(); got != want {
			t.Errorf("%q.ContentType() = %q, want %q", format, got, want)
		}
	}
}

func TestWriteRejectsAnUnknownFormat(t *testing.T) {
	t.Parallel()

	_, err := export.Write(
		&bytes.Buffer{},
		export.Format("xlsx"),
		[]export.Column{{Name: "a", Kind: datatype.Integer}},
		&sliceRows{},
	)
	if err == nil {
		t.Fatal("Write(xlsx) succeeded")
	}
}

func TestAShortRowIsWrittenAsNullCells(t *testing.T) {
	t.Parallel()

	columns := []export.Column{
		{Name: "a", Kind: datatype.Integer},
		{Name: "b", Kind: datatype.String},
	}
	rows := &sliceRows{rows: [][]any{{int64(1)}}} // missing b

	var buf bytes.Buffer
	if _, err := export.Write(&buf, export.CSV, columns, rows); err != nil {
		t.Fatalf("Write: %v", err)
	}

	want := "a,b\r\n1,\r\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
