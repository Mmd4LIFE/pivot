package repo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

// Login attempt tracking is unscoped for the same reason session resolution
// is: it runs before authentication succeeds, so there is no scope yet. The
// organization is an explicit parameter, supplied by whoever resolved which
// tenant the login is against.

// NormalizeEmail lowercases and trims an address.
//
// Applied consistently on lookup and on failure recording, so that "Ada@x.com"
// and "ada@x.com" share one lockout counter. Without it, varying the case is
// enough to reset the count — which makes the lockout decorative.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// GetLoginAttempt returns the failure record for an address, or ErrNotFound.
func (r *SystemRepo) GetLoginAttempt(
	ctx context.Context, orgID uuid.UUID, email string,
) (model.LoginAttempt, error) {
	attempt, err := r.q.GetLoginAttempt(ctx, model.GetLoginAttemptParams{
		OrgID: orgID, Email: NormalizeEmail(email),
	})
	if err != nil {
		return model.LoginAttempt{}, translate(err)
	}

	return attempt, nil
}

// RecordFailedLogin increments the failure count and returns the new state.
//
// The row is created on first failure whether or not the account exists —
// locking out only real accounts would let an attacker enumerate users by
// observing which addresses start refusing.
func (r *SystemRepo) RecordFailedLogin(
	ctx context.Context, orgID uuid.UUID, email string, now time.Time,
) (model.LoginAttempt, error) {
	attempt, err := r.q.RecordFailedLogin(ctx, model.RecordFailedLoginParams{
		ID:            newID(),
		OrgID:         orgID,
		Email:         NormalizeEmail(email),
		FirstFailedAt: dbtypes.NewTime(now),
		LastFailedAt:  dbtypes.NewTime(now),
		LockedUntil:   dbtypes.NullTime{},
	})
	if err != nil {
		return model.LoginAttempt{}, translate(err)
	}

	return attempt, nil
}

// LockLogin applies a lockout window to an address.
func (r *SystemRepo) LockLogin(
	ctx context.Context, orgID uuid.UUID, email string, until time.Time,
) error {
	n, err := r.q.SetLoginLock(ctx, model.SetLoginLockParams{
		LockedUntil: dbtypes.NewNullTime(until),
		OrgID:       orgID,
		Email:       NormalizeEmail(email),
	})

	return affectedOrNotFound(n, err)
}

// ClearLoginAttempts removes the failure record after a successful login.
//
// Zero rows is not an error: a user who has never failed has no record, which
// is the common case.
func (r *SystemRepo) ClearLoginAttempts(ctx context.Context, orgID uuid.UUID, email string) error {
	_, err := r.q.ClearLoginAttempts(ctx, model.ClearLoginAttemptsParams{
		OrgID: orgID, Email: NormalizeEmail(email),
	})
	if err != nil {
		return translate(err)
	}

	return nil
}

// SweepLoginAttempts removes stale failure records.
//
// Rows for addresses that never existed would otherwise accumulate for every
// address a dictionary attack tries, so this runs alongside the session sweep.
// Records still under an active lock are kept.
func (r *SystemRepo) SweepLoginAttempts(
	ctx context.Context, now time.Time, retain time.Duration,
) (int64, error) {
	n, err := r.q.DeleteStaleLoginAttempts(ctx, model.DeleteStaleLoginAttemptsParams{
		LastFailedAt: dbtypes.NewTime(now.Add(-retain)),
		LockedUntil:  dbtypes.NewNullTime(now),
	})
	if err != nil {
		return 0, translate(err)
	}

	return n, nil
}

// LockedUntil reports the active lock expiry for an address, if any.
//
// A missing record means no failures, which is not an error condition.
func (r *SystemRepo) LockedUntil(
	ctx context.Context, orgID uuid.UUID, email string, now time.Time,
) (time.Time, bool, error) {
	attempt, err := r.GetLoginAttempt(ctx, orgID, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return time.Time{}, false, nil
		}

		return time.Time{}, false, err
	}

	if !attempt.LockedUntil.Valid {
		return time.Time{}, false, nil
	}

	until := attempt.LockedUntil.Time.Time
	if !until.After(now) {
		return time.Time{}, false, nil
	}

	return until, true, nil
}
