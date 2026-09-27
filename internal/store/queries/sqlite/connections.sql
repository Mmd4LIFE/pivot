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
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetConnection :one
SELECT * FROM connections
WHERE id = ? AND org_id = ? AND deleted_at IS NULL;

-- name: GetConnectionBySlug :one
SELECT * FROM connections
WHERE org_id = ? AND slug = ? AND deleted_at IS NULL;

-- name: ListConnections :many
SELECT * FROM connections
WHERE org_id = ? AND deleted_at IS NULL
ORDER BY name;

-- A versioned update, like every other mutable row here: a stale version
-- matches nothing, which the repository reports as a conflict rather than as a
-- missing row.
-- name: UpdateConnection :one
UPDATE connections
SET slug = ?, name = ?, description = ?,
    host = ?, port = ?, database = ?, username = ?, password = ?,
    ssl_mode = ?, options = ?,
    max_open_conns = ?, max_rows = ?, query_timeout_seconds = ?,
    is_enabled = ?, updated_by = ?, updated_at = ?,
    version = version + 1
WHERE id = ? AND org_id = ? AND version = ? AND deleted_at IS NULL
RETURNING *;

-- Recording a test result is deliberately not a versioned update. It is not a
-- change somebody made, it is an observation about the world, and making it
-- bump the version would mean a background health check invalidates the form
-- an administrator has open.
-- name: RecordConnectionTest :execrows
UPDATE connections
SET last_tested_at = ?, last_test_ok = ?, last_test_error = ?
WHERE id = ? AND org_id = ? AND deleted_at IS NULL;

-- name: SoftDeleteConnection :execrows
UPDATE connections
SET deleted_at = ?, updated_by = ?
WHERE id = ? AND org_id = ? AND deleted_at IS NULL;

-- name: CountConnections :one
SELECT COUNT(*) FROM connections
WHERE org_id = ? AND deleted_at IS NULL;
