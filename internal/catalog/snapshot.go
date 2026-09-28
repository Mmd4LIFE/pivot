package catalog

import (
	"context"
	"sort"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

/*
A snapshot is what Pivot knew before a sync ran.

Read once, in two queries, and indexed by the identity a source object
actually has -- schema, table and column names -- rather than by the row id
Pivot assigned. A source knows nothing about Pivot's ids, so they are the wrong
key for the comparison even though they are the right key for storage.

It also tracks what has been seen. That is what lets the sweep's row counts be
turned back into names: the database can say five objects went, and only this
can say which five.
*/
type snapshot struct {
	tables  map[tableKey]model.CatalogTable
	columns map[columnKey]model.CatalogColumn

	seenTables  map[tableKey]bool
	seenColumns map[columnKey]bool

	// byID maps a stored table back to its identity, so a column's key can be
	// built from the column rows alone -- they carry a table_id and nothing
	// else about where they live.
	byID map[uuid.UUID]tableKey
}

type tableKey struct {
	schema string
	table  string
}

type columnKey struct {
	schema string
	table  string
	column string
}

func (s *Syncer) snapshot(ctx context.Context, connectionID uuid.UUID) (*snapshot, error) {
	tables, err := s.store.Tables(ctx, connectionID)
	if err != nil {
		return nil, err
	}

	columns, err := s.store.Columns(ctx, connectionID)
	if err != nil {
		return nil, err
	}

	shot := &snapshot{
		tables:      make(map[tableKey]model.CatalogTable, len(tables)),
		columns:     make(map[columnKey]model.CatalogColumn, len(columns)),
		seenTables:  make(map[tableKey]bool, len(tables)),
		seenColumns: make(map[columnKey]bool, len(columns)),
		byID:        make(map[uuid.UUID]tableKey, len(tables)),
	}

	for _, table := range tables {
		key := tableKey{schema: table.SchemaName, table: table.TableName}
		shot.tables[key] = table
		shot.byID[table.ID] = key
	}

	for _, column := range columns {
		owner, ok := shot.byID[column.TableID]
		if !ok {
			// A column whose table is not in the same read. Possible only if
			// something deleted the table between the two queries, which
			// nothing does -- removal is a flag. Skipped rather than guessed
			// at: a column with no table has no identity to compare against.
			continue
		}

		shot.columns[columnKey{
			schema: owner.schema, table: owner.table, column: column.ColumnName,
		}] = column
	}

	return shot, nil
}

// table returns what was known about a table, and marks it seen.
func (s *snapshot) table(schema, name string) (model.CatalogTable, bool) {
	key := tableKey{schema: schema, table: name}
	s.seenTables[key] = true

	stored, ok := s.tables[key]

	return stored, ok
}

// column returns what was known about a column, and marks it seen.
func (s *snapshot) column(schema, table, name string) (model.CatalogColumn, bool) {
	key := columnKey{schema: schema, table: table, column: name}
	s.seenColumns[key] = true

	stored, ok := s.columns[key]

	return stored, ok
}

/*
missing lists what was known, was not seen by this sync, and was not already
marked gone.

Already-gone objects are excluded because they were reported when they went.
A catalog that re-reported every long-dead table on every sync would make the
report useless within a week, which is how change detection becomes something
people filter out of their alerts.
*/
func (s *snapshot) missing() []Change {
	var gone []Change

	for key, table := range s.tables {
		if s.seenTables[key] || table.RemovedAt.Valid {
			continue
		}

		gone = append(gone, Change{Kind: Removed, Schema: key.schema, Table: key.table})
	}

	for key, column := range s.columns {
		if s.seenColumns[key] || column.RemovedAt.Valid {
			continue
		}

		gone = append(gone, Change{
			Kind: Removed, Schema: key.schema, Table: key.table, Column: key.column,
		})
	}

	// Sorted, because the two loops above walk maps and a report somebody
	// reads -- or a test that compares one -- cannot depend on Go's
	// randomized iteration order.
	sort.Slice(gone, func(i, j int) bool {
		a, b := gone[i], gone[j]
		if a.Schema != b.Schema {
			return a.Schema < b.Schema
		}

		if a.Table != b.Table {
			return a.Table < b.Table
		}

		return a.Column < b.Column
	})

	return gone
}
