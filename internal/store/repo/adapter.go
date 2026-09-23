package repo

import (
	"context"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/gen/lite"
	"github.com/Mmd4LIFE/pivot/internal/store/gen/pg"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

// The adapters below are mechanical: every body is a type conversion, because
// sqlc is configured to emit structurally identical types for both engines
// (see sqlc.yaml and internal/store/dbtypes). If a conversion here stops
// compiling, the two generated packages have drifted apart — that is the
// failure mode these adapters are designed to surface loudly.

// Compile-time assertions that both adapters satisfy the interface.
var (
	_ Querier = (*pgQuerier)(nil)
	_ Querier = (*liteQuerier)(nil)
)

// NewQuerier returns the Querier for a database's engine.
func NewQuerier(db *store.DB) Querier {
	// Wrapped in the tracing decorator here, at the one place a Querier is
	// ever constructed, so no repository has to remember to ask for it.
	if db.IsPostgres() {
		return traced(&pgQuerier{q: pg.New(db.DB)})
	}

	return traced(&liteQuerier{q: lite.New(db.DB)})
}

// --- PostgreSQL -----------------------------------------------------------

type pgQuerier struct{ q *pg.Queries }

func (a *pgQuerier) CreateOrganization(
	ctx context.Context, p model.CreateOrganizationParams,
) (model.Organization, error) {
	row, err := a.q.CreateOrganization(ctx, pg.CreateOrganizationParams(p))

	return model.Organization(row), err
}

func (a *pgQuerier) GetOrganization(ctx context.Context, id uuid.UUID) (model.Organization, error) {
	row, err := a.q.GetOrganization(ctx, id)

	return model.Organization(row), err
}

func (a *pgQuerier) GetOrganizationBySlug(ctx context.Context, slug string) (model.Organization, error) {
	row, err := a.q.GetOrganizationBySlug(ctx, slug)

	return model.Organization(row), err
}

func (a *pgQuerier) ListOrganizations(
	ctx context.Context, p model.ListOrganizationsParams,
) ([]model.Organization, error) {
	rows, err := a.q.ListOrganizations(ctx, pg.ListOrganizationsParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.Organization, len(rows))
	for i, r := range rows {
		out[i] = model.Organization(r)
	}

	return out, nil
}

func (a *pgQuerier) UpdateOrganization(
	ctx context.Context, p model.UpdateOrganizationParams,
) (model.Organization, error) {
	row, err := a.q.UpdateOrganization(ctx, pg.UpdateOrganizationParams(p))

	return model.Organization(row), err
}

func (a *pgQuerier) SoftDeleteOrganization(
	ctx context.Context, p model.SoftDeleteOrganizationParams,
) (int64, error) {
	return a.q.SoftDeleteOrganization(ctx, pg.SoftDeleteOrganizationParams(p))
}

func (a *pgQuerier) CountOrganizations(ctx context.Context) (int64, error) {
	return a.q.CountOrganizations(ctx)
}

func (a *pgQuerier) CreateUser(ctx context.Context, p model.CreateUserParams) (model.User, error) {
	row, err := a.q.CreateUser(ctx, pg.CreateUserParams(p))

	return model.User(row), err
}

func (a *pgQuerier) GetUser(ctx context.Context, p model.GetUserParams) (model.User, error) {
	row, err := a.q.GetUser(ctx, pg.GetUserParams(p))

	return model.User(row), err
}

func (a *pgQuerier) GetUserByEmail(ctx context.Context, p model.GetUserByEmailParams) (model.User, error) {
	row, err := a.q.GetUserByEmail(ctx, pg.GetUserByEmailParams(p))

	return model.User(row), err
}

func (a *pgQuerier) ListUsers(ctx context.Context, p model.ListUsersParams) ([]model.User, error) {
	rows, err := a.q.ListUsers(ctx, pg.ListUsersParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.User, len(rows))
	for i, r := range rows {
		out[i] = model.User(r)
	}

	return out, nil
}

func (a *pgQuerier) UpdateUser(ctx context.Context, p model.UpdateUserParams) (model.User, error) {
	row, err := a.q.UpdateUser(ctx, pg.UpdateUserParams(p))

	return model.User(row), err
}

func (a *pgQuerier) UpdateUserPassword(ctx context.Context, p model.UpdateUserPasswordParams) (int64, error) {
	return a.q.UpdateUserPassword(ctx, pg.UpdateUserPasswordParams(p))
}

func (a *pgQuerier) RecordUserLogin(ctx context.Context, p model.RecordUserLoginParams) (int64, error) {
	return a.q.RecordUserLogin(ctx, pg.RecordUserLoginParams(p))
}

func (a *pgQuerier) SoftDeleteUser(ctx context.Context, p model.SoftDeleteUserParams) (int64, error) {
	return a.q.SoftDeleteUser(ctx, pg.SoftDeleteUserParams(p))
}

func (a *pgQuerier) CountUsers(ctx context.Context, orgID uuid.UUID) (int64, error) {
	return a.q.CountUsers(ctx, orgID)
}

// --- SQLite ---------------------------------------------------------------

type liteQuerier struct{ q *lite.Queries }

func (a *liteQuerier) CreateOrganization(
	ctx context.Context, p model.CreateOrganizationParams,
) (model.Organization, error) {
	row, err := a.q.CreateOrganization(ctx, lite.CreateOrganizationParams(p))

	return model.Organization(row), err
}

func (a *liteQuerier) GetOrganization(ctx context.Context, id uuid.UUID) (model.Organization, error) {
	row, err := a.q.GetOrganization(ctx, id)

	return model.Organization(row), err
}

func (a *liteQuerier) GetOrganizationBySlug(ctx context.Context, slug string) (model.Organization, error) {
	row, err := a.q.GetOrganizationBySlug(ctx, slug)

	return model.Organization(row), err
}

// The two list methods below construct their params field by field rather
// than converting.
//
// sqlc numbers the named limit and offset arguments differently per dialect —
// PostgreSQL gets offset=$1, limit=$2 while SQLite gets limit=?1, offset=?2 —
// so the generated structs declare the same fields in a different ORDER. Go
// permits struct conversion only when fields correspond in order, so the
// conversion that works everywhere else fails here. Naming the fields is
// immune to order, and the generated SQL binds each correctly; the pagination
// test proves it.

func (a *liteQuerier) ListOrganizations(
	ctx context.Context, p model.ListOrganizationsParams,
) ([]model.Organization, error) {
	rows, err := a.q.ListOrganizations(ctx, lite.ListOrganizationsParams{
		Limit:  p.Limit,
		Offset: p.Offset,
	})
	if err != nil {
		return nil, err
	}

	out := make([]model.Organization, len(rows))
	for i, r := range rows {
		out[i] = model.Organization(r)
	}

	return out, nil
}

func (a *liteQuerier) UpdateOrganization(
	ctx context.Context, p model.UpdateOrganizationParams,
) (model.Organization, error) {
	row, err := a.q.UpdateOrganization(ctx, lite.UpdateOrganizationParams(p))

	return model.Organization(row), err
}

func (a *liteQuerier) SoftDeleteOrganization(
	ctx context.Context, p model.SoftDeleteOrganizationParams,
) (int64, error) {
	return a.q.SoftDeleteOrganization(ctx, lite.SoftDeleteOrganizationParams(p))
}

func (a *liteQuerier) CountOrganizations(ctx context.Context) (int64, error) {
	return a.q.CountOrganizations(ctx)
}

func (a *liteQuerier) CreateUser(ctx context.Context, p model.CreateUserParams) (model.User, error) {
	row, err := a.q.CreateUser(ctx, lite.CreateUserParams(p))

	return model.User(row), err
}

func (a *liteQuerier) GetUser(ctx context.Context, p model.GetUserParams) (model.User, error) {
	row, err := a.q.GetUser(ctx, lite.GetUserParams(p))

	return model.User(row), err
}

func (a *liteQuerier) GetUserByEmail(ctx context.Context, p model.GetUserByEmailParams) (model.User, error) {
	row, err := a.q.GetUserByEmail(ctx, lite.GetUserByEmailParams(p))

	return model.User(row), err
}

func (a *liteQuerier) ListUsers(ctx context.Context, p model.ListUsersParams) ([]model.User, error) {
	rows, err := a.q.ListUsers(ctx, lite.ListUsersParams{
		OrgID:  p.OrgID,
		Limit:  p.Limit,
		Offset: p.Offset,
	})
	if err != nil {
		return nil, err
	}

	out := make([]model.User, len(rows))
	for i, r := range rows {
		out[i] = model.User(r)
	}

	return out, nil
}

func (a *liteQuerier) UpdateUser(ctx context.Context, p model.UpdateUserParams) (model.User, error) {
	row, err := a.q.UpdateUser(ctx, lite.UpdateUserParams(p))

	return model.User(row), err
}

func (a *liteQuerier) UpdateUserPassword(ctx context.Context, p model.UpdateUserPasswordParams) (int64, error) {
	return a.q.UpdateUserPassword(ctx, lite.UpdateUserPasswordParams(p))
}

func (a *liteQuerier) RecordUserLogin(ctx context.Context, p model.RecordUserLoginParams) (int64, error) {
	return a.q.RecordUserLogin(ctx, lite.RecordUserLoginParams(p))
}

func (a *liteQuerier) SoftDeleteUser(ctx context.Context, p model.SoftDeleteUserParams) (int64, error) {
	return a.q.SoftDeleteUser(ctx, lite.SoftDeleteUserParams(p))
}

func (a *liteQuerier) CountUsers(ctx context.Context, orgID uuid.UUID) (int64, error) {
	return a.q.CountUsers(ctx, orgID)
}

// --- groups and attributes: PostgreSQL ------------------------------------

func (a *pgQuerier) CreateGroup(ctx context.Context, p model.CreateGroupParams) (model.Group, error) {
	row, err := a.q.CreateGroup(ctx, pg.CreateGroupParams(p))

	return model.Group(row), err
}

func (a *pgQuerier) GetGroup(ctx context.Context, p model.GetGroupParams) (model.Group, error) {
	row, err := a.q.GetGroup(ctx, pg.GetGroupParams(p))

	return model.Group(row), err
}

func (a *pgQuerier) GetGroupByName(ctx context.Context, p model.GetGroupByNameParams) (model.Group, error) {
	row, err := a.q.GetGroupByName(ctx, pg.GetGroupByNameParams(p))

	return model.Group(row), err
}

func (a *pgQuerier) ListGroups(ctx context.Context, p model.ListGroupsParams) ([]model.Group, error) {
	rows, err := a.q.ListGroups(ctx, pg.ListGroupsParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.Group, len(rows))
	for i, r := range rows {
		out[i] = model.Group(r)
	}

	return out, nil
}

func (a *pgQuerier) ListChildGroups(ctx context.Context, p model.ListChildGroupsParams) ([]model.Group, error) {
	rows, err := a.q.ListChildGroups(ctx, pg.ListChildGroupsParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.Group, len(rows))
	for i, r := range rows {
		out[i] = model.Group(r)
	}

	return out, nil
}

func (a *pgQuerier) UpdateGroup(ctx context.Context, p model.UpdateGroupParams) (model.Group, error) {
	row, err := a.q.UpdateGroup(ctx, pg.UpdateGroupParams(p))

	return model.Group(row), err
}

func (a *pgQuerier) SoftDeleteGroup(ctx context.Context, p model.SoftDeleteGroupParams) (int64, error) {
	return a.q.SoftDeleteGroup(ctx, pg.SoftDeleteGroupParams(p))
}

func (a *pgQuerier) AddGroupMember(ctx context.Context, p model.AddGroupMemberParams) error {
	return a.q.AddGroupMember(ctx, pg.AddGroupMemberParams(p))
}

func (a *pgQuerier) RemoveGroupMember(ctx context.Context, p model.RemoveGroupMemberParams) (int64, error) {
	return a.q.RemoveGroupMember(ctx, pg.RemoveGroupMemberParams(p))
}

func (a *pgQuerier) ListGroupMembers(ctx context.Context, p model.ListGroupMembersParams) ([]model.User, error) {
	rows, err := a.q.ListGroupMembers(ctx, pg.ListGroupMembersParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.User, len(rows))
	for i, r := range rows {
		out[i] = model.User(r)
	}

	return out, nil
}

func (a *pgQuerier) ListUserGroups(ctx context.Context, p model.ListUserGroupsParams) ([]model.Group, error) {
	rows, err := a.q.ListUserGroups(ctx, pg.ListUserGroupsParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.Group, len(rows))
	for i, r := range rows {
		out[i] = model.Group(r)
	}

	return out, nil
}

func (a *pgQuerier) IsGroupMember(ctx context.Context, p model.IsGroupMemberParams) (bool, error) {
	return a.q.IsGroupMember(ctx, pg.IsGroupMemberParams(p))
}

func (a *pgQuerier) UpsertUserAttribute(ctx context.Context, p model.UpsertUserAttributeParams) (model.UserAttribute, error) {
	row, err := a.q.UpsertUserAttribute(ctx, pg.UpsertUserAttributeParams(p))

	return model.UserAttribute(row), err
}

func (a *pgQuerier) GetUserAttribute(ctx context.Context, p model.GetUserAttributeParams) (model.UserAttribute, error) {
	row, err := a.q.GetUserAttribute(ctx, pg.GetUserAttributeParams(p))

	return model.UserAttribute(row), err
}

func (a *pgQuerier) ListUserAttributes(ctx context.Context, p model.ListUserAttributesParams) ([]model.UserAttribute, error) {
	rows, err := a.q.ListUserAttributes(ctx, pg.ListUserAttributesParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.UserAttribute, len(rows))
	for i, r := range rows {
		out[i] = model.UserAttribute(r)
	}

	return out, nil
}

func (a *pgQuerier) DeleteUserAttribute(ctx context.Context, p model.DeleteUserAttributeParams) (int64, error) {
	return a.q.DeleteUserAttribute(ctx, pg.DeleteUserAttributeParams(p))
}

func (a *pgQuerier) DeleteUserAttributesBySource(ctx context.Context, p model.DeleteUserAttributesBySourceParams) (int64, error) {
	return a.q.DeleteUserAttributesBySource(ctx, pg.DeleteUserAttributesBySourceParams(p))
}

// --- groups and attributes: SQLite ----------------------------------------

func (a *liteQuerier) CreateGroup(ctx context.Context, p model.CreateGroupParams) (model.Group, error) {
	row, err := a.q.CreateGroup(ctx, lite.CreateGroupParams(p))

	return model.Group(row), err
}

func (a *liteQuerier) GetGroup(ctx context.Context, p model.GetGroupParams) (model.Group, error) {
	row, err := a.q.GetGroup(ctx, lite.GetGroupParams(p))

	return model.Group(row), err
}

func (a *liteQuerier) GetGroupByName(ctx context.Context, p model.GetGroupByNameParams) (model.Group, error) {
	row, err := a.q.GetGroupByName(ctx, lite.GetGroupByNameParams(p))

	return model.Group(row), err
}

func (a *liteQuerier) ListGroups(ctx context.Context, p model.ListGroupsParams) ([]model.Group, error) {
	rows, err := a.q.ListGroups(ctx, lite.ListGroupsParams{OrgID: p.OrgID, Limit: p.Limit, Offset: p.Offset})
	if err != nil {
		return nil, err
	}

	out := make([]model.Group, len(rows))
	for i, r := range rows {
		out[i] = model.Group(r)
	}

	return out, nil
}

func (a *liteQuerier) ListChildGroups(ctx context.Context, p model.ListChildGroupsParams) ([]model.Group, error) {
	rows, err := a.q.ListChildGroups(ctx, lite.ListChildGroupsParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.Group, len(rows))
	for i, r := range rows {
		out[i] = model.Group(r)
	}

	return out, nil
}

func (a *liteQuerier) UpdateGroup(ctx context.Context, p model.UpdateGroupParams) (model.Group, error) {
	row, err := a.q.UpdateGroup(ctx, lite.UpdateGroupParams(p))

	return model.Group(row), err
}

func (a *liteQuerier) SoftDeleteGroup(ctx context.Context, p model.SoftDeleteGroupParams) (int64, error) {
	return a.q.SoftDeleteGroup(ctx, lite.SoftDeleteGroupParams(p))
}

func (a *liteQuerier) AddGroupMember(ctx context.Context, p model.AddGroupMemberParams) error {
	return a.q.AddGroupMember(ctx, lite.AddGroupMemberParams(p))
}

func (a *liteQuerier) RemoveGroupMember(ctx context.Context, p model.RemoveGroupMemberParams) (int64, error) {
	return a.q.RemoveGroupMember(ctx, lite.RemoveGroupMemberParams(p))
}

func (a *liteQuerier) ListGroupMembers(ctx context.Context, p model.ListGroupMembersParams) ([]model.User, error) {
	rows, err := a.q.ListGroupMembers(ctx, lite.ListGroupMembersParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.User, len(rows))
	for i, r := range rows {
		out[i] = model.User(r)
	}

	return out, nil
}

func (a *liteQuerier) ListUserGroups(ctx context.Context, p model.ListUserGroupsParams) ([]model.Group, error) {
	rows, err := a.q.ListUserGroups(ctx, lite.ListUserGroupsParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.Group, len(rows))
	for i, r := range rows {
		out[i] = model.Group(r)
	}

	return out, nil
}

func (a *liteQuerier) IsGroupMember(ctx context.Context, p model.IsGroupMemberParams) (bool, error) {
	return a.q.IsGroupMember(ctx, lite.IsGroupMemberParams(p))
}

func (a *liteQuerier) UpsertUserAttribute(ctx context.Context, p model.UpsertUserAttributeParams) (model.UserAttribute, error) {
	row, err := a.q.UpsertUserAttribute(ctx, lite.UpsertUserAttributeParams(p))

	return model.UserAttribute(row), err
}

func (a *liteQuerier) GetUserAttribute(ctx context.Context, p model.GetUserAttributeParams) (model.UserAttribute, error) {
	row, err := a.q.GetUserAttribute(ctx, lite.GetUserAttributeParams(p))

	return model.UserAttribute(row), err
}

func (a *liteQuerier) ListUserAttributes(ctx context.Context, p model.ListUserAttributesParams) ([]model.UserAttribute, error) {
	rows, err := a.q.ListUserAttributes(ctx, lite.ListUserAttributesParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.UserAttribute, len(rows))
	for i, r := range rows {
		out[i] = model.UserAttribute(r)
	}

	return out, nil
}

func (a *liteQuerier) DeleteUserAttribute(ctx context.Context, p model.DeleteUserAttributeParams) (int64, error) {
	return a.q.DeleteUserAttribute(ctx, lite.DeleteUserAttributeParams(p))
}

func (a *liteQuerier) DeleteUserAttributesBySource(ctx context.Context, p model.DeleteUserAttributesBySourceParams) (int64, error) {
	return a.q.DeleteUserAttributesBySource(ctx, lite.DeleteUserAttributesBySourceParams(p))
}

// --- sessions and login attempts: PostgreSQL ------------------------------

func (a *pgQuerier) GetSessionByTokenHash(ctx context.Context, hash string) (model.Session, error) {
	row, err := a.q.GetSessionByTokenHash(ctx, hash)

	return model.Session(row), err
}

func (a *pgQuerier) CreateSession(ctx context.Context, p model.CreateSessionParams) (model.Session, error) {
	row, err := a.q.CreateSession(ctx, pg.CreateSessionParams(p))

	return model.Session(row), err
}

func (a *pgQuerier) TouchSession(ctx context.Context, p model.TouchSessionParams) (int64, error) {
	return a.q.TouchSession(ctx, pg.TouchSessionParams(p))
}

func (a *pgQuerier) RevokeSession(ctx context.Context, p model.RevokeSessionParams) (int64, error) {
	return a.q.RevokeSession(ctx, pg.RevokeSessionParams(p))
}

func (a *pgQuerier) RevokeSessionForUser(ctx context.Context, p model.RevokeSessionForUserParams) (int64, error) {
	return a.q.RevokeSessionForUser(ctx, pg.RevokeSessionForUserParams(p))
}

func (a *pgQuerier) RevokeUserSessions(ctx context.Context, p model.RevokeUserSessionsParams) (int64, error) {
	return a.q.RevokeUserSessions(ctx, pg.RevokeUserSessionsParams(p))
}

func (a *pgQuerier) RevokeOtherUserSessions(
	ctx context.Context, p model.RevokeOtherUserSessionsParams,
) (int64, error) {
	return a.q.RevokeOtherUserSessions(ctx, pg.RevokeOtherUserSessionsParams(p))
}

func (a *pgQuerier) ListUserSessions(ctx context.Context, p model.ListUserSessionsParams) ([]model.Session, error) {
	rows, err := a.q.ListUserSessions(ctx, pg.ListUserSessionsParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.Session, len(rows))
	for i, r := range rows {
		out[i] = model.Session(r)
	}

	return out, nil
}

func (a *pgQuerier) DeleteExpiredSessions(ctx context.Context, p model.DeleteExpiredSessionsParams) (int64, error) {
	return a.q.DeleteExpiredSessions(ctx, pg.DeleteExpiredSessionsParams(p))
}

func (a *pgQuerier) GetLoginAttempt(ctx context.Context, p model.GetLoginAttemptParams) (model.LoginAttempt, error) {
	row, err := a.q.GetLoginAttempt(ctx, pg.GetLoginAttemptParams(p))

	return model.LoginAttempt(row), err
}

func (a *pgQuerier) RecordFailedLogin(ctx context.Context, p model.RecordFailedLoginParams) (model.LoginAttempt, error) {
	row, err := a.q.RecordFailedLogin(ctx, pg.RecordFailedLoginParams(p))

	return model.LoginAttempt(row), err
}

func (a *pgQuerier) SetLoginLock(ctx context.Context, p model.SetLoginLockParams) (int64, error) {
	return a.q.SetLoginLock(ctx, pg.SetLoginLockParams(p))
}

func (a *pgQuerier) ClearLoginAttempts(ctx context.Context, p model.ClearLoginAttemptsParams) (int64, error) {
	return a.q.ClearLoginAttempts(ctx, pg.ClearLoginAttemptsParams(p))
}

func (a *pgQuerier) DeleteStaleLoginAttempts(ctx context.Context, p model.DeleteStaleLoginAttemptsParams) (int64, error) {
	return a.q.DeleteStaleLoginAttempts(ctx, pg.DeleteStaleLoginAttemptsParams(p))
}

// --- sessions and login attempts: SQLite -----------------------------------

func (a *liteQuerier) GetSessionByTokenHash(ctx context.Context, hash string) (model.Session, error) {
	row, err := a.q.GetSessionByTokenHash(ctx, hash)

	return model.Session(row), err
}

func (a *liteQuerier) CreateSession(ctx context.Context, p model.CreateSessionParams) (model.Session, error) {
	row, err := a.q.CreateSession(ctx, lite.CreateSessionParams(p))

	return model.Session(row), err
}

func (a *liteQuerier) TouchSession(ctx context.Context, p model.TouchSessionParams) (int64, error) {
	return a.q.TouchSession(ctx, lite.TouchSessionParams(p))
}

func (a *liteQuerier) RevokeSession(ctx context.Context, p model.RevokeSessionParams) (int64, error) {
	return a.q.RevokeSession(ctx, lite.RevokeSessionParams(p))
}

func (a *liteQuerier) RevokeSessionForUser(ctx context.Context, p model.RevokeSessionForUserParams) (int64, error) {
	return a.q.RevokeSessionForUser(ctx, lite.RevokeSessionForUserParams(p))
}

func (a *liteQuerier) RevokeUserSessions(ctx context.Context, p model.RevokeUserSessionsParams) (int64, error) {
	return a.q.RevokeUserSessions(ctx, lite.RevokeUserSessionsParams(p))
}

func (a *liteQuerier) RevokeOtherUserSessions(
	ctx context.Context, p model.RevokeOtherUserSessionsParams,
) (int64, error) {
	return a.q.RevokeOtherUserSessions(ctx, lite.RevokeOtherUserSessionsParams(p))
}

func (a *liteQuerier) ListUserSessions(ctx context.Context, p model.ListUserSessionsParams) ([]model.Session, error) {
	rows, err := a.q.ListUserSessions(ctx, lite.ListUserSessionsParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.Session, len(rows))
	for i, r := range rows {
		out[i] = model.Session(r)
	}

	return out, nil
}

func (a *liteQuerier) DeleteExpiredSessions(ctx context.Context, p model.DeleteExpiredSessionsParams) (int64, error) {
	return a.q.DeleteExpiredSessions(ctx, lite.DeleteExpiredSessionsParams(p))
}

func (a *liteQuerier) GetLoginAttempt(ctx context.Context, p model.GetLoginAttemptParams) (model.LoginAttempt, error) {
	row, err := a.q.GetLoginAttempt(ctx, lite.GetLoginAttemptParams(p))

	return model.LoginAttempt(row), err
}

func (a *liteQuerier) RecordFailedLogin(ctx context.Context, p model.RecordFailedLoginParams) (model.LoginAttempt, error) {
	row, err := a.q.RecordFailedLogin(ctx, lite.RecordFailedLoginParams(p))

	return model.LoginAttempt(row), err
}

func (a *liteQuerier) SetLoginLock(ctx context.Context, p model.SetLoginLockParams) (int64, error) {
	return a.q.SetLoginLock(ctx, lite.SetLoginLockParams(p))
}

func (a *liteQuerier) ClearLoginAttempts(ctx context.Context, p model.ClearLoginAttemptsParams) (int64, error) {
	return a.q.ClearLoginAttempts(ctx, lite.ClearLoginAttemptsParams(p))
}

func (a *liteQuerier) DeleteStaleLoginAttempts(ctx context.Context, p model.DeleteStaleLoginAttemptsParams) (int64, error) {
	return a.q.DeleteStaleLoginAttempts(ctx, lite.DeleteStaleLoginAttemptsParams(p))
}

// --- role assignments (postgres) ------------------------------------------

func (a *pgQuerier) ListObjectGrants(ctx context.Context, p model.ListObjectGrantsParams) ([]model.RoleAssignment, error) {
	rows, err := a.q.ListObjectGrants(ctx, pg.ListObjectGrantsParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.RoleAssignment, len(rows))
	for i, r := range rows {
		out[i] = model.RoleAssignment(r)
	}

	return out, nil
}

func (a *pgQuerier) ListSubjectGrants(ctx context.Context, p model.ListSubjectGrantsParams) ([]model.RoleAssignment, error) {
	rows, err := a.q.ListSubjectGrants(ctx, pg.ListSubjectGrantsParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.RoleAssignment, len(rows))
	for i, r := range rows {
		out[i] = model.RoleAssignment(r)
	}

	return out, nil
}

func (a *pgQuerier) ListGrantsOnObject(ctx context.Context, p model.ListGrantsOnObjectParams) ([]model.RoleAssignment, error) {
	rows, err := a.q.ListGrantsOnObject(ctx, pg.ListGrantsOnObjectParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.RoleAssignment, len(rows))
	for i, r := range rows {
		out[i] = model.RoleAssignment(r)
	}

	return out, nil
}

func (a *pgQuerier) GrantRole(ctx context.Context, p model.GrantRoleParams) error {
	return a.q.GrantRole(ctx, pg.GrantRoleParams(p))
}

func (a *pgQuerier) RevokeRole(ctx context.Context, p model.RevokeRoleParams) (int64, error) {
	return a.q.RevokeRole(ctx, pg.RevokeRoleParams(p))
}

func (a *pgQuerier) RevokeAllForSubject(ctx context.Context, p model.RevokeAllForSubjectParams) (int64, error) {
	return a.q.RevokeAllForSubject(ctx, pg.RevokeAllForSubjectParams(p))
}

func (a *pgQuerier) CountRoleHolders(ctx context.Context, p model.CountRoleHoldersParams) (int64, error) {
	return a.q.CountRoleHolders(ctx, pg.CountRoleHoldersParams(p))
}

// --- role assignments (sqlite) --------------------------------------------

func (a *liteQuerier) ListObjectGrants(ctx context.Context, p model.ListObjectGrantsParams) ([]model.RoleAssignment, error) {
	rows, err := a.q.ListObjectGrants(ctx, lite.ListObjectGrantsParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.RoleAssignment, len(rows))
	for i, r := range rows {
		out[i] = model.RoleAssignment(r)
	}

	return out, nil
}

func (a *liteQuerier) ListSubjectGrants(ctx context.Context, p model.ListSubjectGrantsParams) ([]model.RoleAssignment, error) {
	rows, err := a.q.ListSubjectGrants(ctx, lite.ListSubjectGrantsParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.RoleAssignment, len(rows))
	for i, r := range rows {
		out[i] = model.RoleAssignment(r)
	}

	return out, nil
}

func (a *liteQuerier) ListGrantsOnObject(ctx context.Context, p model.ListGrantsOnObjectParams) ([]model.RoleAssignment, error) {
	rows, err := a.q.ListGrantsOnObject(ctx, lite.ListGrantsOnObjectParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.RoleAssignment, len(rows))
	for i, r := range rows {
		out[i] = model.RoleAssignment(r)
	}

	return out, nil
}

func (a *liteQuerier) GrantRole(ctx context.Context, p model.GrantRoleParams) error {
	return a.q.GrantRole(ctx, lite.GrantRoleParams(p))
}

func (a *liteQuerier) RevokeRole(ctx context.Context, p model.RevokeRoleParams) (int64, error) {
	return a.q.RevokeRole(ctx, lite.RevokeRoleParams(p))
}

func (a *liteQuerier) RevokeAllForSubject(ctx context.Context, p model.RevokeAllForSubjectParams) (int64, error) {
	return a.q.RevokeAllForSubject(ctx, lite.RevokeAllForSubjectParams(p))
}

func (a *liteQuerier) CountRoleHolders(ctx context.Context, p model.CountRoleHoldersParams) (int64, error) {
	return a.q.CountRoleHolders(ctx, lite.CountRoleHoldersParams(p))
}

// --- identity providers (postgres) -----------------------------------------

func (a *pgQuerier) CreateIdentityProvider(ctx context.Context, p model.CreateIdentityProviderParams) (model.IdentityProvider, error) {
	row, err := a.q.CreateIdentityProvider(ctx, pg.CreateIdentityProviderParams(p))

	return model.IdentityProvider(row), err
}

func (a *pgQuerier) GetIdentityProvider(ctx context.Context, p model.GetIdentityProviderParams) (model.IdentityProvider, error) {
	row, err := a.q.GetIdentityProvider(ctx, pg.GetIdentityProviderParams(p))

	return model.IdentityProvider(row), err
}

func (a *pgQuerier) GetIdentityProviderBySlug(ctx context.Context, p model.GetIdentityProviderBySlugParams) (model.IdentityProvider, error) {
	row, err := a.q.GetIdentityProviderBySlug(ctx, pg.GetIdentityProviderBySlugParams(p))

	return model.IdentityProvider(row), err
}

func (a *pgQuerier) ListIdentityProviders(ctx context.Context, orgID uuid.UUID) ([]model.IdentityProvider, error) {
	rows, err := a.q.ListIdentityProviders(ctx, orgID)
	if err != nil {
		return nil, err
	}

	out := make([]model.IdentityProvider, len(rows))
	for i, r := range rows {
		out[i] = model.IdentityProvider(r)
	}

	return out, nil
}

func (a *pgQuerier) UpdateIdentityProvider(ctx context.Context, p model.UpdateIdentityProviderParams) (model.IdentityProvider, error) {
	row, err := a.q.UpdateIdentityProvider(ctx, pg.UpdateIdentityProviderParams(p))

	return model.IdentityProvider(row), err
}

func (a *pgQuerier) SoftDeleteIdentityProvider(ctx context.Context, p model.SoftDeleteIdentityProviderParams) (int64, error) {
	return a.q.SoftDeleteIdentityProvider(ctx, pg.SoftDeleteIdentityProviderParams(p))
}

func (a *pgQuerier) GetFederatedIdentity(ctx context.Context, p model.GetFederatedIdentityParams) (model.FederatedIdentity, error) {
	row, err := a.q.GetFederatedIdentity(ctx, pg.GetFederatedIdentityParams(p))

	return model.FederatedIdentity(row), err
}

func (a *pgQuerier) ListFederatedIdentitiesForUser(ctx context.Context, p model.ListFederatedIdentitiesForUserParams) ([]model.FederatedIdentity, error) {
	rows, err := a.q.ListFederatedIdentitiesForUser(ctx, pg.ListFederatedIdentitiesForUserParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.FederatedIdentity, len(rows))
	for i, r := range rows {
		out[i] = model.FederatedIdentity(r)
	}

	return out, nil
}

func (a *pgQuerier) LinkFederatedIdentity(ctx context.Context, p model.LinkFederatedIdentityParams) (model.FederatedIdentity, error) {
	row, err := a.q.LinkFederatedIdentity(ctx, pg.LinkFederatedIdentityParams(p))

	return model.FederatedIdentity(row), err
}

func (a *pgQuerier) RecordFederatedLogin(ctx context.Context, p model.RecordFederatedLoginParams) (int64, error) {
	return a.q.RecordFederatedLogin(ctx, pg.RecordFederatedLoginParams(p))
}

// --- identity providers (sqlite) -----------------------------------------

func (a *liteQuerier) CreateIdentityProvider(ctx context.Context, p model.CreateIdentityProviderParams) (model.IdentityProvider, error) {
	row, err := a.q.CreateIdentityProvider(ctx, lite.CreateIdentityProviderParams(p))

	return model.IdentityProvider(row), err
}

func (a *liteQuerier) GetIdentityProvider(ctx context.Context, p model.GetIdentityProviderParams) (model.IdentityProvider, error) {
	row, err := a.q.GetIdentityProvider(ctx, lite.GetIdentityProviderParams(p))

	return model.IdentityProvider(row), err
}

func (a *liteQuerier) GetIdentityProviderBySlug(ctx context.Context, p model.GetIdentityProviderBySlugParams) (model.IdentityProvider, error) {
	row, err := a.q.GetIdentityProviderBySlug(ctx, lite.GetIdentityProviderBySlugParams(p))

	return model.IdentityProvider(row), err
}

func (a *liteQuerier) ListIdentityProviders(ctx context.Context, orgID uuid.UUID) ([]model.IdentityProvider, error) {
	rows, err := a.q.ListIdentityProviders(ctx, orgID)
	if err != nil {
		return nil, err
	}

	out := make([]model.IdentityProvider, len(rows))
	for i, r := range rows {
		out[i] = model.IdentityProvider(r)
	}

	return out, nil
}

func (a *liteQuerier) UpdateIdentityProvider(ctx context.Context, p model.UpdateIdentityProviderParams) (model.IdentityProvider, error) {
	row, err := a.q.UpdateIdentityProvider(ctx, lite.UpdateIdentityProviderParams(p))

	return model.IdentityProvider(row), err
}

func (a *liteQuerier) SoftDeleteIdentityProvider(ctx context.Context, p model.SoftDeleteIdentityProviderParams) (int64, error) {
	return a.q.SoftDeleteIdentityProvider(ctx, lite.SoftDeleteIdentityProviderParams(p))
}

func (a *liteQuerier) GetFederatedIdentity(ctx context.Context, p model.GetFederatedIdentityParams) (model.FederatedIdentity, error) {
	row, err := a.q.GetFederatedIdentity(ctx, lite.GetFederatedIdentityParams(p))

	return model.FederatedIdentity(row), err
}

func (a *liteQuerier) ListFederatedIdentitiesForUser(ctx context.Context, p model.ListFederatedIdentitiesForUserParams) ([]model.FederatedIdentity, error) {
	rows, err := a.q.ListFederatedIdentitiesForUser(ctx, lite.ListFederatedIdentitiesForUserParams(p))
	if err != nil {
		return nil, err
	}

	out := make([]model.FederatedIdentity, len(rows))
	for i, r := range rows {
		out[i] = model.FederatedIdentity(r)
	}

	return out, nil
}

func (a *liteQuerier) LinkFederatedIdentity(ctx context.Context, p model.LinkFederatedIdentityParams) (model.FederatedIdentity, error) {
	row, err := a.q.LinkFederatedIdentity(ctx, lite.LinkFederatedIdentityParams(p))

	return model.FederatedIdentity(row), err
}

func (a *liteQuerier) RecordFederatedLogin(ctx context.Context, p model.RecordFederatedLoginParams) (int64, error) {
	return a.q.RecordFederatedLogin(ctx, lite.RecordFederatedLoginParams(p))
}
