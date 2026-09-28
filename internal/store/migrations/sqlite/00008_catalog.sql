-- +goose Up

-- The catalog. The PostgreSQL copy of this migration carries the reasoning;
-- this is the same pair of tables in SQLite's types -- TEXT for UUIDs and
-- timestamps, INTEGER for booleans, and STRICT so a declared type is enforced
-- rather than advisory.

CREATE TABLE catalog_tables (
    id            TEXT NOT NULL PRIMARY KEY,
    org_id        TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    connection_id TEXT NOT NULL,

    schema_name   TEXT NOT NULL,
    table_name    TEXT NOT NULL,
    table_type    TEXT NOT NULL DEFAULT 'table',
    comment       TEXT NOT NULL DEFAULT '',

    first_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_seen_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    removed_at    TEXT,

    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    version       INTEGER NOT NULL DEFAULT 1,

    FOREIGN KEY (connection_id, org_id) REFERENCES connections (id, org_id) ON DELETE CASCADE,
    UNIQUE (connection_id, schema_name, table_name),
    UNIQUE (id, org_id)
) STRICT;

CREATE INDEX catalog_tables_connection_idx
    ON catalog_tables (connection_id) WHERE removed_at IS NULL;

CREATE TABLE catalog_columns (
    id            TEXT NOT NULL PRIMARY KEY,
    org_id        TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    table_id      TEXT NOT NULL,

    column_name    TEXT NOT NULL,
    source_type    TEXT NOT NULL,
    canonical_type TEXT NOT NULL DEFAULT 'unknown',

    is_nullable   INTEGER NOT NULL DEFAULT 1,
    position      INTEGER NOT NULL DEFAULT 0,
    comment       TEXT NOT NULL DEFAULT '',

    first_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_seen_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    removed_at    TEXT,

    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    version       INTEGER NOT NULL DEFAULT 1,

    FOREIGN KEY (table_id, org_id) REFERENCES catalog_tables (id, org_id) ON DELETE CASCADE,
    UNIQUE (table_id, column_name),
    UNIQUE (id, org_id)
) STRICT;

CREATE INDEX catalog_columns_table_idx
    ON catalog_columns (table_id) WHERE removed_at IS NULL;

-- +goose Down

DROP TABLE catalog_columns;
DROP TABLE catalog_tables;
