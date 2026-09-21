package authz

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

// repoStore implements [Store] over the repository layer.
//
// The dependency runs this way — the authorization domain importing storage,
// not the reverse — so that internal/store stays unaware of permissions. It is
// the same shape as internal/auth.
type repoStore struct {
	repos *repo.Repositories
}

// NewStore returns the [Store] backed by the repository layer.
func NewStore(repos *repo.Repositories) Store { return &repoStore{repos: repos} }

// New builds the production checker: a graph resolver over the repositories,
// wrapped in a decision cache.
//
// One constructor rather than three call sites assembling the same three
// pieces, so an instance cannot end up running uncached or — worse —
// unwrapped in a way that skips invalidation.
func New(repos *repo.Repositories) (Checker, *Cache) {
	cache := NewCache(NewResolver(NewStore(repos)))

	return cache, cache
}

// RelationsOn returns the relations any of the given subjects hold on an
// object.
//
// The store query returns a widened set — the user's own grants plus every
// group grant on the object — because a variable-length IN list is not
// portable across both engines. The intersection happens here, against the
// usersets the resolver already expanded.
func (s *repoStore) RelationsOn(
	ctx context.Context, subjects []Subject, object Object,
) ([]Relation, error) {
	objectID, err := uuid.Parse(object.ID)
	if err != nil {
		return nil, fmt.Errorf("authz: object id %q: %w", object.ID, err)
	}

	// The one user among the subjects, if any. The resolver always puts the
	// subject it was asked about first.
	var userID uuid.UUID

	allowed := make(map[string]bool, len(subjects))

	for _, sub := range subjects {
		allowed[sub.String()] = true

		if sub.Type == SubjectUser && !sub.IsUserset() && userID == uuid.Nil {
			parsed, perr := uuid.Parse(sub.ID)
			if perr != nil {
				return nil, fmt.Errorf("authz: subject id %q: %w", sub.ID, perr)
			}

			userID = parsed
		}
	}

	rows, err := s.repos.Roles.GrantsOnObject(ctx, string(object.Type), objectID, userID)
	if err != nil {
		return nil, err
	}

	relations := make([]Relation, 0, len(rows))

	for _, row := range rows {
		candidate := Subject{
			Type:     SubjectType(row.SubjectType),
			ID:       row.SubjectID.String(),
			Relation: Relation(row.SubjectRelation),
		}

		// A group grant that belongs to a group this subject is not in. The
		// query could not exclude it portably, so it is excluded here.
		if !allowed[candidate.String()] {
			continue
		}

		relations = append(relations, Relation(row.Relation))
	}

	return relations, nil
}

// GroupsForUser returns the groups a user belongs to directly.
func (s *repoStore) GroupsForUser(ctx context.Context, userID string) ([]string, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("authz: user id %q: %w", userID, err)
	}

	groups, err := s.repos.Groups.ListForUser(ctx, id)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.ID.String())
	}

	return out, nil
}

// ParentGroup returns a group's parent, or "" when it has none.
func (s *repoStore) ParentGroup(ctx context.Context, groupID string) (string, error) {
	id, err := uuid.Parse(groupID)
	if err != nil {
		return "", fmt.Errorf("authz: group id %q: %w", groupID, err)
	}

	group, err := s.repos.Groups.Get(ctx, id)
	if err != nil {
		return "", err
	}

	if !group.ParentGroupID.Valid {
		return "", nil
	}

	return group.ParentGroupID.UUID.String(), nil
}
