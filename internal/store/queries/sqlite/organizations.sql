-- name: CreateOrganization :one
INSERT INTO organizations (id, name, slug, settings, plan, created_by, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetOrganization :one
SELECT * FROM organizations
WHERE id = ? AND deleted_at IS NULL;

-- name: GetOrganizationBySlug :one
SELECT * FROM organizations
WHERE slug = ? AND deleted_at IS NULL;

-- name: ListOrganizations :many
SELECT * FROM organizations
WHERE deleted_at IS NULL
ORDER BY name
LIMIT ? OFFSET ?;

-- Optimistic concurrency: the WHERE clause carries the caller's expected
-- version, so a stale update affects zero rows instead of silently winning.
-- name: UpdateOrganization :one
UPDATE organizations
SET name = ?,
    slug = ?,
    settings = ?,
    plan = ?,
    updated_by = ?,
    updated_at = ?,
    version = version + 1
WHERE id = ? AND version = ? AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteOrganization :execrows
UPDATE organizations
SET deleted_at = ?, updated_by = ?, version = version + 1
WHERE id = ? AND deleted_at IS NULL;

-- name: CountOrganizations :one
SELECT count(*) FROM organizations WHERE deleted_at IS NULL;
