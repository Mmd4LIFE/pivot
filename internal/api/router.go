package api

import (
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/observability"
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

	// Auth serves the authentication endpoints and backs the session-derived
	// tenant scope. Nil registers no authentication surface, which is what
	// tests that only exercise middleware want.
	Auth *AuthHandler

	// Roles serves the role catalog and role assignment. Nil registers no
	// authorization surface and leaves every endpoint ungated, which is valid
	// only in tests.
	Roles *RoleHandler

	// Checker answers permission questions for [RequirePermission]. It is set
	// from Roles when Roles is present.
	Checker authz.Checker

	// OIDC serves the single sign-on flow and provider administration. Nil
	// registers no SSO surface.
	OIDC *OIDCHandler

	// Setup serves the first run: whether this Pivot has been claimed, and the
	// endpoint that claims it. Nil registers neither, which is what an
	// instance provisioned entirely from the CLI wants.
	Setup *SetupHandler

	// Telemetry receives error reports from the browser. Nil registers no
	// reporting endpoint, so the frontend's reports get a coded 404 and it
	// stops trying -- which is what an API-only deployment wants.
	Telemetry *TelemetryHandler

	// SPA serves the browser application on every path that is not an API
	// call or a health probe. Nil answers those paths with a coded 404, which
	// is what an API-only deployment and most tests want.
	SPA http.Handler

	// TenantResolver attributes a request to an organization. When Auth is
	// configured this defaults to [SessionTenantResolver]; nil with no Auth
	// leaves the API unscoped, which is valid only in tests.
	TenantResolver TenantResolver

	// Checks are readiness probes.
	Checks []Check

	// MaxBodyBytes caps request bodies. Zero uses DefaultMaxBodyBytes.
	MaxBodyBytes int64

	// Limits, per endpoint class. Zero values use the defaults.
	DefaultLimit   RateLimit
	AuthLimit      RateLimit
	TelemetryLimit RateLimit

	// Metrics is the instrument set the request middleware records into. Nil
	// leaves the middleware off entirely rather than recording into a no-op,
	// so a router built without it costs nothing at all.
	Metrics *observability.Metrics

	// MetricsHandler serves the Prometheus endpoint. Nil leaves /metrics
	// unregistered, which answers 404 -- and a 404 tells an operator their
	// scrape is pointed at a Pivot with metrics off, which is the truth. An
	// empty 200 would not.
	MetricsHandler http.Handler
}

// Router builds the handler tree.
type Router struct {
	cfg    RouterConfig
	mux    *http.ServeMux
	checks []Check

	defaultLimiter   *Limiter
	authLimiter      *Limiter
	telemetryLimiter *Limiter

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
//	session        so the tenant resolver has an identity to read
//	tenant         so no handler ever runs without a scope
//
// Recovery sits inside logging so that a panicking request still produces an
// access log line; the reverse order loses the record of what crashed.
//
// Rate limiting before session resolution is load-bearing in the same way: a
// throttled request must not cost a database round trip, and on the login
// endpoint it must not cost an Argon2 hash.
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

	if cfg.TelemetryLimit.Rate <= 0 {
		cfg.TelemetryLimit = LimitTelemetry
	}

	// Authentication implies session-derived scoping. Making it the default
	// rather than something the caller remembers to pass is what stops an
	// instance booting with login endpoints and no tenant enforcement.
	if cfg.Auth != nil && cfg.TenantResolver == nil {
		cfg.TenantResolver = SessionTenantResolver()
	}

	if cfg.Roles != nil && cfg.Checker == nil {
		cfg.Checker = cfg.Roles.checker
	}

	r := &Router{
		cfg:              cfg,
		mux:              http.NewServeMux(),
		checks:           cfg.Checks,
		defaultLimiter:   NewLimiter(cfg.DefaultLimit),
		authLimiter:      NewLimiter(cfg.AuthLimit),
		telemetryLimiter: NewLimiter(cfg.TelemetryLimit),
	}

	r.routes()

	return r
}

// Handler returns the fully wrapped handler.
func (r *Router) Handler() http.Handler {
	chain := []Middleware{
		// First, so everything below runs inside the span -- including the
		// logging middleware, whose line then carries the trace.
		WithTracing(),
	}

	// Recorded for every request that arrives, including the ones the rate
	// limiter and the session middleware reject. A metric that only counts
	// requests which got as far as a handler cannot show a service being
	// hammered, which is when somebody looks at it.
	if r.cfg.Metrics != nil {
		chain = append(chain, WithMetrics(r.cfg.Metrics, r.mux))
	}

	chain = append(chain,
		WithRequestID(),
		WithLogging(r.cfg.Log),
		WithRecovery(r.cfg.Log),
		WithSecurityHeaders(),
		WithCORS(r.cfg.CORS),
		WithMaxBodySize(r.cfg.MaxBodyBytes),
	)

	return Chain(chain...)(r.mux)
}

// routes registers the HTTP surface.
func (r *Router) routes() {
	// Probes sit outside the API prefix and outside rate limiting: throttling
	// a readiness probe would make an orchestrator declare a healthy instance
	// dead precisely when it is busiest.
	r.mux.HandleFunc("GET /healthz", r.handleLive)
	r.mux.HandleFunc("GET /readyz", r.handleReady)

	// Metrics sits beside the probes: outside the API prefix, outside rate
	// limiting, and outside authentication.
	//
	// Unauthenticated is the deliberate part. Prometheus has no good way to
	// hold a session, every scrape would otherwise cost an Argon2 verification,
	// and the endpoint exposes request counts and latencies rather than
	// anything about the data. It is the operator's job to keep :8080 off the
	// public internet, which is already true of every other route here.
	if r.cfg.MetricsHandler != nil {
		r.mux.Handle("GET "+observability.MetricsPath, r.cfg.MetricsHandler)
	}

	// Everything under the API prefix is rate limited, session-aware and
	// tenant scoped.
	authed := Chain(
		WithRateLimit(r.defaultLimiter, KeyByIP),
		r.withSession(),
		r.withTenant(),
	)

	r.setupRoutes()
	r.authRoutes(authed)
	r.roleRoutes(authed)
	r.oidcRoutes(authed)
	r.telemetryRoutes()

	// A catch-all so an unknown API path produces the standard error envelope
	// rather than net/http's plain-text 404, which a client cannot parse.
	r.mux.Handle(APIPrefix+"/", authed(http.HandlerFunc(r.handleAPINotFound)))

	// Anything outside the API prefix that is not a probe.
	//
	// The application is mounted last and as a catch-all, so it never shadows
	// the API: Go's mux prefers the more specific pattern, and every API route
	// is more specific than "/".
	if r.cfg.SPA != nil {
		r.mux.Handle("/", r.cfg.SPA)
	} else {
		r.mux.HandleFunc("/", r.handleNotFound)
	}
}

// withTenant applies tenant scoping when a resolver is configured.
func (r *Router) withTenant() Middleware {
	if r.cfg.TenantResolver == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	return WithTenant(r.cfg.TenantResolver, r.cfg.Log)
}

// withSession resolves the session cookie when authentication is configured.
func (r *Router) withSession() Middleware {
	if r.cfg.Auth == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	return WithSession(r.cfg.Auth.Service(), r.cfg.Auth.Cookie(), r.cfg.Log)
}

// authRoutes registers the authentication surface.
//
// Login and logout are registered outside the tenant chain on purpose. Login
// has no organization to be scoped to yet — resolving one is what it does —
// and logout revokes by token hash, so requiring a valid session in order to
// end one would mean an expired session could never be cleaned up.
func (r *Router) authRoutes(authed Middleware) {
	h := r.cfg.Auth
	if h == nil {
		return
	}

	// The strict limiter, keyed by address *and* path so that hammering login
	// does not consume the allowance for any other endpoint. This is the only
	// thing standing between an anonymous caller and a 64 MiB Argon2
	// allocation per request, which is why it sits outside the handler rather
	// than inside it.
	login := Chain(WithRateLimit(r.authLimiter, KeyByIPAndPath))

	r.mux.Handle("POST "+APIPrefix+"/auth/login", login(http.HandlerFunc(h.handleLogin)))

	r.mux.Handle("POST "+APIPrefix+"/auth/logout",
		Chain(WithRateLimit(r.defaultLimiter, KeyByIP))(http.HandlerFunc(h.handleLogout)))

	r.mux.Handle("GET "+APIPrefix+"/auth/me", authed(http.HandlerFunc(h.handleMe)))
	r.mux.Handle("GET "+APIPrefix+"/auth/sessions", authed(http.HandlerFunc(h.handleListSessions)))
	r.mux.Handle("DELETE "+APIPrefix+"/auth/sessions/{id}", authed(http.HandlerFunc(h.handleRevokeSession)))
}

// roleRoutes registers the authorization surface.
//
// Every gated route is `authed` followed by a permission, in that order: the
// tenant middleware establishes who and which organization, and only then is
// there a question for the checker to answer. Reversing them would ask "may
// you?" before "who are you?", which has no answer.
func (r *Router) roleRoutes(authed Middleware) {
	h := r.cfg.Roles
	if h == nil {
		return
	}

	// The catalog is readable by anyone logged in: which roles exist and
	// what they mean is documentation, not a secret. Who holds them is not.
	r.mux.Handle("GET "+APIPrefix+"/roles", authed(http.HandlerFunc(h.handleCatalog)))

	manageRoles := Chain(authed, r.require(authz.PermManageRoles))

	r.mux.Handle("GET "+APIPrefix+"/organization/role-assignments",
		manageRoles(http.HandlerFunc(h.handleList)))
	r.mux.Handle("POST "+APIPrefix+"/organization/role-assignments",
		manageRoles(http.HandlerFunc(h.handleGrant)))
	r.mux.Handle("DELETE "+APIPrefix+"/organization/role-assignments/{subjectType}/{subjectId}/{role}",
		manageRoles(http.HandlerFunc(h.handleRevoke)))

	r.mux.Handle("DELETE "+APIPrefix+"/admin/sessions/{id}",
		Chain(authed, r.require(authz.PermManageSessions))(
			http.HandlerFunc(h.handleRevokeAnySession)))
}

// oidcRoutes registers the single sign-on surface.
//
// The flow endpoints are deliberately outside the tenant chain. Starting an
// SSO login is what *produces* a session, so requiring one first would be
// circular — the same reason password login sits outside it.
//
// Both are throttled with the strict auth limiter: /start does provider
// discovery and /callback does a token exchange plus password-free account
// creation, and neither should be available to an anonymous caller at an
// unbounded rate.
func (r *Router) oidcRoutes(authed Middleware) {
	h := r.cfg.OIDC
	if h == nil {
		return
	}

	public := Chain(WithRateLimit(r.authLimiter, KeyByIPAndPath))

	r.mux.Handle("GET "+APIPrefix+"/auth/providers",
		Chain(WithRateLimit(r.defaultLimiter, KeyByIP))(http.HandlerFunc(h.handleList)))

	r.mux.Handle("GET "+APIPrefix+"/auth/oidc/{provider}/start",
		public(http.HandlerFunc(h.handleStart)))
	r.mux.Handle("GET "+APIPrefix+"/auth/oidc/{provider}/callback",
		public(http.HandlerFunc(h.handleCallback)))

	// Administration. Pointing an organization at a different directory is
	// deciding who its users are, which is the same power as granting roles by
	// another route - so it takes the same class of permission.
	manage := Chain(authed, r.require(authz.PermManageOrganization))

	r.mux.Handle("GET "+APIPrefix+"/organization/identity-providers",
		manage(http.HandlerFunc(h.handleAdminList)))
	r.mux.Handle("POST "+APIPrefix+"/organization/identity-providers",
		manage(http.HandlerFunc(h.handleAdminCreate)))
	r.mux.Handle("PUT "+APIPrefix+"/organization/identity-providers/{id}",
		manage(http.HandlerFunc(h.handleAdminUpdate)))
	r.mux.Handle("DELETE "+APIPrefix+"/organization/identity-providers/{id}",
		manage(http.HandlerFunc(h.handleAdminDelete)))
}

// setupRoutes registers the first run.
//
// Unauthenticated, and unavoidably so: there is nobody to authenticate as.
// That is the whole security problem of a first run, and it is handled in
// three places rather than one -- the endpoint refuses once an organization
// exists, the service requires the token printed in the startup banner, and
// the strict auth limiter is what stands between an anonymous caller and an
// unbounded number of Argon2 hashes.
//
// Outside the tenant chain, like login, because claiming an instance is what
// creates the organization a scope would be resolved from.
func (r *Router) setupRoutes() {
	h := r.cfg.Setup
	if h == nil {
		return
	}

	// The status endpoint gets the ordinary limiter: the browser asks it on
	// every cold load, including after setup is long finished, and it reads
	// one count.
	r.mux.Handle("GET "+APIPrefix+"/setup/status",
		Chain(WithRateLimit(r.defaultLimiter, KeyByIP))(http.HandlerFunc(h.handleStatus)))

	r.mux.Handle("POST "+APIPrefix+"/setup",
		Chain(WithRateLimit(r.authLimiter, KeyByIPAndPath))(http.HandlerFunc(h.handleInitialize)))
}

// telemetryRoutes registers the browser error endpoint.
//
// Unauthenticated, and that is the whole point: the errors worth having are
// disproportionately the ones that happen before login, on the page where
// logging in was supposed to work. Requiring a session would collect reports
// from exactly the users who are not having the problem.
//
// So it is defended by shape instead. The strict telemetry limiter is keyed by
// address and path, the body cap is 16 KiB rather than the general 1 MiB, and
// the handler writes to the log and nothing else -- there is no store to fill,
// no query to run, and no response worth probing.
//
// The session middleware runs but is not required: a report from somebody
// logged in gets their user ID attached, which is the difference between "an
// error happened" and "an error happened to this person, who can be asked what
// they were doing". It sits after the rate limiter, so a flood is refused
// before it costs a session lookup.
func (r *Router) telemetryRoutes() {
	h := r.cfg.Telemetry
	if h == nil {
		return
	}

	// No body-size middleware here: the handler applies [MaxErrorReportBytes]
	// itself, so it stays safe wherever it is mounted. Two readers with the
	// same limit would only make it ambiguous which one refused.
	report := Chain(
		WithRateLimit(r.telemetryLimiter, KeyByIPAndPath),
		r.withSession(),
	)

	r.mux.Handle("POST "+APIPrefix+"/telemetry/errors",
		report(http.HandlerFunc(h.handleErrorReport)))
}

// require builds the permission middleware for a route.
//
// With no checker configured this denies rather than passing through. An
// unconfigured authorization backend is a misconfiguration, and the safe
// reading of one is that nothing is permitted — [authz.Enforce] treats a nil
// checker exactly that way.
func (r *Router) require(perm authz.Permission) Middleware {
	return RequirePermission(r.cfg.Checker, perm, r.cfg.Log)
}

// AuthLimiter returns the limiter for authentication endpoints.
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
