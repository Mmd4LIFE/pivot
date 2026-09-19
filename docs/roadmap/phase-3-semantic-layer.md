# Phase 3 — Semantic Layer

**Months:** M7–M9 · **Effort:** 44 ew · **Team:** 7 · **Release:** **v0.5**

---

## Goal

Build the spine. A governed, version-controlled definition of what the business's data
means — models, dimensions, metrics, joins, and access policies — that every other surface
compiles against.

This is the phase that separates Pivot from Metabase and Superset. It is also the phase
most likely to be underestimated, because a semantic layer is a compiler, and compilers are
harder than they look.

## Exit criteria

- [ ] A dbt project imports; every metric produces output identical to `dbt` itself
- [ ] A metric defined once returns the same value in a chart, the API, and an export
- [ ] The compiler generates correct SQL for all 8 v0.1 dialects (golden-file verified)
- [ ] A cross-database join executes correctly through DuckDB federation
- [ ] An accelerated model serves a dashboard without touching the source warehouse
- [ ] Semantic definitions round-trip to Git and back with no loss

---

## 1. Semantic model definition

| ID | Feature | Pri | Size |
|---|---|---|---|
| P3-SEM-001 | YAML schema for models, dimensions, measures, and metrics | P0 | L |
| P3-SEM-002 | Model = a table, view, or SQL expression with typed columns | P0 | M |
| P3-SEM-003 | Dimensions: categorical, temporal, geo, with hierarchies | P0 | M |
| P3-SEM-004 | Measures: aggregations over a model's columns | P0 | M |
| P3-SEM-005 | Metrics: named business calculations built on measures | P0 | L |
| P3-SEM-006 | Ratio, derived, and cumulative metrics | P0 | L |
| P3-SEM-007 | Time-based metrics: period-over-period, rolling windows, YTD/QTD/MTD | P0 | L |
| P3-SEM-008 | Join graph: declared relationships with cardinality and fan-out safety | P0 | XL |
| P3-SEM-009 | Metric-level filters and default filters | P0 | M |
| P3-SEM-010 | Formatting and display metadata (units, currency, precision) | P0 | S |
| P3-SEM-011 | Validation with actionable errors and a `pivot validate` CLI | P0 | M |
| P3-SEM-012 | Semantic types shared with the catalog layer | P0 | S |

**The join graph (P3-SEM-008) is the hardest correctness problem in the product.** When a
user selects a measure from one model and a dimension from another, the compiler must find
a join path, and — critically — detect **fan-out**: joining a one-to-many relationship
inflates the measure. `SUM(orders.revenue)` joined to `order_items` silently multiplies
revenue by the item count. Looker solved this with symmetric aggregates; we do the same.
Getting this wrong produces confidently wrong numbers, which is the worst failure mode a
BI tool has. This item gets the most test coverage in the phase.

## 2. Authoring

| ID | Feature | Pri | Size |
|---|---|---|---|
| P3-AUT-001 | Visual model editor with the join graph on a React Flow canvas | P0 | XL |
| P3-AUT-002 | YAML editor with schema validation and autocomplete | P0 | M |
| P3-AUT-003 | Two-way sync between visual and YAML representations | P0 | L |
| P3-AUT-004 | Metric builder with live preview of the resulting value | P0 | L |
| P3-AUT-005 | Auto-generate a starter model from a table, using profiling data | P0 | M |
| P3-AUT-006 | Join suggestions from foreign keys and name heuristics | P1 | M |
| P3-AUT-007 | Impact analysis: what breaks if this model changes | P1 | L |
| P3-AUT-008 | **AI: generate model descriptions and metric documentation** | P1 | M |
| P3-AUT-009 | **AI: suggest metrics from schema and query history** | P2 | M |

**P3-AUT-003 is genuinely hard and genuinely necessary.** The analytics engineer wants
YAML in Git; the analyst wants a UI. Both must edit the same definition without one
clobbering the other. The approach: YAML is the canonical form, the visual editor is a
projection, and every UI edit produces a minimal YAML diff that preserves comments and
formatting. Rejected: a database-backed model with YAML export, which makes Git the second
source of truth and guarantees drift.

## 3. Query compiler

| ID | Feature | Pri | Size |
|---|---|---|---|
| P3-CMP-001 | Semantic query AST (models, dimensions, metrics, filters, order, limit) | P0 | L |
| P3-CMP-002 | Join path resolution with shortest-path and explicit override | P0 | XL |
| P3-CMP-003 | Symmetric aggregates for fan-out-safe measures | P0 | XL |
| P3-CMP-004 | Dialect-specific SQL generation for all supported sources | P0 | XL |
| P3-CMP-005 | Predicate pushdown and filter placement optimization | P0 | L |
| P3-CMP-006 | **RLS predicate injection at compile time** | P0 | L |
| P3-CMP-007 | **Column masking applied at compile time** | P0 | M |
| P3-CMP-008 | Query plan explanation surfaced in the UI | P1 | M |
| P3-CMP-009 | Golden-file test suite: semantic query → expected SQL per dialect | P0 | L |
| P3-CMP-010 | Compile-time cost estimation and guardrails | P1 | M |

**P3-CMP-006 and 007 are built in Phase 3 and activated in Phase 4.** The compiler takes a
user context and injects row filters and column masks before generating SQL. Building the
hooks now means Phase 4 adds policies, not plumbing — and, more importantly, it means there
is exactly one place where access control is applied, matching
[architectural rule #1](00-principles.md#3-architectural-rules).

**P3-CMP-009 is the phase's safety net.** A directory of semantic queries with expected SQL
output per dialect, diffed on every commit. Compiler changes are terrifying without it,
because a subtle change in join ordering produces different numbers and no test failure.

## 4. dbt integration

| ID | Feature | Pri | Size |
|---|---|---|---|
| P3-DBT-001 | Import `manifest.json`: models, columns, descriptions, tests | P0 | L |
| P3-DBT-002 | Import dbt metrics / MetricFlow semantic models | P0 | L |
| P3-DBT-003 | Continuous sync from a Git repository | P0 | M |
| P3-DBT-004 | dbt Cloud API integration | P1 | M |
| P3-DBT-005 | Surface dbt test results as freshness and quality indicators | P1 | M |
| P3-DBT-006 | Map dbt lineage into Pivot's lineage graph | P1 | M |
| P3-DBT-007 | Detect drift between dbt definitions and Pivot overrides | P1 | M |
| P3-DBT-008 | Export Pivot semantic models to dbt format | P2 | M |

**This is the wedge from [the vision](../vision.md#6-the-wedge).** A company running dbt
points Pivot at its repo and gets a governed BI layer immediately. Everything else in the
product becomes reachable from a 30-minute setup. P3-DBT-001 through 003 are the most
commercially important items in this phase.

## 5. Federation & acceleration

| ID | Feature | Pri | Size |
|---|---|---|---|
| P3-FED-001 | Cross-source query planning (split, execute, join locally) | P0 | XL |
| P3-FED-002 | DuckDB execution layer for federated joins | P0 | L |
| P3-FED-003 | Extract-to-Parquet materialization on object storage | P0 | L |
| P3-FED-004 | Acceleration policy per model (full refresh, schedule, TTL) | P0 | M |
| P3-FED-005 | Transparent routing: accelerated if fresh, source if not | P0 | L |
| P3-FED-006 | Incremental refresh with a watermark column | P1 | L |
| P3-FED-007 | Acceleration monitoring: freshness, size, hit rate, cost saved | P1 | M |
| P3-FED-008 | Automatic acceleration recommendations from query patterns | P2 | M |

**P3-FED-005 is the headline feature.** The user changes nothing; queries silently run
against a local Parquet extract when it's fresh enough for the request's freshness
requirement, and against the source when it isn't. The UI shows which happened and why.
This is the single largest warehouse-cost lever in the product — and the strongest answer
to "why not just use Metabase."

**Open question #4** from [tech-stack.md](../architecture/tech-stack.md#13-open-questions):
whether incremental refresh (P3-FED-006) must ship in this phase. It is currently P1;
full-refresh-only is acceptable for v0.5 if the phase is running hot.

## 6. Caching v2

| ID | Feature | Pri | Size |
|---|---|---|---|
| P3-CCH-001 | Semantic cache invalidation on model definition change | P0 | L |
| P3-CCH-002 | Source freshness detection (max timestamp, row count, source metadata) | P0 | M |
| P3-CCH-003 | Per-model and per-metric cache policies | P0 | M |
| P3-CCH-004 | Cache warming driven by dashboard schedules | P1 | M |
| P3-CCH-005 | Partial cache reuse (serve a subset from cache, fetch the rest) | P2 | L |
| P3-CCH-006 | Cache observability: hit rate, size, evictions, savings | P1 | M |

See [ADR-0006](../architecture/adr/0006-caching-strategy.md).

## 7. Versioning & Git

| ID | Feature | Pri | Size |
|---|---|---|---|
| P3-GIT-001 | Semantic layer serialized to a Git-friendly file tree | P0 | M |
| P3-GIT-002 | Git sync: pull from and push to a branch | P0 | L |
| P3-GIT-003 | Development branches with isolated semantic definitions | P1 | L |
| P3-GIT-004 | PR-based review flow for model changes | P1 | M |
| P3-GIT-005 | Change history with authorship and diff | P0 | M |
| P3-GIT-006 | Environment promotion (dev → staging → prod) | P1 | L |
| P3-GIT-007 | CI validation of semantic definitions (`pivot validate` in a workflow) | P0 | S |

## 8. Exploration on the semantic layer

| ID | Feature | Pri | Size |
|---|---|---|---|
| P3-EXP-001 | Query builder switches source from tables to semantic models | P0 | L |
| P3-EXP-002 | Metric browser with descriptions, lineage, and owners | P0 | M |
| P3-EXP-003 | Only valid dimension/metric combinations are selectable | P0 | M |
| P3-EXP-004 | Metric detail page: definition, SQL, usage, dependents | P0 | M |
| P3-EXP-005 | Certified/verified metric badges | P1 | S |
| P3-EXP-006 | Metric usage analytics | P1 | M |

**P3-EXP-003 is a quiet but important difference.** In a table-based builder, users can
construct meaningless queries. In a semantic builder, the join graph determines what's
valid, and invalid combinations aren't offered. Users stop producing wrong answers because
wrong answers become unreachable.

---

## Technical notes

### Why build rather than adopt Cube
Cube is a good product and adopting it would save months. Rejected because: it's a separate
service with its own datastore, deployment, and auth model — breaking the single-binary
commitment; RLS enforcement would live outside our compiler, breaking
[architectural rule #1](00-principles.md#3-architectural-rules); and the semantic layer is
the core differentiator, so owning it matters. We remain **compatible** — Cube's model
format is an import target. See [ADR-0005](../architecture/adr/0005-semantic-layer.md).

### The compiler is the security boundary
Every query in Pivot — UI, API, alert, flow, AI — passes through the semantic compiler,
which applies the requesting user's RLS predicates and column masks before emitting SQL.
There is no second code path. This is why the compiler is built before the permission
model.

### Symmetric aggregates, concretely
Given `orders` (1) → `order_items` (many), `SUM(orders.total)` after the join
double-counts. The fix is to aggregate over a distinct key:
`SUM(DISTINCT order_id_hash + total) - SUM(DISTINCT order_id_hash)`, with dialect-specific
implementations. Where a dialect can't express it, the compiler falls back to a subquery
pre-aggregation and records why in the query plan. Fan-out that cannot be made safe is a
hard error, never a silent wrong number.

---

## Explicitly deferred

| Deferred | To | Why |
|---|---|---|
| Typed expression language for metrics | Post-v2.0 | YAML + the Phase 2 formula language is enough; a real language needs its own phase |
| Multi-hop join path ambiguity resolution UI | Phase 4 | Compiler picks shortest path; manual override exists |
| Streaming / real-time models | Phase 9 | Different execution model |
| Cube and LookML importers | Phase 10 | dbt is the wedge; others follow demand |
| Aggregate awareness (auto-routing to rollup tables) | Phase 6 | Depends on Flows materialization |

---

## Risks

| Risk | Mitigation |
|---|---|
| Fan-out handling is subtly wrong and produces bad numbers | Golden-file tests per dialect; a correctness suite built from known-tricky schemas; fan-out that can't be made safe errors rather than guesses |
| The semantic layer grows into LookML | YAML-only, frozen schema at phase start. New capability requires an ADR |
| dbt metric semantics diverge from ours and imports are lossy | Conformance test: import a reference dbt project and diff every metric against `dbt`'s own output |
| Two-way YAML/visual sync corrupts hand-written files | Round-trip property tests; preserve comments; never rewrite a file we can't parse losslessly |
| Federation performance is worse than pushing down | Cost-based decision: federate only when a single-source plan is impossible or measurably slower |
| Compiler complexity makes the phase overrun | The compiler is the phase. If Git sync or acceleration slips to Phase 4, that's acceptable; compiler correctness is not |
