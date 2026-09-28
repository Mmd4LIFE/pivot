-- +goose Up

-- The query log. The PostgreSQL copy of this migration carries the reasoning;
-- this is the same table in SQLite's types.

CREATE TABLE query_log (
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
    UNIQUE (id, org_id)
) STRICT;

CREATE INDEX query_log_org_started_idx ON query_log (org_id, started_at DESC);

CREATE INDEX query_log_running_idx ON query_log (org_id, started_at) WHERE state = 'running';

-- +goose Down

DROP TABLE query_log;
