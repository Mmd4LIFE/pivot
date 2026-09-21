package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/auth"
	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/oidc"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

// OIDCHandler serves the single sign-on flow.
type OIDCHandler struct {
	repos       *repo.Repositories
	registry    *oidc.Registry
	provisioner *oidc.Provisioner
	auth        *auth.Service
	cookie      CookieConfig
	log         *slog.Logger

	// baseURL is the externally reachable root, used to build the redirect URI
	// registered with the identity provider.
	baseURL string
}

// NewOIDCHandler builds the SSO endpoints.
func NewOIDCHandler(
	repos *repo.Repositories, registry *oidc.Registry, authSvc *auth.Service,
	cookie CookieConfig, baseURL string, log *slog.Logger,
) *OIDCHandler {
	return &OIDCHandler{
		repos:       repos,
		registry:    registry,
		provisioner: oidc.NewProvisioner(repos, log),
		auth:        authSvc,
		cookie:      cookie,
		baseURL:     strings.TrimRight(baseURL, "/"),
		log:         log,
	}
}

// --- the flow cookie -------------------------------------------------------

// flowCookieName holds the state of a login in progress.
const flowCookieName = "pivot_oidc_flow"

// flowCookiePath scopes the cookie to the SSO endpoints, so it is not attached
// to every other request for the ten minutes it lives.
const flowCookiePath = APIPrefix + "/auth/oidc"

// flowCookieMaxAge bounds how long a half-finished login stays resumable.
//
// Ten minutes is generous for "click the button, type a password at your
// identity provider, come back" and short enough that an abandoned flow does
// not linger. The authorization code itself expires far sooner at the
// provider, so this is the outer bound rather than the operative one.
const flowCookieMaxAge = 600

// flowState is what has to survive the redirect to the identity provider.
//
// It lives in an HttpOnly cookie rather than a database table because it is
// per-browser, short-lived, and worthless to anyone else: the server's memory
// of a conversation, handed to the only party that can continue it.
//
// The security boundary is worth stating. Whoever can set cookies on this
// origin can plant a flow and complete a login as themselves in the victim's
// browser — the classic login-CSRF. That is the same boundary every
// cookie-based flow has, and the defense is that a foreign origin cannot set
// our cookies. What the state check adds is that an attacker who can only
// *send* the victim to a callback URL, which is the far easier position,
// achieves nothing.
type flowState struct {
	OrgID    string `json:"o"`
	Provider string `json:"p"`
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`

	// Return is where to send the browser once the session exists. Validated
	// on the way in, not on the way out — see safeReturnPath.
	Return string `json:"r,omitempty"`
}

// encode renders the flow for a cookie value.
func (f flowState) encode() (string, error) {
	data, err := json.Marshal(f)
	if err != nil {
		return "", fmt.Errorf("api: encode oidc flow: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(data), nil
}

// decodeFlow reads a flow cookie.
func decodeFlow(raw string) (flowState, error) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return flowState{}, fmt.Errorf("api: malformed oidc flow cookie: %w", err)
	}

	var f flowState
	if err := json.Unmarshal(data, &f); err != nil {
		return flowState{}, fmt.Errorf("api: malformed oidc flow cookie: %w", err)
	}

	return f, nil
}

// setFlowCookie stores the in-progress login.
//
// SameSite=Lax is required here, and Strict would break every login: the
// callback is a top-level navigation *from the identity provider's origin*,
// and Strict withholds cookies on exactly that. Lax sends them on a top-level
// GET, which is what the callback is.
func (h *OIDCHandler) setFlowCookie(w http.ResponseWriter, r *http.Request, value string) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is deliberate, see CookieConfig.secureFor
		Name:     flowCookieName,
		Value:    value,
		Path:     flowCookiePath,
		Domain:   h.cookie.Domain,
		MaxAge:   flowCookieMaxAge,
		HttpOnly: true,
		Secure:   h.cookie.secureFor(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// clearFlowCookie removes it. Called on every callback, successful or not: a
// flow is single-use, and leaving it behind invites a replay.
func (h *OIDCHandler) clearFlowCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is deliberate, see CookieConfig.secureFor
		Name:     flowCookieName,
		Value:    "",
		Path:     flowCookiePath,
		Domain:   h.cookie.Domain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookie.secureFor(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// --- GET /auth/providers ---------------------------------------------------

type providerButton struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type providerListResponse struct {
	Providers []providerButton `json:"providers"`
}

// handleList returns the SSO buttons a login page should offer.
//
// Unauthenticated by necessity — it is consumed by the login page, before
// anyone has a session — which is why it returns slug and display name and
// nothing else. Issuer and client ID are not secret, but publishing an
// organization's identity provider to anonymous callers maps its vendor
// relationships for no benefit.
func (h *OIDCHandler) handleList(w http.ResponseWriter, r *http.Request) {
	orgID, err := h.resolveOrg(r)
	if err != nil {
		// An unresolvable organization is an empty list, not an error: a login
		// page asking "what can I offer?" gets "nothing", which is true and
		// reveals nothing about which organizations exist.
		WriteJSON(r.Context(), w, http.StatusOK, providerListResponse{
			Providers: []providerButton{},
		})

		return
	}

	providers, err := h.repos.System().ListEnabledProviders(r.Context(), orgID)
	if err != nil {
		WriteError(w, r, err)

		return
	}

	out := make([]providerButton, 0, len(providers))
	for _, p := range providers {
		out = append(out, providerButton{Slug: p.Slug, Name: p.Name})
	}

	WriteJSON(r.Context(), w, http.StatusOK, providerListResponse{Providers: out})
}

// resolveOrg decides which organization an unauthenticated SSO request is for.
//
// Same rule as password login: an explicit `organization` slug, or the only
// organization when exactly one exists. With several and none named, there is
// no answer — picking one would start a login against a tenant nobody asked
// for.
func (h *OIDCHandler) resolveOrg(r *http.Request) (uuid.UUID, error) {
	sys := h.repos.System()

	if slug := r.URL.Query().Get("organization"); slug != "" {
		org, err := sys.GetOrganizationBySlug(r.Context(), slug)
		if err != nil {
			return uuid.Nil, err
		}

		return org.ID, nil
	}

	orgs, err := sys.ListOrganizations(r.Context(), 2, 0)
	if err != nil {
		return uuid.Nil, err
	}

	if len(orgs) != 1 {
		return uuid.Nil, fmt.Errorf("%w: name an organization", repo.ErrNotFound)
	}

	return orgs[0].ID, nil
}

// --- GET /auth/oidc/{provider}/start ---------------------------------------

// handleStart begins a login and redirects to the identity provider.
func (h *OIDCHandler) handleStart(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("provider")

	orgID, err := h.resolveOrg(r)
	if err != nil {
		WriteError(w, r, NewError(CodeNotFound, "No such identity provider", err))

		return
	}

	provider, err := h.repos.System().FindIdentityProvider(r.Context(), orgID, slug)
	if err != nil {
		// A disabled or missing provider look the same from here. An anonymous
		// caller has no business distinguishing them.
		WriteError(w, r, NewError(CodeNotFound, "No such identity provider", err))

		return
	}

	if !provider.IsEnabled.Bool() {
		WriteError(w, r, NewError(CodeNotFound, "No such identity provider", nil))

		return
	}

	flow, err := oidc.NewFlow()
	if err != nil {
		WriteError(w, r, err)

		return
	}

	authURL, err := h.registry.AuthCodeURL(
		r.Context(), toProvider(provider), h.redirectURI(r, slug), flow)
	if err != nil {
		WriteError(w, r, oidcError(err))

		return
	}

	encoded, err := flowState{
		OrgID:    orgID.String(),
		Provider: slug,
		State:    flow.State,
		Nonce:    flow.Nonce,
		Verifier: flow.Verifier,
		Return:   safeReturnPath(r.URL.Query().Get("return")),
	}.encode()
	if err != nil {
		WriteError(w, r, err)

		return
	}

	h.setFlowCookie(w, r, encoded)

	// gosec traces the provider slug from the path into this redirect and
	// calls it tainted. The destination is not attacker-controlled: authURL is
	// the authorization endpoint from the provider's own discovery document,
	// and the slug only selects which stored row to read — one that must exist
	// and be enabled, or this handler returned 404 above. The one value that
	// does come from the request, the Host used to build redirect_uri, lands
	// in a query parameter rather than in the destination, and the identity
	// provider rejects it if it does not match what is registered there.
	http.Redirect(w, r, authURL, http.StatusFound) //nolint:gosec // G710: destination is the configured provider's endpoint
}

// redirectURI is the callback this instance registers with the provider.
//
// With no configured base URL it is derived from the request, which is right
// for a local install and wrong behind a proxy that rewrites the scheme or
// Host. The identity provider is the backstop either way: it compares the
// redirect_uri against its own registered list and refuses a mismatch, so a
// forged Host header produces a failed login rather than a redirect anywhere
// useful.
func (h *OIDCHandler) redirectURI(r *http.Request, slug string) string {
	base := h.baseURL
	if base == "" {
		base = requestBaseURL(r)
	}

	return base + APIPrefix + "/auth/oidc/" + url.PathEscape(slug) + "/callback"
}

// requestBaseURL reconstructs this instance's root from the request.
func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	return scheme + "://" + r.Host
}

// safeReturnPath sanitizes a post-login destination.
//
// Only a same-site absolute path is allowed. Anything else — an absolute URL,
// a protocol-relative "//evil.example", a backslash a browser may normalize
// into a slash — is dropped, because an open redirect on the login path is
// what turns a phishing link into a convincing one.
func safeReturnPath(in string) string {
	if in == "" || !strings.HasPrefix(in, "/") {
		return ""
	}

	// "//host" and "/\host" are both read as protocol-relative by some
	// browsers, which makes them absolute URLs wearing a path's clothes.
	if strings.HasPrefix(in, "//") || strings.HasPrefix(in, `/\`) {
		return ""
	}

	if strings.ContainsAny(in, "\r\n") {
		return ""
	}

	return in
}

// --- GET /auth/oidc/{provider}/callback ------------------------------------

// handleCallback completes a login.
func (h *OIDCHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	// The flow is single-use whatever happens next.
	defer h.clearFlowCookie(w, r)

	cookie, err := r.Cookie(flowCookieName)
	if err != nil {
		WriteError(w, r, NewError(CodeUnauthorized,
			"No login is in progress; start again", err))

		return
	}

	flow, err := decodeFlow(cookie.Value)
	if err != nil {
		WriteError(w, r, NewError(CodeUnauthorized, "The login could not be resumed", err))

		return
	}

	query := r.URL.Query()

	// The provider may report its own failure — a user who canceled, a client
	// the administrator has since disabled.
	if providerError := query.Get("error"); providerError != "" {
		h.log.Warn("the identity provider refused the login",
			slog.String("provider", flow.Provider),
			slog.String("error", providerError),
			slog.String("description", query.Get("error_description")),
		)

		WriteError(w, r, NewError(CodeUnauthorized,
			"The identity provider did not complete the login", nil))

		return
	}

	// The state check. An attacker who can send a victim to a crafted callback
	// URL cannot make it match the cookie this browser holds.
	if query.Get("state") == "" || query.Get("state") != flow.State {
		WriteError(w, r, NewError(CodeUnauthorized,
			"The login could not be verified; start again", nil))

		return
	}

	// The slug in the path must be the one the flow started with, or a flow
	// begun for one provider could be completed against another.
	if r.PathValue("provider") != flow.Provider {
		WriteError(w, r, NewError(CodeUnauthorized,
			"The login could not be verified; start again", nil))

		return
	}

	orgID, err := uuid.Parse(flow.OrgID)
	if err != nil {
		WriteError(w, r, NewError(CodeUnauthorized, "The login could not be resumed", err))

		return
	}

	provider, err := h.repos.System().FindIdentityProvider(r.Context(), orgID, flow.Provider)
	if err != nil {
		WriteError(w, r, NewError(CodeNotFound, "No such identity provider", err))

		return
	}

	identity, err := h.registry.Exchange(r.Context(), toProvider(provider),
		h.redirectURI(r, flow.Provider),
		oidc.Flow{State: flow.State, Nonce: flow.Nonce, Verifier: flow.Verifier},
		query.Get("code"),
	)
	if err != nil {
		h.log.Warn("single sign-on exchange failed",
			slog.String("provider", flow.Provider),
			logging.Err(err),
		)

		WriteError(w, r, oidcError(err))

		return
	}

	result, err := h.provisioner.Provision(r.Context(), orgID, provider, identity)
	if err != nil {
		WriteError(w, r, provisionError(err))

		return
	}

	session, err := h.auth.StartSession(r.Context(), result.User, clientIP(r), r.UserAgent())
	if err != nil {
		WriteError(w, r, err)

		return
	}

	h.cookie.set(w, r, session.Token, session.Session.AbsoluteExpiresAt.Time)

	h.log.Info("single sign-on succeeded",
		slog.String("provider", flow.Provider),
		slog.String("user_id", result.User.ID.String()),
		slog.Bool("provisioned", result.Created),
	)

	// A browser arrives here by redirect, so it gets one back. An API client
	// that would rather have JSON asks for it.
	if flow.Return != "" || !wantsJSON(r) {
		destination := flow.Return
		if destination == "" {
			destination = "/"
		}

		http.Redirect(w, r, destination, http.StatusFound)

		return
	}

	WriteJSON(r.Context(), w, http.StatusOK, sessionEnvelope{
		User:    newUserResponse(result.User),
		Session: newSessionResponse(session.Session, true),
	})
}

// wantsJSON reports whether the caller asked for JSON rather than a redirect.
func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

// toProvider converts a stored row into what internal/oidc needs.
func toProvider(row model.IdentityProvider) oidc.Provider {
	mapping, err := oidc.UnmarshalMapping(row.ClaimMapping)
	if err != nil {
		// A malformed mapping falls back to the standard claim names rather
		// than failing the login: the defaults are right for most providers,
		// and refusing every login over a bad configuration value is a worse
		// outcome than ignoring it. The error surfaces in the provider's
		// administration screen.
		mapping = oidc.ClaimMapping{}
	}

	return oidc.Provider{
		Slug:          row.Slug,
		Name:          row.Name,
		Issuer:        row.Issuer,
		ClientID:      row.ClientID,
		ClientSecret:  row.ClientSecret,
		Scopes:        oidc.ParseScopes(row.Scopes),
		AutoProvision: row.AutoProvision.Bool(),
		DefaultRole:   row.DefaultRole,
		Mapping:       mapping,
	}
}

// oidcError maps a protocol failure onto the error contract.
//
// Discovery failing is the identity provider being unreachable — a 503, and an
// operator's problem. Everything else is a login to refuse, and all of them
// get one answer: which step of a token's validation failed is of interest to
// an attacker and to nobody else.
func oidcError(err error) error {
	if errors.Is(err, oidc.ErrDiscovery) {
		return NewError(CodeUnavailable,
			"The identity provider could not be reached", err)
	}

	return NewError(CodeUnauthorized, "The login could not be completed", err)
}

// provisionError maps a provisioning failure onto the error contract.
//
// These are distinguishable from each other, unlike the token failures above,
// because each one names something an administrator has to go and fix — and
// the person seeing it has already proven who they are.
func provisionError(err error) error {
	switch {
	case errors.Is(err, oidc.ErrNotProvisioned):
		return NewError(CodeForbidden,
			"This identity has no account here, and this provider does not create them", err)

	case errors.Is(err, oidc.ErrAccountDisabled):
		return NewError(CodeForbidden, "This account is disabled", err)

	case errors.Is(err, oidc.ErrNoEmail):
		return NewError(CodeForbidden,
			"The identity provider sent no email address, which is required to create an account", err)

	case errors.Is(err, oidc.ErrEmailNotVerified):
		return NewError(CodeForbidden,
			"The identity provider has not verified this address", err)

	default:
		return err
	}
}
