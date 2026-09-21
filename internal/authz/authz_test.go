package authz_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

// --- fail closed ----------------------------------------------------------

// errStore fails every call, standing in for an unreachable backend.
type errStore struct{ err error }

func (s errStore) RelationsOn(context.Context, []authz.Subject, authz.Object) ([]authz.Relation, error) {
	return nil, s.err
}

func (s errStore) GroupsForUser(context.Context, string) ([]string, error) { return nil, s.err }

func (s errStore) ParentGroup(context.Context, string) (string, error) { return "", s.err }

// With the backend unavailable, every decision must be deny.
//
// This is the property ADR-0009 is most insistent about: an authorization
// system that fails open is worse than none, because it creates the false
// belief that access is controlled. An outage must look like "nobody can do
// anything", never like "everybody can do everything".
func TestUnavailableBackendDeniesEverything(t *testing.T) {
	t.Parallel()

	down := errors.New("connection refused")
	checker := authz.NewCache(authz.NewResolver(errStore{err: down}))

	req := authz.Request{
		Subject:    authz.User(uuid.Must(uuid.NewV7()).String()),
		Permission: authz.PermViewContent,
		Object:     authz.Object{Type: authz.TypeOrganization, ID: uuid.Must(uuid.NewV7()).String()},
	}

	t.Run("Check reports the failure rather than an answer", func(t *testing.T) {
		decision, err := checker.Check(context.Background(), req)
		if err == nil {
			t.Fatal("a failing store produced a decision")
		}

		if decision.Allowed {
			t.Error("a failing store produced an allow")
		}
	})

	// Enforce is what handlers use, and it is the shape that makes failing
	// closed the path of least resistance: there is no boolean to misread.
	t.Run("Enforce denies", func(t *testing.T) {
		err := authz.Enforce(context.Background(), checker, req)
		if err == nil {
			t.Fatal("Enforce allowed a request it could not decide")
		}

		if !errors.Is(err, authz.ErrUnavailable) {
			t.Errorf("error = %v, want ErrUnavailable", err)
		}
	})

	// The permission is checked for every permission, not only the one above:
	// a resolver that short-circuits on some path would be caught here.
	t.Run("every permission denies", func(t *testing.T) {
		for _, p := range authz.AllPermissions {
			r := req
			r.Permission = p

			if err := authz.Enforce(context.Background(), checker, r); err == nil {
				t.Errorf("permission %q was allowed with the backend down", p)
			}
		}
	})
}

// A nil checker is a wiring bug, and it must deny rather than panic or pass.
// An instance that booted without authorization configured must not serve as
// though everyone were an administrator.
func TestNilCheckerDenies(t *testing.T) {
	t.Parallel()

	err := authz.Enforce(context.Background(), nil, authz.Request{
		Subject:    authz.User("u"),
		Permission: authz.PermViewContent,
		Object:     authz.Object{Type: authz.TypeOrganization, ID: "o"},
	})

	if !errors.Is(err, authz.ErrUnavailable) {
		t.Errorf("error = %v, want ErrUnavailable", err)
	}
}

// An unregistered permission denies. A typo in a handler must lock a door
// rather than open one.
func TestUnknownPermissionDenies(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		repos, ctx, org := newOrg(t, db)
		checker, _ := authz.New(repos)

		user, err := repos.Users.Create(ctx, repo.CreateUser{
			Email: "ada@example.com", Name: "Ada", IsActive: true,
		})
		if err != nil {
			t.Fatalf("create user: %v", err)
		}

		// Make them an administrator, so a "yes" could only come from the
		// permission lookup rather than from having no grants.
		if gerr := repos.Roles.Grant(ctx, repo.GrantRole{
			SubjectType: "user", SubjectID: user.ID,
			Relation:   string(authz.RelationAdmin),
			ObjectType: "organization", ObjectID: org.ID,
		}); gerr != nil {
			t.Fatalf("grant: %v", gerr)
		}

		err = authz.Enforce(ctx, checker, authz.Request{
			Subject:    authz.User(user.ID.String()),
			Permission: "deploy_the_thing",
			Object:     authz.Object{Type: authz.TypeOrganization, ID: org.ID.String()},
		})

		if err == nil {
			t.Fatal("an unknown permission was allowed, for an administrator no less")
		}

		if !errors.Is(err, authz.ErrUnavailable) {
			t.Errorf("error = %v, want it to wrap ErrUnavailable", err)
		}
	})
}

// --- cycles ---------------------------------------------------------------

// cycleStore reports a group whose parent is itself.
type cycleStore struct{}

func (cycleStore) RelationsOn(context.Context, []authz.Subject, authz.Object) ([]authz.Relation, error) {
	return nil, nil
}

func (cycleStore) GroupsForUser(context.Context, string) ([]string, error) {
	return []string{"a"}, nil
}

// a's parent is b, b's parent is a.
func (cycleStore) ParentGroup(_ context.Context, groupID string) (string, error) {
	if groupID == "a" {
		return "b", nil
	}

	return "a", nil
}

// A cycle in group nesting must terminate.
//
// The API refuses to create one, but a bad import or a direct database edit is
// enough. An unbounded walk would pin a request thread until the client gave
// up, which is a denial of service dressed as a slow page.
//
// Terminating with an answer is the right outcome, not an error: with a and b
// as each other's parents, a member of either really is a member of both, and
// the visited set makes that the answer rather than an infinite loop.
func TestGroupCycleTerminates(t *testing.T) {
	t.Parallel()

	checker := authz.NewResolver(cycleStore{})

	done := make(chan error, 1)

	go func() {
		_, err := checker.Check(context.Background(), authz.Request{
			Subject:    authz.User("u"),
			Permission: authz.PermViewContent,
			Object:     authz.Object{Type: authz.TypeOrganization, ID: "o"},
		})
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a cyclic group graph failed rather than resolving: %v", err)
		}

	case <-time.After(5 * time.Second):
		t.Fatal("the resolver did not terminate on a cyclic group graph")
	}
}

// deepStore is an infinite chain: every group has a fresh, distinct parent.
//
// The visited set cannot stop this one, because nothing repeats. Only the
// depth cap can.
type deepStore struct{}

func (deepStore) RelationsOn(context.Context, []authz.Subject, authz.Object) ([]authz.Relation, error) {
	return nil, nil
}

func (deepStore) GroupsForUser(context.Context, string) ([]string, error) {
	return []string{"g0"}, nil
}

func (deepStore) ParentGroup(_ context.Context, groupID string) (string, error) {
	return groupID + "x", nil
}

// Nesting deeper than the cap denies rather than walking forever.
func TestExcessiveNestingDenies(t *testing.T) {
	t.Parallel()

	checker := authz.NewResolver(deepStore{})

	done := make(chan error, 1)

	go func() {
		_, err := checker.Check(context.Background(), authz.Request{
			Subject:    authz.User("u"),
			Permission: authz.PermViewContent,
			Object:     authz.Object{Type: authz.TypeOrganization, ID: "o"},
		})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("an unbounded group chain produced a decision")
		}

		if !errors.Is(err, authz.ErrUnavailable) {
			t.Errorf("error = %v, want ErrUnavailable", err)
		}

	case <-time.After(5 * time.Second):
		t.Fatal("the resolver did not terminate on an unbounded group chain")
	}
}

// --- caching --------------------------------------------------------------

// Permission changes must take effect within five seconds.
//
// Two mechanisms, tested separately, because each covers a case the other does
// not: invalidation handles a write made through this process, and the TTL
// handles one that was not.
func TestPermissionChangesTakeEffectQuickly(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		repos, ctx, org := newOrg(t, db)
		checker, cache := authz.New(repos)

		user, err := repos.Users.Create(ctx, repo.CreateUser{
			Email: "ada@example.com", Name: "Ada", IsActive: true,
		})
		if err != nil {
			t.Fatalf("create user: %v", err)
		}

		req := authz.Request{
			Subject:    authz.User(user.ID.String()),
			Permission: authz.PermManageUsers,
			Object:     authz.Object{Type: authz.TypeOrganization, ID: org.ID.String()},
		}

		// Denied, and now cached as denied.
		if derr := authz.Enforce(ctx, checker, req); derr == nil {
			t.Fatal("a user with no grants was allowed")
		}

		grant := repo.GrantRole{
			SubjectType: "user", SubjectID: user.ID,
			Relation:   string(authz.RelationAdmin),
			ObjectType: "organization", ObjectID: org.ID,
		}

		if gerr := repos.Roles.Grant(ctx, grant); gerr != nil {
			t.Fatalf("grant: %v", gerr)
		}

		t.Run("invalidation is immediate", func(t *testing.T) {
			cache.Invalidate()

			if aerr := authz.Enforce(ctx, checker, req); aerr != nil {
				t.Errorf("after granting and invalidating, still denied: %v", aerr)
			}
		})

		// Now revoke without invalidating, to prove the TTL alone bounds how
		// long a stale allow survives. A stale *allow* is the dangerous
		// direction, which is why this is the case worth testing.
		if rerr := repos.Roles.Revoke(ctx, grant); rerr != nil {
			t.Fatalf("revoke: %v", rerr)
		}

		t.Run("the TTL bounds a stale allow without invalidation", func(t *testing.T) {
			// Warm the cache with the now-stale allow.
			if aerr := authz.Enforce(ctx, checker, req); aerr != nil {
				t.Fatalf("expected the stale cached allow: %v", aerr)
			}

			// Advance past the TTL rather than sleeping through it.
			future := time.Now().Add(authz.CacheTTL + time.Second)
			cache.SetClock(func() time.Time { return future })

			if derr := authz.Enforce(ctx, checker, req); derr == nil {
				t.Error("a revoked grant was still allowed after the TTL expired")
			}

			if authz.CacheTTL >= 5*time.Second {
				t.Errorf("CacheTTL is %s; permission changes must take effect in under 5s",
					authz.CacheTTL)
			}
		})
	})
}

// countingStore records how often it is consulted.
type countingStore struct {
	calls int
}

func (s *countingStore) RelationsOn(context.Context, []authz.Subject, authz.Object) ([]authz.Relation, error) {
	s.calls++

	return []authz.Relation{authz.RelationAdmin}, nil
}

func (s *countingStore) GroupsForUser(context.Context, string) ([]string, error) { return nil, nil }

func (s *countingStore) ParentGroup(context.Context, string) (string, error) { return "", nil }

// The cache must actually cache, or the 10ms budget in ADR-0009 is a hope.
func TestCacheAvoidsRepeatedLookups(t *testing.T) {
	t.Parallel()

	counter := &countingStore{}
	cache := authz.NewCache(authz.NewResolver(counter))

	req := authz.Request{
		Subject:    authz.User("u"),
		Permission: authz.PermManageUsers,
		Object:     authz.Object{Type: authz.TypeOrganization, ID: "o"},
	}

	for range 10 {
		if _, err := cache.Check(context.Background(), req); err != nil {
			t.Fatalf("check: %v", err)
		}
	}

	if counter.calls != 1 {
		t.Errorf("the store was consulted %d times for 10 identical checks, want 1", counter.calls)
	}

	hits, misses, _ := cache.Stats()
	if hits != 9 || misses != 1 {
		t.Errorf("hits=%d misses=%d, want 9 and 1", hits, misses)
	}
}

// A failed check must not be cached: one database blip would otherwise become
// three seconds of denials for everyone.
func TestErrorsAreNotCached(t *testing.T) {
	t.Parallel()

	failing := &flakyStore{err: errors.New("down")}
	cache := authz.NewCache(authz.NewResolver(failing))

	req := authz.Request{
		Subject:    authz.User("u"),
		Permission: authz.PermViewContent,
		Object:     authz.Object{Type: authz.TypeOrganization, ID: "o"},
	}

	if _, err := cache.Check(context.Background(), req); err == nil {
		t.Fatal("expected a failure")
	}

	// Recover, and the very next check must reach the store.
	failing.err = nil

	decision, err := cache.Check(context.Background(), req)
	if err != nil {
		t.Fatalf("after recovery: %v", err)
	}

	if !decision.Allowed {
		t.Error("the recovered store's answer was not used; the failure was cached")
	}
}

// flakyStore fails until its error is cleared.
type flakyStore struct{ err error }

func (s *flakyStore) RelationsOn(context.Context, []authz.Subject, authz.Object) ([]authz.Relation, error) {
	if s.err != nil {
		return nil, s.err
	}

	return []authz.Relation{authz.RelationViewer}, nil
}

func (s *flakyStore) GroupsForUser(context.Context, string) ([]string, error) { return nil, s.err }

func (s *flakyStore) ParentGroup(context.Context, string) (string, error) { return "", s.err }

// --- the role table -------------------------------------------------------

// Every role must grant something and no role may grant a permission that does
// not exist. A typo in the table would otherwise be a permission nobody holds.
func TestRoleTableIsWellFormed(t *testing.T) {
	t.Parallel()

	known := make(map[authz.Permission]bool, len(authz.AllPermissions))
	for _, p := range authz.AllPermissions {
		known[p] = true
	}

	for _, role := range authz.BuiltinRoles {
		perms := authz.PermissionsFor(role)
		if len(perms) == 0 {
			t.Errorf("role %q grants nothing", role)
		}

		for _, p := range perms {
			if !known[p] {
				t.Errorf("role %q grants unregistered permission %q", role, p)
			}
		}
	}

	// Admin must be a superset of every other role, or "administrator" means
	// less than it says and an escalation path opens up where a lesser role can
	// do something an admin cannot.
	for _, role := range authz.BuiltinRoles {
		for _, p := range authz.PermissionsFor(role) {
			if !authz.RoleGrants(authz.RelationAdmin, p) {
				t.Errorf("role %q grants %q but admin does not", role, p)
			}
		}
	}

	// Raw SQL is a deliberate grant, not a side effect of being able to edit.
	if authz.RoleGrants(authz.RelationEditor, authz.PermNativeQuery) {
		t.Error("editor grants native_query; raw SQL bypasses semantic RLS and must be deliberate")
	}

	if authz.RoleGrants(authz.RelationViewer, authz.PermNativeQuery) {
		t.Error("viewer grants native_query")
	}
}
