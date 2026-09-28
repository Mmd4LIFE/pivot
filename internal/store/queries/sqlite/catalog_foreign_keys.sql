-- Declared relationships between tables.
--
-- Placeholders are positional in both dialects and must appear in the same
-- order, and none is used twice - SQLite's `?` is positional and a repeat is a
-- second parameter, which would make the two generated structs different sizes.
-- Query files are ASCII: sqlc's SQLite generator miscounts byte offsets on
-- multibyte characters and corrupts generation.

-- name: UpsertCatalogForeignKey :one
INSERT INTO catalog_foreign_keys (
    id, org_id, connection_id, constraint_name,
    from_schema, from_table, from_column,
    to_schema, to_table, to_column,
    ordinal, first_seen_at, last_seen_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (connection_id, from_schema, from_table, constraint_name, ordinal) DO UPDATE SET
    to_schema    = EXCLUDED.to_schema,
    to_table     = EXCLUDED.to_table,
    to_column    = EXCLUDED.to_column,
    from_column  = EXCLUDED.from_column,
    last_seen_at = EXCLUDED.last_seen_at,
    removed_at   = NULL,
    updated_at   = EXCLUDED.last_seen_at,
    version      = catalog_foreign_keys.version + 1
RETURNING *;

-- name: SweepCatalogForeignKeys :execrows
UPDATE catalog_foreign_keys
SET removed_at = ?, updated_at = ?, version = version + 1
WHERE connection_id = ? AND org_id = ?
  AND last_seen_at < ?
  AND removed_at IS NULL;

-- name: ListCatalogForeignKeys :many
SELECT * FROM catalog_foreign_keys
WHERE connection_id = ? AND org_id = ?
ORDER BY from_schema, from_table, constraint_name, ordinal;
