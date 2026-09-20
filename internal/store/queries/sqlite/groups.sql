-- name: CreateGroup :one
INSERT INTO groups (id, org_id, name, description, parent_group_id, external_id,
                    created_by, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetGroup :one
SELECT * FROM groups
WHERE id = ? AND org_id = ? AND deleted_at IS NULL;

-- name: GetGroupByName :one
SELECT * FROM groups
WHERE org_id = ? AND name = ? AND deleted_at IS NULL;

-- name: ListGroups :many
SELECT * FROM groups
WHERE org_id = ? AND deleted_at IS NULL
ORDER BY name
LIMIT ? OFFSET ?;

-- name: ListChildGroups :many
SELECT * FROM groups
WHERE org_id = ? AND parent_group_id = ? AND deleted_at IS NULL
ORDER BY name;

-- name: UpdateGroup :one
UPDATE groups
SET name = ?,
    description = ?,
    parent_group_id = ?,
    external_id = ?,
    updated_by = ?,
    updated_at = ?,
    version = version + 1
WHERE id = ? AND org_id = ? AND version = ? AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteGroup :execrows
UPDATE groups
SET deleted_at = ?, updated_by = ?, version = version + 1
WHERE id = ? AND org_id = ? AND deleted_at IS NULL;

-- name: AddGroupMember :exec
INSERT INTO group_members (org_id, group_id, user_id, added_by)
VALUES (?, ?, ?, ?)
ON CONFLICT (group_id, user_id) DO NOTHING;

-- name: RemoveGroupMember :execrows
DELETE FROM group_members
WHERE org_id = ? AND group_id = ? AND user_id = ?;

-- name: ListGroupMembers :many
SELECT u.* FROM users u
JOIN group_members gm ON gm.user_id = u.id
WHERE gm.org_id = ? AND gm.group_id = ? AND u.deleted_at IS NULL
ORDER BY u.email;

-- name: ListUserGroups :many
SELECT g.* FROM groups g
JOIN group_members gm ON gm.group_id = g.id
WHERE gm.org_id = ? AND gm.user_id = ? AND g.deleted_at IS NULL
ORDER BY g.name;

-- name: IsGroupMember :one
SELECT EXISTS (
    SELECT 1 FROM group_members
    WHERE org_id = ? AND group_id = ? AND user_id = ?
);
