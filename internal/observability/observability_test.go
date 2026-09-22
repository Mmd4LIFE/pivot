package observability_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/Mmd4LIFE/pivot/internal/observability"
)

/*
Note on parallelism.

The tests that call Setup are deliberately *not* parallel. Setup installs the
global tracer provider, which is process-wide state, so two of them running at
once means one test's provider decides whether the other test's span is
sampled. That produced exactly the flake it sounds like: "a span was not
sampled at a ratio of 1", intermittently, because the no-op provider from the
disabled-tracing test had won the race.

The tests that only read -- the propagator, the tracer accessor -- are parallel,
because they install nothing.
*/

func discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// The default, and the one that matters most: a single-binary install on
// somebody's laptop must not try to reach a collector that is not there.
func TestDisabledTracingIsANoOp(t *testing.T) {
	shutdown, err := observability.Setup(context.Background(),
		observability.Config{Enabled: false}, discard())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	if shutdown == nil {
		t.Fatal("shutdown is nil; a caller cannot defer it")
	}

	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown: %v", err)
	}

	// The instrumentation still runs, it just records nothing. That is what
	// lets every call site be unconditional instead of guarded.
	_, span := observability.Start(context.Background(), "anything")
	defer span.End()

	if span.SpanContext().IsSampled() {
		t.Error("a span was sampled with tracing disabled")
	}
}

func TestEnabledTracingNeedsAnEndpoint(t *testing.T) {
	_, err := observability.Setup(context.Background(),
		observability.Config{Enabled: true, Endpoint: ""}, discard())
	if err == nil {
		t.Fatal("tracing was enabled with no endpoint to send to")
	}

	if !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("error = %q, want it to name the missing endpoint", err)
	}
}

func TestEnabledTracingStartsAndFlushes(t *testing.T) {
	// A collector that accepts anything. The exporter does not connect until
	// it exports, so this is about the setup and shutdown path rather than the
	// wire format.
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()

	shutdown, err := observability.Setup(context.Background(), observability.Config{
		Enabled:        true,
		Endpoint:       strings.TrimPrefix(collector.URL, "http://"),
		Insecure:       true,
		SampleRatio:    1,
		ServiceName:    "pivot-test",
		ServiceVersion: "0.0.0-test",
	}, discard())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	ctx, span := observability.Start(context.Background(), "unit-test-span")
	span.End()

	if !span.SpanContext().IsValid() {
		t.Error("the span has no valid context with tracing enabled")
	}

	if !span.SpanContext().IsSampled() {
		t.Error("a span was not sampled at a ratio of 1")
	}

	// The last batch is the one describing whatever went wrong just before the
	// operator restarted the process, so shutdown has to flush rather than
	// merely stop.
	flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := shutdown(flushCtx); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

func TestSamplingIsParentRespecting(t *testing.T) {
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()

	// Nothing would be sampled on its own merits.
	shutdown, err := observability.Setup(context.Background(), observability.Config{
		Enabled:     true,
		Endpoint:    strings.TrimPrefix(collector.URL, "http://"),
		Insecure:    true,
		SampleRatio: 0,
		ServiceName: "pivot-test",
	}, discard())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	defer func() { _ = shutdown(context.Background()) }()

	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")

	parent := trace.ContextWithSpanContext(context.Background(),
		trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     spanID,
			TraceFlags: trace.FlagsSampled,
			Remote:     true,
		}))

	_, span := observability.Start(parent, "child")
	defer span.End()

	// A request that arrived already sampled stays sampled even at ratio 0.
	// Otherwise a trace is half-recorded, which is worse than not recorded:
	// the gap looks like the work never happened.
	if !span.SpanContext().IsSampled() {
		t.Error("a sampled parent produced an unsampled child")
	}
}

func TestThePropagatorWorksWithoutSetup(t *testing.T) {
	t.Parallel()

	// Deliberately no Setup call. This is the fragility a test found: reading
	// OpenTelemetry's global propagator means any process that has not called
	// Setup silently drops every incoming trace -- the request is still
	// served, still traced, and belongs to the wrong story.
	p := observability.Propagator()

	const (
		incoming    = "4bf92f3577b34da6a3ce929d0e0e4736"
		traceparent = "00-" + incoming + "-00f067aa0ba902b7-01"
	)

	header := http.Header{}
	header.Set("traceparent", traceparent)

	ctx := p.Extract(context.Background(), propagation.HeaderCarrier(header))

	got := trace.SpanContextFromContext(ctx)
	if !got.IsValid() {
		t.Fatal("no span context was extracted")
	}

	if got.TraceID().String() != incoming {
		t.Errorf("trace id = %s, want %s", got.TraceID(), incoming)
	}
}

func TestThePropagatorRoundTrips(t *testing.T) {
	t.Parallel()

	p := observability.Propagator()

	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")

	ctx := trace.ContextWithSpanContext(context.Background(),
		trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     spanID,
			TraceFlags: trace.FlagsSampled,
		}))

	header := http.Header{}
	p.Inject(ctx, propagation.HeaderCarrier(header))

	if header.Get("traceparent") == "" {
		t.Fatal("nothing was injected; an outbound call would start a new trace")
	}

	back := trace.SpanContextFromContext(
		p.Extract(context.Background(), propagation.HeaderCarrier(header)))

	if back.TraceID() != traceID {
		t.Errorf("trace id did not survive the round trip: %s", back.TraceID())
	}
}

func TestTracerIsUsableBeforeSetup(t *testing.T) {
	// Packages call observability.Start at import-time-ish points and in tests
	// that never configure anything. It has to be safe.
	if observability.Tracer() == nil {
		t.Fatal("Tracer returned nil")
	}

	_, span := observability.Start(context.Background(), "before-setup")
	span.End()
}
