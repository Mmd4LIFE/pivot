# Phase 4 — Collaborate & Govern

**Months:** M9–M11 · **Effort:** 40 ew · **Team:** 8 · **Release:** **v0.7**

---

## Goal

Make Pivot trustworthy enough for a company to put real data in it. Fine-grained
permissions, row-level security, column masking, audit logging, lineage, and the
collaboration features that turn a tool into a shared workspace.

This is the phase that decides whether a security review passes.

## Exit criteria

- [ ] External penetration test finds no critical or high findings
- [ ] RLS verified: a user cannot retrieve a restricted row through *any* surface — UI,
      API, export, alert, embed, or cached result
- [ ] Every data access produces an audit record with actor, object, and query
- [ ] Column-level lineage resolves from a dashboard card to source columns
- [ ] Permission changes take effect within 5 seconds across a multi-node deployment

---

## 1. Permission model

| ID | Feature | Pri | Size |
|---|---|---|---|
| P4-PRM-001 | Collection hierarchy with inherited permissions | P0 | L |
| P4-PRM-002 | Per-object permissions (view, edit, manage) with explicit override | P0 | L |
| P4-PRM-003 | Group-based assignment with nested groups | P0 | M |
| P4-PRM-004 | Connection-level permissions (who can query what source) | P0 | M |
| P4-PRM-005 | Model- and metric-level permissions | P0 | M |
| P4-PRM-006 | Native SQL permission, separate from builder access | P0 | M |
| P4-PRM-007 | Download/export permission, independent of view | P0 | S |
| P4-PRM-008 | Permission debugger: "why can this user see this?" | P0 | L |
| P4-PRM-009 | Bulk permission management | P1 | M |
| P4-PRM-010 | Custom roles with composable capability sets | P1 | L |
| P4-PRM-011 | Permission templates for common setups | P2 | S |

**P4-PRM-008 is not a nice-to-have.** Every BI tool at scale produces the question "why
can Dana see the salary dashboard?" and every tool answers it badly. The debugger traces
the actual OpenFGA decision path and renders it: this user is in this group, which has
editor on this collection, which this dashboard inherits from. It cuts support load and
prevents the misconfiguration that causes leaks.

**P4-PRM-006 and 007 exist because they're the two most common governance failures.**
Native SQL access bypasses semantic-layer RLS by definition — a user who can write
arbitrary SQL against a connection can read anything that connection can. So it's a
distinct, deliberately-granted permission, and the UI says exactly that. Download
permission is separate because "can look at an aggregate on screen" and "can export the
underlying PII to a spreadsheet" are genuinely different decisions.

## 2. Row- and column-level security

| ID | Feature | Pri | Size |
|---|---|---|---|
| P4-RLS-001 | RLS policies attached to semantic models | P0 | XL |
| P4-RLS-002 | User attributes (from SSO claims, SCIM, or manual assignment) | P0 | M |
| P4-RLS-003 | Attribute-based filters (`region = {{user.region}}`) | P0 | L |
| P4-RLS-004 | Lookup-table-based policies (mapping table joins) | P0 | L |
| P4-RLS-005 | Group-based policies | P0 | M |
| P4-RLS-006 | Column masking: hash, redact, partial, nullify | P0 | L |
| P4-RLS-007 | Policy simulation: preview results as another user | P0 | M |
| P4-RLS-008 | RLS-aware cache partitioning | P0 | L |
| P4-RLS-009 | Policy test framework (assertions in CI) | P0 | M |
| P4-RLS-010 | Default-deny mode for models without a policy | P1 | S |
| P4-RLS-011 | Policy coverage report | P1 | S |

**P4-RLS-008 completes the cache design started in
[Phase 1](phase-1-connect-and-query.md#4-query-execution-engine).** The cache key includes
a hash of the resolved policy set, so users under identical policies share cached results
and users under different policies are structurally unable to. The Phase 1 test cases for
this are already written; this is where they go green.

**P4-RLS-009 is what makes RLS maintainable.** Policies are code, and code without tests
rots. Declarative assertions — "user with region=EU sees 1,204 rows; user with region=US
sees 3,891; user with no region sees 0" — run in CI on every semantic layer change. A
policy regression is caught in a PR, not by a customer.

## 3. Audit & compliance

| ID | Feature | Pri | Size |
|---|---|---|---|
| P4-AUD-001 | Audit log: every read, write, permission change, and login | P0 | L |
| P4-AUD-002 | Data access log: who queried what, which rows, which columns | P0 | M |
| P4-AUD-003 | Tamper-evident log (hash chain) | P1 | M |
| P4-AUD-004 | Audit search and filtering UI | P0 | M |
| P4-AUD-005 | Export to SIEM (syslog, webhook, S3) | P1 | M |
| P4-AUD-006 | Configurable retention policies | P0 | S |
| P4-AUD-007 | Admin activity report | P1 | S |
| P4-AUD-008 | Failed-access-attempt monitoring and alerting | P1 | M |
| P4-AUD-009 | GDPR: data subject export and deletion | P1 | M |
| P4-AUD-010 | PII tagging on catalog columns | P1 | M |

## 4. Lineage & impact

| ID | Feature | Pri | Size |
|---|---|---|---|
| P4-LIN-001 | Column-level lineage from source through models to charts | P0 | XL |
| P4-LIN-002 | Interactive lineage graph UI | P0 | L |
| P4-LIN-003 | Impact analysis: what breaks if this column changes | P0 | L |
| P4-LIN-004 | Reverse lineage: where does this dashboard number come from | P0 | M |
| P4-LIN-005 | Lineage extraction from raw SQL (via SQLGlot) | P1 | L |
| P4-LIN-006 | Open Lineage / OpenMetadata export | P2 | M |
| P4-LIN-007 | Breaking-change alerts on schema drift | P1 | M |

**P4-LIN-004 is the feature executives use without knowing its name.** Clicking a number
on a dashboard and seeing the full chain — metric definition, model, source table, source
column, last refresh — is how trust is built. It's also the fastest way to resolve the
"these two dashboards disagree" conversation.

## 5. Collaboration

| ID | Feature | Pri | Size |
|---|---|---|---|
| P4-COL-001 | Comments on dashboards, cards, questions, and models | P0 | L |
| P4-COL-002 | @mentions with notification | P0 | M |
| P4-COL-003 | Point annotations on charts (a note pinned to a data point) | P1 | L |
| P4-COL-004 | Threaded discussions with resolve | P1 | M |
| P4-COL-005 | Activity feed per object and per user | P1 | M |
| P4-COL-006 | Notification center with per-channel preferences | P0 | M |
| P4-COL-007 | Presence indicators (who else is viewing) | P2 | M |
| P4-COL-008 | Shareable annotated snapshots | P2 | M |

**P4-COL-003 is underrated.** "Revenue dipped here because of the outage on the 14th" is
institutional knowledge that otherwise lives in Slack and evaporates. Anchoring it to the
data point means the next person to see the dip gets the explanation.

## 6. Content governance

| ID | Feature | Pri | Size |
|---|---|---|---|
| P4-GOV-001 | Verified/certified content with reviewer and date | P0 | M |
| P4-GOV-002 | Ownership assignment and orphaned-content detection | P0 | M |
| P4-GOV-003 | Deprecation workflow with redirect to a replacement | P1 | M |
| P4-GOV-004 | Duplicate detection (near-identical questions) | P2 | M |
| P4-GOV-005 | Content lifecycle: stale, unused, archive candidates | P1 | M |
| P4-GOV-006 | Usage analytics per object (views, users, last accessed) | P0 | M |
| P4-GOV-007 | Official vs. personal content separation | P0 | S |
| P4-GOV-008 | Bulk archive and cleanup tooling | P1 | S |

**P4-GOV-005 addresses the failure mode from
[the vision](../vision.md#11-the-easy-tool-ceiling).** Every mature BI install accumulates
thousands of dead dashboards, and nobody dares delete any of them. Surfacing "this hasn't
been opened in 9 months and its owner left" makes cleanup safe and routine instead of
scary and deferred forever.

## 7. Query governance

| ID | Feature | Pri | Size |
|---|---|---|---|
| P4-QG-001 | Cost attribution per user, group, dashboard, and connection | P0 | L |
| P4-QG-002 | Query budgets with soft and hard limits | P0 | L |
| P4-QG-003 | Expensive-query detection and alerting | P0 | M |
| P4-QG-004 | Automatic throttling of runaway consumers | P1 | M |
| P4-QG-005 | Optimization recommendations (acceleration candidates) | P1 | M |
| P4-QG-006 | Warehouse cost dashboard | P0 | M |
| P4-QG-007 | Per-connection maintenance windows | P2 | S |

**This is Priya's surface** from [the vision](../vision.md#primary-personas), and it's a
genuine competitive gap. No open-source BI tool tells you that one dashboard is responsible
for 40% of the Snowflake bill. Pivot does, because the query log
([P1-QE-007](phase-1-connect-and-query.md#4-query-execution-engine)) was designed for it
from the first phase.

---

## Technical notes

### Permission changes must propagate fast
A revoked permission that takes a minute to take effect is a vulnerability. Authorization
decisions are cached in Valkey for performance; writes to the OpenFGA relationship graph
publish invalidation events that all nodes consume. Target: under 5 seconds at p99,
verified by an automated test in CI.

### Audit logging cannot be lossy or blocking
Audit writes go to a durable buffer and are flushed asynchronously — a slow audit sink must
never slow a query. But audit loss is a compliance failure, so the buffer is disk-backed
and survives restart. If the buffer fills, the system fails *closed* on
audit-required operations rather than silently dropping records. That tradeoff is
configurable, and the default is the safe one.

### Lineage is computed, not declared
Column-level lineage comes from three sources: the semantic layer (explicit and reliable),
dbt's manifest (explicit and reliable), and parsing raw SQL via SQLGlot (best-effort).
The UI distinguishes confident lineage from inferred lineage, because presenting a guess
as a fact is how lineage tools lose credibility.

---

## Explicitly deferred

| Deferred | To | Why |
|---|---|---|
| SOC 2 certification | Phase 9 | Controls built here; audit period comes later |
| Real-time collaborative editing | Post-v2.0 | High complexity, unclear demand for BI authoring |
| Data quality monitoring | Phase 6 | Belongs with Flows |
| Cross-org content sharing | Phase 8 | Needs the embedding model |
| Approval workflows for publishing | Phase 9 | Enterprise governance; verified content covers most needs |

---

## Risks

| Risk | Mitigation |
|---|---|
| RLS has a bypass through some surface (export, alert, embed, cache) | Enforcement lives only in the compiler; an automated test asserts every surface routes through it; external pentest is an exit criterion |
| The permission model is powerful and incomprehensible | The debugger is P0; usability-test permission setup with 5 real admins before the phase closes |
| Column-level lineage from raw SQL is unreliable | Mark inferred lineage distinctly; never present a guess as a fact |
| Audit log volume overwhelms Postgres | Partition by time, aggressive retention defaults, documented SIEM export for long-term storage |
| Cache partitioning by policy destroys the hit rate | Measure early; policies are hashed as a set so identical-policy users share cache |
