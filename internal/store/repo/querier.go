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
}
