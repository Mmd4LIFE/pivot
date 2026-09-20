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
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetUser :one
SELECT * FROM users
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE org_id = $1 AND email = $2 AND deleted_at IS NULL;

-- name: ListUsers :many
SELECT * FROM users
WHERE org_id = $1 AND deleted_at IS NULL
ORDER BY email
LIMIT sqlc.arg('limit')::bigint OFFSET sqlc.arg('offset')::bigint;

-- name: UpdateUser :one
UPDATE users
SET email = $1,
    name = $2,
    avatar_url = $3,
    is_active = $4,
    locale = $5,
    timezone = $6,
    updated_by = $7,
    updated_at = $8,
    version = version + 1
WHERE id = $9 AND org_id = $10 AND version = $11 AND deleted_at IS NULL
RETURNING *;

-- Password changes are separate from profile updates so a general "update the
-- user" call can never rewrite a credential by accident.
-- name: UpdateUserPassword :execrows
UPDATE users
SET password_hash = $1, updated_by = $2, updated_at = $3, version = version + 1
WHERE id = $4 AND org_id = $5 AND deleted_at IS NULL;

-- name: RecordUserLogin :execrows
UPDATE users
SET last_login_at = $1
WHERE id = $2 AND org_id = $3 AND deleted_at IS NULL;

-- name: SoftDeleteUser :execrows
UPDATE users
SET deleted_at = $1, updated_by = $2, version = version + 1
WHERE id = $3 AND org_id = $4 AND deleted_at IS NULL;

-- name: CountUsers :one
SELECT count(*) FROM users WHERE org_id = $1 AND deleted_at IS NULL;
