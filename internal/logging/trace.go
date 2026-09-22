package logging

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// traceHandler stamps the active trace onto every record.
//
// This is the join between two systems that are otherwise separate: a trace
// shows the shape of a request and a log line shows what it said, and without
// a shared identifier nobody can get from one to the other. With it, "this
// request was slow" and "this request logged a warning" become the same
// investigation.
//
// It wraps rather than replaces the handler, so the format stays whatever the
// operator configured.
type traceHandler struct{ slog.Handler }

// WithTrace returns a logger whose records carry trace_id and span_id.
//
// When there is no span in the context -- because tracing is off, or the work
// is a background sweep rather than a request -- nothing is added. An empty
// trace_id on every line would be noise in the common case, and it would make
// a log search for a real one harder rather than easier.
func WithTrace(log *slog.Logger) *slog.Logger {
	return slog.New(&traceHandler{Handler: log.Handler()})
}

func (h *traceHandler) Handle(ctx context.Context, record slog.Record) error {
	span := trace.SpanContextFromContext(ctx)

	if span.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", span.TraceID().String()),
			slog.String("span_id", span.SpanID().String()),
		)
	}

	return h.Handler.Handle(ctx, record)
}

// WithAttrs and WithGroup have to be forwarded explicitly, or the embedded
// handler returns itself unwrapped and every derived logger silently loses
// trace correlation. That is a quiet failure: the logs still work, they just
// stop being connected to anything.
func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{Handler: h.Handler.WithGroup(name)}
}
