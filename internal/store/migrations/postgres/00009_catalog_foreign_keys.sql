-- +goose Up

-- Declared relationships between tables, one row per column of each key.
--
-- A row per column rather than per constraint, with `ordinal` giving the
-- order. Two ordered lists per constraint would need an array type PostgreSQL
-- has and SQLite does not, and a JSON blob would put the ordering somewhere
-- nothing can index or join on.
--
-- The ordering is the part that matters. Every source Pivot reads exposes a
-- foreign key as two column lists, and reading them back by joining rather
-- than by position *crosses* them: a two-column key becomes four pairs, and a
-- join built from it matches on columns that were never related. It returns
-- rows, which is why it is worth a column in the schema rather than a comment.
--
-- from_* and to_* are names, not references into catalog_tables. A
-- relationship can point at a table this connection cannot see -- a schema
-- granted piecemeal is the ordinary way -- and a foreign key dropped because
-- its target was invisible is information thrown away for tidiness. Whether
-- the target is catalogued is a join away and never stale; storing a flag for
-- it would be derived state that goes wrong the moment the catalog changes.

CREATE TABLE catalog_foreign_keys (
    id            UUID        NOT NULL PRIMARY KEY,
    org_id        UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    connection_id UUID        NOT NULL,

    constraint_name TEXT      NOT NULL,

    from_schema   TEXT        NOT NULL,
    from_table    TEXT        NOT NULL,
    from_column   TEXT        NOT NULL,

    to_schema     TEXT        NOT NULL,
    to_table      TEXT        NOT NULL,
    to_column     TEXT        NOT NULL,

    ordinal       BIGINT      NOT NULL DEFAULT 1,

    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    removed_at    TIMESTAMPTZ,

    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    version       BIGINT      NOT NULL DEFAULT 1,

    CONSTRAINT catalog_foreign_keys_connection_fk
        FOREIGN KEY (connection_id, org_id) REFERENCES connections (id, org_id) ON DELETE CASCADE,

    -- The table is part of the identity as well as the name, because a
    -- constraint name is unique per table in some sources and per schema in
    -- others -- MySQL allows two tables to carry the same one, and keying on
    -- the name alone would merge two relationships into one wrong row.
    CONSTRAINT catalog_foreign_keys_identity_key
        UNIQUE (connection_id, from_schema, from_table, constraint_name, ordinal),

    CONSTRAINT catalog_foreign_keys_org_key UNIQUE (id, org_id)
);

CREATE INDEX catalog_foreign_keys_connection_idx
    ON catalog_foreign_keys (connection_id) WHERE removed_at IS NULL;

-- What Phase 3's join inference asks: "what points at this table".
CREATE INDEX catalog_foreign_keys_target_idx
    ON catalog_foreign_keys (connection_id, to_schema, to_table) WHERE removed_at IS NULL;

-- +goose Down

DROP TABLE catalog_foreign_keys;
