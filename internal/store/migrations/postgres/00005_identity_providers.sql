-- +goose Up

-- Identity providers: an organization's configured SSO connections.
--
-- Per organization rather than per instance, because a multi-tenant deployment
-- has to let each tenant point at its own directory. A single-tenant install
-- simply has one row.
--
-- slug is the URL segment, so /api/v1/auth/oidc/okta/start names a row. It is
-- unique per organization and not globally: two tenants may both call theirs
-- "okta", and the organization is resolved before the slug.
--
-- client_secret is stored as written. There is no encryption yet - Part 15
-- builds the envelope encryption that this column and Phase 1's connection
-- credentials will both use. Until then a database dump exposes it, which is
-- why the API never returns it and the startup log says so. PKCE is used
-- regardless of whether a secret is set, so a provider configured as a public
-- client needs no secret at all.
--
-- claim_mapping holds the whole of "which claim means what" in one JSON
-- document instead of a column per claim, because identity providers disagree
-- about names in ways a fixed schema cannot anticipate.
--
-- link_by_email is OFF by default, and that default is a security decision.
-- With it on, a first login whose email matches an existing account adopts
-- that account. That is what an organization turning SSO on for the first time
-- wants - nobody loses the account they already had. It is also how a recycled
-- address becomes an account takeover: directories reassign addresses, and the
-- new holder of ada@example.com would inherit the old Ada's access. So it is
-- opt-in, per provider, and meant to be turned off again after a migration.

CREATE TABLE identity_providers (
    id             UUID        PRIMARY KEY,
    org_id         UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    slug           TEXT        NOT NULL,
    name           TEXT        NOT NULL,
    kind           TEXT        NOT NULL DEFAULT 'oidc',
    issuer         TEXT        NOT NULL,
    client_id      TEXT        NOT NULL,
    client_secret  TEXT        NOT NULL DEFAULT '',
    scopes         TEXT        NOT NULL DEFAULT 'openid profile email',
    is_enabled     BOOLEAN     NOT NULL DEFAULT TRUE,
    auto_provision BOOLEAN     NOT NULL DEFAULT TRUE,
    default_role   TEXT        NOT NULL DEFAULT 'viewer',
    link_by_email  BOOLEAN     NOT NULL DEFAULT FALSE,
    claim_mapping  JSONB       NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by     UUID,
    updated_by     UUID,
    deleted_at     TIMESTAMPTZ,
    version        BIGINT      NOT NULL DEFAULT 1,
    CONSTRAINT identity_providers_kind_check CHECK (kind IN ('oidc'))
);

-- Partial, so a deleted provider's slug can be reused - the same pattern as
-- every other soft-deleted table here.
CREATE UNIQUE INDEX identity_providers_org_slug_key
    ON identity_providers (org_id, slug) WHERE deleted_at IS NULL;

CREATE INDEX identity_providers_org_idx ON identity_providers (org_id) WHERE deleted_at IS NULL;

-- The target of the composite foreign key below. PostgreSQL requires the
-- referenced columns to carry a unique constraint, and (id, org_id) has none
-- from the primary key alone. Migration 00002 added the same pair to users and
-- groups for the same reason.
ALTER TABLE identity_providers ADD CONSTRAINT identity_providers_id_org_id_key UNIQUE (id, org_id);

-- Federated identities: which external subject is which Pivot user.
--
-- The link is (provider, subject), never the email address. An email can be
-- reassigned inside a directory, and matching on it means the new holder of
-- alice@example.com inherits the old Alice's access. The OIDC `sub` claim is
-- the stable identifier, and it is the only thing that may be trusted to
-- identify a returning user.
--
-- The foreign keys are composite on (id, org_id), per migration 00002: a
-- single-column key would let a row name one organization while pointing at
-- another's user.

CREATE TABLE federated_identities (
    id          UUID        PRIMARY KEY,
    org_id      UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    provider_id UUID        NOT NULL,
    user_id     UUID        NOT NULL,
    subject     TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ,
    CONSTRAINT federated_identities_provider_fkey
        FOREIGN KEY (provider_id, org_id) REFERENCES identity_providers (id, org_id) ON DELETE CASCADE,
    CONSTRAINT federated_identities_user_fkey
        FOREIGN KEY (user_id, org_id) REFERENCES users (id, org_id) ON DELETE CASCADE
);

-- One external subject maps to one Pivot user per provider.
CREATE UNIQUE INDEX federated_identities_provider_subject_key
    ON federated_identities (provider_id, subject);

CREATE INDEX federated_identities_user_idx ON federated_identities (org_id, user_id);

-- +goose Down

DROP TABLE federated_identities;
DROP TABLE identity_providers;
