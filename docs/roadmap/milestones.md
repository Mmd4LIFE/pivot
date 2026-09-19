# Release Plan

From empty repository to v2.0. Each release has a theme, a target user, a gate, and an
explicit statement of what it still can't do.

The last column is the most important one. A release that oversells itself burns the
goodwill of exactly the early users who matter most.

---

## v0.1 — "It queries" · M4

**Theme:** The SQL analyst's tool
**For:** Internal use and 5 friendly design partners

**Ships:** [Phase 0](phase-0-foundations.md) + [Phase 1](phase-1-connect-and-query.md) —
8 connectors, schema catalog, SQL editor with autocomplete, result grid, exports, saved
questions, basic sharing, OIDC login, single-binary install.

**Gate:** 5 external testers connect their own database and save a question without help.

**Cannot yet:** Charts. Dashboards. Non-SQL users. Permissions beyond basic roles.

**Honest positioning:** A good SQL client with saving and sharing. Not yet a BI tool.

---

## v0.3 — "It visualizes" · M7

**Theme:** The team dashboard
**For:** 20 design partners; first public alpha

**Ships:** [Phase 2](phase-2-visualization-dashboards.md) — visual query builder, 30+ chart
types, real pivot tables, dashboards with filters and cross-filtering, drill-down, mobile
layouts, PDF export, collections and search.

**Gate:** 3 teams run a real recurring meeting from a Pivot dashboard.

**Cannot yet:** Governed metrics. RLS. Alerts. Anything scheduled.

**Honest positioning:** Comparable to early Metabase. The differentiators aren't built yet.

---

## v0.5 — "It governs" · M9

**Theme:** One definition of every number
**For:** Design partners running dbt; public beta

**Ships:** [Phase 3](phase-3-semantic-layer.md) — semantic layer with models, metrics, and
joins; dbt import and sync; the query compiler; cross-database federation; Parquet
acceleration; Git versioning; semantic cache invalidation.

**Gate:** A dbt project imports and every metric matches `dbt`'s own output exactly.

**Cannot yet:** Row-level security. Audit logs. Fine-grained permissions.

**Honest positioning:** The first release where Pivot is doing something Metabase and
Superset don't. This is the wedge release.

---

## v0.7 — "It's trustworthy" · M11

**Theme:** Safe for real data
**For:** Companies with a security review

**Ships:** [Phase 4](phase-4-collaboration-governance.md) — collection permissions, RLS,
column masking, audit logging, column-level lineage, comments and annotations, verified
content, query governance and cost attribution.

**Gate:** External penetration test with no critical or high findings; RLS verified across
every surface.

**Cannot yet:** Alerts. Scheduled reports. Flows.

**Honest positioning:** Deployable on sensitive data. Still requires people to log in to
get value.

---

## v0.9 — "It reaches you" · M13

**Theme:** Analytics that come to you
**For:** Release candidate users

**Ships:** [Phase 5](phase-5-alerting-distribution.md) — threshold and anomaly alerts,
Slack/Teams/email/webhook delivery, dashboard subscriptions, personalized reports, PDF and
Excel generation, the distributed scheduler.

**Gate:** 100 alerts run 30 days with zero missed fires and zero false deliveries.

**Cannot yet:** Data pipelines. AI.

**Honest positioning:** Feature-complete as a BI tool. The last release before GA.

---

## v1.0 — GA · M16

**Theme:** Production ready
**For:** General availability

**Ships:** [Phase 6](phase-6-flows.md) — visual pipeline builder, ELT with 10+ sources,
SQL and Python transforms, data quality tests, materialization, reverse-ETL to 4
destinations, flow operations. Plus hardening, performance work, and documentation across
everything prior.

**Gate:** 10 companies in production; p95 dashboard load under 2s; 30 days at 99.9%.

**Cannot yet:** AI analyst. Embedding. Enterprise identity and scale.

**Honest positioning:** A complete, self-hostable data platform. Competitive with Metabase
and Superset on breadth, ahead on governance and cost control.

---

## v1.5 — "It answers" · M19

**Theme:** The AI analyst
**For:** Everyone; the commercial inflection point

**Ships:** [Phase 7](phase-7-ai.md) — Ask Pivot, the agentic analyst with root-cause
analysis, automated insights, AI-enriched alerts, assistive AI throughout, the evaluation
harness, local model support.

**Gate:** 95% on the 500-question benchmark; zero permission violations.

**Cannot yet:** Embedding at scale. SOC 2.

**Honest positioning:** The release where the semantic layer investment pays off. If the
benchmark gate isn't met, this ships as assisted authoring rather than as an analyst — and
we say so.

---

## v1.8 — "It embeds" · M21

**Theme:** Analytics as a feature of your product
**For:** SaaS companies

**Ships:** [Phase 8](phase-8-embedded-extensibility.md) — JWT embedding, React and JS SDKs,
white-labeling, the plugin system with custom visualizations, complete public API, Arrow
Flight SQL, Slack app, Sheets add-on, MCP server.

**Gate:** 3 design partners ship Pivot embedded to their own customers.

**Cannot yet:** SOC 2. 10,000-user scale.

**Honest positioning:** The business-model release. Embedded analytics is where BI revenue
concentrates.

---

## v2.0 — "It scales" · M24

**Theme:** Enterprise standard
**For:** Large organizations

**Ships:** [Phase 9](phase-9-enterprise-scale.md) — horizontal scale, HA, zero-downtime
upgrades, SCIM, multi-IdP, SOC 2 Type II, GDPR tooling, air-gapped deployment, advanced
multi-tenancy, operational console.

**Gate:** SOC 2 Type II issued; 5,000-user deployment for 90 days at 99.95%.

**Honest positioning:** Credible alternative to Looker and Power BI Premium for companies
that want to own their stack.

---

## Cadence between majors

- **Minor release every 4 weeks**, on a fixed date. If a feature isn't ready, it ships in
  the next one. The date does not move.
- **Patch releases as needed**; security fixes within 48 hours of a verified report.
- **Every release upgrades cleanly from the previous two minors.**
- **Feature flags** gate incomplete work. A flag older than two minors is a bug with an
  owner.

---

## What would change this plan

Honest statements of the conditions under which this plan is wrong.

**Accelerate AI (pull Phase 7 earlier).** If competitive pressure makes a 16-month wait
untenable, the compromise is to ship AI against tables rather than the semantic layer —
accepting lower accuracy — and re-ground it on the semantic layer at v0.5. **This is not
recommended.** It ships the thing we're differentiating against, and the first wrong answer
costs more trust than the feature earns.

**Cut Flows from v1.0.** Flows is the largest phase and the most separable. If timeline
pressure is severe, GA at M13 with Phases 0–5 is a coherent, complete BI product, and
Flows becomes v1.2. This is the **recommended** compression if one is needed.

**Delay embedding.** Phase 8 moves after Phase 9 if enterprise deals arrive before SaaS
embedding demand. Market-driven; either order works.

**Extend Phase 3.** If the semantic layer isn't right, everything downstream inherits the
problem. Adding 4 weeks to Phase 3 is cheaper than any later fix. This is the one phase
where slipping the date is the correct call.

---

## Milestone dependencies

```
P0 ──► P1 ──► P2 ──┬──► P3 ──► P4 ──► P5 ──► P6 ──► P7 ──► P8 ──► P9
                   │           │              │
                   └───────────┴──────────────┘
                     P10 (continuous from M6)

Hard dependencies:
  P3 → P4   RLS is enforced in the semantic compiler
  P4 → P5   Alerts run as a user; personalized delivery needs RLS
  P5 → P6   Flows reuse the scheduler and delivery infrastructure
  P3 → P7   AI compiles to semantic queries, not SQL
  P4 → P7   AI retrieval must be permission-filtered
  P4 → P8   Embedding maps external identity to RLS policies
```
