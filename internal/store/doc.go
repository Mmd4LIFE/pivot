// Package store owns Pivot's metadata: migrations, generated queries, and the
// tenant-scoped repository layer.
//
// Two invariants govern everything here.
//
// Portability: every schema change and query must work on both PostgreSQL 16+
// and SQLite 3.45+, verified by running the same suite against both in CI.
// Postgres-only capabilities (pgvector, partitioning) degrade gracefully rather
// than breaking the SQLite build. See ADR-0003.
//
// Tenant isolation: the repository layer injects org scope into every query
// from request context. It must be structurally impossible to write a
// repository method that returns another tenant's rows — not merely unusual.
// This is why org_id is carried on child tables too, denormalized on purpose.
//
// Queries are written as SQL and compiled to typed Go by sqlc. An ORM was
// rejected because its abstraction breaks exactly when metadata queries get
// interesting, and because dual-dialect support needs to be verifiable at
// build time.
//
// Built across Parts 3 (migrations and schema) and 4 (repository layer).
package store
