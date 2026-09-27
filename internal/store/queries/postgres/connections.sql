-- Connections: the data sources an organization can query.
--
-- Placeholders are positional in both dialects and must appear in the same
-- order, because the two generated parameter structs are converted directly
-- into one another and a differing field order breaks the conversion - the
-- lesson of Part 4-a. Query files are ASCII: sqlc's SQLite generator miscounts
-- byte offsets on multibyte characters and corrupts generation.

-- name: CreateConnection :one
INSERT INTO connections (
    id, org_id, slug, name, kind, description,
    host, port, database, username, password, ssl_mode, options,
    max_open_conns, max_rows, query_timeout_seconds, is_enabled,
    created_by, updated_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
RETURNING *;

-- name: GetConnection :one
SELECT * FROM connections
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

-- name: GetConnectionBySlug :one
SELECT * FROM connections
WHERE org_id = $1 AND slug = $2 AND deleted_at IS NULL;

-- name: ListConnections :many
SELECT * FROM connections
WHERE org_id = $1 AND deleted_at IS NULL
ORDER BY name;

-- A versioned update, like every other mutable row here: a stale version
-- matches nothing, which the repository reports as a conflict rather than as a
-- missing row.
-- name: UpdateConnection :one
UPDATE connections
SET slug = $1, name = $2, description = $3,
    host = $4, port = $5, database = $6, username = $7, password = $8,
    ssl_mode = $9, options = $10,
    max_open_conns = $11, max_rows = $12, query_timeout_seconds = $13,
    is_enabled = $14, updated_by = $15, updated_at = $16,
    version = version + 1
WHERE id = $17 AND org_id = $18 AND version = $19 AND deleted_at IS NULL
RETURNING *;

-- Recording a test result is deliberately not a versioned update. It is not a
-- change somebody made, it is an observation about the world, and making it
-- bump the version would mean a background health check invalidates the form
-- an administrator has open.
-- name: RecordConnectionTest :execrows
UPDATE connections
SET last_tested_at = $1, last_test_ok = $2, last_test_error = $3
WHERE id = $4 AND org_id = $5 AND deleted_at IS NULL;

-- name: SoftDeleteConnection :execrows
UPDATE connections
SET deleted_at = $1, updated_by = $2
WHERE id = $3 AND org_id = $4 AND deleted_at IS NULL;

-- name: CountConnections :one
SELECT COUNT(*) FROM connections
WHERE org_id = $1 AND deleted_at IS NULL;
