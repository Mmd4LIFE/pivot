package oidc_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/oidc"
)

const testRedirectURL = "https://pivot.example/api/v1/auth/oidc/test/callback"

// testProvider builds a provider pointed at the fake identity provider.
func testProvider(idp *fakeIDP) oidc.Provider {
	return oidc.Provider{
		Slug:          "test",
		Name:          "Test IdP",
		Issuer:        idp.issuer(),
		ClientID:      "pivot",
		Scopes:        []string{"profile", "email"},
		AutoProvision: true,
		DefaultRole:   "viewer",
	}
}

// newRegistry returns a registry that talks to the in-process provider.
func newRegistry() *oidc.Registry {
	r := oidc.NewRegistry()
	r.SetHTTPClient(http.DefaultClient)

	return r
}

// The happy path, end to end: build an authorization URL, walk the redirect,
// redeem the code, and get back a verified identity.
func TestAuthorizationCodeFlow(t *testing.T) {
	t.Parallel()

	idp := newFakeIDP(t)
	idp.claims["email"] = "ada@example.com"
	idp.claims["name"] = "Ada Lovelace"
	idp.claims["groups"] = []any{"analysts", "everyone"}

	registry := newRegistry()
	provider := testProvider(idp)
	ctx := context.Background()

	flow, err := oidc.NewFlow()
	if err != nil {
		t.Fatalf("new flow: %v", err)
	}

	authURL, err := registry.AuthCodeURL(ctx, provider, testRedirectURL, flow)
	if err != nil {
		t.Fatalf("auth url: %v", err)
	}

	// PKCE must be on the wire. Without the challenge, a stolen authorization
	// code can be redeemed by whoever stole it.
	if !strings.Contains(authURL, "code_challenge=") {
		t.Error("the authorization URL carries no code_challenge")
	}

	if !strings.Contains(authURL, "code_challenge_method=S256") {
		t.Error("the authorization URL does not request S256")
	}

	if !strings.Contains(authURL, "nonce=") {
		t.Error("the authorization URL carries no nonce")
	}

	// openid must be requested, or the response is an OAuth2 one with no ID
	// token in it.
	if !strings.Contains(authURL, "scope=openid") {
		t.Errorf("the authorization URL does not request the openid scope: %s", authURL)
	}

	callback := idp.follow(t, authURLFor(authURL, "subject-ada"))

	if got := callback.Get("state"); got != flow.State {
		t.Errorf("state = %q, want the one we sent", got)
	}

	identity, err := registry.Exchange(ctx, provider, testRedirectURL, flow, callback.Get("code"))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	if identity.Subject != "subject-ada" {
		t.Errorf("subject = %q, want subject-ada", identity.Subject)
	}

	if identity.Email != "ada@example.com" {
		t.Errorf("email = %q", identity.Email)
	}

	if identity.Name != "Ada Lovelace" {
		t.Errorf("name = %q", identity.Name)
	}

	if len(identity.Groups) != 2 || identity.Groups[0] != "analysts" {
		t.Errorf("groups = %v, want [analysts everyone]", identity.Groups)
	}
}

// --- the rejection paths --------------------------------------------------

// A token signed with a key the provider never published must be refused.
//
// This is the single most important check in the package: without signature
// verification, an ID token is a string the caller made up, and anyone could
// log in as anyone.
func TestTokenSignedWithAnUnknownKeyIsRejected(t *testing.T) {
	t.Parallel()

	idp := newFakeIDP(t)
	idp.claims["email"] = "ada@example.com"
	idp.signWithWrongKey = true

	registry := newRegistry()
	provider := testProvider(idp)
	ctx := context.Background()

	flow, err := oidc.NewFlow()
	if err != nil {
		t.Fatalf("new flow: %v", err)
	}

	authURL, err := registry.AuthCodeURL(ctx, provider, testRedirectURL, flow)
	if err != nil {
		t.Fatalf("auth url: %v", err)
	}

	callback := idp.follow(t, authURL)

	_, err = registry.Exchange(ctx, provider, testRedirectURL, flow, callback.Get("code"))
	if err == nil {
		t.Fatal("a token signed with an unpublished key was accepted")
	}

	if !errors.Is(err, oidc.ErrInvalidToken) {
		t.Errorf("error = %v, want ErrInvalidToken", err)
	}
}

// A token whose issuer is not the configured one must be refused, even when
// the signature is good. Otherwise a token minted by one tenant's provider is
// accepted by another's.
func TestTokenFromAnotherIssuerIsRejected(t *testing.T) {
	t.Parallel()

	idp := newFakeIDP(t)
	idp.claims["email"] = "ada@example.com"
	idp.issuerOverride = "https://evil.example"

	registry := newRegistry()
	provider := testProvider(idp)
	ctx := context.Background()

	flow, err := oidc.NewFlow()
	if err != nil {
		t.Fatalf("new flow: %v", err)
	}

	authURL, err := registry.AuthCodeURL(ctx, provider, testRedirectURL, flow)
	if err != nil {
		t.Fatalf("auth url: %v", err)
	}

	callback := idp.follow(t, authURL)

	_, err = registry.Exchange(ctx, provider, testRedirectURL, flow, callback.Get("code"))
	if err == nil {
		t.Fatal("a token from a foreign issuer was accepted")
	}

	if !errors.Is(err, oidc.ErrInvalidToken) {
		t.Errorf("error = %v, want ErrInvalidToken", err)
	}
}

// A token carrying a different nonce must be refused.
//
// The nonce is what ties a token to *this* login. Without the check, a token
// obtained during one login can be replayed into another session — and
// go-oidc cannot do this for us, because it does not know what we sent.
func TestReplayedNonceIsRejected(t *testing.T) {
	t.Parallel()

	idp := newFakeIDP(t)
	idp.claims["email"] = "ada@example.com"
	idp.nonceOverride = "a-nonce-from-some-other-login"

	registry := newRegistry()
	provider := testProvider(idp)
	ctx := context.Background()

	flow, err := oidc.NewFlow()
	if err != nil {
		t.Fatalf("new flow: %v", err)
	}

	authURL, err := registry.AuthCodeURL(ctx, provider, testRedirectURL, flow)
	if err != nil {
		t.Fatalf("auth url: %v", err)
	}

	callback := idp.follow(t, authURL)

	_, err = registry.Exchange(ctx, provider, testRedirectURL, flow, callback.Get("code"))
	if err == nil {
		t.Fatal("a token with a foreign nonce was accepted")
	}

	if !errors.Is(err, oidc.ErrInvalidToken) {
		t.Errorf("error = %v, want ErrInvalidToken", err)
	}

	if !strings.Contains(err.Error(), "nonce") {
		t.Errorf("error = %v, want it to name the nonce", err)
	}
}

// Redeeming with the wrong PKCE verifier must fail at the provider.
//
// This asserts the protocol is actually being used rather than merely
// advertised: the challenge went out, and a mismatched verifier is refused.
func TestWrongPKCEVerifierIsRejected(t *testing.T) {
	t.Parallel()

	idp := newFakeIDP(t)
	idp.claims["email"] = "ada@example.com"

	registry := newRegistry()
	provider := testProvider(idp)
	ctx := context.Background()

	flow, err := oidc.NewFlow()
	if err != nil {
		t.Fatalf("new flow: %v", err)
	}

	authURL, err := registry.AuthCodeURL(ctx, provider, testRedirectURL, flow)
	if err != nil {
		t.Fatalf("auth url: %v", err)
	}

	callback := idp.follow(t, authURL)

	// A different flow means a different verifier - what an attacker holding
	// only the authorization code would have.
	attacker, err := oidc.NewFlow()
	if err != nil {
		t.Fatalf("new flow: %v", err)
	}

	attacker.Nonce = flow.Nonce

	_, err = registry.Exchange(ctx, provider, testRedirectURL, attacker, callback.Get("code"))
	if err == nil {
		t.Fatal("a code was redeemed with the wrong PKCE verifier")
	}

	if !errors.Is(err, oidc.ErrExchange) {
		t.Errorf("error = %v, want ErrExchange", err)
	}
}

// A token with no subject has nothing stable to identify a user by.
func TestTokenWithoutSubjectIsRejected(t *testing.T) {
	t.Parallel()

	idp := newFakeIDP(t)
	idp.claims["email"] = "ada@example.com"
	idp.claims["sub"] = ""

	registry := newRegistry()
	provider := testProvider(idp)
	ctx := context.Background()

	flow, err := oidc.NewFlow()
	if err != nil {
		t.Fatalf("new flow: %v", err)
	}

	authURL, err := registry.AuthCodeURL(ctx, provider, testRedirectURL, flow)
	if err != nil {
		t.Fatalf("auth url: %v", err)
	}

	callback := idp.follow(t, authURL)

	_, err = registry.Exchange(ctx, provider, testRedirectURL, flow, callback.Get("code"))
	if err == nil {
		t.Fatal("a token with no subject was accepted")
	}
}

// --- discovery ------------------------------------------------------------

// An unreachable issuer is a discovery failure, distinct from a bad token:
// one is the identity provider being down, the other is a login to refuse.
func TestUnreachableIssuerIsADiscoveryFailure(t *testing.T) {
	t.Parallel()

	registry := newRegistry()

	provider := oidc.Provider{
		Slug:     "broken",
		Issuer:   "http://127.0.0.1:1/nowhere",
		ClientID: "pivot",
	}

	flow, err := oidc.NewFlow()
	if err != nil {
		t.Fatalf("new flow: %v", err)
	}

	_, err = registry.AuthCodeURL(context.Background(), provider, testRedirectURL, flow)
	if err == nil {
		t.Fatal("discovery against an unreachable issuer succeeded")
	}

	if !errors.Is(err, oidc.ErrDiscovery) {
		t.Errorf("error = %v, want ErrDiscovery", err)
	}
}

// Discovery must be cached, or every login pays a round trip to the provider
// and Pivot's login depends on that endpoint being up at that instant.
func TestDiscoveryIsCached(t *testing.T) {
	t.Parallel()

	idp := newFakeIDP(t)
	registry := newRegistry()
	provider := testProvider(idp)
	ctx := context.Background()

	flow, err := oidc.NewFlow()
	if err != nil {
		t.Fatalf("new flow: %v", err)
	}

	if _, err := registry.AuthCodeURL(ctx, provider, testRedirectURL, flow); err != nil {
		t.Fatalf("first: %v", err)
	}

	// Taking the provider off the air must not break a subsequent login, which
	// is the entire point of caching it.
	idp.server.Close()

	if _, err := registry.AuthCodeURL(ctx, provider, testRedirectURL, flow); err != nil {
		t.Errorf("second call re-discovered rather than using the cache: %v", err)
	}

	// Forgetting it must force rediscovery, so an administrator who repoints a
	// provider does not have to wait out the TTL.
	registry.Forget(provider.Issuer)

	if _, err := registry.AuthCodeURL(ctx, provider, testRedirectURL, flow); err == nil {
		t.Error("Forget did not drop the cached provider")
	}
}

// Each flow must be unique, or state and nonce defend against nothing.
func TestFlowSecretsAreUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, 300)

	for range 100 {
		flow, err := oidc.NewFlow()
		if err != nil {
			t.Fatalf("new flow: %v", err)
		}

		for _, value := range []string{flow.State, flow.Nonce, flow.Verifier} {
			if value == "" {
				t.Fatal("a flow secret was empty")
			}

			if seen[value] {
				t.Fatalf("a flow secret repeated: %q", value)
			}

			seen[value] = true
		}
	}
}
