-- +goose Up

-- Role assignments, stored in Zanzibar's shape: a (subject, relation, object)
-- triple. See ADR-0009 and its amendment.
--
-- The shape is the point. A row here is one OpenFGA tuple, so migrating to it
-- is an export and a Write call rather than a translation — and until then the
-- same rows answer the same questions through internal/authz. A
-- "user_roles(user_id, role)" table would have been smaller and would have had
-- to be thrown away.
--
-- What is deliberately NOT here: group membership. It already exists in
-- group_members, and copying it into this table would create a second source
-- of truth for the same fact, with no mechanism keeping them honest. The
-- resolver reads membership from group_members and role grants from here.
--
-- subject_relation is what makes a userset expressible. Empty means the
-- subject is one user; 'member' means every member of the named group, which
-- is how a role is granted to a group rather than one person at a time.

CREATE TABLE role_assignments (
    id               TEXT NOT NULL PRIMARY KEY,
    org_id           TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    subject_type     TEXT NOT NULL CHECK (subject_type IN ('user', 'group')),
    subject_id       TEXT NOT NULL,
    subject_relation TEXT NOT NULL DEFAULT '',
    relation         TEXT NOT NULL,
    object_type      TEXT NOT NULL CHECK (object_type IN ('organization', 'group')),
    object_id        TEXT NOT NULL,
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_by       TEXT
) STRICT;

-- A tuple is a fact: it either exists or it does not, so storing it twice is
-- meaningless and a second grant must be a no-op rather than a duplicate row.
CREATE UNIQUE INDEX role_assignments_tuple_key
    ON role_assignments (org_id, subject_type, subject_id, subject_relation, relation, object_type, object_id);

-- The request-path query: given a handful of subjects, which relations do they
-- hold on this object. Ordered to match that predicate.
CREATE INDEX role_assignments_lookup_idx
    ON role_assignments (org_id, object_type, object_id, subject_type, subject_id);

-- The administrative query: what does this subject hold, anywhere.
CREATE INDEX role_assignments_subject_idx
    ON role_assignments (org_id, subject_type, subject_id);

-- +goose Down

DROP TABLE role_assignments;
