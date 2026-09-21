package repo

import (
	"context"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

const entityRoleAssignment = "role_assignment"

// RoleRepo stores role assignments as Zanzibar tuples.
//
// It speaks in plain identifiers rather than in internal/authz's types on
// purpose: the storage layer should not depend on the authorization domain.
// The adapter that bridges them lives in internal/authz, the same way
// internal/auth wraps this package rather than the reverse.
type RoleRepo struct {
	base
}

// GrantsOnObject returns the grants relevant to one user on one object: their
// own, plus every group grant on that object.
//
// The group grants are deliberately unfiltered. Narrowing them to the groups
// this user belongs to would need a variable-length IN list, which Postgres
// and SQLite express differently and which sqlc cannot generate portably. The
// caller already knows the user's groups and intersects in memory, over a set
// bounded by the number of groups holding a role on a single object.
func (r *RoleRepo) GrantsOnObject(
	ctx context.Context, objectType string, objectID, userID uuid.UUID,
) ([]model.RoleAssignment, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := r.q.ListObjectGrants(ctx, model.ListObjectGrantsParams{
		OrgID:      s.OrgID(),
		ObjectType: objectType,
		ObjectID:   objectID,
		SubjectID:  userID,
	})
	if err != nil {
		return nil, translate(err)
	}

	return rows, nil
}

// GrantsForSubject returns everything a subject holds, anywhere.
func (r *RoleRepo) GrantsForSubject(
	ctx context.Context, subjectType string, subjectID uuid.UUID,
) ([]model.RoleAssignment, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := r.q.ListSubjectGrants(ctx, model.ListSubjectGrantsParams{
		OrgID:       s.OrgID(),
		SubjectType: subjectType,
		SubjectID:   subjectID,
	})
	if err != nil {
		return nil, translate(err)
	}

	return rows, nil
}

// ListOnObject returns every grant on an object, for an administration screen
// answering "who has access to this?".
func (r *RoleRepo) ListOnObject(
	ctx context.Context, objectType string, objectID uuid.UUID,
) ([]model.RoleAssignment, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := r.q.ListGrantsOnObject(ctx, model.ListGrantsOnObjectParams{
		OrgID:      s.OrgID(),
		ObjectType: objectType,
		ObjectID:   objectID,
	})
	if err != nil {
		return nil, translate(err)
	}

	return rows, nil
}

// GrantRole describes a relationship to store.
type GrantRole struct {
	SubjectType     string
	SubjectID       uuid.UUID
	SubjectRelation string
	Relation        string
	ObjectType      string
	ObjectID        uuid.UUID
}

// Grant stores a relationship.
//
// Granting the same relationship twice is a no-op rather than an error: a
// tuple is a fact, and a fact is either recorded or not. Returning a conflict
// would make every caller write "grant unless already granted", which is the
// same thing with more ways to get it wrong.
func (r *RoleRepo) Grant(ctx context.Context, in GrantRole) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	if gerr := r.q.GrantRole(ctx, model.GrantRoleParams{
		ID:              newID(),
		OrgID:           s.OrgID(),
		SubjectType:     in.SubjectType,
		SubjectID:       in.SubjectID,
		SubjectRelation: in.SubjectRelation,
		Relation:        in.Relation,
		ObjectType:      in.ObjectType,
		ObjectID:        in.ObjectID,
		CreatedBy:       s.ActorID(),
	}); gerr != nil {
		return translate(gerr)
	}

	r.emit(ctx, ChangeCreated, entityRoleAssignment, in.ObjectID, s.OrgID(), s.ActorID())

	return nil
}

// Revoke removes a relationship.
//
// Removing one that is not there is not an error, for the same reason granting
// twice is not: the caller asked for a state, and the state holds.
func (r *RoleRepo) Revoke(ctx context.Context, in GrantRole) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	n, rerr := r.q.RevokeRole(ctx, model.RevokeRoleParams{
		OrgID:           s.OrgID(),
		SubjectType:     in.SubjectType,
		SubjectID:       in.SubjectID,
		SubjectRelation: in.SubjectRelation,
		Relation:        in.Relation,
		ObjectType:      in.ObjectType,
		ObjectID:        in.ObjectID,
	})
	if rerr != nil {
		return translate(rerr)
	}

	if n > 0 {
		r.emit(ctx, ChangeDeleted, entityRoleAssignment, in.ObjectID, s.OrgID(), s.ActorID())
	}

	return nil
}

// RevokeAllForSubject removes every grant a subject holds.
//
// A deleted user or group must not leave grants behind: an identifier can be
// reused by a future import, and inheriting a stranger's permissions is the
// kind of bug nobody finds until it matters.
func (r *RoleRepo) RevokeAllForSubject(
	ctx context.Context, subjectType string, subjectID uuid.UUID,
) (int64, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return 0, err
	}

	n, rerr := r.q.RevokeAllForSubject(ctx, model.RevokeAllForSubjectParams{
		OrgID:       s.OrgID(),
		SubjectType: subjectType,
		SubjectID:   subjectID,
	})
	if rerr != nil {
		return 0, translate(rerr)
	}

	if n > 0 {
		r.emit(ctx, ChangeDeleted, entityRoleAssignment, subjectID, s.OrgID(), s.ActorID())
	}

	return n, nil
}

// CountHolders returns how many subjects hold a relation on an object.
//
// This exists so that removing the last administrator can be refused. An
// organization with nobody who can grant roles is only recoverable from the
// command line, and the people most likely to reach that state are the ones
// least likely to have shell access.
func (r *RoleRepo) CountHolders(
	ctx context.Context, relation, objectType string, objectID uuid.UUID,
) (int64, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return 0, err
	}

	n, cerr := r.q.CountRoleHolders(ctx, model.CountRoleHoldersParams{
		OrgID:      s.OrgID(),
		Relation:   relation,
		ObjectType: objectType,
		ObjectID:   objectID,
	})
	if cerr != nil {
		return 0, translate(cerr)
	}

	return n, nil
}
