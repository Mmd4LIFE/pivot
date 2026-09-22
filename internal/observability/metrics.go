package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// MetricsConfig is what an operator can set for metrics.
type MetricsConfig struct {
	// Enabled exposes /metrics. Unlike tracing this defaults to *on*: a
	// Prometheus endpoint costs nothing until something scrapes it, needs no
	// collector to exist, and an operator who has to go and enable metrics
	// before they can find out why the thing is slow has been failed already.
	Enabled bool

	// ServiceName labels every series.
	ServiceName string
}

// Metrics is the instrument set the HTTP layer records into.
//
// Three instruments, and no more without a reason. Every series is cardinality
// somebody pays for at query time, and the classic way to make Prometheus
// unusable is a label with a request ID or a user ID in it.
type Metrics struct {
	// Requests counts by method, route and status class. Rate and error rate
	// both come from this one counter -- errors are not a separate metric,
	// they are a filter on the status label, which is what keeps the two
	// numbers consistent by construction.
	Requests metric.Int64Counter

	// Duration is a histogram, in seconds. A histogram rather than a gauge or
	// a summary because the question is almost always "what does the slow end
	// look like", and an average cannot answer it.
	Duration metric.Float64Histogram

	// InFlight is how many requests are being served right now. It is the
	// metric that distinguishes "slow" from "stuck".
	InFlight metric.Int64UpDownCounter
}

// SetupMetrics installs the meter provider and returns the instruments, the
// handler that serves them, and a shutdown.
//
// The handler is *returned* rather than fetched later from a package
// variable, and that is the whole shape of this function. The first version
// kept the registry in a global and exposed a MetricsHandler() accessor, which
// gave three problems in one: the variable was written without
// synchronization and the race detector found it as soon as two servers
// started at once; a second call with metrics disabled left the first call's
// handler in place; and nothing in the signature said the two had to agree.
// Returning both makes the lifetime explicit and the global unnecessary.
//
// Instruments are usable when disabled, so the HTTP layer records
// unconditionally rather than branching on every request.
func SetupMetrics(
	cfg MetricsConfig, log *slog.Logger,
) (*Metrics, http.Handler, Shutdown, error) {
	if !cfg.Enabled {
		log.Debug("metrics are disabled")

		instruments, shutdown, err := newInstruments(otel.GetMeterProvider().Meter(ScopeName))

		// A nil handler, so the router leaves /metrics unregistered and a
		// scrape gets a 404 -- which is the truth. An empty 200 would tell an
		// operator their scrape works when it is measuring nothing.
		return instruments, nil, shutdown, err
	}

	// A dedicated registry rather than prometheus.DefaultRegisterer. The
	// default is global mutable state that any dependency can register into,
	// and a duplicate registration there panics at startup -- which is a
	// strange way to find out that two libraries both wanted a "requests"
	// counter.
	registry := prometheus.NewRegistry()

	exporter, err := promexporter.New(promexporter.WithRegisterer(registry))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("observability: build the Prometheus exporter: %w", err)
	}

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	otel.SetMeterProvider(provider)

	instruments, _, err := newInstruments(provider.Meter(ScopeName))
	if err != nil {
		return nil, nil, nil, err
	}

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		// An error while gathering is worth surfacing to whoever is scraping,
		// rather than silently serving a partial set of series.
		ErrorHandling: promhttp.HTTPErrorOnError,
	})

	log.Info("metrics enabled", slog.String("path", MetricsPath))

	return instruments, handler, provider.Shutdown, nil
}

// MetricsPath is where the endpoint is mounted.
const MetricsPath = "/metrics"

// newInstruments creates the three instruments on a meter.
func newInstruments(meter metric.Meter) (*Metrics, Shutdown, error) {
	requests, err := meter.Int64Counter(
		"pivot.http.requests",
		metric.WithDescription("HTTP requests served"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("observability: request counter: %w", err)
	}

	duration, err := meter.Float64Histogram(
		"pivot.http.duration",
		metric.WithDescription("How long a request took"),
		metric.WithUnit("s"),
		// Buckets chosen for this product rather than the library default.
		// Pivot's NFRs care about the sub-second range for the API and the
		// several-second range for a query, and the default buckets spend most
		// of their resolution where nothing happens.
		metric.WithExplicitBucketBoundaries(
			0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30,
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("observability: duration histogram: %w", err)
	}

	inFlight, err := meter.Int64UpDownCounter(
		"pivot.http.in_flight",
		metric.WithDescription("Requests being served right now"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("observability: in-flight counter: %w", err)
	}

	return &Metrics{Requests: requests, Duration: duration, InFlight: inFlight},
		func(context.Context) error { return nil },
		nil
}

// StatusClass buckets a status code as 2xx, 4xx and so on.
//
// The label is the class rather than the code, deliberately. A label per status
// code multiplies every series by the number of codes the application can
// return, and no dashboard has ever needed to tell 502 from 503 before it has
// told errors from successes.
func StatusClass(status int) attribute.KeyValue {
	return attribute.String("http.status_class", fmt.Sprintf("%dxx", status/100))
}
