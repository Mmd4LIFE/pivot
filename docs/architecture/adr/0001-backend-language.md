# ADR-0001: Go for the control plane

**Status:** Accepted
**Date:** 2026-09-19

## Context

Pivot's control plane handles HTTP and WebSocket APIs, authentication and authorization,
metadata CRUD, connector orchestration, query lifecycle management, job scheduling, and
the semantic compiler.

Four constraints shape the choice:

1. **Distribution.** [Vision commitment #1](../../vision.md#7-non-negotiable-product-commitments)
   is 30 seconds from download to first query, with no runtime and no dependencies. This is
   a product requirement, not a preference — Metabase's single most effective growth
   mechanism is `java -jar metabase.jar`.
2. **Concurrency.** The workload is I/O-bound: thousands of concurrently in-flight
   warehouse queries, each cancellable.
3. **Operations.** Most Pivot installations are operated by our users, often without a
   dedicated platform team. Predictable memory, fast startup, small footprint.
4. **Velocity.** The roadmap is broad. Development speed across a large surface matters
   more than peak performance on any single path.

## Decision

Go 1.23+ for the control plane, with the frontend embedded via `embed.FS` so a release is
one static binary.

## Rationale

Go is the only mainstream option that satisfies constraint 1 cleanly while being strong on
2, 3, and 4. Goroutines make concurrent query orchestration cheap, and `context.Context`
propagation makes cancellation correct — a user closing a browser tab propagates through to
`KILL QUERY` on Snowflake as an idiomatic pattern rather than an afterthought.

Startup is ~100ms versus 10–30s for the JVM, idle memory is ~150MB rather than ~2GB, and
cross-compilation for six platforms happens on one CI machine.

The ecosystem covers what we need: `pgx`, mature warehouse drivers, `sqlc` for typed
queries, first-class OpenTelemetry and gRPC.

## Alternatives considered

### JVM (Java / Kotlin / Clojure)

**The closest call in this document.** Three real advantages:

- **JDBC gives the broadest database driver coverage that exists.** Every warehouse ships a
  JDBC driver. We will hand-write Go connectors that JVM would get for free.
- **Apache Calcite** is a production-grade SQL parser, optimizer, and dialect translator
  that would save months on the semantic compiler.
- Proven in this exact domain: Metabase (Clojure) and Trino (Java).

Rejected on distribution and operations. The JVM requires a runtime, has a cold start that
makes autoscaling awkward, and has a memory profile that complicates the
small-deployment story that is our on-ramp.

We accept the connector cost explicitly. The mitigation, if coverage becomes the binding
constraint on adoption, is a JVM connector sidecar for long-tail JDBC sources — not
rewriting the control plane. This is tracked as
[open question #1](../tech-stack.md#13-open-questions).

### Rust

Faster, and genuinely better for the compute layer. Rejected for the control plane because
the control plane is mostly CRUD, HTTP handlers, and integration glue — the domain where
the borrow checker costs the most and buys the least. A 3× speedup on code paths
representing 5% of wall-clock time does not justify the velocity cost across a roadmap this
broad.

We use Rust indirectly (Arrow's Rust implementation backs several dependencies), and a
future distributed query engine would be Rust/DataFusion as a separate service.

### Python

Superset's choice, and the reason Superset needs Celery, Redis, Gunicorn, and a deployment
guide. The GIL makes concurrent query orchestration painful, packaging is not a single
artifact, and result-set processing performance is poor.

We use Python where it's unambiguously best — AI and SQL analysis — isolated in a service
that can be scaled or disabled independently ([ADR-0008](0008-ai-architecture.md)).

### TypeScript / Node

One language across the stack is a real benefit. Rejected: weak for CPU-bound columnar
processing, less mature warehouse drivers, and `node_modules` in production undermines the
single-artifact story.

## Consequences

**Positive**
- One static binary; `embed.FS` includes the frontend, migrations, and sample data
- ~100ms startup, ~150MB idle
- Correct, idiomatic cancellation propagation
- Trivial cross-compilation and containerization
- A large hiring pool for infrastructure work

**Negative**
- **Every connector is hand-written.** No JDBC fallback. This is the real cost, and it
  shapes the Phase 1 emphasis on the connector conformance suite
- **No Calcite.** We build the semantic compiler and lean on SQLGlot (Python) for dialect
  transpilation and lineage
- CGo for DuckDB complicates cross-compilation and requires a per-platform build matrix
- More verbose than the alternatives; error handling is explicit everywhere

**Neutral**
- Generics are adequate for our needs but less expressive than Rust's or Kotlin's
- The standard library's opinions shape the codebase, which is mostly good

## Revisit if

- Connector coverage becomes the top-cited reason for lost deals, **and** the JVM sidecar
  mitigation proves insufficient
- We need query optimization sophistication we cannot replicate without Calcite
- CGo build complexity from DuckDB exceeds the value of embedded compute

---

## Amendments

ADRs are immutable once accepted, and the decision above — Go for the control plane — is
unchanged. This section records factual changes to its parameters.

### 2026-09-20 — minimum Go version raised from 1.23 to 1.26

**What changed.** `go.mod` now declares `go 1.26.0`.

**Why.** Part 3-a added the metadata store. `modernc.org/sqlite` requires `go 1.25.0` in
every published version, and `github.com/pressly/goose/v3` v3.28.0 requires `go 1.26.0`.
Neither is optional: modernc is the pure-Go SQLite driver that keeps the Part 13
cross-compilation matrix a single build, and goose is the migration runner.

**What we gave up.** The original rationale for 1.23 was a low contributor barrier. That
cost is real but small: Go 1.26 and 1.27 are both current, and Go's support policy covers
the last two releases, so 1.26 is not an unusual ask in late 2026.

**The alternative we rejected.** Pinning goose to an older release would have bought
1.25 instead of 1.26 — a one-version difference not worth running a dependency behind
upstream. Dropping modernc for a CGo SQLite driver would have cost far more: CGo turns
six-platform cross-compilation into a cross-toolchain problem, which is a much larger
price than a version floor.
