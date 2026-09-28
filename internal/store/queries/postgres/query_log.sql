-- The query log.
--
-- Placeholders are positional in both dialects and must appear in the same
-- order, and none is used twice - SQLite's `?` is positional and a repeat is a
-- second parameter. Query files are ASCII: sqlc's SQLite generator miscounts
-- byte offsets on multibyte characters and corrupts generation.

-- name: StartQueryLog :one
INSERT INTO query_log (id, org_id, connection_id, user_id, sql_text, started_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: FinishQueryLog :execrows
UPDATE query_log
SET state = $1,
    finished_at = $2,
    duration_ms = $3,
    rows_returned = $4,
    bytes_estimated = $5,
    truncated = $6,
    error_message = $7
WHERE id = $8 AND org_id = $9;

-- name: ListQueryLog :many
SELECT * FROM query_log
WHERE org_id = $1
ORDER BY started_at DESC
LIMIT sqlc.arg('limit')::bigint;

-- name: ListRunningQueries :many
SELECT * FROM query_log
WHERE org_id = $1 AND state = 'running'
ORDER BY started_at;

-- name: GetQueryLogEntry :one
SELECT * FROM query_log
WHERE id = $1 AND org_id = $2;
