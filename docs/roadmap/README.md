# Pivot Roadmap — Zero to Hero

**Horizon:** 24 months to v2.0 · **Last reviewed:** 2026-09-19

This is the complete build plan for Pivot, from an empty repository to an embedded,
enterprise-grade BI platform. Eleven phases, each with a hard exit criterion.

---

## Planning assumptions

These are the inputs the timeline depends on. If they change, the timeline changes.

| Assumption | Value |
|---|---|
| Team at start | 3 engineers (2 backend, 1 frontend) |
| Team at M6 | 6 engineers (3 backend, 2 frontend, 1 full-stack) |
| Team at M12 | 10 engineers (+1 data/AI, +1 designer, +1 DevRel) |
| Team at M18 | 14 engineers (+2 engineers, +1 PM, +1 support) |
| Working weeks per quarter | 11 (accounting for holidays, interrupts, on-call) |
| Overhead on estimates | 30% (bugs, review, meetings, the unknown) |

**The estimates in the phase documents are engineer-weeks of focused work.** The 30%
overhead is applied at the phase level, not per feature. A phase that sums to 40 ew is
planned as 52 ew of calendar capacity.

---

## The phase map

| Phase | Name | Months | Release | Core question it answers |
|---|---|---|---|---|
| **0** | [Foundations](phase-0-foundations.md) | M0–M2 | — | Can we build, test, ship, and authenticate? |
| **1** | [Connect & Query](phase-1-connect-and-query.md) | M2–M4 | **v0.1** | Can a user reach their data and get an answer? |
| **2** | [Visualize & Dashboard](phase-2-visualization-dashboards.md) | M4–M7 | **v0.3** | Can a non-engineer build something they'd show a room? |
| **3** | [Semantic Layer](phase-3-semantic-layer.md) | M7–M9 | **v0.5** | Is there one definition of every number? |
| **4** | [Collaborate & Govern](phase-4-collaboration-governance.md) | M9–M11 | **v0.7** | Can a company trust it with real permissions? |
| **5** | [Alert & Distribute](phase-5-alerting-distribution.md) | M11–M13 | **v0.9** | Does Pivot reach people who never open it? |
| **6** | [Flows](phase-6-flows.md) | M13–M16 | **v1.0 GA** | Can Pivot move and shape data, not just read it? |
| **7** | [Pivot AI](phase-7-ai.md) | M16–M19 | **v1.5** | Can someone get a *correct* answer by asking? |
| **8** | [Embed & Extend](phase-8-embedded-extensibility.md) | M19–M21 | **v1.8** | Can others build on Pivot? |
| **9** | [Enterprise & Scale](phase-9-enterprise-scale.md) | M21–M24 | **v2.0** | Will a 5,000-person company deploy it? |
| **10** | [Ecosystem](phase-10-ecosystem.md) | Continuous | — | Does Pivot survive without us? |

Phase 10 runs in parallel from M6 onward — community, docs, integrations, and DevRel are
not a final step.

---

## Timeline

```
M0    M2      M4        M7      M9     M11     M13       M16       M19    M21      M24
│─P0──│──P1───│────P2────│──P3──│──P4──│──P5───│───P6────│───P7────│──P8──│───P9───│
│     │ v0.1  │   v0.3   │ v0.5 │ v0.7 │ v0.9  │ v1.0 GA │  v1.5   │ v1.8 │  v2.0  │
│                                                                                   │
│         ┌──────────────── P10: Ecosystem (continuous from M6) ──────────────────┐ │
```

### Why this order

The sequence is driven by dependency, not by what's exciting.

**Semantic layer before governance (P3 → P4)** because row-level security is enforced in
the semantic compiler. Building permissions first means building them twice.

**Governance before alerting (P4 → P5)** because an alert is a query that runs as someone
else, on a schedule, with no human watching. Getting that wrong leaks data silently.

**Alerting before flows (P5 → P6)** because flows need the same scheduler, retry, and
delivery infrastructure that alerts force us to build properly.

**Everything before AI (→ P7)** because this is the decision the whole plan turns on. An
AI analyst built on raw schemas is a demo. An AI analyst built on a semantic layer with
enforced permissions, verified metrics, and lineage is a product. We ship AI in month 16,
not month 3, because month 3's version would be the thing we're trying to replace.

The counter-argument — that 16 months without AI is commercially untenable in 2026 — is
real. The hedge is in Phase 1 and Phase 2: SQL autocomplete, natural-language chart
formatting, and auto-generated descriptions ship early as scoped, low-risk AI features.
They're useful and they don't require trust we haven't earned yet.

---

## Release gates

A release ships when its gate passes. No gate has ever been passed by asserting it.

| Release | Gate |
|---|---|
| **v0.1** | 5 external testers connect their own database and save a question without help |
| **v0.3** | 3 teams run a real recurring meeting off a Pivot dashboard |
| **v0.5** | A dbt project imports and its metrics match dbt's output exactly |
| **v0.7** | A security review passes with no critical findings; RLS verified by an external pentest |
| **v0.9** | 100 alerts run for 30 days with zero false deliveries and zero missed fires |
| **v1.0** | 10 companies in production; p95 dashboard load under 2s; 30 days at 99.9% uptime |
| **v1.5** | AI answers match the governed metric on 95% of a 500-question benchmark |
| **v1.8** | 3 design partners ship Pivot embedded in their own product |
| **v2.0** | SOC 2 Type II; a 5,000-user deployment sustained for 90 days |

---

## Per-phase effort summary

| Phase | Effort (ew) | With 30% overhead | Team size | Calendar |
|---|---|---|---|---|
| 0 | 22 | 29 | 3 | 8 weeks |
| 1 | 34 | 44 | 4 | 9 weeks |
| 2 | 62 | 81 | 6 | 12 weeks |
| 3 | 44 | 57 | 7 | 8 weeks |
| 4 | 40 | 52 | 8 | 7 weeks |
| 5 | 38 | 49 | 8 | 7 weeks |
| 6 | 68 | 88 | 10 | 10 weeks |
| 7 | 72 | 94 | 11 | 10 weeks |
| 8 | 46 | 60 | 12 | 6 weeks |
| 9 | 78 | 101 | 14 | 9 weeks |
| **Total** | **504** | **655** | — | **~24 months** |

Phase 10 is continuous and budgeted at roughly 1.5 engineers from M6.

---

## Cross-cutting workstreams

Four concerns are not phases. They are constant, and every phase carries their cost.

**Security.** Threat modeling for each new surface, dependency scanning in CI, quarterly
external pentests from Phase 4, and a documented disclosure process. Budget 8% of each
phase.

**Accessibility.** WCAG 2.2 AA is a definition-of-done item on every UI feature, not a
Phase 9 remediation project. Charts need keyboard navigation and screen-reader data
tables. Retrofitting accessibility onto a BI tool costs 5× what building it in does.

**Documentation.** Every user-facing feature ships with docs in the same PR. A feature
without docs is not done. Budget 10% of each phase.

**Performance.** Every phase has performance budgets in
[non-functional-requirements.md](non-functional-requirements.md), enforced by automated
regression tests in CI. Performance is defended continuously, never "optimized later."

---

## The risks that could break this plan

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Connector coverage becomes the adoption blocker | High | High | Prioritize by market share; JVM sidecar for the long tail (open question #1) |
| Semantic layer scope creeps into a LookML-sized language | High | High | Ship YAML-only in P3; a typed expression language needs its own phase |
| DuckDB memory instability under concurrent load | Medium | High | Hard per-query memory caps from day one; load test at every release; `nocgo` fallback |
| AI accuracy stays below the trust threshold | Medium | High | Ship as assisted-authoring first; the agentic analyst gates on benchmark scores |
| A funded competitor ships the same thesis first | Medium | Medium | The dbt wedge and self-hosting are hard to copy from a cloud-only position |
| Team scaling lags the plan | High | High | Phases 8 and 9 are the compressible ones; 6 and 7 are not |
| Postgres/SQLite dual support taxes every migration | Medium | Medium | CI runs both; if the tax exceeds 15% of migration effort, drop SQLite to dev-only |

The two risks worth watching hardest are **semantic layer scope creep** and **connector
coverage**, because both are the kind that consume a quarter before anyone names them.

---

## Navigation

- **Standards and definition of done:** [00-principles.md](00-principles.md)
- **Complete feature inventory:** [feature-matrix.md](feature-matrix.md)
- **Release plan:** [milestones.md](milestones.md)
- **Performance and scale targets:** [non-functional-requirements.md](non-functional-requirements.md)
