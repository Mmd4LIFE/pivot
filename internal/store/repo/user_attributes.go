package repo

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

const entityUserAttribute = "user_attribute"

// validSources mirrors the CHECK constraint in both schemas. Validating here
// as well turns a database error into a named one, and keeps the allowed set
// visible to anyone reading the Go.
var validSources = []string{
	model.SourceManual,
	model.SourceOIDC,
	model.SourceSAML,
	model.SourceSCIM,
}

// UserAttributeRepo manages key/value attributes on users.
//
// These feed row-level security in Phase 4: a policy like
// `region = {{ user.region }}` resolves against exactly these rows. That is why
// provenance is tracked explicitly — an identity provider sync must be able to
// replace the attributes it owns without disturbing ones an administrator set
// by hand, and the reverse.
type UserAttributeRepo struct {
	base
}

// SetAttribute describes an attribute to write.
type SetAttribute struct {
	UserID uuid.UUID
	Key    string
	Value  string

	// Source records who wrote this: "manual", "oidc", "saml" or "scim".
	// Empty defaults to manual.
	Source string
}

// Set creates or replaces an attribute.
func (r *UserAttributeRepo) Set(ctx context.Context, in SetAttribute) (model.UserAttribute, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.UserAttribute{}, err
	}

	source := in.Source
	if source == "" {
		source = model.SourceManual
	}

	if !slices.Contains(validSources, source) {
		return model.UserAttribute{}, fmt.Errorf(
			"repo: invalid attribute source %q (want one of %v)", source, validSources)
	}

	if in.Key == "" {
		return model.UserAttribute{}, fmt.Errorf("repo: attribute key is empty")
	}

	attr, err := r.q.UpsertUserAttribute(ctx, model.UpsertUserAttributeParams{
		ID:        newID(),
		OrgID:     s.OrgID(),
		UserID:    in.UserID,
		Key:       in.Key,
		Value:     in.Value,
		Source:    source,
		UpdatedAt: r.now(),
	})
	if err != nil {
		return model.UserAttribute{}, translate(err)
	}

	r.emit(ctx, ChangeUpdated, entityUserAttribute, attr.ID, s.OrgID(), s.ActorID())

	return attr, nil
}

// Get returns one attribute.
func (r *UserAttributeRepo) Get(
	ctx context.Context, userID uuid.UUID, key string,
) (model.UserAttribute, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.UserAttribute{}, err
	}

	attr, err := r.q.GetUserAttribute(ctx, model.GetUserAttributeParams{
		OrgID: s.OrgID(), UserID: userID, Key: key,
	})
	if err != nil {
		return model.UserAttribute{}, translate(err)
	}

	return attr, nil
}

// List returns a user's attributes, ordered by key.
func (r *UserAttributeRepo) List(ctx context.Context, userID uuid.UUID) ([]model.UserAttribute, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	attrs, err := r.q.ListUserAttributes(ctx, model.ListUserAttributesParams{
		OrgID: s.OrgID(), UserID: userID,
	})
	if err != nil {
		return nil, translate(err)
	}

	return attrs, nil
}

// Map returns a user's attributes as a key/value map, which is the shape the
// row-level security compiler in Phase 4 will want.
func (r *UserAttributeRepo) Map(ctx context.Context, userID uuid.UUID) (map[string]string, error) {
	attrs, err := r.List(ctx, userID)
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, len(attrs))
	for _, a := range attrs {
		out[a.Key] = a.Value
	}

	return out, nil
}

// Delete removes one attribute.
func (r *UserAttributeRepo) Delete(ctx context.Context, userID uuid.UUID, key string) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	n, derr := r.q.DeleteUserAttribute(ctx, model.DeleteUserAttributeParams{
		OrgID: s.OrgID(), UserID: userID, Key: key,
	})

	if aerr := affectedOrNotFound(n, derr); aerr != nil {
		return aerr
	}

	r.emit(ctx, ChangeDeleted, entityUserAttribute, userID, s.OrgID(), s.ActorID())

	return nil
}

// DeleteBySource removes every attribute a given source owns.
//
// This is what makes an identity provider sync safe to re-run: it clears only
// what that provider wrote, leaving manual assignments alone. Deleting
// everything and re-adding would silently discard an administrator's work.
//
// It returns the number removed, and zero is not an error — a provider that
// previously set nothing is a normal state, unlike a missing row on Delete.
func (r *UserAttributeRepo) DeleteBySource(
	ctx context.Context, userID uuid.UUID, source string,
) (int64, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return 0, err
	}

	if !slices.Contains(validSources, source) {
		return 0, fmt.Errorf("repo: invalid attribute source %q (want one of %v)", source, validSources)
	}

	n, derr := r.q.DeleteUserAttributesBySource(ctx, model.DeleteUserAttributesBySourceParams{
		OrgID: s.OrgID(), UserID: userID, Source: source,
	})
	if derr != nil {
		return 0, translate(derr)
	}

	if n > 0 {
		r.emit(ctx, ChangeDeleted, entityUserAttribute, userID, s.OrgID(), s.ActorID())
	}

	return n, nil
}
