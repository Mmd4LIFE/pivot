# Architecture Decision Records

Decisions that are hard to reverse and that someone will ask about later.

| # | Decision | Status | Date |
|---|---|---|---|
| [0001](0001-backend-language.md) | Go for the control plane | Accepted | 2026-09-19 |
| [0002](0002-frontend-stack.md) | React SPA, no SSR framework | Accepted | 2026-09-19 |
| [0003](0003-metadata-database.md) | Postgres primary, SQLite for quickstart | Accepted | 2026-09-19 |
| [0004](0004-query-engine.md) | DuckDB + Arrow for analytical compute | Accepted | 2026-09-19 |
| [0005](0005-semantic-layer.md) | Build our own semantic layer, dbt-compatible | Accepted | 2026-09-19 |
| [0006](0006-caching-strategy.md) | Three-tier cache with generation-based invalidation | Accepted | 2026-09-19 |
| [0007](0007-job-orchestration.md) | River for jobs; Temporal optional at scale | Accepted | 2026-09-19 |
| [0008](0008-ai-architecture.md) | Separate Python AI service; NL → semantic query | Accepted | 2026-09-19 |
| [0009](0009-authorization.md) | OpenFGA, enforced at the query compiler | Accepted | 2026-09-19 |

## Conventions

- **Numbers are permanent** and never reused, even if an ADR is withdrawn.
- **ADRs are immutable once accepted.** A changed decision gets a new ADR that supersedes
  the old one; the old one is marked `Superseded by ADR-NNNN` and otherwise left alone.
- **Status values:** `Proposed`, `Accepted`, `Superseded by ADR-NNNN`, `Deprecated`.
- Every ADR must state its **negative consequences**. A decision with no downsides was not
  a decision.
- Every ADR must state **when to revisit it**, in observable terms.

Template: [0000-template.md](0000-template.md)
