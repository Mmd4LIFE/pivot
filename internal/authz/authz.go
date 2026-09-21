package authz

import (
	"context"
	"errors"
	"fmt"
)

// Errors callers branch on.
var (
	// ErrDenied is returned by [Enforce] when a check answers no. It is
	// distinct from a failure to reach an answer, which is [ErrUnavailable]:
	// both mean "no", and only one means something is broken.
	ErrDenied = errors.New("authz: not permitted")

	// ErrUnavailable means the decision could not be made — the store failed,
	// the context expired, the graph was too deep.
	//
	// It maps to deny at every call site. An authorization system that fails
	// open is worse than none, because it creates the false belief that access
	// is controlled. It stays distinguishable from ErrDenied so that an
	// operator can tell "this user may not" from "Pivot is broken", which are
	// the same answer and very different pages.
	ErrUnavailable = errors.New("authz: decision unavailable")

	// ErrUnknownPermission means the permission is not in the registry. It is
	// a programming error, and it denies.
	ErrUnknownPermission = errors.New("authz: unknown permission")
)

// Request is one authorization question.
type Request struct {
	// Subject is who is asking. Usually a user built with [User].
	Subject Subject

	// Permission is what they want to do.
	Permission Permission

	// Object is what they want to do it to.
	Object Object
}

// String renders the question the way the assertion files write it, so a log
// line and a failing test read the same.
func (r Request) String() string {
	return fmt.Sprintf("%s#%s@%s", r.Object, r.Permission, r.Subject)
}

// Decision is an answer, with enough of the reasoning attached to explain it.
//
// The reasoning is carried from the start rather than bolted on later because
// "why can Dana see this?" is a question every BI tool is asked and most answer
// badly. Phase 4's permission debugger (P4-PRM-008) is built on this field.
type Decision struct {
	// Allowed is the answer.
	Allowed bool

	// Via is the relation that granted it, empty on a denial.
	Via Relation

	// Through names the group the relation was inherited from, empty when the
	// subject held it directly.
	Through string
}

// Reason renders a human-readable explanation.
func (d Decision) Reason() string {
	if !d.Allowed {
		return "no relation grants it"
	}

	if d.Through != "" {
		return fmt.Sprintf("%s via group %s", d.Via, d.Through)
	}

	return string(d.Via) + " held directly"
}

// Checker answers authorization questions.
//
// This interface is the point of ADR-0009's design, and the reason the backing
// implementation can change without touching a single call site. See the
// amendment in that ADR for why Phase 0 ships a local implementation.
//
// Implementations must fail closed: any error is a denial at the call site,
// and [Enforce] is the helper that makes that the path of least resistance.
type Checker interface {
	// Check answers one question.
	Check(ctx context.Context, req Request) (Decision, error)

	// Explain answers the same question with the full reasoning, for the
	// permission debugger and for tests that need to say *why* a denial
	// happened rather than only that it did.
	Explain(ctx context.Context, req Request) (Explanation, error)
}

// Explanation is a Decision plus the evidence behind it.
type Explanation struct {
	Decision

	// Request is the question asked, echoed so an explanation stands alone.
	Request Request

	// Subjects are every identity considered: the user, and the userset of
	// each group they belong to, directly or through nesting.
	Subjects []Subject

	// Relations are the relations found on the object for those subjects.
	Relations []Relation

	// Granting are the relations that would have answered yes. Comparing this
	// with Relations is what turns "denied" into "denied, and here is what you
	// would have needed".
	Granting []Role
}

// Enforce is Check reduced to an error, which is what a handler wants.
//
// Every failure — denied, store unreachable, context canceled — returns an
// error, so the only way to proceed is an explicit nil check. A handler cannot
// accidentally treat an unavailable backend as permission granted, because
// there is no boolean to misread.
func Enforce(ctx context.Context, c Checker, req Request) error {
	if c == nil {
		// A nil checker is a wiring bug. Denying is the only safe reading:
		// the alternative is an instance that serves every request as if
		// authorization were configured when it is not.
		return fmt.Errorf("%w: no checker configured", ErrUnavailable)
	}

	decision, err := c.Check(ctx, req)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrUnavailable, req, err)
	}

	if !decision.Allowed {
		return fmt.Errorf("%w: %s", ErrDenied, req)
	}

	return nil
}
