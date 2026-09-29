-- +goose Up

-- Who is running this query, and has anybody asked for it to stop.
--
-- A kill has to cross a process boundary. Two Pivots behind a load balancer
-- means the administrator pressing the button is usually not on the instance
-- holding the query -- and for SQLite and DuckDB sources there is no second
-- process to ask at all, because the query runs inside the Pivot that opened
-- the file.
--
-- So the kill travels as a row and the killing stays where it already works.
-- The killer writes cancel_requested_at; the instance that owns the query
-- notices and cancels the context it handed the connector, which is the path
-- Part 20-b measured stopping a real query in pg_stat_activity. Nothing new
-- kills anything.
--
-- `owner` is an opaque token minted per process, not a registry. There is no
-- instances table on purpose: a table of processes needs a lifecycle, a
-- heartbeat of its own and something to collect the dead ones, and all of that
-- exists to answer a question this column answers directly. The token dies
-- with the rows it stamped.
--
-- Three states, not two. Empty means nothing ever claimed the row -- written
-- before this migration, or by a path with no monitor -- and reads as
-- "unknown". A row with an owner and a fresh heartbeat is running. A row with
-- an owner and a stale one is abandoned, and that is the distinction the
-- Done-when asks for.

ALTER TABLE query_log
    ADD COLUMN owner               TEXT NOT NULL DEFAULT '',
    ADD COLUMN heartbeat_at        TIMESTAMPTZ,
    ADD COLUMN cancel_requested_at TIMESTAMPTZ,
    ADD COLUMN cancel_requested_by UUID;

-- Serves the two statements that key on owner: the cancel poll and the
-- heartbeat. Partial, because a finished query is never either.
CREATE INDEX query_log_owner_running_idx ON query_log (owner) WHERE state = 'running';

-- +goose Down

DROP INDEX IF EXISTS query_log_owner_running_idx;

ALTER TABLE query_log
    DROP COLUMN owner,
    DROP COLUMN heartbeat_at,
    DROP COLUMN cancel_requested_at,
    DROP COLUMN cancel_requested_by;
