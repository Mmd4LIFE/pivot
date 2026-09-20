package repo_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// This file is the reason Part 4 exists. Phase 0 calls tenant isolation the
// most important item in the phase, and these tests are what turn that from an
// intention into a property.

// twoTenants provisions two organizations with deliberately IDENTICAL user
// data, so that any leak shows up as a wrong row rather than an empty result
// that might have been correct by accident.
type twoTenants struct {
	repos *repo.Repositories

	orgA, orgB     model.Organization
	userA, userB   model.User
	scopeA, scopeB tenant.Scope
	ctxA, ctxB     context.Context
}

const sharedEmail = "identical@example.com"

func setupTwoTenants(t *testing.T, db *store.DB) twoTenants {
	t.Helper()

	repos := repo.New(db)
	sys := repos.System()
	ctx := context.Background()

	orgA, err := sys.CreateOrganization(ctx, repo.CreateOrganization{Name: "Alpha", Slug: "alpha"})
	if err != nil {
		t.Fatalf("create org A: %v", err)
	}

	orgB, err := sys.CreateOrganization(ctx, repo.CreateOrganization{Name: "Beta", Slug: "beta"})
	if err != nil {
		t.Fatalf("create org B: %v", err)
	}

	scopeA := tenant.MustNewScope(orgA.ID, uuid.NullUUID{})
	scopeB := tenant.MustNewScope(orgB.ID, uuid.NullUUID{})
	ctxA := tenant.WithScope(ctx, scopeA)
	ctxB := tenant.WithScope(ctx, scopeB)

	// Same email in both tenants. The unique index is (org_id, email), so this
	// is legal — and it is exactly the case a leak would expose.
	userA, err := repos.Users.Create(ctxA, repo.CreateUser{
		Email: sharedEmail, Name: "Same Name", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}

	userB, err := repos.Users.Create(ctxB, repo.CreateUser{
		Email: sharedEmail, Name: "Same Name", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create user B: %v", err)
	}

	return twoTenants{
		repos: repos,
		orgA:  orgA, orgB: orgB,
		userA: userA, userB: userB,
		scopeA: scopeA, scopeB: scopeB,
		ctxA: ctxA, ctxB: ctxB,
	}
}

// The headline requirement: every read scoped to A returns zero B rows.
func TestCrossTenantReadsReturnNothing(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		tt := setupTwoTenants(t, db)

		t.Run("get by id", func(t *testing.T) {
			// A's scope, B's user id.
			_, err := tt.repos.Users.Get(tt.ctxA, tt.userB.ID)
			if !errors.Is(err, repo.ErrNotFound) {
				t.Errorf("Get(B's user) under A = %v, want ErrNotFound", err)
			}

			// The same id under its own scope must work, proving the id is
			// valid and the denial above came from scoping.
			if _, err := tt.repos.Users.Get(tt.ctxB, tt.userB.ID); err != nil {
				t.Errorf("Get(B's user) under B = %v, want success", err)
			}
		})

		t.Run("get by email", func(t *testing.T) {
			// Both tenants have this email. Each must see only its own.
			got, err := tt.repos.Users.GetByEmail(tt.ctxA, sharedEmail)
			if err != nil {
				t.Fatalf("GetByEmail under A: %v", err)
			}

			if got.ID != tt.userA.ID {
				t.Errorf("GetByEmail under A returned user %v, want A's %v", got.ID, tt.userA.ID)
			}

			if got.OrgID != tt.orgA.ID {
				t.Errorf("GetByEmail under A returned a row from org %v", got.OrgID)
			}
		})

		t.Run("list", func(t *testing.T) {
			users, err := tt.repos.Users.List(tt.ctxA, 100, 0)
			if err != nil {
				t.Fatalf("List under A: %v", err)
			}

			if len(users) != 1 {
				t.Fatalf("List under A returned %d users, want 1", len(users))
			}

			for _, u := range users {
				if u.OrgID != tt.orgA.ID {
					t.Errorf("List under A leaked a row from org %v", u.OrgID)
				}
			}
		})

		t.Run("count", func(t *testing.T) {
			n, err := tt.repos.Users.Count(tt.ctxA)
			if err != nil {
				t.Fatalf("Count under A: %v", err)
			}

			if n != 1 {
				t.Errorf("Count under A = %d, want 1 (B's user must not be counted)", n)
			}
		})

		t.Run("current organization", func(t *testing.T) {
			org, err := tt.repos.Organizations.Current(tt.ctxA)
			if err != nil {
				t.Fatalf("Current under A: %v", err)
			}

			if org.ID != tt.orgA.ID {
				t.Errorf("Current under A = %v, want A's org %v", org.ID, tt.orgA.ID)
			}
		})
	})
}

// Writes must be scoped too. A leak here is worse than a read leak: it
// corrupts another tenant's data rather than merely exposing it.
func TestCrossTenantWritesAreRejected(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		tt := setupTwoTenants(t, db)

		t.Run("update", func(t *testing.T) {
			_, err := tt.repos.Users.Update(tt.ctxA, repo.UpdateUser{
				ID: tt.userB.ID, Email: "hijacked@example.com", Name: "Hijacked",
				IsActive: true, Locale: "en", Timezone: "UTC", Version: tt.userB.Version,
			})
			if err == nil {
				t.Error("updating B's user under A succeeded")
			}

			// B's user must be untouched.
			after, gerr := tt.repos.Users.Get(tt.ctxB, tt.userB.ID)
			if gerr != nil {
				t.Fatalf("re-read B's user: %v", gerr)
			}

			if after.Email != sharedEmail {
				t.Errorf("B's user email = %q after a cross-tenant update attempt", after.Email)
			}
		})

		t.Run("set password", func(t *testing.T) {
			err := tt.repos.Users.SetPassword(tt.ctxA, tt.userB.ID, "$argon2id$fake")
			if !errors.Is(err, repo.ErrNotFound) {
				t.Errorf("SetPassword on B's user under A = %v, want ErrNotFound", err)
			}

			after, gerr := tt.repos.Users.Get(tt.ctxB, tt.userB.ID)
			if gerr != nil {
				t.Fatalf("re-read B's user: %v", gerr)
			}

			if after.PasswordHash.Valid {
				t.Error("B's password was set by a call scoped to A")
			}
		})

		t.Run("soft delete", func(t *testing.T) {
			err := tt.repos.Users.SoftDelete(tt.ctxA, tt.userB.ID)
			if !errors.Is(err, repo.ErrNotFound) {
				t.Errorf("SoftDelete on B's user under A = %v, want ErrNotFound", err)
			}

			if _, gerr := tt.repos.Users.Get(tt.ctxB, tt.userB.ID); gerr != nil {
				t.Errorf("B's user is gone after a cross-tenant delete attempt: %v", gerr)
			}
		})

		t.Run("record login", func(t *testing.T) {
			err := tt.repos.Users.RecordLogin(tt.ctxA, tt.userB.ID)
			if !errors.Is(err, repo.ErrNotFound) {
				t.Errorf("RecordLogin on B's user under A = %v, want ErrNotFound", err)
			}
		})
	})
}

// Every exported repository method must refuse a context with no scope.
//
// This is the "unscoped query fails" requirement, and it is written with
// reflection on purpose: a hand-written list would go stale the moment someone
// adds a method, which is exactly when the check matters most.
func TestEveryMethodRefusesAnUnscopedContext(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		repos := repo.New(db)
		bare := context.Background() // deliberately no scope

		targets := map[string]any{
			"Users":         repos.Users,
			"Organizations": repos.Organizations,
		}

		var checked int

		for name, target := range targets {
			v := reflect.ValueOf(target)
			typ := v.Type()

			for i := range typ.NumMethod() {
				method := typ.Method(i)

				// Only methods whose first argument is a context are scoped
				// data access; anything else is not this test's business.
				if method.Type.NumIn() < 2 || method.Type.In(1) != reflect.TypeOf((*context.Context)(nil)).Elem() {
					continue
				}

				checked++

				args := make([]reflect.Value, method.Type.NumIn())
				args[0] = v
				args[1] = reflect.ValueOf(bare)

				// Zero values for everything else; the scope check must fire
				// before any argument is looked at.
				for j := 2; j < method.Type.NumIn(); j++ {
					args[j] = reflect.Zero(method.Type.In(j))
				}

				out := method.Func.Call(args)

				last := out[len(out)-1]
				if last.IsNil() {
					t.Errorf("%s.%s succeeded with no tenant in context; "+
						"every method must fail closed", name, method.Name)

					continue
				}

				err, ok := last.Interface().(error)
				if !ok {
					t.Errorf("%s.%s last return is not an error", name, method.Name)

					continue
				}

				if !errors.Is(err, tenant.ErrNoScope) {
					t.Errorf("%s.%s with no scope = %v, want tenant.ErrNoScope",
						name, method.Name, err)
				}
			}
		}

		// Guard against the test silently checking nothing — a refactor that
		// renamed the repositories would otherwise make this pass vacuously.
		if checked < 10 {
			t.Errorf("only %d methods were checked; the reflection walk is not finding them", checked)
		}
	})
}

// An invalid scope (no organization) must be refused just like a missing one,
// so that a zero-value Scope cannot become "organization uuid.Nil".
func TestInvalidScopeIsRefused(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		repos := repo.New(db)

		// tenant.Scope's fields are unexported, so a zero value is the only
		// way to construct an invalid one — which is the point.
		ctx := tenant.WithScope(context.Background(), tenant.Scope{})

		if _, err := repos.Users.List(ctx, 10, 0); !errors.Is(err, tenant.ErrInvalidScope) {
			t.Errorf("List with a zero-value scope = %v, want tenant.ErrInvalidScope", err)
		}
	})
}

// A scope cannot be built for a nil organization in the first place.
func TestNewScopeRejectsNilOrganization(t *testing.T) {
	t.Parallel()

	if _, err := tenant.NewScope(uuid.Nil, uuid.NullUUID{}); !errors.Is(err, tenant.ErrInvalidScope) {
		t.Errorf("NewScope(uuid.Nil) = %v, want ErrInvalidScope", err)
	}
}
