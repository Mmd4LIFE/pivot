-- name: CreateGroup :one
INSERT INTO groups (id, org_id, name, description, parent_group_id, external_id,
                    created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetGroup :one
SELECT * FROM groups
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

-- name: GetGroupByName :one
SELECT * FROM groups
WHERE org_id = $1 AND name = $2 AND deleted_at IS NULL;

-- name: ListGroups :many
SELECT * FROM groups
WHERE org_id = $1 AND deleted_at IS NULL
ORDER BY name
LIMIT sqlc.arg('limit')::bigint OFFSET sqlc.arg('offset')::bigint;

-- name: ListChildGroups :many
SELECT * FROM groups
WHERE org_id = $1 AND parent_group_id = $2 AND deleted_at IS NULL
ORDER BY name;

-- name: UpdateGroup :one
UPDATE groups
SET name = $1,
    description = $2,
    parent_group_id = $3,
    external_id = $4,
    updated_by = $5,
    updated_at = $6,
    version = version + 1
WHERE id = $7 AND org_id = $8 AND version = $9 AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteGroup :execrows
UPDATE groups
SET deleted_at = $1, updated_by = $2, version = version + 1
WHERE id = $3 AND org_id = $4 AND deleted_at IS NULL;

-- name: AddGroupMember :exec
INSERT INTO group_members (org_id, group_id, user_id, added_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (group_id, user_id) DO NOTHING;

-- name: RemoveGroupMember :execrows
DELETE FROM group_members
WHERE org_id = $1 AND group_id = $2 AND user_id = $3;

-- name: ListGroupMembers :many
SELECT u.* FROM users u
JOIN group_members gm ON gm.user_id = u.id
WHERE gm.org_id = $1 AND gm.group_id = $2 AND u.deleted_at IS NULL
ORDER BY u.email;

-- name: ListUserGroups :many
SELECT g.* FROM groups g
JOIN group_members gm ON gm.group_id = g.id
WHERE gm.org_id = $1 AND gm.user_id = $2 AND g.deleted_at IS NULL
ORDER BY g.name;

-- name: IsGroupMember :one
SELECT EXISTS (
    SELECT 1 FROM group_members
    WHERE org_id = $1 AND group_id = $2 AND user_id = $3
);
