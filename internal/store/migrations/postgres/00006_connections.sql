-- +goose Up

-- Connections: the data sources an organization can query.
--
-- Per organization, like everything else here. A connection belongs to exactly
-- one tenant and is never visible across them, which is enforced by the same
-- composite foreign keys migration 00002 introduced rather than by a WHERE
-- clause somebody has to remember.
--
-- slug names a connection in a URL and in a query's provenance, so it is
-- unique per organization and not globally: two tenants may both call theirs
-- "warehouse".
--
-- `kind` says which connector drives it. The CHECK constraint lists only what
-- is implemented, which means adding a connector is a migration -- deliberate,
-- because a row naming a connector that does not exist is a connection nobody
-- can open and nobody can explain.
--
-- host/port/database/username are separate columns rather than one DSN string.
-- A DSN is convenient to store and impossible to validate, to redact, to show
-- half of in a UI, or to change one field of. Connector-specific settings that
-- do not generalize -- a Snowflake warehouse, a BigQuery project -- go in
-- `options`, for the same reason claim_mapping is JSON on identity_providers:
-- sources disagree in ways a fixed schema cannot anticipate.
--
-- `password` is an envelope from internal/secrets, never plaintext. Part 15-d
-- built that; this column is its second purpose, and the purpose string binds
-- the ciphertext to this column so a value lifted out of identity_providers
-- cannot be pasted in here and decrypted.
--
-- ssl_mode is a column rather than an option because it is the setting most
-- often wrong and most worth seeing without opening a JSON blob.

CREATE TABLE connections (
    id          UUID        PRIMARY KEY,
    org_id      UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    slug        TEXT        NOT NULL,
    name        TEXT        NOT NULL,
    kind        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',

    host        TEXT        NOT NULL DEFAULT '',
    port        BIGINT      NOT NULL DEFAULT 0,
    database    TEXT        NOT NULL DEFAULT '',
    username    TEXT        NOT NULL DEFAULT '',
    password    TEXT        NOT NULL DEFAULT '',
    ssl_mode    TEXT        NOT NULL DEFAULT '',
    options     JSONB       NOT NULL DEFAULT '{}',

    -- Resource governance, per connection. Zero means "use the instance
    -- default", so a connection created before a limit existed does not pin
    -- itself to whatever that limit happened to be on the day.
    max_open_conns  BIGINT  NOT NULL DEFAULT 0,
    max_rows        BIGINT  NOT NULL DEFAULT 0,
    query_timeout_seconds BIGINT NOT NULL DEFAULT 0,

    is_enabled  BOOLEAN     NOT NULL DEFAULT TRUE,

    -- What happened the last time anybody tested it. Stored rather than
    -- recomputed because "this connection is broken" is the first thing an
    -- administrator wants on a list page, and testing every row to render one
    -- would make that page a thundering herd against everybody's warehouse.
    --
    -- last_test_ok is not nullable: "never tested" is last_tested_at IS NULL,
    -- and a three-state boolean beside a nullable timestamp is the same fact
    -- stored twice, which is the same fact disagreeing with itself later.
    --
    -- The integers are BIGINT rather than INTEGER so that both engines
    -- generate int64 and the two model structs stay convertible - the same
    -- reason login_attempts.failed_count is BIGINT.
    last_tested_at    TIMESTAMPTZ,
    last_test_ok      BOOLEAN NOT NULL DEFAULT FALSE,
    last_test_error   TEXT    NOT NULL DEFAULT '',

    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  UUID,
    updated_by  UUID,
    deleted_at  TIMESTAMPTZ,
    version     BIGINT      NOT NULL DEFAULT 1,

    CONSTRAINT connections_kind_check CHECK (kind IN ('postgres'))
);

-- Partial, so a deleted connection's slug can be reused.
CREATE UNIQUE INDEX connections_org_slug_key
    ON connections (org_id, slug) WHERE deleted_at IS NULL;

CREATE INDEX connections_org_idx ON connections (org_id) WHERE deleted_at IS NULL;

-- The target of composite foreign keys from the tables Phase 1 adds next: a
-- single-column key would let a row name one organization while pointing at
-- another's connection.
ALTER TABLE connections ADD CONSTRAINT connections_id_org_id_key UNIQUE (id, org_id);

-- +goose Down

DROP TABLE connections;
