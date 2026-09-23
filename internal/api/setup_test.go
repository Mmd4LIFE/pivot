package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/auth"
	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/setup"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

/*
The first run, over HTTP.

internal/setup tests the rules. These test the part a browser meets: the status
the page branches on, the cookie that makes "claimed" and "signed in" one step,
and the refusals.
*/

const (
	setupStatusPath = api.APIPrefix + "/setup/status"
	setupPath       = api.APIPrefix + "/setup"
	setupToken      = "the-token-from-the-banner"
)

// setupFixture is an unclaimed Pivot: a migrated database with nothing in it.
type setupFixture struct {
	server *httptest.Server
	client *http.Client
}

func newSetupFixture(t *testing.T, db *store.DB, token string) *setupFixture {
	t.Helper()

	if err := store.Migrate(context.Background(), db, discardLogger()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repos := repo.New(db)
	svc := auth.NewService(repos, auth.DefaultPolicy(), discardLogger())
	checker, cache := authz.New(repos)

	authHandler := api.NewAuthHandler(svc, repos, api.DefaultCookie(), discardLogger())
	authHandler.SetChecker(checker)

	router := api.NewRouter(api.RouterConfig{
		Log:     discardLogger(),
		CORS:    api.DefaultCORS(),
		Auth:    authHandler,
		Roles:   api.NewRoleHandler(repos, checker, cache, discardLogger()),
		Checker: checker,
		Setup: api.NewSetupHandler(
			setup.NewService(repos, token), authHandler, discardLogger()),
	})

	srv := httptest.NewServer(router.Handler())
	t.Cleanup(srv.Close)

	return &setupFixture{server: srv, client: newTestClient(nil)}
}

// claim posts a setup request.
func (f *setupFixture) claim(t *testing.T, body string) response {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodPost, f.server.URL+setupPath, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	return send(t, f.client, req)
}

func (f *setupFixture) status(t *testing.T) map[string]any {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodGet, f.server.URL+setupStatusPath, http.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp := send(t, f.client, req)

	if resp.status != http.StatusOK {
		t.Fatalf("status endpoint = %d, want 200: %s", resp.status, resp)
	}

	var out map[string]any
	if uerr := json.Unmarshal(resp.body, &out); uerr != nil {
		t.Fatalf("status body is not JSON: %v\n%s", uerr, resp.body)
	}

	return out
}

// validClaim is a well-formed request body.
func validClaim(token string) string {
	body, _ := json.Marshal(map[string]string{
		"organization": "Acme Analytics",
		"name":         "Ada Lovelace",
		"email":        "ada@example.com",
		"password":     "a-long-enough-password",
		"token":        token,
	})

	return string(body)
}

// The deliverable, over HTTP: an empty database in, a signed-in administrator
// out, in one request.
func TestClaimingAnInstanceSignsYouIn(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newSetupFixture(t, db, setupToken)

		before := f.status(t)
		if before["initialized"] != false {
			t.Fatalf("a fresh instance reports initialized = %v", before["initialized"])
		}

		if before["tokenRequired"] != true {
			t.Errorf("tokenRequired = %v, want true", before["tokenRequired"])
		}

		resp := f.claim(t, validClaim(setupToken))

		if resp.status != http.StatusCreated {
			t.Fatalf("claim = %d, want 201: %s", resp.status, resp)
		}

		// The cookie is the deliverable. Without it the last step of installing
		// Pivot is typing the password you just chose into a login form.
		cookie := resp.cookie(api.DefaultCookie().Name)
		if cookie == nil {
			t.Fatal("claiming the instance set no session cookie")
		}

		if !cookie.HttpOnly {
			t.Error("the session cookie is not HttpOnly")
		}

		// And it is a session the rest of the API accepts, which is the only
		// way to know the cookie is real rather than well-formed.
		req, err := http.NewRequestWithContext(context.Background(),
			http.MethodGet, f.server.URL+api.APIPrefix+"/auth/me", http.NoBody)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}

		req.AddCookie(cookie)

		me := send(t, f.client, req)
		if me.status != http.StatusOK {
			t.Fatalf("/auth/me = %d, want 200: %s", me.status, me)
		}

		if !strings.Contains(string(me.body), "ada@example.com") {
			t.Errorf("/auth/me does not name the new user: %s", me.body)
		}

		after := f.status(t)
		if after["initialized"] != true {
			t.Errorf("after claiming, initialized = %v", after["initialized"])
		}
	})
}

// Setup closes permanently. This is the test that matters most on this
// endpoint: an open one is a way to mint administrators.
func TestSetupClosesAfterTheFirstClaim(t *testing.T) {
	t.Parallel()

	f := newSetupFixture(t, openSQLite(t), setupToken)

	if resp := f.claim(t, validClaim(setupToken)); resp.status != http.StatusCreated {
		t.Fatalf("first claim = %d, want 201: %s", resp.status, resp)
	}

	second, _ := json.Marshal(map[string]string{
		"organization": "Mallory Incorporated",
		"email":        "mallory@example.com",
		"password":     "another-long-password",
		"token":        setupToken,
	})

	resp := f.claim(t, string(second))

	if resp.status != http.StatusConflict {
		t.Fatalf("second claim = %d, want 409: %s", resp.status, resp)
	}

	if code := codeOf(t, resp); code != api.CodeAlreadyInitialized {
		t.Errorf("code = %s, want %s", code, api.CodeAlreadyInitialized)
	}
}

// The token is what stands between an unclaimed instance on a network and
// whoever is scanning it.
func TestAClaimWithoutTheTokenIsRefused(t *testing.T) {
	t.Parallel()

	for name, token := range map[string]string{
		"absent": "",
		"wrong":  "a-guess",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newSetupFixture(t, openSQLite(t), setupToken)

			resp := f.claim(t, validClaim(token))

			if resp.status != http.StatusForbidden {
				t.Fatalf("claim = %d, want 403: %s", resp.status, resp)
			}

			// One code for both, deliberately: the next step is the same, and
			// an unauthenticated endpoint should not help somebody work out
			// which half they got right.
			if code := codeOf(t, resp); code != api.CodeSetupTokenInvalid {
				t.Errorf("code = %s, want %s", code, api.CodeSetupTokenInvalid)
			}

			// Still claimable afterwards. A refusal that consumed the one
			// chance would leave an instance nobody can ever set up.
			if f.status(t)["initialized"] != false {
				t.Error("a refused claim marked the instance as set up")
			}
		})
	}
}

// An instance configured without a token is claimable by anyone who can reach
// it. That is a supported configuration for a laptop and a terrible one for a
// network, so it is tested rather than assumed either way.
func TestAnInstanceWithNoTokenSaysSo(t *testing.T) {
	t.Parallel()

	f := newSetupFixture(t, openSQLite(t), "")

	if f.status(t)["tokenRequired"] != false {
		t.Error("tokenRequired is true on an instance with no token configured")
	}

	if resp := f.claim(t, validClaim("")); resp.status != http.StatusCreated {
		t.Fatalf("claim = %d, want 201: %s", resp.status, resp)
	}
}

func TestAnIncompleteClaimIsRefused(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"no organization": `{"email":"ada@example.com","password":"a-long-enough-password","token":"` + setupToken + `"}`,
		"no email":        `{"organization":"Acme","password":"a-long-enough-password","token":"` + setupToken + `"}`,
		"no password":     `{"organization":"Acme","email":"ada@example.com","token":"` + setupToken + `"}`,
		"blank organization": `{"organization":"   ","email":"ada@example.com",` +
			`"password":"a-long-enough-password","token":"` + setupToken + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newSetupFixture(t, openSQLite(t), setupToken)

			resp := f.claim(t, body)

			if resp.status != http.StatusUnprocessableEntity {
				t.Fatalf("claim = %d, want 422: %s", resp.status, resp)
			}
		})
	}
}

// A short password comes back as a field error rather than a 500. The rule
// lives in the hasher, and this is the path that carries it to the form.
func TestAShortPasswordNamesTheField(t *testing.T) {
	t.Parallel()

	f := newSetupFixture(t, openSQLite(t), setupToken)

	body, _ := json.Marshal(map[string]string{
		"organization": "Acme",
		"email":        "ada@example.com",
		"password":     "short",
		"token":        setupToken,
	})

	resp := f.claim(t, string(body))

	if resp.status != http.StatusUnprocessableEntity {
		t.Fatalf("claim = %d, want 422: %s", resp.status, resp)
	}

	if !strings.Contains(string(resp.body), `"field":"password"`) {
		t.Errorf("the error does not name the password field: %s", resp.body)
	}
}

// The strict authentication limiter, because claiming an instance costs an
// Argon2 hash and the endpoint has no session to hide behind.
func TestClaimingIsRateLimited(t *testing.T) {
	t.Parallel()

	f := newSetupFixture(t, openSQLite(t), setupToken)

	var throttled bool

	for range 2 * api.LimitAuth.Burst {
		// Wrong token on purpose: the instance must stay unclaimed so that
		// every attempt reaches the limiter rather than the "already done"
		// refusal.
		if resp := f.claim(t, validClaim("wrong")); resp.status == http.StatusTooManyRequests {
			throttled = true

			break
		}
	}

	if !throttled {
		t.Error("setup is not rate limited; an anonymous caller can hash without bound")
	}
}

// codeOf reads the error code out of a response.
func codeOf(t *testing.T, resp response) api.Code {
	t.Helper()

	var body api.ErrorResponse
	if err := json.Unmarshal(resp.body, &body); err != nil {
		t.Fatalf("response is not the error envelope: %v\n%s", err, resp.body)
	}

	return body.Error.Code
}
