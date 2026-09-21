package repo

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

const entityUser = "user"

// UserRepo reads and writes users within the caller's organization.
//
// Every method takes the user's own ID where one is needed, and never an
// organization: the org comes from the scope and is injected into the query.
// A caller holding a user ID from another tenant gets ErrNotFound, which is
// deliberately the same answer as a genuinely absent row.
type UserRepo struct {
	base
}

// CreateUser describes a new user.
type CreateUser struct {
	Email        string
	Name         string
	AvatarURL    string
	PasswordHash string
	IsActive     bool
	Locale       string
	Timezone     string
}

// Create adds a user to the caller's organization.
func (r *UserRepo) Create(ctx context.Context, in CreateUser) (model.User, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.User{}, err
	}

	locale := in.Locale
	if locale == "" {
		locale = "en"
	}

	timezone := in.Timezone
	if timezone == "" {
		timezone = "UTC"
	}

	user, err := r.q.CreateUser(ctx, model.CreateUserParams{
		ID:           newID(),
		OrgID:        s.OrgID(),
		Email:        in.Email,
		Name:         in.Name,
		AvatarURL:    model.NewNullString(in.AvatarURL),
		PasswordHash: model.NewNullString(in.PasswordHash),
		IsActive:     dbtypes.Bool(in.IsActive),
		Locale:       locale,
		Timezone:     timezone,
		CreatedBy:    s.ActorID(),
		UpdatedBy:    s.ActorID(),
	})
	if err != nil {
		return model.User{}, translate(err)
	}

	r.emit(ctx, ChangeCreated, entityUser, user.ID, s.OrgID(), s.ActorID())

	return user, nil
}

// Get returns a user by ID, within the caller's organization.
func (r *UserRepo) Get(ctx context.Context, id uuid.UUID) (model.User, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.User{}, err
	}

	user, err := r.q.GetUser(ctx, model.GetUserParams{ID: id, OrgID: s.OrgID()})
	if err != nil {
		return model.User{}, translate(err)
	}

	return user, nil
}

// GetByEmail returns a user by email, within the caller's organization.
func (r *UserRepo) GetByEmail(ctx context.Context, email string) (model.User, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.User{}, err
	}

	user, err := r.q.GetUserByEmail(ctx, model.GetUserByEmailParams{OrgID: s.OrgID(), Email: email})
	if err != nil {
		return model.User{}, translate(err)
	}

	return user, nil
}

// List returns users in the caller's organization, ordered by email.
func (r *UserRepo) List(ctx context.Context, limit, offset int64) ([]model.User, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 50
	}

	users, err := r.q.ListUsers(ctx, model.ListUsersParams{
		OrgID: s.OrgID(), Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, translate(err)
	}

	return users, nil
}

// Count returns the number of live users in the caller's organization.
func (r *UserRepo) Count(ctx context.Context) (int64, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return 0, err
	}

	n, err := r.q.CountUsers(ctx, s.OrgID())

	return n, translate(err)
}

// UpdateUser describes a profile change. It deliberately cannot change the
// password: see [UserRepo.SetPassword].
type UpdateUser struct {
	ID        uuid.UUID
	Email     string
	Name      string
	AvatarURL string
	IsActive  bool
	Locale    string
	Timezone  string
	Version   int64
}

// Update modifies a user's profile.
func (r *UserRepo) Update(ctx context.Context, in UpdateUser) (model.User, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.User{}, err
	}

	user, err := r.q.UpdateUser(ctx, model.UpdateUserParams{
		Email:     in.Email,
		Name:      in.Name,
		AvatarURL: model.NewNullString(in.AvatarURL),
		IsActive:  dbtypes.Bool(in.IsActive),
		Locale:    in.Locale,
		Timezone:  in.Timezone,
		UpdatedBy: s.ActorID(),
		UpdatedAt: r.now(),
		ID:        in.ID,
		OrgID:     s.OrgID(),
		Version:   in.Version,
	})
	if err != nil {
		if translated := translate(err); errors.Is(translated, ErrNotFound) {
			return model.User{}, ErrConflict
		} else if translated != nil {
			return model.User{}, translated
		}
	}

	r.emit(ctx, ChangeUpdated, entityUser, user.ID, s.OrgID(), s.ActorID())

	return user, nil
}

// SetPassword replaces a user's password hash.
//
// Separate from [UserRepo.Update] so that a general profile update cannot
// rewrite a credential by accident, and so the audit trail distinguishes "a
// profile changed" from "a credential changed".
//
// The argument is a hash, never a password: this layer does no hashing, and
// accepting a plaintext here would invite someone to store one.
func (r *UserRepo) SetPassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	n, uerr := r.q.UpdateUserPassword(ctx, model.UpdateUserPasswordParams{
		PasswordHash: model.NewNullString(passwordHash),
		UpdatedBy:    s.ActorID(),
		UpdatedAt:    r.now(),
		ID:           id,
		OrgID:        s.OrgID(),
	})

	if perr := affectedOrNotFound(n, uerr); perr != nil {
		return perr
	}

	r.emit(ctx, ChangeUpdated, entityUser, id, s.OrgID(), s.ActorID())

	return nil
}

// RecordLogin stamps a successful sign-in.
//
// It does not bump the version: a login is not an edit, and making it one
// would cause a concurrent profile save to fail with a spurious conflict.
func (r *UserRepo) RecordLogin(ctx context.Context, id uuid.UUID) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	n, uerr := r.q.RecordUserLogin(ctx, model.RecordUserLoginParams{
		LastLoginAt: dbtypes.NewNullTime(r.now().Time),
		ID:          id,
		OrgID:       s.OrgID(),
	})

	return affectedOrNotFound(n, uerr)
}

// SoftDelete marks a user deleted within the caller's organization.
func (r *UserRepo) SoftDelete(ctx context.Context, id uuid.UUID) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	n, uerr := r.q.SoftDeleteUser(ctx, model.SoftDeleteUserParams{
		DeletedAt: dbtypes.NewNullTime(r.now().Time),
		UpdatedBy: s.ActorID(),
		ID:        id,
		OrgID:     s.OrgID(),
	})

	if derr := affectedOrNotFound(n, uerr); derr != nil {
		return derr
	}

	// Role grants do not survive the user.
	//
	// The user row stays — this is a soft delete — but its grants are removed
	// outright, because the subject columns in role_assignments are polymorphic
	// and so carry no foreign key to cascade through. Leaving them would mean a
	// restored or re-imported account silently inheriting whatever it used to
	// hold, which is the kind of thing nobody notices until it matters.
	if _, rerr := r.q.RevokeAllForSubject(ctx, model.RevokeAllForSubjectParams{
		OrgID: s.OrgID(), SubjectType: subjectTypeUser, SubjectID: id,
	}); rerr != nil {
		return translate(rerr)
	}

	r.emit(ctx, ChangeDeleted, entityUser, id, s.OrgID(), s.ActorID())

	return nil
}
