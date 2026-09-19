# Pivot

**The open business intelligence platform for the AI era.**

Pivot is a self-hostable BI platform that unifies data connectivity, a governed semantic
layer, exploration, visualization, dashboards, alerting, data flows, and an AI analyst
into one product — without forcing you to choose between "easy" and "serious".

> Metabase made BI approachable. Looker made it governed. dbt made it versioned.
> Pivot is all three, plus an AI analyst that actually understands your business.

---

## Status

**Pre-alpha — documentation and design phase.**

No code has been written yet. This repository currently contains the product vision,
architecture decisions, and a complete zero-to-one roadmap. Start here:

| Document | What it covers |
|---|---|
| **[CHECKLIST.md](CHECKLIST.md)** | **The build checklist — what's done, what's next** |
| [docs/vision.md](docs/vision.md) | Why Pivot exists, who it's for, how it wins |
| [docs/architecture/tech-stack.md](docs/architecture/tech-stack.md) | Every technology choice, with rationale and rejected alternatives |
| [docs/architecture/system-design.md](docs/architecture/system-design.md) | Services, data flow, deployment topologies |
| [docs/roadmap/README.md](docs/roadmap/README.md) | The 11-phase, 24-month roadmap |
| [docs/roadmap/feature-matrix.md](docs/roadmap/feature-matrix.md) | The complete feature inventory vs. competitors |

---

## The stack at a glance

| Layer | Choice |
|---|---|
| **Backend** | Go 1.23+ (control plane, API, connectors, scheduler) |
| **Compute** | DuckDB embedded + Apache Arrow (acceleration, federation, cache) |
| **AI service** | Python 3.12 (FastAPI) — NL→SQL, insights, agentic analyst |
| **Frontend** | React 19 + TypeScript + Vite |
| **Charts** | Apache ECharts + D3 + deck.gl (geo) |
| **Metadata DB** | PostgreSQL 16 (SQLite for single-node quickstart) |
| **Cache / bus** | Valkey (Redis-compatible) |
| **Object storage** | S3-compatible (MinIO for self-hosted) |
| **Authorization** | OpenFGA (Zanzibar-style relationship-based access control) |
| **Jobs** | River (Postgres-backed queue); Temporal optional at enterprise scale |
| **Deployment** | Single static binary, Docker, Helm/Kubernetes |

Full rationale, including what we rejected and why:
[docs/architecture/tech-stack.md](docs/architecture/tech-stack.md).

---

## Design principles

1. **One binary to start, a cluster to scale.** `./pivot` should work in 30 seconds with
   zero dependencies. The same codebase runs 10,000 users on Kubernetes.
2. **The semantic layer is not optional.** Metrics are defined once, versioned in Git,
   and consumed identically by charts, alerts, the API, and the AI.
3. **AI is grounded, never guessing.** Every AI answer is anchored to the semantic layer
   and returns the SQL it ran. No hallucinated metrics.
4. **Governance is a feature, not a tax.** Row-level security, lineage, and audit are
   first-class from Phase 0 — not bolted on for the enterprise tier.
5. **Open by default.** Apache 2.0 core. Open API. No feature is locked behind a license
   check unless it exists only to serve enterprise operations.

---

## Repository layout (planned)

```
pivot/
├── docs/                    # ← you are here
├── cmd/pivot/               # Go entrypoint (single binary)
├── internal/                # Go control plane
│   ├── api/                 # REST + WebSocket handlers
│   ├── connectors/          # Warehouse & database drivers
│   ├── semantic/            # Semantic layer compiler
│   ├── query/               # Execution, federation, cache
│   ├── flows/               # Pipeline engine
│   ├── alerts/              # Rules & delivery
│   └── authz/               # OpenFGA integration
├── services/ai/             # Python AI service
├── web/                     # React frontend
├── sdk/                     # Embedding SDKs (TS, Python)
└── deploy/                  # Helm charts, Compose, Terraform
```

---

## License

Apache 2.0 (planned). See [docs/roadmap/phase-10-ecosystem.md](docs/roadmap/phase-10-ecosystem.md)
for the open-core boundary.
