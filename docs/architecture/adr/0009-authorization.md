# ADR-0009: OpenFGA for authorization, enforced at the query compiler

**Status:** Accepted
**Date:** 2026-09-19

## Context

Permissions are where BI tools fail at scale. The requirements are genuinely recursive:

- Collections nest, and permissions inherit down unless explicitly overridden
- Groups nest, and users belong to many
- A dashboard card querying a model the viewer can't access must be denied even when the
  dashboard itself is shared
- Row-level security filters results by user attributes
- "Can view an aggregate" and "can export the underlying rows" are different grants
- "Can build a query" and "can write arbitrary SQL" are different grants

Two separate questions: how to *model* permissions, and where to *enforce* them.

## Decision

**Model** with OpenFGA — an open-source implementation of Google's Zanzibar,
relationship-based access control. Embedded as a library in single-node mode, a separate
service in multi-node.

**Enforce** in the semantic query compiler. Every query — UI, API, alert, flow, export,
embed, AI — is compiled by one component that injects RLS predicates and column masks before
emitting SQL. Connectors accept compiled query objects, not SQL strings, so no other path
to data exists.

## Rationale

### Why relationships rather than a permissions table

The requirements above are a graph traversal. Expressed in SQL, they become a recursive CTE
that nobody can debug, that's slow, and whose behavior nobody can predict from reading it.
Expressed as a relationship model, they're declarative, testable, and — critically —
*explainable*, which is what makes the permission debugger
([P4-PRM-008](../../roadmap/phase-4-collaboration-governance.md#1-permission-model))
possible at all. "Why can Dana see this?" is a question every BI tool gets and most answer
badly.

Zanzibar is battle-tested at Google's scale for exactly this problem shape, and OpenFGA is
a mature open-source implementation with a permissive license.

### Why enforcement lives in the compiler

This is the more important half of the decision.

If authorization is checked in API handlers, then every new surface is a new opportunity to
forget. Pivot has many surfaces — and by Phase 7, one of them is an AI generating queries.
Handler-level checks would mean auditing every one of them forever, and the audit would
eventually fail.

Putting enforcement in the single component that produces SQL inverts the property: a new
surface inherits the entire permission model automatically, because the only thing it can
do is hand a query to the compiler. When Phase 7 adds an AI analyst, it gets every RLS
policy for free.

This is [architectural rule #1](../../roadmap/00-principles.md#3-architectural-rules), and
it's asserted by an automated test rather than trusted to review.

### Why it fails closed

If OpenFGA is unreachable, Pivot denies all access. An authorization system that fails open
is worse than none, because it creates the false belief that access is controlled. This
makes OpenFGA a hard dependency, addressed by HA rather than by degradation — a tradeoff
recorded in [system-design.md](../system-design.md#7-failure-domains).

## Alternatives considered

### Hand-rolled RBAC in Postgres
What most products do, and what Metabase and Superset do. Simple at first. Rejected: it
becomes a recursive CTE with inheritance and overrides, and then it becomes the thing nobody
wants to touch. This is the identified fallback if OpenFGA's embedded mode proves immature —
behind the same interface, which is why the interface exists.

### Casbin
Lighter, Go-native, no separate service. Rejected: policy-based rather than
relationship-based, which fits role checks well and fits nested inheritance with overrides
poorly. Explaining a decision is also harder.

### SpiceDB
Also Zanzibar-inspired and technically excellent. Rejected narrowly: OpenFGA has better
embedding support for our single-binary mode, and the CNCF governance is a mild advantage
for an open-source product. Close call; either would work.

### Cloud IAM (AWS/GCP)
Cloud-specific, unavailable self-hosted or air-gapped. Non-starter.

### Enforcement in API handlers
Rejected on the reasoning above. It's the default approach and it's how bypasses happen.

## Consequences

**Positive**
- Nested collections, nested groups, and inheritance-with-override are expressible and
  debuggable
- Permission decisions are explainable, which enables the debugger
- **New surfaces cannot introduce authorization bypasses** — the property the whole design
  is for
- RLS, masking, and object permissions are enforced in one place, so they're testable in
  one place

**Negative**
- **Another component to deploy and operate** in multi-node mode
- **OpenFGA is a hard dependency that fails closed**, meaning its availability is Pivot's
  availability
- Authorization decisions need aggressive caching to meet the 10ms p95 budget, which adds
  an invalidation problem — permission changes must propagate in under 5 seconds, verified
  by test
- The relationship model is a new concept for contributors, with a real learning curve
- A compiler-only enforcement path means the compiler is on every hot path, so its
  performance is everyone's performance

**Neutral**
- Native SQL still bypasses semantic RLS by definition. Handled by making it a separate
  permission and documenting it plainly in
  [the security model](../security-model.md#12-known-limitations-stated-plainly), not by
  pretending otherwise

## Revisit if

- OpenFGA's embedded mode proves unsuitable (fall back to hand-rolled RBAC behind the same
  interface)
- Authorization latency cannot meet the 10ms p95 budget even with caching
- OpenFGA's maintenance or licensing status changes
