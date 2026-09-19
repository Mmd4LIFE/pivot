# Phase 9 — Enterprise & Scale

**Months:** M21–M24 · **Effort:** 78 ew · **Team:** 14 · **Release:** **v2.0**

---

## Goal

Make Pivot deployable by a 5,000-person company with a security team, a compliance
department, and a procurement process. High availability, horizontal scale, enterprise
identity, certification, and the operational tooling that makes a platform team say yes.

None of this is exciting. All of it is the difference between a tool teams like and a
platform companies standardize on.

## Exit criteria

- [ ] SOC 2 Type II report issued
- [ ] A 5,000-user deployment sustained for 90 days at 99.95% uptime
- [ ] 10,000 concurrent dashboard viewers with p95 load under 2s
- [ ] Zero-downtime upgrade verified across two minor versions
- [ ] Disaster recovery exercise: full restore in under 1 hour, RPO under 5 minutes
- [ ] Air-gapped install completes with no external network access

---

## 1. Scale & performance

| ID | Feature | Pri | Size |
|---|---|---|---|
| P9-SCL-001 | Horizontal scaling of stateless API nodes | P0 | L |
| P9-SCL-002 | Worker autoscaling on queue depth | P0 | M |
| P9-SCL-003 | Read-replica routing for metadata queries | P0 | M |
| P9-SCL-004 | Connection pool sharing and multiplexing across nodes | P0 | L |
| P9-SCL-005 | Distributed cache coherence and invalidation | P0 | L |
| P9-SCL-006 | Query result pagination and cursors at the API layer | P0 | M |
| P9-SCL-007 | Metadata partitioning and archival for large installs | P1 | L |
| P9-SCL-008 | Multi-region deployment with regional query routing | P1 | XL |
| P9-SCL-009 | CDN integration for static assets and cached results | P1 | M |
| P9-SCL-010 | Load shedding and graceful degradation under pressure | P0 | M |
| P9-SCL-011 | Performance regression suite at production scale | P0 | L |

**P9-SCL-010 is what keeps a bad day from becoming an outage.** Under overload the system
sheds in priority order: background cache warming stops, then scheduled refreshes queue,
then non-interactive exports defer — interactive queries are the last thing to degrade. A
system that treats all load as equal fails all of it simultaneously.

## 2. High availability

| ID | Feature | Pri | Size |
|---|---|---|---|
| P9-HA-001 | Active-active multi-node with no single point of failure | P0 | L |
| P9-HA-002 | Zero-downtime rolling upgrades | P0 | L |
| P9-HA-003 | Backward/forward-compatible migrations (expand-contract) | P0 | L |
| P9-HA-004 | Automated backup with tested restore | P0 | M |
| P9-HA-005 | Point-in-time recovery | P1 | M |
| P9-HA-006 | Disaster recovery runbook and automation | P0 | M |
| P9-HA-007 | Health-check-driven traffic management | P0 | S |
| P9-HA-008 | Chaos testing in CI (node kill, network partition, DB failover) | P1 | L |
| P9-HA-009 | Warm standby in a second region | P2 | L |

**P9-HA-003 is the technical prerequisite for P9-HA-002.** Zero-downtime upgrades require
that version N and N+1 run simultaneously against the same schema. That means every
migration is expand-contract: add the new column, dual-write, backfill, switch reads, and
only remove the old column a release later. It is slower to develop and it is the only way
rolling upgrades actually work.

## 3. Enterprise identity

| ID | Feature | Pri | Size |
|---|---|---|---|
| P9-IAM-001 | SCIM 2.0 user and group provisioning | P0 | L |
| P9-IAM-002 | Multiple simultaneous identity providers | P0 | M |
| P9-IAM-003 | Group-to-role mapping rules from IdP claims | P0 | M |
| P9-IAM-004 | WebAuthn / passkeys | P1 | M |
| P9-IAM-005 | Organization-wide MFA enforcement | P0 | S |
| P9-IAM-006 | Session policies (timeout, concurrent limits, IP allowlist) | P0 | M |
| P9-IAM-007 | Service accounts with scoped, rotatable credentials | P0 | M |
| P9-IAM-008 | Delegated administration per department | P1 | M |
| P9-IAM-009 | Break-glass emergency access with mandatory audit | P1 | S |
| P9-IAM-010 | Attribute sync from IdP into RLS user attributes | P0 | M |

**P9-IAM-010 completes the RLS story from
[Phase 4](phase-4-collaboration-governance.md#2-row--and-column-level-security).** Manually
assigning a `region` attribute to 5,000 users is not a real option. Syncing it from the
identity provider is. This is the item that makes RLS operable at enterprise scale.

## 4. Compliance & certification

| ID | Item | Pri | Size |
|---|---|---|---|
| P9-CMP-001 | SOC 2 Type II (controls, evidence, audit period) | P0 | XL |
| P9-CMP-002 | GDPR: DPA, data residency, subject rights automation | P0 | L |
| P9-CMP-003 | HIPAA readiness: BAA, PHI handling, encryption attestation | P1 | L |
| P9-CMP-004 | ISO 27001 alignment | P2 | L |
| P9-CMP-005 | Penetration test program (quarterly, published summaries) | P0 | M |
| P9-CMP-006 | Vulnerability disclosure program and bug bounty | P0 | M |
| P9-CMP-007 | SBOM for every release, signed | P0 | S |
| P9-CMP-008 | FIPS 140-3 validated cryptography mode | P2 | M |
| P9-CMP-009 | Trust center (public security posture and subprocessors) | P1 | M |
| P9-CMP-010 | Customer-managed encryption keys (BYOK via KMS) | P1 | L |

**P9-CMP-001 is XL and mostly not engineering work.** An audit period of 3–12 months,
formal policies, evidence collection, and access reviews. It starts at the beginning of
Phase 9 — arguably earlier — because the audit window is calendar time we cannot compress.
The engineering share is evidence automation: access reviews, change management records,
and control monitoring generated from the systems we already have.

## 5. Operations

| ID | Feature | Pri | Size |
|---|---|---|---|
| P9-OPS-001 | Admin console: cluster health, resources, capacity | P0 | L |
| P9-OPS-002 | Configuration management with validation and dry-run | P0 | M |
| P9-OPS-003 | Feature flags with per-org targeting | P0 | M |
| P9-OPS-004 | Maintenance mode with a user-facing notice | P0 | S |
| P9-OPS-005 | Instance migration and tenant export/import | P1 | L |
| P9-OPS-006 | Automated capacity recommendations | P1 | M |
| P9-OPS-007 | Support bundle generator (logs, config, diagnostics, redacted) | P0 | M |
| P9-OPS-008 | Runbook library for common failure modes | P0 | M |
| P9-OPS-009 | Upgrade pre-flight checks | P0 | M |
| P9-OPS-010 | Resource quotas per organization | P1 | M |

**P9-OPS-007 pays for itself in the first month.** Self-hosted support without diagnostics
is an email thread that takes a week. One command producing a redacted bundle with logs,
config, version, migration state, and health data turns it into an hour.

## 6. Air-gapped & sovereign

| ID | Feature | Pri | Size |
|---|---|---|---|
| P9-AIR-001 | Fully offline installation with bundled images | P0 | M |
| P9-AIR-002 | Offline license validation | P0 | M |
| P9-AIR-003 | Local LLM deployment guide and validated configurations | P0 | M |
| P9-AIR-004 | Offline documentation bundle | P1 | S |
| P9-AIR-005 | Manual update channel with signature verification | P0 | M |
| P9-AIR-006 | Zero-telemetry verified mode | P0 | S |
| P9-AIR-007 | Sovereign cloud deployment guides (EU, gov regions) | P1 | M |

Government, defense, healthcare, and finance need this, and most competitors serve them
badly or not at all. It's a differentiated segment reachable largely because of decisions
already made — self-hosting, local models, no mandatory phone-home.

## 7. Advanced multi-tenancy

| ID | Feature | Pri | Size |
|---|---|---|---|
| P9-MT-001 | Full tenant isolation with per-tenant resource limits | P0 | L |
| P9-MT-002 | Tenant provisioning and lifecycle API | P0 | M |
| P9-MT-003 | Per-tenant database isolation option | P1 | L |
| P9-MT-004 | Cross-tenant admin with audited access | P1 | M |
| P9-MT-005 | Tenant-level usage metering and billing export | P1 | M |
| P9-MT-006 | Noisy-neighbor detection and throttling | P0 | M |

---

## Technical notes

### What actually breaks at 10,000 concurrent users
Not the API nodes — those scale horizontally and trivially. The real constraints, in the
order they bite:

1. **Warehouse connection limits.** Snowflake and Postgres cap concurrent connections.
   Solved by pooling and multiplexing (P9-SCL-004), not by adding Pivot nodes.
2. **Metadata database contention.** Every page load reads permissions, content, and
   preferences. Solved by read replicas (P9-SCL-003) and aggressive authorization caching.
3. **Cache invalidation storms.** A model change invalidating a million cache entries
   across a cluster. Solved by generation-based invalidation — bump a version counter
   rather than enumerate keys.
4. **Scheduled-job thundering herd.** Addressed in Phase 5 with jitter; re-verified at
   scale here.

The performance work in this phase targets these four. Adding replicas does not fix any of
them.

### Upgrade safety is a product feature
Self-hosted customers upgrade on their own schedule, and a bad upgrade is the fastest way
to lose one permanently. Every release: tested upgrade from the previous two minors,
tested rollback, pre-flight checks that refuse an unsafe upgrade, and migrations rehearsed
against restored production-scale snapshots.

---

## Explicitly deferred

| Deferred | To | Why |
|---|---|---|
| FedRAMP authorization | Post-v2.0 | Enormous scope; pursue with a committed anchor customer |
| Multi-region active-active writes | Post-v2.0 | Active-passive with regional read routing covers the need |
| Per-tenant custom code execution | — | Security surface too large |
| On-premise Kubernetes operator | Phase 10 | Helm is sufficient |

---

## Risks

| Risk | Mitigation |
|---|---|
| SOC 2 timeline slips and blocks enterprise deals | Start the audit period at M18, not M21. Calendar time cannot be compressed |
| Zero-downtime upgrades prove impossible with existing migrations | Adopt expand-contract discipline from Phase 0; audit all prior migrations in Phase 8 |
| Scale testing needs infrastructure we don't have | Budget for a dedicated load-test environment from M18; k6 scenarios built incrementally from Phase 1 |
| Enterprise features fragment the codebase into OSS and EE forks | One codebase, license-gated features, no forks. Enforced architecturally |
| The phase is entirely unglamorous and morale suffers | Pair it with visible polish work; this is also where accumulated UX debt gets paid down |
