-- Failed login tracking, keyed by the ATTEMPTED email rather than a user id:
-- a row must exist even when the account does not, or the lockout itself
-- becomes a user-enumeration oracle.

-- name: GetLoginAttempt :one
SELECT * FROM login_attempts WHERE org_id = ? AND email = ?;

-- name: RecordFailedLogin :one
INSERT INTO login_attempts (id, org_id, email, failed_count, first_failed_at, last_failed_at, locked_until)
VALUES (?, ?, ?, 1, ?, ?, ?)
ON CONFLICT (org_id, email) DO UPDATE
SET failed_count   = login_attempts.failed_count + 1,
    last_failed_at = excluded.last_failed_at,
    locked_until   = excluded.locked_until
RETURNING *;

-- Applied after a lockout threshold is crossed, once the new count is known.
-- name: SetLoginLock :execrows
UPDATE login_attempts
SET locked_until = ?
WHERE org_id = ? AND email = ?;

-- A successful login clears the record entirely.
-- name: ClearLoginAttempts :execrows
DELETE FROM login_attempts WHERE org_id = ? AND email = ?;

-- Swept alongside expired sessions, so a dictionary attack cannot grow this
-- table without bound.
-- name: DeleteStaleLoginAttempts :execrows
DELETE FROM login_attempts
WHERE last_failed_at < ? AND (locked_until IS NULL OR locked_until < ?);
