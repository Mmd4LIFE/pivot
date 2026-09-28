package query

import (
	"slices"
	"testing"
	"time"
)

/*
p95 for a cached query, measured.

Measured rather than asserted, following Part 20-a: a test that runs one cached
query and checks it took under 200 ms passes against an implementation that
takes 199, and tells nobody. This records every sample, reports the
distribution, and fails on the ninety-fifth percentile.

What is timed is the whole pipeline -- authorize, fingerprint, plan, lookup,
replay to exhaustion and close -- because that is what a caller waits for. The
query log cannot answer this question: its duration is whole milliseconds and
a cache hit is three orders of magnitude below one.
*/
func TestACachedQueryIsFastEnough(t *testing.T) {
	t.Parallel()

	const (
		samples = 200
		budget  = 200 * time.Millisecond
	)

	f := newCacheFixture(t)
	ctx := f.as(t, f.analyst)

	// Warm it, and confirm the warm-up really was a miss -- a measurement of
	// two hundred misses would pass this test and mean nothing.
	warm, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("warm: %v", err)
	}

	drain(t, warm)

	if warm.CacheStatus != CacheMiss {
		t.Fatalf("the warm-up reported %q, want miss", warm.CacheStatus)
	}

	timings := make([]time.Duration, 0, samples)

	for i := range samples {
		start := time.Now()

		ex, eerr := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
		if eerr != nil {
			t.Fatalf("sample %d: %v", i, eerr)
		}

		for ex.Stream.Next() { //nolint:revive // draining is the work being timed
		}

		if cerr := ex.Stream.Close(); cerr != nil {
			t.Fatalf("sample %d close: %v", i, cerr)
		}

		timings = append(timings, time.Since(start))

		if ex.CacheStatus != CacheHit {
			t.Fatalf("sample %d reported %q, want hit", i, ex.CacheStatus)
		}
	}

	slices.Sort(timings)

	p50 := timings[len(timings)*50/100]
	p95 := timings[len(timings)*95/100]
	worst := timings[len(timings)-1]

	// Reported whether or not it passes. A number in the log is what makes
	// the next person's regression visible; a bare pass is not.
	t.Logf("cached query over %d samples: p50 %v, p95 %v, max %v", samples, p50, p95, worst)

	if p95 > budget {
		t.Errorf("p95 is %v, over the %v budget", p95, budget)
	}

	// The source was opened exactly once, for the warm-up. Without this the
	// measurement could be of two hundred perfectly quick round trips to a
	// local SQLite file.
	if opens := f.opens.Load(); opens != 1 {
		t.Errorf("the source was opened %d times during the measurement, want 1", opens)
	}
}
