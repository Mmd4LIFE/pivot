# ADR-0004: DuckDB embedded, Apache Arrow as the internal format

**Status:** Accepted
**Date:** 2026-09-19

## Context

Metabase and Superset push every query to the source database and stream rows back. Simple,
and it has three consequences: every dashboard refresh bills the warehouse, cross-database
joins are impossible, and result post-processing happens row-by-row in application memory.

Pivot needs local analytical compute for four jobs:

1. **Acceleration** — materialize a model to Parquet, serve a thousand dashboard loads from
   it without touching Snowflake
2. **Federation** — join Postgres against a CSV against a BigQuery extract
3. **Last-mile transforms** — pivot, window functions, blending, without a round trip
4. **Flows** — the transformation engine for pipeline nodes

## Decision

Embed DuckDB in the Go process via `go-duckdb`. Use Apache Arrow as the universal internal
data format, from connector through cache to API response and the Python AI service.

## Rationale

**DuckDB** is embeddable in-process, which protects the single-binary story — no separate
server to operate. It's vectorized and fast, reads Parquet, CSV, JSON, Iceberg, and Delta
natively, has Postgres and MySQL scanner extensions, and its SQL dialect is close enough to
Postgres that our compiler needs few special cases.

**Arrow** is the internal contract because it's columnar, zero-copy, and language-neutral.
The Go control plane and the Python AI service share a memory representation with no
serialization cost. Arrow Flight SQL then gives us a wire protocol for large transfers and
third-party BI-tool interop (P8-API-006) essentially for free.

The strategic payoff is [P2-DPF-003](../../roadmap/phase-2-visualization-dashboards.md):
detecting that twenty dashboard cards share a base query, running it once, and deriving the
rest locally. That turns twenty warehouse queries into one. A tool that only pushes down
cannot do this, and it is the foundation of the cost-governance story.

## Alternatives considered

### Apache DataFusion (Rust)
More extensible, and the right choice for a future *distributed* engine. Rejected for v1:
embedding it in Go means FFI complexity comparable to DuckDB's CGo, with less SQL maturity
and fewer format readers. Revisit when we need distributed compute.

### ClickHouse embedded (chDB)
Faster on some scan-heavy workloads. Rejected: heavier to embed, and a dialect further from
Postgres, which would mean more compiler special cases. ClickHouse remains supported as a
*source* and as an optional acceleration target.

### Application-layer processing
What Metabase does. Rejected — it's how a 2M-row export becomes a 30-second p99.

### A separate compute service
Cleaner isolation and independent scaling. Rejected for v1 because it breaks single-binary
distribution. The DuckDB integration is written behind an interface so this remains possible
later without a rewrite.

## Consequences

**Positive**
- Cross-database joins
- Parquet acceleration: the single largest warehouse-cost lever in the product
- Shared-subquery consolidation across dashboard cards
- Zero-copy interchange with the Python AI service
- Arrow Flight SQL interop comes almost free

**Negative**
- **CGo.** Complicates cross-compilation, raises build complexity, and requires a
  per-platform CI matrix. Mitigated by a `nocgo` build tag that disables acceleration and
  federation for environments needing a pure-Go binary
- **Memory pressure is a real operational risk.** DuckDB under concurrent heavy queries can
  consume the host. Mitigated by hard per-query memory caps enforced from day one
  ([NFR §1.3](../../roadmap/non-functional-requirements.md#13-resource-budgets)) — a query
  over budget is killed, not allowed to run
- Binary size grows by roughly 30MB
- Another SQL dialect to reason about in the compiler

**Neutral**
- Arrow as the internal format means row-oriented conversion happens once, at the edge —
  which constrains every design downstream. A transform needing a materialized result is
  now a design error rather than an optimization target

## Revisit if

- DuckDB memory instability proves unmanageable under production concurrency
- CGo build complexity outweighs the benefit (fall back to `nocgo` as the default)
- We need distributed compute, at which point DataFusion as a separate service is the path
