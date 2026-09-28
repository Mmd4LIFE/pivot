# ADR-0012: The L1 result cache holds decoded rows, and a fingerprint stands in for the policy set

**Status:** Accepted
**Date:** 2026-09-28
**Deciders:** Pivot maintainers

**Amends:** [ADR-0006](0006-caching-strategy.md) — the L1 tier's stored format, and the
name of the thing the key is derived from.

## Context

[ADR-0006](0006-caching-strategy.md) fixed the caching strategy in the abstract, before
any of it was built. Part 21 is the first tier actually implemented, and building it
surfaced three things the abstract design did not have to answer.

**ADR-0006 says L1 stores "Arrow batches".** That was written before Part 20-a existed. It
now has a concrete meaning: `arrow.RecordBatch`, which is *reference counted*. A cached
entry is by definition shared — that is the entire point — and a shared reference-counted
buffer replayed to an unknown number of concurrent readers is a discipline, not a
guarantee. Getting it wrong is a use-after-free or a silent leak, and neither announces
itself.

**ADR-0006 says the key includes "a hash of the requesting user's resolved policy set".**
Row-level security does not ship until Phase 4. There is no policy set today. The ADR
anticipated this and gave the reason to build it anyway — *retrofitting identity into a
cache key invalidates every assumption built on top of it* — but it did not say what to
hash in the meantime, and "nothing, for now" is the answer that produces the data breach
the requirement exists to prevent.

**Part 20-a's deliverable was constant-memory streaming**, measured: 20,000 rows and
200,000 rows both peak at 3.1 MB. A cache is materialization. These are in direct tension,
and the tension is not resolvable by being careful — it needs a rule.

## Decision

The L1 cache stores **decoded rows**, deep-copied on insert and immutable thereafter, and
serves a hit as a `connectors.Stream` that replays them. Arrow conversion happens above the
cache, on the same code path for a hit and a miss.

The key is derived from a **`policy.Fingerprint`** — a hash of every authorization fact
that could change which rows a caller may see. Today that is the set of permissions the
subject holds, resolved through the authorization graph. In Phase 4 it grows to include the
resolved RLS predicate set. The key derivation does not change.

**A result is cached only while it fits a per-entry byte budget.** The pipeline tees rows
into a pending entry as it streams; the moment the entry exceeds the budget the pending
entry is discarded and the stream continues untouched, in constant memory.

## Rationale

### Rows, because a cached entry is shared by construction

An entry exists to be read by many callers at once. `[][]any` of decoded values, copied
once at insert and never written again, is safe to replay concurrently with no protocol at
all — the safety is a property of the data, not of every future caller remembering to
`Retain` and `Release`.

There is a second reason, and it is the stronger one: **a hit and a miss must not be able
to differ.** Caching rows means the Arrow conversion runs on the same values in both cases,
through the same `Batches` code. Caching Arrow would create a second path, and the two
would drift on exactly the things that are easy to get subtly different — a decimal
rendered as text, a null in a column the source declared `NOT NULL`, a truncation flag.
Part 20-a made `Query` a loop over `Stream` for this reason; this is the same argument.

The cost is real and worth stating: `[]any` boxing is less compact than an Arrow buffer,
so L1 holds fewer bytes of result per byte of memory than ADR-0006 assumed. L1 is bounded
by a byte budget rather than by a row count, so the effect is a smaller cache, not a
surprise.

### A fingerprint, because the seam has to exist before the thing that fills it

`policy.Fingerprint` is one value with one job: *given a caller, what could change which
rows they are allowed to see?* Today the honest answer is their resolved permissions. That
is not a placeholder — two users with different roles genuinely resolve differently, and
keying on it means they cannot share an entry.

The point is the shape. In Phase 4, RLS predicates are added to what the fingerprint
resolves from. Nothing else moves: not the key derivation, not the cache, not the pipeline,
and not the test that proves two callers with different policies cannot collide — that test
is written now against a role difference and keeps passing when the definition widens.

**The fingerprint fails closed.** A caller whose policy set cannot be resolved is not
cached and is not served from cache. The alternative — treating an unresolvable policy set
as an empty one — makes every such caller collide with every other, which is precisely the
breach. An error resolving a fingerprint costs a cache miss; the other way costs somebody
else's rows.

### A byte budget, because the alternative is choosing between two shipped properties

Without a bound, caching a ten-million-row export holds it entirely in memory and undoes
Part 20-a inside a 512 MB container. With a *row count* bound, a thousand rows of wide text
does the same thing.

Teeing under a byte budget keeps both properties: small results — dashboard cards, which
are the results worth caching — are cached, and large ones stream exactly as they did
before and are never cached. Peak memory is bounded by `min(result size, budget)`.

Discarding the partial entry rather than truncating it matters: a truncated cache entry is
a wrong answer that looks like a right one, which is the same failure mode Part 16 refused
for truncated results.

## Alternatives considered

### Cache Arrow batches, as ADR-0006 said

Denser, and saves the row→Arrow conversion on a hit. Rejected on the shared-mutable-state
argument above, and because the conversion it saves is negligible against the warehouse
round trip the cache is actually eliminating. Worth revisiting for L3, where the stored
form is Parquet and the reference-counting question does not arise.

### Key by user ID until RLS exists

Trivially correct, and it would have shipped faster. Rejected for the reason ADR-0006 gives
for rejecting it permanently: the hit rate collapses in proportion to user count. It would
also have made the Phase 4 change a migration of every key rather than a widening of one
function — which is the specific outcome ADR-0006 was written to avoid.

### No bound, and rely on the LRU

An LRU bounded by entry *count* evicts eventually, but "eventually" is after the ten-million
-row result is already resident. The bound has to apply while the result is arriving, not
after.

### Cache at the connector instead of in the pipeline

Closer to the source and would catch the catalog's introspection reads too. Rejected: the
connector has no idea who is asking, and a cache that cannot see the caller cannot key on
their policy set. Part 20-b made the pipeline the only door precisely so that decisions
needing to know the caller have somewhere to live.

## Consequences

**Positive:** the correctness property is structural rather than careful — two callers with
different policy sets cannot produce the same key, and the Phase 4 change is one function.
A hit and a miss cannot diverge, because they share the conversion path. Constant-memory
streaming survives a cache existing.

**Negative:** L1 holds less result per byte than ADR-0006 assumed, so the hit rate for large
results will be lower than that ADR's >70% target implies; the target should be read as
applying to results under the budget. The fingerprint costs an authorization resolution on
every query, which is cached (`authz.Cache`) but not free. And results above the budget are
never cached at all, which is a cliff rather than a gradient — a dashboard card that grows
past the budget silently stops being fast.

**Neutral:** ADR-0006's tier table, key formula, generation-based invalidation and targets
are otherwise unchanged. L2 and L3 remain unbuilt and are free to store Arrow or Parquet;
this decision is about L1 only.

## Revisit if

- L1's hit rate measured in a real deployment falls below 40%, which would suggest the byte
  budget is set below the size of the results people actually repeat.
- Profiling shows the row→Arrow conversion on a hit is a material share of cached-query
  latency — the p95 target is 200 ms and a hit should be three orders of magnitude under it,
  so this would mean something else is wrong.
- Arrow gains a cheap immutable-slice type that removes the reference-counting hazard.
- Phase 4 finds that the RLS predicate set cannot be resolved cheaply enough to fingerprint
  on every query, which would make the per-query resolution the bottleneck this ADR assumed
  it was not.
