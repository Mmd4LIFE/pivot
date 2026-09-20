package repo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

// seedGroup creates a group in a tenant and returns it.
func seedGroup(ctx context.Context, t *testing.T, tt twoTenants, name string) model.Group {
	t.Helper()

	g, err := tt.repos.Groups.Create(ctx, repo.CreateGroup{Name: name, Description: name})
	if err != nil {
		t.Fatalf("create group %q: %v", name, err)
	}

	return g
}

// The same cross-tenant proof as users, extended to groups — deliberately
// identical in shape, because a new repository should not need a new kind of
// test to be trusted.
func TestCrossTenantGroupAccessIsDenied(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		tt := setupTwoTenants(t, db)

		// Identical group names in both tenants.
		groupA := seedGroup(tt.ctxA, t, tt, "Engineering")
		groupB := seedGroup(tt.ctxB, t, tt, "Engineering")

		t.Run("get", func(t *testing.T) {
			if _, err := tt.repos.Groups.Get(tt.ctxA, groupB.ID); !errors.Is(err, repo.ErrNotFound) {
				t.Errorf("Get(B's group) under A = %v, want ErrNotFound", err)
			}

			if _, err := tt.repos.Groups.Get(tt.ctxB, groupB.ID); err != nil {
				t.Errorf("Get(B's group) under B = %v, want success", err)
			}
		})

		t.Run("get by name returns own tenant", func(t *testing.T) {
			got, err := tt.repos.Groups.GetByName(tt.ctxA, "Engineering")
			if err != nil {
				t.Fatalf("GetByName under A: %v", err)
			}

			if got.ID != groupA.ID {
				t.Errorf("GetByName under A returned %v, want A's group %v", got.ID, groupA.ID)
			}
		})

		t.Run("list", func(t *testing.T) {
			groups, err := tt.repos.Groups.List(tt.ctxA, 100, 0)
			if err != nil {
				t.Fatalf("List under A: %v", err)
			}

			if len(groups) != 1 {
				t.Fatalf("List under A returned %d groups, want 1", len(groups))
			}

			if groups[0].OrgID != tt.orgA.ID {
				t.Errorf("List under A leaked a group from org %v", groups[0].OrgID)
			}
		})

		t.Run("soft delete", func(t *testing.T) {
			if err := tt.repos.Groups.SoftDelete(tt.ctxA, groupB.ID); !errors.Is(err, repo.ErrNotFound) {
				t.Errorf("SoftDelete(B's group) under A = %v, want ErrNotFound", err)
			}

			if _, err := tt.repos.Groups.Get(tt.ctxB, groupB.ID); err != nil {
				t.Errorf("B's group is gone after a cross-tenant delete attempt: %v", err)
			}
		})
	})
}

// Membership must be scoped too: adding another tenant's user to your group
// would be a cross-tenant write dressed up as a local one.
func TestGroupMembership(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		tt := setupTwoTenants(t, db)
		group := seedGroup(tt.ctxA, t, tt, "Platform")

		t.Run("add and list", func(t *testing.T) {
			if err := tt.repos.Groups.AddMember(tt.ctxA, group.ID, tt.userA.ID); err != nil {
				t.Fatalf("AddMember: %v", err)
			}

			members, err := tt.repos.Groups.ListMembers(tt.ctxA, group.ID)
			if err != nil {
				t.Fatalf("ListMembers: %v", err)
			}

			if len(members) != 1 || members[0].ID != tt.userA.ID {
				t.Errorf("members = %+v, want just A's user", members)
			}
		})

		t.Run("add is idempotent", func(t *testing.T) {
			// A SCIM sync replaying the same state must not error.
			if err := tt.repos.Groups.AddMember(tt.ctxA, group.ID, tt.userA.ID); err != nil {
				t.Errorf("second AddMember = %v, want success (membership is a set)", err)
			}

			members, err := tt.repos.Groups.ListMembers(tt.ctxA, group.ID)
			if err != nil {
				t.Fatalf("ListMembers: %v", err)
			}

			if len(members) != 1 {
				t.Errorf("member count = %d after a duplicate add, want 1", len(members))
			}
		})

		t.Run("is member", func(t *testing.T) {
			ok, err := tt.repos.Groups.IsMember(tt.ctxA, group.ID, tt.userA.ID)
			if err != nil {
				t.Fatalf("IsMember: %v", err)
			}

			if !ok {
				t.Error("IsMember = false for a member")
			}

			// B's user is not in A's group, and asking from B's scope about A's
			// group must not report membership either.
			ok, err = tt.repos.Groups.IsMember(tt.ctxB, group.ID, tt.userB.ID)
			if err != nil {
				t.Fatalf("IsMember under B: %v", err)
			}

			if ok {
				t.Error("IsMember = true across tenants")
			}
		})

		t.Run("list for user", func(t *testing.T) {
			groups, err := tt.repos.Groups.ListForUser(tt.ctxA, tt.userA.ID)
			if err != nil {
				t.Fatalf("ListForUser: %v", err)
			}

			if len(groups) != 1 || groups[0].ID != group.ID {
				t.Errorf("groups = %+v, want just the one", groups)
			}

			// B's scope must see nothing for A's user.
			groups, err = tt.repos.Groups.ListForUser(tt.ctxB, tt.userA.ID)
			if err != nil {
				t.Fatalf("ListForUser under B: %v", err)
			}

			if len(groups) != 0 {
				t.Errorf("ListForUser under B returned %d groups for A's user", len(groups))
			}
		})

		t.Run("cross-tenant member add is rejected", func(t *testing.T) {
			// A's group, B's user. The foreign keys are org-scoped, so the
			// database must refuse this even though both IDs exist.
			err := tt.repos.Groups.AddMember(tt.ctxA, group.ID, tt.userB.ID)
			if err == nil {
				t.Error("adding another tenant's user to a group succeeded")
			}
		})

		t.Run("remove", func(t *testing.T) {
			if err := tt.repos.Groups.RemoveMember(tt.ctxA, group.ID, tt.userA.ID); err != nil {
				t.Fatalf("RemoveMember: %v", err)
			}

			ok, err := tt.repos.Groups.IsMember(tt.ctxA, group.ID, tt.userA.ID)
			if err != nil {
				t.Fatalf("IsMember: %v", err)
			}

			if ok {
				t.Error("IsMember = true after removal")
			}

			// Removing again is a not-found, not a silent success.
			if err := tt.repos.Groups.RemoveMember(tt.ctxA, group.ID, tt.userA.ID); !errors.Is(err, repo.ErrNotFound) {
				t.Errorf("second RemoveMember = %v, want ErrNotFound", err)
			}
		})
	})
}

func TestGroupNesting(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		tt := setupTwoTenants(t, db)

		parent := seedGroup(tt.ctxA, t, tt, "Engineering")

		child, err := tt.repos.Groups.Create(tt.ctxA, repo.CreateGroup{
			Name:          "Platform",
			ParentGroupID: uuid.NullUUID{UUID: parent.ID, Valid: true},
		})
		if err != nil {
			t.Fatalf("create child: %v", err)
		}

		children, err := tt.repos.Groups.ListChildren(tt.ctxA, parent.ID)
		if err != nil {
			t.Fatalf("ListChildren: %v", err)
		}

		if len(children) != 1 || children[0].ID != child.ID {
			t.Errorf("children = %+v, want just the child", children)
		}

		// A group that is its own parent would make the Phase 7 tree walk loop.
		if _, err := tt.repos.Groups.Update(tt.ctxA, repo.UpdateGroup{
			ID:            child.ID,
			Name:          "Platform",
			ParentGroupID: uuid.NullUUID{UUID: child.ID, Valid: true},
			Version:       child.Version,
		}); err == nil {
			t.Error("a group was allowed to become its own parent")
		}
	})
}

// Attribute provenance is what makes an identity-provider sync safe to re-run.
func TestUserAttributeProvenance(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		tt := setupTwoTenants(t, db)
		attrs := tt.repos.UserAttributes

		// An administrator sets one by hand; the IdP sets two.
		if _, err := attrs.Set(tt.ctxA, repo.SetAttribute{
			UserID: tt.userA.ID, Key: "clearance", Value: "high", Source: model.SourceManual,
		}); err != nil {
			t.Fatalf("set manual attribute: %v", err)
		}

		for k, v := range map[string]string{"region": "EU", "department": "eng"} {
			if _, err := attrs.Set(tt.ctxA, repo.SetAttribute{
				UserID: tt.userA.ID, Key: k, Value: v, Source: model.SourceOIDC,
			}); err != nil {
				t.Fatalf("set oidc attribute %q: %v", k, err)
			}
		}

		all, err := attrs.Map(tt.ctxA, tt.userA.ID)
		if err != nil {
			t.Fatalf("Map: %v", err)
		}

		if len(all) != 3 || all["region"] != "EU" || all["clearance"] != "high" {
			t.Fatalf("attributes = %+v, want three with those values", all)
		}

		// Re-running the sync clears only what the IdP owns.
		n, err := attrs.DeleteBySource(tt.ctxA, tt.userA.ID, model.SourceOIDC)
		if err != nil {
			t.Fatalf("DeleteBySource: %v", err)
		}

		if n != 2 {
			t.Errorf("DeleteBySource removed %d, want 2", n)
		}

		remaining, err := attrs.Map(tt.ctxA, tt.userA.ID)
		if err != nil {
			t.Fatalf("Map: %v", err)
		}

		if len(remaining) != 1 || remaining["clearance"] != "high" {
			t.Errorf("after an IdP resync the manual attribute must survive; got %+v", remaining)
		}

		// Deleting a source that owns nothing is a normal state, not an error.
		if _, err := attrs.DeleteBySource(tt.ctxA, tt.userA.ID, model.SourceSCIM); err != nil {
			t.Errorf("DeleteBySource for an unused source = %v, want success", err)
		}
	})
}

func TestUserAttributeUpsertReplacesInPlace(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		tt := setupTwoTenants(t, db)
		attrs := tt.repos.UserAttributes

		if _, err := attrs.Set(tt.ctxA, repo.SetAttribute{
			UserID: tt.userA.ID, Key: "region", Value: "EU",
		}); err != nil {
			t.Fatalf("first set: %v", err)
		}

		if _, err := attrs.Set(tt.ctxA, repo.SetAttribute{
			UserID: tt.userA.ID, Key: "region", Value: "US", Source: model.SourceSCIM,
		}); err != nil {
			t.Fatalf("second set: %v", err)
		}

		got, err := attrs.Get(tt.ctxA, tt.userA.ID, "region")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		if got.Value != "US" || got.Source != model.SourceSCIM {
			t.Errorf("attribute = (%q, %q), want (US, scim)", got.Value, got.Source)
		}

		list, err := attrs.List(tt.ctxA, tt.userA.ID)
		if err != nil {
			t.Fatalf("List: %v", err)
		}

		if len(list) != 1 {
			t.Errorf("attribute count = %d after upsert, want 1", len(list))
		}
	})
}

func TestUserAttributeRejectsUnknownSource(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		tt := setupTwoTenants(t, db)

		_, err := tt.repos.UserAttributes.Set(tt.ctxA, repo.SetAttribute{
			UserID: tt.userA.ID, Key: "region", Value: "EU", Source: "telepathy",
		})
		if err == nil {
			t.Error("an invalid attribute source was accepted")
		}
	})
}

func TestCrossTenantAttributeAccessIsDenied(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		tt := setupTwoTenants(t, db)

		if _, err := tt.repos.UserAttributes.Set(tt.ctxB, repo.SetAttribute{
			UserID: tt.userB.ID, Key: "region", Value: "APAC",
		}); err != nil {
			t.Fatalf("set B's attribute: %v", err)
		}

		// A's scope must not see it, by key or by list.
		if _, err := tt.repos.UserAttributes.Get(tt.ctxA, tt.userB.ID, "region"); !errors.Is(err, repo.ErrNotFound) {
			t.Errorf("Get(B's attribute) under A = %v, want ErrNotFound", err)
		}

		list, err := tt.repos.UserAttributes.List(tt.ctxA, tt.userB.ID)
		if err != nil {
			t.Fatalf("List under A: %v", err)
		}

		if len(list) != 0 {
			t.Errorf("List under A returned %d of B's attributes", len(list))
		}
	})
}
