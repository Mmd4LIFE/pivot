# Phase 10 — Ecosystem

**Months:** Continuous from M6 · **Effort:** ~1.5 engineers ongoing · **Release:** continuous

---

## Goal

Build the things that make Pivot survive without us: documentation people can actually
learn from, a community that answers its own questions, connector coverage that follows
demand, mobile access, and an open-source governance model that makes contributing
rational.

This is not a final phase. It runs in parallel from month 6, because a community that
starts at month 22 doesn't exist.

## Why it starts at M6

The open-source projects that win are the ones where a stranger can go from curiosity to
first contribution without talking to anyone. That requires docs, a clean contribution
path, and visible activity — all of which compound. Starting this at v2.0 means competing
against projects that have three years of accumulated answers on the internet.

---

## 1. Documentation

| ID | Item | Pri | Size |
|---|---|---|---|
| P10-DOC-001 | Documentation site with versioning and search | P0 | M |
| P10-DOC-002 | Getting-started paths per persona (analyst, admin, developer) | P0 | L |
| P10-DOC-003 | Complete feature reference, maintained in-PR | P0 | Ongoing |
| P10-DOC-004 | API reference generated from OpenAPI | P0 | S |
| P10-DOC-005 | Semantic layer authoring guide with worked examples | P0 | L |
| P10-DOC-006 | Deployment guides (Docker, K8s, cloud marketplaces, air-gapped) | P0 | L |
| P10-DOC-007 | Migration guides from Metabase, Superset, Looker, Tableau | P0 | L |
| P10-DOC-008 | Video tutorials and a guided product tour | P1 | L |
| P10-DOC-009 | Troubleshooting and error-code reference | P0 | M |
| P10-DOC-010 | Architecture and internals docs for contributors | P1 | M |
| P10-DOC-011 | Sample projects and reference implementations | P1 | M |

**P10-DOC-007 is a direct growth lever.** Someone actively frustrated with their current
BI tool is the most motivated prospect that exists. A migration guide with automated
content import (Metabase questions and dashboards → Pivot equivalents) converts that
frustration. Build the importers as part of the guides.

**P10-DOC-009 pairs with the error-code convention from
[the definition of done](00-principles.md#documentation).** Every user-facing error carries
a code that maps to a documentation page explaining what happened and what to do. This is a
small discipline with an outsized effect on support load.

## 2. Community

| ID | Item | Pri | Size |
|---|---|---|---|
| P10-COM-001 | Public roadmap and RFC process | P0 | S |
| P10-COM-002 | Discord or Slack community with staffed support hours | P0 | Ongoing |
| P10-COM-003 | GitHub Discussions for Q&A | P0 | S |
| P10-COM-004 | Contribution guide with good-first-issue curation | P0 | M |
| P10-COM-005 | Published governance model and maintainer path | P1 | M |
| P10-COM-006 | Community showcase (dashboards, plugins, semantic layers) | P1 | M |
| P10-COM-007 | Regular release notes and a changelog people read | P0 | Ongoing |
| P10-COM-008 | Office hours and community calls | P1 | Ongoing |
| P10-COM-009 | Security disclosure process and hall of fame | P0 | S |

## 3. Connector expansion

Priority follows demand, tracked in a public voting issue. The framework from
[Phase 1](phase-1-connect-and-query.md#1-connector-framework) makes each of these roughly a
week once the conformance suite is green.

| ID | Group | Connectors | Pri |
|---|---|---|---|
| P10-CON-001 | Warehouses | Firebolt, SingleStore, Exasol, Vertica, Teradata | P1 |
| P10-CON-002 | Query engines | Trino, Presto, Athena, Dremio, Starburst | P0 |
| P10-CON-003 | Databases | SQL Server, Oracle, DB2, CockroachDB, YugabyteDB | P0 |
| P10-CON-004 | Real-time OLAP | Apache Pinot, Apache Druid, Rockset | P1 |
| P10-CON-005 | NoSQL | MongoDB, DynamoDB, Cassandra, Couchbase | P1 |
| P10-CON-006 | Search | Elasticsearch, OpenSearch | P1 |
| P10-CON-007 | Lakehouse | Iceberg, Delta Lake, Hudi (direct) | P0 |
| P10-CON-008 | Time series | InfluxDB, TimescaleDB, Prometheus | P1 |
| P10-CON-009 | Graph | Neo4j | P2 |
| P10-CON-010 | SaaS (via Flows) | 50+ via the Airbyte/Singer bridge | P1 |

**P10-CON-007 is strategically higher priority than it looks.** Direct Iceberg and Delta
reading means Pivot queries the lakehouse without a warehouse in the path at all — which is
where a significant part of the market is moving, and which DuckDB makes straightforward.

## 4. Mobile

| ID | Item | Pri | Size |
|---|---|---|---|
| P10-MOB-001 | Progressive web app with offline dashboard caching | P0 | L |
| P10-MOB-002 | Native iOS app | P1 | XL |
| P10-MOB-003 | Native Android app | P1 | XL |
| P10-MOB-004 | Push notifications for alerts | P1 | M |
| P10-MOB-005 | Biometric authentication | P1 | S |
| P10-MOB-006 | Mobile-optimized chart interactions | P0 | L |
| P10-MOB-007 | Offline viewing of cached dashboards | P1 | L |
| P10-MOB-008 | Mobile AI assistant | P2 | M |

**PWA first (P10-MOB-001), native later.** A well-built PWA delivers most of the value —
home screen install, push notifications, offline caching — at a fraction of the cost, and
it validates whether mobile demand is real before committing to two native codebases.
Native apps get built when push notification reliability and biometric requirements from
actual customers justify them.

## 5. Developer experience

| ID | Item | Pri | Size |
|---|---|---|---|
| P10-DX-001 | One-command local development environment | P0 | M |
| P10-DX-002 | Seeded demo data generator | P0 | M |
| P10-DX-003 | Plugin development kit with templates | P1 | M |
| P10-DX-004 | GitHub Action for semantic layer CI | P1 | S |
| P10-DX-005 | Cloud marketplace listings (AWS, GCP, Azure) | P1 | M |
| P10-DX-006 | One-click deploy to Railway, Render, Fly.io | P1 | S |
| P10-DX-007 | Kubernetes operator | P2 | L |
| P10-DX-008 | Public sandbox instance with sample data | P1 | M |

**P10-DX-008 removes the largest friction in BI evaluation.** Nobody wants to install a
tool and connect production data to find out whether they like it. A public sandbox with a
realistic dataset lets someone evaluate Pivot in 90 seconds. It also gives every
documentation page and blog post a live link to point at.

## 6. Open-source strategy

The license boundary, stated plainly.

### Apache 2.0 core — everything that makes Pivot good
Connectors, semantic layer, query engine, visualization, dashboards, alerting, flows,
**row-level security, audit logging, and lineage**, the AI features, the public API, SDKs,
and the plugin system.

**Governance is not an upsell.** RLS and audit logging are in the open core because a
product where the safe configuration costs extra is a product that ships insecure
deployments. This is a deliberate departure from how most open-core BI vendors draw the
line, and it's a commitment from
[the vision](../vision.md#7-non-negotiable-product-commitments).

### Commercial — things that only matter when you're big
SCIM provisioning, multiple simultaneous identity providers, advanced multi-tenancy,
white-label branding removal, customer-managed encryption keys, FIPS mode, air-gapped
license management, priority support, and SLAs.

The test: **would a 20-person company need this?** If yes, it's open. If it only matters
at 2,000 people with a compliance department, it's commercial. That boundary is defensible
to the community because it tracks a real distinction rather than an arbitrary paywall.

| ID | Item | Pri | Size |
|---|---|---|---|
| P10-OSS-001 | License boundary documented and enforced in code | P0 | M |
| P10-OSS-002 | CLA or DCO process | P0 | S |
| P10-OSS-003 | Public security audit and published results | P1 | M |
| P10-OSS-004 | Reproducible builds | P2 | M |
| P10-OSS-005 | Foundation donation evaluation | P2 | — |

---

## Ongoing metrics

| Metric | Target by M24 |
|---|---|
| GitHub stars | 10,000 |
| External contributors (merged PR) | 150 |
| Community members | 5,000 |
| Docs satisfaction | > 4.2/5 |
| Median time to first response in community | < 4 hours |
| Self-serve install → first query completion rate | > 60% |
| Connectors contributed by the community | 15 |

Stars are a vanity metric and are listed because investors ask. The number that actually
predicts survival is **external contributors** — it measures whether the codebase is
approachable and the contribution path works.

---

## Risks

| Risk | Mitigation |
|---|---|
| Documentation lags features permanently | Docs required in the feature PR; no exceptions, enforced in review |
| Community starts too late to compound | Starts at M6, not at v1.0. Non-negotiable |
| The open-core boundary erodes toward closed | The "20-person company" test is published; moving a feature to commercial requires public justification |
| Connector requests outpace capacity | Public demand voting, a documented contribution path, and the Airbyte bridge for the long tail |
| Community support burden overwhelms a small team | Invest in docs and error codes first; staffed hours, not always-on |
