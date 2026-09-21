package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/auth"
	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// CookieConfig describes the session cookie.
//
// The cookie is always HttpOnly and always SameSite=Lax, and neither is
// configurable. HttpOnly is what keeps a cross-site scripting bug from
// becoming a stolen session, and Lax is the CSRF defense for every
// state-changing endpoint here: a browser does not attach a Lax cookie to a
// cross-site POST or DELETE, so an attacker's page cannot act as the user.
// Making either an option would mean someone eventually turns it off.
type CookieConfig struct {
	// Name of the cookie.
	Name string

	// Path the cookie applies to. Empty means "/".
	Path string

	// Domain scopes the cookie. Empty means host-only, which is the safest
	// default and what a single-host install wants.
	Domain string

	// Secure forces the Secure attribute even on a plaintext request.
	//
	// A TLS request gets Secure regardless. This exists for the deployment
	// where a proxy terminates TLS and Pivot only ever sees plain HTTP, so it
	// cannot tell that the browser's connection was encrypted.
	Secure bool
}

// DefaultCookie returns the cookie settings used when none are configured.
func DefaultCookie() CookieConfig {
	return CookieConfig{Name: "pivot_session", Path: "/"}
}

// path returns the cookie path, defaulting to the whole site.
func (c CookieConfig) path() string {
	if c.Path == "" {
		return "/"
	}

	return c.Path
}

// secureFor reports whether the Secure attribute belongs on this response.
//
// Secure is omitted on a plaintext request unless forced, because a browser
// silently discards a Secure cookie sent over http:// — which would leave
// `pivot serve` on localhost with a login form that never logs anyone in.
func (c CookieConfig) secureFor(r *http.Request) bool {
	return c.Secure || r.TLS != nil
}

// set writes the session cookie.
//
// The cookie's lifetime is the session's absolute cap, never its idle expiry.
// The idle expiry slides, and a cookie whose Max-Age tracked it would have to
// be rewritten on every request; the server is the authority on session
// validity either way, so the cookie lifetime is only a hint that stops a dead
// cookie lingering in the browser for weeks.
func (c CookieConfig) set(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	maxAge := int(time.Until(expires).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}

	// gosec wants Secure unconditionally. It is conditional on purpose: see
	// secureFor. HttpOnly and SameSite, the two that have no such trade-off,
	// are hard-coded above any configuration.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is deliberate, see secureFor
		Name:     c.Name,
		Value:    token,
		Path:     c.path(),
		Domain:   c.Domain,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   c.secureFor(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// clear expires the session cookie.
//
// Every attribute except the value must match the cookie that was set, or the
// browser treats it as a different cookie and keeps the original.
func (c CookieConfig) clear(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is deliberate, see secureFor
		Name:     c.Name,
		Value:    "",
		Path:     c.path(),
		Domain:   c.Domain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.secureFor(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// token reads the session token from a request, or "".
func (c CookieConfig) token(r *http.Request) string {
	cookie, err := r.Cookie(c.Name)
	if err != nil {
		return ""
	}

	return cookie.Value
}

// --- identity in the request context ---------------------------------------

type identityKey struct{}

// ErrNoSession is what the session-backed tenant resolver returns for a
// request that carries no usable session.
var ErrNoSession = errors.New("api: request carries no session")

// IdentityFrom returns the authenticated session in ctx, if any.
func IdentityFrom(ctx context.Context) (*auth.Authenticated, bool) {
	a, ok := ctx.Value(identityKey{}).(*auth.Authenticated)

	return a, ok
}

// WithSession resolves a session cookie into an authenticated identity.
//
// It is deliberately permissive: a request with no session, or with one that
// has expired, passes through with no identity attached. Rejecting is
// [WithTenant]'s job, because the login endpoint has to run without a session
// and a single middleware cannot be both required and optional.
//
// A failure to *reach* the database is not the same as "not logged in" and is
// not treated as one. Answering 401 there would tell a user their session had
// expired and send them to log in again, which cannot work while the database
// is down — so it becomes a 503 and says what is actually wrong.
func WithSession(svc *auth.Service, cookie CookieConfig, log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := cookie.token(r)
			if token == "" {
				next.ServeHTTP(w, r)

				return
			}

			authed, err := svc.Authenticate(r.Context(), token)
			if err != nil {
				if !errors.Is(err, auth.ErrSessionInvalid) {
					WriteError(w, r, NewError(CodeUnavailable,
						"The session could not be verified", err))

					return
				}

				// Drop the dead cookie so the browser stops sending it, then
				// carry on unauthenticated.
				cookie.clear(w, r)
				next.ServeHTTP(w, r)

				return
			}

			ctx := context.WithValue(r.Context(), identityKey{}, authed)
			ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With(
				slog.String("user_id", authed.Session.UserID.String()),
				slog.String("session_id", authed.Session.ID.String()),
			))

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SessionTenantResolver attributes a request to the organization its session
// belongs to.
//
// This is the resolver Part 4-b left a placeholder for. The organization comes
// from the session row, never from the request — no header, no path segment,
// no body field — so a caller cannot name a tenant they have not authenticated
// against.
func SessionTenantResolver() TenantResolver {
	return ResolverFunc(func(r *http.Request) (tenant.Scope, error) {
		authed, ok := IdentityFrom(r.Context())
		if !ok {
			return tenant.Scope{}, NewError(CodeUnauthorized,
				CodeUnauthorized.Summary(), ErrNoSession)
		}

		return authed.Scope, nil
	})
}

// --- handlers --------------------------------------------------------------

// AuthHandler serves the authentication endpoints.
type AuthHandler struct {
	svc    *auth.Service
	repos  *repo.Repositories
	cookie CookieConfig
	log    *slog.Logger
}

// NewAuthHandler builds the authentication endpoints.
func NewAuthHandler(
	svc *auth.Service, repos *repo.Repositories, cookie CookieConfig, log *slog.Logger,
) *AuthHandler {
	if cookie.Name == "" {
		cookie.Name = DefaultCookie().Name
	}

	return &AuthHandler{svc: svc, repos: repos, cookie: cookie, log: log}
}

// Cookie returns the cookie settings, so the router can share them with
// [WithSession] without the caller passing them twice.
func (h *AuthHandler) Cookie() CookieConfig { return h.cookie }

// Service returns the authentication service, for the session middleware.
func (h *AuthHandler) Service() *auth.Service { return h.svc }

// --- wire types ------------------------------------------------------------
//
// These are hand-written rather than derived from the model types, and that is
// the point: a column added to `users` cannot appear in an API response by
// accident. password_hash is the reason — it lives on model.User, and any
// scheme that serialized the model directly would be one forgotten tag away
// from publishing it.

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`

	// Organization is the slug to authenticate against. It may be omitted when
	// the instance has exactly one organization, which is the self-hosted case.
	Organization string `json:"organization,omitempty"`
}

// maxEmailLength is the practical limit on an address (RFC 5321).
const maxEmailLength = 320

// maxSlugLength bounds the organization slug.
const maxSlugLength = 100

func (b *loginRequest) Validate() []Detail {
	return Collect(
		Required("email", b.Email),
		MaxLen("email", b.Email, maxEmailLength),
		Required("password", b.Password),
		// Bounded before hashing: Argon2 will happily hash a megabyte, which is
		// a cheap way to make an unauthenticated endpoint expensive.
		MaxLen("password", b.Password, auth.MaxPasswordLength),
		MaxLen("organization", b.Organization, maxSlugLength),
	)
}

type userResponse struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organizationId"`
	Email          string `json:"email"`
	Name           string `json:"name"`
	AvatarURL      string `json:"avatarUrl,omitempty"`
	Locale         string `json:"locale"`
	Timezone       string `json:"timezone"`
	IsActive       bool   `json:"isActive"`
}

func newUserResponse(u model.User) userResponse {
	return userResponse{
		ID:             u.ID.String(),
		OrganizationID: u.OrgID.String(),
		Email:          u.Email,
		Name:           u.Name,
		AvatarURL:      u.AvatarURL.String,
		Locale:         u.Locale,
		Timezone:       u.Timezone,
		IsActive:       u.IsActive.Bool(),
	}
}

type sessionResponse struct {
	ID                string    `json:"id"`
	IssuedAt          time.Time `json:"issuedAt"`
	ExpiresAt         time.Time `json:"expiresAt"`
	AbsoluteExpiresAt time.Time `json:"absoluteExpiresAt"`
	LastSeenAt        time.Time `json:"lastSeenAt"`
	IP                string    `json:"ip,omitempty"`
	UserAgent         string    `json:"userAgent,omitempty"`

	// Current marks the session making this request, so a "sign out everywhere
	// else" button knows which row to leave alone.
	Current bool `json:"current"`
}

func newSessionResponse(s model.Session, current bool) sessionResponse {
	return sessionResponse{
		ID:                s.ID.String(),
		IssuedAt:          s.IssuedAt.Time,
		ExpiresAt:         s.ExpiresAt.Time,
		AbsoluteExpiresAt: s.AbsoluteExpiresAt.Time,
		LastSeenAt:        s.LastSeenAt.Time,
		IP:                s.IP,
		UserAgent:         s.UserAgent,
		Current:           current,
	}
}

// sessionEnvelope is the body of both login and /auth/me.
//
// Note what is absent: the session token. It is set as an HttpOnly cookie and
// returned nowhere else — putting it in the body would hand it to any script
// on the page, which is exactly what HttpOnly exists to prevent.
type sessionEnvelope struct {
	User    userResponse    `json:"user"`
	Session sessionResponse `json:"session"`
}

type sessionListResponse struct {
	Sessions []sessionResponse `json:"sessions"`
}

// --- POST /auth/login ------------------------------------------------------

// handleLogin authenticates a user and opens a session.
//
// This endpoint runs outside the tenant chain: a caller has no organization
// until they have authenticated. It is the one place the organization comes
// from the request, and the password check is what validates the claim.
func (h *AuthHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body loginRequest
	if err := Decode(w, r, &body); err != nil {
		WriteError(w, r, err)

		return
	}

	ctx := r.Context()

	orgID, err := h.resolveOrganization(ctx, body.Organization, body.Password)
	if err != nil {
		WriteError(w, r, err)

		return
	}

	result, err := h.svc.Login(ctx, auth.Credentials{
		OrgID:     orgID,
		Email:     body.Email,
		Password:  body.Password,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		WriteError(w, r, loginError(err))

		return
	}

	h.cookie.set(w, r, result.Token, result.Session.AbsoluteExpiresAt.Time)

	WriteJSON(ctx, w, http.StatusOK, sessionEnvelope{
		User:    newUserResponse(result.User),
		Session: newSessionResponse(result.Session, true),
	})
}

// loginError maps an authentication failure onto the error contract.
func loginError(err error) error {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		// One answer for unknown account, wrong password and disabled account
		// alike. internal/auth already makes them cost the same; giving them
		// the same code is the other half of that.
		return NewError(CodeUnauthorized, "Incorrect email or password", err)

	case errors.Is(err, auth.ErrAccountLocked):
		return NewError(CodeAccountLocked, CodeAccountLocked.Summary(), err)

	default:
		return err
	}
}

// resolveOrganization decides which tenant a login is against.
//
// With a slug, that one. Without a slug and with exactly one organization,
// that one — the self-hosted case, where asking would be noise. Without a slug
// and with several, the caller must say which: defaulting to "the first one"
// would silently authenticate people against a tenant they did not name.
func (h *AuthHandler) resolveOrganization(
	ctx context.Context, slug, password string,
) (uuid.UUID, error) {
	sys := h.repos.System()

	if slug != "" {
		org, err := sys.GetOrganizationBySlug(ctx, slug)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				// Same answer and same cost as a wrong password. A cheap 404
				// here would let an attacker map an instance's tenants without
				// a single valid credential.
				auth.SpendVerifyTime(password)

				return uuid.Nil, NewError(CodeUnauthorized, "Incorrect email or password", err)
			}

			return uuid.Nil, err
		}

		return org.ID, nil
	}

	// Two is all it takes to know whether "exactly one" holds.
	orgs, err := sys.ListOrganizations(ctx, 2, 0)
	if err != nil {
		return uuid.Nil, err
	}

	switch len(orgs) {
	case 0:
		auth.SpendVerifyTime(password)

		return uuid.Nil, NewError(CodeUnauthorized, "Incorrect email or password", nil)

	case 1:
		return orgs[0].ID, nil

	default:
		return uuid.Nil, ValidationError(Detail{
			Field:   "organization",
			Message: "is required: this instance hosts more than one organization",
		})
	}
}

// --- POST /auth/logout -----------------------------------------------------

// handleLogout revokes the current session.
//
// It needs no tenant scope, because a session is revoked by its token hash,
// and it succeeds whether or not the session was valid: the caller asked to be
// logged out, and they are. Reporting "you were not logged in" would only give
// a client an error to handle on the one path where nothing can be wrong.
func (h *AuthHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := h.cookie.token(r)

	if token != "" {
		if err := h.svc.Logout(r.Context(), token); err != nil {
			WriteError(w, r, err)

			return
		}
	}

	// Cleared even when there was no token, so a client that somehow holds a
	// malformed cookie is rid of it.
	h.cookie.clear(w, r)

	w.WriteHeader(http.StatusNoContent)
}

// --- GET /auth/me ----------------------------------------------------------

// handleMe returns the authenticated user.
func (h *AuthHandler) handleMe(w http.ResponseWriter, r *http.Request) {
	authed, ok := IdentityFrom(r.Context())
	if !ok {
		// Unreachable behind the tenant middleware, which rejects a request
		// with no session before any handler runs. Checked anyway: "the
		// middleware guarantees it" is how a handler ends up nil-dereferencing
		// after somebody reorders a chain.
		WriteError(w, r, NewError(CodeUnauthorized, CodeUnauthorized.Summary(), ErrNoSession))

		return
	}

	user, err := h.repos.Users.Get(r.Context(), authed.Session.UserID)
	if err != nil {
		WriteError(w, r, err)

		return
	}

	WriteJSON(r.Context(), w, http.StatusOK, sessionEnvelope{
		User:    newUserResponse(user),
		Session: newSessionResponse(authed.Session, true),
	})
}

// --- GET /auth/sessions ----------------------------------------------------

// handleListSessions returns the caller's own active sessions.
func (h *AuthHandler) handleListSessions(w http.ResponseWriter, r *http.Request) {
	authed, ok := IdentityFrom(r.Context())
	if !ok {
		WriteError(w, r, NewError(CodeUnauthorized, CodeUnauthorized.Summary(), ErrNoSession))

		return
	}

	sessions, err := h.repos.Sessions.List(r.Context(), authed.Session.UserID)
	if err != nil {
		WriteError(w, r, err)

		return
	}

	out := make([]sessionResponse, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, newSessionResponse(s, s.ID == authed.Session.ID))
	}

	WriteJSON(r.Context(), w, http.StatusOK, sessionListResponse{Sessions: out})
}

// --- DELETE /auth/sessions/{id} --------------------------------------------

// handleRevokeSession ends one of the caller's own sessions.
//
// It uses the user-scoped revoke, so naming another member's session ID
// returns 404 rather than ending their session. The organization-scoped
// variant would have allowed that, which is the right power for an
// administrator and the wrong one here.
func (h *AuthHandler) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	authed, ok := IdentityFrom(r.Context())
	if !ok {
		WriteError(w, r, NewError(CodeUnauthorized, CodeUnauthorized.Summary(), ErrNoSession))

		return
	}

	sessionID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, r, ValidationError(Detail{Field: "id", Message: "must be a UUID"}))

		return
	}

	if rerr := h.repos.Sessions.RevokeOwn(r.Context(), authed.Session.UserID, sessionID); rerr != nil {
		WriteError(w, r, rerr)

		return
	}

	// Revoking the session you are using is a logout, so the cookie goes too —
	// otherwise the browser keeps sending a token the server has already
	// rejected, and every later request looks like a mysterious 401.
	if sessionID == authed.Session.ID {
		h.cookie.clear(w, r)
	}

	w.WriteHeader(http.StatusNoContent)
}
