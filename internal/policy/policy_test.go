package policy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/policy"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

/*
The fingerprint.

What these protect is a single property: two callers resolve to the same
fingerprint exactly when nothing about their authorization could make them see
different rows. Everything else in the result cache is a performance question.
This is the one where being wrong means one person reading another's data.
*/

var org = uuid.MustParse("11111111-1111-1111-1111-111111111111")

// Different standing, different fingerprint.
func TestDifferentPolicySetsFingerprintDifferently(t *testing.T) {
	t.Parallel()

	analyst := resolve(t, granting{authz.RelationAnalyst}, someone())
	admin := resolve(t, granting{authz.RelationAdmin}, someone())

	if analyst.Equal(admin) {
		t.Error("an analyst and an administrator share a fingerprint")
	}
}

/*
The same standing, held by two different people, is the same fingerprint.

This is the half that makes the cache worth having. ADR-0006 rejected keying on
the user id precisely because the hit rate then collapses in proportion to user
count -- hundreds of people fall into a handful of policy sets, and they should
share.
*/
func TestTheSamePolicySetSharesAFingerprint(t *testing.T) {
	t.Parallel()

	first := resolve(t, granting{authz.RelationAnalyst}, someone())
	second := resolve(t, granting{authz.RelationAnalyst}, someone())

	if !first.Equal(second) {
		t.Error("two callers with identical standing derived different fingerprints")
	}
}

// Two roles carrying the same permissions are the same policy set, whatever
// they are called. What decides sharing is what a caller can see.
func TestTheOrderGrantsArriveInDoesNotMatter(t *testing.T) {
	t.Parallel()

	forward := resolve(t, granting{authz.RelationAnalyst, authz.RelationViewer}, someone())
	backward := resolve(t, granting{authz.RelationViewer, authz.RelationAnalyst}, someone())

	if !forward.Equal(backward) {
		t.Error("the same grants in a different order derived different fingerprints")
	}
}

// A tenant is part of the fingerprint, so two organizations cannot share one
// even if their users hold identical roles.
func TestTenantsDoNotShareAFingerprint(t *testing.T) {
	t.Parallel()

	ours := resolve(t, granting{authz.RelationAdmin}, someone())

	theirs, err := policy.Resolve(t.Context(), granting{authz.RelationAdmin},
		tenant.MustNewScope(uuid.New(), someone()))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if ours.Equal(theirs) {
		t.Error("two organizations share a fingerprint")
	}
}

/*
Background work has its own fingerprint, and it is not a person's.

A scheduled refresh has no actor to resolve grants for. The dangerous answer is
"the empty policy set", because that is also what a caller holding no grants
resolves to -- and what an unauthenticated request would look like if one ever
reached here.
*/
func TestBackgroundWorkIsItsOwnPolicySet(t *testing.T) {
	t.Parallel()

	system, err := policy.Resolve(t.Context(), granting{},
		tenant.MustNewScope(org, uuid.NullUUID{}))
	if err != nil {
		t.Fatalf("resolve the system scope: %v", err)
	}

	if !system.Resolved() {
		t.Fatal("background work has no fingerprint")
	}

	// A caller who holds nothing at all is the closest a person gets, and
	// must still be a different fingerprint.
	nobody := resolve(t, granting{}, someone())

	if system.Equal(nobody) {
		t.Error("background work shares a fingerprint with a caller holding no grants")
	}
}

/*
A caller who cannot be resolved has no fingerprint, and two of them are not
equal to each other.

The zero value is the whole point. If an unresolved fingerprint compared equal
to another unresolved one, every caller the authorization store could not
answer for would share one cache entry -- and the store being unavailable is
exactly the moment nobody is watching.
*/
func TestAnUnresolvableCallerFailsClosed(t *testing.T) {
	t.Parallel()

	_, err := policy.Resolve(t.Context(), broken{}, tenant.MustNewScope(org, someone()))
	if !errors.Is(err, policy.ErrUnresolved) {
		t.Fatalf("err = %v, want ErrUnresolved", err)
	}

	var first, second policy.Fingerprint

	if first.Resolved() || second.Resolved() {
		t.Error("the zero fingerprint reports itself as resolved")
	}

	if first.Equal(second) {
		t.Error("two unresolved fingerprints compare equal")
	}

	if got := first.String(); got != "unresolved" {
		t.Errorf("the zero fingerprint renders as %q", got)
	}
}

// A missing resolver is the same answer as a broken one.
func TestNoResolverIsAlsoAFailure(t *testing.T) {
	t.Parallel()

	if _, err := policy.Resolve(t.Context(), nil, tenant.MustNewScope(org, someone())); !errors.Is(err, policy.ErrUnresolved) {
		t.Errorf("err = %v, want ErrUnresolved", err)
	}
}

// A scopeless context resolves to nothing, rather than to a tenantless
// fingerprint that would be shared across every organization at once.
func TestAScopelessCallerHasNoFingerprint(t *testing.T) {
	t.Parallel()

	if _, err := policy.Resolve(t.Context(), granting{authz.RelationAdmin}, tenant.Scope{}); !errors.Is(err, policy.ErrUnresolved) {
		t.Errorf("err = %v, want ErrUnresolved", err)
	}
}

/*
A relation nobody recognizes contributes nothing, and says so consistently.

The conservative direction: an unrecognized grant resolves to no additional
capability, so a caller holding one shares only with callers whose recognized
standing matches. Worth a test because the alternative -- hashing the unknown
relation as its own input -- is equally defensible and the two differ
observably, so a future change here should be deliberate.
*/
func TestAnUnrecognizedGrantAddsNoCapability(t *testing.T) {
	t.Parallel()

	plain := resolve(t, granting{authz.RelationAnalyst}, someone())
	exotic := resolve(t, granting{authz.RelationAnalyst, authz.Relation("archivist")}, someone())

	if !plain.Equal(exotic) {
		t.Error("an unrecognized relation changed the policy set")
	}
}

// --- helpers -----------------------------------------------------------------

func someone() uuid.NullUUID { return uuid.NullUUID{UUID: uuid.New(), Valid: true} }

func resolve(t *testing.T, g authz.Granter, actor uuid.NullUUID) policy.Fingerprint {
	t.Helper()

	fp, err := policy.Resolve(t.Context(), g, tenant.MustNewScope(org, actor))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	return fp
}

// granting reports a fixed standing, whoever asks.
type granting []authz.Relation

func (g granting) Grants(context.Context, authz.Subject, authz.Object) ([]authz.Relation, error) {
	return g, nil
}

// broken cannot answer at all.
type broken struct{}

func (broken) Grants(context.Context, authz.Subject, authz.Object) ([]authz.Relation, error) {
	return nil, errors.New("the authorization store is unavailable")
}
