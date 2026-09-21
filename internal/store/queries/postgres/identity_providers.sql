-- Identity providers and the federated identities they issue.
--
-- Placeholders are positional in both dialects and must appear in the same
-- order, because the two generated parameter structs are converted directly
-- into one another and a differing field order breaks the conversion - the
-- lesson of Part 4-a. Query files are ASCII: sqlc's SQLite generator miscounts
-- byte offsets on multibyte characters and corrupts generation.

-- name: CreateIdentityProvider :one
INSERT INTO identity_providers (
    id, org_id, slug, name, kind, issuer, client_id, client_secret,
    scopes, is_enabled, auto_provision, link_by_email, default_role,
    claim_mapping, created_by, updated_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
RETURNING *;

-- name: GetIdentityProvider :one
SELECT * FROM identity_providers
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

-- name: GetIdentityProviderBySlug :one
SELECT * FROM identity_providers
WHERE org_id = $1 AND slug = $2 AND deleted_at IS NULL;

-- name: ListIdentityProviders :many
SELECT * FROM identity_providers
WHERE org_id = $1 AND deleted_at IS NULL
ORDER BY name;

-- name: UpdateIdentityProvider :one
UPDATE identity_providers
SET slug = $1, name = $2, issuer = $3, client_id = $4, client_secret = $5,
    scopes = $6, is_enabled = $7, auto_provision = $8, link_by_email = $9,
    default_role = $10, claim_mapping = $11, updated_by = $12, updated_at = $13,
    version = version + 1
WHERE id = $14 AND org_id = $15 AND version = $16 AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteIdentityProvider :execrows
UPDATE identity_providers
SET deleted_at = $1, updated_by = $2
WHERE id = $3 AND org_id = $4 AND deleted_at IS NULL;

-- Federated identities.
--
-- Lookup is by (provider, subject) and never by email: an address can be
-- reassigned inside a directory, and matching on it would hand the new holder
-- the old one's account.

-- name: GetFederatedIdentity :one
SELECT * FROM federated_identities
WHERE provider_id = $1 AND subject = $2;

-- name: ListFederatedIdentitiesForUser :many
SELECT * FROM federated_identities
WHERE org_id = $1 AND user_id = $2
ORDER BY created_at;

-- name: LinkFederatedIdentity :one
INSERT INTO federated_identities (id, org_id, provider_id, user_id, subject, last_login_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: RecordFederatedLogin :execrows
UPDATE federated_identities
SET last_login_at = $1
WHERE provider_id = $2 AND subject = $3;
