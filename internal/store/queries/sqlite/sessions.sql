-- Session lookup by token is deliberately NOT org-scoped: resolving a session
-- is how the tenant is discovered in the first place, so it cannot require
-- knowing the tenant already. Every other query here is scoped as usual.

-- name: CreateSession :one
INSERT INTO sessions (id, org_id, user_id, token_hash, expires_at,
                      absolute_expires_at, ip, user_agent)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetSessionByTokenHash :one
SELECT * FROM sessions WHERE token_hash = ?;

-- Sliding idle expiry. The absolute cap is never touched, so an active session
-- still ends when it reaches it.
-- name: TouchSession :execrows
UPDATE sessions
SET last_seen_at = ?, expires_at = ?
WHERE id = ? AND revoked_at IS NULL;

-- name: RevokeSession :execrows
UPDATE sessions
SET revoked_at = ?
WHERE id = ? AND org_id = ? AND revoked_at IS NULL;

-- Revoking every session for a user is what a password change and a
-- compromise response both need.
-- name: RevokeUserSessions :execrows
UPDATE sessions
SET revoked_at = ?
WHERE user_id = ? AND org_id = ? AND revoked_at IS NULL;

-- name: ListUserSessions :many
SELECT * FROM sessions
WHERE user_id = ? AND org_id = ? AND revoked_at IS NULL
ORDER BY last_seen_at DESC;

-- Expired and revoked rows are swept rather than left to accumulate.
-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions
WHERE absolute_expires_at < ? OR (revoked_at IS NOT NULL AND revoked_at < ?);
