// Package oidc turns an OpenID Connect login into a Pivot user.
//
// The flow is Authorization Code with PKCE and auto-discovery, per ADR-0008
// and docs/architecture/security-model.md#2-authentication.
//
// Three properties are not negotiable, and each exists because its absence is
// a known attack rather than a theoretical one:
//
// The ID token is verified, never merely decoded. Signature against the
// provider's published keys, issuer, audience, expiry — go-oidc does this, and
// it is the reason this package has a dependency rather than a JSON decoder. A
// decoded-but-unverified token is a string the caller supplied.
//
// A returning user is matched on the provider's `sub` claim and never on their
// email address. Addresses get reassigned inside directories; if alice leaves
// and a new alice@example.com is created, matching on email hands the newcomer
// the old account. `sub` is the only stable identifier a provider promises.
//
// The nonce is checked. Without it an ID token obtained for one login can be
// replayed into another session.
package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Errors a caller may branch on.
var (
	// ErrDiscovery means the provider's metadata could not be fetched. It is
	// distinct from a bad token: one is the identity provider being down, the
	// other is a login that should be refused.
	ErrDiscovery = errors.New("oidc: provider discovery failed")

	// ErrExchange means the authorization code could not be redeemed.
	ErrExchange = errors.New("oidc: code exchange failed")

	// ErrNoIDToken means the token response carried no id_token, which makes
	// it an OAuth2 response rather than an OpenID Connect one.
	ErrNoIDToken = errors.New("oidc: response contained no id_token")

	// ErrInvalidToken covers every reason an ID token was rejected: bad
	// signature, wrong issuer, wrong audience, expired, replayed nonce.
	ErrInvalidToken = errors.New("oidc: id token is not valid")

	// ErrNoSubject means the token carried no subject, so there is nothing
	// stable to identify the user by.
	ErrNoSubject = errors.New("oidc: id token has no subject")
)

// Provider is a configured identity provider.
//
// It is the database row turned into something usable, and deliberately holds
// no network state — discovery lives in [Registry], so a provider value can be
// passed around and compared without dragging an HTTP client behind it.
type Provider struct {
	Slug   string
	Name   string
	Issuer string

	ClientID string

	// ClientSecret may be empty. PKCE makes a public client safe, so a
	// provider configured without a secret is supported rather than refused.
	ClientSecret string

	Scopes []string

	// AutoProvision creates a Pivot user on first login. With it off, an
	// unknown subject is refused — which is what an organization that manages
	// its user list out of band wants.
	AutoProvision bool

	// DefaultRole is granted to a user created by provisioning.
	DefaultRole string

	Mapping ClaimMapping
}

// scopes returns the scopes to request, always including openid.
//
// A request without `openid` is an OAuth2 authorization, not an OpenID Connect
// one, and returns no ID token. Adding it unconditionally turns a confusing
// "no id_token in response" into something that cannot happen.
func (p Provider) scopes() []string {
	out := make([]string, 0, len(p.Scopes)+1)
	out = append(out, gooidc.ScopeOpenID)

	for _, s := range p.Scopes {
		if s != gooidc.ScopeOpenID && s != "" {
			out = append(out, s)
		}
	}

	return out
}

// Registry discovers providers and caches what it learns.
//
// Discovery is an HTTP round trip to the identity provider. Doing it on every
// login would make each one slower and would make Pivot's login depend on the
// provider's metadata endpoint being up at that instant, rather than on it
// having been up recently. The cache turns a hard dependency into a soft one.
type Registry struct {
	ttl time.Duration
	now func() time.Time

	// client is used for discovery and for the token exchange. It is injected
	// into the context rather than held on the oauth2 config because go-oidc
	// reads it from there too, so one setting covers both round trips.
	client *http.Client

	mu      sync.Mutex
	entries map[string]*cacheEntry
}

// cacheEntry is one discovered issuer.
type cacheEntry struct {
	provider *gooidc.Provider
	expires  time.Time
}

// DefaultDiscoveryTTL is how long discovery is trusted.
//
// An hour is well inside the window in which a provider might rotate signing
// keys, which is fine: go-oidc's key set refreshes independently of the
// document cached here.
const DefaultDiscoveryTTL = time.Hour

// NewRegistry builds a discovery cache.
func NewRegistry() *Registry {
	return &Registry{
		ttl:     DefaultDiscoveryTTL,
		now:     time.Now,
		client:  http.DefaultClient,
		entries: make(map[string]*cacheEntry),
	}
}

// SetClock replaces the time source. For tests only.
func (r *Registry) SetClock(now func() time.Time) { r.now = now }

// SetHTTPClient replaces the client used for discovery and token exchange.
//
// Tests point it at an in-process provider. A deployment behind an outbound
// proxy would use it too, which is why it is not test-only.
func (r *Registry) SetHTTPClient(c *http.Client) { r.client = c }

// withClient puts the registry's HTTP client in the context.
//
// Both go-oidc and oauth2 look for a client there rather than taking one as a
// parameter, so this is the single place that has to know about the
// convention.
func (r *Registry) withClient(ctx context.Context) context.Context {
	if r.client == nil {
		return ctx
	}

	return context.WithValue(ctx, oauth2.HTTPClient, r.client)
}

// discover returns the provider metadata for an issuer.
func (r *Registry) discover(ctx context.Context, issuer string) (*gooidc.Provider, error) {
	r.mu.Lock()

	if entry, ok := r.entries[issuer]; ok && entry.expires.After(r.now()) {
		r.mu.Unlock()

		return entry.provider, nil
	}

	r.mu.Unlock()

	// Discovery happens outside the lock: it is a network call, and holding a
	// mutex across one means a slow provider blocks logins to every other.
	// Two concurrent first-logins may both discover, which costs one extra
	// request and is cheaper than the alternative.
	provider, err := gooidc.NewProvider(r.withClient(ctx), issuer)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrDiscovery, issuer, err)
	}

	r.mu.Lock()
	r.entries[issuer] = &cacheEntry{provider: provider, expires: r.now().Add(r.ttl)}
	r.mu.Unlock()

	return provider, nil
}

// Forget drops an issuer from the cache, so the next login rediscovers.
// An administrator who has just repointed a provider should not have to wait
// out the TTL.
func (r *Registry) Forget(issuer string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.entries, issuer)
}

// oauthConfig builds the OAuth2 configuration for a provider.
func (r *Registry) oauthConfig(
	discovered *gooidc.Provider, p Provider, redirectURL string,
) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		Endpoint:     discovered.Endpoint(),
		RedirectURL:  redirectURL,
		Scopes:       p.scopes(),
	}
}

// Flow is the state a login carries across the redirect to the provider.
//
// All three values must survive until the callback and must be checked when it
// arrives. State defends against cross-site request forgery on the callback;
// Nonce defends against an ID token obtained elsewhere being replayed into
// this session; Verifier is the PKCE secret that proves the callback belongs
// to the same client that started the flow.
type Flow struct {
	State    string
	Nonce    string
	Verifier string
}

// NewFlow generates the per-login secrets.
func NewFlow() (Flow, error) {
	state, err := randomString()
	if err != nil {
		return Flow{}, err
	}

	nonce, err := randomString()
	if err != nil {
		return Flow{}, err
	}

	return Flow{State: state, Nonce: nonce, Verifier: oauth2.GenerateVerifier()}, nil
}

// randomBytes is the entropy per generated value: 256 bits, the same as a
// session token.
const randomBytes = 32

func randomString() (string, error) {
	buf := make([]byte, randomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oidc: generate random value: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// AuthCodeURL returns the URL to send the browser to.
//
// PKCE is always used, even when a client secret is configured. The two defend
// against different things — the secret proves which application is asking,
// the code challenge proves the callback reaches the same browser that
// started — and OAuth 2.1 requires the challenge regardless.
func (r *Registry) AuthCodeURL(
	ctx context.Context, p Provider, redirectURL string, flow Flow,
) (string, error) {
	discovered, err := r.discover(ctx, p.Issuer)
	if err != nil {
		return "", err
	}

	cfg := r.oauthConfig(discovered, p, redirectURL)

	return cfg.AuthCodeURL(flow.State,
		gooidc.Nonce(flow.Nonce),
		oauth2.S256ChallengeOption(flow.Verifier),
	), nil
}

// Exchange redeems an authorization code and returns the verified identity.
//
// Every check that matters happens here, and the order is deliberate: redeem
// the code, confirm the response actually contains an ID token, verify that
// token's signature and claims against the provider's published keys, then
// confirm the nonce matches the one this flow issued. A failure at any step is
// a refused login, never a degraded one.
func (r *Registry) Exchange(
	ctx context.Context, p Provider, redirectURL string, flow Flow, code string,
) (Identity, error) {
	discovered, err := r.discover(ctx, p.Issuer)
	if err != nil {
		return Identity{}, err
	}

	cfg := r.oauthConfig(discovered, p, redirectURL)
	ctx = r.withClient(ctx)

	token, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrExchange, err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return Identity{}, ErrNoIDToken
	}

	verifier := discovered.Verifier(&gooidc.Config{ClientID: p.ClientID})

	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}

	// The nonce ties this token to this login. go-oidc does not check it for
	// us: it cannot know what we sent.
	if idToken.Nonce != flow.Nonce {
		return Identity{}, fmt.Errorf("%w: nonce mismatch", ErrInvalidToken)
	}

	if idToken.Subject == "" {
		return Identity{}, ErrNoSubject
	}

	var claims map[string]any
	if cerr := idToken.Claims(&claims); cerr != nil {
		return Identity{}, fmt.Errorf("%w: unreadable claims: %w", ErrInvalidToken, cerr)
	}

	return p.Mapping.Apply(idToken.Subject, claims), nil
}

// ParseScopes splits the stored, space-separated scope string.
func ParseScopes(s string) []string {
	return strings.Fields(s)
}
