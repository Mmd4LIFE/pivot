package api_test

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

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
