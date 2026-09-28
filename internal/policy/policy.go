/*
Package policy answers one question: what about a caller could change which
rows they are allowed to see?

The answer is a [Fingerprint], and the result cache is keyed on it. That is the
whole reason this package exists as its own thing rather than as a function in
internal/query: ADR-0006 requires the cache key to carry the caller's resolved
policy set, ADR-0012 names this the seam where that is computed, and Phase 4
widens what it resolves from without touching anything that consumes it.

Getting this wrong is not a performance bug. Two callers who should not share a
cache entry and do is user A reading user B's rows, delivered by an
optimization. So the package fails closed everywhere it can: an unresolvable
caller has no fingerprint, and a query with no fingerprint is not cached and is
not served from cache.
*/
package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// ErrUnresolved means the caller's standing could not be established. It is
// not a denial -- it is "ask again" -- and its only effect is that nothing is
// cached for this query.
var ErrUnresolved = errors.New("policy: the caller's policy set could not be resolved")

/*
Fingerprint identifies everything about a caller that could change which rows
they may see.

Two callers with the same fingerprint may share a cached result. Two with
different fingerprints structurally cannot, because the fingerprint is part of
the cache key rather than something checked alongside it.

The zero value is deliberately unusable. A [Fingerprint] that was never
resolved is not "the empty policy set" -- it is the absence of an answer, and
the two must not be spellable the same way, because treating one as the other
is how every unresolvable caller ends up sharing a single cache entry.
*/
type Fingerprint struct {
	sum string
}

// String renders the fingerprint. Safe to log: it is a hash, and it names a
// policy set rather than a person.
func (f Fingerprint) String() string {
	if f.sum == "" {
		return "unresolved"
	}

	return f.sum
}

// Resolved reports whether this fingerprint came from an actual resolution.
// A caller holding an unresolved fingerprint must not cache.
func (f Fingerprint) Resolved() bool { return f.sum != "" }

// Equal compares two fingerprints. Two unresolved fingerprints are *not*
// equal, for the reason the type comment gives.
func (f Fingerprint) Equal(other Fingerprint) bool {
	return f.Resolved() && other.Resolved() && f.sum == other.sum
}

/*
Version prefixes every fingerprint.

When Phase 4 adds row-level security predicates to what is resolved here, every
existing fingerprint describes a policy set computed under the old rules. They
must not continue to match. Bumping this makes every one of them unreachable at
once, which is the same mechanism ADR-0006 uses for invalidation and for the
same reason: a scan that has to find and delete them is a scan that can miss
one.
*/
const Version = "policy/v1"

/*
Resolve computes the fingerprint of the caller in this context.

Today it resolves from the permissions the caller holds in their organization.
That is not a placeholder standing in for the real thing: two callers with
different roles genuinely resolve differently, and keying the cache on it means
they cannot share an entry. Phase 4 adds the resolved RLS predicate set to what
goes into the hash, and nothing above this function changes.

Permissions rather than the relations they come from, deliberately. What
decides whether two callers may share a result is what they are able to see,
and two roles that granted exactly the same permissions would be the same
policy set under different names. Hashing the relation would split a cache
entry over a distinction that has no effect on any row.
*/
func Resolve(ctx context.Context, granter authz.Granter, scope tenant.Scope) (Fingerprint, error) {
	if !scope.IsValid() {
		return Fingerprint{}, fmt.Errorf("%w: no tenant scope", ErrUnresolved)
	}

	actor := scope.ActorID()
	if !actor.Valid {
		// Background work, acting as Pivot itself. It has its own fingerprint
		// rather than an empty one, so that what a scheduled refresh caches is
		// reachable only by other scheduled work -- and never by a person who
		// happens to hold no grants.
		return fingerprint(scope.OrgID().String(), []string{"system"}), nil
	}

	if granter == nil {
		return Fingerprint{}, fmt.Errorf("%w: no resolver configured", ErrUnresolved)
	}

	relations, err := granter.Grants(ctx, authz.User(actor.UUID.String()), authz.Object{
		Type: authz.TypeOrganization,
		ID:   scope.OrgID().String(),
	})
	if err != nil {
		return Fingerprint{}, fmt.Errorf("%w: %w", ErrUnresolved, err)
	}

	return fingerprint(scope.OrgID().String(), permissionsOf(relations)), nil
}

/*
permissionsOf flattens a set of held relations into the permissions they carry.

A relation that is not a built-in role contributes nothing, and that is the
conservative direction: an unrecognized grant resolves to no additional
capability, so a caller holding one shares a cache entry only with callers
whose *recognized* standing matches. The alternative -- treating an unknown
relation as its own opaque input to the hash -- is also defensible, and would
be the right change on the day relations stop being a closed set.
*/
func permissionsOf(relations []authz.Relation) []string {
	held := make(map[authz.Permission]bool)

	for _, relation := range relations {
		if !authz.IsBuiltinRole(relation) {
			continue
		}

		for _, permission := range authz.PermissionsFor(relation) {
			held[permission] = true
		}
	}

	out := make([]string, 0, len(held))
	for permission := range held {
		out = append(out, string(permission))
	}

	slices.Sort(out)

	return out
}

/*
fingerprint hashes the tenant and the sorted capability list.

The tenant is in the hash rather than only in the cache key so that the
fingerprint is meaningful on its own: it is written to logs and compared in
tests, and one that matched across organizations would look like a sharing
opportunity to anybody reading it.

Elements are length-prefixed. Joining "view" and "content" with a separator
produces the same bytes as joining "view|content" alone, and a hash that two
different policy sets can both produce is the one failure this whole package
exists to prevent.
*/
func fingerprint(orgID string, elements []string) Fingerprint {
	var b strings.Builder

	b.WriteString(Version)
	writeElement(&b, orgID)

	for _, element := range elements {
		writeElement(&b, element)
	}

	sum := sha256.Sum256([]byte(b.String()))

	return Fingerprint{sum: hex.EncodeToString(sum[:])}
}

func writeElement(b *strings.Builder, element string) {
	fmt.Fprintf(b, "|%d:%s", len(element), element)
}
