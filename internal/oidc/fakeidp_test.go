package oidc_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A minimal OpenID Connect provider, in process, signing with a real RSA key.
//
// Not a mock of Pivot's code — a mock would test itself. This is the other end
// of the protocol: it publishes a discovery document and a JWKS, issues real
// RS256-signed ID tokens, and enforces PKCE. Everything Pivot does to verify a
// token therefore runs for real, against keys it fetched over HTTP.
//
// A Keycloak container proves interoperability with a production identity
// provider, which this cannot; that is Part 8-b's. What this gives instead is
// the ability to make a token *wrong* on purpose — a bad signature, a foreign
// issuer, a replayed nonce — which is how the rejection paths get tested at
// all. A real provider will not issue you a token it has broken.
type fakeIDP struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	keyID  string

	// issued maps an authorization code to the login it belongs to.
	issued map[string]*pendingLogin

	// claims are added to every ID token, so a test can describe the directory
	// it wants without touching the signing code.
	claims map[string]any

	// signWithWrongKey makes the provider sign with a key it never published,
	// which is what an attacker supplying their own token looks like.
	signWithWrongKey bool

	// issuerOverride replaces the `iss` claim, for the confused-deputy case
	// where a token from another tenant is presented here.
	issuerOverride string

	// nonceOverride replaces the nonce, to test replay rejection.
	nonceOverride string
}

// pendingLogin is an authorization in flight.
type pendingLogin struct {
	nonce         string
	codeChallenge string
	subject       string
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	idp := &fakeIDP{
		key:    key,
		keyID:  "test-key-1",
		issued: make(map[string]*pendingLogin),
		claims: map[string]any{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", idp.handleDiscovery)
	mux.HandleFunc("/jwks", idp.handleJWKS)
	mux.HandleFunc("/authorize", idp.handleAuthorize)
	mux.HandleFunc("/token", idp.handleToken)

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)

	return idp
}

func (i *fakeIDP) issuer() string { return i.server.URL }

func (i *fakeIDP) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                i.issuer(),
		"authorization_endpoint":                i.issuer() + "/authorize",
		"token_endpoint":                        i.issuer() + "/token",
		"jwks_uri":                              i.issuer() + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

func (i *fakeIDP) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	pub := i.key.PublicKey

	writeJSON(w, map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"kid": i.keyID,
			"use": "sig",
			"alg": "RS256",
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}},
	})
}

// handleAuthorize records the login and redirects back with a code.
//
// A browser would show a login form here. The test skips the human and goes
// straight to the redirect, which is the part Pivot actually sees.
func (i *fakeIDP) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	code := fmt.Sprintf("code-%d", len(i.issued)+1)

	subject := q.Get("login_hint")
	if subject == "" {
		subject = "subject-1"
	}

	i.issued[code] = &pendingLogin{
		nonce:         q.Get("nonce"),
		codeChallenge: q.Get("code_challenge"),
		subject:       subject,
	}

	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)

		return
	}

	rq := redirect.Query()
	rq.Set("code", code)
	rq.Set("state", q.Get("state"))
	redirect.RawQuery = rq.Encode()

	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

// handleToken redeems a code, enforcing PKCE.
func (i *fakeIDP) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)

		return
	}

	code := r.Form.Get("code")

	login, ok := i.issued[code]
	if !ok {
		writeError(w, "invalid_grant", "unknown code")

		return
	}

	// PKCE: the verifier must hash to the challenge sent at authorize time.
	// A provider that skips this check would let a stolen code be redeemed by
	// anyone, which is the whole thing PKCE exists to stop.
	if login.codeChallenge != "" {
		verifier := r.Form.Get("code_verifier")
		if verifier == "" {
			writeError(w, "invalid_request", "missing code_verifier")

			return
		}

		if s256(verifier) != login.codeChallenge {
			writeError(w, "invalid_grant", "code_verifier does not match")

			return
		}
	}

	nonce := login.nonce
	if i.nonceOverride != "" {
		nonce = i.nonceOverride
	}

	issuer := i.issuer()
	if i.issuerOverride != "" {
		issuer = i.issuerOverride
	}

	claims := map[string]any{
		"iss":   issuer,
		"sub":   login.subject,
		"aud":   r.Form.Get("client_id"),
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
		"nonce": nonce,
	}

	// The client id arrives as Basic auth when a secret is configured.
	if claims["aud"] == "" {
		if id, _, ok := r.BasicAuth(); ok {
			claims["aud"] = id
		}
	}

	for k, v := range i.claims {
		claims[k] = v
	}

	signingKey := i.key
	if i.signWithWrongKey {
		other, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			http.Error(w, "keygen", http.StatusInternalServerError)

			return
		}

		signingKey = other
	}

	idToken, err := signRS256(signingKey, i.keyID, claims)
	if err != nil {
		http.Error(w, "sign: "+err.Error(), http.StatusInternalServerError)

		return
	}

	writeJSON(w, map[string]any{
		"access_token": "access-" + code,
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     idToken,
	})
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": code, "error_description": description,
	})
}

// follow walks the authorize redirect and returns the callback's query values,
// standing in for the browser.
func (i *fakeIDP) follow(t *testing.T, authURL string) url.Values {
	t.Helper()

	// The redirect target is Pivot's callback, which does not exist in these
	// tests, so the client is told not to follow it.
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(authURL) //nolint:noctx // a test against an in-process server
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	location, err := resp.Location()
	if err != nil {
		t.Fatalf("authorize did not redirect: %v", err)
	}

	return location.Query()
}

// loginAs points the next authorization at a particular subject.
func authURLFor(base, subject string) string {
	if strings.Contains(base, "?") {
		return base + "&login_hint=" + url.QueryEscape(subject)
	}

	return base + "?login_hint=" + url.QueryEscape(subject)
}
