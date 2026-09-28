package catalog_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/catalog"
	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

/*
Relationships through the sync.

The source is the same real SQLite file the rest of these tests use, so the
foreign keys are ones SQLite actually reported rather than ones a fake made up
-- which matters here more than anywhere, because the shape of a composite key
is exactly what every source gets to describe differently.
*/

const (
	createParent = `CREATE TABLE regions (
		tenant_id INTEGER NOT NULL,
		code      INTEGER NOT NULL,
		PRIMARY KEY (tenant_id, code)
	)`

	createChild = `CREATE TABLE stores (
		id        INTEGER NOT NULL PRIMARY KEY,
		tenant_id INTEGER NOT NULL,
		code      INTEGER NOT NULL,
		FOREIGN KEY (tenant_id, code) REFERENCES regions (tenant_id, code)
	)`
)

// A composite relationship is reported once, not once per column.
func TestACompositeRelationshipIsReportedOnce(t *testing.T) {
	t.Parallel()

	src := newSource(t, createParent, createChild)
	store := newFakeStore()

	report, err := syncOnce(t, store, src, 1)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if report.RelationshipsSeen != 1 {
		t.Errorf("saw %d relationships, want 1", report.RelationshipsSeen)
	}

	var added []catalog.Change

	for _, change := range report.Changes {
		if change.Constraint != "" && change.Kind == catalog.Added {
			added = append(added, change)
		}
	}

	if len(added) != 1 {
		t.Fatalf("a two-column key was reported %d times, want once:\n%s",
			len(added), changes(report))
	}

	if added[0].Table != "stores" {
		t.Errorf("the relationship is reported on %q, want stores", added[0].Table)
	}

	// And both of its columns were stored, in order.
	if store.recordedKeys != 2 {
		t.Errorf("stored %d key columns, want 2", store.recordedKeys)
	}

	stored, _ := store.ForeignKeys(t.Context(), uuid.Nil)
	byOrdinal := map[int64]model.CatalogForeignKey{}

	for _, key := range stored {
		byOrdinal[key.Ordinal] = key
	}

	for ordinal, want := range map[int64]string{1: "tenant_id", 2: "code"} {
		got, ok := byOrdinal[ordinal]
		if !ok {
			t.Fatalf("no key column at ordinal %d", ordinal)
		}

		if got.FromColumn != want || got.ToColumn != want {
			t.Errorf("ordinal %d pairs %s -> %s, want %s -> %s",
				ordinal, got.FromColumn, got.ToColumn, want, want)
		}
	}
}

// An unchanged source reports no relationship change, like everything else.
func TestAnUnchangedRelationshipIsSilent(t *testing.T) {
	t.Parallel()

	src := newSource(t, createParent, createChild)
	store := newFakeStore()

	if _, err := syncOnce(t, store, src, 1); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	report, err := syncOnce(t, store, src, 2)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if !report.Unchanged() {
		t.Errorf("an unchanged source reported changes:\n%s", changes(report))
	}

	if report.RelationshipsSeen != 1 {
		t.Errorf("saw %d relationships, want 1", report.RelationshipsSeen)
	}
}

/*
A dropped constraint is marked gone and reported once, like a dropped table.

Checked by rebuilding the child table without the key, because SQLite cannot
drop a constraint -- which is also what a real migration does.
*/
func TestADroppedRelationshipIsReportedOnce(t *testing.T) {
	t.Parallel()

	src := newSource(t, createParent, createChild)
	store := newFakeStore()

	if _, err := syncOnce(t, store, src, 1); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	src.apply(
		"DROP TABLE stores",
		`CREATE TABLE stores (
			id        INTEGER NOT NULL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			code      INTEGER NOT NULL
		)`,
	)

	report, err := syncOnce(t, store, src, 2)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	var removed int

	for _, change := range report.Changes {
		if change.Constraint != "" && change.Kind == catalog.Removed {
			removed++
		}
	}

	if removed != 1 {
		t.Fatalf("a dropped constraint was reported %d times, want once:\n%s",
			removed, changes(report))
	}

	// Marked, not deleted.
	stored, _ := store.ForeignKeys(t.Context(), uuid.Nil)
	if len(stored) != 2 {
		t.Errorf("%d key columns stored, want both kept", len(stored))
	}

	for _, key := range stored {
		if !key.RemovedAt.Valid {
			t.Errorf("%s ordinal %d is not marked gone", key.ConstraintName, key.Ordinal)
		}
	}

	// And not reported again.
	third, err := syncOnce(t, store, src, 3)
	if err != nil {
		t.Fatalf("third sync: %v", err)
	}

	for _, change := range third.Changes {
		if change.Constraint != "" {
			t.Errorf("an old removal was re-reported:\n%s", changes(third))
		}
	}
}

/*
A source that cannot report relationships does not have them swept.

The distinction [connectors.ErrNoForeignKeys] exists for. Sweeping on the
strength of not having asked would mark every stored relationship dropped the
first time a source went quiet -- a schema's structure deleted because a
connector lacks a feature.
*/
func TestASourceThatCannotReportRelationshipsSweepsNothing(t *testing.T) {
	t.Parallel()

	src := newSource(t, createParent, createChild)
	store := newFakeStore()

	// One real sync, so there is something to lose.
	if _, err := syncOnce(t, store, src, 1); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	before, _ := store.ForeignKeys(t.Context(), uuid.Nil)
	if len(before) != 2 {
		t.Fatalf("%d key columns before, want 2", len(before))
	}

	// The same source, through a connector that will not answer.
	silent := silentAboutKeys{Connector: src.connector()}

	at := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)

	report, err := catalog.NewSyncer(store).
		WithClock(func() time.Time { return at }).
		Sync(t.Context(), uuid.New(), silent)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if !report.ForeignKeysUnavailable {
		t.Error("the report does not say relationships were unavailable")
	}

	after, _ := store.ForeignKeys(t.Context(), uuid.Nil)
	for _, key := range after {
		if key.RemovedAt.Valid {
			t.Errorf("%s ordinal %d was marked gone because nobody asked",
				key.ConstraintName, key.Ordinal)
		}
	}

	// And the summary says so rather than reporting zero.
	if !strings.Contains(report.Summary(), "not reported by this source") {
		t.Errorf("summary = %q, want it to distinguish 'cannot say' from 'none'",
			report.Summary())
	}
}

// silentAboutKeys is a connector that reports everything except relationships.
type silentAboutKeys struct {
	connectors.Connector
}

func (silentAboutKeys) ForeignKeys(context.Context) ([]connectors.ForeignKey, error) {
	return nil, connectors.ErrNoForeignKeys
}

/*
A relationship pointing at a table Pivot has not cataloged is stored anyway.

A schema granted piecemeal is the ordinary reason, and dropping the key for
tidiness would throw away the only record that the relationship exists. Phase
3 can see that one end is missing with a join; it cannot recover a row nobody
wrote.
*/
func TestARelationshipToAnUncatalogedTableIsStored(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	src := newSource(t, createParent, createChild)

	dangling := danglingKeys{Connector: src.connector()}

	at := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)

	report, err := catalog.NewSyncer(store).
		WithClock(func() time.Time { return at }).
		Sync(t.Context(), uuid.New(), dangling)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if report.RelationshipsSeen != 1 {
		t.Errorf("saw %d relationships, want 1", report.RelationshipsSeen)
	}

	stored, _ := store.ForeignKeys(t.Context(), uuid.Nil)
	if len(stored) != 1 {
		t.Fatalf("%d key columns stored, want 1", len(stored))
	}

	if stored[0].ToTable != "somewhere_else" {
		t.Errorf("target = %q, want the uncataloged table kept as named",
			stored[0].ToTable)
	}
}

// danglingKeys reports a relationship whose target is not in the catalog.
type danglingKeys struct {
	connectors.Connector
}

func (danglingKeys) ForeignKeys(context.Context) ([]connectors.ForeignKey, error) {
	return []connectors.ForeignKey{{
		Name:       "stores_elsewhere_fk",
		FromSchema: "main", FromTable: "stores", FromColumn: "tenant_id",
		ToSchema: "other", ToTable: "somewhere_else", ToColumn: "id",
		Ordinal: 1,
	}}, nil
}

// The store interface is satisfied by the repository, which is what the CLI
// hands the syncer -- checked at compile time so a signature drift is a build
// failure rather than a runtime surprise.
var _ catalog.Store = (*repo.CatalogRepo)(nil)
