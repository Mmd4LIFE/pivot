package repo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

// openSession creates a session for a tenant's user.
func openSession(t *testing.T, tt twoTenants, orgIdx int) model.Session {
	t.Helper()

	org, user := tt.orgA.ID, tt.userA.ID
	if orgIdx == 1 {
		org, user = tt.orgB.ID, tt.userB.ID
	}

	now := time.Now()

	s, err := tt.repos.System().CreateSession(context.Background(), repo.CreateSession{
		OrgID:  org,
		UserID: user,
		// Full UUIDs: v7 is time-sortable, so rows created milliseconds
		// apart share their leading characters and a truncated key collides.
		TokenHash:         "hash-" + org.String() + "-" + user.String(),
		ExpiresAt:         now.Add(time.Hour),
		AbsoluteExpiresAt: now.Add(24 * time.Hour),
		IP:                "127.0.0.1",
		UserAgent:         "test",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	return s
}

// Sessions are a scoped repository like any other, so the same cross-tenant
// proof applies — a session ID from one tenant must be unusable from another.
func TestCrossTenantSessionAccessIsDenied(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		tt := setupTwoTenants(t, db)

		sessionA := openSession(t, tt, 0)
		sessionB := openSession(t, tt, 1)

		t.Run("list is scoped", func(t *testing.T) {
			sessions, err := tt.repos.Sessions.List(tt.ctxA, tt.userA.ID)
			if err != nil {
				t.Fatalf("List under A: %v", err)
			}

			if len(sessions) != 1 || sessions[0].ID != sessionA.ID {
				t.Errorf("List under A = %+v, want just A's session", sessions)
			}

			// B's user, asked from A's scope.
			leaked, err := tt.repos.Sessions.List(tt.ctxA, tt.userB.ID)
			if err != nil {
				t.Fatalf("List under A for B's user: %v", err)
			}

			if len(leaked) != 0 {
				t.Errorf("List under A returned %d of B's sessions", len(leaked))
			}
		})

		t.Run("revoke is scoped", func(t *testing.T) {
			if err := tt.repos.Sessions.Revoke(tt.ctxA, sessionB.ID); !errors.Is(err, repo.ErrNotFound) {
				t.Errorf("revoking B's session under A = %v, want ErrNotFound", err)
			}

			// B's session must still be live.
			sessions, err := tt.repos.Sessions.List(tt.ctxB, tt.userB.ID)
			if err != nil {
				t.Fatalf("List under B: %v", err)
			}

			if len(sessions) != 1 {
				t.Error("B's session was revoked by a call scoped to A")
			}
		})

		t.Run("revoke all is scoped", func(t *testing.T) {
			n, err := tt.repos.Sessions.RevokeAllForUser(tt.ctxA, tt.userB.ID)
			if err != nil {
				t.Fatalf("RevokeAllForUser: %v", err)
			}

			if n != 0 {
				t.Errorf("revoked %d of B's sessions from A's scope", n)
			}
		})
	})
}

// A session cannot name one organization while pointing at another's user —
// the composite foreign key makes that impossible, as it does for every other
// child table since migration 00002.
func TestSessionCannotCrossTenants(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		tt := setupTwoTenants(t, db)
		now := time.Now()

		_, err := tt.repos.System().CreateSession(context.Background(), repo.CreateSession{
			OrgID:             tt.orgA.ID,  // A's organization
			UserID:            tt.userB.ID, // B's user
			TokenHash:         "cross-tenant-attempt",
			ExpiresAt:         now.Add(time.Hour),
			AbsoluteExpiresAt: now.Add(24 * time.Hour),
		})
		if err == nil {
			t.Error("a session was created naming one org and another's user")
		}
	})
}
