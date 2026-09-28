package query

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

/*
A fixture with the real authorization graph behind it.

The headline property of Part 21 is that two callers with different policy sets
cannot share a cached result, and proving that against a stub checker would
prove only that the stub returns what it was told to. These tests create real
users, grant them real roles, and let the real resolver work out what they
hold, so the fingerprint is computed from the same facts production computes it
from.
*/
type cacheFixture struct {
	*fixture

	cache   *Cache
	checker authz.Checker

	analyst uuid.UUID
	admin   uuid.UUID
}

// newCacheFixture builds a pipeline with the cache on, over a real graph
// containing one analyst and one administrator.
func newCacheFixture(t *testing.T, opts ...CacheOption) *cacheFixture {
	t.Helper()

	return newCacheFixtureWith(t, newFixture(t, fixtureOptions{}), opts...)
}

// newCacheFixtureOn is newCacheFixture over a metadata database the caller
// chose.
func newCacheFixtureOn(t *testing.T, db *store.DB, opts ...CacheOption) *cacheFixture {
	t.Helper()

	return newCacheFixtureWith(t, newFixtureOn(t, db, fixtureOptions{}), opts...)
}

func newCacheFixtureWith(t *testing.T, base *fixture, opts ...CacheOption) *cacheFixture {
	t.Helper()

	checker, _ := authz.New(base.repos)

	granter, ok := checker.(authz.Granter)
	if !ok {
		t.Fatal("the production checker does not resolve grants")
	}

	cache := NewCache(opts...)

	base.executor = NewExecutor(base.repos, checker,
		withOpener(base.countingOpener()), WithCache(cache, granter))

	f := &cacheFixture{fixture: base, cache: cache, checker: checker}

	f.analyst = f.seedUser(t, "analyst@example.com", authz.RelationAnalyst)
	f.admin = f.seedUser(t, "admin@example.com", authz.RelationAdmin)

	return f
}

// seedUser creates a user and grants them a role on the organization.
func (f *cacheFixture) seedUser(t *testing.T, email string, relation authz.Relation) uuid.UUID {
	t.Helper()

	user, err := f.repos.Users.Create(f.ctx, repo.CreateUser{
		Email: email, Name: email, IsActive: true,
	})
	if err != nil {
		t.Fatalf("create %s: %v", email, err)
	}

	if err := f.repos.Roles.Grant(f.ctx, repo.GrantRole{
		SubjectType: "user", SubjectID: user.ID,
		Relation:   string(relation),
		ObjectType: string(authz.TypeOrganization), ObjectID: f.orgID,
	}); err != nil {
		t.Fatalf("grant %s to %s: %v", relation, email, err)
	}

	return user.ID
}

// as returns a context scoped to the organization and acting as one of the
// seeded users.
func (f *cacheFixture) as(t *testing.T, userID uuid.UUID) context.Context {
	t.Helper()

	return tenant.WithScope(t.Context(),
		tenant.MustNewScope(f.orgID, uuid.NullUUID{UUID: userID, Valid: true}))
}
