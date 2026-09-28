package query

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/Mmd4LIFE/pivot/internal/observability"
)

/*
Instruments is what the cache reports about itself.

Four series and one label, and the label has three values. That restraint is
the observability package's own rule -- every series is cardinality somebody
pays for at query time -- and it is worth restating here because a cache is
exactly where the temptation goes wrong: a counter labeled by cache key, or
by user, is the documented way to make Prometheus unusable, and it is also the
first thing anybody reaches for when a hit rate looks low.

The question these answer is "is the cache working": the hit rate comes from
one counter filtered by label, which keeps hits and misses consistent by
construction, and the two gauges say whether it is full.
*/
type Instruments struct {
	lookups   metric.Int64Counter
	evictions metric.Int64Counter
}

// resultKey labels a lookup with hit, miss or uncached -- the same vocabulary
// the query log's cache_status column uses, so a dashboard and the log cannot
// disagree about what happened.
const resultKey = attribute.Key("pivot.cache.result")

/*
newInstruments builds the cache's instruments on the global meter.

Never fails. An instrument that could not be created is left nil and recording
through it does nothing, because a cache that refused to start because its
counter would not register would be trading the feature for the measurement of
the feature.

The global meter provider rather than one passed in, because OpenTelemetry's
global delegates: instruments created before [observability.SetupMetrics]
installs the real provider are repointed at it when it arrives, which is the
ordering every caller here actually has.
*/
func newCacheInstruments() *Instruments {
	meter := otel.GetMeterProvider().Meter(observability.ScopeName)

	lookups, err := meter.Int64Counter(
		"pivot.query.cache.lookups",
		metric.WithDescription("Result cache lookups, by outcome"),
		metric.WithUnit("{lookup}"),
	)
	if err != nil {
		lookups = nil
	}

	evictions, err := meter.Int64Counter(
		"pivot.query.cache.evictions",
		metric.WithDescription("Entries dropped to stay within the byte budget"),
		metric.WithUnit("{entry}"),
	)
	if err != nil {
		evictions = nil
	}

	return &Instruments{lookups: lookups, evictions: evictions}
}

func (i *Instruments) recordLookup(ctx context.Context, result string) {
	if i == nil || i.lookups == nil {
		return
	}

	i.lookups.Add(ctx, 1, metric.WithAttributes(resultKey.String(result)))
}

func (i *Instruments) recordEviction(ctx context.Context, n int64) {
	if i == nil || i.evictions == nil || n == 0 {
		return
	}

	i.evictions.Add(ctx, n)
}

/*
observe registers the two gauges, which are read from the cache rather than
pushed to it.

Observable rather than incremented, because "how full is it" is a question
about the cache's current state and a counter maintained alongside the state
is a second copy of it that can be wrong. The callback reads [Cache.Stats],
which reads under the same lock everything else does.

Returns nothing, like [newCacheInstruments] and for the same reason: the only
failure is that this meter already carries these instruments, which happens
when a process builds a second cache, and a cache that refused to exist over it
would have traded the feature for the measurement of the feature.
*/
func (c *Cache) observe() {
	meter := otel.GetMeterProvider().Meter(observability.ScopeName)

	entries, err := meter.Int64ObservableGauge(
		"pivot.query.cache.entries",
		metric.WithDescription("Results held in the cache"),
		metric.WithUnit("{entry}"),
	)
	if err != nil {
		return
	}

	bytes, err := meter.Int64ObservableGauge(
		"pivot.query.cache.bytes",
		metric.WithDescription("Memory the cached results occupy"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return
	}

	// The registration handle is not kept: these gauges live as long as the
	// cache, which lives as long as the process, so there is nothing to
	// unregister and nothing to hold it for.
	if _, err = meter.RegisterCallback(
		func(_ context.Context, o metric.Observer) error {
			stats := c.Stats()

			o.ObserveInt64(entries, int64(stats.Entries))
			o.ObserveInt64(bytes, stats.Bytes)

			return nil
		},
		entries, bytes,
	); err != nil {
		return
	}
}
