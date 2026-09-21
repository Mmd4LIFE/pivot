-- Role assignments are Zanzibar tuples. See ADR-0009's amendment.
--
-- Placeholders are positional in both dialects and must appear in the same
-- order, because the two generated parameter structs are converted directly
-- into one another and a differing field order breaks the conversion - the
-- lesson of Part 4-a. sqlc's named arguments are avoided because its SQLite
-- path mis-substitutes numbered placeholders.

-- The request-path query, and the one shape worth explaining.
--
-- The resolver asks which relations a set of subjects hold on an object, where
-- the subjects are one user plus the member-userset of every group they belong
-- to. That is a variable-length IN list, which Postgres expresses as an array
-- and SQLite cannot express at all without building SQL by hand - and
-- hand-built SQL is how injection happens.
--
-- So the predicate is widened instead: the user's own grants, plus every group
-- grant on this object. The caller intersects the group rows with the groups
-- it already knows the user belongs to. The widened set is bounded by the
-- number of groups holding a role on one object, which is small, and the query
-- is identical on both engines.

-- name: ListObjectGrants :many
SELECT * FROM role_assignments
WHERE org_id = ?
  AND object_type = ?
  AND object_id = ?
  AND (
        (subject_type = 'user' AND subject_id = ?)
     OR subject_type = 'group'
  );

-- name: ListSubjectGrants :many
SELECT * FROM role_assignments
WHERE org_id = ?
  AND subject_type = ?
  AND subject_id = ?
ORDER BY object_type, object_id, relation;

-- name: ListGrantsOnObject :many
SELECT * FROM role_assignments
WHERE org_id = ?
  AND object_type = ?
  AND object_id = ?
ORDER BY subject_type, subject_id, relation;

-- Granting a role twice is a no-op, not a duplicate row: a tuple is a fact,
-- and a fact is either stored or not.
-- name: GrantRole :exec
INSERT INTO role_assignments (
    id, org_id, subject_type, subject_id, subject_relation,
    relation, object_type, object_id, created_by
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (org_id, subject_type, subject_id, subject_relation, relation, object_type, object_id)
DO NOTHING;

-- name: RevokeRole :execrows
DELETE FROM role_assignments
WHERE org_id = ?
  AND subject_type = ?
  AND subject_id = ?
  AND subject_relation = ?
  AND relation = ?
  AND object_type = ?
  AND object_id = ?;

-- Removing a user or group takes its grants with it.
-- name: RevokeAllForSubject :execrows
DELETE FROM role_assignments
WHERE org_id = ?
  AND subject_type = ?
  AND subject_id = ?;

-- name: CountRoleHolders :one
SELECT COUNT(*) FROM role_assignments
WHERE org_id = ?
  AND relation = ?
  AND object_type = ?
  AND object_id = ?;
