package api

import (
	"net/http"
	"strconv"

	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/Mmd4LIFE/pivot/internal/observability"
)

// TraceResponseHeader carries this request's trace back to the caller.
//
// W3C Trace Context Level 2 names this header and fixes its format, which is
// the same `00-<trace>-<span>-<flags>` as traceparent. A browser that wants to
// report an error with something an operator can look up needs to be told the
// trace ID, because the alternative -- letting it generate one -- hands the
// sampling decision to the client.
const TraceResponseHeader = "traceresponse"

// WithTracing opens a span for every request and continues an incoming trace.
//
// Hand-rolled rather than `otelhttp`, and the reason is the span name. The
// contrib middleware names a span after the route pattern it is given, which
// this router does not hand it -- so every span would arrive as "HTTP GET" and
// a trace view would be a wall of identical rows. Naming it "GET /api/v1/auth/me"
// makes the trace readable, and it costs forty lines rather than a dependency.
//
// It goes first in the chain, before the request ID and the logger, so that
// everything downstream -- including the log line the logging middleware
// writes -- happens inside the span and carries its trace.
func WithTracing() Middleware {
	tracer := observability.Tracer()
	propagator := observability.Propagator()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A trace that started in a caller continues here rather than
			// beginning again. Without this, a request arriving from another
			// traced service produces two unconnected traces, which is the
			// failure distributed tracing exists to prevent.
			ctx := propagator.Extract(
				r.Context(), propagation.HeaderCarrier(r.Header))

			ctx, span := tracer.Start(ctx,
				r.Method+" "+r.URL.Path,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(r.Method),
					semconv.URLPath(r.URL.Path),
					semconv.UserAgentOriginal(r.UserAgent()),
				),
			)
			defer span.End()

			// Tell the caller which trace this was, before anything can write
			// a status. This is how the browser learns a real trace ID to put
			// on an error report: it cannot invent one without also inventing
			// the sampling decision, and a trace ID that names nothing in the
			// trace store is worse than none.
			if sc := span.SpanContext(); sc.IsValid() {
				w.Header().Set(TraceResponseHeader, "00-"+
					sc.TraceID().String()+"-"+sc.SpanID().String()+"-"+sc.TraceFlags().String())
			}

			recorder := &tracedWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(recorder, r.WithContext(ctx))

			span.SetAttributes(semconv.HTTPResponseStatusCode(recorder.status))

			// 5xx is this instance's fault and marks the span an error; 4xx is
			// the caller's and does not. Marking 404s as errors makes an error
			// rate that alerts on somebody else's typo.
			if recorder.status >= http.StatusInternalServerError {
				span.SetStatus(otelcodes.Error, strconv.Itoa(recorder.status))
			}
		})
	}
}

// tracedWriter records the status code for the span.
type tracedWriter struct {
	http.ResponseWriter

	status int
}

func (w *tracedWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Flush forwards to the underlying writer when it supports flushing.
//
// Without this a streaming response buffers until the handler returns, which
// is how adding a middleware silently breaks server-sent events. Phase 4 wants
// them.
func (w *tracedWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
