-- +goose Up

-- The SQLite mirror of the PostgreSQL migration. See the postgres/ copy for
-- why this exists; this file records only what differs.
--
-- SQLite cannot ALTER a constraint, so the two tables are rebuilt: create the
-- replacement, copy the rows, drop the original, rename. That is the standard
-- SQLite pattern and the reason this migration is long where the PostgreSQL
-- one is a handful of ALTERs.

CREATE UNIQUE INDEX users_id_org_id_key ON users (id, org_id);
CREATE UNIQUE INDEX groups_id_org_id_key ON groups (id, org_id);

-- --- group_members --------------------------------------------------------

CREATE TABLE group_members_rebuilt (
    org_id   TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    group_id TEXT NOT NULL,
    user_id  TEXT NOT NULL,
    added_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    added_by TEXT,
    PRIMARY KEY (group_id, user_id),
    FOREIGN KEY (group_id, org_id) REFERENCES groups (id, org_id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, org_id) REFERENCES users (id, org_id) ON DELETE CASCADE
) STRICT;

INSERT INTO group_members_rebuilt (org_id, group_id, user_id, added_at, added_by)
SELECT org_id, group_id, user_id, added_at, added_by FROM group_members;

DROP TABLE group_members;

ALTER TABLE group_members_rebuilt RENAME TO group_members;

CREATE INDEX group_members_user_id_idx ON group_members (user_id);
CREATE INDEX group_members_org_id_idx ON group_members (org_id);

-- --- user_attributes ------------------------------------------------------

CREATE TABLE user_attributes_rebuilt (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    source     TEXT NOT NULL DEFAULT 'manual',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CONSTRAINT user_attributes_source_check
        CHECK (source IN ('manual', 'oidc', 'saml', 'scim')),
    FOREIGN KEY (user_id, org_id) REFERENCES users (id, org_id) ON DELETE CASCADE
) STRICT;

INSERT INTO user_attributes_rebuilt (id, org_id, user_id, key, value, source, created_at, updated_at)
SELECT id, org_id, user_id, key, value, source, created_at, updated_at FROM user_attributes;

DROP TABLE user_attributes;

ALTER TABLE user_attributes_rebuilt RENAME TO user_attributes;

CREATE UNIQUE INDEX user_attributes_user_key_key ON user_attributes (user_id, key);
CREATE INDEX user_attributes_org_id_idx ON user_attributes (org_id);

-- +goose Down

DROP INDEX user_attributes_org_id_idx;
DROP INDEX user_attributes_user_key_key;
DROP TABLE user_attributes;

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

DROP INDEX group_members_org_id_idx;
DROP INDEX group_members_user_id_idx;
DROP TABLE group_members;

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

DROP INDEX groups_id_org_id_key;
DROP INDEX users_id_org_id_key;
