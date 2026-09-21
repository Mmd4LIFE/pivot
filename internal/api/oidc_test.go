package api_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
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

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/oidc"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

// The single sign-on flow, driven end to end through Pivot's own endpoints
// against an in-process identity provider that signs real tokens.
//
// internal/oidc proves the protocol. This proves the surface: the flow cookie
// survives the redirect, state is checked, a session comes out the other side,
// and the provider's secret never does.

// --- a minimal identity provider ------------------------------------------

type idp struct {
	server  *httptest.Server
	key     *rsa.PrivateKey
	issued  map[string]*idpLogin
	claims  map[string]any
	subject string
}

type idpLogin struct {
	nonce     string
	challenge string
}

func newIDP(t *testing.T) *idp {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	p := &idp{
		key:     key,
		issued:  make(map[string]*idpLogin),
		claims:  map[string]any{},
		subject: "subject-1",
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		idpJSON(w, map[string]any{
			"issuer":                                p.server.URL,
			"authorization_endpoint":                p.server.URL + "/authorize",
			"token_endpoint":                        p.server.URL + "/token",
			"jwks_uri":                              p.server.URL + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"code_challenge_methods_supported":      []string{"S256"},
		})
	})

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		pub := p.key.PublicKey
		idpJSON(w, map[string]any{"keys": []map[string]any{{
			"kty": "RSA", "kid": "k1", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}}})
	})

	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		code := fmt.Sprintf("code-%d", len(p.issued)+1)
		p.issued[code] = &idpLogin{nonce: q.Get("nonce"), challenge: q.Get("code_challenge")}

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
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)

			return
		}

		login, ok := p.issued[r.Form.Get("code")]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			idpJSON(w, map[string]string{"error": "invalid_grant"})

			return
		}

		// A code is single-use. Redeeming one twice must fail, which is what
		// stops a replayed callback producing a second session.
		delete(p.issued, r.Form.Get("code"))

		if login.challenge != "" {
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != login.challenge {
				w.WriteHeader(http.StatusBadRequest)
				idpJSON(w, map[string]string{"error": "invalid_grant"})

				return
			}
		}

		audience := r.Form.Get("client_id")
		if audience == "" {
			if id, _, ok := r.BasicAuth(); ok {
				audience = id
			}
		}

		claims := map[string]any{
			"iss": p.server.URL, "sub": p.subject, "aud": audience,
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
			"nonce": login.nonce,
		}

		for k, v := range p.claims {
			claims[k] = v
		}

		token, err := idpSign(p.key, claims)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		idpJSON(w, map[string]any{
			"access_token": "at", "token_type": "Bearer",
			"expires_in": 3600, "id_token": token,
		})
	})

	p.server = httptest.NewServer(mux)
	t.Cleanup(p.server.Close)

	return p
}

func idpJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// idpSign produces an RS256 JWT.
func idpSign(key *rsa.PrivateKey, claims map[string]any) (string, error) {
	header, err := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": "k1"})
	if err != nil {
		return "", fmt.Errorf("header: %w", err)
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("claims: %w", err)
	}

	input := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload)

	digest := sha256.Sum256([]byte(input))

	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}

	return input + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// --- the fixture ----------------------------------------------------------

// ssoFixture is a Pivot server with SSO wired to an in-process provider.
type ssoFixture struct {
	*authFixture

	idp      *idp
	provider string
}

func newSSOFixture(t *testing.T, db *store.DB, in repo.CreateIdentityProvider) *ssoFixture {
	t.Helper()

	provider := newIDP(t)

	base := newAuthFixture(t, db, func(c *api.RouterConfig) {
		c.AuthLimit = api.RateLimit{Rate: 1000, Burst: 1000}
	})

	in.Issuer = provider.server.URL

	if _, err := base.repos.IdentityProviders.Create(base.ctx, in); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// The handler needs the real base URL, which only exists once the test
	// server is listening — so the router is rebuilt with it.
	registry := oidc.NewRegistry()

	base.rebuildWithOIDC(t, registry)

	return &ssoFixture{authFixture: base, idp: provider, provider: in.Slug}
}

// follow walks a redirect chain by hand, carrying the fixture's cookie jar, so
// a test can inspect each hop rather than only the destination.
func (f *ssoFixture) follow(t *testing.T, path string) response {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodGet, f.server.URL+path, http.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	return send(t, f.client, req)
}

// login walks the whole flow: start, the provider's redirect, the callback.
func (f *ssoFixture) login(t *testing.T) response {
	t.Helper()

	start := f.follow(t, api.APIPrefix+"/auth/oidc/"+f.provider+"/start")
	if start.status != http.StatusFound {
		t.Fatalf("start = %d, want 302: %s", start.status, start)
	}

	authURL := start.header("Location")
	if authURL == "" {
		t.Fatal("start did not redirect anywhere")
	}

	// The provider redirects back to Pivot's callback.
	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodGet, authURL, http.NoBody)
	if err != nil {
		t.Fatalf("build authorize request: %v", err)
	}

	authorized := send(t, f.client, req)
	if authorized.status != http.StatusFound {
		t.Fatalf("authorize = %d, want 302: %s", authorized.status, authorized)
	}

	callback := authorized.header("Location")
	if callback == "" {
		t.Fatal("the provider did not redirect back")
	}

	cbReq, err := http.NewRequestWithContext(context.Background(),
		http.MethodGet, callback, http.NoBody)
	if err != nil {
		t.Fatalf("build callback request: %v", err)
	}

	cbReq.Header.Set("Accept", "application/json")

	return send(t, f.client, cbReq)
}

// --- the tests ------------------------------------------------------------

func ssoProvider(slug string) repo.CreateIdentityProvider {
	return repo.CreateIdentityProvider{
		Slug: slug, Name: "Test IdP", ClientID: "pivot",
		IsEnabled: true, AutoProvision: true, DefaultRole: "viewer",
	}
}

// The whole point: an SSO login produces a Pivot session, and that session is
// indistinguishable from one a password login would have produced.
func TestSSOLoginProducesASession(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newSSOFixture(t, db, ssoProvider("test"))
		f.idp.claims["email"] = "grace@example.com"
		f.idp.claims["name"] = "Grace Hopper"
		f.idp.subject = "subject-grace"

		resp := f.login(t)
		if resp.status != http.StatusOK {
			t.Fatalf("callback = %d, want 200: %s", resp.status, resp)
		}

		// The session cookie must be set, with the same attributes the
		// password path uses.
		cookie := resp.cookie(api.DefaultCookie().Name)
		if cookie == nil {
			t.Fatal("no session cookie was set")
		}

		if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
			t.Errorf("session cookie attributes differ from the password path: %+v", cookie)
		}

		// And the session actually works.
		me := f.request(t, http.MethodGet, api.APIPrefix+"/auth/me", nil)
		if me.status != http.StatusOK {
			t.Fatalf("/auth/me after SSO = %d: %s", me.status, me)
		}

		body := decodeJSON[struct {
			User struct {
				Email string `json:"email"`
			} `json:"user"`
		}](t, me)

		if body.User.Email != "grace@example.com" {
			t.Errorf("email = %q, want grace@example.com", body.User.Email)
		}
	})
}

// The flow cookie must survive the redirect, and the callback must refuse a
// state that does not match it.
func TestCallbackRejectsAMismatchedState(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newSSOFixture(t, db, ssoProvider("test"))
		f.idp.claims["email"] = "grace@example.com"

		start := f.follow(t, api.APIPrefix+"/auth/oidc/test/start")
		if start.status != http.StatusFound {
			t.Fatalf("start = %d: %s", start.status, start)
		}

		// The flow cookie must be set, HttpOnly, and Lax — Strict would be
		// withheld on the callback, which arrives from the provider's origin.
		flow := start.cookie("pivot_oidc_flow")
		if flow == nil {
			t.Fatal("no flow cookie was set")
		}

		if !flow.HttpOnly {
			t.Error("the flow cookie is not HttpOnly")
		}

		if flow.SameSite != http.SameSiteLaxMode {
			t.Errorf("flow cookie SameSite = %v; Strict would break every login", flow.SameSite)
		}

		// Walk the provider, then tamper with the state on the way back.
		authURL := start.header("Location")

		req, err := http.NewRequestWithContext(context.Background(),
			http.MethodGet, authURL, http.NoBody)
		if err != nil {
			t.Fatalf("build: %v", err)
		}

		authorized := send(t, f.client, req)

		callback, err := url.Parse(authorized.header("Location"))
		if err != nil {
			t.Fatalf("parse callback: %v", err)
		}

		q := callback.Query()
		q.Set("state", "not-the-state-we-issued")
		callback.RawQuery = q.Encode()

		cbReq, err := http.NewRequestWithContext(context.Background(),
			http.MethodGet, callback.String(), http.NoBody)
		if err != nil {
			t.Fatalf("build callback: %v", err)
		}

		resp := send(t, f.client, cbReq)
		if resp.status != http.StatusUnauthorized {
			t.Errorf("a forged state was accepted: %d %s", resp.status, resp)
		}
	})
}

// A callback with no flow cookie has nothing to check against and must refuse.
func TestCallbackWithoutAFlowIsRefused(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newSSOFixture(t, db, ssoProvider("test"))

		resp := f.follow(t,
			api.APIPrefix+"/auth/oidc/test/callback?code=anything&state=anything")

		if resp.status != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401: %s", resp.status, resp)
		}
	})
}

// Replaying a completed callback must not produce a second session.
//
// The flow cookie is cleared on the way out and the provider refuses a reused
// code; either alone would stop this, and both is the point.
func TestCallbackCannotBeReplayed(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newSSOFixture(t, db, ssoProvider("test"))
		f.idp.claims["email"] = "grace@example.com"

		start := f.follow(t, api.APIPrefix+"/auth/oidc/test/start")

		req, err := http.NewRequestWithContext(context.Background(),
			http.MethodGet, start.header("Location"), http.NoBody)
		if err != nil {
			t.Fatalf("build: %v", err)
		}

		authorized := send(t, f.client, req)
		callback := authorized.header("Location")

		first, err := http.NewRequestWithContext(context.Background(),
			http.MethodGet, callback, http.NoBody)
		if err != nil {
			t.Fatalf("build: %v", err)
		}

		if resp := send(t, f.client, first); resp.status != http.StatusFound &&
			resp.status != http.StatusOK {
			t.Fatalf("first callback = %d: %s", resp.status, resp)
		}

		second, err := http.NewRequestWithContext(context.Background(),
			http.MethodGet, callback, http.NoBody)
		if err != nil {
			t.Fatalf("build: %v", err)
		}

		resp := send(t, f.client, second)
		if resp.status == http.StatusOK || resp.status == http.StatusFound {
			t.Errorf("a callback was replayed successfully: %d %s", resp.status, resp)
		}
	})
}

// --- the provider list ----------------------------------------------------

// The login page needs the buttons before anyone is authenticated, so this is
// open — and must therefore say as little as possible.
func TestProviderListIsPublicAndMinimal(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newSSOFixture(t, db, ssoProvider("test"))

		resp := f.request(t, http.MethodGet, api.APIPrefix+"/auth/providers", nil)
		if resp.status != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.status, resp)
		}

		body := decodeJSON[struct {
			Providers []struct {
				Slug string `json:"slug"`
				Name string `json:"name"`
			} `json:"providers"`
		}](t, resp)

		if len(body.Providers) != 1 || body.Providers[0].Slug != "test" {
			t.Fatalf("providers = %+v, want one named test", body.Providers)
		}

		// Nothing beyond slug and name. The issuer and client id are not
		// secret, but publishing an organization's vendor relationships to
		// anonymous callers buys nothing.
		for _, forbidden := range []string{"issuer", "clientId", "clientSecret", "http"} {
			if strings.Contains(resp.String(), forbidden) {
				t.Errorf("the public provider list contains %q:\n%s", forbidden, resp)
			}
		}
	})
}

// --- the client secret ----------------------------------------------------

// The client secret is write-only. There is no read path, for an
// administrator or anyone else.
func TestClientSecretIsNeverReturned(t *testing.T) {
	t.Parallel()

	const secret = "a-very-secret-client-secret"

	bothEngines(t, func(t *testing.T, db *store.DB) {
		in := ssoProvider("test")
		in.ClientSecret = secret

		f := newSSOFixture(t, db, in)

		// An administrator, so the gated endpoints are reachable.
		if gerr := f.repos.Roles.Grant(f.ctx, repo.GrantRole{
			SubjectType: "user", SubjectID: f.user.ID, Relation: "admin",
			ObjectType: "organization", ObjectID: f.org.ID,
		}); gerr != nil {
			t.Fatalf("grant: %v", gerr)
		}

		if resp := f.loginPassword(t); resp.status != http.StatusOK {
			t.Fatalf("login: %s", resp)
		}

		bodies := map[string]string{}
		bodies["public list"] = f.request(t, http.MethodGet,
			api.APIPrefix+"/auth/providers", nil).String()
		bodies["admin list"] = f.request(t, http.MethodGet,
			api.APIPrefix+"/organization/identity-providers", nil).String()

		for name, body := range bodies {
			if strings.Contains(body, secret) {
				t.Errorf("the %s response contains the client secret:\n%s", name, body)
			}

			if strings.Contains(body, "clientSecret") {
				t.Errorf("the %s response has a clientSecret field:\n%s", name, body)
			}
		}

		// It is still stored, and still reported as present.
		if !strings.Contains(bodies["admin list"], `"hasClientSecret":true`) {
			t.Errorf("the admin list does not report that a secret is configured:\n%s",
				bodies["admin list"])
		}
	})
}

// Configuring an identity provider decides who an organization's users are, so
// it takes an administrator.
func TestProviderAdministrationRequiresPermission(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newSSOFixture(t, db, ssoProvider("test"))

		// Logged in, but with no role at all.
		if resp := f.loginPassword(t); resp.status != http.StatusOK {
			t.Fatalf("login: %s", resp)
		}

		for _, call := range []struct{ method, path string }{
			{http.MethodGet, "/organization/identity-providers"},
			{http.MethodPost, "/organization/identity-providers"},
		} {
			resp := f.request(t, call.method, api.APIPrefix+call.path, nil)
			if resp.status != http.StatusForbidden {
				t.Errorf("%s %s = %d, want 403: %s",
					call.method, call.path, resp.status, resp)
			}
		}
	})
}

// --- open redirect --------------------------------------------------------

// The post-login destination must not become an open redirect.
//
// A login URL that will bounce the browser anywhere is what turns a phishing
// link into a convincing one: the victim sees a real Pivot domain, logs in for
// real, and lands somewhere else entirely.
func TestReturnPathCannotLeaveTheSite(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newSSOFixture(t, db, ssoProvider("test"))
		f.idp.claims["email"] = "grace@example.com"

		for _, hostile := range []string{
			"https://evil.example/phish",
			"//evil.example/phish",
			`/\evil.example`,
			"http://evil.example",
		} {
			start := f.follow(t, api.APIPrefix+"/auth/oidc/test/start?return="+
				url.QueryEscape(hostile))
			if start.status != http.StatusFound {
				t.Fatalf("start = %d: %s", start.status, start)
			}

			req, err := http.NewRequestWithContext(context.Background(),
				http.MethodGet, start.header("Location"), http.NoBody)
			if err != nil {
				t.Fatalf("build: %v", err)
			}

			authorized := send(t, f.client, req)

			cbReq, cerr := http.NewRequestWithContext(context.Background(),
				http.MethodGet, authorized.header("Location"), http.NoBody)
			if cerr != nil {
				t.Fatalf("build: %v", cerr)
			}

			resp := send(t, f.client, cbReq)

			if location := resp.header("Location"); strings.Contains(location, "evil.example") {
				t.Errorf("return=%q redirected off-site to %q", hostile, location)
			}
		}
	})
}

// A same-site path is honored, so the feature still works.
func TestReturnPathIsHonoredWhenSafe(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newSSOFixture(t, db, ssoProvider("test"))
		f.idp.claims["email"] = "grace@example.com"

		start := f.follow(t, api.APIPrefix+"/auth/oidc/test/start?return=%2Fdashboards%2F7")

		req, err := http.NewRequestWithContext(context.Background(),
			http.MethodGet, start.header("Location"), http.NoBody)
		if err != nil {
			t.Fatalf("build: %v", err)
		}

		authorized := send(t, f.client, req)

		cbReq, cerr := http.NewRequestWithContext(context.Background(),
			http.MethodGet, authorized.header("Location"), http.NoBody)
		if cerr != nil {
			t.Fatalf("build: %v", cerr)
		}

		resp := send(t, f.client, cbReq)

		if got := resp.header("Location"); got != "/dashboards/7" {
			t.Errorf("Location = %q, want /dashboards/7", got)
		}
	})
}

// A disabled provider is indistinguishable from one that does not exist.
func TestDisabledProviderIsNotFound(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		in := ssoProvider("test")
		in.IsEnabled = false

		f := newSSOFixture(t, db, in)

		resp := f.follow(t, api.APIPrefix+"/auth/oidc/test/start")
		if resp.status != http.StatusNotFound {
			t.Errorf("status = %d, want 404: %s", resp.status, resp)
		}

		listed := f.request(t, http.MethodGet, api.APIPrefix+"/auth/providers", nil)
		if strings.Contains(listed.String(), "test") {
			t.Errorf("a disabled provider was offered on the login page: %s", listed)
		}
	})
}
