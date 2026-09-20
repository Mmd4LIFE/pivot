// Package tenant carries the organization and actor a request runs as.
//
// This is the root of Pivot's tenant isolation. The rule it exists to enforce:
// a repository method never takes an organization as a parameter. It reads the
// scope from the context instead, so a caller cannot pass the wrong tenant —
// there is nothing to pass. Forgetting the scope is not a silent widening of a
// query; it is an error before any SQL runs.
//
// See docs/architecture/system-design.md section 4 and architectural rule 6 in
// docs/roadmap/00-principles.md.
package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Errors returned when a scope is missing or malformed. Callers check these
// with errors.Is; they are deliberately distinguishable so a missing scope
// (a programming error) reads differently from an invalid one (bad input).
var (
	// ErrNoScope means the context carries no tenant. Any repository call in
	// this state fails rather than running an unscoped query.
	ErrNoScope = errors.New("tenant: no scope in context")

	// ErrInvalidScope means a scope was constructed with a nil organization.
	ErrInvalidScope = errors.New("tenant: scope has no organization")
)

// Scope is the organization and actor a request runs as.
//
// Its fields are unexported and it has no usable zero value, so a Scope can
// only come from [NewScope], which validates it. That closes the gap where
// `var s Scope` would otherwise produce a silently org-less scope that queries
// would then treat as "organization uuid.Nil".
type Scope struct {
	orgID   uuid.UUID
	actorID uuid.NullUUID
}

// NewScope builds a scope for an organization, optionally attributed to an
// actor. A nil organization is rejected.
//
// The actor is separate from the organization because not every write has a
// human behind it: migrations, scheduled jobs and system tasks act on an
// organization with no actor, and created_by must record that honestly rather
// than inventing a user.
func NewScope(orgID uuid.UUID, actorID uuid.NullUUID) (Scope, error) {
	if orgID == uuid.Nil {
		return Scope{}, ErrInvalidScope
	}

	return Scope{orgID: orgID, actorID: actorID}, nil
}

// NewSystemScope builds a scope with no actor, for background work.
func NewSystemScope(orgID uuid.UUID) (Scope, error) {
	return NewScope(orgID, uuid.NullUUID{})
}

// MustNewScope is [NewScope] for tests and static configuration, where a nil
// organization is a bug rather than a runtime condition.
func MustNewScope(orgID uuid.UUID, actorID uuid.NullUUID) Scope {
	s, err := NewScope(orgID, actorID)
	if err != nil {
		panic(fmt.Sprintf("tenant: %v", err))
	}

	return s
}

// OrgID returns the organization this scope is bound to.
func (s Scope) OrgID() uuid.UUID { return s.orgID }

// ActorID returns the acting user, if any.
func (s Scope) ActorID() uuid.NullUUID { return s.actorID }

// IsValid reports whether the scope names an organization.
func (s Scope) IsValid() bool { return s.orgID != uuid.Nil }

// String implements [fmt.Stringer]. The actor is included because scopes show
// up in audit and debug output, where "who" matters as much as "which tenant".
func (s Scope) String() string {
	if s.actorID.Valid {
		return fmt.Sprintf("org=%s actor=%s", s.orgID, s.actorID.UUID)
	}

	return fmt.Sprintf("org=%s actor=system", s.orgID)
}

// WithActor returns a copy of the scope attributed to a different actor,
// keeping the same organization.
//
// There is deliberately no WithOrg counterpart: changing tenant mid-request is
// the operation this package exists to prevent. Crossing tenants means building
// a new scope explicitly, which is visible in review.
func (s Scope) WithActor(actorID uuid.NullUUID) Scope {
	return Scope{orgID: s.orgID, actorID: actorID}
}

// contextKey is unexported so no other package can write or overwrite the
// scope by constructing the same key.
type contextKey struct{}

// WithScope returns a context carrying the scope.
func WithScope(ctx context.Context, s Scope) context.Context {
	return context.WithValue(ctx, contextKey{}, s)
}

// FromContext returns the scope in ctx.
//
// It returns [ErrNoScope] when there is none and [ErrInvalidScope] when the
// stored scope names no organization. Repositories call this first and return
// the error unchanged, which is what makes a missing scope fail closed.
func FromContext(ctx context.Context) (Scope, error) {
	s, ok := ctx.Value(contextKey{}).(Scope)
	if !ok {
		return Scope{}, ErrNoScope
	}

	if !s.IsValid() {
		return Scope{}, ErrInvalidScope
	}

	return s, nil
}

// Has reports whether ctx carries a usable scope, for callers that want to
// branch rather than fail.
func Has(ctx context.Context) bool {
	_, err := FromContext(ctx)

	return err == nil
}
