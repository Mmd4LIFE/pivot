-- Session lookup by token is deliberately NOT org-scoped: resolving a session
-- is how the tenant is discovered in the first place, so it cannot require
-- knowing the tenant already. Every other query here is scoped as usual.

-- name: CreateSession :one
INSERT INTO sessions (id, org_id, user_id, token_hash, expires_at,
                      absolute_expires_at, ip, user_agent)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetSessionByTokenHash :one
SELECT * FROM sessions WHERE token_hash = $1;

-- Sliding idle expiry. The absolute cap is never touched, so an active session
-- still ends when it reaches it.
-- name: TouchSession :execrows
UPDATE sessions
SET last_seen_at = $1, expires_at = $2
WHERE id = $3 AND revoked_at IS NULL;

-- name: RevokeSession :execrows
UPDATE sessions
SET revoked_at = $1
WHERE id = $2 AND org_id = $3 AND revoked_at IS NULL;

-- Revoking every session for a user is what a password change and a
-- compromise response both need.
-- name: RevokeUserSessions :execrows
UPDATE sessions
SET revoked_at = $1
WHERE user_id = $2 AND org_id = $3 AND revoked_at IS NULL;

-- name: ListUserSessions :many
SELECT * FROM sessions
WHERE user_id = $1 AND org_id = $2 AND revoked_at IS NULL
ORDER BY last_seen_at DESC;

-- Expired and revoked rows are swept rather than left to accumulate.
-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions
WHERE absolute_expires_at < $1 OR (revoked_at IS NOT NULL AND revoked_at < $2);
