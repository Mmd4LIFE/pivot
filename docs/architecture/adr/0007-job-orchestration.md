# ADR-0007: River for background jobs; Temporal optional at enterprise scale

**Status:** Accepted
**Date:** 2026-09-19

## Context

Pivot needs durable background execution for scheduled dashboard refreshes, alert
evaluation, cache warming, schema sync, subscription delivery, export generation, and —
from [Phase 6](../../roadmap/phase-6-flows.md) — data pipelines that run for hours with
per-node state and resume-from-failure.

Two distinct workloads with different requirements:

- **Jobs** — short, numerous, idempotent, must not be lost
- **Flows** — long-running DAGs with checkpointing, partial resumption, and complex retry

The constraint that shapes the choice: self-hosted users should not have to operate
additional infrastructure for the common case.

## Decision

**River** (a Postgres-backed job queue for Go) for all background jobs, and for Flow
execution in v1 via an explicit DAG executor built on top. **Temporal** as an opt-in
alternative backend for Flows at enterprise scale, behind the same interface.

## Rationale

**River needs no new infrastructure.** It uses the Postgres we already require. For a
product whose on-ramp is a single binary, "and also run a Temporal cluster" is a
non-starter.

**Transactional enqueueing is the decisive feature.** Jobs are enqueued in the same
Postgres transaction as the data change that triggers them. Either both commit or neither
does. This eliminates an entire bug class — "the record saved but the job never ran", and
its inverse — that plagues queues living in separate infrastructure.

**Temporal is genuinely better for long-running workflows**, with durable execution,
sophisticated retry policies, and workflow versioning. It also requires its own cluster and
database. At enterprise scale that cost is justified; at 20 users it is absurd. Making it a
pluggable backend rather than a dependency lets each deployment decide.

Writing the Flow engine against an interface with both backends implemented from the start
keeps this a deployment choice rather than a future rewrite.

## Alternatives considered

### Temporal as the only backend
Best-in-class durable execution, and Flows would be simpler to build on it. Rejected: it
makes a heavyweight external dependency mandatory for everyone, which breaks the
single-binary story and adds significant operational burden to every self-hosted install.

### Celery
Rejected immediately — it's Python, and it's the reason Superset's deployment is hard.

### Redis/Valkey-backed queue (Asynq, Sidekiq-style)
Fast and simple. Rejected on durability: Valkey is
[optional in single-node mode](../tech-stack.md#52-cache-and-message-bus-valkey), and using
it as the durability boundary for jobs that must not be lost is the wrong tradeoff. Losing
an alert evaluation because Redis restarted is unacceptable.

### Cloud-managed queues (SQS, Pub/Sub)
Rejected: cloud-specific, unavailable in self-hosted and air-gapped deployments, and no
transactional enqueueing.

### A homegrown queue
Rejected. This is a solved problem, and the failure modes (visibility timeouts, poison
messages, leader election) are exactly the ones that are subtle and expensive to get wrong.

## Consequences

**Positive**
- No new infrastructure for the common case
- Transactional enqueueing removes a whole bug class
- Job state is queryable with SQL, which makes debugging and the schedule-monitoring UI
  straightforward
- Enterprise deployments can adopt Temporal without a migration

**Negative**
- **Postgres becomes the job queue's throughput ceiling.** At very high job volume this
  will bite, and the mitigation is the Temporal backend rather than tuning
- **We build the DAG executor ourselves** for Flows v1 — checkpointing, partial resumption,
  fan-out/fan-in. Temporal would have given this. This is real Phase 6 scope
- Maintaining two backends behind one interface has an ongoing cost, and the Temporal path
  will inevitably be less exercised
- Long-running flows on a Postgres queue require care around connection holding and
  visibility timeouts

**Neutral**
- Job tables add to the metadata database's growth and must be included in retention
  policies

## Revisit if

- Postgres job throughput becomes a measured bottleneck before enterprise deployments
  justify Temporal
- The DAG executor's complexity in Phase 6 exceeds the cost of adopting Temporal outright
- River's maintenance status changes
