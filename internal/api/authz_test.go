package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/auth"
	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// The endpoint half of the declarative permission harness.
//
// internal/authz asserts what the checker decides. This asserts that the HTTP
// surface actually asks it — a different failure, and the more likely one: a
// model can be perfectly correct while a route forgets to be gated, and no
// amount of unit testing the resolver would notice.
//
// Both halves read the same file, so the specification has one home.

// modelSpecPath is the shared specification.
const modelSpecPath = "../authz/testdata/model_v1.yaml"

type endpointSpec struct {
	Fixture struct {
		Users  []string `yaml:"users"`
		Groups []struct {
			Name   string `yaml:"name"`
			Parent string `yaml:"parent"`
		} `yaml:"groups"`
		Members map[string][]string `yaml:"members"`
		Grants  []struct {
			Subject  string `yaml:"subject"`
			Relation string `yaml:"relation"`
			Object   string `yaml:"object"`
		} `yaml:"grants"`
	} `yaml:"fixture"`

	Endpoints []struct {
		As     string `yaml:"as"`
		Method string `yaml:"method"`
		Path   string `yaml:"path"`
		Status int    `yaml:"status"`
	} `yaml:"endpoints"`
}

func loadEndpointSpec(t *testing.T) endpointSpec {
	t.Helper()

	data, err := os.ReadFile(filepath.Clean(modelSpecPath))
	if err != nil {
		t.Fatalf("read %s: %v", modelSpecPath, err)
	}

	var s endpointSpec
	if err := yaml.Unmarshal(data, &s); err != nil {
		t.Fatalf("parse %s: %v", modelSpecPath, err)
	}

	if len(s.Endpoints) == 0 {
		t.Fatalf("%s declares no endpoint assertions", modelSpecPath)
	}

	return s
}

// authzFixture is a running server with the specification's world in it.
type authzFixture struct {
	*authFixture

	users  map[string]uuid.UUID
	groups map[string]uuid.UUID

	// clients are logged-in sessions, one per user, reused across assertions.
	// Logging in per row would be both slow and self-defeating: the login
	// limiter from Part 6-b would throttle the table, and a test that breaks
	// when someone adds an assertion is worse than no test.
	clients map[string]*authFixture
}

// newAuthzFixture builds the specification's fixture behind a real server with
// the authorization surface wired up.
func newAuthzFixture(t *testing.T, db *store.DB, s endpointSpec) *authzFixture {
	t.Helper()

	// The login limiter is exercised by its own test; here it would only
	// throttle the fixture's setup.
	base := newAuthFixture(t, db, func(c *api.RouterConfig) {
		c.AuthLimit = api.RateLimit{Rate: 1000, Burst: 1000}
	})

	f := &authzFixture{
		authFixture: base,
		users:       make(map[string]uuid.UUID, len(s.Fixture.Users)),
		groups:      make(map[string]uuid.UUID, len(s.Fixture.Groups)),
		clients:     make(map[string]*authFixture, len(s.Fixture.Users)),
	}

	hash, err := auth.HashPassword(fixturePassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	for _, name := range s.Fixture.Users {
		email := name + "@example.com"

		// The base fixture already seeded one user, and the specification
		// names it too. Reuse rather than collide — a rename here would make
		// the two halves of the harness describe different worlds.
		if existing, gerr := base.repos.Users.GetByEmail(base.ctx, email); gerr == nil {
			f.users[name] = existing.ID

			continue
		}

		user, cerr := base.repos.Users.Create(base.ctx, repo.CreateUser{
			Email: email, Name: name, PasswordHash: hash, IsActive: true,
		})
		if cerr != nil {
			t.Fatalf("create user %s: %v", name, cerr)
		}

		f.users[name] = user.ID
	}

	for _, g := range s.Fixture.Groups {
		group, cerr := base.repos.Groups.Create(base.ctx, repo.CreateGroup{Name: g.Name})
		if cerr != nil {
			t.Fatalf("create group %s: %v", g.Name, cerr)
		}

		f.groups[g.Name] = group.ID
	}

	for _, g := range s.Fixture.Groups {
		if g.Parent == "" {
			continue
		}

		current, gerr := base.repos.Groups.Get(base.ctx, f.groups[g.Name])
		if gerr != nil {
			t.Fatalf("read group %s: %v", g.Name, gerr)
		}

		if _, uerr := base.repos.Groups.Update(base.ctx, repo.UpdateGroup{
			ID:            current.ID,
			Name:          current.Name,
			Description:   current.Description,
			ParentGroupID: uuid.NullUUID{UUID: f.groups[g.Parent], Valid: true},
			ExternalID:    current.ExternalID.String,
			Version:       current.Version,
		}); uerr != nil {
			t.Fatalf("set parent of %s: %v", g.Name, uerr)
		}
	}

	for groupName, members := range s.Fixture.Members {
		for _, member := range members {
			if aerr := base.repos.Groups.AddMember(base.ctx,
				f.groups[groupName], f.users[member]); aerr != nil {
				t.Fatalf("add %s to %s: %v", member, groupName, aerr)
			}
		}
	}

	for _, g := range s.Fixture.Grants {
		subject, perr := authz.ParseSubject(g.Subject)
		if perr != nil {
			t.Fatalf("grant subject: %v", perr)
		}

		id, ok := f.users[subject.ID]
		if !ok {
			id, ok = f.groups[subject.ID]
			if !ok {
				t.Fatalf("grant names unknown subject %q", subject.ID)
			}
		}

		if gerr := base.repos.Roles.Grant(base.ctx, repo.GrantRole{
			SubjectType:     string(subject.Type),
			SubjectID:       id,
			SubjectRelation: string(subject.Relation),
			Relation:        g.Relation,
			ObjectType:      string(authz.TypeOrganization),
			ObjectID:        base.org.ID,
		}); gerr != nil {
			t.Fatalf("grant %s: %v", g.Subject, gerr)
		}
	}

	return f
}

// clientFor logs in as a named user and returns a client carrying its cookie.
func (f *authzFixture) clientFor(t *testing.T, symbolic string) *authFixture {
	t.Helper()

	name := strings.TrimPrefix(symbolic, "user:")

	if existing, ok := f.clients[name]; ok {
		return existing
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}

	client := &authFixture{
		server: f.server, handler: f.handler, client: &http.Client{Jar: jar},
		repos: f.repos, org: f.org, ctx: f.ctx,
	}

	if resp := client.login(t, name+"@example.com", fixturePassword); resp.status != http.StatusOK {
		t.Fatalf("login as %s failed: %s", name, resp)
	}

	f.clients[name] = client

	return client
}

// TestEndpointPermissions runs the specification's endpoint table.
func TestEndpointPermissions(t *testing.T) {
	t.Parallel()

	s := loadEndpointSpec(t)

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthzFixture(t, db, s)

		for _, e := range s.Endpoints {
			name := fmt.Sprintf("%s/%s_%s/%d", e.As, e.Method, strings.ReplaceAll(e.Path, "/", "_"), e.Status)

			t.Run(name, func(t *testing.T) {
				client := f.clientFor(t, e.As)

				resp := client.request(t, e.Method, api.APIPrefix+e.Path, nil)
				if resp.status != e.Status {
					t.Errorf("%s %s as %s = %d, want %d\n  body: %s",
						e.Method, e.Path, e.As, resp.status, e.Status, resp)
				}

				// A 403 must carry the forbidden code, so a client can tell
				// "not permitted" from every other refusal without parsing
				// prose.
				if e.Status == http.StatusForbidden {
					if code := errorCode(t, resp); code != api.CodeForbidden {
						t.Errorf("code = %q, want %q", code, api.CodeForbidden)
					}
				}
			})
		}
	})
}

// Every gated endpoint must return 503 when the checker cannot decide, and
// none may return 200.
//
// This is the fail-closed property at the HTTP boundary rather than in the
// authz package: an outage must look like "nobody can do anything", and must
// be distinguishable from "you may not", or an operator debugging it is told
// the wrong thing.
func TestGatedEndpointsFailClosed(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		// The routes stay registered; only the checker is broken.
		f := newAuthFixture(t, db, func(c *api.RouterConfig) {
			c.Checker = brokenChecker{}
		})

		if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
			t.Fatalf("login: %s", resp)
		}

		gated := []struct {
			method, path string
		}{
			{http.MethodGet, "/organization/role-assignments"},
			{http.MethodPost, "/organization/role-assignments"},
			{http.MethodDelete, "/admin/sessions/" + uuid.Nil.String()},
		}

		for _, g := range gated {
			resp := f.request(t, g.method, api.APIPrefix+g.path, nil)

			if resp.status == http.StatusOK {
				t.Errorf("%s %s returned 200 with the checker down", g.method, g.path)
			}

			if resp.status != http.StatusServiceUnavailable {
				t.Errorf("%s %s = %d, want 503 with the checker down: %s",
					g.method, g.path, resp.status, resp)

				continue
			}

			if code := errorCode(t, resp); code != api.CodeUnavailable {
				t.Errorf("%s %s code = %q, want %q", g.method, g.path, code, api.CodeUnavailable)
			}
		}
	})
}

// brokenChecker stands in for an unreachable authorization backend.
type brokenChecker struct{}

func (brokenChecker) Check(context.Context, authz.Request) (authz.Decision, error) {
	return authz.Decision{}, fmt.Errorf("%w: backend down", authz.ErrUnavailable)
}

func (brokenChecker) Explain(context.Context, authz.Request) (authz.Explanation, error) {
	return authz.Explanation{}, fmt.Errorf("%w: backend down", authz.ErrUnavailable)
}

// With no checker configured at all, a gated route must still refuse. An
// instance that booted without authorization wired up must not serve as though
// everyone were an administrator.
func TestGatedEndpointsDenyWithNoCheckerConfigured(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db, func(c *api.RouterConfig) {
			c.Checker = nil
		})

		if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
			t.Fatalf("login: %s", resp)
		}

		resp := f.request(t, http.MethodGet, api.APIPrefix+"/organization/role-assignments", nil)
		if resp.status == http.StatusOK {
			t.Error("a gated endpoint served 200 with no checker configured")
		}
	})
}

// The last administrator cannot be removed over the API.
//
// An organization with nobody who can grant roles is recoverable only from the
// command line, and the people most likely to reach that state are the least
// likely to have shell access.
func TestLastAdministratorCannotBeRemoved(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)

		if gerr := f.repos.Roles.Grant(f.ctx, repo.GrantRole{
			SubjectType: "user", SubjectID: f.user.ID,
			Relation:   string(authz.RelationAdmin),
			ObjectType: string(authz.TypeOrganization), ObjectID: f.org.ID,
		}); gerr != nil {
			t.Fatalf("grant: %v", gerr)
		}

		if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
			t.Fatalf("login: %s", resp)
		}

		path := fmt.Sprintf("%s/organization/role-assignments/user/%s/admin",
			api.APIPrefix, f.user.ID)

		resp := f.request(t, http.MethodDelete, path, nil)
		if resp.status != http.StatusUnprocessableEntity {
			t.Fatalf("removing the last admin = %d, want 422: %s", resp.status, resp)
		}

		// And they are still an administrator afterwards, which is the part
		// that actually matters.
		listed := f.request(t, http.MethodGet, api.APIPrefix+"/organization/role-assignments", nil)
		if listed.status != http.StatusOK {
			t.Errorf("the admin lost access: %d %s", listed.status, listed)
		}
	})
}

// A grant naming a subject from another organization must be refused.
//
// The columns are polymorphic so no foreign key can enforce it; the repository
// checks instead. Without it the row stores fine and dangles.
func TestGrantRefusesAForeignSubject(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)

		other, err := f.repos.System().CreateOrganization(context.Background(),
			repo.CreateOrganization{Name: "Globex", Slug: "globex"})
		if err != nil {
			t.Fatalf("create org: %v", err)
		}

		otherCtx := tenant.WithScope(context.Background(),
			tenant.MustNewScope(other.ID, uuid.NullUUID{}))

		stranger, err := f.repos.Users.Create(otherCtx, repo.CreateUser{
			Email: "stranger@example.com", Name: "Stranger", IsActive: true,
		})
		if err != nil {
			t.Fatalf("create stranger: %v", err)
		}

		gerr := f.repos.Roles.Grant(f.ctx, repo.GrantRole{
			SubjectType: "user", SubjectID: stranger.ID,
			Relation:   string(authz.RelationAdmin),
			ObjectType: string(authz.TypeOrganization), ObjectID: f.org.ID,
		})

		if gerr == nil {
			t.Fatal("granted a role to a user from another organization")
		}
	})
}

// Deleting a user removes their grants, so a reused identifier cannot inherit
// a stranger's permissions.
func TestDeletingAUserRemovesItsGrants(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)

		if gerr := f.repos.Roles.Grant(f.ctx, repo.GrantRole{
			SubjectType: "user", SubjectID: f.user.ID,
			Relation:   string(authz.RelationAdmin),
			ObjectType: string(authz.TypeOrganization), ObjectID: f.org.ID,
		}); gerr != nil {
			t.Fatalf("grant: %v", gerr)
		}

		if derr := f.repos.Users.SoftDelete(f.ctx, f.user.ID); derr != nil {
			t.Fatalf("delete: %v", derr)
		}

		rows, err := f.repos.Roles.GrantsForSubject(f.ctx, "user", f.user.ID)
		if err != nil {
			t.Fatalf("list grants: %v", err)
		}

		if len(rows) != 0 {
			t.Errorf("a deleted user kept %d grant(s)", len(rows))
		}
	})
}
