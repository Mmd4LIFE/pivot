-- +goose Up

-- Drop the enumeration of connector kinds.
--
-- The PostgreSQL copy of this migration carries the reasoning. The difference
-- here is mechanical: SQLite cannot alter a CHECK constraint, so the table is
-- rebuilt. This is SQLite's own documented procedure -- create the replacement,
-- copy the rows, drop the original, rename -- and the indexes go with it,
-- because dropping a table drops them.
--
-- Column-by-column identical to 00006 apart from the check, deliberately
-- spelled out rather than generated: the portability harness compares this
-- against the PostgreSQL table, and a rebuild that quietly loses a column is
-- exactly what that harness exists to catch.

CREATE TABLE connections_new (
    id          TEXT    NOT NULL PRIMARY KEY,
    org_id      TEXT    NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    slug        TEXT    NOT NULL,
    name        TEXT    NOT NULL,
    kind        TEXT    NOT NULL CHECK (kind <> ''),
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

INSERT INTO connections_new SELECT * FROM connections;

DROP TABLE connections;

ALTER TABLE connections_new RENAME TO connections;

-- Recreated, because they belonged to the table that was dropped.
CREATE UNIQUE INDEX connections_org_slug_key
    ON connections (org_id, slug) WHERE deleted_at IS NULL;

CREATE INDEX connections_org_idx ON connections (org_id) WHERE deleted_at IS NULL;

-- +goose Down

CREATE TABLE connections_old (
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

-- Rows naming a connector the old constraint does not allow cannot come back.
-- A down migration that silently dropped them would be worse, so this fails
-- instead: restoring the old shape means dealing with them first.
INSERT INTO connections_old SELECT * FROM connections;

DROP TABLE connections;

ALTER TABLE connections_old RENAME TO connections;

CREATE UNIQUE INDEX connections_org_slug_key
    ON connections (org_id, slug) WHERE deleted_at IS NULL;

CREATE INDEX connections_org_idx ON connections (org_id) WHERE deleted_at IS NULL;
