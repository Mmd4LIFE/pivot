// Package repo is the tenant-scoped data access layer.
//
// Its defining property: no method takes an organization as a parameter. The
// tenant comes from [tenant.Scope] in the context, so a caller cannot pass the
// wrong one — there is nothing to pass. A context without a scope is an error
// before any SQL runs, never an unscoped query.
//
// Soft-delete filtering, optimistic concurrency and change events are applied
// here too, so no call site has to remember them.
package repo

import (
	"context"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

// Querier is the engine-neutral query surface the repositories are written
// against. Both generated packages are adapted onto it.
//
// It speaks in [model] types rather than either engine's generated structs, so
// nothing above this line knows which database is underneath.
type Querier interface {
	// Organizations. These take an ID rather than an org scope because an
	// organization IS the tenant; the scoping happens in the repository, which
	// only ever passes the scope's own ID.
	CreateOrganization(context.Context, model.CreateOrganizationParams) (model.Organization, error)
	GetOrganization(context.Context, uuid.UUID) (model.Organization, error)
	GetOrganizationBySlug(context.Context, string) (model.Organization, error)
	ListOrganizations(context.Context, model.ListOrganizationsParams) ([]model.Organization, error)
	UpdateOrganization(context.Context, model.UpdateOrganizationParams) (model.Organization, error)
	SoftDeleteOrganization(context.Context, model.SoftDeleteOrganizationParams) (int64, error)
	CountOrganizations(context.Context) (int64, error)

	// Users. Every one of these carries OrgID in its parameters, and the
	// repository is the only thing that fills it in.
	CreateUser(context.Context, model.CreateUserParams) (model.User, error)
	GetUser(context.Context, model.GetUserParams) (model.User, error)
	GetUserByEmail(context.Context, model.GetUserByEmailParams) (model.User, error)
	ListUsers(context.Context, model.ListUsersParams) ([]model.User, error)
	UpdateUser(context.Context, model.UpdateUserParams) (model.User, error)
	UpdateUserPassword(context.Context, model.UpdateUserPasswordParams) (int64, error)
	RecordUserLogin(context.Context, model.RecordUserLoginParams) (int64, error)
	SoftDeleteUser(context.Context, model.SoftDeleteUserParams) (int64, error)
	CountUsers(context.Context, uuid.UUID) (int64, error)

	// Groups, including nesting and membership.
	CreateGroup(context.Context, model.CreateGroupParams) (model.Group, error)
	GetGroup(context.Context, model.GetGroupParams) (model.Group, error)
	GetGroupByName(context.Context, model.GetGroupByNameParams) (model.Group, error)
	ListGroups(context.Context, model.ListGroupsParams) ([]model.Group, error)
	ListChildGroups(context.Context, model.ListChildGroupsParams) ([]model.Group, error)
	UpdateGroup(context.Context, model.UpdateGroupParams) (model.Group, error)
	SoftDeleteGroup(context.Context, model.SoftDeleteGroupParams) (int64, error)
	AddGroupMember(context.Context, model.AddGroupMemberParams) error
	RemoveGroupMember(context.Context, model.RemoveGroupMemberParams) (int64, error)
	ListGroupMembers(context.Context, model.ListGroupMembersParams) ([]model.User, error)
	ListUserGroups(context.Context, model.ListUserGroupsParams) ([]model.Group, error)
	IsGroupMember(context.Context, model.IsGroupMemberParams) (bool, error)

	// User attributes. These feed row-level security in Phase 4, which is why
	// their provenance is carried explicitly rather than inferred.
	UpsertUserAttribute(context.Context, model.UpsertUserAttributeParams) (model.UserAttribute, error)
	GetUserAttribute(context.Context, model.GetUserAttributeParams) (model.UserAttribute, error)
	ListUserAttributes(context.Context, model.ListUserAttributesParams) ([]model.UserAttribute, error)
	DeleteUserAttribute(context.Context, model.DeleteUserAttributeParams) (int64, error)
	DeleteUserAttributesBySource(context.Context, model.DeleteUserAttributesBySourceParams) (int64, error)

	// Sessions. GetSessionByTokenHash is deliberately unscoped: resolving a
	// session is how the tenant is discovered, so it cannot require knowing
	// the tenant already.
	CreateSession(context.Context, model.CreateSessionParams) (model.Session, error)
	GetSessionByTokenHash(context.Context, string) (model.Session, error)
	TouchSession(context.Context, model.TouchSessionParams) (int64, error)
	RevokeSession(context.Context, model.RevokeSessionParams) (int64, error)
	RevokeSessionForUser(context.Context, model.RevokeSessionForUserParams) (int64, error)
	RevokeUserSessions(context.Context, model.RevokeUserSessionsParams) (int64, error)
	ListUserSessions(context.Context, model.ListUserSessionsParams) ([]model.Session, error)
	DeleteExpiredSessions(context.Context, model.DeleteExpiredSessionsParams) (int64, error)

	// Role assignments: Zanzibar tuples. ListObjectGrants deliberately returns
	// a widened set — the user's own grants plus every group grant on the
	// object — because a variable-length IN list is not portable across both
	// engines. The caller intersects.
	ListObjectGrants(context.Context, model.ListObjectGrantsParams) ([]model.RoleAssignment, error)
	ListSubjectGrants(context.Context, model.ListSubjectGrantsParams) ([]model.RoleAssignment, error)
	ListGrantsOnObject(context.Context, model.ListGrantsOnObjectParams) ([]model.RoleAssignment, error)
	GrantRole(context.Context, model.GrantRoleParams) error
	RevokeRole(context.Context, model.RevokeRoleParams) (int64, error)
	RevokeAllForSubject(context.Context, model.RevokeAllForSubjectParams) (int64, error)
	CountRoleHolders(context.Context, model.CountRoleHoldersParams) (int64, error)

	// Login attempts, for progressive lockout.
	GetLoginAttempt(context.Context, model.GetLoginAttemptParams) (model.LoginAttempt, error)
	RecordFailedLogin(context.Context, model.RecordFailedLoginParams) (model.LoginAttempt, error)
	SetLoginLock(context.Context, model.SetLoginLockParams) (int64, error)
	ClearLoginAttempts(context.Context, model.ClearLoginAttemptsParams) (int64, error)
	DeleteStaleLoginAttempts(context.Context, model.DeleteStaleLoginAttemptsParams) (int64, error)
}
