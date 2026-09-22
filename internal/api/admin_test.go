package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/oidc"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

/*
The administrative endpoints, doing their job.

Until now these were only ever tested for *refusal*. The endpoint assertion
harness in authz_test.go proves that an unpermitted caller gets a 403, which is
the property Part 7-b cared about -- but a 403 never reaches the handler, so
`handleGrant`, `handleAdminCreate`, `handleAdminUpdate` and `handleAdminDelete`
had zero coverage between them. Every existing test that needed a role granted
called the repository directly and went around the endpoint entirely.

So the surface an administrator actually uses had never been driven. That is
not a coverage number, it is a gap: these endpoints write role assignments and
identity providers, which are the two things that decide who can get in.

The coverage gate is what surfaced it, which is the argument for the gate.
*/

// asAdmin returns a fixture whose user is signed in and is an administrator.
//
// The role is granted through the repository rather than the endpoint, because
// somebody has to be the first administrator and the endpoint requires one
// already -- the same bootstrap problem `pivot admin create-user` solves by
// making the first user in an organization an admin.
func asAdmin(t *testing.T, db *store.DB) *authFixture {
	t.Helper()

	f := newAuthFixture(t, db)

	if err := f.repos.Roles.Grant(f.ctx, repo.GrantRole{
		SubjectType: string(authz.SubjectUser),
		SubjectID:   f.user.ID,
		Relation:    string(authz.RelationAdmin),
		ObjectType:  string(authz.TypeOrganization),
		ObjectID:    f.org.ID,
	}); err != nil {
		t.Fatalf("granting the first admin: %v", err)
	}

	if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
		t.Fatalf("login = %d, want 200: %s", resp.status, resp)
	}

	return f
}

// asAdminWithProviders is asAdmin with the single sign-on handler wired up.
//
// Provider administration lives on the OIDC handler, which the base fixture
// leaves nil -- so without this the endpoints are not merely forbidden, they
// do not exist, and the router answers 404. That is the correct behavior for
// an instance with SSO switched off, and it is why these tests have to ask
// for it explicitly.
func asAdminWithProviders(t *testing.T, db *store.DB) *authFixture {
	t.Helper()

	f := asAdmin(t, db)

	registry := oidc.NewRegistry()
	f.rebuildWithOIDC(t, registry)

	return f
}

// ── the role catalog ─────────────────────────────────────────────────────────

func TestRoleCatalogListsTheBuiltins(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := asAdmin(t, db)

		resp := f.request(t, http.MethodGet, api.APIPrefix+"/roles", nil)
		if resp.status != http.StatusOK {
			t.Fatalf("GET /roles = %d, want 200: %s", resp.status, resp)
		}

		body := decodeJSON[struct {
			Roles []struct {
				Name        string   `json:"name"`
				Permissions []string `json:"permissions"`
			} `json:"roles"`
		}](t, resp)

		if len(body.Roles) == 0 {
			t.Fatal("the catalog is empty")
		}

		// The catalog is what an administrator reads to decide what a role
		// means. A role with no permissions listed tells them nothing.
		for _, role := range body.Roles {
			if len(role.Permissions) == 0 {
				t.Errorf("role %q lists no permissions", role.Name)
			}
		}
	})
}

// ── granting and revoking ────────────────────────────────────────────────────

// secondUser creates another member of the same organization.
func secondUser(t *testing.T, f *authFixture, email string) uuid.UUID {
	t.Helper()

	user, err := f.repos.Users.Create(f.ctx, repo.CreateUser{
		Email: email, Name: "Grace", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create second user: %v", err)
	}

	return user.ID
}

func TestGrantThenListThenRevoke(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := asAdmin(t, db)
		subject := secondUser(t, f, "grace@example.com")

		granted := f.request(t, http.MethodPost, api.APIPrefix+"/organization/role-assignments",
			map[string]string{
				"subjectType": "user",
				"subjectId":   subject.String(),
				"role":        "editor",
			})
		if granted.status != http.StatusCreated && granted.status != http.StatusOK {
			t.Fatalf("grant = %d, want 200 or 201: %s", granted.status, granted)
		}

		listed := f.request(t, http.MethodGet, api.APIPrefix+"/organization/role-assignments", nil)
		if listed.status != http.StatusOK {
			t.Fatalf("list = %d, want 200: %s", listed.status, listed)
		}

		body := decodeJSON[struct {
			Assignments []struct {
				SubjectID string `json:"subjectId"`
				Role      string `json:"role"`
			} `json:"assignments"`
		}](t, listed)

		found := false

		for _, a := range body.Assignments {
			if a.SubjectID == subject.String() && a.Role == "editor" {
				found = true
			}
		}

		if !found {
			t.Fatalf("the grant is not in the list: %s", listed)
		}

		path := fmt.Sprintf("%s/organization/role-assignments/user/%s/editor",
			api.APIPrefix, subject)

		revoked := f.request(t, http.MethodDelete, path, nil)
		if revoked.status != http.StatusNoContent && revoked.status != http.StatusOK {
			t.Fatalf("revoke = %d, want 204 or 200: %s", revoked.status, revoked)
		}

		// And it is really gone, rather than merely reported as removed.
		after := decodeJSON[struct {
			Assignments []struct {
				SubjectID string `json:"subjectId"`
				Role      string `json:"role"`
			} `json:"assignments"`
		}](t, f.request(t, http.MethodGet, api.APIPrefix+"/organization/role-assignments", nil))

		for _, a := range after.Assignments {
			if a.SubjectID == subject.String() && a.Role == "editor" {
				t.Error("the assignment survived its own revocation")
			}
		}
	})
}

func TestGrantRejectsWhatItShould(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := asAdmin(t, db)
		subject := secondUser(t, f, "grace@example.com")

		cases := []struct {
			name  string
			body  map[string]string
			field string
		}{
			{
				name:  "no subject type",
				body:  map[string]string{"subjectId": subject.String(), "role": "editor"},
				field: "subjectType",
			},
			{
				name: "a subject type that is not a user or a group",
				body: map[string]string{
					"subjectType": "unicorn", "subjectId": subject.String(), "role": "editor",
				},
				field: "subjectType",
			},
			{
				name: "a subject id that is not a UUID",
				body: map[string]string{
					"subjectType": "user", "subjectId": "not-a-uuid", "role": "editor",
				},
				field: "subjectId",
			},
			{
				// The model is a closed set. Inventing a role here would create
				// an assignment the checker can never match, which is worse
				// than refusing: it looks granted and grants nothing.
				name: "a role that does not exist",
				body: map[string]string{
					"subjectType": "user", "subjectId": subject.String(), "role": "sorcerer",
				},
				field: "role",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				resp := f.request(t, http.MethodPost,
					api.APIPrefix+"/organization/role-assignments", tc.body)

				if resp.status != http.StatusUnprocessableEntity {
					t.Fatalf("status = %d, want 422: %s", resp.status, resp)
				}

				var body api.ErrorResponse
				if err := decodeInto(resp, &body); err != nil {
					t.Fatalf("decode: %v", err)
				}

				// The message has to name the field, or the caller is left
				// guessing which of three values was wrong.
				named := false

				for _, d := range body.Error.Details {
					if d.Field == tc.field {
						named = true
					}
				}

				if !named {
					t.Errorf("no detail names %q: %s", tc.field, resp)
				}
			})
		}
	})
}

// ── identity providers ───────────────────────────────────────────────────────

func TestProviderCreateUpdateDelete(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := asAdminWithProviders(t, db)

		created := f.request(t, http.MethodPost,
			api.APIPrefix+"/organization/identity-providers",
			map[string]any{
				"slug":         "okta",
				"name":         "Okta",
				"issuer":       "https://example.okta.com",
				"clientId":     "0oa1234567890",
				"clientSecret": "a-secret-nobody-should-read-back",
			})
		if created.status != http.StatusCreated {
			t.Fatalf("create = %d, want 201: %s", created.status, created)
		}

		// The secret is write-only. Reading it back over the API would make
		// every administrator's browser history a place it lives.
		if got := string(created.body); contains(got, "a-secret-nobody-should-read-back") {
			t.Error("the client secret was echoed back in the response")
		}

		provider := decodeJSON[struct {
			ID              string `json:"id"`
			Version         int64  `json:"version"`
			HasClientSecret bool   `json:"hasClientSecret"`
			Slug            string `json:"slug"`
		}](t, created)

		if !provider.HasClientSecret {
			t.Error("hasClientSecret is false after setting one")
		}

		listed := f.request(t, http.MethodGet,
			api.APIPrefix+"/organization/identity-providers", nil)
		if listed.status != http.StatusOK {
			t.Fatalf("list = %d, want 200: %s", listed.status, listed)
		}

		updated := f.request(t, http.MethodPut,
			api.APIPrefix+"/organization/identity-providers/"+provider.ID,
			map[string]any{
				"slug":     "okta",
				"name":     "Okta (production)",
				"issuer":   "https://example.okta.com",
				"clientId": "0oa1234567890",
				"version":  provider.Version,
			})
		if updated.status != http.StatusOK {
			t.Fatalf("update = %d, want 200: %s", updated.status, updated)
		}

		// A second update at the old version must lose. Without this an
		// administrator's change can silently overwrite another's.
		stale := f.request(t, http.MethodPut,
			api.APIPrefix+"/organization/identity-providers/"+provider.ID,
			map[string]any{
				"slug":     "okta",
				"name":     "Okta (stale)",
				"issuer":   "https://example.okta.com",
				"clientId": "0oa1234567890",
				"version":  provider.Version,
			})
		if stale.status == http.StatusOK {
			t.Error("a stale version was accepted; optimistic concurrency is not working")
		}

		deleted := f.request(t, http.MethodDelete,
			api.APIPrefix+"/organization/identity-providers/"+provider.ID, nil)
		if deleted.status != http.StatusNoContent {
			t.Fatalf("delete = %d, want 204: %s", deleted.status, deleted)
		}
	})
}

func TestProviderRejectsWhatItShould(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := asAdminWithProviders(t, db)

		cases := []struct {
			name  string
			body  map[string]any
			field string
		}{
			{
				name:  "no issuer",
				body:  map[string]any{"slug": "okta", "name": "Okta", "clientId": "x"},
				field: "issuer",
			},
			{
				// A plain-HTTP issuer would send the client secret and every
				// token over the wire in clear.
				name: "an issuer that is not HTTPS",
				body: map[string]any{
					"slug": "okta", "name": "Okta",
					"issuer": "http://example.okta.com", "clientId": "x",
				},
				field: "issuer",
			},
			{
				// The slug is a URL segment: /auth/oidc/{slug}/start.
				name: "a slug that is not URL-safe",
				body: map[string]any{
					"slug": "not a slug!", "name": "Okta",
					"issuer": "https://example.okta.com", "clientId": "x",
				},
				field: "slug",
			},
			{
				name: "a default role that does not exist",
				body: map[string]any{
					"slug": "okta", "name": "Okta",
					"issuer": "https://example.okta.com", "clientId": "x",
					"defaultRole": "sorcerer",
				},
				field: "defaultRole",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				resp := f.request(t, http.MethodPost,
					api.APIPrefix+"/organization/identity-providers", tc.body)

				if resp.status != http.StatusUnprocessableEntity {
					t.Fatalf("status = %d, want 422: %s", resp.status, resp)
				}

				var body api.ErrorResponse
				if err := decodeInto(resp, &body); err != nil {
					t.Fatalf("decode: %v", err)
				}

				named := false

				for _, d := range body.Error.Details {
					if d.Field == tc.field {
						named = true
					}
				}

				if !named {
					t.Errorf("no detail names %q: %s", tc.field, resp)
				}
			})
		}
	})
}

func TestProviderSlugMustBeUnique(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := asAdminWithProviders(t, db)

		body := map[string]any{
			"slug": "okta", "name": "Okta",
			"issuer": "https://example.okta.com", "clientId": "x",
		}

		if first := f.request(t, http.MethodPost,
			api.APIPrefix+"/organization/identity-providers", body); first.status != http.StatusCreated {
			t.Fatalf("first create = %d: %s", first.status, first)
		}

		// The slug is the URL segment that routes a login. Two providers
		// sharing one would make which login runs a matter of row order.
		second := f.request(t, http.MethodPost,
			api.APIPrefix+"/organization/identity-providers", body)
		if second.status == http.StatusCreated {
			t.Error("a duplicate slug was accepted")
		}
	})
}

func TestUpdatingAProviderThatDoesNotExist(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := asAdminWithProviders(t, db)

		resp := f.request(t, http.MethodPut,
			api.APIPrefix+"/organization/identity-providers/"+uuid.New().String(),
			map[string]any{
				"slug": "okta", "name": "Okta",
				"issuer": "https://example.okta.com", "clientId": "x", "version": 1,
			})

		if resp.status != http.StatusNotFound {
			t.Errorf("status = %d, want 404: %s", resp.status, resp)
		}
	})
}
