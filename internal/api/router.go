package api

import (
	"log/slog"
	"net/http"
	"sync/atomic"
)

// APIPrefix is the versioned API root. The version is in the path because a
// breaking change gets a new prefix and the old one keeps working for its
// deprecation window — a header-based scheme makes that much harder to route.
const APIPrefix = "/api/v1"

// RouterConfig assembles the HTTP surface.
type RouterConfig struct {
	Log *slog.Logger

	// CORS is closed by default; opening it is deliberate.
	CORS CORSConfig

	// TenantResolver attributes a request to an organization. Nil leaves the
	// API unscoped, which is only valid before Part 6 wires up sessions.
	TenantResolver TenantResolver

	// Checks are readiness probes.
	Checks []Check

	// MaxBodyBytes caps request bodies. Zero uses DefaultMaxBodyBytes.
	MaxBodyBytes int64

	// Limits, per endpoint class. Zero values use the defaults.
	DefaultLimit RateLimit
	AuthLimit    RateLimit
}

// Router builds the handler tree.
type Router struct {
	cfg    RouterConfig
	mux    *http.ServeMux
	checks []Check

	defaultLimiter *Limiter
	authLimiter    *Limiter

	// readyFlag is owned by the Server, which flips it during shutdown. The
	// router only reads it, so readiness and the drain stay in one place.
	readyFlag *atomic.Bool
}

// setReady binds the router to the server's readiness flag.
func (r *Router) setReady(flag *atomic.Bool) { r.readyFlag = flag }

// ready reports whether this instance should receive traffic. A router built
// without a server (in tests) is ready as soon as it exists.
func (r *Router) ready() bool {
	if r.readyFlag == nil {
		return true
	}

	return r.readyFlag.Load()
}

// NewRouter builds the HTTP surface.
//
// The middleware order below is a security property, not a style choice:
//
//	requestID      so every log line and error carries a correlation ID
//	logging        so a panic below is still recorded as a request
//	recovery       so everything after it can fail without killing the process
//	security       so headers are set even on an error response
//	CORS           so a preflight is answered before any work happens
//	body limit     so an oversized body is refused before it is read
//	rate limit     so throttling costs as little as possible
//	tenant         so no handler ever runs without a scope
//
// Recovery sits inside logging so that a panicking request still produces an
// access log line; the reverse order loses the record of what crashed.
func NewRouter(cfg RouterConfig) *Router {
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = DefaultMaxBodyBytes
	}

	if cfg.DefaultLimit.Rate <= 0 {
		cfg.DefaultLimit = LimitDefault
	}

	if cfg.AuthLimit.Rate <= 0 {
		cfg.AuthLimit = LimitAuth
	}

	r := &Router{
		cfg:            cfg,
		mux:            http.NewServeMux(),
		checks:         cfg.Checks,
		defaultLimiter: NewLimiter(cfg.DefaultLimit),
		authLimiter:    NewLimiter(cfg.AuthLimit),
	}

	r.routes()

	return r
}

// Handler returns the fully wrapped handler.
func (r *Router) Handler() http.Handler {
	base := Chain(
		WithRequestID(),
		WithLogging(r.cfg.Log),
		WithRecovery(r.cfg.Log),
		WithSecurityHeaders(),
		WithCORS(r.cfg.CORS),
		WithMaxBodySize(r.cfg.MaxBodyBytes),
	)

	return base(r.mux)
}

// routes registers the HTTP surface.
func (r *Router) routes() {
	// Probes sit outside the API prefix and outside rate limiting: throttling
	// a readiness probe would make an orchestrator declare a healthy instance
	// dead precisely when it is busiest.
	r.mux.HandleFunc("GET /healthz", r.handleLive)
	r.mux.HandleFunc("GET /readyz", r.handleReady)

	// Everything under the API prefix is rate limited and tenant scoped.
	api := Chain(
		WithRateLimit(r.defaultLimiter, KeyByIP),
		r.withTenant(),
	)

	// A catch-all so an unknown API path produces the standard error envelope
	// rather than net/http's plain-text 404, which a client cannot parse.
	r.mux.Handle(APIPrefix+"/", api(http.HandlerFunc(r.handleAPINotFound)))

	// Anything outside the API prefix that is not a probe. Part 9 replaces
	// this with the SPA fallback.
	r.mux.HandleFunc("/", r.handleNotFound)
}

// withTenant applies tenant scoping when a resolver is configured.
func (r *Router) withTenant() Middleware {
	if r.cfg.TenantResolver == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	return WithTenant(r.cfg.TenantResolver, r.cfg.Log)
}

// AuthLimiter returns the limiter for authentication endpoints, which Part 6
// applies to login.
func (r *Router) AuthLimiter() *Limiter { return r.authLimiter }

// Mux exposes the underlying mux so later parts can register handlers without
// this package having to know about them.
func (r *Router) Mux() *http.ServeMux { return r.mux }

func (r *Router) handleAPINotFound(w http.ResponseWriter, req *http.Request) {
	WriteError(w, req, &APIError{
		Code:    CodeNotFound,
		Message: "No such endpoint: " + req.Method + " " + req.URL.Path,
	})
}

func (r *Router) handleNotFound(w http.ResponseWriter, req *http.Request) {
	WriteError(w, req, &APIError{
		Code:    CodeNotFound,
		Message: CodeNotFound.Summary(),
	})
}
