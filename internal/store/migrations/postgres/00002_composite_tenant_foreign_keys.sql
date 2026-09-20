-- +goose Up

-- Close a tenant isolation hole in the v1 schema.
--
-- group_members and user_attributes referenced groups(id) and users(id) alone.
-- Each foreign key was satisfied independently, so a row like
-- (org_id=A, group_id=A's group, user_id=B's user) was accepted: organization A
-- exists, A's group exists, B's user exists. Nothing tied the user to the
-- organization the row claimed.
--
-- The repository layer sets org_id from the request scope, so this was not
-- reachable by accident through it — but "not reachable through the current
-- code" is exactly the guarantee Part 4 exists to replace with a structural
-- one. Composite keys make the bad row impossible at the database level, on
-- both engines, regardless of what any future caller does.
--
-- Found by TestGroupMembership/cross-tenant_member_add_is_rejected.

ALTER TABLE users ADD CONSTRAINT users_id_org_id_key UNIQUE (id, org_id);
ALTER TABLE groups ADD CONSTRAINT groups_id_org_id_key UNIQUE (id, org_id);

ALTER TABLE group_members DROP CONSTRAINT group_members_group_id_fkey;
ALTER TABLE group_members DROP CONSTRAINT group_members_user_id_fkey;

ALTER TABLE group_members
    ADD CONSTRAINT group_members_group_fkey
    FOREIGN KEY (group_id, org_id) REFERENCES groups (id, org_id) ON DELETE CASCADE;

ALTER TABLE group_members
    ADD CONSTRAINT group_members_user_fkey
    FOREIGN KEY (user_id, org_id) REFERENCES users (id, org_id) ON DELETE CASCADE;

ALTER TABLE user_attributes DROP CONSTRAINT user_attributes_user_id_fkey;

ALTER TABLE user_attributes
    ADD CONSTRAINT user_attributes_user_fkey
    FOREIGN KEY (user_id, org_id) REFERENCES users (id, org_id) ON DELETE CASCADE;

-- +goose Down

ALTER TABLE user_attributes DROP CONSTRAINT user_attributes_user_fkey;
ALTER TABLE user_attributes
    ADD CONSTRAINT user_attributes_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE;

ALTER TABLE group_members DROP CONSTRAINT group_members_user_fkey;
ALTER TABLE group_members DROP CONSTRAINT group_members_group_fkey;

ALTER TABLE group_members
    ADD CONSTRAINT group_members_group_id_fkey
    FOREIGN KEY (group_id) REFERENCES groups (id) ON DELETE CASCADE;

ALTER TABLE group_members
    ADD CONSTRAINT group_members_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE;

ALTER TABLE groups DROP CONSTRAINT groups_id_org_id_key;
ALTER TABLE users DROP CONSTRAINT users_id_org_id_key;
