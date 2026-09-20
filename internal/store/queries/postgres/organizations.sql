-- name: CreateOrganization :one
INSERT INTO organizations (id, name, slug, settings, plan, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetOrganization :one
SELECT * FROM organizations
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetOrganizationBySlug :one
SELECT * FROM organizations
WHERE slug = $1 AND deleted_at IS NULL;

-- name: ListOrganizations :many
SELECT * FROM organizations
WHERE deleted_at IS NULL
ORDER BY name
LIMIT sqlc.arg('limit')::bigint OFFSET sqlc.arg('offset')::bigint;

-- Optimistic concurrency: the WHERE clause carries the caller's expected
-- version, so a stale update affects zero rows instead of silently winning.
-- name: UpdateOrganization :one
UPDATE organizations
SET name = $1,
    slug = $2,
    settings = $3,
    plan = $4,
    updated_by = $5,
    updated_at = $6,
    version = version + 1
WHERE id = $7 AND version = $8 AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteOrganization :execrows
UPDATE organizations
SET deleted_at = $1, updated_by = $2, version = version + 1
WHERE id = $3 AND deleted_at IS NULL;

-- name: CountOrganizations :one
SELECT count(*) FROM organizations WHERE deleted_at IS NULL;
