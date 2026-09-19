# ADR-0006: Three-tier cache with generation-based invalidation

**Status:** Accepted
**Date:** 2026-09-19

## Context

Caching is the difference between a BI tool that's pleasant and one that's expensive and
slow. It's also where a performance optimization becomes a data breach.

Three requirements, one of which is in tension with the others:

1. **Fast** — cached queries under 200ms p95
2. **Cheap** — cut warehouse spend substantially; target >70% hit rate
3. **Correct** — a cached result must never be served to a user whose permissions differ
   from the one it was computed for

Requirement 3 is in tension with 2: keying the cache by user ID guarantees correctness and
destroys the hit rate.

## Decision

A three-tier cache (in-process → Valkey → object storage) with cache keys derived from the
compiled SQL **plus a hash of the requesting user's resolved policy set**, and
generation-based invalidation driven by semantic model changes and source freshness.

## Rationale

### Tiers match access patterns

| Tier | Store | Format | Latency | TTL | For |
|---|---|---|---|---|---|
| L1 | In-process LRU | Arrow batches | <1ms | seconds–minutes | Repeated views of the same dashboard |
| L2 | Valkey | Arrow IPC | ~5ms | minutes–hours | Shared across nodes |
| L3 | Object storage | Parquet | ~50ms | hours–days | Large results, materializations |

### The key includes the policy set, not the user

```
key = hash(compiled_sql + tenant_id + hash(resolved_policy_set) + freshness_requirement)
```

Hashing the *resolved policy set* rather than the user ID means users subject to identical
policies share cache entries, while users under different policies are structurally unable
to collide. In a typical organization, hundreds of users fall into a handful of policy sets
— so the hit rate stays high and correctness is not a matter of care.

This design was fixed in [Phase 1](../../roadmap/phase-1-connect-and-query.md), before RLS
existed, because retrofitting identity into a cache key invalidates every assumption built
on top of it.

### Invalidation bumps a generation, never enumerates keys

Three triggers:

| Trigger | Mechanism |
|---|---|
| TTL expiry | Per-policy time bound |
| Semantic change | A model or metric changes → increment that model's generation counter |
| Source freshness | Watermark check (max timestamp, row count, source metadata) → increment |

Cache keys incorporate the generation counters of every model they derive from. Bumping a
counter makes every derived key unreachable instantly, with no scan and no delete.

The alternative — enumerating and deleting affected keys — requires a reverse index from
models to keys, and a model change at scale could mean deleting millions of entries across
a cluster. Generation counters make that a single integer increment. Orphaned entries
expire by TTL and are evicted by LRU.

## Alternatives considered

### Single-tier (Valkey only)
Simpler. Rejected: no in-process tier means a network round trip for every hit, which
misses the 200ms target for dashboards where the same query repeats across cards. And
Valkey is a poor store for multi-gigabyte results.

### Key by user ID
Correct and simple. Rejected: the hit rate collapses in proportion to user count, which
defeats the purpose in exactly the large deployments where caching matters most.

### Key by query only
Maximum hit rate. Rejected: it's a data breach. A user's restricted result would be served
to any user issuing the same logical query.

### No caching, rely on warehouse result caching
Snowflake and BigQuery cache results themselves. Rejected: it doesn't work across
federation or acceleration, provides no control over invalidation, does nothing for
non-warehouse sources, and still bills for the query.

### Explicit key-list invalidation
Rejected on the scan cost described above.

## Consequences

**Positive**
- High hit rates without correctness risk
- Model changes invalidate instantly and cheaply
- Acceleration (L3 Parquet) reuses the same machinery
- Cache observability comes free: entries record their model dependencies

**Negative**
- **Policy-set hashing must be exactly right.** A bug here is a cross-user data leak. It
  gets dedicated tests and is listed as a security invariant in the
  [NFRs](../../roadmap/non-functional-requirements.md#security-invariants)
- Orphaned entries after a generation bump occupy space until TTL expiry — acceptable,
  since LRU eviction handles pressure
- Three tiers means three failure modes and more operational surface
- Freshness detection requires a source query, which has its own cost; mitigated by
  checking on a slower schedule than the cache TTL

**Neutral**
- Cache warming (P3-CCH-004) becomes necessary to realize the benefit for scheduled
  dashboards

## Revisit if

- Hit rates fall below 50% in production, indicating policy sets are more fragmented than
  expected
- Generation-counter contention becomes a bottleneck at scale
- Partial cache reuse (serve a subset, fetch the remainder) proves necessary — currently
  deferred as P2
