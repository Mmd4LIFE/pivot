package repo

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

const entityIdentityProvider = "identity_provider"

// IdentityProviderRepo manages an organization's SSO connections.
//
// Note what is *not* here: resolving a provider in order to start a login.
// That happens before the caller has a session, so it has no tenant to be
// scoped to and lives on [SystemRepo] where its unscoped nature is visible at
// the call site — the same arrangement as session resolution in Part 6-a.
type IdentityProviderRepo struct {
	base
}

// CreateIdentityProvider describes a new SSO connection.
type CreateIdentityProvider struct {
	Slug          string
	Name          string
	Issuer        string
	ClientID      string
	ClientSecret  string
	Scopes        string
	IsEnabled     bool
	AutoProvision bool

	// LinkByEmail adopts an existing local account whose address matches, on
	// first login. Off by default: see migration 00005 for why.
	LinkByEmail  bool
	DefaultRole  string
	ClaimMapping dbtypes.JSON
}

// Create adds a provider to the caller's organization.
func (r *IdentityProviderRepo) Create(
	ctx context.Context, in CreateIdentityProvider,
) (model.IdentityProvider, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.IdentityProvider{}, err
	}

	mapping := in.ClaimMapping
	if len(mapping) == 0 {
		mapping = dbtypes.JSON("{}")
	}

	scopes := in.Scopes
	if scopes == "" {
		scopes = "openid profile email"
	}

	role := in.DefaultRole
	if role == "" {
		role = "viewer"
	}

	provider, err := r.q.CreateIdentityProvider(ctx, model.CreateIdentityProviderParams{
		ID:            newID(),
		OrgID:         s.OrgID(),
		Slug:          in.Slug,
		Name:          in.Name,
		Kind:          "oidc",
		Issuer:        in.Issuer,
		ClientID:      in.ClientID,
		ClientSecret:  in.ClientSecret,
		Scopes:        scopes,
		IsEnabled:     dbtypes.Bool(in.IsEnabled),
		AutoProvision: dbtypes.Bool(in.AutoProvision),
		LinkByEmail:   dbtypes.Bool(in.LinkByEmail),
		DefaultRole:   role,
		ClaimMapping:  mapping,
		CreatedBy:     s.ActorID(),
		UpdatedBy:     s.ActorID(),
	})
	if err != nil {
		return model.IdentityProvider{}, translate(err)
	}

	r.emit(ctx, ChangeCreated, entityIdentityProvider, provider.ID, s.OrgID(), s.ActorID())

	return provider, nil
}

// Get returns one provider.
func (r *IdentityProviderRepo) Get(
	ctx context.Context, id uuid.UUID,
) (model.IdentityProvider, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.IdentityProvider{}, err
	}

	provider, err := r.q.GetIdentityProvider(ctx, model.GetIdentityProviderParams{
		ID: id, OrgID: s.OrgID(),
	})
	if err != nil {
		return model.IdentityProvider{}, translate(err)
	}

	return provider, nil
}

// GetBySlug returns a provider by its URL segment.
func (r *IdentityProviderRepo) GetBySlug(
	ctx context.Context, slug string,
) (model.IdentityProvider, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.IdentityProvider{}, err
	}

	provider, err := r.q.GetIdentityProviderBySlug(ctx, model.GetIdentityProviderBySlugParams{
		OrgID: s.OrgID(), Slug: slug,
	})
	if err != nil {
		return model.IdentityProvider{}, translate(err)
	}

	return provider, nil
}

// List returns every provider in the caller's organization.
func (r *IdentityProviderRepo) List(ctx context.Context) ([]model.IdentityProvider, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	providers, err := r.q.ListIdentityProviders(ctx, s.OrgID())
	if err != nil {
		return nil, translate(err)
	}

	return providers, nil
}

// UpdateIdentityProvider describes a change.
type UpdateIdentityProvider struct {
	ID            uuid.UUID
	Slug          string
	Name          string
	Issuer        string
	ClientID      string
	ClientSecret  string
	Scopes        string
	IsEnabled     bool
	AutoProvision bool
	LinkByEmail   bool
	DefaultRole   string
	ClaimMapping  dbtypes.JSON
	Version       int64
}

// Update modifies a provider.
func (r *IdentityProviderRepo) Update(
	ctx context.Context, in UpdateIdentityProvider,
) (model.IdentityProvider, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.IdentityProvider{}, err
	}

	mapping := in.ClaimMapping
	if len(mapping) == 0 {
		mapping = dbtypes.JSON("{}")
	}

	provider, err := r.q.UpdateIdentityProvider(ctx, model.UpdateIdentityProviderParams{
		Slug:          in.Slug,
		Name:          in.Name,
		Issuer:        in.Issuer,
		ClientID:      in.ClientID,
		ClientSecret:  in.ClientSecret,
		Scopes:        in.Scopes,
		IsEnabled:     dbtypes.Bool(in.IsEnabled),
		AutoProvision: dbtypes.Bool(in.AutoProvision),
		LinkByEmail:   dbtypes.Bool(in.LinkByEmail),
		DefaultRole:   in.DefaultRole,
		ClaimMapping:  mapping,
		UpdatedBy:     s.ActorID(),
		UpdatedAt:     r.now(),
		ID:            in.ID,
		OrgID:         s.OrgID(),
		Version:       in.Version,
	})
	if err != nil {
		// A versioned UPDATE that matches nothing is a conflict, not a missing
		// row: the caller's version is stale, or the row is deleted.
		if translated := translate(err); errors.Is(translated, ErrNotFound) {
			return model.IdentityProvider{}, ErrConflict
		} else if translated != nil {
			return model.IdentityProvider{}, translated
		}
	}

	r.emit(ctx, ChangeUpdated, entityIdentityProvider, provider.ID, s.OrgID(), s.ActorID())

	return provider, nil
}

// SoftDelete marks a provider deleted.
func (r *IdentityProviderRepo) SoftDelete(ctx context.Context, id uuid.UUID) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	n, derr := r.q.SoftDeleteIdentityProvider(ctx, model.SoftDeleteIdentityProviderParams{
		DeletedAt: dbtypes.NewNullTime(r.now().Time),
		UpdatedBy: s.ActorID(),
		ID:        id,
		OrgID:     s.OrgID(),
	})

	if serr := affectedOrNotFound(n, derr); serr != nil {
		return serr
	}

	r.emit(ctx, ChangeDeleted, entityIdentityProvider, id, s.OrgID(), s.ActorID())

	return nil
}

// LinkedIdentities returns a user's federated logins, for a profile screen
// that answers "which directory is this account tied to?".
func (r *IdentityProviderRepo) LinkedIdentities(
	ctx context.Context, userID uuid.UUID,
) ([]model.FederatedIdentity, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := r.q.ListFederatedIdentitiesForUser(ctx, model.ListFederatedIdentitiesForUserParams{
		OrgID: s.OrgID(), UserID: userID,
	})
	if err != nil {
		return nil, translate(err)
	}

	return rows, nil
}

// --- unscoped provider resolution -----------------------------------------

// FindIdentityProvider resolves a provider before the caller has a session.
//
// Unscoped by necessity, exactly like session resolution: starting an SSO
// login means looking up a provider in an organization the caller has not yet
// authenticated against. The organization is an explicit parameter, supplied
// by whoever resolved which tenant the login is for.
func (r *SystemRepo) FindIdentityProvider(
	ctx context.Context, orgID uuid.UUID, slug string,
) (model.IdentityProvider, error) {
	provider, err := r.q.GetIdentityProviderBySlug(ctx, model.GetIdentityProviderBySlugParams{
		OrgID: orgID, Slug: slug,
	})
	if err != nil {
		return model.IdentityProvider{}, translate(err)
	}

	return provider, nil
}

// ListEnabledProviders returns an organization's usable providers, for a login
// page that has to offer the buttons before anyone has logged in.
func (r *SystemRepo) ListEnabledProviders(
	ctx context.Context, orgID uuid.UUID,
) ([]model.IdentityProvider, error) {
	all, err := r.q.ListIdentityProviders(ctx, orgID)
	if err != nil {
		return nil, translate(err)
	}

	out := make([]model.IdentityProvider, 0, len(all))

	for _, p := range all {
		if p.IsEnabled.Bool() {
			out = append(out, p)
		}
	}

	return out, nil
}

// FindFederatedIdentity resolves an external subject to a Pivot user.
//
// Unscoped for the same reason: it runs mid-login, and its answer is what
// establishes which user the session will belong to.
func (r *SystemRepo) FindFederatedIdentity(
	ctx context.Context, providerID uuid.UUID, subject string,
) (model.FederatedIdentity, error) {
	identity, err := r.q.GetFederatedIdentity(ctx, model.GetFederatedIdentityParams{
		ProviderID: providerID, Subject: subject,
	})
	if err != nil {
		return model.FederatedIdentity{}, translate(err)
	}

	return identity, nil
}

// LinkFederatedIdentity records that an external subject is a Pivot user.
type LinkFederatedIdentity struct {
	OrgID      uuid.UUID
	ProviderID uuid.UUID
	UserID     uuid.UUID
	Subject    string
	At         dbtypes.NullTime
}

// LinkFederatedIdentity creates the link.
func (r *SystemRepo) LinkFederatedIdentity(
	ctx context.Context, in LinkFederatedIdentity,
) (model.FederatedIdentity, error) {
	identity, err := r.q.LinkFederatedIdentity(ctx, model.LinkFederatedIdentityParams{
		ID:          newID(),
		OrgID:       in.OrgID,
		ProviderID:  in.ProviderID,
		UserID:      in.UserID,
		Subject:     in.Subject,
		LastLoginAt: in.At,
	})
	if err != nil {
		return model.FederatedIdentity{}, translate(err)
	}

	return identity, nil
}

// RecordFederatedLogin stamps the last login on an existing link.
func (r *SystemRepo) RecordFederatedLogin(
	ctx context.Context, providerID uuid.UUID, subject string, at dbtypes.NullTime,
) error {
	n, err := r.q.RecordFederatedLogin(ctx, model.RecordFederatedLoginParams{
		LastLoginAt: at, ProviderID: providerID, Subject: subject,
	})

	return affectedOrNotFound(n, err)
}
