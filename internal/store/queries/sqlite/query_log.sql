-- The query log.
--
-- Placeholders are positional in both dialects and must appear in the same
-- order, and none is used twice - SQLite's `?` is positional and a repeat is a
-- second parameter. Query files are ASCII: sqlc's SQLite generator miscounts
-- byte offsets on multibyte characters and corrupts generation.

-- name: StartQueryLog :one
INSERT INTO query_log (id, org_id, connection_id, user_id, sql_text, started_at, owner)
VALUES (?, ?, ?, ?, ?, ?, ?)
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

-- name: QueryUsageByUser :many
-- The casts are not decoration. Without them sqlc cannot infer what SUM
-- returns here and emits `interface{}`, while the PostgreSQL copy's ::bigint
-- gives int64 -- and the two generated row structs stop being convertible,
-- which is the portability tax ADR-0003 accepted showing up again.
SELECT user_id,
       CAST(COUNT(*) AS INTEGER)                                          AS queries,
       CAST(COALESCE(SUM(duration_ms), 0) AS INTEGER)                     AS total_ms,
       CAST(COALESCE(SUM(rows_returned), 0) AS INTEGER)                   AS total_rows,
       CAST(COALESCE(SUM(bytes_estimated), 0) AS INTEGER)                 AS total_bytes,
       CAST(COALESCE(SUM(CASE WHEN state = 'failed' THEN 1 ELSE 0 END), 0) AS INTEGER) AS failures,
       CAST(COALESCE(SUM(CASE WHEN cache_status = 'hit' THEN 1 ELSE 0 END), 0) AS INTEGER) AS cache_hits
FROM query_log
WHERE org_id = ? AND started_at >= ?
GROUP BY user_id
ORDER BY total_ms DESC;

-- name: RequestQueryCancel :execrows
UPDATE query_log
SET cancel_requested_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    cancel_requested_by = ?
WHERE id = ? AND org_id = ? AND state = 'running';

-- name: ListCancelRequested :many
SELECT id FROM query_log
WHERE owner = ? AND state = 'running' AND cancel_requested_at IS NOT NULL;

-- name: HeartbeatOwnedQueries :execrows
UPDATE query_log
SET heartbeat_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE owner = ? AND state = 'running';
