-- +goose Up

-- The catalog: what Pivot has seen in the databases it is connected to.
--
-- Two tables rather than one document per connection. A column is the thing
-- everything above here points at -- Phase 3's models name one, Part 23's
-- autocomplete lists them, Part 24 formats by one's type -- and a JSON blob
-- would mean rewriting the whole schema to record that one column's type
-- changed.
--
-- `first_seen_at` and `last_seen_at` rather than a plain updated_at. What a
-- sync needs to answer is "is this still there", and the two dates answer it
-- without a second table: anything whose last_seen_at is older than the sync
-- that just ran is gone.
--
-- Nothing here is deleted by a sync. A table that vanishes is marked
-- `removed_at` and kept, for two reasons. A permissions change or a migration
-- caught mid-flight makes a table disappear for one sync and come back on the
-- next, and deleting would take the descriptions and the models pointing at it
-- with it. And "this used to exist" is the question somebody asks when a
-- dashboard breaks.

CREATE TABLE catalog_tables (
    id            UUID        NOT NULL PRIMARY KEY,
    org_id        UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    connection_id UUID        NOT NULL,

    schema_name   TEXT        NOT NULL,
    table_name    TEXT        NOT NULL,
    table_type    TEXT        NOT NULL DEFAULT 'table',
    comment       TEXT        NOT NULL DEFAULT '',

    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Set when a sync no longer finds it; cleared if it comes back.
    removed_at    TIMESTAMPTZ,

    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    version       BIGINT      NOT NULL DEFAULT 1,

    -- The connection is scoped to the organization already, and repeating
    -- org_id here is what lets every query filter on it without a join --
    -- the same tenant-scoping shape the rest of the schema uses.
    CONSTRAINT catalog_tables_connection_fk
        FOREIGN KEY (connection_id, org_id) REFERENCES connections (id, org_id) ON DELETE CASCADE,

    -- A table is identified by where it lives, not by a name Pivot made up.
    -- This is what makes a sync an upsert rather than a delete-and-insert.
    CONSTRAINT catalog_tables_identity_key UNIQUE (connection_id, schema_name, table_name),

    CONSTRAINT catalog_tables_org_key UNIQUE (id, org_id)
);

CREATE INDEX catalog_tables_connection_idx
    ON catalog_tables (connection_id) WHERE removed_at IS NULL;

CREATE TABLE catalog_columns (
    id            UUID        NOT NULL PRIMARY KEY,
    org_id        UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    table_id      UUID        NOT NULL,

    column_name   TEXT        NOT NULL,

    -- Both spellings, deliberately. source_type is what the database called
    -- it; canonical_type is what internal/datatype made of that. Keeping the
    -- first is what lets a stored catalog be re-normalized when Pivot learns a
    -- mapping it did not have, without going back to the warehouse to ask.
    source_type    TEXT       NOT NULL,
    canonical_type TEXT       NOT NULL DEFAULT 'unknown',

    is_nullable   BOOLEAN     NOT NULL DEFAULT TRUE,
    position      BIGINT      NOT NULL DEFAULT 0,
    comment       TEXT        NOT NULL DEFAULT '',

    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    removed_at    TIMESTAMPTZ,

    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    version       BIGINT      NOT NULL DEFAULT 1,

    CONSTRAINT catalog_columns_table_fk
        FOREIGN KEY (table_id, org_id) REFERENCES catalog_tables (id, org_id) ON DELETE CASCADE,

    CONSTRAINT catalog_columns_identity_key UNIQUE (table_id, column_name),

    CONSTRAINT catalog_columns_org_key UNIQUE (id, org_id)
);

CREATE INDEX catalog_columns_table_idx
    ON catalog_columns (table_id) WHERE removed_at IS NULL;

-- +goose Down

DROP TABLE catalog_columns;
DROP TABLE catalog_tables;
