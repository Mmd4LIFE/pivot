-- +goose Up

-- Declared relationships between tables. The PostgreSQL copy of this migration
-- carries the reasoning; this is the same table in SQLite's types.

CREATE TABLE catalog_foreign_keys (
    id            TEXT NOT NULL PRIMARY KEY,
    org_id        TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    connection_id TEXT NOT NULL,

    constraint_name TEXT NOT NULL,

    from_schema   TEXT NOT NULL,
    from_table    TEXT NOT NULL,
    from_column   TEXT NOT NULL,

    to_schema     TEXT NOT NULL,
    to_table      TEXT NOT NULL,
    to_column     TEXT NOT NULL,

    ordinal       INTEGER NOT NULL DEFAULT 1,

    first_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_seen_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    removed_at    TEXT,

    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    version       INTEGER NOT NULL DEFAULT 1,

    FOREIGN KEY (connection_id, org_id) REFERENCES connections (id, org_id) ON DELETE CASCADE,
    UNIQUE (connection_id, from_schema, from_table, constraint_name, ordinal),
    UNIQUE (id, org_id)
) STRICT;

CREATE INDEX catalog_foreign_keys_connection_idx
    ON catalog_foreign_keys (connection_id) WHERE removed_at IS NULL;

CREATE INDEX catalog_foreign_keys_target_idx
    ON catalog_foreign_keys (connection_id, to_schema, to_table) WHERE removed_at IS NULL;

-- +goose Down

DROP TABLE catalog_foreign_keys;
