-- +goose Up

-- Connections: the data sources an organization can query.
--
-- The PostgreSQL copy of this migration carries the reasoning; this is the
-- same table in SQLite's types. The differences are the usual ones: TEXT for
-- UUIDs and timestamps, INTEGER for booleans, CHECK inline rather than named,
-- and STRICT so a column's declared type is enforced rather than advisory.

CREATE TABLE connections (
    id          TEXT    NOT NULL PRIMARY KEY,
    org_id      TEXT    NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    slug        TEXT    NOT NULL,
    name        TEXT    NOT NULL,
    kind        TEXT    NOT NULL CHECK (kind IN ('postgres')),
    description TEXT    NOT NULL DEFAULT '',

    host        TEXT    NOT NULL DEFAULT '',
    port        INTEGER NOT NULL DEFAULT 0,
    database    TEXT    NOT NULL DEFAULT '',
    username    TEXT    NOT NULL DEFAULT '',
    password    TEXT    NOT NULL DEFAULT '',
    ssl_mode    TEXT    NOT NULL DEFAULT '',
    options     TEXT    NOT NULL DEFAULT '{}',

    max_open_conns        INTEGER NOT NULL DEFAULT 0,
    max_rows              INTEGER NOT NULL DEFAULT 0,
    query_timeout_seconds INTEGER NOT NULL DEFAULT 0,

    is_enabled  INTEGER NOT NULL DEFAULT 1,

    last_tested_at  TEXT,
    last_test_ok    INTEGER NOT NULL DEFAULT 0,
    last_test_error TEXT    NOT NULL DEFAULT '',

    created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_by  TEXT,
    updated_by  TEXT,
    deleted_at  TEXT,
    version     INTEGER NOT NULL DEFAULT 1,
    UNIQUE (id, org_id)
) STRICT;

-- Partial, so a deleted connection's slug can be reused.
CREATE UNIQUE INDEX connections_org_slug_key
    ON connections (org_id, slug) WHERE deleted_at IS NULL;

CREATE INDEX connections_org_idx ON connections (org_id) WHERE deleted_at IS NULL;

-- +goose Down

DROP TABLE connections;
