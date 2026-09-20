-- Every query is scoped by org_id, including lookups by primary key. The id is
-- already unique, so the extra predicate buys nothing on its own -- it means a
-- caller holding an id from one tenant cannot read a row from another, even by
-- mistake. Part 4 makes that scoping structural.
--
-- No parameter is reused and none are numbered, in either dialect. Reuse works
-- in PostgreSQL but not SQLite, and a differing parameter count would make the
-- two generated params structs diverge.
--
-- Never write a literal question mark in a comment in these files: sqlc counts
-- placeholders inside comments, which shifts its substitution offsets and
-- corrupts the generated SQL into tokens like RETURNINid.

-- name: CreateUser :one
INSERT INTO users (id, org_id, email, name, avatar_url, password_hash,
                   is_active, locale, timezone, created_by, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetUser :one
SELECT * FROM users
WHERE id = ? AND org_id = ? AND deleted_at IS NULL;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE org_id = ? AND email = ? AND deleted_at IS NULL;

-- name: ListUsers :many
SELECT * FROM users
WHERE org_id = ? AND deleted_at IS NULL
ORDER BY email
LIMIT ? OFFSET ?;

-- name: UpdateUser :one
UPDATE users
SET email = ?,
    name = ?,
    avatar_url = ?,
    is_active = ?,
    locale = ?,
    timezone = ?,
    updated_by = ?,
    updated_at = ?,
    version = version + 1
WHERE id = ? AND org_id = ? AND version = ? AND deleted_at IS NULL
RETURNING *;

-- Password changes are separate from profile updates so a general "update the
-- user" call can never rewrite a credential by accident.
-- name: UpdateUserPassword :execrows
UPDATE users
SET password_hash = ?, updated_by = ?, updated_at = ?, version = version + 1
WHERE id = ? AND org_id = ? AND deleted_at IS NULL;

-- name: RecordUserLogin :execrows
UPDATE users
SET last_login_at = ?
WHERE id = ? AND org_id = ? AND deleted_at IS NULL;

-- name: SoftDeleteUser :execrows
UPDATE users
SET deleted_at = ?, updated_by = ?, version = version + 1
WHERE id = ? AND org_id = ? AND deleted_at IS NULL;

-- name: CountUsers :one
SELECT count(*) FROM users WHERE org_id = ? AND deleted_at IS NULL;
