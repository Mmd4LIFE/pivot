package repo

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

/*
CatalogRepo stores what Pivot has seen in a connected database.

Storage only. It knows nothing about connectors, introspection or diffing --
[catalog.Sync] owns that, one layer up, because a repository that reached out
to somebody's warehouse would be a repository nothing could test without one.

The shape it does impose is the one that makes a sync a comparison: every
object is upserted with the timestamp of the sync that saw it, and afterwards a
single sweep marks everything older than that sync as gone. Two statements per
object and one at the end, with no temporary table and no transaction held open
across a slow source.
*/
type CatalogRepo struct {
	base
}

// SeenTable is a table a sync found.
type SeenTable struct {
	ConnectionID uuid.UUID
	Schema       string
	Name         string
	Type         string
	Comment      string
}

// SeenColumn is a column a sync found.
type SeenColumn struct {
	TableID       uuid.UUID
	Name          string
	SourceType    string
	CanonicalType string
	Nullable      bool
	Position      int64
	Comment       string
}

/*
RecordTable stores a table a sync saw, at the sync's timestamp.

`at` is the sync's own start time rather than now(). Every object in one sync
has to carry the same timestamp or the sweep cannot tell "not seen this time"
from "seen a few milliseconds earlier": a sync over ten thousand tables takes
long enough for now() to move underneath it.
*/
func (r *CatalogRepo) RecordTable(
	ctx context.Context, in SeenTable, at time.Time,
) (model.CatalogTable, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.CatalogTable{}, err
	}

	stamp := dbtypes.NewTime(at)

	table, err := r.q.UpsertCatalogTable(ctx, model.UpsertCatalogTableParams{
		ID:           newID(),
		OrgID:        s.OrgID(),
		ConnectionID: in.ConnectionID,
		SchemaName:   in.Schema,
		TableName:    in.Name,
		TableType:    in.Type,
		Comment:      in.Comment,
		FirstSeenAt:  stamp,
		LastSeenAt:   stamp,
	})
	if err != nil {
		return model.CatalogTable{}, translate(err)
	}

	return table, nil
}

// RecordColumn stores a column a sync saw, at the sync's timestamp.
func (r *CatalogRepo) RecordColumn(
	ctx context.Context, in SeenColumn, at time.Time,
) (model.CatalogColumn, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.CatalogColumn{}, err
	}

	stamp := dbtypes.NewTime(at)

	column, err := r.q.UpsertCatalogColumn(ctx, model.UpsertCatalogColumnParams{
		ID:            newID(),
		OrgID:         s.OrgID(),
		TableID:       in.TableID,
		ColumnName:    in.Name,
		SourceType:    in.SourceType,
		CanonicalType: in.CanonicalType,
		IsNullable:    dbtypes.Bool(in.Nullable),
		Position:      in.Position,
		Comment:       in.Comment,
		FirstSeenAt:   stamp,
		LastSeenAt:    stamp,
	})
	if err != nil {
		return model.CatalogColumn{}, translate(err)
	}

	return column, nil
}

/*
MarkGone flags everything this sync did not see, and says how much.

Marked rather than deleted. A table disappears for reasons that are not
"somebody dropped it" -- a permissions change, a migration caught mid-flight,
a replica that had not caught up -- and a delete would take the descriptions
and the models pointing at it along with it. It also destroys the answer to
"when did this go", which is the question somebody asks when a dashboard
breaks.

Columns are swept before tables so that a table going away still records which
of its columns went with it.
*/
func (r *CatalogRepo) MarkGone(
	ctx context.Context, connectionID uuid.UUID, syncedAt time.Time,
) (tables, columns int64, err error) {
	s, serr := r.scope(ctx)
	if serr != nil {
		return 0, 0, serr
	}

	stamp := dbtypes.NewTime(syncedAt)
	removed := dbtypes.NewNullTime(syncedAt)

	columns, err = r.q.SweepCatalogColumns(ctx, model.SweepCatalogColumnsParams{
		RemovedAt:    removed,
		UpdatedAt:    stamp,
		OrgID:        s.OrgID(),
		LastSeenAt:   stamp,
		ConnectionID: connectionID,
	})
	if err != nil {
		return 0, 0, translate(err)
	}

	tables, err = r.q.SweepCatalogTables(ctx, model.SweepCatalogTablesParams{
		RemovedAt:    removed,
		UpdatedAt:    stamp,
		ConnectionID: connectionID,
		OrgID:        s.OrgID(),
		LastSeenAt:   stamp,
	})
	if err != nil {
		return 0, 0, translate(err)
	}

	return tables, columns, nil
}

// Tables returns every table cataloged for a connection, including the ones
// marked gone -- a caller wanting only the living ones filters on RemovedAt,
// and a caller asking "what happened to it" needs the rest.
func (r *CatalogRepo) Tables(
	ctx context.Context, connectionID uuid.UUID,
) ([]model.CatalogTable, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	tables, err := r.q.ListCatalogTables(ctx, model.ListCatalogTablesParams{
		ConnectionID: connectionID, OrgID: s.OrgID(),
	})

	return tables, translate(err)
}

// Columns returns every column cataloged for a connection, in table and
// position order.
func (r *CatalogRepo) Columns(
	ctx context.Context, connectionID uuid.UUID,
) ([]model.CatalogColumn, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	columns, err := r.q.ListCatalogColumns(ctx, model.ListCatalogColumnsParams{
		ConnectionID: connectionID, OrgID: s.OrgID(),
	})

	return columns, translate(err)
}

// ColumnsOf returns one table's columns.
func (r *CatalogRepo) ColumnsOf(
	ctx context.Context, tableID uuid.UUID,
) ([]model.CatalogColumn, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	columns, err := r.q.ListCatalogColumnsForTable(ctx, model.ListCatalogColumnsForTableParams{
		TableID: tableID, OrgID: s.OrgID(),
	})

	return columns, translate(err)
}

// Table returns one cataloged table by where it lives.
func (r *CatalogRepo) Table(
	ctx context.Context, connectionID uuid.UUID, schema, name string,
) (model.CatalogTable, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.CatalogTable{}, err
	}

	table, err := r.q.GetCatalogTable(ctx, model.GetCatalogTableParams{
		ConnectionID: connectionID, OrgID: s.OrgID(),
		SchemaName: schema, TableName: name,
	})
	if err != nil {
		return model.CatalogTable{}, translate(err)
	}

	return table, nil
}
