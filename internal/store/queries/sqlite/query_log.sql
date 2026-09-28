-- The query log.
--
-- Placeholders are positional in both dialects and must appear in the same
-- order, and none is used twice - SQLite's `?` is positional and a repeat is a
-- second parameter. Query files are ASCII: sqlc's SQLite generator miscounts
-- byte offsets on multibyte characters and corrupts generation.

-- name: StartQueryLog :one
INSERT INTO query_log (id, org_id, connection_id, user_id, sql_text, started_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: FinishQueryLog :execrows
UPDATE query_log
SET state = ?,
    finished_at = ?,
    duration_ms = ?,
    rows_returned = ?,
    bytes_estimated = ?,
    truncated = ?,
    cache_status = ?,
    error_message = ?
WHERE id = ? AND org_id = ?;

-- name: ListQueryLog :many
SELECT * FROM query_log
WHERE org_id = ?
ORDER BY started_at DESC
LIMIT ?;

-- name: ListRunningQueries :many
SELECT * FROM query_log
WHERE org_id = ? AND state = 'running'
ORDER BY started_at;

-- name: GetQueryLogEntry :one
SELECT * FROM query_log
WHERE id = ? AND org_id = ?;
