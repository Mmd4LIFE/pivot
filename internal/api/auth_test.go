package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/auth"
	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/oidc"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// These tests drive the real stack — router, middleware, auth service,
// repositories, migrated database — over a real HTTP listener with a real
// cookie jar. An authentication flow is exactly the kind of thing that passes
// unit tests and fails end to end, because the failure modes live in the
// seams: a cookie attribute that makes the browser drop it, a middleware
// ordering that runs Argon2 before the rate limiter.

const postgresURLEnv = "PIVOT_TEST_POSTGRES_URL"

const (
	fixtureEmail    = "ada@example.com"
	fixturePassword = "a-sufficiently-long-password"
	fixtureSlug     = "acme"
)

// authFixture is a running server with one organization and one user.
type authFixture struct {
	server  *httptest.Server
	handler http.Handler
	client  *http.Client
	repos   *repo.Repositories
	org     model.Organization
	user    model.User
	ctx     context.Context

	// svc and cfg are kept so a test can re-serve the fixture with more wired
	// up — see rebuildWithOIDC.
	svc *auth.Service
	cfg api.RouterConfig
}

// fullRouter builds the complete HTTP surface — authentication included —
// over a throwaway SQLite database.
//
// The spec-drift test needs it. A router built without an auth handler never
// registers the auth paths, so every documented one would fall through to the
// catch-all and 404, and the test would be checking a surface nobody serves.
func fullRouter(t *testing.T) http.Handler {
	t.Helper()

	return newAuthFixture(t, openSQLite(t)).handler
}

func testDBConfig(url string) config.DatabaseConfig {
	cfg := config.Default().Database
	cfg.URL = url
	cfg.AutoMigrate = false

	return cfg
}

func openSQLite(t *testing.T) *store.DB {
	t.Helper()

	db, err := store.Open(context.Background(),
		testDBConfig(filepath.Join(t.TempDir(), "api-test.db")), discardLogger())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}

func openPostgres(t *testing.T) *store.DB {
	t.Helper()

	url := os.Getenv(postgresURLEnv)
	if url == "" {
		t.Skipf("%s not set; run `make test-all` to include PostgreSQL", postgresURLEnv)
	}

	schema := "api_" + sanitize(t.Name())

	admin, err := store.Open(context.Background(), testDBConfig(url), discardLogger())
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = admin.Close() }()

	ctx := context.Background()
	if _, derr := admin.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); derr != nil {
		t.Fatalf("drop schema: %v", derr)
	}

	if _, cerr := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); cerr != nil {
		t.Fatalf("create schema: %v", cerr)
	}

	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}

	db, err := store.Open(ctx, testDBConfig(url+sep+"search_path="+schema), discardLogger())
	if err != nil {
		t.Fatalf("open postgres schema: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()

		cleanup, cerr := store.Open(context.Background(), testDBConfig(url), discardLogger())
		if cerr != nil {
			return
		}
		defer func() { _ = cleanup.Close() }()

		_, _ = cleanup.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	return db
}

func sanitize(in string) string {
	out := make([]rune, 0, len(in))

	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		default:
			out = append(out, '_')
		}
	}

	if len(out) > 40 {
		out = out[:40]
	}

	return string(out)
}

// newAuthFixture migrates a database, seeds one organization and user, and
// serves the real router over a real listener.
func newAuthFixture(t *testing.T, db *store.DB, opts ...func(*api.RouterConfig)) *authFixture {
	t.Helper()

	if err := store.Migrate(context.Background(), db, discardLogger()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repos := repo.New(db)

	org, err := repos.System().CreateOrganization(context.Background(),
		repo.CreateOrganization{Name: "Acme", Slug: fixtureSlug})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	ctx := tenant.WithScope(context.Background(),
		tenant.MustNewScope(org.ID, uuid.NullUUID{}))

	hash, err := auth.HashPassword(fixturePassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	user, err := repos.Users.Create(ctx, repo.CreateUser{
		Email: fixtureEmail, Name: "Ada", PasswordHash: hash, IsActive: true,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	svc := auth.NewService(repos, auth.DefaultPolicy(), discardLogger())
	checker, cache := authz.New(repos)

	authHandler := api.NewAuthHandler(svc, repos, api.DefaultCookie(), discardLogger())
	authHandler.SetChecker(checker)

	cfg := api.RouterConfig{
		Log:     discardLogger(),
		CORS:    api.DefaultCORS(),
		Auth:    authHandler,
		Roles:   api.NewRoleHandler(repos, checker, cache, discardLogger()),
		Checker: checker,
	}

	// Options run last so a test can substitute a broken checker without the
	// routes disappearing with it.
	for _, opt := range opts {
		opt(&cfg)
	}

	handler := api.NewRouter(cfg).Handler()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}

	return &authFixture{
		server:  srv,
		handler: handler,
		client:  newTestClient(jar),
		repos:   repos,
		org:     org,
		user:    user,
		ctx:     ctx,
		svc:     svc,
		cfg:     cfg,
	}
}

// newTestClient returns a client that does not follow redirects.
//
// The SSO flow is a chain of them, and a test that wants to assert what each
// hop does has to see each hop. Following automatically would collapse the
// whole flow into its destination.
func newTestClient(jar http.CookieJar) *http.Client {
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// rebuildWithOIDC re-serves the fixture with single sign-on wired up.
//
// The OIDC handler needs this instance's externally reachable URL to build the
// redirect URI, and that only exists once the test server is listening — so
// the router is built twice rather than guessing a port.
func (f *authFixture) rebuildWithOIDC(t *testing.T, registry *oidc.Registry) {
	t.Helper()

	f.cfg.OIDC = api.NewOIDCHandler(
		f.repos, registry, f.svc, api.DefaultCookie(), f.server.URL, discardLogger())

	handler := api.NewRouter(f.cfg).Handler()

	f.server.Config.Handler = handler
	f.handler = handler
}

// loginPassword logs the fixture's seeded user in over the password endpoint.
func (f *authFixture) loginPassword(t *testing.T) response {
	t.Helper()

	return f.login(t, fixtureEmail, fixturePassword)
}

// response is a completed exchange: the body is already read and closed, so a
// test can assert on the status, the cookies and the body in any order without
// juggling an open reader.
type response struct {
	status  int
	headers http.Header
	cookies []*http.Cookie
	body    []byte
}

// header returns one response header.
func (r response) header(name string) string { return r.headers.Get(name) }

// String renders the body, for failure messages.
func (r response) String() string { return string(r.body) }

// cookie returns a named cookie from the response, or nil.
func (r response) cookie(name string) *http.Cookie {
	for _, c := range r.cookies {
		if c.Name == name {
			return c
		}
	}

	return nil
}

// send issues one request and drains the response.
func send(t *testing.T, client *http.Client, req *http.Request) response {
	t.Helper()

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	return response{
		status:  resp.StatusCode,
		headers: resp.Header.Clone(),
		cookies: resp.Cookies(),
		body:    body,
	}
}

// request issues a call against the fixture's server, carrying cookies.
func (f *authFixture) request(t *testing.T, method, path string, body any) response {
	t.Helper()

	var reader io.Reader

	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}

		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, f.server.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return send(t, f.client, req)
}

func (f *authFixture) login(t *testing.T, email, password string) response {
	t.Helper()

	return f.request(t, http.MethodPost, api.APIPrefix+"/auth/login",
		map[string]string{"email": email, "password": password})
}

func decodeJSON[T any](t *testing.T, resp response) T {
	t.Helper()

	var out T
	if err := json.Unmarshal(resp.body, &out); err != nil {
		t.Fatalf("decode body: %v\nbody: %s", err, resp.body)
	}

	return out
}

// errorCode reads the error envelope's code.
func errorCode(t *testing.T, resp response) api.Code {
	t.Helper()

	var body api.ErrorResponse
	if err := json.Unmarshal(resp.body, &body); err != nil {
		t.Fatalf("response is not the error envelope: %v\nbody: %s", err, resp.body)
	}

	return body.Error.Code
}

// bothEngines runs a test against SQLite and, when configured, PostgreSQL.
func bothEngines(t *testing.T, run func(t *testing.T, db *store.DB)) {
	t.Helper()

	t.Run("sqlite", func(t *testing.T) { run(t, openSQLite(t)) })
	t.Run("postgres", func(t *testing.T) { run(t, openPostgres(t)) })
}

// --- the round trip --------------------------------------------------------

// The whole point of the part: log in over HTTP, be recognized, log out, stop
// being recognized. Anything less than the full cycle can pass while the
// session is unrevokable.
func TestLoginMeLogout(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)

		resp := f.login(t, fixtureEmail, fixturePassword)
		if resp.status != http.StatusOK {
			t.Fatalf("login status = %d, want 200: %s", resp.status, resp)
		}

		cookie := resp.cookie(api.DefaultCookie().Name)
		if cookie == nil {
			t.Fatal("login set no session cookie")
		}

		if !cookie.HttpOnly {
			t.Error("the session cookie is not HttpOnly; a script can read it")
		}

		if cookie.SameSite != http.SameSiteLaxMode {
			t.Errorf("SameSite = %v, want Lax — it is the CSRF defense here", cookie.SameSite)
		}

		// Secure is deliberately absent over plaintext: a browser discards a
		// Secure cookie sent over http://, which would break the local install.
		if cookie.Secure {
			t.Error("the cookie is Secure on a plaintext listener; localhost would drop it")
		}

		me := f.request(t, http.MethodGet, api.APIPrefix+"/auth/me", nil)
		if me.status != http.StatusOK {
			t.Fatalf("/auth/me status = %d, want 200: %s", me.status, me)
		}

		body := decodeJSON[struct {
			User struct {
				ID    string `json:"id"`
				Email string `json:"email"`
			} `json:"user"`
		}](t, me)

		if body.User.Email != fixtureEmail {
			t.Errorf("/auth/me returned %q, want %q", body.User.Email, fixtureEmail)
		}

		if body.User.ID != f.user.ID.String() {
			t.Errorf("/auth/me returned user %q, want %q", body.User.ID, f.user.ID)
		}

		out := f.request(t, http.MethodPost, api.APIPrefix+"/auth/logout", nil)
		if out.status != http.StatusNoContent {
			t.Fatalf("logout status = %d, want 204: %s", out.status, out)
		}

		// The very next call must fail. Immediate revocation is the entire
		// reason sessions are server-side rather than stateless tokens.
		after := f.request(t, http.MethodGet, api.APIPrefix+"/auth/me", nil)
		if after.status != http.StatusUnauthorized {
			t.Errorf("after logout /auth/me = %d, want 401", after.status)
		}

		if code := errorCode(t, after); code != api.CodeUnauthorized {
			t.Errorf("after logout code = %q, want %q", code, api.CodeUnauthorized)
		}
	})
}

// A password hash must never reach a client. Asserting on the JSON rather than
// only on logs is the point: the wire types are hand-written precisely so a
// new column cannot appear in a response, and this is what proves it.
func TestResponsesNeverCarryASecret(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)

		// Ordered, not a map literal: the login has to happen before the two
		// calls that depend on the cookie it sets.
		bodies := map[string]string{}
		bodies["login"] = f.login(t, fixtureEmail, fixturePassword).String()
		bodies["me"] = f.request(t, http.MethodGet, api.APIPrefix+"/auth/me", nil).String()
		bodies["sessions"] = f.request(t, http.MethodGet, api.APIPrefix+"/auth/sessions", nil).String()

		for name, body := range bodies {
			for _, forbidden := range []string{
				"$argon2id$", // the hash itself
				"passwordHash", "password_hash",
				"tokenHash", "token_hash",
				fixturePassword,
			} {
				if strings.Contains(body, forbidden) {
					t.Errorf("the %s response contains %q:\n%s", name, forbidden, body)
				}
			}
		}
	})
}

// The session token is set as an HttpOnly cookie and returned nowhere else.
// Putting it in the body would hand it to any script on the page, which is
// what HttpOnly exists to prevent.
func TestLoginDoesNotReturnTheTokenInTheBody(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)

		resp := f.login(t, fixtureEmail, fixturePassword)

		cookie := resp.cookie(api.DefaultCookie().Name)
		if cookie == nil || cookie.Value == "" {
			t.Fatal("no session cookie was set")
		}

		if strings.Contains(resp.String(), cookie.Value) {
			t.Errorf("the login response body contains the session token:\n%s", resp)
		}
	})
}

// --- throttling before Argon2 ----------------------------------------------

// An unauthenticated endpoint that allocates 64 MiB per call is a
// denial-of-service surface, so the limiter has to sit *outside* the handler.
//
// The assertion is indirect on purpose, and stronger for it: every login that
// reaches the service records a failed attempt, so the recorded count is
// exactly the number of requests that got past the limiter. If throttling
// happened inside the handler, the count would equal the number of requests
// sent.
func TestLoginIsThrottledBeforeAnyPasswordWork(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db, func(c *api.RouterConfig) {
			c.AuthLimit = api.RateLimit{Rate: 1, Burst: 3}
		})

		const attempts = 25

		var throttled, reached int

		for range attempts {
			resp := f.login(t, fixtureEmail, "the-wrong-password-entirely")

			switch resp.status {
			case http.StatusTooManyRequests:
				if code := errorCode(t, resp); code == api.CodeRateLimited {
					throttled++
				}
			case http.StatusUnauthorized:
				reached++
			default:
				t.Fatalf("unexpected login status %d: %s", resp.status, resp)
			}
		}

		if throttled == 0 {
			t.Fatalf("%d rapid logins from one address were never throttled", attempts)
		}

		attempt, err := f.repos.System().GetLoginAttempt(context.Background(), f.org.ID, fixtureEmail)
		if err != nil {
			t.Fatalf("read login attempts: %v", err)
		}

		if attempt.FailedCount != int64(reached) {
			t.Errorf("recorded %d failures but %d requests reached the service; "+
				"throttled requests are doing password work",
				attempt.FailedCount, reached)
		}

		if attempt.FailedCount >= attempts {
			t.Errorf("every one of %d requests reached Argon2; the limiter is inside the handler",
				attempts)
		}
	})
}

// A lockout and a rate limit are both 429, and a client must be able to tell
// them apart without reading prose. This is the concrete case the error
// registry's "codes are independent of status" rule exists for.
func TestLockoutIsDistinguishableFromRateLimiting(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		// A generous limiter, so the 429 under test is the lockout and not
		// the throttle.
		f := newAuthFixture(t, db, func(c *api.RouterConfig) {
			c.AuthLimit = api.RateLimit{Rate: 1000, Burst: 1000}
		})

		var locked bool

		for range 15 {
			resp := f.login(t, fixtureEmail, "the-wrong-password-entirely")
			if resp.status != http.StatusTooManyRequests {
				continue
			}

			if code := errorCode(t, resp); code == api.CodeAccountLocked {
				locked = true

				break
			}
		}

		if !locked {
			t.Fatal("repeated failures never produced PIVOT-AUTH-005")
		}

		// The right password must not get in while the lockout holds.
		if resp := f.login(t, fixtureEmail, fixturePassword); resp.status == http.StatusOK {
			t.Error("a locked account accepted the correct password")
		}
	})
}

// --- organization resolution -----------------------------------------------

// Login needs an organization before a scope exists. With one it is implied;
// with several the caller must say which. Defaulting to "the first one" would
// silently authenticate people against a tenant they never named.
func TestLoginOrganizationResolution(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)

		t.Run("implied when only one exists", func(t *testing.T) {
			if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
				t.Errorf("status = %d, want 200: %s", resp.status, resp)
			}
		})

		t.Run("an unknown slug looks like a wrong password", func(t *testing.T) {
			resp := f.request(t, http.MethodPost, api.APIPrefix+"/auth/login",
				map[string]string{
					"email":        fixtureEmail,
					"password":     fixturePassword,
					"organization": "no-such-tenant",
				})

			if resp.status != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401: a 404 here maps an instance's tenants",
					resp.status)
			}
		})

		// Adding a second organization must turn the implied case into an
		// explicit one rather than picking a winner.
		if _, err := f.repos.System().CreateOrganization(context.Background(),
			repo.CreateOrganization{Name: "Globex", Slug: "globex"}); err != nil {
			t.Fatalf("create second org: %v", err)
		}

		t.Run("required once several exist", func(t *testing.T) {
			resp := f.login(t, fixtureEmail, fixturePassword)

			if resp.status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422: %s", resp.status, resp)
			}

			if code := errorCode(t, resp); code != api.CodeValidationFailed {
				t.Errorf("code = %q, want %q", code, api.CodeValidationFailed)
			}
		})

		t.Run("named explicitly still works", func(t *testing.T) {
			resp := f.request(t, http.MethodPost, api.APIPrefix+"/auth/login",
				map[string]string{
					"email":        fixtureEmail,
					"password":     fixturePassword,
					"organization": fixtureSlug,
				})

			if resp.status != http.StatusOK {
				t.Errorf("status = %d, want 200: %s", resp.status, resp)
			}
		})
	})
}

// --- session management ----------------------------------------------------

func TestSessionListAndRevoke(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)

		if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
			t.Fatalf("login failed: %s", resp)
		}

		listed := f.request(t, http.MethodGet, api.APIPrefix+"/auth/sessions", nil)
		if listed.status != http.StatusOK {
			t.Fatalf("list status = %d: %s", listed.status, listed)
		}

		body := decodeJSON[struct {
			Sessions []struct {
				ID      string `json:"id"`
				Current bool   `json:"current"`
			} `json:"sessions"`
		}](t, listed)

		if len(body.Sessions) != 1 {
			t.Fatalf("listed %d sessions, want 1", len(body.Sessions))
		}

		if !body.Sessions[0].Current {
			t.Error("the session making the request is not marked current")
		}

		revoked := f.request(t, http.MethodDelete,
			api.APIPrefix+"/auth/sessions/"+body.Sessions[0].ID, nil)

		if revoked.status != http.StatusNoContent {
			t.Fatalf("revoke status = %d: %s", revoked.status, revoked)
		}

		after := f.request(t, http.MethodGet, api.APIPrefix+"/auth/me", nil)
		if after.status != http.StatusUnauthorized {
			t.Errorf("after revoking the current session /auth/me = %d, want 401", after.status)
		}
	})
}

// Revoking is scoped to the caller, not merely to their organization.
//
// The org-scoped repository method would have allowed a colleague's session to
// be ended by anyone in the same tenant — right for an administrator, wrong
// for the endpoint that manages your own devices.
func TestCannotRevokeAnotherUsersSession(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)

		// A second user in the same organization, with a live session.
		hash, err := auth.HashPassword(fixturePassword)
		if err != nil {
			t.Fatalf("hash: %v", err)
		}

		other, err := f.repos.Users.Create(f.ctx, repo.CreateUser{
			Email: "grace@example.com", Name: "Grace", PasswordHash: hash, IsActive: true,
		})
		if err != nil {
			t.Fatalf("create second user: %v", err)
		}

		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("cookie jar: %v", err)
		}

		victim := &authFixture{
			server: f.server, handler: f.handler, client: &http.Client{Jar: jar},
			repos: f.repos, org: f.org, user: other, ctx: f.ctx,
		}

		if resp := victim.login(t, "grace@example.com", fixturePassword); resp.status != http.StatusOK {
			t.Fatalf("second user login failed: %s", resp)
		}

		victimSessions, err := f.repos.Sessions.List(f.ctx, other.ID)
		if err != nil {
			t.Fatalf("list victim sessions: %v", err)
		}

		if len(victimSessions) != 1 {
			t.Fatalf("victim has %d sessions, want 1", len(victimSessions))
		}

		// Now the attacker logs in and names the victim's session ID.
		if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
			t.Fatalf("login failed: %s", resp)
		}

		resp := f.request(t, http.MethodDelete,
			api.APIPrefix+"/auth/sessions/"+victimSessions[0].ID.String(), nil)

		if resp.status != http.StatusNotFound {
			t.Errorf("revoking another user's session returned %d, want 404", resp.status)
		}

		// And the victim is still logged in, which is the part that matters.
		me := victim.request(t, http.MethodGet, api.APIPrefix+"/auth/me", nil)
		if me.status != http.StatusOK {
			t.Errorf("the victim's session was ended: /auth/me = %d", me.status)
		}
	})
}

// --- unauthenticated access ------------------------------------------------

// With session-backed scoping, an anonymous caller has no tenant, so every
// scoped endpoint must refuse before a handler runs.
func TestAuthenticatedEndpointsRejectAnonymousCallers(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)

		for _, path := range []string{"/auth/me", "/auth/sessions"} {
			resp := f.request(t, http.MethodGet, api.APIPrefix+path, nil)

			if resp.status != http.StatusUnauthorized {
				t.Errorf("GET %s without a session = %d, want 401", path, resp.status)
			}

			if code := errorCode(t, resp); code != api.CodeUnauthorized {
				t.Errorf("GET %s code = %q, want %q", path, code, api.CodeUnauthorized)
			}
		}
	})
}

// Logging out without a session is a success: the caller asked to be logged
// out and they are. An error here gives a client something to handle on the
// one path where nothing can be wrong.
func TestLogoutWithoutASessionSucceeds(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)

		resp := f.request(t, http.MethodPost, api.APIPrefix+"/auth/logout", nil)
		if resp.status != http.StatusNoContent {
			t.Errorf("status = %d, want 204: %s", resp.status, resp)
		}
	})
}

// A stale cookie must be cleared, or the browser keeps sending a token the
// server has already rejected and every later request looks like a mystery.
func TestAStaleCookieIsCleared(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)

		req, err := http.NewRequestWithContext(context.Background(),
			http.MethodGet, f.server.URL+api.APIPrefix+"/auth/me", http.NoBody)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}

		req.AddCookie(&http.Cookie{
			Name:  api.DefaultCookie().Name,
			Value: "a-token-that-was-never-issued",
		})

		resp := send(t, f.client, req)

		if resp.status != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.status)
		}

		cleared := resp.cookie(api.DefaultCookie().Name)
		if cleared == nil || cleared.MaxAge >= 0 {
			t.Error("the dead session cookie was not cleared")
		}
	})
}

// The tenant on a request comes from the session row and nowhere else. A
// header or body field naming an organization must have no effect — that is
// the property that makes cross-tenant access require a real credential.
func TestTenantComesFromTheSessionNotTheRequest(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)

		if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
			t.Fatalf("login failed: %s", resp)
		}

		other, err := f.repos.System().CreateOrganization(context.Background(),
			repo.CreateOrganization{Name: "Globex", Slug: "globex"})
		if err != nil {
			t.Fatalf("create second org: %v", err)
		}

		req, rerr := http.NewRequestWithContext(context.Background(),
			http.MethodGet, f.server.URL+api.APIPrefix+"/auth/me", http.NoBody)
		if rerr != nil {
			t.Fatalf("build request: %v", rerr)
		}

		// Every plausible way a client might try to name a tenant.
		req.Header.Set("X-Organization-Id", other.ID.String())
		req.Header.Set("X-Org-Id", other.ID.String())
		req.Header.Set("X-Tenant", other.Slug)

		resp := send(t, f.client, req)

		body := decodeJSON[struct {
			User struct {
				OrganizationID string `json:"organizationId"`
			} `json:"user"`
		}](t, resp)

		if body.User.OrganizationID != f.org.ID.String() {
			t.Errorf("organization = %q, want the session's %q",
				body.User.OrganizationID, f.org.ID)
		}
	})
}

// decodeInto unmarshals a response body into any destination.
func decodeInto(resp response, into any) error {
	return json.Unmarshal(resp.body, into)
}

// contains is strings.Contains, named locally so the intent reads at the call
// site: "the response body contains the secret".
func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }
