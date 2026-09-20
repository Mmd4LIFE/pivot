-- Attributes feed row-level security in Phase 4, so their provenance matters:
-- an IdP sync must not silently overwrite a deliberate manual assignment, and
-- vice versa. The source column records which wrote a value, and the upsert
-- below carries it forward.
--
-- updated_at is written through the INSERT column list and read back with
-- the excluded row, rather than bound directly in the DO UPDATE clause. sqlc
-- does not recognize a placeholder there: it emits the SQL with the placeholder
-- intact but omits the argument, so the call fails at runtime with an argument
-- count mismatch. Keeping every placeholder inside VALUES avoids that.

-- name: UpsertUserAttribute :one
INSERT INTO user_attributes (id, org_id, user_id, key, value, source, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (user_id, key) DO UPDATE
SET value = excluded.value,
    source = excluded.source,
    updated_at = excluded.updated_at
RETURNING *;

-- name: GetUserAttribute :one
SELECT * FROM user_attributes
WHERE org_id = ? AND user_id = ? AND key = ?;

-- name: ListUserAttributes :many
SELECT * FROM user_attributes
WHERE org_id = ? AND user_id = ?
ORDER BY key;

-- name: DeleteUserAttribute :execrows
DELETE FROM user_attributes
WHERE org_id = ? AND user_id = ? AND key = ?;

-- Replacing an IdP's attributes must not disturb manually assigned ones, so
-- deletion is scoped by source.
-- name: DeleteUserAttributesBySource :execrows
DELETE FROM user_attributes
WHERE org_id = ? AND user_id = ? AND source = ?;
