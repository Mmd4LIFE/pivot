-- +goose Up

-- Identity and tenancy. See docs/architecture/data-model.md section 1.
--
-- Every table carries org_id, including child tables that could derive it from
-- their parent. The redundancy is deliberate: it lets the repository layer
-- (Part 4) scope every query uniformly, so a forgotten join can never widen
-- the scope of a read.

CREATE TABLE organizations (
    id         UUID        PRIMARY KEY,
    name       TEXT        NOT NULL,
    slug       TEXT        NOT NULL,
    settings   JSONB       NOT NULL DEFAULT '{}',
    plan       TEXT        NOT NULL DEFAULT 'free',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by UUID,
    updated_by UUID,
    deleted_at TIMESTAMPTZ,
    version    BIGINT      NOT NULL DEFAULT 1
);

-- Partial unique indexes: a soft-deleted row must not block reuse of its slug.
CREATE UNIQUE INDEX organizations_slug_key
    ON organizations (slug)
    WHERE deleted_at IS NULL;

CREATE TABLE users (
    id            UUID        PRIMARY KEY,
    org_id        UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    email         TEXT        NOT NULL,
    name          TEXT        NOT NULL DEFAULT '',
    avatar_url    TEXT,
    password_hash TEXT,
    is_active     BOOLEAN     NOT NULL DEFAULT TRUE,
    last_login_at TIMESTAMPTZ,
    locale        TEXT        NOT NULL DEFAULT 'en',
    timezone      TEXT        NOT NULL DEFAULT 'UTC',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by    UUID,
    updated_by    UUID,
    deleted_at    TIMESTAMPTZ,
    version       BIGINT      NOT NULL DEFAULT 1
);

CREATE UNIQUE INDEX users_org_email_key
    ON users (org_id, email)
    WHERE deleted_at IS NULL;

CREATE INDEX users_org_id_idx ON users (org_id);

-- Attributes feed row-level security in Phase 4. `source` records provenance
-- so an IdP-synced value is not silently clobbered by a manual edit.
CREATE TABLE user_attributes (
    id         UUID        PRIMARY KEY,
    org_id     UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    key        TEXT        NOT NULL,
    value      TEXT        NOT NULL,
    source     TEXT        NOT NULL DEFAULT 'manual',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT user_attributes_source_check
        CHECK (source IN ('manual', 'oidc', 'saml', 'scim'))
);

CREATE UNIQUE INDEX user_attributes_user_key_key ON user_attributes (user_id, key);

CREATE INDEX user_attributes_org_id_idx ON user_attributes (org_id);

CREATE TABLE groups (
    id              UUID        PRIMARY KEY,
    org_id          UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    description     TEXT        NOT NULL DEFAULT '',
    parent_group_id UUID        REFERENCES groups (id) ON DELETE SET NULL,
    external_id     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      UUID,
    updated_by      UUID,
    deleted_at      TIMESTAMPTZ,
    version         BIGINT      NOT NULL DEFAULT 1
);

CREATE UNIQUE INDEX groups_org_name_key
    ON groups (org_id, name)
    WHERE deleted_at IS NULL;

CREATE INDEX groups_org_id_idx ON groups (org_id);

CREATE INDEX groups_parent_group_id_idx ON groups (parent_group_id);

-- A join table: no soft delete and no version. Removing a membership is a
-- real delete, and there is nothing to concurrently edit.
CREATE TABLE group_members (
    org_id   UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    group_id UUID        NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    user_id  UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    added_by UUID,
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX group_members_user_id_idx ON group_members (user_id);

CREATE INDEX group_members_org_id_idx ON group_members (org_id);

-- +goose Down

DROP TABLE group_members;
DROP TABLE groups;
DROP TABLE user_attributes;
DROP TABLE users;
DROP TABLE organizations;
