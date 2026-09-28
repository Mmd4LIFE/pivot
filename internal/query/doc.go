// Package query runs compiled queries and manages their lifecycle: planning,
// routing, execution, streaming, caching, cancellation, and logging.
//
// The pipeline is streaming end to end. Arrow record batches flow from the
// connector through transforms to the serializer, and no stage materializes a
// whole result set. That is what makes 10M rows to CSV possible inside a
// 512MB container, and it means a transform requiring a materialized result is
// a design error rather than an optimization target.
//
// Caching is three-tier (in-process, Valkey, object storage) with keys derived
// from the compiled SQL plus a hash of the requesting user's resolved policy
// set. Hashing the policy set rather than the user ID keeps hit rates high
// while making it structurally impossible for two users under different
// policies to share an entry. Invalidation bumps a generation counter instead
// of enumerating keys. See ADR-0006.
//
// [Cache] is the L1 tier. It holds decoded rows rather than Arrow batches and
// keys on a [policy.Fingerprint]; ADR-0012 amends ADR-0006 with the reasons for
// both. The fingerprint is part of the key rather than a check beside it, so
// two callers with different policy sets are not refused each other's entry --
// they cannot name it. A caller who cannot be fingerprinted is neither cached
// nor served from cache, which costs a miss and is the only direction worth
// failing in.
//
// An entry is filled by teeing rows aside as they stream, and the copy is
// abandoned the moment it outgrows the per-entry byte budget. That is what lets
// this coexist with the constant-memory property above: results worth caching
// are cached, and results too large to hold are streamed exactly as they were
// before the cache existed.
//
// Local compute lives here too: DuckDB handles cross-source federation,
// Parquet acceleration, and the shared-subquery consolidation that turns a
// twenty-card dashboard into one warehouse query. See ADR-0004.
//
// [Executor] is the single door. Every execution goes through the same five
// stages -- parse, authorize, plan, execute, stream -- and authorization
// happens in stage two, before a connector is opened. ADR-0009 makes the
// semantic compiler the only place row-level security is injected, which is a
// guarantee only as good as the promise that nothing else can reach a source;
// the tests in single_door_test.go are what make that promise checkable.
//
// Every execution is written to the query log in two phases: a row when it
// starts and the outcome when it ends. A query that is still running is
// therefore visible, and a process that dies mid-query leaves the last thing
// Pivot knew rather than nothing at all.
//
// Built across Parts 20-23; extended in Phase 3.
package query
