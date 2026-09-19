# Phase 1 — Connect & Query

**Months:** M2–M4 · **Effort:** 34 ew · **Team:** 4 · **Release:** **v0.1**

---

## Goal

Make Pivot useful for exactly one person: an analyst who can write SQL. Connect to a
database, browse the schema, write a query, see results, save it, share it.

This phase is narrow on purpose. It produces the first genuinely usable artifact and
forces us to solve the hardest infrastructure problem in the product — the query execution
path — before any UI depends on it.

## Exit criteria

- [ ] 8 connectors pass the full conformance suite against real containerized instances
- [ ] A 10M-row result streams to CSV without the server exceeding its memory budget
- [ ] Query cancellation propagates to the source database and is verified per connector
- [ ] p95 for a cached query is under 200ms
- [ ] 5 external testers connect their own database and save a question unassisted
- [ ] A malicious query cannot escape its connection's credentials or resource limits

---

## 1. Connector framework

| ID | Feature | Pri | Size |
|---|---|---|---|
| P1-CONN-001 | Connector interface: connect, introspect, execute, cancel, capabilities | P0 | M |
| P1-CONN-002 | Connection pooling with per-connection limits | P0 | M |
| P1-CONN-003 | Credential storage via envelope encryption | P0 | S |
| P1-CONN-004 | Connection test with actionable, specific error messages | P0 | S |
| P1-CONN-005 | SSH tunnel support | P1 | M |
| P1-CONN-006 | TLS/mTLS client certificate configuration | P1 | S |
| P1-CONN-007 | Capability declaration (window functions, CTEs, dialect quirks) | P0 | S |
| P1-CONN-008 | Connector conformance test suite | P0 | L |
| P1-CONN-009 | Health monitoring with circuit breaker | P1 | M |
| P1-CONN-010 | Per-connection resource governance (max rows, timeout, concurrency) | P0 | M |

**The conformance suite (P1-CONN-008) is the highest-leverage item in this phase.** It's a
single test suite every connector must pass: type mapping across all source types, NULL
handling, timezone and timestamp semantics, large result streaming, cancellation
propagation, error classification, and unicode. Write it once and every subsequent
connector is a week instead of a month — and the long tail of "dates are off by one in
Redshift" bugs never happens.

## 2. Connectors (v0.1 set)

| ID | Connector | Pri | Size | Notes |
|---|---|---|---|---|
| P1-DB-001 | PostgreSQL | P0 | S | Reference implementation; `pgx` |
| P1-DB-002 | MySQL / MariaDB | P0 | S | |
| P1-DB-003 | SQLite / DuckDB file | P0 | XS | Also enables the demo dataset |
| P1-DB-004 | Snowflake | P0 | M | Key-pair auth, warehouse selection |
| P1-DB-005 | BigQuery | P0 | M | Service account + workload identity; byte-scan estimation |
| P1-DB-006 | Redshift | P1 | S | Postgres wire protocol, different dialect |
| P1-DB-007 | ClickHouse | P1 | M | Native protocol |
| P1-DB-008 | Databricks / Spark SQL | P1 | M | |
| P1-DB-009 | CSV / Parquet / JSON file upload | P0 | M | Via DuckDB |
| P1-DB-010 | Google Sheets | P2 | M | Highest-demand non-database source |

Later phases add SQL Server, Oracle, Trino, Athena, MongoDB, Elasticsearch, Pinot, Druid,
and the SaaS connectors. Priority is by market share, and the v0.1 set is deliberately the
minimum that covers most warehouses.

## 3. Schema catalog

| ID | Feature | Pri | Size |
|---|---|---|---|
| P1-CAT-001 | Schema introspection: databases, schemas, tables, columns, types | P0 | M |
| P1-CAT-002 | Scheduled sync with change detection | P0 | M |
| P1-CAT-003 | Type normalization to a canonical Pivot type system | P0 | M |
| P1-CAT-004 | Foreign key and constraint discovery | P1 | S |
| P1-CAT-005 | Column profiling: cardinality, null rate, min/max, samples | P1 | M |
| P1-CAT-006 | Semantic type inference (email, URL, currency, geo, timestamp) | P1 | M |
| P1-CAT-007 | Catalog browser UI with search | P0 | M |
| P1-CAT-008 | Table and column descriptions, editable and synced from source comments | P1 | S |
| P1-CAT-009 | Table preview with sampling | P0 | S |
| P1-CAT-010 | Schema change history and drift detection | P2 | M |

**Profiling (P1-CAT-005) and semantic types (P1-CAT-006) look like polish and are not.**
They power default chart selection in Phase 2, automatic join suggestions in Phase 3, and
— critically — they are the grounding context for AI in Phase 7. Knowing that
`users.status` has four distinct values is the difference between a good NL→SQL result and
a guess. Building this early makes every later phase better.

## 4. Query execution engine

| ID | Feature | Pri | Size |
|---|---|---|---|
| P1-QE-001 | Execution pipeline: parse → authorize → plan → execute → stream | P0 | L |
| P1-QE-002 | Arrow-native result streaming | P0 | M |
| P1-QE-003 | Query cancellation propagated to source | P0 | M |
| P1-QE-004 | Timeouts at query, connection, and org level | P0 | S |
| P1-QE-005 | Row limits with explicit truncation signaling | P0 | S |
| P1-QE-006 | Concurrent query governance (per-user and per-connection queues) | P0 | M |
| P1-QE-007 | Query log: user, SQL, duration, rows, bytes, cache status, error | P0 | M |
| P1-QE-008 | Result cache (L1 in-process, L2 Valkey, L3 object storage) | P0 | L |
| P1-QE-009 | Cache key derivation including user identity for RLS correctness | P0 | M |
| P1-QE-010 | Query queue with priority (interactive > scheduled > export) | P1 | M |
| P1-QE-011 | Partial result streaming to the UI as rows arrive | P1 | M |
| P1-QE-012 | Cost estimation before execution (BigQuery dry run, `EXPLAIN`) | P1 | M |

**P1-QE-009 deserves emphasis.** Cache keys must include the identity of the requesting
user whenever row-level security could apply. Getting this wrong means user A sees user B's
cached rows — a data breach delivered by a performance optimization. The key is derived
from the compiled query *plus* the resolved RLS predicate set, so two users with identical
policies share cache and two with different policies never can. This is designed now, in
Phase 1, even though RLS doesn't ship until Phase 4, because retrofitting identity into a
cache key means invalidating every assumption built on top of it.

## 5. SQL editor

| ID | Feature | Pri | Size |
|---|---|---|---|
| P1-SQL-001 | CodeMirror 6 editor with per-dialect SQL syntax highlighting | P0 | M |
| P1-SQL-002 | Schema-aware autocomplete (tables, columns, functions, aliases) | P0 | L |
| P1-SQL-003 | Execute, cancel, and run-selection | P0 | S |
| P1-SQL-004 | Multi-tab workspace with persisted state | P0 | M |
| P1-SQL-005 | Query formatting | P1 | S |
| P1-SQL-006 | Inline error display mapped to line and column | P0 | M |
| P1-SQL-007 | Query history, searchable, per user | P0 | M |
| P1-SQL-008 | Named parameters with typed inputs (`{{start_date}}`) | P0 | M |
| P1-SQL-009 | Snippets and saved SQL fragments | P2 | S |
| P1-SQL-010 | Side-by-side schema browser with click-to-insert | P0 | S |
| P1-SQL-011 | **AI: natural-language → SQL draft** (assisted, never auto-run) | P1 | M |
| P1-SQL-012 | **AI: explain this query in plain language** | P1 | S |
| P1-SQL-013 | Reference another saved question as a CTE | P1 | M |

**P1-SQL-011 and 012 are the early AI hedge described in the
[roadmap](README.md#why-this-order).** They're scoped to assisted authoring: the AI drafts,
a human reviews and runs. No claim of correctness is made, so no trust is risked. They
ship 15 months before the AI analyst and deliver most of the demo value.

## 6. Results & export

| ID | Feature | Pri | Size |
|---|---|---|---|
| P1-RES-001 | Virtualized result grid handling 1M+ rows client-side | P0 | L |
| P1-RES-002 | Column sort, resize, reorder, show/hide, pin | P0 | M |
| P1-RES-003 | Type-aware cell formatting (numbers, dates, booleans, JSON, links) | P0 | M |
| P1-RES-004 | Export: CSV, TSV, JSON, Excel, Parquet | P0 | M |
| P1-RES-005 | Streaming export for results larger than memory | P0 | M |
| P1-RES-006 | Async export for very large results, with notification | P1 | M |
| P1-RES-007 | Copy cell, row, or selection | P1 | S |
| P1-RES-008 | Summary statistics per column | P2 | S |
| P1-RES-009 | Row detail expansion panel | P2 | S |

## 7. Saved questions

| ID | Feature | Pri | Size |
|---|---|---|---|
| P1-Q-001 | Save a query with name, description, and tags | P0 | S |
| P1-Q-002 | Personal and shared collections (basic; full model in Phase 4) | P0 | M |
| P1-Q-003 | Version history with diff and restore | P1 | M |
| P1-Q-004 | Duplicate and fork | P1 | S |
| P1-Q-005 | Search across saved questions | P0 | M |
| P1-Q-006 | Share via link with permission inheritance | P0 | M |
| P1-Q-007 | Run-on-open with a configurable freshness policy | P0 | S |
| P1-Q-008 | Favorites and recently viewed | P1 | S |

## 8. Admin

| ID | Feature | Pri | Size |
|---|---|---|---|
| P1-ADM-001 | Connection management UI (CRUD, test, permissions) | P0 | M |
| P1-ADM-002 | User and group management | P0 | M |
| P1-ADM-003 | Query monitor: running queries, kill, per-user usage | P0 | M |
| P1-ADM-004 | Instance settings (branding, email, defaults) | P0 | M |
| P1-ADM-005 | SMTP configuration with a test send | P0 | S |
| P1-ADM-006 | Sample dataset and guided tour for first run | P1 | M |

**P1-ADM-006 matters more than its size suggests.** A BI tool with no data is an empty
form. Shipping an embedded sample dataset (DuckDB file, a realistic e-commerce schema)
means a new user sees a working product before they've found their warehouse credentials.
It's also what makes the 30-second promise demonstrable.

---

## Technical notes

### Streaming, end to end
The result path is streaming from source cursor to HTTP response. No stage materializes a
full result set in memory. Arrow record batches flow from the connector through transforms
to the serializer. This is what makes "10M rows to CSV in a 512MB container" possible, and
it constrains every design decision downstream — a transform that needs the whole result
is a design error, not an optimization target.

### The three-tier cache
- **L1** — in-process LRU, Arrow batches, sub-millisecond, seconds-to-minutes TTL
- **L2** — Valkey, serialized Arrow IPC, shared across nodes, minutes-to-hours
- **L3** — object storage, Parquet, for large results and materialization, hours-to-days

Invalidation is by TTL in Phase 1. Semantic invalidation (when the underlying model
changes) arrives with the semantic layer in Phase 3; see
[ADR-0006](../architecture/adr/0006-caching-strategy.md).

### SQL injection and query safety
User SQL runs as written — that's the point of a SQL editor. Safety comes from the
boundary, not from parsing: connection credentials are scoped to least privilege (we
document and warn about over-privileged connections), resource limits are enforced per
connection, results are row-limited, and parameterized inputs are bound as parameters,
never interpolated. Pivot-generated SQL always uses the compiler. The query log records
everything for audit.

---

## Explicitly deferred

| Deferred | To | Why |
|---|---|---|
| Visual query builder | Phase 2 | SQL first; the builder needs the execution engine proven |
| Charts | Phase 2 | v0.1 is a table. Deliberately |
| Cross-database joins | Phase 3 | Needs the federation layer and the semantic layer |
| Semantic invalidation of cache | Phase 3 | No semantic model to invalidate against yet |
| Row-level security | Phase 4 | Cache keys are designed for it now; enforcement comes later |
| Most connectors | Phases 2–6 | Coverage follows demand after the framework is proven |

---

## Risks

| Risk | Mitigation |
|---|---|
| Connector conformance suite is under-scoped and bugs leak per-connector | Treat it as the phase's primary deliverable; a connector without a green suite does not ship |
| Cache key design proves wrong when RLS arrives in Phase 4 | Design the key with RLS in mind now; write the Phase 4 test cases in Phase 1 |
| The virtualized grid can't hit 1M rows smoothly | Spike in week 1. Fallback: server-side pagination above a row threshold |
| Snowflake/BigQuery auth complexity exceeds estimates | These two are the highest-value connectors; allocate a dedicated engineer |
| Query cancellation silently doesn't work on some sources | Explicit conformance test per connector; a source that can't cancel is documented as such and gets a hard timeout |
