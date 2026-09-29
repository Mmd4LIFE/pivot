-- The query log.
--
-- Placeholders are positional in both dialects and must appear in the same
-- order, and none is used twice - SQLite's `?` is positional and a repeat is a
-- second parameter. Query files are ASCII: sqlc's SQLite generator miscounts
-- byte offsets on multibyte characters and corrupts generation.

-- name: StartQueryLog :one
INSERT INTO query_log (id, org_id, connection_id, user_id, sql_text, started_at, owner)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: FinishQueryLog :execrows
UPDATE query_log
SET state = $1,
    finished_at = $2,
    duration_ms = $3,
    rows_returned = $4,
    bytes_estimated = $5,
    truncated = $6,
    cache_status = $7,
    error_message = $8
WHERE id = $9 AND org_id = $10;

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

-- name: QueryUsageByUser :many
SELECT user_id,
       COUNT(*)::bigint                                                        AS queries,
       COALESCE(SUM(duration_ms), 0)::bigint                                   AS total_ms,
       COALESCE(SUM(rows_returned), 0)::bigint                                 AS total_rows,
       COALESCE(SUM(bytes_estimated), 0)::bigint                               AS total_bytes,
       COALESCE(SUM(CASE WHEN state = 'failed' THEN 1 ELSE 0 END), 0)::bigint  AS failures,
       COALESCE(SUM(CASE WHEN cache_status = 'hit' THEN 1 ELSE 0 END), 0)::bigint AS cache_hits
FROM query_log
WHERE org_id = $1 AND started_at >= $2
GROUP BY user_id
ORDER BY total_ms DESC;

-- name: RequestQueryCancel :execrows
UPDATE query_log
SET cancel_requested_at = now(),
    cancel_requested_by = $1
WHERE id = $2 AND org_id = $3 AND state = 'running';

-- name: ListCancelRequested :many
SELECT id FROM query_log
WHERE owner = $1 AND state = 'running' AND cancel_requested_at IS NOT NULL;

-- name: HeartbeatOwnedQueries :execrows
UPDATE query_log
SET heartbeat_at = now()
WHERE owner = $1 AND state = 'running';
