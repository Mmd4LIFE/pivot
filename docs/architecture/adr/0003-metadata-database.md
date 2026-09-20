# ADR-0003: PostgreSQL primary, SQLite for quickstart

**Status:** Accepted
**Date:** 2026-09-19

## Context

Pivot needs a database for its own metadata: users, permissions, connections, semantic
models, dashboards, alerts, flows, audit logs, and query history.

Two requirements pull in opposite directions:

- **Production** needs concurrency, replication, HA, JSON support, full-text search, vector
  search, and partitioning for high-volume tables.
- **The 30-second install** needs zero external dependencies. Requiring a Postgres instance
  before a user can try Pivot kills the on-ramp that
  [ADR-0001](0001-backend-language.md) was chosen to protect.

## Decision

PostgreSQL 16+ as the production metadata database. SQLite 3.45+ as the zero-config default
for new installs and development. Every schema change and query must work on both, verified
in CI. A documented `pivot migrate` path upgrades SQLite → Postgres.

## Rationale

Supporting both is the only way to get both properties. The alternative — requiring
Postgres — means the first-run experience is `docker compose up` and a connection string,
which loses the users who would have tried the binary.

Postgres earns the production role on JSONB (for schema-flexible chart and flow
definitions), `pgvector` (semantic search without a second datastore), partitioning (for
`query_log` and `audit_log`, the only unbounded tables), mature HA, and the fact that
essentially every prospective user already knows how to operate it.

SQLite is a genuine production choice for small teams, not just a demo mode: a
20-person company running one Pivot binary with a SQLite file is a supported configuration.

## Alternatives considered

### Postgres only
Simpler in every way — one dialect, one test target, no portability tax on migrations. The
cost is the first-run experience, and we judge that cost higher. This is the option to fall
back to if the portability tax proves worse than estimated.

### SQLite only
Cannot support multi-node deployments, which ends the story at Phase 9.

### Embedded Postgres (e.g. `embedded-postgres`)
Ships a real Postgres binary inside the application, giving one dialect and zero
dependencies. Rejected: it adds ~100MB to the binary, has platform-specific quirks, and
turns process management into our problem. Genuinely tempting, and worth reconsidering if
the dual-dialect tax becomes painful.

### A document database (MongoDB)
Metadata is highly relational — permissions, lineage, and content hierarchies are graphs
and joins. Wrong shape, and it would add a dependency without removing one.

## Consequences

**Positive**
- The 30-second install works with no dependencies
- Production gets a real database with HA, replication, and partitioning
- Users grow from SQLite to Postgres on their own schedule, with a supported path
- Local development and CI are fast; no container needed for most tests

**Negative**
- **Every migration is written and tested twice.** This is the real cost, and it recurs on
  every schema change for the life of the project
- Postgres-specific features must degrade gracefully: `pgvector` semantic search falls back
  to full-text on SQLite, partitioning falls back to retention-based deletes
- `sqlc` configuration is more complex with two dialects
- Some query patterns must be written to the lower common denominator

**Neutral**
- CI runs the full suite twice, roughly doubling database test time

## Revisit if

- **The portability tax exceeds 15% of migration effort**, measured at the end of each
  phase. If it does, demote SQLite to development-only and require Postgres for production.
  This threshold is stated explicitly so the decision can be made on data rather than on
  accumulated irritation.
- Embedded Postgres matures enough to replace both
- A feature we genuinely need has no acceptable SQLite degradation

---

## Measurements

### 2026-09-20 — after Parts 3-a and 3-b (schema v1, five tables)

| Cost | PostgreSQL | SQLite | Note |
|---|---|---|---|
| sqlc type overrides | 6 (`db_type`) | **34 (per-column)** | SQLite has no db_type to key on |
| Migration lines | 86 | 86 | A full mirror |
| Query lines | 120 | 120 | A full mirror |
| Bridging code | — | 152 (`dbtypes`) | Shared, but exists only because of SQLite |

SQLite-attributable: roughly **460 lines plus 34 config entries**, against ~2,800 lines of
store code — call it **16%**, marginally over the threshold.

**The decision stands, for now**, because the headline number overstates the ongoing cost
and the structural risk was eliminated rather than merely absorbed.

Custom column types (`dbtypes.Time`, `Bool`, `JSON`) make the two generated packages
**byte-identical apart from the package clause** — same models, same Querier, same params
structs. Two consequences matter more than the line count:

1. Because the structs are structurally identical, Go permits direct conversion between
   them (`pg.CreateOrganizationParams(p)`). The repository layer in Part 4 therefore needs
   one conversion per type, not a hand-written field-by-field mapping per engine.
2. Divergence can no longer be silent. A missing override changes a generated type, and
   the round-trip tests fail on the engine that drifted.

The marginal cost of a new table is now: mirror the migration, mirror the queries, add
~7 override lines. That is mechanical and caught by tests when skipped — which is a very
different risk profile from 16% of effort spent on judgment calls.

**Re-measure at the end of Phase 1**, when the catalog tables land and the schema roughly
triples. The number to watch is not total lines but whether any override or query mirror
has ever been forgotten and shipped.
