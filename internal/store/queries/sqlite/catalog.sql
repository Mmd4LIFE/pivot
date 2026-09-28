-- The catalog: what Pivot has seen in a connected database.
--
-- Placeholders are positional in both dialects and must appear in the same
-- order, because the two generated parameter structs are converted directly
-- into one another and a differing field order breaks the conversion - the
-- lesson of Part 4-a. Query files are ASCII: sqlc's SQLite generator miscounts
-- byte offsets on multibyte characters and corrupts generation.
--
-- No placeholder is used twice, even where PostgreSQL would allow it. SQLite's
-- `?` is positional and a repeat is a *second* parameter, so ? appearing twice
-- here and `?` appearing twice there produce parameter structs of different
-- sizes - which is exactly the divergence the note above is about.
--
-- The shape of a sync is upsert-then-sweep. Every table the source reports is
-- upserted with a fresh last_seen_at; afterwards, anything in this connection
-- not seen by that sweep is marked removed. That is what makes a sync a
-- comparison rather than a replacement, and it needs no temporary table and no
-- transaction held open across the whole source.

-- name: UpsertCatalogTable :one
INSERT INTO catalog_tables (
    id, org_id, connection_id, schema_name, table_name, table_type, comment,
    first_seen_at, last_seen_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (connection_id, schema_name, table_name) DO UPDATE SET
    table_type   = EXCLUDED.table_type,
    comment      = EXCLUDED.comment,
    last_seen_at = EXCLUDED.last_seen_at,
    removed_at   = NULL,
    updated_at   = EXCLUDED.last_seen_at,
    version      = catalog_tables.version + 1
RETURNING *;

-- name: UpsertCatalogColumn :one
INSERT INTO catalog_columns (
    id, org_id, table_id, column_name, source_type, canonical_type,
    is_nullable, position, comment, first_seen_at, last_seen_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (table_id, column_name) DO UPDATE SET
    source_type    = EXCLUDED.source_type,
    canonical_type = EXCLUDED.canonical_type,
    is_nullable    = EXCLUDED.is_nullable,
    position       = EXCLUDED.position,
    comment        = EXCLUDED.comment,
    last_seen_at   = EXCLUDED.last_seen_at,
    removed_at     = NULL,
    updated_at     = EXCLUDED.last_seen_at,
    version        = catalog_columns.version + 1
RETURNING *;

-- name: SweepCatalogTables :execrows
UPDATE catalog_tables
SET removed_at = ?, updated_at = ?, version = version + 1
WHERE connection_id = ? AND org_id = ?
  AND last_seen_at < ?
  AND removed_at IS NULL;

-- name: SweepCatalogColumns :execrows
UPDATE catalog_columns
SET removed_at = ?, updated_at = ?, version = version + 1
WHERE catalog_columns.org_id = ?
  AND catalog_columns.removed_at IS NULL
  AND catalog_columns.last_seen_at < ?
  AND catalog_columns.table_id IN (
      -- Not filtered on org_id again. The outer clause already restricts this
      -- to the caller's organization, and repeating it would make org_id a
      -- second parameter -- which sqlc names OrgID_2 and which the two
      -- generated structs then have to agree about for no benefit.
      SELECT catalog_tables.id FROM catalog_tables
      WHERE catalog_tables.connection_id = ?
  );

-- name: ListCatalogTables :many
SELECT * FROM catalog_tables
WHERE connection_id = ? AND org_id = ?
ORDER BY schema_name, table_name;

-- name: ListCatalogColumns :many
SELECT c.* FROM catalog_columns c
JOIN catalog_tables t ON t.id = c.table_id
WHERE t.connection_id = ? AND c.org_id = ?
ORDER BY t.schema_name, t.table_name, c.position;

-- name: GetCatalogTable :one
SELECT * FROM catalog_tables
WHERE connection_id = ? AND org_id = ? AND schema_name = ? AND table_name = ?;

-- name: ListCatalogColumnsForTable :many
SELECT * FROM catalog_columns
WHERE table_id = ? AND org_id = ?
ORDER BY position;
