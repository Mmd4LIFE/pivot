package repo

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

const entityOrganization = "organization"

// OrganizationRepo reads and writes the caller's own organization.
//
// Note what is missing: no method takes an organization ID. An organization is
// the tenant, so "which organization?" is always answered by the scope. Acting
// on a different one is not a matter of passing a different argument — it
// requires a different scope, which is visible at the call site and in review.
//
// Provisioning a new organization has no tenant to scope to, so it lives on
// [SystemRepo], named to make its unscoped nature obvious.
type OrganizationRepo struct {
	base
}

// Current returns the caller's organization.
func (r *OrganizationRepo) Current(ctx context.Context) (model.Organization, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.Organization{}, err
	}

	org, err := r.q.GetOrganization(ctx, s.OrgID())
	if err != nil {
		return model.Organization{}, translate(err)
	}

	return org, nil
}

// UpdateOrganization describes a change to the caller's organization.
//
// Version is the value the caller read. A mismatch means someone else wrote
// first, and the update is rejected rather than silently overwriting them.
type UpdateOrganization struct {
	Name     string
	Slug     string
	Settings dbtypes.JSON
	Plan     string
	Version  int64
}

// Update modifies the caller's organization.
func (r *OrganizationRepo) Update(
	ctx context.Context, in UpdateOrganization,
) (model.Organization, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.Organization{}, err
	}

	org, err := r.q.UpdateOrganization(ctx, model.UpdateOrganizationParams{
		Name:      in.Name,
		Slug:      in.Slug,
		Settings:  in.Settings,
		Plan:      in.Plan,
		UpdatedBy: s.ActorID(),
		UpdatedAt: r.now(),
		ID:        s.OrgID(),
		Version:   in.Version,
	})
	if err != nil {
		// A versioned UPDATE that matches nothing is a conflict, not a
		// missing row: the caller's version is stale, or the row is deleted.
		if translated := translate(err); errors.Is(translated, ErrNotFound) {
			return model.Organization{}, ErrConflict
		} else if translated != nil {
			return model.Organization{}, translated
		}
	}

	r.emit(ctx, ChangeUpdated, entityOrganization, org.ID, org.ID, s.ActorID())

	return org, nil
}

// SoftDelete marks the caller's organization deleted.
//
// The row stays: recovering an organization deleted by mistake must be
// possible, and hard deletion would cascade through every table that
// references it.
func (r *OrganizationRepo) SoftDelete(ctx context.Context) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	n, err := r.q.SoftDeleteOrganization(ctx, model.SoftDeleteOrganizationParams{
		DeletedAt: dbtypes.NewNullTime(r.now().Time),
		UpdatedBy: s.ActorID(),
		ID:        s.OrgID(),
	})

	if derr := affectedOrNotFound(n, err); derr != nil {
		return derr
	}

	r.emit(ctx, ChangeDeleted, entityOrganization, s.OrgID(), s.OrgID(), s.ActorID())

	return nil
}

// SystemRepo holds operations that legitimately have no tenant.
//
// Provisioning and cross-tenant administration cannot be scoped, because there
// is no organization to scope to yet. Rather than weakening the scoped
// repositories to accommodate them, they live here under a name that makes
// every call site say what it is doing. Part 7 gates these behind an
// administrative permission.
type SystemRepo struct {
	q      Querier
	events *EventBus
}

// NewSystemRepo returns the unscoped repository.
func NewSystemRepo(q Querier, events *EventBus) *SystemRepo {
	return &SystemRepo{q: q, events: events}
}

// System returns the unscoped repository for provisioning.
func (r *Repositories) System() *SystemRepo { return NewSystemRepo(r.q, r.events) }

// CreateOrganization provisions a new tenant.
type CreateOrganization struct {
	Name     string
	Slug     string
	Settings dbtypes.JSON
	Plan     string

	// CreatedBy is the provisioning actor, if any. The first organization on a
	// fresh install has none, which is why this is nullable rather than taken
	// from a scope.
	CreatedBy uuid.NullUUID
}

// CreateOrganization creates a tenant and returns it.
func (r *SystemRepo) CreateOrganization(
	ctx context.Context, in CreateOrganization,
) (model.Organization, error) {
	settings := in.Settings
	if len(settings) == 0 {
		settings = dbtypes.JSON("{}")
	}

	plan := in.Plan
	if plan == "" {
		plan = "free"
	}

	org, err := r.q.CreateOrganization(ctx, model.CreateOrganizationParams{
		ID:        newID(),
		Name:      in.Name,
		Slug:      in.Slug,
		Settings:  settings,
		Plan:      plan,
		CreatedBy: in.CreatedBy,
		UpdatedBy: in.CreatedBy,
	})
	if err != nil {
		return model.Organization{}, translate(err)
	}

	if r.events != nil {
		r.events.publish(ctx, ChangeEvent{
			Kind:       ChangeCreated,
			EntityType: entityOrganization,
			EntityID:   org.ID,
			OrgID:      org.ID,
			ActorID:    in.CreatedBy,
			At:         dbtypes.Now(),
		})
	}

	return org, nil
}

// GetOrganizationBySlug looks up a tenant by slug, for login and routing —
// the one place a caller legitimately needs an organization before having a
// scope for it.
func (r *SystemRepo) GetOrganizationBySlug(ctx context.Context, slug string) (model.Organization, error) {
	org, err := r.q.GetOrganizationBySlug(ctx, slug)
	if err != nil {
		return model.Organization{}, translate(err)
	}

	return org, nil
}

// ListOrganizations returns tenants, for instance administration.
func (r *SystemRepo) ListOrganizations(ctx context.Context, limit, offset int64) ([]model.Organization, error) {
	if limit <= 0 {
		limit = 50
	}

	orgs, err := r.q.ListOrganizations(ctx, model.ListOrganizationsParams{Limit: limit, Offset: offset})
	if err != nil {
		return nil, translate(err)
	}

	return orgs, nil
}

// CountOrganizations returns the number of live tenants.
func (r *SystemRepo) CountOrganizations(ctx context.Context) (int64, error) {
	n, err := r.q.CountOrganizations(ctx)

	return n, translate(err)
}
