package authz

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Mmd4LIFE/pivot/internal/observability"
)

// Store is the persistence the resolver needs. internal/store/repo implements
// it, which is why this is an interface here and not a concrete type: the
// resolver knows about relationships, not about SQL or tenants.
//
// Note what is absent: any organization parameter. Tenant scoping comes from
// the context, exactly as everywhere else — see internal/tenant.
type Store interface {
	// RelationsOn returns the relations any of the given subjects hold on an
	// object. One call rather than one per subject: a user in twelve groups
	// would otherwise cost thirteen round trips on a request-path check.
	RelationsOn(ctx context.Context, subjects []Subject, object Object) ([]Relation, error)

	// GroupsForUser returns the IDs of the groups a user belongs to directly.
	// Nesting is resolved by the caller, not by the query.
	GroupsForUser(ctx context.Context, userID string) ([]string, error)

	// ParentGroup returns a group's parent, or "" when it has none.
	ParentGroup(ctx context.Context, groupID string) (string, error)
}

// maxGroupDepth bounds how far group nesting is followed.
//
// Nesting is stored as a parent pointer, so a cycle is representable even if
// the API refuses to create one — a bad migration or a direct database edit is
// enough. An unbounded walk would hang a request thread; this turns the same
// situation into a denial and a log line. Ten is far deeper than any real
// organization chart.
const maxGroupDepth = 10

// Resolver answers permission questions by walking the relationship graph.
//
// The walk is deliberately in Go rather than in SQL. ADR-0009 rejects
// hand-rolled RBAC because it becomes a recursive CTE nobody can debug, and
// that objection is about the recursive CTE, not about Go: a bounded loop over
// two indexed queries is readable, portable across both engines without a
// dialect branch (architectural rule 3), and — unlike a CTE — can explain
// itself.
type Resolver struct {
	store Store
}

// NewResolver builds a resolver over a store.
func NewResolver(store Store) *Resolver { return &Resolver{store: store} }

// Check answers one question.
func (r *Resolver) Check(ctx context.Context, req Request) (Decision, error) {
	explanation, err := r.Explain(ctx, req)
	if err != nil {
		return Decision{}, err
	}

	return explanation.Decision, nil
}

// Explain answers a question and shows its work.
//
// Check is implemented in terms of this rather than the other way around, so
// the explanation can never drift from the decision: there is one code path,
// and the debugger sees exactly what the enforcement saw.
func (r *Resolver) Explain(ctx context.Context, req Request) (Explanation, error) {
	// The span goes here rather than on Check, for the same reason Check is
	// implemented in terms of Explain: one code path, so a traced request and
	// an explained one cannot disagree about what happened.
	ctx, span := observability.Start(ctx, "authz.Check",
		attribute.String("authz.permission", string(req.Permission)),
		attribute.String("authz.object_type", string(req.Object.Type)),
	)
	defer span.End()

	out := Explanation{Request: req}

	granting := RolesGranting(req.Permission)
	if len(granting) == 0 {
		// An unregistered permission denies rather than defaulting to allow.
		// A typo in a handler must lock a door, not open one.
		if !knownPermission(req.Permission) {
			return out, fmt.Errorf("%w: %q", ErrUnknownPermission, req.Permission)
		}

		out.Granting = granting

		return out, nil
	}

	out.Granting = granting

	subjects, err := r.expand(ctx, req.Subject)
	if err != nil {
		return out, err
	}

	out.Subjects = subjects

	relations, err := r.store.RelationsOn(ctx, subjects, req.Object)
	if err != nil {
		return out, fmt.Errorf("read relations: %w", err)
	}

	out.Relations = relations

	held := make(map[Relation]bool, len(relations))
	for _, rel := range relations {
		held[rel] = true
	}

	// Most privileged first, so the explanation names the strongest reason
	// rather than whichever happened to be checked first.
	for _, role := range granting {
		if held[role] {
			out.Allowed = true
			out.Via = role

			break
		}
	}

	// The decision itself, on the span. "Why was this request a 403" is the
	// question a trace gets opened to answer, and without this the trace shows
	// only that an authorization check happened.
	span.SetAttributes(
		attribute.Bool("authz.allowed", out.Allowed),
		attribute.String("authz.via", string(out.Via)),
		attribute.Int("authz.subjects", len(out.Subjects)),
	)

	return out, nil
}

// expand turns a subject into every identity that speaks for it: the subject
// itself, plus the member-userset of each group it belongs to, following
// nesting upward.
//
// Upward is the direction that matters. A user in "EU Analysts", which is a
// child of "Analysts", is a member of both — a role granted to the parent
// reaches the child's members. Walking downward instead would mean granting a
// role to a group and having it not apply to the sub-team, which is the
// opposite of what anyone expects.
func (r *Resolver) expand(ctx context.Context, subject Subject) ([]Subject, error) {
	subjects := []Subject{subject}

	if subject.Type != SubjectUser {
		return subjects, nil
	}

	direct, err := r.store.GroupsForUser(ctx, subject.ID)
	if err != nil {
		return nil, fmt.Errorf("read group membership: %w", err)
	}

	seen := make(map[string]bool, len(direct))
	queue := make([]string, 0, len(direct))

	for _, id := range direct {
		if !seen[id] {
			seen[id] = true

			queue = append(queue, id)
		}
	}

	for depth := 0; len(queue) > 0; depth++ {
		if depth >= maxGroupDepth {
			// A cycle, or nesting deeper than anyone intended. Refusing is the
			// fail-closed answer: returning what has been collected so far
			// would make a permission depend on how a loop happened to unwind.
			return nil, fmt.Errorf("%w: group nesting deeper than %d levels", ErrUnavailable, maxGroupDepth)
		}

		next := make([]string, 0, len(queue))

		for _, groupID := range queue {
			subjects = append(subjects, GroupMembers(groupID))

			parent, perr := r.store.ParentGroup(ctx, groupID)
			if perr != nil {
				return nil, fmt.Errorf("read group parent: %w", perr)
			}

			if parent != "" && !seen[parent] {
				seen[parent] = true

				next = append(next, parent)
			}
		}

		queue = next
	}

	return subjects, nil
}

// knownPermission reports whether a permission is in the registry.
func knownPermission(p Permission) bool {
	for _, known := range AllPermissions {
		if known == p {
			return true
		}
	}

	return false
}
