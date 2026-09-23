package repo

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

const entitySession = "session"

// SessionRepo manages sessions within the caller's organization.
//
// Creating and resolving a session are NOT here: both happen before a tenant
// is known — resolving a session is how the tenant is discovered — so they
// live on [SystemRepo] where their unscoped nature is visible at the call
// site.
type SessionRepo struct {
	base
}

// List returns a user's active sessions, newest activity first.
func (r *SessionRepo) List(ctx context.Context, userID uuid.UUID) ([]model.Session, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	sessions, err := r.q.ListUserSessions(ctx, model.ListUserSessionsParams{
		UserID: userID, OrgID: s.OrgID(),
	})
	if err != nil {
		return nil, translate(err)
	}

	return sessions, nil
}

// RevokeOwn ends one of a specific user's own sessions.
//
// This is what the "my sessions" endpoint needs, and it is separate from
// [SessionRepo.Revoke] for a reason worth stating: Revoke is scoped to the
// organization alone, so any member could end any other member's session with
// it. That is correct for an administrator and wrong for a user managing their
// own devices, and the difference is enforced in the WHERE clause rather than
// by an if statement in a handler.
//
// A session that belongs to someone else returns [ErrNotFound], the same as
// one that does not exist — the caller has no business learning the difference.
func (r *SessionRepo) RevokeOwn(ctx context.Context, userID, sessionID uuid.UUID) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	n, rerr := r.q.RevokeSessionForUser(ctx, model.RevokeSessionForUserParams{
		RevokedAt: dbtypes.NewNullTime(r.now().Time),
		ID:        sessionID,
		UserID:    userID,
		OrgID:     s.OrgID(),
	})

	if verr := affectedOrNotFound(n, rerr); verr != nil {
		return verr
	}

	r.emit(ctx, ChangeDeleted, entitySession, sessionID, s.OrgID(), s.ActorID())

	return nil
}

// Revoke ends one session immediately, anywhere in the caller's organization.
func (r *SessionRepo) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	n, rerr := r.q.RevokeSession(ctx, model.RevokeSessionParams{
		RevokedAt: dbtypes.NewNullTime(r.now().Time),
		ID:        sessionID,
		OrgID:     s.OrgID(),
	})

	if verr := affectedOrNotFound(n, rerr); verr != nil {
		return verr
	}

	r.emit(ctx, ChangeDeleted, entitySession, sessionID, s.OrgID(), s.ActorID())

	return nil
}

// RevokeAllForUser ends every session a user holds.
//
// This is what a password change and a compromise response both need. It
// returns the count rather than an error on zero: a user with no active
// sessions is a normal state, not a missing row.
func (r *SessionRepo) RevokeAllForUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return 0, err
	}

	n, rerr := r.q.RevokeUserSessions(ctx, model.RevokeUserSessionsParams{
		RevokedAt: dbtypes.NewNullTime(r.now().Time),
		UserID:    userID,
		OrgID:     s.OrgID(),
	})
	if rerr != nil {
		return 0, translate(rerr)
	}

	if n > 0 {
		r.emit(ctx, ChangeDeleted, entitySession, userID, s.OrgID(), s.ActorID())
	}

	return n, nil
}

// RevokeAllForUserExcept ends every session a user holds except one.
//
// What changing your own password needs. Revoking everything would sign the
// user out of the device they are standing at, which makes the safe action
// feel like a punishment and teaches people not to take it -- and the session
// being kept belongs to somebody who has just proved they know the current
// password.
func (r *SessionRepo) RevokeAllForUserExcept(
	ctx context.Context, userID, keepSessionID uuid.UUID,
) (int64, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return 0, err
	}

	n, rerr := r.q.RevokeOtherUserSessions(ctx, model.RevokeOtherUserSessionsParams{
		RevokedAt: dbtypes.NewNullTime(r.now().Time),
		UserID:    userID,
		OrgID:     s.OrgID(),
		ID:        keepSessionID,
	})
	if rerr != nil {
		return 0, translate(rerr)
	}

	if n > 0 {
		r.emit(ctx, ChangeDeleted, entitySession, userID, s.OrgID(), s.ActorID())
	}

	return n, nil
}

// --- unscoped session operations ------------------------------------------

// CreateSession records a new session.
//
// Unscoped because it runs during login, before a scope exists. The org and
// user come from the authenticated credentials rather than from a request
// context, which is why they are explicit parameters here and nowhere else.
type CreateSession struct {
	OrgID     uuid.UUID
	UserID    uuid.UUID
	TokenHash string

	// ExpiresAt is the idle timeout; AbsoluteExpiresAt is the hard cap.
	ExpiresAt         time.Time
	AbsoluteExpiresAt time.Time

	IP        string
	UserAgent string
}

// CreateSession stores a session and returns it.
func (r *SystemRepo) CreateSession(ctx context.Context, in CreateSession) (model.Session, error) {
	session, err := r.q.CreateSession(ctx, model.CreateSessionParams{
		ID:                newID(),
		OrgID:             in.OrgID,
		UserID:            in.UserID,
		TokenHash:         in.TokenHash,
		ExpiresAt:         dbtypes.NewTime(in.ExpiresAt),
		AbsoluteExpiresAt: dbtypes.NewTime(in.AbsoluteExpiresAt),
		IP:                in.IP,
		UserAgent:         in.UserAgent,
	})
	if err != nil {
		return model.Session{}, translate(err)
	}

	return session, nil
}

// GetSessionByTokenHash resolves a session token.
//
// Unscoped by necessity: this is the call that establishes which tenant a
// request belongs to, so it cannot require knowing the tenant first. It
// returns the row as stored — expiry and revocation are evaluated by the
// caller in [internal/auth], so that the policy lives in one place rather than
// being half in SQL and half in Go.
func (r *SystemRepo) GetSessionByTokenHash(ctx context.Context, tokenHash string) (model.Session, error) {
	session, err := r.q.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return model.Session{}, translate(err)
	}

	return session, nil
}

// TouchSession pushes the idle expiry forward and records activity.
//
// The absolute cap is untouched, so an actively used session still ends when
// it reaches it. Zero rows affected means the session was revoked between
// resolution and touch, which the caller treats as a failed authentication.
func (r *SystemRepo) TouchSession(
	ctx context.Context, sessionID uuid.UUID, now, expiresAt time.Time,
) error {
	n, err := r.q.TouchSession(ctx, model.TouchSessionParams{
		LastSeenAt: dbtypes.NewTime(now),
		ExpiresAt:  dbtypes.NewTime(expiresAt),
		ID:         sessionID,
	})

	return affectedOrNotFound(n, err)
}

// SweepExpired deletes sessions past their absolute expiry, and revoked
// sessions older than the retention window.
//
// Revoked rows are kept briefly rather than deleted immediately: an
// investigation into "who was logged in when" needs them, and Phase 4's audit
// work will read them.
func (r *SystemRepo) SweepExpired(ctx context.Context, now time.Time, retain time.Duration) (int64, error) {
	n, err := r.q.DeleteExpiredSessions(ctx, model.DeleteExpiredSessionsParams{
		AbsoluteExpiresAt: dbtypes.NewTime(now),
		RevokedAt:         dbtypes.NewNullTime(now.Add(-retain)),
	})
	if err != nil {
		return 0, translate(err)
	}

	return n, nil
}
