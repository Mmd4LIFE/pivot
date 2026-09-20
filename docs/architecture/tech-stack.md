# Technology Stack

**Status:** Proposed · **Last reviewed:** 2026-09-19

This document records every significant technology choice for Pivot, the reasoning behind
it, what we rejected, and the conditions under which we would revisit it. Decisions that
are expensive to reverse also have an [ADR](adr/).

---

## Summary

| Layer | Choice | Version |
|---|---|---|
| Control plane language | **Go** | 1.26+ |
| Analytical compute | **DuckDB** (embedded) + **Apache Arrow** | 1.1+ / 17+ |
| AI service language | **Python** | 3.12 |
| SQL parsing & transpilation | **SQLGlot** (Python sidecar) | latest |
| Frontend framework | **React + TypeScript** | 19 / 5.6+ |
| Build tool | **Vite** | 6+ |
| Charting | **Apache ECharts** + **D3** + **deck.gl** | 5.5+ |
| Data grid | **TanStack Table** + custom virtualizer | v8 |
| SQL editor | **CodeMirror 6** | 6+ |
| Flow canvas | **React Flow (XYFlow)** | 12+ |
| Styling / UI | **Tailwind CSS** + **Radix UI** primitives | 4 / latest |
| Metadata database | **PostgreSQL** (SQLite for quickstart) | 16+ / 3.45+ |
| Cache & pub-sub | **Valkey** (Redis-compatible) | 8+ |
| Object storage | **S3-compatible** (MinIO self-hosted) | — |
| Authorization | **OpenFGA** | 1.8+ |
| Job queue | **River** (Postgres-backed) | latest |
| Workflow engine (enterprise) | **Temporal** | optional |
| Vector search | **pgvector** | 0.8+ |
| Observability | **OpenTelemetry** + Prometheus | — |
| Deployment | Single binary · Docker · Helm | — |

---

## 1. Backend: Go

**Decision: Go for the control plane** (floor 1.26+, see the amendment in ADR-0001). See [ADR-0001](adr/0001-backend-language.md).

### Why

**Single-binary distribution is a product requirement, not a preference.** Metabase's
single most effective growth mechanism is `java -jar metabase.jar`. Our
[vision commitment #1](../vision.md#7-non-negotiable-product-commitments) is 30 seconds to
first query. Go compiles to one static binary with no runtime, and `embed.FS` lets us
bake the entire React frontend, migrations, and default content into it. `./pivot` and
you have a BI platform. Nothing else on the shortlist does this as cleanly.

**The workload is I/O concurrency, not CPU.** A BI control plane spends its life waiting:
on warehouse queries, on HTTP, on the metadata DB. Goroutines make thousands of
concurrently in-flight queries cheap and, more importantly, make cancellation correct.
`context.Context` propagating a user's browser-tab-close through to `KILL QUERY` on
Snowflake is the kind of thing that's an afterthought elsewhere and idiomatic here.

**Operational characteristics matter for self-hosted software.** We don't operate most
Pivot installations — our users do, often without a dedicated platform team. Go gives
predictable memory, fast startup (~100ms vs. JVM's 10–30s), and a small footprint. A tool
that idles at 150MB gets deployed on the spare VM. One that idles at 2GB gets a
procurement conversation.

**Ecosystem fit.** `database/sql` + mature drivers for Postgres (pgx), MySQL, ClickHouse,
Snowflake, BigQuery, and Databricks. First-class gRPC, OpenTelemetry, and Kubernetes
tooling. `sqlc` for type-safe queries. Cross-compilation for every platform from one CI
machine.

### What we rejected

**Rust.** Genuinely better for the compute layer and meaningfully faster. Rejected for the
*control plane* because the control plane is mostly CRUD, HTTP handlers, and integration
glue — the domain where Rust's borrow checker costs the most and buys the least. Team
velocity on a product this broad matters more than a 3× improvement on code paths that
are 5% of wall-clock time. **We do use Rust indirectly**: DuckDB's vectorized engine is
C++, and Arrow's Rust implementation backs several dependencies. If we ever need a
distributed query engine, it will be Rust/DataFusion, as a separate service.

**JVM (Java/Kotlin/Clojure).** The strongest rejected candidate. JDBC gives the broadest
database driver coverage that exists — every warehouse ships a JDBC driver, and we'll
have to write connectors by hand that JVM would get free. Apache Calcite is a
production-grade SQL parser, optimizer, and dialect translator that would save months on
the semantic layer. Metabase (Clojure) and Trino (Java) prove it works.

Rejected on distribution and operations: the JVM needs a runtime, has a slow cold start
that makes serverless and autoscaling awkward, and has a memory profile that complicates
the small-deployment story. *This is the closest call in this document.* If our connector
coverage stalls, the mitigation is a JVM-based connector sidecar for long-tail databases —
not rewriting the control plane.

**Python.** Superset's choice, and the reason Superset needs Celery, Redis, Gunicorn, and
a deployment guide. The GIL makes concurrent query orchestration painful, packaging is
not a single artifact, and performance on result-set processing is poor. We use Python
where it's unambiguously the best tool — AI and SQL analysis — and isolate it in a service
we can scale or disable independently.

**TypeScript / Node.** Appealing for one language across the stack. Rejected: weak for
CPU-bound columnar processing, worse database driver maturity for warehouses, and
`node_modules` in production undermines the single-artifact story. Single-language is a
real benefit, but not worth the runtime characteristics.

### Revisit if

Connector coverage becomes the binding constraint on adoption, or we need Calcite-class
query optimization we can't replicate.

---

## 2. Analytical compute: DuckDB + Apache Arrow

**Decision: DuckDB embedded in the Go process, Arrow as the universal internal format.**
See [ADR-0004](adr/0004-query-engine.md).

### The problem it solves

Metabase and Superset push every query to the source database and stream rows back. This
is simple and has three consequences: every dashboard refresh bills the warehouse, you
cannot join across two different databases, and result post-processing happens in
application memory row-by-row.

Pivot needs a local analytical engine for four jobs:

1. **Acceleration.** Materialize a model to Parquet on object storage; serve a thousand
   dashboard loads from it without touching Snowflake. This is the single largest
   warehouse-cost lever we have, and it's a headline feature.
2. **Federation.** Join a Postgres table against a CSV against a BigQuery extract. DuckDB
   reads Parquet, CSV, JSON, Iceberg, and Delta natively, and has Postgres/MySQL scanner
   extensions.
3. **Last-mile transformation.** Pivot/unpivot, window functions, and blending applied to
   a result set without a round trip.
4. **Flows.** The transformation engine for pipeline nodes, running locally on extracted
   data.

### Why DuckDB

Embeddable (in-process, no server to operate — which protects the single-binary story),
vectorized and genuinely fast, excellent Parquet support, Arrow-native zero-copy
interchange, and a SQL dialect close enough to Postgres that our compiler doesn't need a
special case. Go bindings via `go-duckdb` are maintained and CGo-based.

**Apache Arrow is the internal data contract.** Every result set — from a connector, from
DuckDB, to the cache, to the API, to the Python AI service — is Arrow. Columnar, zero-copy,
and language-neutral, which means the Go control plane and the Python AI service share
memory representation without serialization cost. Arrow Flight SQL is the wire protocol
for large transfers and the interface we expose for BI-tool interop.

### What we rejected

**Apache DataFusion (Rust).** Excellent, more extensible, better for a future distributed
engine. Rejected for v1 because embedding it in Go means FFI complexity comparable to
DuckDB's CGo with less SQL maturity and fewer format readers. Revisit for a distributed
compute tier.

**ClickHouse as embedded engine (chDB).** Faster for some scans; much heavier to embed and
a dialect further from Postgres. We may *support ClickHouse as a source and as an optional
acceleration target* — but not as the embedded default.

**Application-layer processing.** What Metabase does. Rejected: it's how you end up with
a 30-second p99 on a 2M-row export.

### Known risks

CGo complicates cross-compilation and raises the floor on build complexity. Mitigation:
build matrix in CI per platform, and a `nocgo` build tag that disables acceleration and
federation for environments that need a pure-Go binary. DuckDB's memory management under
concurrent heavy queries needs hard limits and per-query memory caps from day one — see
[NFRs](../roadmap/non-functional-requirements.md).

---

## 3. AI service: Python

**Decision: a separate Python 3.12 FastAPI service.** See [ADR-0008](adr/0008-ai-architecture.md).

### Why separate, and why Python

The AI ecosystem is Python and will remain Python. Model SDKs, embedding libraries,
forecasting (Prophet, statsmodels), anomaly detection, and evaluation tooling are all
Python-first. Reimplementing in Go means perpetually trailing.

**Separate, not embedded**, for four reasons: it scales independently (AI load is spiky
and GPU-adjacent); it can be disabled entirely for air-gapped or AI-skeptical deployments
without touching the core; it isolates the blast radius of a heavyweight dependency tree;
and it lets us ship core updates without revalidating model behavior.

The single-binary story survives because AI is **opt-in**. `./pivot` gives you a full BI
platform. AI features light up when the service is reachable, and degrade to disabled with
a clear message when it isn't.

### What it owns

- NL→SQL generation grounded in the semantic layer
- Embedding generation and semantic search over the catalog (stored in pgvector)
- Auto-insight narratives and chart recommendations
- The agentic analyst loop (multi-step reasoning, tool use)
- Forecasting and anomaly detection for alerts
- **SQL parsing, transpilation, and lineage extraction via SQLGlot**

That last item is the pragmatic compromise worth naming. **SQLGlot** is the best
multi-dialect SQL parser and transpiler that exists — 25+ dialects, a real optimizer, and
column-level lineage. There is no Go equivalent. Rather than write one, we expose SQLGlot
over gRPC from this service for dialect translation, query validation, and lineage. The
hot path (compiling semantic queries) uses a Go-native builder that emits dialect SQL
directly; SQLGlot handles the harder, less latency-sensitive analysis.

**Consequence to accept:** SQL lineage and dialect translation for arbitrary user SQL
degrade when the AI service is down. They must fail soft, never block a query.

### Model strategy

Provider-agnostic routing with a default to Anthropic's Claude family. Tasks are routed by
cost and capability: **Claude Opus 5** for the agentic analyst and complex multi-step
reasoning, **Claude Sonnet 5** for standard NL→SQL and narrative generation, **Claude
Haiku 4.5** for classification, autocomplete, and high-volume cheap calls. Bring-your-own
key for OpenAI, Google, AWS Bedrock, and Azure. Local models via Ollama/vLLM for air-gapped
deployments — a hard requirement for our self-hosted commitment, and the reason the
prompting layer must not depend on provider-specific features.

---

## 4. Frontend: React 19 + TypeScript + Vite

**Decision: React 19, TypeScript 5.6+, Vite 6, shipped as a static SPA.**
See [ADR-0002](adr/0002-frontend-stack.md).

### Why React

Not because it's the best framework in the abstract — because a BI tool needs a *deep*
ecosystem of exactly the components that are hard to build: virtualized data grids, chart
libraries with React bindings, drag-and-drop layout engines, node-based canvases, and code
editors. React has the most mature version of each. It's also the framework our embedding
customers already use, which matters directly for the SDK.

### Why an SPA, not Next.js

Pivot is an authenticated application, not a content site. SEO is irrelevant; SSR buys us
nothing and costs us a Node process in production — which breaks the single-binary
distribution that is a core product commitment. Vite builds to static assets, we `embed.FS`
them into the Go binary, and the Go server serves them. No Node at runtime, anywhere.

### The component stack

| Concern | Choice | Why |
|---|---|---|
| **Charts** | **Apache ECharts** | The broadest chart-type coverage of any OSS library, Canvas rendering that handles 100k+ points, built-in zoom/brush/tooltip, and strong accessibility. Chart *breadth* is a BI product requirement and ECharts starts us 40 chart types ahead. |
| **Custom viz** | **D3** | For bespoke visualizations ECharts doesn't cover (Sankey variants, custom network layouts) and for the custom-viz plugin API. |
| **Geospatial** | **deck.gl** + MapLibre | WebGL-accelerated maps that render millions of points. Serious geo is a competitive gap in Metabase. |
| **Data grid** | **TanStack Table v8** + custom virtualization | Headless, so we control rendering for pivot tables with frozen headers, subtotals, and expandable groups. Rejected AG Grid: the features we need are enterprise-licensed, which conflicts with Apache 2.0. |
| **SQL editor** | **CodeMirror 6** | Half of Monaco's bundle size, better mobile and accessibility, excellent SQL grammar, and a clean extension API for schema-aware autocomplete. Monaco's IntelliSense edge doesn't justify shipping VS Code. |
| **Flow canvas** | **React Flow (XYFlow)** | The mature choice for node-based editors. Powers the Flows pipeline builder and the semantic layer join graph. |
| **Dashboard layout** | **dnd-kit** + custom grid engine | `react-grid-layout` is effectively unmaintained. dnd-kit is accessible and modern; the responsive grid is ours. |
| **Styling** | **Tailwind CSS 4** | Fast iteration, no CSS-in-JS runtime cost, trivially themeable via CSS custom properties — which is what white-label embedding needs. |
| **Components** | **Radix UI** primitives | Unstyled, accessible, keyboard-correct. We build our design system on top rather than fighting someone else's opinions. |
| **Server state** | **TanStack Query** | Caching, deduplication, background refetch, and optimistic updates. The right model for a query-heavy app. |
| **Client state** | **Zustand** | Minimal, no boilerplate. Redux is unnecessary when server state lives in TanStack Query. |
| **Forms** | **React Hook Form** + **Zod** | Zod schemas are shared with API validation and generated from OpenAPI. |
| **Tables/routing** | **TanStack Router** | Type-safe routes and search params. Dashboard filter state lives in the URL — that's how sharing works — and type-safe search params make it not awful. |

### Performance commitments

A BI frontend dies by a thousand re-renders. Non-negotiable from Phase 0: every data grid
and chart list is virtualized; dashboard cards load independently and stream; the initial
bundle is code-split per surface with a hard budget (see
[NFRs](../roadmap/non-functional-requirements.md)); and Web Workers handle Arrow
deserialization and client-side aggregation so the main thread stays free.

---

## 5. Databases

### 5.1 Metadata: PostgreSQL 16+

Everything Pivot knows about itself — users, connections, semantic models, dashboards,
alerts, flows, audit logs — lives in Postgres.

**Why:** JSONB for the schema-flexible parts (chart configs, flow definitions) alongside
relational integrity for the parts that need it. `pgvector` for semantic search without a
second datastore. `pg_cron`, logical replication, and a mature HA story. Every one of our
users can already operate it.

**SQLite for quickstart.** `./pivot` with no configuration uses an embedded SQLite file.
This is what makes the 30-second promise real. The constraint this imposes: **all schema
and queries must be portable across both.** We use `sqlc` with a dialect-checked query
set and run the full test suite against both engines in CI. A documented `pivot migrate`
path upgrades SQLite → Postgres when a user outgrows it. Anything genuinely
Postgres-specific (vector search, advanced audit) degrades gracefully on SQLite rather
than breaking it.

### 5.2 Cache and message bus: Valkey

Query result cache metadata, session store, rate limiting, distributed locks, WebSocket
fan-out across nodes, and pub/sub for live dashboard updates.

**Valkey over Redis** because of the 2024 Redis license change — Valkey is the
Linux-Foundation-governed BSD fork, and an Apache 2.0 product should not have a
source-available dependency at its core. Wire-compatible, so Redis works for anyone who
prefers it.

Valkey is **optional in single-node mode** (in-process cache is used instead) and
**required for multi-node**. That boundary is deliberate and enforced at startup.

### 5.3 Result cache storage: S3-compatible object storage

Materialized results, Parquet accelerations, exports, and attachments go to object
storage — S3, GCS, Azure Blob, or MinIO for self-hosted. Postgres stores the metadata and
pointers; the bytes never go in the database. DuckDB reads Parquet directly from object
storage, which is what makes acceleration work without a data-movement step.

### 5.4 What we're not adding

- **No dedicated vector database.** pgvector handles catalog-scale embeddings (tens of
  thousands of objects) comfortably. Adding Pinecone/Qdrant/Weaviate for that workload is
  an operational burden with no payoff.
- **No Elasticsearch.** Postgres full-text search plus pgvector covers catalog search.
- **No ClickHouse for internal telemetry in v1.** Usage analytics go to Postgres with
  partitioning and a retention policy. ClickHouse becomes an option at the scale where
  Postgres genuinely hurts — and by then it's a supported *source* connector anyway.

Each of these is a real capability with a real operational cost. We add them when a
measured limit is hit, not in anticipation.

---

## 6. Authorization: OpenFGA

**Decision: OpenFGA for fine-grained authorization.** See [ADR-0009](adr/0009-authorization.md).

Permissions are where BI tools go to die. Metabase's model is confusing at scale.
Superset's is worse. Looker's is powerful and requires a specialist.

OpenFGA is an open-source implementation of Google's Zanzibar — relationship-based access
control. It answers "can user U perform action A on object O?" over a graph of
relationships, which is exactly the shape of BI permissions:

- A user is a member of a group; the group has `editor` on a collection; collections nest;
  a dashboard inherits from its collection *unless* explicitly overridden.
- A dashboard card queries a model the user can't see — the card must be denied even
  though the dashboard is shared.
- Row-level security applies a filter derived from user attributes.

Expressing that in SQL tables produces a recursive query nobody can debug. Expressing it
as a relationship model is legible and testable.

**Deployment:** embedded as a library in single-node mode; a separate service in
multi-node. Authorization decisions are cached aggressively in Valkey with explicit
invalidation on relationship writes.

**The enforcement rule:** authorization is evaluated in the **query compiler**, not the
API handler. Every query — from the UI, the API, an alert, a flow, or the AI — passes
through one compiler that applies the user's RLS policies and column masks before SQL is
generated. There is no bypass path.

---

## 7. Jobs and orchestration

### 7.1 Background jobs: River

Scheduled dashboard refreshes, alert evaluation, cache warming, schema sync, subscription
delivery, and export generation.

**River** is a Postgres-backed job queue for Go with transactional enqueueing —
jobs commit atomically with the data change that triggered them, which eliminates an
entire class of "the record saved but the job never ran" bugs. No extra infrastructure:
it uses the Postgres we already require.

Rejected: Celery (Python, wrong language, and the reason Superset's deployment is hard),
Sidekiq-style Redis queues (Redis as a durability boundary is a bad idea for jobs that
must not be lost), and a homegrown queue (this is solved).

### 7.2 Flows: River v1, Temporal at scale

Flows (data pipelines) are DAGs with retries, per-node state, partial resumption, and runs
that last hours. River handles this for v1 with an explicit DAG executor on top.

**Temporal** becomes an opt-in backend at enterprise scale, where durable execution,
sophisticated retry policies, and long-running workflow versioning earn their operational
cost. Deliberately not a v1 dependency: Temporal requires its own cluster and database,
which is a heavy ask for a self-hosted install.

The Flows engine is written against an interface with both backends implemented, so this
stays a deployment choice rather than a rewrite.

---

## 8. API layer

| Interface | Technology | Purpose |
|---|---|---|
| **Public API** | REST, OpenAPI 3.1 | Everything the UI does, documented and versioned. The UI is a client, not a privileged caller. |
| **Live updates** | WebSocket (SSE fallback) | Query progress, dashboard refresh, collaborative presence, alert notifications |
| **Bulk data** | Arrow Flight SQL | Large result transfer and third-party BI-tool interop |
| **Internal** | gRPC | Go ↔ Python AI service |
| **Webhooks** | Signed HTTP | Outbound events for alerts and flow completion |

**REST over GraphQL.** GraphQL is a good fit for the metadata graph and a bad fit for the
rest: query results are tabular and large, caching semantics are hard, and it complicates
rate limiting and cost control — which for a BI tool are security features. A well-designed
REST API with field selection and OpenAPI-generated clients covers our needs with less
machinery. Revisit only if embedding partners demand it.

**API versioning is in the path** (`/api/v1/`). Breaking changes get a new version; the
prior version is supported for 12 months. The SDK and the UI pin a version.

---

## 9. Security and identity

| Concern | Choice |
|---|---|
| **Sessions** | Server-side sessions in Valkey/Postgres, `HttpOnly` `Secure` `SameSite=Lax` cookies |
| **SSO** | OIDC and SAML 2.0 native; works with Okta, Entra ID, Google, Keycloak, Auth0 |
| **Provisioning** | SCIM 2.0 for user and group sync |
| **API auth** | Scoped API keys and OAuth 2.0 client credentials for machine access |
| **Embedding** | Signed JWT with embedded user attributes for RLS |
| **Secrets** | Envelope encryption; AES-256-GCM data keys wrapped by a KMS key (AWS KMS, GCP KMS, Vault, or a local master key) |
| **MFA** | TOTP built in; WebAuthn/passkeys in Phase 9 |
| **Transport** | TLS 1.3, HSTS, automatic certs via ACME in managed mode |

**No JWTs for browser sessions.** Stateless tokens can't be revoked, and a BI tool needs
"remove this person's access now" to actually mean now. Server-side sessions, invalidated
instantly. JWTs are used only for embedding, where they're short-lived and scoped.

Full detail: [security-model.md](security-model.md).

---

## 10. Observability

**OpenTelemetry for everything** — traces, metrics, and logs — exported to whatever the
user runs. No vendor lock-in and no proprietary agent.

- **Traces** span the full request: HTTP → authz → semantic compile → cache lookup →
  warehouse execution → serialization. When a dashboard is slow, the trace says which of
  those six things was slow. This is the debugging experience competitors lack.
- **Metrics** in Prometheus format at `/metrics`, with a shipped Grafana dashboard.
- **Logs** structured JSON via `slog`, with trace correlation.
- **Query log** as a first-class product feature, not just ops telemetry: every query with
  its user, source, duration, bytes scanned, cache status, and estimated cost. This powers
  the Query Governance surface in Phase 4 and is how Priya finds the $40k dashboard.

---

## 11. Development and delivery

| Concern | Choice |
|---|---|
| **Repo** | Monorepo. One version, atomic cross-cutting changes, one CI |
| **Go tooling** | `golangci-lint`, `sqlc`, `gotestsum`, `testcontainers-go` |
| **Frontend tooling** | Biome (lint + format, replaces ESLint/Prettier), Vitest, Playwright |
| **Python tooling** | `uv`, `ruff`, `pytest`, `mypy --strict` |
| **API contract** | OpenAPI as the source of truth; TS and Python clients generated |
| **CI/CD** | GitHub Actions; matrix build across platforms; release via GoReleaser |
| **Containers** | Distroless base, multi-arch (amd64 + arm64), SBOM, cosign-signed |
| **Migrations** | `goose`, forward-only, every one tested against Postgres and SQLite |
| **E2E** | Playwright against a real stack in testcontainers |
| **Load testing** | k6, with a scenario suite run per release |

**Testing philosophy:** the connector layer and the semantic compiler get the most rigor.
Connectors are tested against real databases in containers, never mocks — a mocked
Snowflake driver tests nothing. The semantic compiler gets golden-file tests: a semantic
query in, expected SQL per dialect out. Those two suites are the ones that catch the bugs
that would otherwise reach production as wrong numbers.

---

## 12. Deployment topologies

### Single binary (1–50 users)
`./pivot` — embedded SQLite, in-process cache, embedded OpenFGA and DuckDB, no AI. One
process, one file, no dependencies. This is the on-ramp.

### Container image (the common case)
A multi-stage, distroless, non-root image published to GHCR as
`ghcr.io/mmd4life/pivot`, multi-arch for `linux/amd64` and `linux/arm64`, with a
`HEALTHCHECK` on `/healthz`, an SBOM, and a cosign signature. This is how most real
deployments run Pivot, and it is what both the Compose stack and the Helm chart consume.
The image is built on every pull request and published on tags, so a broken `Dockerfile`
never reaches a release.

### Docker Compose (50–500 users)
Pivot + Postgres + Valkey + MinIO + AI service. One `docker compose up`, a reference
configuration we maintain and test. A separate `docker-compose.dev.yml` brings up only the
dependencies, for running the binary locally against real services.

### Kubernetes (500+ users)
Helm chart with separately scalable deployments: API servers (stateless, HPA on request
rate), worker pool (HPA on queue depth), AI service (optional, GPU node affinity), plus
managed Postgres, Valkey, and object storage. Multi-region with read replicas in Phase 9.

### Air-gapped
Every component runs without internet. Local LLM via Ollama or vLLM. Offline license
validation. Bundled container images. A first-class supported configuration, because it's
a segment competitors serve badly.

---

## 13. Open questions

Tracked here until resolved into ADRs.

| # | Question | Decide by |
|---|---|---|
| 1 | Do we ship a JVM connector sidecar for long-tail JDBC sources, or hand-write Go connectors indefinitely? | Phase 1 exit |
| 2 | Is the semantic layer DSL YAML-only, or do we need a typed expression language for complex metrics? | Phase 3 entry |
| 3 | Does the custom-viz plugin API use Web Components or a React-specific contract? | Phase 8 entry |
| 4 | Does acceleration need incremental refresh in v1, or is full-refresh acceptable? | Phase 3 exit |
| 5 | Do we adopt Iceberg as the acceleration storage format instead of raw Parquet? | Phase 6 entry |

---

## 14. Decision log

| ADR | Decision |
|---|---|
| [0001](adr/0001-backend-language.md) | Go for the control plane |
| [0002](adr/0002-frontend-stack.md) | React SPA, no SSR framework |
| [0003](adr/0003-metadata-database.md) | Postgres primary, SQLite for quickstart |
| [0004](adr/0004-query-engine.md) | DuckDB + Arrow for compute |
| [0005](adr/0005-semantic-layer.md) | Build our own; dbt-compatible |
| [0006](adr/0006-caching-strategy.md) | Three-tier cache with semantic invalidation |
| [0007](adr/0007-job-orchestration.md) | River for jobs; Temporal optional at scale |
| [0008](adr/0008-ai-architecture.md) | Separate Python AI service, semantic grounding |
| [0009](adr/0009-authorization.md) | OpenFGA, enforced at the query compiler |
