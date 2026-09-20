-- +goose Up

-- The SQLite mirror of the PostgreSQL schema. See the postgres/ copy for the
-- design commentary; this file records only what differs and why.
--
-- Every table is STRICT. SQLite's default type affinity would happily store a
-- string in an INTEGER column, which is precisely how a bug passes here and
-- fails on PostgreSQL. STRICT makes the two engines disagree at migration
-- time instead of in production.
--
-- Type mapping:
--   UUID        -> TEXT  (canonical 36-char form)
--   JSONB       -> TEXT  (JSON document)
--   TIMESTAMPTZ -> TEXT  (RFC3339 with milliseconds, always UTC)
--   BOOLEAN     -> INTEGER (0/1)
--   BIGINT      -> INTEGER
--
-- Timestamp defaults use strftime with an explicit UTC format so both engines
-- produce the same shape of value. The Go layer writes timestamps explicitly;
-- these defaults are a safety net, not the primary path.

CREATE TABLE organizations (
    id         TEXT    PRIMARY KEY,
    name       TEXT    NOT NULL,
    slug       TEXT    NOT NULL,
    settings   TEXT    NOT NULL DEFAULT '{}',
    plan       TEXT    NOT NULL DEFAULT 'free',
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_by TEXT,
    updated_by TEXT,
    deleted_at TEXT,
    version    INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE UNIQUE INDEX organizations_slug_key
    ON organizations (slug)
    WHERE deleted_at IS NULL;

CREATE TABLE users (
    id            TEXT    PRIMARY KEY,
    org_id        TEXT    NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    email         TEXT    NOT NULL,
    name          TEXT    NOT NULL DEFAULT '',
    avatar_url    TEXT,
    password_hash TEXT,
    is_active     INTEGER NOT NULL DEFAULT 1,
    last_login_at TEXT,
    locale        TEXT    NOT NULL DEFAULT 'en',
    timezone      TEXT    NOT NULL DEFAULT 'UTC',
    created_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_by    TEXT,
    updated_by    TEXT,
    deleted_at    TEXT,
    version       INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE UNIQUE INDEX users_org_email_key
    ON users (org_id, email)
    WHERE deleted_at IS NULL;

CREATE INDEX users_org_id_idx ON users (org_id);

CREATE TABLE user_attributes (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    source     TEXT NOT NULL DEFAULT 'manual',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CONSTRAINT user_attributes_source_check
        CHECK (source IN ('manual', 'oidc', 'saml', 'scim'))
) STRICT;

CREATE UNIQUE INDEX user_attributes_user_key_key ON user_attributes (user_id, key);

CREATE INDEX user_attributes_org_id_idx ON user_attributes (org_id);

CREATE TABLE groups (
    id              TEXT    PRIMARY KEY,
    org_id          TEXT    NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name            TEXT    NOT NULL,
    description     TEXT    NOT NULL DEFAULT '',
    parent_group_id TEXT    REFERENCES groups (id) ON DELETE SET NULL,
    external_id     TEXT,
    created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_by      TEXT,
    updated_by      TEXT,
    deleted_at      TEXT,
    version         INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE UNIQUE INDEX groups_org_name_key
    ON groups (org_id, name)
    WHERE deleted_at IS NULL;

CREATE INDEX groups_org_id_idx ON groups (org_id);

CREATE INDEX groups_parent_group_id_idx ON groups (parent_group_id);

CREATE TABLE group_members (
    org_id   TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    group_id TEXT NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    user_id  TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    added_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    added_by TEXT,
    PRIMARY KEY (group_id, user_id)
) STRICT;

CREATE INDEX group_members_user_id_idx ON group_members (user_id);

CREATE INDEX group_members_org_id_idx ON group_members (org_id);

-- +goose Down

DROP TABLE group_members;
DROP TABLE groups;
DROP TABLE user_attributes;
DROP TABLE users;
DROP TABLE organizations;
