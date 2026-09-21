package api

import (
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/Mmd4LIFE/pivot/internal/authz"
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

	// TenantResolver attributes a request to an organization. When Auth is
	// configured this defaults to [SessionTenantResolver]; nil with no Auth
	// leaves the API unscoped, which is valid only in tests.
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

	// Everything under the API prefix is rate limited, session-aware and
	// tenant scoped.
	authed := Chain(
		WithRateLimit(r.defaultLimiter, KeyByIP),
		r.withSession(),
		r.withTenant(),
	)

	r.authRoutes(authed)
	r.roleRoutes(authed)

	// A catch-all so an unknown API path produces the standard error envelope
	// rather than net/http's plain-text 404, which a client cannot parse.
	r.mux.Handle(APIPrefix+"/", authed(http.HandlerFunc(r.handleAPINotFound)))

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
