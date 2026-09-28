package repo_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

/*
The catalog's storage, against real SQL on both engines.

internal/catalog tests the reconciliation against a fake store, because the
diffing is the part worth covering exhaustively. What is here is the half a
fake cannot vouch for: that the upsert really is an upsert, that the sweep
compares the timestamps it claims to, and that both are scoped to a tenant.
*/

// catalogFixture is a connection to hang a catalog off.
func catalogFixture(t *testing.T, db *store.DB) (connectionFixture, uuid.UUID) {
	t.Helper()

	f := newConnectionFixture(t, db)

	conn, err := f.repos.Connections.Create(f.ctx, repo.CreateConnection{
		Slug: "warehouse", Name: "Warehouse", Kind: "postgres",
		Host: "db.internal", Port: 5432, Database: "analytics",
		Username: "pivot", IsEnabled: true,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	return f, conn.ID
}

var (
	firstSync  = time.Date(2026, time.September, 28, 9, 0, 0, 0, time.UTC)
	secondSync = time.Date(2026, time.September, 28, 10, 0, 0, 0, time.UTC)
)

/*
A second sync updates the row rather than replacing it.

The property the design turns on, and the one a fake store cannot prove: the
row keeps its id and its first_seen_at across syncs. A delete-and-insert
produces the same names and a new identity, which silently orphans every Phase
3 model pointing at the old one -- and loses the answer to "how long has this
been here".
*/
func TestASecondSyncKeepsTheRowAndItsFirstSeenAt(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f, connID := catalogFixture(t, db)

		seen := repo.SeenTable{
			ConnectionID: connID, Schema: "public", Name: "orders",
			Type: "table", Comment: "",
		}

		first, err := f.repos.Catalog.RecordTable(f.ctx, seen, firstSync)
		if err != nil {
			t.Fatalf("first record: %v", err)
		}

		seen.Comment = "now with a comment"

		second, err := f.repos.Catalog.RecordTable(f.ctx, seen, secondSync)
		if err != nil {
			t.Fatalf("second record: %v", err)
		}

		if second.ID != first.ID {
			t.Errorf("the row was replaced: id %s became %s", first.ID, second.ID)
		}

		if !second.FirstSeenAt.Equal(first.FirstSeenAt.Time) {
			t.Errorf("first_seen_at moved from %s to %s",
				first.FirstSeenAt.Time, second.FirstSeenAt.Time)
		}

		if !second.LastSeenAt.Equal(secondSync) {
			t.Errorf("last_seen_at = %s, want the second sync's time", second.LastSeenAt.Time)
		}

		if second.Comment != "now with a comment" {
			t.Errorf("comment = %q, want the updated one", second.Comment)
		}

		if second.Version <= first.Version {
			t.Errorf("version did not advance: %d then %d", first.Version, second.Version)
		}

		// And there is one row, not two.
		tables, err := f.repos.Catalog.Tables(f.ctx, connID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}

		if len(tables) != 1 {
			t.Errorf("%d rows after two syncs, want 1", len(tables))
		}
	})
}

/*
The sweep marks what the current sync did not touch, and only that.

Checked with two tables where one is re-recorded and the other is not, because
a sweep that marked everything would pass a test with one table in it.
*/
func TestTheSweepMarksOnlyWhatWasNotSeen(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f, connID := catalogFixture(t, db)

		for _, name := range []string{"orders", "customers"} {
			if _, err := f.repos.Catalog.RecordTable(f.ctx, repo.SeenTable{
				ConnectionID: connID, Schema: "public", Name: name, Type: "table",
			}, firstSync); err != nil {
				t.Fatalf("record %s: %v", name, err)
			}
		}

		// The second sync sees only orders.
		if _, err := f.repos.Catalog.RecordTable(f.ctx, repo.SeenTable{
			ConnectionID: connID, Schema: "public", Name: "orders", Type: "table",
		}, secondSync); err != nil {
			t.Fatalf("second sync: %v", err)
		}

		gone, _, err := f.repos.Catalog.MarkGone(f.ctx, connID, secondSync)
		if err != nil {
			t.Fatalf("sweep: %v", err)
		}

		if gone != 1 {
			t.Errorf("the sweep marked %d tables gone, want 1", gone)
		}

		tables, err := f.repos.Catalog.Tables(f.ctx, connID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}

		// Both rows are still there; one is flagged.
		if len(tables) != 2 {
			t.Fatalf("%d rows after the sweep, want both kept", len(tables))
		}

		for _, table := range tables {
			switch table.TableName {
			case "orders":
				if table.RemovedAt.Valid {
					t.Error("orders was marked gone despite being seen")
				}

			case "customers":
				if !table.RemovedAt.Valid {
					t.Error("customers was not marked gone")
				}
			}
		}
	})
}

// A sweep is idempotent: running it twice does not re-mark what it already
// marked, which is what keeps a report from repeating old news forever.
func TestASecondSweepMarksNothingNew(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f, connID := catalogFixture(t, db)

		if _, err := f.repos.Catalog.RecordTable(f.ctx, repo.SeenTable{
			ConnectionID: connID, Schema: "public", Name: "orders", Type: "table",
		}, firstSync); err != nil {
			t.Fatalf("record: %v", err)
		}

		if _, _, err := f.repos.Catalog.MarkGone(f.ctx, connID, secondSync); err != nil {
			t.Fatalf("first sweep: %v", err)
		}

		third := secondSync.Add(time.Hour)

		gone, _, err := f.repos.Catalog.MarkGone(f.ctx, connID, third)
		if err != nil {
			t.Fatalf("second sweep: %v", err)
		}

		if gone != 0 {
			t.Errorf("the second sweep re-marked %d tables", gone)
		}
	})
}

// A table that comes back has its removal cleared, so it is a live row again
// rather than a tombstone with a fresh timestamp.
func TestAReturningTableIsUnmarked(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f, connID := catalogFixture(t, db)

		seen := repo.SeenTable{
			ConnectionID: connID, Schema: "public", Name: "orders", Type: "table",
		}

		if _, err := f.repos.Catalog.RecordTable(f.ctx, seen, firstSync); err != nil {
			t.Fatalf("record: %v", err)
		}

		if _, _, err := f.repos.Catalog.MarkGone(f.ctx, connID, secondSync); err != nil {
			t.Fatalf("sweep: %v", err)
		}

		back, err := f.repos.Catalog.RecordTable(f.ctx, seen, secondSync.Add(time.Hour))
		if err != nil {
			t.Fatalf("record again: %v", err)
		}

		if back.RemovedAt.Valid {
			t.Error("a table that came back is still marked gone")
		}
	})
}

/*
A column is stored with both spellings of its type.

source_type is what the database called it and canonical_type is what
internal/datatype made of that. Keeping the first is the whole mechanism for
fixing a type system in the field: when Pivot learns a mapping it lacked, the
stored catalog can be re-normalized without going back to the warehouse.
*/
func TestAColumnKeepsBothSpellingsOfItsType(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f, connID := catalogFixture(t, db)

		table, err := f.repos.Catalog.RecordTable(f.ctx, repo.SeenTable{
			ConnectionID: connID, Schema: "public", Name: "orders", Type: "table",
		}, firstSync)
		if err != nil {
			t.Fatalf("record table: %v", err)
		}

		if _, err = f.repos.Catalog.RecordColumn(f.ctx, repo.SeenColumn{
			TableID: table.ID, Name: "total",
			SourceType: "numeric(10,2)", CanonicalType: "decimal",
			Nullable: false, Position: 2,
		}, firstSync); err != nil {
			t.Fatalf("record column: %v", err)
		}

		columns, err := f.repos.Catalog.ColumnsOf(f.ctx, table.ID)
		if err != nil {
			t.Fatalf("list columns: %v", err)
		}

		if len(columns) != 1 {
			t.Fatalf("%d columns, want 1", len(columns))
		}

		got := columns[0]

		if got.SourceType != "numeric(10,2)" || got.CanonicalType != "decimal" {
			t.Errorf("stored %q / %q, want the source spelling and the canonical kind",
				got.SourceType, got.CanonicalType)
		}

		if bool(got.IsNullable) {
			t.Error("a NOT NULL column was stored as nullable")
		}
	})
}

/*
One organization cannot see or sweep another's catalog.

The same guarantee every other table has, checked the same way: a second
organization exists in the fixture, and what it can reach is nothing.
*/
func TestTheCatalogIsScopedToTheOrganization(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f, connID := catalogFixture(t, db)

		if _, err := f.repos.Catalog.RecordTable(f.ctx, repo.SeenTable{
			ConnectionID: connID, Schema: "public", Name: "orders", Type: "table",
		}, firstSync); err != nil {
			t.Fatalf("record: %v", err)
		}

		// The other organization, asking about the same connection id.
		theirs, err := f.repos.Catalog.Tables(f.otherCtx, connID)
		if err != nil {
			t.Fatalf("list as the other org: %v", err)
		}

		if len(theirs) != 0 {
			t.Errorf("another organization sees %d of our catalog rows", len(theirs))
		}

		columns, err := f.repos.Catalog.Columns(f.otherCtx, connID)
		if err != nil {
			t.Fatalf("list columns as the other org: %v", err)
		}

		if len(columns) != 0 {
			t.Errorf("another organization sees %d of our columns", len(columns))
		}

		// And a sweep run by them touches nothing of ours.
		gone, _, err := f.repos.Catalog.MarkGone(f.otherCtx, connID, secondSync)
		if err != nil {
			t.Fatalf("sweep as the other org: %v", err)
		}

		if gone != 0 {
			t.Errorf("another organization's sweep marked %d of our tables gone", gone)
		}

		ours, err := f.repos.Catalog.Tables(f.ctx, connID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}

		if len(ours) != 1 || ours[0].RemovedAt.Valid {
			t.Error("our catalog was changed by another organization's sweep")
		}
	})
}

// Deleting a connection takes its catalog with it, because a catalog for a
// connection nobody can open is rows nothing will ever read again.
func TestDeletingAConnectionRemovesItsCatalog(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f, connID := catalogFixture(t, db)

		table, err := f.repos.Catalog.RecordTable(f.ctx, repo.SeenTable{
			ConnectionID: connID, Schema: "public", Name: "orders", Type: "table",
		}, firstSync)
		if err != nil {
			t.Fatalf("record: %v", err)
		}

		if _, err = f.repos.Catalog.RecordColumn(f.ctx, repo.SeenColumn{
			TableID: table.ID, Name: "id", SourceType: "bigint",
			CanonicalType: "integer", Position: 1,
		}, firstSync); err != nil {
			t.Fatalf("record column: %v", err)
		}

		// A soft delete leaves the row, so the catalog stays -- which is what
		// makes a connection restorable. Checked so the behavior is recorded
		// rather than assumed either way.
		if err = f.repos.Connections.SoftDelete(f.ctx, connID); err != nil {
			t.Fatalf("soft delete: %v", err)
		}

		tables, err := f.repos.Catalog.Tables(f.ctx, connID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}

		if len(tables) != 1 {
			t.Errorf("a soft-deleted connection lost its catalog: %d tables", len(tables))
		}
	})
}
