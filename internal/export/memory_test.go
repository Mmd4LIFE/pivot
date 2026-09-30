package export_test

import (
	"io"
	"runtime"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/datatype"
	"github.com/Mmd4LIFE/pivot/internal/export"
)

/*
TestMemoryDoesNotGrowWithRows is the Done-when for Part 24-a.

A ten-million-row export that assembled the document would grow with the
answer; one that forgets each row after writing it would not. Comparing two
sizes rather than asserting a ceiling is deliberate: a ceiling is a guess
about the machine, and flatness is the claim.
*/

func TestMemoryDoesNotGrowWithRows(t *testing.T) {
	const (
		small = 100_000
		large = 1_000_000
	)

	smallPeak := peakHeapWhileExporting(t, small)
	largePeak := peakHeapWhileExporting(t, large)

	t.Logf("peak heap: %d rows -> %.1f MB, %d rows -> %.1f MB",
		small, float64(smallPeak)/(1<<20), large, float64(largePeak)/(1<<20))

	// Ten times the rows must not be anything like ten times the memory. Two
	// times is a generous ceiling that a materializing writer cannot meet and
	// a streaming one clears with room to spare.
	if largePeak > smallPeak*2 {
		t.Errorf("peak heap went from %.1f MB to %.1f MB for 10x the rows, "+
			"so the document is being assembled rather than streamed",
			float64(smallPeak)/(1<<20), float64(largePeak)/(1<<20))
	}

	const ceiling = 64 << 20

	if largePeak > ceiling {
		t.Errorf("exporting %d rows peaked at %.1f MB, over the %d MB ceiling",
			large, float64(largePeak)/(1<<20), ceiling>>20)
	}
}

func peakHeapWhileExporting(t *testing.T, n int) uint64 {
	t.Helper()

	runtime.GC()

	var before runtime.MemStats

	runtime.ReadMemStats(&before)

	columns := []export.Column{
		{Name: "id", Kind: datatype.Integer},
		{Name: "region", Kind: datatype.String},
		{Name: "amount", Kind: datatype.Decimal},
	}

	rows := &genRows{n: n, row: make([]any, 3)}

	written, err := export.Write(io.Discard, export.CSV, columns, rows)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if written != int64(n) {
		t.Fatalf("wrote %d rows, want %d", written, n)
	}

	var after runtime.MemStats

	runtime.ReadMemStats(&after)

	if after.HeapInuse < before.HeapInuse {
		return 0
	}

	return after.HeapInuse - before.HeapInuse
}

/*
genRows invents rows on the fly and never keeps them.

The point of the memory test is the writer, not a slice sitting behind it. A
prebuilt [][]any of a million rows would dwarf the buffer and make every
implementation look like it grew with the answer.
*/
type genRows struct {
	n, i int
	row  []any
}

func (g *genRows) Next() bool {
	if g.i >= g.n {
		return false
	}

	g.i++
	g.row[0] = int64(g.i)
	g.row[1] = "west"
	g.row[2] = "12.50"

	return true
}

func (g *genRows) Row() []any { return g.row }

func (g *genRows) Err() error { return nil }
