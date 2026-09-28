/*
Package catalog keeps Pivot's record of what is in a connected database.

A sync reads a source's schema and reconciles it with what Pivot already knew.
The word doing the work is *reconciles*: the obvious implementation deletes
everything for a connection and inserts what it just read, and that is wrong in
three ways at once.

It destroys history. `first_seen_at`, the descriptions somebody wrote, and the
identity every Phase 3 model points at all go, and come back as different rows
with the same names.

It cannot answer the question worth asking. "What changed since yesterday" is
the reason to sync on a timer at all -- a column that changed type is a
dashboard about to be wrong -- and delete-then-insert knows nothing.

And it fails destructively. A source that answers with half its tables because
a permission was revoked, or because a migration is running, takes the other
half of the catalog with it.

So a sync upserts what it sees, sweeps what it did not, and reports the
difference.
*/
package catalog

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

// ChangeKind is what happened to an object between two syncs.
type ChangeKind string

const (
	// Added is an object Pivot had not seen before, or one that had been
	// marked gone and has come back.
	Added ChangeKind = "added"

	// Removed is an object the source no longer reports.
	Removed ChangeKind = "removed"

	// Changed is an object still there whose shape moved -- a column's type or
	// its nullability. The one that breaks things quietly.
	Changed ChangeKind = "changed"
)

// Change is one difference a sync found.
type Change struct {
	Kind   ChangeKind
	Schema string
	Table  string

	// Column is empty for a change to the table itself.
	Column string

	// Constraint names a relationship, for a change to one. Table then names
	// the table the key is declared on.
	Constraint string

	// Detail says what moved, for a log or an alert. Empty for an addition or
	// a removal, where the kind is the whole story.
	Detail string
}

// String renders a change for a log line or a CLI.
func (c Change) String() string {
	where := c.Schema + "." + c.Table

	switch {
	case c.Constraint != "":
		where += " (" + c.Constraint + ")"
	case c.Column != "":
		where += "." + c.Column
	}

	if c.Detail == "" {
		return string(c.Kind) + " " + where
	}

	return string(c.Kind) + " " + where + ": " + c.Detail
}

// Report is what a sync found.
type Report struct {
	// TablesSeen, ColumnsSeen and RelationshipsSeen are what the source
	// reported, whether or not anything about them moved.
	TablesSeen        int
	ColumnsSeen       int
	RelationshipsSeen int

	// ForeignKeysUnavailable says the source could not report relationships at
	// all -- which is a different fact from having none, and one Phase 3's
	// join inference has to be able to tell apart. Nothing was swept in that
	// case, because sweeping on the strength of not having asked would mark
	// every relationship dropped.
	ForeignKeysUnavailable bool

	// Changes is everything that differs from what Pivot knew, in the order
	// found: tables before their columns.
	Changes []Change

	StartedAt  time.Time
	FinishedAt time.Time
}

// Count returns how many changes of a kind the sync found.
func (r Report) Count(kind ChangeKind) int {
	n := 0

	for _, change := range r.Changes {
		if change.Kind == kind {
			n++
		}
	}

	return n
}

// Unchanged says whether the source looks exactly as it did last time.
func (r Report) Unchanged() bool { return len(r.Changes) == 0 }

// Summary is one line for a log.
func (r Report) Summary() string {
	relationships := fmt.Sprintf("%d relationships", r.RelationshipsSeen)
	if r.ForeignKeysUnavailable {
		relationships = "relationships not reported by this source"
	}

	return fmt.Sprintf(
		"%d tables, %d columns, %s, %d added, %d changed, %d removed in %s",
		r.TablesSeen, r.ColumnsSeen, relationships,
		r.Count(Added), r.Count(Changed), r.Count(Removed),
		r.FinishedAt.Sub(r.StartedAt).Round(time.Millisecond))
}

// Store is the part of the repository layer a sync needs. An interface so the
// diffing can be tested without a database, and so this package does not grow
// a dependency on everything a *repo.Repositories carries.
type Store interface {
	Tables(ctx context.Context, connectionID uuid.UUID) ([]model.CatalogTable, error)
	Columns(ctx context.Context, connectionID uuid.UUID) ([]model.CatalogColumn, error)
	RecordTable(ctx context.Context, in repo.SeenTable, at time.Time) (model.CatalogTable, error)
	RecordColumn(ctx context.Context, in repo.SeenColumn, at time.Time) (model.CatalogColumn, error)
	MarkGone(ctx context.Context, connectionID uuid.UUID, syncedAt time.Time) (int64, int64, error)

	ForeignKeys(ctx context.Context, connectionID uuid.UUID) ([]model.CatalogForeignKey, error)
	RecordForeignKey(ctx context.Context, in repo.SeenForeignKey, at time.Time) (model.CatalogForeignKey, error)
	MarkForeignKeysGone(ctx context.Context, connectionID uuid.UUID, syncedAt time.Time) (int64, error)
}

// Syncer reconciles one connection's catalog.
type Syncer struct {
	store Store

	// now is injectable so a test can make two syncs distinguishable without
	// sleeping. A sync's timestamp is load-bearing -- it is what the sweep
	// compares against -- so it cannot be left to whatever the clock does
	// between two statements.
	now func() time.Time
}

// NewSyncer builds a syncer over a store.
func NewSyncer(store Store) *Syncer {
	return &Syncer{store: store, now: time.Now}
}

// WithClock replaces the clock, for tests.
func (s *Syncer) WithClock(now func() time.Time) *Syncer {
	s.now = now

	return s
}

/*
Sync reconciles a connection's catalog with what the source reports now.

The source is read *first and completely*, before anything is written. That
ordering is the point: [connectors.Connector.Introspect] releases its pooled
connection when it returns, so every write below happens with nothing held
against somebody's warehouse. A sync that interleaved reads and writes would
hold a source connection for as long as Pivot's own database took, which on a
large schema is the difference between a second and a minute.
*/
func (s *Syncer) Sync(
	ctx context.Context, connectionID uuid.UUID, source connectors.Connector,
) (Report, error) {
	started := s.now()

	// What Pivot knew, read before the source so that a slow introspection
	// does not sit between the two halves of the comparison.
	previous, err := s.snapshot(ctx, connectionID)
	if err != nil {
		return Report{}, err
	}

	// The source, in full. Nothing is held after this line.
	tables, err := source.Introspect(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("introspect the source: %w", err)
	}

	report := Report{StartedAt: started}

	for _, table := range tables {
		if rerr := s.reconcile(ctx, connectionID, table, previous, &report, started); rerr != nil {
			return Report{}, rerr
		}
	}

	if rerr := s.reconcileRelationships(ctx, connectionID, source, &report, started); rerr != nil {
		return Report{}, rerr
	}

	// Everything not touched above. The counts come back from the database
	// rather than being inferred, and the names come from the snapshot -- a
	// count alone tells somebody a table went and not which one.
	goneTables, goneColumns, err := s.store.MarkGone(ctx, connectionID, started)
	if err != nil {
		return Report{}, fmt.Errorf("mark what the source no longer reports: %w", err)
	}

	// Computed once: it walks maps, and calling it twice would also mean two
	// different orderings of the same answer.
	gone := previous.missing()
	report.Changes = append(report.Changes, gone...)

	// A disagreement here means the sweep and the snapshot saw different
	// worlds, which is worth saying rather than hiding: it is the signature of
	// a concurrent sync on the same connection.
	if named := int64(countKind(gone, Removed)); named != goneTables+goneColumns {
		report.Changes = append(report.Changes, Change{
			Kind:   Changed,
			Detail: fmt.Sprintf("the sweep marked %d objects gone and %d were named; another sync may be running against this connection", goneTables+goneColumns, named),
		})
	}

	report.FinishedAt = s.now()

	return report, nil
}

// reconcile stores one table and its columns, recording what differs.
func (s *Syncer) reconcile(
	ctx context.Context,
	connectionID uuid.UUID,
	table connectors.Table,
	previous *snapshot,
	report *Report,
	at time.Time,
) error {
	report.TablesSeen++

	before, known := previous.table(table.Schema, table.Name)

	stored, err := s.store.RecordTable(ctx, repo.SeenTable{
		ConnectionID: connectionID,
		Schema:       table.Schema,
		Name:         table.Name,
		Type:         string(table.Type),
		Comment:      table.Comment,
	}, at)
	if err != nil {
		return fmt.Errorf("record %s.%s: %w", table.Schema, table.Name, err)
	}

	switch {
	case !known:
		report.Changes = append(report.Changes, Change{
			Kind: Added, Schema: table.Schema, Table: table.Name,
		})

	case before.RemovedAt.Valid:
		// It had been marked gone and is back. Reported as an addition,
		// because that is what it is to anything downstream -- and the row
		// keeps its original first_seen_at, so the history is intact.
		report.Changes = append(report.Changes, Change{
			Kind: Added, Schema: table.Schema, Table: table.Name,
			Detail: "was marked gone and has come back",
		})
	}

	for _, column := range table.Columns {
		report.ColumnsSeen++

		if err := s.reconcileColumn(ctx, stored.ID, table, column, previous, report, at); err != nil {
			return err
		}
	}

	return nil
}

func (s *Syncer) reconcileColumn(
	ctx context.Context,
	tableID uuid.UUID,
	table connectors.Table,
	column connectors.Column,
	previous *snapshot,
	report *Report,
	at time.Time,
) error {
	before, known := previous.column(table.Schema, table.Name, column.Name)

	if _, err := s.store.RecordColumn(ctx, repo.SeenColumn{
		TableID:       tableID,
		Name:          column.Name,
		SourceType:    column.SourceType,
		CanonicalType: string(column.Type.Kind),
		Nullable:      column.Nullable,
		Position:      int64(column.Position),
		Comment:       column.Comment,
	}, at); err != nil {
		return fmt.Errorf("record %s.%s.%s: %w", table.Schema, table.Name, column.Name, err)
	}

	change := Change{Schema: table.Schema, Table: table.Name, Column: column.Name}

	switch {
	case !known:
		change.Kind = Added

	case before.RemovedAt.Valid:
		change.Kind = Added
		change.Detail = "was marked gone and has come back"

	default:
		detail := describeColumnChange(before, column)
		if detail == "" {
			return nil
		}

		change.Kind = Changed
		change.Detail = detail
	}

	report.Changes = append(report.Changes, change)

	return nil
}

/*
describeColumnChange says what moved about a column, or "" if nothing did.

Type and nullability only. A comment or a position moving is not something any
consumer can be wrong about, and reporting it would bury the two that are:
a column whose type changed is a chart about to render nonsense, and one that
became nullable is an aggregate about to skip rows.

The canonical type is compared as well as the source spelling, because either
can move without the other. A source type of `varchar(50)` becoming
`varchar(100)` is worth saying and does not change the kind; a type Pivot could
not map becoming one it can changes the kind and not the source.
*/
func describeColumnChange(before model.CatalogColumn, now connectors.Column) string {
	var detail string

	if before.SourceType != now.SourceType {
		detail = fmt.Sprintf("type %s became %s", before.SourceType, now.SourceType)
	}

	if canonical := string(now.Type.Kind); before.CanonicalType != canonical {
		if detail != "" {
			detail += "; "
		}

		detail += fmt.Sprintf("Pivot reads it as %s where it read %s",
			canonical, before.CanonicalType)
	}

	if bool(before.IsNullable) != now.Nullable {
		if detail != "" {
			detail += "; "
		}

		if now.Nullable {
			detail += "became nullable"
		} else {
			detail += "became NOT NULL"
		}
	}

	return detail
}

func countKind(changes []Change, kind ChangeKind) int {
	n := 0

	for _, change := range changes {
		if change.Kind == kind {
			n++
		}
	}

	return n
}
