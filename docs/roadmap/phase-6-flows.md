# Phase 6 — Flows

**Months:** M13–M16 · **Effort:** 68 ew · **Team:** 10 · **Release:** **v1.0 GA**

---

## Goal

Let Pivot move and shape data, not just read it. A visual pipeline builder for ingestion,
transformation, materialization, data quality, and reverse-ETL — running on the same
scheduler, permission model, and lineage graph as everything else.

This is the phase that closes the loop from [the vision](../vision.md#13-the-stitched-together-stack):
the company that would otherwise run Airflow + dbt + Fivetran + Monte Carlo + Census gets
the 80% case in one system.

**v1.0 GA ships at the end of this phase.** Not because Flows is required for BI, but
because a platform that can only read is a feature, and one that can also write is a
product.

## Exit criteria

- [ ] A flow ingests from an API, transforms, tests, and materializes on a schedule
- [ ] A failed node resumes from its failure point without re-running upstream work
- [ ] A flow processing 100M rows completes within its SLA and within its memory budget
- [ ] Flow outputs appear in lineage alongside dbt and semantic models
- [ ] 10 companies running Pivot in production; p95 dashboard load under 2s; 99.9% uptime

---

## 1. Flow engine

| ID | Feature | Pri | Size |
|---|---|---|---|
| P6-ENG-001 | DAG definition, validation, and cycle detection | P0 | L |
| P6-ENG-002 | Executor with topological scheduling and parallel branches | P0 | XL |
| P6-ENG-003 | Per-node state persistence and checkpointing | P0 | L |
| P6-ENG-004 | Resume from failure without re-running successful nodes | P0 | L |
| P6-ENG-005 | Retry policies (count, backoff, per-node override) | P0 | M |
| P6-ENG-006 | Timeouts at node and flow level | P0 | S |
| P6-ENG-007 | Concurrency control and resource pools | P0 | M |
| P6-ENG-008 | Conditional branching and skip logic | P0 | M |
| P6-ENG-009 | Fan-out / fan-in (map over a list, collect results) | P1 | L |
| P6-ENG-010 | Sub-flows (a flow as a node in another flow) | P1 | M |
| P6-ENG-011 | Backfill with parameterized date ranges | P0 | L |
| P6-ENG-012 | Incremental processing with watermark state | P0 | L |
| P6-ENG-013 | Temporal backend as an opt-in alternative to River | P2 | L |

**P6-ENG-004 is the feature that makes flows usable in anger.** A 12-node pipeline that
fails at node 10 must not re-run nodes 1–9. That requires materializing intermediate
results (to Parquet on object storage) and a state model that knows what's still valid.
It's the difference between "re-run it and get coffee" and "re-run it and go home."

**P6-ENG-012 is where correctness gets hard.** Incremental processing needs
exactly-once-ish semantics under retries: watermarks advance only on confirmed commit,
writes are idempotent via merge keys, and a retry after partial success must not duplicate
rows. The design uses staging tables with atomic swap, never direct appends.

## 2. Visual builder

| ID | Feature | Pri | Size |
|---|---|---|---|
| P6-BLD-001 | React Flow canvas with node palette and connections | P0 | XL |
| P6-BLD-002 | Node configuration panels, typed and validated | P0 | L |
| P6-BLD-003 | Live data preview at any node | P0 | XL |
| P6-BLD-004 | Schema propagation and type checking across edges | P0 | L |
| P6-BLD-005 | Inline validation with actionable errors | P0 | M |
| P6-BLD-006 | Auto-layout and canvas navigation | P1 | M |
| P6-BLD-007 | Node search, grouping, and comments on canvas | P1 | M |
| P6-BLD-008 | Version history and diff | P0 | M |
| P6-BLD-009 | YAML/JSON representation with Git sync | P0 | L |
| P6-BLD-010 | Flow templates library | P1 | M |
| P6-BLD-011 | **AI: generate a flow from a natural-language description** | P2 | L |

**P6-BLD-003 is what makes a visual pipeline builder actually better than code.** Clicking
any node and seeing the data as it exists at that point — sampled, fast, without running
the whole pipeline — is the debugging experience Airflow has never had. It requires the
engine to support partial, sampled execution of an arbitrary DAG prefix. XL, and worth it.

## 3. Source nodes

| ID | Node | Pri | Size |
|---|---|---|---|
| P6-SRC-001 | Database table / query (any connector) | P0 | M |
| P6-SRC-002 | File: CSV, JSON, Parquet, Excel from object storage or upload | P0 | M |
| P6-SRC-003 | REST API with pagination, auth, and rate limiting | P0 | L |
| P6-SRC-004 | Webhook receiver (event-triggered flows) | P1 | M |
| P6-SRC-005 | Semantic model / metric as a source | P0 | S |
| P6-SRC-006 | Google Sheets | P1 | M |
| P6-SRC-007 | SaaS: Salesforce, HubSpot, Stripe, Google Analytics, Shopify | P1 | XL |
| P6-SRC-008 | S3 / GCS / Azure Blob file listing and watch | P1 | M |
| P6-SRC-009 | Kafka consumer (micro-batch) | P2 | L |
| P6-SRC-010 | Airbyte / Singer connector bridge | P2 | L |

**P6-SRC-010 is the pragmatic hedge on connector coverage.** Rather than writing 200 SaaS
connectors, bridge to the Airbyte and Singer ecosystems, which already have them. We
hand-build only the top-5 highest-demand sources (P6-SRC-007) where the integration quality
matters most.

## 4. Transform nodes

| ID | Node | Pri | Size |
|---|---|---|---|
| P6-TRN-001 | SQL transform (DuckDB or pushdown to source) | P0 | L |
| P6-TRN-002 | Filter, select, rename, cast | P0 | M |
| P6-TRN-003 | Join (all types, with fan-out warnings) | P0 | M |
| P6-TRN-004 | Union and append | P0 | S |
| P6-TRN-005 | Aggregate and group-by | P0 | M |
| P6-TRN-006 | Pivot and unpivot | P0 | M |
| P6-TRN-007 | Window functions | P1 | M |
| P6-TRN-008 | Deduplicate with a key and tiebreak rule | P0 | S |
| P6-TRN-009 | Computed columns using the Phase 2 expression language | P0 | M |
| P6-TRN-010 | Lookup / enrichment from another dataset | P1 | M |
| P6-TRN-011 | **Python transform** (sandboxed) | P0 | XL |
| P6-TRN-012 | JSON flatten and nested-structure handling | P1 | M |
| P6-TRN-013 | Date-spine and gap-filling | P1 | S |
| P6-TRN-014 | Sampling | P2 | XS |
| P6-TRN-015 | **AI transform** (classify, extract, summarize per row) | P2 | L |

**P6-TRN-011 is XL because sandboxing is the whole problem.** Arbitrary Python from a user
is remote code execution by design. The execution model: a separate container per run,
gVisor isolation, no network by default, read-only filesystem except a scratch directory,
hard CPU/memory/time limits, and an allowlisted package set with a documented process for
additions. This is a security surface we take on deliberately because "I need pandas for
this one step" is otherwise a hard wall — but it gets a dedicated threat model and it is
disabled by default on new installs.

**P6-TRN-015 is a genuinely new capability.** Running an LLM over rows to classify support
tickets, extract entities from free text, or summarize reviews turns unstructured data into
analyzable columns. Batched, cached by content hash, with cost controls — the failure mode
is a flow that spends $4,000 on tokens overnight, so the node has a hard budget cap that
must be set before it runs.

## 5. Data quality

| ID | Feature | Pri | Size |
|---|---|---|---|
| P6-DQ-001 | Test node: not null, unique, accepted values, range, regex | P0 | L |
| P6-DQ-002 | Referential integrity tests | P0 | M |
| P6-DQ-003 | Row count and volume anomaly tests | P0 | M |
| P6-DQ-004 | Freshness tests | P0 | S |
| P6-DQ-005 | Custom SQL tests | P0 | S |
| P6-DQ-006 | Severity: warn vs. fail-the-flow | P0 | S |
| P6-DQ-007 | Quarantine failing rows to a separate output | P1 | M |
| P6-DQ-008 | Data quality dashboard with trends | P0 | M |
| P6-DQ-009 | Quality score per dataset, surfaced in the catalog | P1 | M |
| P6-DQ-010 | dbt test results integrated into the same view | P1 | S |
| P6-DQ-011 | **Automatic profiling-based test suggestions** | P2 | M |

**P6-DQ-009 closes an important loop.** A quality badge on a dataset in the catalog — and
on the dashboards built from it — means consumers see data problems before they act on the
numbers. This is the cheap 80% of what a data observability product sells.

## 6. Destination nodes

| ID | Node | Pri | Size |
|---|---|---|---|
| P6-DST-001 | Write to a database table (append, replace, merge/upsert) | P0 | L |
| P6-DST-002 | Materialize to Parquet on object storage | P0 | M |
| P6-DST-003 | Create or refresh a Pivot semantic model | P0 | M |
| P6-DST-004 | Export to file (CSV, Excel, JSON) with delivery | P0 | M |
| P6-DST-005 | **Reverse ETL: Salesforce, HubSpot, Braze, Customer.io** | P1 | XL |
| P6-DST-006 | Webhook / HTTP POST | P0 | S |
| P6-DST-007 | Slack / email notification with results | P0 | S |
| P6-DST-008 | Google Sheets write | P1 | M |
| P6-DST-009 | Trigger another flow | P1 | S |
| P6-DST-010 | Refresh an acceleration / cache warm | P0 | S |

**P6-DST-005 is strategically important and scope-dangerous.** Reverse ETL — pushing
computed segments back into operational tools — is what makes analytics actionable, and
it's a whole product category (Census, Hightouch). We ship four high-value destinations
with proper upsert semantics, rate limiting, and error handling. Not a general-purpose
reverse-ETL platform, and the docs say so.

## 7. Operations

| ID | Feature | Pri | Size |
|---|---|---|---|
| P6-OPS-001 | Run history with per-node timing and row counts | P0 | M |
| P6-OPS-002 | Live run view with streaming logs | P0 | L |
| P6-OPS-003 | Failure notifications with the failing node and error | P0 | M |
| P6-OPS-004 | Flow-level SLA definition and breach alerting | P1 | M |
| P6-OPS-005 | Cost tracking per flow run | P1 | M |
| P6-OPS-006 | Dependency view across flows | P1 | M |
| P6-OPS-007 | Manual run with parameter overrides | P0 | S |
| P6-OPS-008 | Pause, resume, and disable | P0 | S |
| P6-OPS-009 | Flow-level permissions and secrets scoping | P0 | M |
| P6-OPS-010 | Run artifacts retention policy | P1 | S |

---

## Technical notes

### Execution strategy: pushdown first
A transform that can run as SQL in the source database should. Extracting 100M rows to
DuckDB to filter them is wasteful when the warehouse can filter them in place. The planner
pushes down as far as possible and falls back to local DuckDB execution only when the
transform crosses sources, uses Python, or exceeds what the dialect supports. The UI shows
where each node executed, because a silently-local join on 100M rows is a mystery
performance problem.

### Memory discipline
Flows process datasets larger than memory as a matter of course. Every node is written
against a streaming Arrow interface with spill-to-disk in DuckDB. Nodes that inherently
require full materialization (sort, some window functions) declare it, and the planner
budgets for them. A flow that OOMs the server takes down dashboards for everyone, so node
execution runs with hard memory caps and a flow is failed rather than allowed to exhaust
the host.

### Flows and the semantic layer are the same graph
A flow that materializes a table which a semantic model reads is one lineage chain, not
two. Flow outputs register in the catalog, appear in impact analysis, and trigger
downstream cache invalidation. This integration is the entire argument for building Flows
inside Pivot rather than telling users to run Airflow.

---

## Explicitly deferred

| Deferred | To | Why |
|---|---|---|
| True streaming (sub-minute latency) | Phase 9 | Micro-batch covers most BI needs |
| dbt Core execution inside Pivot | Post-v2.0 | We read dbt; running it is a different product |
| ML model training nodes | — | Out of scope per [the vision](../vision.md#9-what-we-are-deliberately-not-building) |
| Full Airbyte connector parity | Phase 10 | The bridge covers the long tail |
| Data contracts | Post-v2.0 | Tests cover the practical need |

---

## Risks

| Risk | Mitigation |
|---|---|
| Python sandbox escape — the highest-severity risk in the product | Dedicated threat model, external security review, gVisor + container isolation, disabled by default, no network by default |
| Scope: Flows is three products (ELT, orchestration, reverse-ETL) | Ruthless P0 discipline. The exit criterion is one end-to-end flow, not category parity |
| Flow execution destabilizes the BI workload | Workers are a separate deployment with their own resource pool; a flow can never starve interactive queries |
| Live preview (P6-BLD-003) proves impractical on large data | Always sampled with a documented sample size; explicit "preview is sampled" labeling |
| v1.0 GA pressure causes quality shortcuts | GA gate is the 10-production-companies criterion, not the calendar |
| Reverse ETL error semantics (partial failures, API limits) are a support burden | Four destinations only, each with a tested failure model and a documented retry contract |
