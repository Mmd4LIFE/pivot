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
	if db.IsPostgres() {
		return &pgQuerier{q: pg.New(db.DB)}
	}

	return &liteQuerier{q: lite.New(db.DB)}
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
