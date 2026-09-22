package api

import (
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/Mmd4LIFE/pivot/internal/observability"
)

// WithMetrics records the three numbers an operator watches.
//
// Rate, duration and error rate all come from two instruments, and errors are
// a *filter* on the status label rather than a counter of their own. That is
// what keeps "requests" and "errors" consistent by construction: they cannot
// drift, because there is only one count.
func WithMetrics(m *observability.Metrics, mux *http.ServeMux) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()

			// The route pattern, not the path. `/api/v1/users/{id}` is one
			// series; `/api/v1/users/01a0...` is one series per user, which is
			// how a metrics backend is destroyed by a product that works.
			route := routePattern(mux, r)

			attrs := metric.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.route", route),
			)

			m.InFlight.Add(r.Context(), 1, attrs)
			defer m.InFlight.Add(r.Context(), -1, attrs)

			recorder := &metricsWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(recorder, r)

			done := metric.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.route", route),
				observability.StatusClass(recorder.status),
			)

			m.Requests.Add(r.Context(), 1, done)
			m.Duration.Record(r.Context(), time.Since(started).Seconds(), done)
		})
	}
}

// routePattern returns the pattern that will match, or a placeholder.
//
// Asked of the mux rather than read from `r.Pattern`, because this middleware
// runs *before* the mux dispatches and `r.Pattern` is only populated after it
// matches -- so reading it here returns empty for every request, and every
// series was labeled "unmatched". A test caught that; nothing else would
// have, since the metric was still being recorded and still looked plausible.
//
// `mux.Handler` performs the same matching the mux is about to perform and
// returns the pattern without serving.
//
// When nothing matches -- a 404 -- the literal path must not be used: somebody
// scanning for admin panels would otherwise create a series per guess and turn
// a probe into an outage of whatever is scraping this.
func routePattern(mux *http.ServeMux, r *http.Request) string {
	if mux == nil {
		return "unmatched"
	}

	_, pattern := mux.Handler(r)
	if pattern == "" {
		return "unmatched"
	}

	// The pattern carries its method -- "GET /healthz" -- which is already a
	// label of its own. Two labels saying the same thing double the series for
	// no information.
	if _, path, found := strings.Cut(pattern, " "); found {
		return path
	}

	return pattern
}

// metricsWriter records the status code.
type metricsWriter struct {
	http.ResponseWriter

	status int
}

func (w *metricsWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *metricsWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
