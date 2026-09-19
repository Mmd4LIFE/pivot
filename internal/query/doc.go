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
// Local compute lives here too: DuckDB handles cross-source federation,
// Parquet acceleration, and the shared-subquery consolidation that turns a
// twenty-card dashboard into one warehouse query. See ADR-0004.
//
// Built across Parts 22-23; extended in Phase 3.
package query
