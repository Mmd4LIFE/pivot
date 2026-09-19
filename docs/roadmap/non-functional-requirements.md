# Non-Functional Requirements

Hard numbers. Every one of these is enforced by an automated test, and a regression against
any of them blocks merge.

Targets without measurement are aspirations. Each requirement below names the phase it
becomes binding and how it is verified.

---

## 1. Performance

### 1.1 Interactive latency

| Operation | Target (p95) | Target (p99) | Binding from |
|---|---|---|---|
| Page load (warm cache) | 1.0s | 2.0s | Phase 2 |
| Page load (cold cache) | 2.5s | 4.0s | Phase 2 |
| Dashboard render, 20 cards, cached | 2.0s | 3.5s | Phase 2 |
| Single cached query | 200ms | 500ms | Phase 1 |
| Cross-filter propagation | 300ms | 600ms | Phase 2 |
| Autocomplete suggestion | 100ms | 250ms | Phase 1 |
| Permission check | 10ms | 50ms | Phase 0 |
| Semantic query compilation | 50ms | 150ms | Phase 3 |
| API request (metadata CRUD) | 150ms | 400ms | Phase 0 |
| Embedded dashboard first render | 1.5s | 3.0s | Phase 8 |
| AI single-metric answer | 8s (median) | 20s | Phase 7 |

Query execution time against a user's warehouse is excluded — we don't control it. But
**overhead added by Pivot** must stay under **150ms p95** on top of raw source execution
time, and that is measured separately.

### 1.2 Throughput & data volume

| Dimension | Target | Binding from |
|---|---|---|
| Result rows streamed to export | 100M, memory-bounded | Phase 1 |
| Result rows in the browser grid | 1M, smooth scroll | Phase 1 |
| Concurrent queries per node | 500 | Phase 1 |
| Concurrent dashboard viewers | 10,000 | Phase 9 |
| Alert evaluations per 5-min window | 10,000 | Phase 5 |
| Flow throughput | 100M rows/hour/worker | Phase 6 |
| Catalog objects | 1M columns indexed | Phase 3 |
| Metadata objects (dashboards, questions) | 500,000 | Phase 9 |

### 1.3 Resource budgets

| Deployment | Memory | CPU | Notes |
|---|---|---|---|
| Single binary, idle | < 150MB | < 1% | The 30-second-start configuration |
| Single binary, 10 active users | < 512MB | < 1 core | |
| API node, 500 concurrent users | < 2GB | 2 cores | |
| Worker node | < 4GB | 2 cores | DuckDB gets a hard cap |
| Per-query memory cap | 1GB default | — | Configurable; enforced, not advisory |
| Chromium render worker | < 1GB | 1 core | Recycled after 50 renders |

**Per-query memory caps are enforced, not advisory.** A query exceeding its budget is
killed with a clear error. The alternative — DuckDB consuming all host memory and taking
down dashboards for everyone — is unacceptable, and it is the primary risk identified for
the compute layer in [tech-stack.md](../architecture/tech-stack.md#known-risks).

### 1.4 Frontend budgets

| Bundle | Gzipped budget | Binding from |
|---|---|---|
| Initial (login → shell) | 200KB | Phase 0 |
| Main application | 600KB | Phase 2 |
| Per-route chunk | 150KB | Phase 2 |
| Embed bundle | 350KB | Phase 8 |
| Chart library (lazy) | 300KB | Phase 2 |

Other frontend requirements, binding from Phase 2:

- Time to Interactive under 2.5s on a mid-tier laptop, throttled to Fast 3G
- Cumulative Layout Shift under 0.1
- No main-thread task over 50ms during dashboard interaction
- Every list or grid over 50 rows virtualized
- 60fps during chart interaction and dashboard drag

---

## 2. Availability & reliability

| Metric | Target | Binding from |
|---|---|---|
| Uptime (single node) | 99.5% | Phase 6 |
| Uptime (HA cluster) | 99.95% | Phase 9 |
| Planned downtime for upgrade | 0 (rolling) | Phase 9 |
| RPO (recovery point objective) | 5 minutes | Phase 9 |
| RTO (recovery time objective) | 1 hour | Phase 9 |
| Alert delivery success | 99.9% | Phase 5 |
| Missed alert evaluations | 0 | Phase 5 |
| Data loss on ungraceful shutdown | 0 committed writes | Phase 0 |

### Degradation policy

When a dependency fails, Pivot degrades in a defined order rather than failing whole:

| Failure | Behavior |
|---|---|
| AI service unavailable | AI features disabled with a clear notice; everything else unaffected |
| Valkey unavailable | Fall back to in-process cache; multi-node features degrade; no data loss |
| Object storage unavailable | Acceleration disabled, queries route to source; exports queue |
| A source database unavailable | Only that connection's content errors; the rest of the app works |
| OpenFGA unavailable | **Fail closed.** No access decisions means no access |
| Metadata database unavailable | Full outage; this is the one hard dependency |

**OpenFGA failing closed is deliberate.** An authorization system that fails open is worse
than no authorization system, because it creates the false belief that access is controlled.

---

## 3. Scalability

| Dimension | v1.0 | v2.0 |
|---|---|---|
| Users per instance | 1,000 | 10,000 |
| Concurrent active users | 200 | 2,000 |
| Connections per instance | 50 | 500 |
| Dashboards | 10,000 | 100,000 |
| Semantic models | 1,000 | 10,000 |
| Alerts | 5,000 | 50,000 |
| Flows | 500 | 5,000 |
| Tenants (embedded) | 100 | 10,000 |
| Audit events/day | 10M | 500M |

Scaling is horizontal for API and worker nodes and vertical-plus-replicas for the metadata
database. The constraints that actually bind at scale are documented in
[Phase 9](phase-9-enterprise-scale.md#technical-notes) — they are not the API tier.

---

## 4. Security

| Requirement | Standard | Binding from |
|---|---|---|
| Transport encryption | TLS 1.3 minimum, TLS 1.2 fallback | Phase 0 |
| Data at rest | AES-256-GCM | Phase 0 |
| Secrets | Envelope encryption, KMS-wrapped | Phase 0 / KMS Phase 9 |
| Password hashing | Argon2id (m=64MB, t=3, p=4) | Phase 0 |
| Session token entropy | 256 bits | Phase 0 |
| Permission propagation delay | < 5s p99 | Phase 4 |
| Critical vulnerability patch | < 48 hours | Phase 0 |
| High vulnerability patch | < 7 days | Phase 0 |
| Dependency scan frequency | Every build | Phase 0 |
| Penetration test | Quarterly | Phase 4 |
| Cross-tenant data leakage | Zero tolerance | Phase 0 |

### Security invariants

These are asserted by automated tests, not by review:

1. No query path bypasses the semantic compiler's authorization
2. No cached result is served to a user under a different policy set
3. No secret appears in logs, traces, error messages, or API responses
4. No tenant's metadata query can return another tenant's rows
5. No AI-generated query executes without compiler validation
6. No embedded token is accepted after expiry or on replay

---

## 5. Accessibility

**WCAG 2.2 Level AA across the entire application**, binding from Phase 0. Not a Phase 9
remediation project.

| Requirement | Standard |
|---|---|
| Keyboard navigation | Every function reachable; visible focus |
| Screen reader | Tested with NVDA, JAWS, and VoiceOver |
| Text contrast | ≥ 4.5:1 |
| UI and chart element contrast | ≥ 3:1 |
| Color independence | No meaning by color alone |
| Motion | Respects `prefers-reduced-motion` |
| Zoom | Usable at 200% without horizontal scroll |
| Chart accessibility | Keyboard-navigable data points, accessible data table equivalent |
| Automated scan | Zero axe violations, enforced in CI |

**Chart accessibility is where BI tools fail**, and it's why it's in the chart abstraction
layer rather than in individual charts. Every visualization gets an equivalent data table
and keyboard traversal for free because the abstraction provides them.

---

## 6. Compatibility

| Dimension | Support |
|---|---|
| Browsers | Last 2 versions of Chrome, Firefox, Safari, Edge |
| Mobile browsers | iOS Safari 16+, Chrome Android last 2 |
| Screen sizes | 320px to 4K |
| Operating systems (server) | Linux (amd64, arm64), macOS, Windows |
| Kubernetes | 1.28+ |
| PostgreSQL (metadata) | 14+ |
| Upgrade path | From any version within 2 minor releases |
| API compatibility | 12-month deprecation window |
| Data export | No proprietary formats; everything exportable |

---

## 7. Operability

| Requirement | Target | Binding from |
|---|---|---|
| Time to first query (new install) | < 30 seconds | Phase 0 |
| Cold start to serving | < 5 seconds | Phase 0 |
| Graceful shutdown | < 30 seconds, no dropped requests | Phase 0 |
| Migration duration (1M objects) | < 5 minutes | Phase 9 |
| Backup duration (10GB metadata) | < 10 minutes | Phase 9 |
| Support bundle generation | < 60 seconds | Phase 9 |
| Config change effect | No restart for most settings | Phase 9 |

---

## 8. Cost efficiency

These are product features, not just operational concerns — warehouse spend is the
loudest complaint in modern BI.

| Metric | Target | Binding from |
|---|---|---|
| Cache hit rate (typical dashboard workload) | > 70% | Phase 3 |
| Warehouse query reduction via acceleration | > 80% for accelerated models | Phase 3 |
| Shared-subquery consolidation on dashboards | > 50% fewer queries | Phase 2 |
| AI cost per answer | < $0.05 median | Phase 7 |
| Infrastructure cost per 100 users | < $200/month | Phase 9 |

---

## 9. How these are enforced

| Requirement class | Enforcement |
|---|---|
| Latency | k6 scenarios in CI against a reference dataset; regression >10% fails the build |
| Bundle size | `size-limit` in CI with per-bundle budgets |
| Memory | Load tests with container limits; OOM fails the build |
| Accessibility | axe-core in CI plus quarterly manual audit |
| Security invariants | Dedicated test suite; any failure is a release blocker |
| Availability | Production SLO monitoring with error budgets |
| Scale | Quarterly load test at the next release's target |

**The reference dataset** is a fixed 50-table, 500M-row synthetic warehouse with a
50-card reference dashboard. Every performance number in this document is measured against
it, so results are comparable across releases. It's generated by
[P10-DX-002](phase-10-ecosystem.md#5-developer-experience) and versioned alongside the
code.
