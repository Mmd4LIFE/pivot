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
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetIdentityProvider :one
SELECT * FROM identity_providers
WHERE id = ? AND org_id = ? AND deleted_at IS NULL;

-- name: GetIdentityProviderBySlug :one
SELECT * FROM identity_providers
WHERE org_id = ? AND slug = ? AND deleted_at IS NULL;

-- name: ListIdentityProviders :many
SELECT * FROM identity_providers
WHERE org_id = ? AND deleted_at IS NULL
ORDER BY name;

-- name: UpdateIdentityProvider :one
UPDATE identity_providers
SET slug = ?, name = ?, issuer = ?, client_id = ?, client_secret = ?,
    scopes = ?, is_enabled = ?, auto_provision = ?, link_by_email = ?,
    default_role = ?, claim_mapping = ?, updated_by = ?, updated_at = ?,
    version = version + 1
WHERE id = ? AND org_id = ? AND version = ? AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteIdentityProvider :execrows
UPDATE identity_providers
SET deleted_at = ?, updated_by = ?
WHERE id = ? AND org_id = ? AND deleted_at IS NULL;

-- Federated identities.
--
-- Lookup is by (provider, subject) and never by email: an address can be
-- reassigned inside a directory, and matching on it would hand the new holder
-- the old one's account.

-- name: GetFederatedIdentity :one
SELECT * FROM federated_identities
WHERE provider_id = ? AND subject = ?;

-- name: ListFederatedIdentitiesForUser :many
SELECT * FROM federated_identities
WHERE org_id = ? AND user_id = ?
ORDER BY created_at;

-- name: LinkFederatedIdentity :one
INSERT INTO federated_identities (id, org_id, provider_id, user_id, subject, last_login_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: RecordFederatedLogin :execrows
UPDATE federated_identities
SET last_login_at = ?
WHERE provider_id = ? AND subject = ?;
