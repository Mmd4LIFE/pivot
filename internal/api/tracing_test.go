package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/store"
)

/*
The deliverable, asserted: a request can be traced end to end.

An in-memory exporter rather than a collector, so the whole chain is checked in
a unit test with nothing to install. What it proves is the part that actually
breaks -- that the spans share one trace and nest in the right order. A
collector would only confirm the wire format, which is OpenTelemetry's problem
and not ours.
*/

// recordSpans installs a real tracer provider that records into memory, and
// restores whatever was there afterwards.
func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()

	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(recorder),
		// Every span, so the assertion is about structure rather than luck.
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	))

	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	return recorder
}

// spanNames returns the recorded names, for readable failures.
func spanNames(recorder *tracetest.SpanRecorder) []string {
	out := make([]string, 0, len(recorder.Ended()))

	for _, s := range recorder.Ended() {
		out = append(out, s.Name())
	}

	return out
}

func TestOneRequestProducesOneTraceAcrossEveryLayer(t *testing.T) {
	// Not parallel: it swaps the global tracer provider.
	bothEngines(t, func(t *testing.T, db *store.DB) {
		recorder := recordSpans(t)

		f := asAdmin(t, db)

		// A request that must pass through every layer: the HTTP middleware,
		// the permission check, and the database behind it.
		resp := f.request(t, http.MethodGet, api.APIPrefix+"/organization/role-assignments", nil)
		if resp.status != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.status, resp)
		}

		ended := recorder.Ended()
		if len(ended) == 0 {
			t.Fatal("no spans were recorded")
		}

		names := spanNames(recorder)

		// HTTP.
		var httpSpan sdktrace.ReadOnlySpan

		for _, s := range ended {
			if strings.HasPrefix(s.Name(), "GET /api/v1/organization/role-assignments") {
				httpSpan = s
			}
		}

		if httpSpan == nil {
			t.Fatalf("no HTTP span; recorded: %v", names)
		}

		// authz.
		var authzSpan sdktrace.ReadOnlySpan

		for _, s := range ended {
			if s.Name() == "authz.Check" {
				authzSpan = s
			}
		}

		if authzSpan == nil {
			t.Fatalf("no authorization span; recorded: %v", names)
		}

		// The database, from the generated decorator.
		var dbSpan sdktrace.ReadOnlySpan

		for _, s := range ended {
			if strings.HasPrefix(s.Name(), "db.") {
				dbSpan = s
			}
		}

		if dbSpan == nil {
			t.Fatalf("no database span; recorded: %v", names)
		}

		// One trace. This is the assertion that matters: three spans that do
		// not share a trace ID are three unrelated facts, and the whole point
		// of tracing is that they are one story.
		traceID := httpSpan.SpanContext().TraceID()

		for _, s := range []sdktrace.ReadOnlySpan{authzSpan, dbSpan} {
			if s.SpanContext().TraceID() != traceID {
				t.Errorf("%s is in trace %s, want %s",
					s.Name(), s.SpanContext().TraceID(), traceID)
			}
		}

		// And they nest rather than sitting side by side: the authorization
		// check happened *inside* the request.
		if !authzSpan.Parent().IsValid() {
			t.Error("the authorization span has no parent")
		}
	})
}

func TestTheAuthorizationSpanRecordsItsDecision(t *testing.T) {
	bothEngines(t, func(t *testing.T, db *store.DB) {
		recorder := recordSpans(t)

		f := asAdmin(t, db)

		if resp := f.request(t, http.MethodGet,
			api.APIPrefix+"/organization/role-assignments", nil); resp.status != http.StatusOK {
			t.Fatalf("status = %d: %s", resp.status, resp)
		}

		found := false

		for _, s := range recorder.Ended() {
			if s.Name() != "authz.Check" {
				continue
			}

			for _, attr := range s.Attributes() {
				if string(attr.Key) == "authz.allowed" {
					found = true

					// "Why was this a 403" is the question a trace is opened to
					// answer. A span that records only that a check happened
					// does not answer it.
					if !attr.Value.AsBool() {
						t.Error("the decision was recorded as denied on a request that succeeded")
					}
				}
			}
		}

		if !found {
			t.Error("the authorization span does not record its decision")
		}
	})
}

func TestAnIncomingTraceIsContinuedRatherThanRestarted(t *testing.T) {
	bothEngines(t, func(t *testing.T, db *store.DB) {
		recorder := recordSpans(t)

		f := asAdmin(t, db)

		// A caller that is already tracing. Starting a new trace here would
		// produce two disconnected halves of one request, which is the exact
		// failure distributed tracing exists to prevent.
		const (
			incomingTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
			traceparent   = "00-" + incomingTrace + "-00f067aa0ba902b7-01"
		)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
			f.server.URL+api.APIPrefix+"/auth/me", http.NoBody)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}

		req.Header.Set("traceparent", traceparent)

		if resp := send(t, f.client, req); resp.status != http.StatusOK {
			t.Fatalf("status = %d: %s", resp.status, resp)
		}

		for _, s := range recorder.Ended() {
			if strings.HasPrefix(s.Name(), "GET ") {
				if got := s.SpanContext().TraceID().String(); got != incomingTrace {
					t.Errorf("trace id = %s, want the incoming %s", got, incomingTrace)
				}

				return
			}
		}

		t.Fatalf("no HTTP span recorded: %v", spanNames(recorder))
	})
}
