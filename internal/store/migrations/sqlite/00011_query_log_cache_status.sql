-- +goose Up

-- Constrain cache_status to the vocabulary that means something.
--
-- The PostgreSQL copy of this migration carries the reasoning. The difference
-- here is mechanical: SQLite cannot add a CHECK to an existing table, so the
-- table is rebuilt -- create the replacement, copy the rows, drop the
-- original, rename -- and the indexes are recreated, because dropping a table
-- drops them.
--
-- Column-by-column identical to 00010 apart from the new check, spelled out
-- rather than generated: the portability harness compares this against the
-- PostgreSQL table, and a rebuild that quietly loses a column is exactly what
-- that harness exists to catch.

CREATE TABLE query_log_new (
    id            TEXT NOT NULL PRIMARY KEY,
    org_id        TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    connection_id TEXT NOT NULL,

    user_id       TEXT,

    sql_text      TEXT NOT NULL,

    state         TEXT NOT NULL DEFAULT 'running',

    started_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at   TEXT,
    duration_ms   INTEGER NOT NULL DEFAULT 0,

    rows_returned   INTEGER NOT NULL DEFAULT 0,
    bytes_estimated INTEGER NOT NULL DEFAULT 0,

    truncated     INTEGER NOT NULL DEFAULT 0,
    cache_status  TEXT NOT NULL DEFAULT 'uncached',
    error_message TEXT NOT NULL DEFAULT '',

    FOREIGN KEY (connection_id, org_id) REFERENCES connections (id, org_id) ON DELETE CASCADE,
    CHECK (state IN ('running', 'succeeded', 'failed', 'canceled')),
    CHECK (cache_status IN ('hit', 'miss', 'uncached')),
    UNIQUE (id, org_id)
) STRICT;

INSERT INTO query_log_new
SELECT id, org_id, connection_id, user_id, sql_text, state, started_at,
       finished_at, duration_ms, rows_returned, bytes_estimated, truncated,
       cache_status, error_message
FROM query_log;

DROP TABLE query_log;

ALTER TABLE query_log_new RENAME TO query_log;

CREATE INDEX query_log_org_started_idx ON query_log (org_id, started_at DESC);

CREATE INDEX query_log_running_idx ON query_log (org_id, started_at) WHERE state = 'running';

-- +goose Down

DROP TABLE query_log;
