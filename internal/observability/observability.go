// Package observability wires OpenTelemetry tracing.
//
// It is off by default and no-ops when off, which is the important property:
// a single-binary install on somebody's laptop should not open a network
// connection to a collector that does not exist, and the instrumentation
// scattered through the rest of the codebase must cost nothing when nobody is
// collecting. OpenTelemetry's no-op tracer makes a span a couple of pointer
// assignments, so the call sites can be unconditional and honest.
//
// Export is OTLP over HTTP rather than gRPC. That was chosen to avoid pulling
// the gRPC tree and it did not work -- `go.opentelemetry.io/proto/otlp`
// depends on gRPC for its generated code either way -- but HTTP is still the
// better default: it survives proxies, it is trivially debuggable with curl,
// and every collector speaks it on 4318.
package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// ScopeName identifies Pivot's own spans, as distinct from a library's.
const ScopeName = "github.com/Mmd4LIFE/pivot"

// Config is what an operator can set. It mirrors config.ObservabilityConfig,
// restated here so this package does not import the configuration package and
// become impossible to test in isolation.
type Config struct {
	// Enabled turns tracing on. Everything below is ignored when it is false.
	Enabled bool

	// Endpoint is the collector's OTLP/HTTP address, host:port with no scheme
	// and no path -- "localhost:4318". The exporter appends /v1/traces.
	Endpoint string

	// Insecure sends over plain HTTP. True is right for a collector running
	// beside Pivot; false for anything across a network.
	Insecure bool

	// SampleRatio is the fraction of traces to keep, 0 to 1. Sampling is
	// head-based and parent-respecting: a request that arrives already sampled
	// stays sampled, so a trace is never half-recorded.
	SampleRatio float64

	// ServiceName and ServiceVersion identify this process to the collector.
	ServiceName    string
	ServiceVersion string
}

// Shutdown flushes and stops the exporter. Always non-nil, so a caller can
// defer it without checking.
type Shutdown func(context.Context) error

// Setup installs the global tracer provider and propagator.
//
// The returned Shutdown must be called before the process exits: spans are
// batched, and the last batch is the one describing whatever went wrong just
// before the operator restarted the process.
func Setup(ctx context.Context, cfg Config, log *slog.Logger) (Shutdown, error) {
	// The propagator goes in either way. It only reads and writes headers, so
	// it costs nothing when tracing is off -- and having it installed means a
	// trace context arriving from a caller is never dropped, which matters the
	// moment somebody turns tracing on upstream but not here.
	otel.SetTextMapPropagator(Propagator())

	if !cfg.Enabled {
		// Explicitly no-op rather than left unset. An unset global provider is
		// already a no-op, but setting it says so, and it means the behavior
		// does not change if a dependency installs one behind our back.
		otel.SetTracerProvider(noop.NewTracerProvider())

		log.Debug("tracing is disabled")

		return func(context.Context) error { return nil }, nil
	}

	if cfg.Endpoint == "" {
		return nil, errors.New("observability: tracing is enabled but no endpoint is set")
	}

	options := []otlptracehttp.Option{otlptracehttp.WithEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		options = append(options, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("observability: build the OTLP exporter: %w", err)
	}

	// NewSchemaless, not NewWithAttributes.
	//
	// Merging two resources that declare different schema URLs fails outright,
	// and `resource.Default()` tracks whatever the SDK ships with -- so
	// pinning a semconv version here means the merge breaks on the next SDK
	// bump, at startup, for anyone who has tracing enabled. A schemaless
	// resource has no version to disagree about and inherits the default's.
	//
	// This was not hypothetical: the first version of this pinned semconv
	// v1.26.0 against an SDK defaulting to v1.43.0, and tracing failed to
	// start. A test caught it; nothing else would have, because the whole path
	// is skipped when tracing is off.
	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("observability: build the resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		// Batched, not synchronous: an exporter that blocks the request path
		// turns a slow collector into a slow product.
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(
			sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio)),
		),
	)

	otel.SetTracerProvider(provider)

	log.Info("tracing enabled",
		slog.String("endpoint", cfg.Endpoint),
		slog.Float64("sample_ratio", cfg.SampleRatio),
		slog.Bool("insecure", cfg.Insecure),
	)

	return provider.Shutdown, nil
}

// Tracer returns Pivot's tracer. Safe before Setup: the global provider
// defaults to a no-op.
func Tracer() trace.Tracer { return otel.Tracer(ScopeName) }

// Propagator is how trace context crosses a process boundary.
//
// Returned explicitly rather than read from OpenTelemetry's global, and that
// is the point: the global is a no-op until something sets it, so a middleware
// that reached for it would silently drop every incoming trace in any process
// where Setup had not run -- a test, a tool, or a future entry point somebody
// adds. Dropping an incoming trace is invisible: the request is still served,
// still traced, and simply belongs to the wrong story.
//
// Setup also installs this globally, for libraries that only know the global.
func Propagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

// Start opens a span on Pivot's tracer.
//
// A thin wrapper, so that call sites throughout the codebase name one package
// rather than reaching for the OpenTelemetry global themselves -- which is
// what makes it possible to answer "where do our spans come from" by grepping
// for one symbol.
func Start(
	ctx context.Context, name string, attrs ...attribute.KeyValue,
) (context.Context, trace.Span) {
	return Tracer().Start(ctx, name, trace.WithAttributes(attrs...))
}
