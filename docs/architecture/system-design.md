# System Design

**Status:** Proposed · **Last reviewed:** 2026-09-19

How the pieces fit together, what happens on a request, and how it's deployed.
Technology choices and their rationale live in [tech-stack.md](tech-stack.md).

---

## 1. Component overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                          CLIENTS                                     │
│   Web SPA  ·  Embedded SDK  ·  REST API  ·  Arrow Flight  ·  Slack   │
└──────────────────────────────┬──────────────────────────────────────┘
                               │ HTTPS / WSS
┌──────────────────────────────▼──────────────────────────────────────┐
│                     PIVOT CONTROL PLANE (Go)                         │
│                                                                      │
│  ┌─────────────┐  ┌──────────────┐  ┌────────────┐  ┌────────────┐  │
│  │  API Layer  │  │  AuthN/AuthZ │  │  Metadata  │  │  Realtime  │  │
│  │ REST · WS   │  │   OpenFGA    │  │   Service  │  │  WS fanout │  │
│  └──────┬──────┘  └───────┬──────┘  └─────┬──────┘  └─────┬──────┘  │
│         │                 │                │               │         │
│  ┌──────▼─────────────────▼────────────────▼───────────────▼──────┐  │
│  │                    SEMANTIC QUERY COMPILER                     │  │
│  │   parse → resolve joins → inject RLS → mask columns → emit SQL │  │
│  │              ◄── THE ONLY PATH TO DATA ──►                     │  │
│  └──────┬─────────────────────────────────────────────────┬───────┘  │
│         │                                                 │          │
│  ┌──────▼──────┐  ┌────────────┐  ┌──────────┐  ┌─────────▼──────┐  │
│  │  Execution  │  │   Cache    │  │  DuckDB  │  │   Connectors   │  │
│  │   Engine    │◄─┤ L1·L2·L3   │  │ federate │  │  40+ drivers   │  │
│  └──────┬──────┘  └────────────┘  │ accel.   │  └────────┬───────┘  │
│         │                         └──────────┘           │          │
│  ┌──────▼──────┐  ┌────────────┐  ┌──────────┐  ┌────────▼───────┐  │
│  │  Scheduler  │  │   Alerts   │  │  Flows   │  │    Render      │  │
│  │   (River)   │  │   engine   │  │  engine  │  │  (Chromium)    │  │
│  └─────────────┘  └────────────┘  └──────────┘  └────────────────┘  │
└────────┬──────────────────────────────────────────────┬─────────────┘
         │ gRPC (optional)                              │
┌────────▼──────────────┐                    ┌──────────▼─────────────┐
│   AI SERVICE (Python) │                    │    DATA SOURCES        │
│  NL→semantic query    │                    │  Snowflake · BigQuery  │
│  Agentic analyst      │                    │  Postgres · Databricks │
│  Embeddings · SQLGlot │                    │  S3 · APIs · Files     │
│  Forecast · Anomaly   │                    └────────────────────────┘
└───────────────────────┘
         ┌──────────────────────────────────────────────┐
         │  STATE: Postgres · Valkey · Object Storage   │
         └──────────────────────────────────────────────┘
```

### Responsibilities

| Component | Owns |
|---|---|
| **API Layer** | HTTP/WS termination, validation, rate limiting, serialization |
| **AuthN/AuthZ** | Identity, sessions, OpenFGA relationship checks, decision cache |
| **Metadata Service** | CRUD for all Pivot entities, tenant scoping, change events |
| **Semantic Compiler** | Semantic query → dialect SQL, with RLS and masking applied |
| **Execution Engine** | Query lifecycle: plan, route, execute, stream, cancel, log |
| **Cache** | Three-tier result caching with generation-based invalidation |
| **DuckDB** | Federation, acceleration, last-mile transforms, flow compute |
| **Connectors** | Source drivers, introspection, dialect capabilities |
| **Scheduler** | Durable jobs, cron, leader election, retries |
| **Alerts / Flows** | Evaluation and pipeline execution on the scheduler |
| **Render** | Headless chart and dashboard rendering for PDF and email |
| **AI Service** | All model interaction, embeddings, SQL analysis via SQLGlot |

---

## 2. The query path

The single most important flow in the system. Every data request follows it.

```
1. REQUEST        Client sends a semantic query or saved question ID
                        ↓
2. AUTHENTICATE   Session or token → user identity + org + attributes
                        ↓
3. AUTHORIZE      OpenFGA: can this user read this question / model?
                        ↓  denied → 403, audited
4. COMPILE        Semantic compiler:
                    a. resolve models, metrics, dimensions
                    b. find join path; apply symmetric aggregates
                    c. INJECT RLS PREDICATES for this user
                    d. APPLY COLUMN MASKS for this user
                    e. emit dialect-specific SQL
                        ↓
5. CACHE KEY      hash(compiled SQL + resolved policy set + freshness)
                        ↓
6. CACHE LOOKUP   L1 in-process → L2 Valkey → L3 object storage
                        ↓  hit → stream cached Arrow → step 10
7. PLAN           Single source? push down.
                  Multi-source or accelerated? route through DuckDB.
                        ↓
8. EXECUTE        Connector executes; Arrow batches stream back.
                  Context cancellation propagates to the source.
                        ↓
9. CACHE WRITE    Write through all tiers per policy
                        ↓
10. LOG           Query log: user, SQL, duration, rows, bytes, cache, cost
                        ↓
11. RESPOND       Stream Arrow (or JSON) to the client
```

**Steps 4c and 4d are the security boundary.** They happen inside the compiler, which is
the only component that emits SQL. There is no code path from an API handler to a data
source that skips it — not for alerts, not for flows, not for exports, not for AI. This is
[architectural rule #1](../roadmap/00-principles.md#3-architectural-rules).

**Step 5 is where a mistake becomes a breach.** The cache key includes a hash of the
*resolved policy set*, not the user ID. Users under identical policies share cache entries;
users under different policies cannot collide. Keying by user alone would destroy the hit
rate; keying by query alone would leak data.

---

## 3. The AI query path

Deliberately different from the SQL path, and this difference is the phase's core bet.

```
1. QUESTION       "What was revenue in EMEA last quarter?"
                        ↓
2. RETRIEVE       Search semantic layer for relevant models and metrics
                  ►► FILTERED BY THIS USER'S PERMISSIONS ◄◄
                        ↓
3. CONTEXT        Assemble: metric defs, dimensions, indexed values,
                  glossary terms, similar past questions
                        ↓
4. GENERATE       LLM emits a STRUCTURED SEMANTIC QUERY
                  { metric: "revenue", dimensions: ["region"],
                    filters: [region=EMEA, period=last_quarter] }
                        ↓
5. VALIDATE       Does every referenced object exist?
                  Is the user permitted to use each one?
                        ↓  invalid → refuse with an explanation
6. COMPILE        ►► SAME COMPILER AS STEP 4 ABOVE ◄◄
                  RLS and masking applied identically
                        ↓
7. EXECUTE        Same execution path, same cache, same query log
                        ↓
8. PRESENT        Result + generated SQL + models used + confidence
```

**The model never produces SQL.** It selects from a constrained set of governed objects.
Three consequences follow:

- **Security:** a compromised or prompt-injected model still cannot exceed the user's
  permissions, because the compiler applies them regardless of what the model emitted.
- **Correctness:** the AI cannot invent a revenue definition; it can only use the one the
  business defined. The answer matches the dashboard by construction.
- **Evaluability:** structured output over a known object set is far easier to benchmark
  than free-form SQL.

Free-form SQL generation exists as an explicitly-labeled, separately-permissioned fallback.

---

## 4. Multi-tenancy

Isolation is enforced at four layers, each independently sufficient for its scope.

| Layer | Mechanism |
|---|---|
| **Metadata** | Repository layer injects tenant scope into every query. No method can return cross-tenant rows |
| **Authorization** | OpenFGA relationships are tenant-rooted; no relationship crosses tenants |
| **Query** | Connections belong to a tenant; the compiler resolves only that tenant's models |
| **Cache** | Tenant ID is a mandatory component of every cache key |

The Phase 8 and 9 test suites include explicit cross-tenant attack scenarios against each
layer. Defense in depth here is warranted: a cross-tenant leak in an embedded deployment is
an existential incident.

---

## 5. Caching architecture

```
Query ──► L1: in-process LRU        ──► hit: <1ms
          Arrow batches, TTL seconds–minutes
             │ miss
             ▼
          L2: Valkey                ──► hit: ~5ms
          Arrow IPC, shared across nodes, TTL minutes–hours
             │ miss
             ▼
          L3: Object storage        ──► hit: ~50ms
          Parquet, large results and materializations, hours–days
             │ miss
             ▼
          Source database / DuckDB
```

### Invalidation

Three triggers, all generation-based rather than key-enumerating:

| Trigger | Effect |
|---|---|
| **TTL expiry** | Per-policy time bound |
| **Semantic change** | A model or metric definition changes → bump that model's generation counter; all keys derived from it become unreachable |
| **Source freshness** | A watermark check (max timestamp, row count, or source metadata) detects new data → bump generation |

Generation-based invalidation matters at scale: a model change that would require deleting
a million cache keys instead requires incrementing one integer. Stale entries expire
naturally. See [ADR-0006](adr/0006-caching-strategy.md).

---

## 6. Deployment topologies

### Single binary — 1 to 50 users

```
┌──────────────────────────────────┐
│  ./pivot  (one process)          │
│  Go API + embedded React         │
│  SQLite metadata                 │
│  In-process cache                │
│  Embedded OpenFGA                │
│  Embedded DuckDB                 │
│  Local filesystem storage        │
│  AI: disabled (or remote)        │
└──────────────────────────────────┘
```

No external dependencies. This is the 30-second install, and it's a real production
configuration for small teams — not just a demo mode.

### Docker Compose — 50 to 500 users

Pivot + Postgres + Valkey + MinIO + AI service, as a maintained and tested reference
configuration.

### Kubernetes — 500+ users

```
     Ingress
        │
   ┌────┴────────────────────────────────┐
   │                                     │
┌──▼──────────┐  ┌──────────────┐  ┌────▼─────────┐
│ API pods    │  │ Worker pods  │  │ Render pods  │
│ stateless   │  │ jobs, alerts │  │ Chromium     │
│ HPA: RPS    │  │ flows        │  │ HPA: queue   │
└──┬──────────┘  │ HPA: depth   │  └──────────────┘
   │             └──────┬───────┘
   │                    │          ┌──────────────┐
   │                    │          │  AI pods     │
   │                    │          │  optional    │
   │                    │          │  GPU affinity│
   └────────┬───────────┴──────────┴──────┬───────┘
            │                             │
   ┌────────▼────────┐  ┌──────────┐  ┌──▼────────┐
   │ Postgres        │  │ Valkey   │  │ OpenFGA   │
   │ primary+replica │  │ cluster  │  │           │
   └─────────────────┘  └──────────┘  └───────────┘
            │
   ┌────────▼────────┐
   │ Object storage  │
   └─────────────────┘
```

**Worker pods are separated from API pods deliberately.** A flow processing 100M rows must
not be able to starve interactive dashboard queries. Different resource profiles, different
scaling signals, different failure domains.

### Air-gapped

Every component above, plus a local LLM (Ollama or vLLM), offline license validation,
bundled images, and zero outbound network. A first-class supported configuration.

---

## 7. Failure domains

| Failure | Blast radius | Behavior |
|---|---|---|
| API pod dies | None | Load balancer routes elsewhere; in-flight requests retry |
| Worker pod dies | Jobs in flight | River redelivers; flows resume from checkpoint |
| Render pod dies | Pending renders | Re-queued; subscription delivery delayed, not lost |
| AI service down | AI features only | Disabled with a notice; core unaffected |
| Valkey down | Performance | Falls back to L1; multi-node coordination degrades |
| Object storage down | Acceleration, exports | Queries route to source; exports queue |
| One source DB down | That connection's content | Other connections unaffected |
| OpenFGA down | **Total** | **Fail closed.** No authz decisions means no access |
| Postgres down | **Total** | The one hard dependency |

Rationale for the two hard dependencies: authorization that fails open is worse than no
authorization, and the metadata database holds the definition of everything. Both are
addressed by HA rather than by graceful degradation.

---

## 8. Data flow: a scheduled subscription

An illustrative end-to-end trace, because it exercises nearly every component.

```
Scheduler fires (leader-elected, jittered)
    ↓
Load subscription: dashboard, recipients, filters
    ↓
Group recipients by RESOLVED POLICY SET   ← not by user
    ↓
For each distinct policy set:
    ├─ Compile each card's query with that policy set
    ├─ Check cache; execute what's missing
    ├─ Render charts (Chromium worker)
    └─ Generate PDF / email HTML
    ↓
For each recipient:
    └─ Deliver the render matching their policy set
       via their preferred channel
    ↓
Record delivery in the transactional outbox
    ↓
Audit log entry per recipient
```

Grouping by policy set rather than by user is what makes this tractable: 200 recipients
under 4 distinct policy sets means 4 renders, not 200. It's also what keeps it correct —
rendering once as the author would leak every restricted row to every recipient.

---

## 9. Related documents

- [tech-stack.md](tech-stack.md) — why each technology was chosen
- [data-model.md](data-model.md) — the metadata schema
- [security-model.md](security-model.md) — authorization and RLS in detail
- [adr/](adr/) — the decisions behind this design
