package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/logging"
)

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// Chain composes middleware so that the first argument is the outermost layer.
//
// Reading order matches execution order, which matters: the sequence is a
// security property, not a style choice. Recovery must wrap everything it is
// meant to catch, and the tenant check must run before any handler touches
// data.
func Chain(mw ...Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		for i := len(mw) - 1; i >= 0; i-- {
			next = mw[i](next)
		}

		return next
	}
}

// --- request ID -----------------------------------------------------------

type requestIDKey struct{}

// RequestIDHeader is the header carrying the correlation ID, both inbound and
// outbound.
const RequestIDHeader = "X-Request-Id"

// maxInboundRequestIDLen bounds a client-supplied ID. An unbounded one ends up
// in every log line for that request, which is a cheap way to flood a log sink.
const maxInboundRequestIDLen = 128

// WithRequestID assigns each request a correlation ID and echoes it back.
//
// A client-supplied ID is honored so a trace can span services, but it is
// length-bounded and stripped of anything that would break log parsing —
// unvalidated client input that lands in every log line is a log-injection
// vector, not just an aesthetic problem.
func WithRequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := sanitizeRequestID(r.Header.Get(RequestIDHeader))
			if id == "" {
				id = uuid.Must(uuid.NewV7()).String()
			}

			w.Header().Set(RequestIDHeader, id)

			ctx := context.WithValue(r.Context(), requestIDKey{}, id)
			ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With(slog.String("request_id", id)))

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequestIDFrom returns the correlation ID in ctx, or "".
func RequestIDFrom(ctx context.Context) string {
	id, ok := ctx.Value(requestIDKey{}).(string)
	if !ok {
		return ""
	}

	return id
}

// sanitizeRequestID keeps only characters safe to embed in a log line.
func sanitizeRequestID(in string) string {
	if len(in) > maxInboundRequestIDLen {
		in = in[:maxInboundRequestIDLen]
	}

	out := make([]rune, 0, len(in))

	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			out = append(out, r)
		case r == '-', r == '_', r == '.':
			out = append(out, r)
		}
	}

	return string(out)
}

// --- logging --------------------------------------------------------------

// statusRecorder captures the status and byte count for the access log.
type statusRecorder struct {
	http.ResponseWriter

	status  int
	written int64
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}

	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}

	n, err := s.ResponseWriter.Write(b)
	s.written += int64(n)

	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer, which
// streaming endpoints in Phase 1 will need for flushing.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// WithLogging emits one structured line per request.
//
// Health probes are logged at debug: a readiness probe every two seconds
// otherwise drowns the signal it is meant to protect.
func WithLogging(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(rec, r)

			if rec.status == 0 {
				rec.status = http.StatusOK
			}

			level := slog.LevelInfo
			if isProbe(r.URL.Path) {
				level = slog.LevelDebug
			}

			logging.FromContext(r.Context()).LogAttrs(r.Context(), level, "request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int64("bytes", rec.written),
				slog.String("duration", time.Since(start).String()),
				slog.String("remote", clientIP(r)),
			)
		})
	}
}

func isProbe(path string) bool {
	return path == "/healthz" || path == "/readyz"
}

// --- panic recovery -------------------------------------------------------

// WithRecovery turns a panic into a 500 with an error code.
//
// A panic in one handler must not take the process down: every other in-flight
// request would die with it. The stack is logged, never sent — it routinely
// contains internal paths and sometimes argument values.
func WithRecovery(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer handlePanic(w, r)

			next.ServeHTTP(w, r)
		})
	}
}

// handlePanic converts a recovered panic into a coded 500.
//
// Separate from the middleware closure so the request — and with it the
// context, logger and correlation ID — is an explicit parameter rather than a
// captured variable.
func handlePanic(w http.ResponseWriter, r *http.Request) {
	rec := recover()
	if rec == nil {
		return
	}

	// A client disconnecting mid-write is not a bug; re-panic so net/http can
	// handle it the way it expects to.
	if rec == http.ErrAbortHandler { //nolint:errorlint // a sentinel panic value, not an error chain
		panic(rec)
	}

	logging.FromContext(r.Context()).Error("handler panicked",
		slog.Any("panic", rec),
		slog.String("path", r.URL.Path),
		slog.String("stack", stackTrace()),
	)

	WriteError(w, r, &APIError{
		Code:    CodeInternal,
		Message: CodeInternal.Summary(),
	})
}

// --- CORS -----------------------------------------------------------------

// CORSConfig controls cross-origin access.
type CORSConfig struct {
	// AllowedOrigins is an explicit list. "*" is honored only when credentials
	// are disabled, because a wildcard with credentials is what turns any
	// website into an authenticated client of this API.
	AllowedOrigins []string

	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           time.Duration
}

// DefaultCORS is deliberately closed: no origins. Same-origin requests do not
// consult CORS at all, so the single-binary deployment works untouched, and
// opening it up is a decision someone has to make explicitly.
func DefaultCORS() CORSConfig {
	return CORSConfig{
		AllowedOrigins:   nil,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Content-Type", "Authorization", RequestIDHeader},
		ExposedHeaders:   []string{RequestIDHeader, TraceResponseHeader},
		AllowCredentials: true,
		MaxAge:           10 * time.Minute,
	}
}

// WithCORS applies cross-origin rules.
func WithCORS(cfg CORSConfig) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if origin != "" && cfg.allows(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				// The response varies by Origin, so a shared cache must not
				// serve one origin's response to another.
				w.Header().Add("Vary", "Origin")

				if cfg.AllowCredentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}

				if len(cfg.ExposedHeaders) > 0 {
					w.Header().Set("Access-Control-Expose-Headers", strings.Join(cfg.ExposedHeaders, ", "))
				}

				if r.Method == http.MethodOptions {
					w.Header().Set("Access-Control-Allow-Methods", strings.Join(cfg.AllowedMethods, ", "))
					w.Header().Set("Access-Control-Allow-Headers", strings.Join(cfg.AllowedHeaders, ", "))
					w.Header().Set("Access-Control-Max-Age", durationSeconds(cfg.MaxAge))
					w.WriteHeader(http.StatusNoContent)

					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (c CORSConfig) allows(origin string) bool {
	for _, allowed := range c.AllowedOrigins {
		if allowed == origin {
			return true
		}

		// A wildcard with credentials would let any site make authenticated
		// requests, so it is refused rather than silently downgraded.
		if allowed == "*" && !c.AllowCredentials {
			return true
		}
	}

	return false
}

func durationSeconds(d time.Duration) string {
	secs := int64(d.Seconds())
	if secs < 0 {
		secs = 0
	}

	return itoa64(secs)
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}

	var buf [20]byte

	i := len(buf)

	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}

	return string(buf[i:])
}

// --- security headers -----------------------------------------------------

// WithSecurityHeaders sets the headers every response should carry.
//
// The CSP is strict because the API serves JSON, not documents: nothing should
// ever be loaded or framed from an API response. Part 9 gives the SPA its own,
// looser policy on the routes that actually serve HTML.
func WithSecurityHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")

			// HSTS only over TLS. Sending it on plaintext is meaningless, and
			// on a local HTTP deployment it would pin a browser to https for a
			// host that does not serve it.
			if r.TLS != nil {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			next.ServeHTTP(w, r)
		})
	}
}

// --- request size ---------------------------------------------------------

// WithMaxBodySize caps request bodies before a handler reads them, so a large
// upload cannot exhaust memory by being decoded.
func WithMaxBodySize(limit int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)

			next.ServeHTTP(w, r)
		})
	}
}

// --- helpers --------------------------------------------------------------

// clientIP returns the caller's address.
//
// X-Forwarded-For is honored only from a configured proxy in Part 9's
// deployment work; until then the socket address is used, because trusting the
// header unconditionally lets any client forge its own identity for rate
// limiting.
func clientIP(r *http.Request) string {
	host, _, found := strings.Cut(r.RemoteAddr, ":")
	if !found {
		return r.RemoteAddr
	}

	// IPv6 addresses contain colons; Cut takes the first, so fall back.
	if strings.Count(r.RemoteAddr, ":") > 1 {
		if idx := strings.LastIndex(r.RemoteAddr, ":"); idx > 0 {
			return strings.Trim(r.RemoteAddr[:idx], "[]")
		}
	}

	return host
}
