package api_test

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/oidc"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

// Conformance against a real identity provider.
//
// Everything else in this package tests Pivot against an identity provider
// Pivot's own authors wrote, which proves the code agrees with itself. This
// proves it agrees with Keycloak — a different implementation of the same
// specification, with its own opinions about claim shapes, `aud` handling,
// and what a discovery document contains.
//
// It is opt-in. The image is a large pull, and a suite that cannot run without
// a container is a suite people stop running. Point PIVOT_TEST_KEYCLOAK_URL at
// a realm's issuer to include it:
//
//	docker run -d --name pivot-keycloak -p 8180:8080 \
//	  -e KC_BOOTSTRAP_ADMIN_USERNAME=admin \
//	  -e KC_BOOTSTRAP_ADMIN_PASSWORD=admin \
//	  quay.io/keycloak/keycloak:26.0 start-dev
//
//	export PIVOT_TEST_KEYCLOAK_URL=http://localhost:8180/realms/master
//	export PIVOT_TEST_KEYCLOAK_CLIENT_ID=pivot
//	export PIVOT_TEST_KEYCLOAK_CLIENT_SECRET=...   # omit for a public client
//
// What it asserts is deliberately narrow: that Pivot can discover a real
// provider and build a valid authorization request against it. Driving a
// browser through Keycloak's login form is Part 11's job, where there is a
// browser to drive.
const (
	keycloakURLEnv    = "PIVOT_TEST_KEYCLOAK_URL"
	keycloakClientEnv = "PIVOT_TEST_KEYCLOAK_CLIENT_ID"
	keycloakSecretEnv = "PIVOT_TEST_KEYCLOAK_CLIENT_SECRET"
)

func TestKeycloakConformance(t *testing.T) {
	t.Parallel()

	issuer := os.Getenv(keycloakURLEnv)
	if issuer == "" {
		t.Skipf("%s not set; see the comment in this file to run it", keycloakURLEnv)
	}

	clientID := os.Getenv(keycloakClientEnv)
	if clientID == "" {
		clientID = "pivot"
	}

	bothEngines(t, func(t *testing.T, db *store.DB) {
		base := newAuthFixture(t, db, func(c *api.RouterConfig) {
			c.AuthLimit = api.RateLimit{Rate: 1000, Burst: 1000}
		})

		if _, err := base.repos.IdentityProviders.Create(base.ctx, repo.CreateIdentityProvider{
			Slug:          "keycloak",
			Name:          "Keycloak",
			Issuer:        strings.TrimRight(issuer, "/"),
			ClientID:      clientID,
			ClientSecret:  os.Getenv(keycloakSecretEnv),
			IsEnabled:     true,
			AutoProvision: true,
			DefaultRole:   "viewer",
		}); err != nil {
			t.Fatalf("create provider: %v", err)
		}

		base.rebuildWithOIDC(t, oidc.NewRegistry())

		// Discovery against the real thing, and an authorization request built
		// from what it published.
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
			base.server.URL+api.APIPrefix+"/auth/oidc/keycloak/start", http.NoBody)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}

		resp := send(t, base.client, req)
		if resp.status != http.StatusFound {
			t.Fatalf("start = %d, want 302: %s", resp.status, resp)
		}

		location, err := url.Parse(resp.header("Location"))
		if err != nil {
			t.Fatalf("parse redirect: %v", err)
		}

		// It must point at Keycloak, not at us.
		if !strings.HasPrefix(location.String(), strings.TrimRight(issuer, "/")) {
			t.Errorf("redirect = %q, want it to start at the issuer %q", location, issuer)
		}

		q := location.Query()

		for _, required := range []string{
			"client_id", "redirect_uri", "response_type",
			"scope", "state", "nonce", "code_challenge", "code_challenge_method",
		} {
			if q.Get(required) == "" {
				t.Errorf("the authorization request to Keycloak omits %s", required)
			}
		}

		if got := q.Get("code_challenge_method"); got != "S256" {
			t.Errorf("code_challenge_method = %q, want S256", got)
		}

		if got := q.Get("response_type"); got != "code" {
			t.Errorf("response_type = %q, want code", got)
		}

		if !strings.Contains(q.Get("scope"), "openid") {
			t.Errorf("scope = %q, want it to include openid", q.Get("scope"))
		}

		// The flow cookie must have been set, or the callback has nothing to
		// verify against.
		if resp.cookie("pivot_oidc_flow") == nil {
			t.Error("no flow cookie was set for the Keycloak login")
		}
	})
}

/*
The whole login, against a real Keycloak.

The test above proves Pivot can talk to Keycloak. This one proves somebody can
log in with it: discovery, the authorization request, Keycloak's own login
form, the callback, the code exchange, the ID token, and a Pivot session at the
end of it. Every step between "Pivot builds a URL" and "this person is signed
in" is a place the two implementations can disagree, and none of them are
exercised by a provider Pivot's own authors wrote.

It provisions its own client and its own user through Keycloak's admin API,
because the redirect URI has to match a server whose port is chosen when the
test starts. Nothing here assumes a realm somebody prepared by hand.

Opt-in on the same variable as the conformance test:

	docker run -d --name pivot-keycloak -p 8180:8080 \
	  -e KC_BOOTSTRAP_ADMIN_USERNAME=admin -e KC_BOOTSTRAP_ADMIN_PASSWORD=admin \
	  quay.io/keycloak/keycloak:26.0 start-dev

	export PIVOT_TEST_KEYCLOAK_URL=http://localhost:8180/realms/master
	go test ./internal/api/ -run TestKeycloakLogin -count=1
*/
func TestKeycloakLoginRoundTrip(t *testing.T) {
	t.Parallel()

	issuer := strings.TrimRight(os.Getenv(keycloakURLEnv), "/")
	if issuer == "" {
		t.Skipf("%s not set; see the comment in this file to run it", keycloakURLEnv)
	}

	kc := newKeycloakAdmin(t, issuer)

	// SQLite only. What is under test is two HTTP implementations agreeing,
	// and running it twice against two databases tests the database.
	f := newAuthFixture(t, openSQLite(t), func(c *api.RouterConfig) {
		c.AuthLimit = api.RateLimit{Rate: 1000, Burst: 1000}
	})

	const (
		secret   = "a-client-secret-for-the-test"
		username = "grace"
		password = "a-long-enough-password"
		email    = "grace@example.com"
	)

	// The redirect URI has to name the fixture's own server, which only has an
	// address once it is listening -- so the client is registered now rather
	// than by whoever started Keycloak.
	clientID := kc.createClient(t, f.server.URL+api.APIPrefix+"/auth/oidc/keycloak/callback", secret)
	kc.createUser(t, username, password, email, "Grace", "Hopper")

	if _, err := f.repos.IdentityProviders.Create(f.ctx, repo.CreateIdentityProvider{
		Slug:          "keycloak",
		Name:          "Keycloak",
		Issuer:        issuer,
		ClientID:      clientID,
		ClientSecret:  secret,
		IsEnabled:     true,
		AutoProvision: true,
		DefaultRole:   "viewer",
	}); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	f.rebuildWithOIDC(t, oidc.NewRegistry())

	// 1. Pivot sends the browser to Keycloak.
	start := f.follow(t, api.APIPrefix+"/auth/oidc/keycloak/start")
	if start.status != http.StatusFound {
		t.Fatalf("start = %d, want 302: %s", start.status, start)
	}

	authorize := start.header("Location")

	// 2. Keycloak's login page. A separate client, because this is the user's
	//    browser talking to the identity provider rather than to Pivot.
	browser := newTestClient(newJar(t))

	page := getString(t, browser, authorize)

	action := formAction(t, page)

	// 3. The user types their password into Keycloak's form.
	form := url.Values{"username": {username}, "password": {password}, "credentialId": {""}}

	submit, err := http.NewRequestWithContext(context.Background(),
		http.MethodPost, action, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build the login request: %v", err)
	}

	submit.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	login := send(t, browser, submit)
	if login.status != http.StatusFound {
		t.Fatalf("Keycloak's login form = %d, want 302: %s", login.status, login)
	}

	callback := login.header("Location")

	if !strings.HasPrefix(callback, f.server.URL) {
		t.Fatalf("Keycloak redirected to %q, which is not this Pivot", callback)
	}

	// 4. Back into Pivot, on the cookie jar that started the flow: the state
	//    and PKCE verifier live in a cookie set by /start, so a callback
	//    arriving on any other jar has to fail.
	back, err := http.NewRequestWithContext(context.Background(),
		http.MethodGet, callback, http.NoBody)
	if err != nil {
		t.Fatalf("build the callback request: %v", err)
	}

	done := send(t, f.client, back)
	if done.status != http.StatusFound {
		t.Fatalf("callback = %d, want 302: %s", done.status, done)
	}

	if cookie := done.cookie(api.DefaultCookie().Name); cookie == nil {
		t.Fatal("the callback set no session cookie")
	}

	// 5. And the session is real, for the person Keycloak authenticated --
	//    provisioned from the claims, because this account did not exist.
	me := f.follow(t, api.APIPrefix+"/auth/me")
	if me.status != http.StatusOK {
		t.Fatalf("/auth/me after an SSO login = %d: %s", me.status, me)
	}

	if !strings.Contains(string(me.body), email) {
		t.Errorf("/auth/me does not name the Keycloak user: %s", me.body)
	}

	if !strings.Contains(string(me.body), "Grace Hopper") {
		t.Errorf("the name from Keycloak's claims did not reach the account: %s", me.body)
	}
}

// --- talking to Keycloak's admin API ----------------------------------------

/*
keycloakAdmin provisions what this test needs and nothing more.

Hand-rolled rather than a client library: it is three requests, and a
dependency added so a test can create a user is a dependency in everybody's
build.
*/
type keycloakAdmin struct {
	base  string // http://host:port
	realm string
	token string

	client *http.Client
}

func newKeycloakAdmin(t *testing.T, issuer string) *keycloakAdmin {
	t.Helper()

	// The issuer is <base>/realms/<realm>; the admin API is <base>/admin/...
	idx := strings.Index(issuer, "/realms/")
	if idx < 0 {
		t.Fatalf("%q does not look like a realm issuer", issuer)
	}

	admin := &keycloakAdmin{
		base:   issuer[:idx],
		realm:  strings.TrimPrefix(issuer[idx:], "/realms/"),
		client: newTestClient(nil),
	}

	user := envOr("PIVOT_TEST_KEYCLOAK_ADMIN", "admin")
	password := envOr("PIVOT_TEST_KEYCLOAK_ADMIN_PASSWORD", "admin")

	form := url.Values{
		"client_id":  {"admin-cli"},
		"username":   {user},
		"password":   {password},
		"grant_type": {"password"},
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		admin.base+"/realms/master/protocol/openid-connect/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build the token request: %v", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp := send(t, admin.client, req)
	if resp.status != http.StatusOK {
		t.Fatalf("Keycloak refused the admin credentials (%d). Set "+
			"PIVOT_TEST_KEYCLOAK_ADMIN and PIVOT_TEST_KEYCLOAK_ADMIN_PASSWORD: %s",
			resp.status, resp)
	}

	var body struct {
		AccessToken string `json:"access_token"`
	}

	if err := json.Unmarshal(resp.body, &body); err != nil {
		t.Fatalf("decode the admin token: %v", err)
	}

	admin.token = body.AccessToken

	return admin
}

// post sends an admin API request and returns the response.
func (k *keycloakAdmin) post(t *testing.T, path string, payload any) response {
	t.Helper()

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		k.base+"/admin/realms/"+k.realm+path, strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+k.token)

	return send(t, k.client, req)
}

// createClient registers a confidential client for one redirect URI.
//
// A fresh client ID per run, so parallel runs and reruns do not collide -- and
// so a client left behind by a crashed test cannot make the next one pass for
// the wrong reason.
func (k *keycloakAdmin) createClient(t *testing.T, redirectURI, secret string) string {
	t.Helper()

	clientID := "pivot-test-" + strconv.FormatInt(time.Now().UnixNano(), 36)

	resp := k.post(t, "/clients", map[string]any{
		"clientId":            clientID,
		"enabled":             true,
		"protocol":            "openid-connect",
		"publicClient":        false,
		"standardFlowEnabled": true,
		"redirectUris":        []string{redirectURI},
		"secret":              secret,
	})

	if resp.status != http.StatusCreated {
		t.Fatalf("create the Keycloak client = %d: %s", resp.status, resp)
	}

	return clientID
}

// createUser adds somebody who can log in.
func (k *keycloakAdmin) createUser(t *testing.T, username, password, email, first, last string) {
	t.Helper()

	resp := k.post(t, "/users", map[string]any{
		"username":      username + "-" + strconv.FormatInt(time.Now().UnixNano(), 36),
		"email":         email,
		"emailVerified": true,
		"firstName":     first,
		"lastName":      last,
		"enabled":       true,
		"credentials": []map[string]any{
			{"type": "password", "value": password, "temporary": false},
		},
	})

	// Conflict is fine: a previous run left this address behind, and the
	// password is the same.
	if resp.status != http.StatusCreated && resp.status != http.StatusConflict {
		t.Fatalf("create the Keycloak user = %d: %s", resp.status, resp)
	}
}

// formAction pulls the login form's target out of Keycloak's HTML.
//
// Parsing a login page is unlovely and it is the point: this is what a browser
// does, and doing it any other way would test something Keycloak does not
// actually serve.
func formAction(t *testing.T, page string) string {
	t.Helper()

	match := regexp.MustCompile(`action="([^"]+)"`).FindStringSubmatch(page)
	if len(match) != 2 {
		t.Fatalf("no form action in Keycloak's login page:\n%s", truncate(page, 500))
	}

	return html.UnescapeString(match[1])
}

func getString(t *testing.T, client *http.Client, target string) string {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, http.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp := send(t, client, req)
	if resp.status != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", target, resp.status, resp)
	}

	return string(resp.body)
}

func newJar(t *testing.T) http.CookieJar {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}

	return jar
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n] + "..."
}
