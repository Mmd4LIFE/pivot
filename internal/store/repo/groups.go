package repo

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

const (
	entityGroup       = "group"
	entityGroupMember = "group_member"
)

// GroupRepo reads and writes groups within the caller's organization.
//
// Groups nest: a group may have a parent, which is how "Engineering" contains
// "Platform". Phase 7 walks that tree to resolve inherited permissions, so the
// parent relationship is a real foreign key rather than a convention.
type GroupRepo struct {
	base
}

// CreateGroup describes a new group.
type CreateGroup struct {
	Name          string
	Description   string
	ParentGroupID uuid.NullUUID

	// ExternalID links the group to an identity provider's group, so that a
	// SCIM or OIDC sync in Part 8 can match on it rather than on a name a user
	// might rename.
	ExternalID string
}

// Create adds a group to the caller's organization.
func (r *GroupRepo) Create(ctx context.Context, in CreateGroup) (model.Group, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.Group{}, err
	}

	group, err := r.q.CreateGroup(ctx, model.CreateGroupParams{
		ID:            newID(),
		OrgID:         s.OrgID(),
		Name:          in.Name,
		Description:   in.Description,
		ParentGroupID: in.ParentGroupID,
		ExternalID:    model.NewNullString(in.ExternalID),
		CreatedBy:     s.ActorID(),
		UpdatedBy:     s.ActorID(),
	})
	if err != nil {
		return model.Group{}, translate(err)
	}

	r.emit(ctx, ChangeCreated, entityGroup, group.ID, s.OrgID(), s.ActorID())

	return group, nil
}

// Get returns a group by ID, within the caller's organization.
func (r *GroupRepo) Get(ctx context.Context, id uuid.UUID) (model.Group, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.Group{}, err
	}

	group, err := r.q.GetGroup(ctx, model.GetGroupParams{ID: id, OrgID: s.OrgID()})
	if err != nil {
		return model.Group{}, translate(err)
	}

	return group, nil
}

// GetByName returns a group by name, within the caller's organization.
func (r *GroupRepo) GetByName(ctx context.Context, name string) (model.Group, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.Group{}, err
	}

	group, err := r.q.GetGroupByName(ctx, model.GetGroupByNameParams{OrgID: s.OrgID(), Name: name})
	if err != nil {
		return model.Group{}, translate(err)
	}

	return group, nil
}

// List returns groups in the caller's organization, ordered by name.
func (r *GroupRepo) List(ctx context.Context, limit, offset int64) ([]model.Group, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 50
	}

	groups, err := r.q.ListGroups(ctx, model.ListGroupsParams{
		OrgID: s.OrgID(), Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, translate(err)
	}

	return groups, nil
}

// ListChildren returns the direct children of a group.
func (r *GroupRepo) ListChildren(ctx context.Context, parentID uuid.UUID) ([]model.Group, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	groups, err := r.q.ListChildGroups(ctx, model.ListChildGroupsParams{
		OrgID:         s.OrgID(),
		ParentGroupID: uuid.NullUUID{UUID: parentID, Valid: true},
	})
	if err != nil {
		return nil, translate(err)
	}

	return groups, nil
}

// UpdateGroup describes a change to a group.
type UpdateGroup struct {
	ID            uuid.UUID
	Name          string
	Description   string
	ParentGroupID uuid.NullUUID
	ExternalID    string
	Version       int64
}

// Update modifies a group.
func (r *GroupRepo) Update(ctx context.Context, in UpdateGroup) (model.Group, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.Group{}, err
	}

	// A group that is its own parent would make the tree walk in Phase 7 loop
	// forever. Deeper cycles need a recursive check, which arrives with the
	// permission resolver that actually walks the tree.
	if in.ParentGroupID.Valid && in.ParentGroupID.UUID == in.ID {
		return model.Group{}, errors.New("repo: a group cannot be its own parent")
	}

	group, err := r.q.UpdateGroup(ctx, model.UpdateGroupParams{
		Name:          in.Name,
		Description:   in.Description,
		ParentGroupID: in.ParentGroupID,
		ExternalID:    model.NewNullString(in.ExternalID),
		UpdatedBy:     s.ActorID(),
		UpdatedAt:     r.now(),
		ID:            in.ID,
		OrgID:         s.OrgID(),
		Version:       in.Version,
	})
	if err != nil {
		if translated := translate(err); errors.Is(translated, ErrNotFound) {
			return model.Group{}, ErrConflict
		} else if translated != nil {
			return model.Group{}, translated
		}
	}

	r.emit(ctx, ChangeUpdated, entityGroup, group.ID, s.OrgID(), s.ActorID())

	return group, nil
}

// SoftDelete marks a group deleted.
func (r *GroupRepo) SoftDelete(ctx context.Context, id uuid.UUID) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	n, derr := r.q.SoftDeleteGroup(ctx, model.SoftDeleteGroupParams{
		DeletedAt: dbtypes.NewNullTime(r.now().Time),
		UpdatedBy: s.ActorID(),
		ID:        id,
		OrgID:     s.OrgID(),
	})

	if serr := affectedOrNotFound(n, derr); serr != nil {
		return serr
	}

	r.emit(ctx, ChangeDeleted, entityGroup, id, s.OrgID(), s.ActorID())

	return nil
}

// AddMember adds a user to a group.
//
// Adding a member twice is not an error: the underlying query does nothing on
// conflict. Membership is a set, and an idempotent add is what a SCIM sync
// replaying the same state needs.
func (r *GroupRepo) AddMember(ctx context.Context, groupID, userID uuid.UUID) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	if aerr := r.q.AddGroupMember(ctx, model.AddGroupMemberParams{
		OrgID:   s.OrgID(),
		GroupID: groupID,
		UserID:  userID,
		AddedBy: s.ActorID(),
	}); aerr != nil {
		return translate(aerr)
	}

	r.emit(ctx, ChangeCreated, entityGroupMember, groupID, s.OrgID(), s.ActorID())

	return nil
}

// RemoveMember removes a user from a group.
func (r *GroupRepo) RemoveMember(ctx context.Context, groupID, userID uuid.UUID) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	n, rerr := r.q.RemoveGroupMember(ctx, model.RemoveGroupMemberParams{
		OrgID:   s.OrgID(),
		GroupID: groupID,
		UserID:  userID,
	})

	if merr := affectedOrNotFound(n, rerr); merr != nil {
		return merr
	}

	r.emit(ctx, ChangeDeleted, entityGroupMember, groupID, s.OrgID(), s.ActorID())

	return nil
}

// ListMembers returns the users in a group.
func (r *GroupRepo) ListMembers(ctx context.Context, groupID uuid.UUID) ([]model.User, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	users, err := r.q.ListGroupMembers(ctx, model.ListGroupMembersParams{
		OrgID: s.OrgID(), GroupID: groupID,
	})
	if err != nil {
		return nil, translate(err)
	}

	return users, nil
}

// ListForUser returns the groups a user belongs to.
func (r *GroupRepo) ListForUser(ctx context.Context, userID uuid.UUID) ([]model.Group, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	groups, err := r.q.ListUserGroups(ctx, model.ListUserGroupsParams{
		OrgID: s.OrgID(), UserID: userID,
	})
	if err != nil {
		return nil, translate(err)
	}

	return groups, nil
}

// IsMember reports whether a user belongs to a group.
func (r *GroupRepo) IsMember(ctx context.Context, groupID, userID uuid.UUID) (bool, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return false, err
	}

	ok, err := r.q.IsGroupMember(ctx, model.IsGroupMemberParams{
		OrgID: s.OrgID(), GroupID: groupID, UserID: userID,
	})
	if err != nil {
		return false, translate(err)
	}

	return ok, nil
}
