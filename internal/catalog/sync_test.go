package catalog_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/Mmd4LIFE/pivot/internal/catalog"
	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

/*
The sync, against a real source.

The source is a SQLite file, because it needs no container and no environment
variable -- so this runs on a bare `go test ./...` rather than only when
somebody remembers to start a database. What is being tested is the
reconciliation, and a source Pivot can change between two syncs is worth more
here than a bigger one it cannot.

The store is a fake. That is deliberate the other way round: the diffing is the
part worth testing exhaustively, and doing it against a real database would
mean a migration and an organization per case for no additional confidence.
The repository's own storage is covered by its own tests, against real SQL on
both engines -- including the one that matters most here, that an upsert keeps
the row's identity and its first_seen_at.
*/

// --- a source that can be changed between syncs -----------------------------

type source struct {
	t    *testing.T
	path string
}

func newSource(t *testing.T, ddl ...string) *source {
	t.Helper()

	s := &source{t: t, path: filepath.Join(t.TempDir(), "source.db")}
	s.apply(ddl...)

	return s
}

// apply runs DDL through a read-write handle, which the connector will not
// give us: it opens every file read-only.
func (s *source) apply(statements ...string) {
	s.t.Helper()

	db, err := sql.Open("sqlite", "file:"+s.path)
	if err != nil {
		s.t.Fatalf("open the source: %v", err)
	}

	defer func() { _ = db.Close() }()

	for _, statement := range statements {
		if _, err := db.ExecContext(s.t.Context(), statement); err != nil {
			s.t.Fatalf("%s: %v", statement, err)
		}
	}
}

func (s *source) connector() connectors.Connector {
	s.t.Helper()

	c, err := connectors.Open(connectors.Config{
		Kind: connectors.KindSQLite, Database: s.path,
	})
	if err != nil {
		s.t.Fatalf("open the connector: %v", err)
	}

	s.t.Cleanup(func() { _ = c.Close() })

	return c
}

// --- a store that records what it was told ----------------------------------

type fakeStore struct {
	tables  map[string]model.CatalogTable
	columns map[uuid.UUID]map[string]model.CatalogColumn

	// syncedAt is the timestamp of the most recent sweep, which is what makes
	// "not seen this time" computable.
	swept time.Time

	recordedTables  int
	recordedColumns int
	recordedKeys    int

	foreignKeys map[string]model.CatalogForeignKey
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		tables:      map[string]model.CatalogTable{},
		columns:     map[uuid.UUID]map[string]model.CatalogColumn{},
		foreignKeys: map[string]model.CatalogForeignKey{},
	}
}

func (f *fakeStore) Tables(context.Context, uuid.UUID) ([]model.CatalogTable, error) {
	out := make([]model.CatalogTable, 0, len(f.tables))
	for _, t := range f.tables {
		out = append(out, t)
	}

	return out, nil
}

func (f *fakeStore) Columns(context.Context, uuid.UUID) ([]model.CatalogColumn, error) {
	var out []model.CatalogColumn

	for _, byName := range f.columns {
		for _, c := range byName {
			out = append(out, c)
		}
	}

	return out, nil
}

func (f *fakeStore) RecordTable(
	_ context.Context, in repo.SeenTable, at time.Time,
) (model.CatalogTable, error) {
	f.recordedTables++

	key := in.Schema + "." + in.Name

	stored, ok := f.tables[key]
	if !ok {
		stored = model.CatalogTable{ID: uuid.New(), SchemaName: in.Schema, TableName: in.Name}
	}

	stored.TableType = in.Type
	stored.Comment = in.Comment
	stored.LastSeenAt = dbTime(at)
	stored.RemovedAt = model.CatalogTable{}.RemovedAt // cleared, as the upsert does

	f.tables[key] = stored

	return stored, nil
}

func (f *fakeStore) RecordColumn(
	_ context.Context, in repo.SeenColumn, at time.Time,
) (model.CatalogColumn, error) {
	f.recordedColumns++

	byName, ok := f.columns[in.TableID]
	if !ok {
		byName = map[string]model.CatalogColumn{}
		f.columns[in.TableID] = byName
	}

	stored := byName[in.Name]
	if stored.ID == uuid.Nil {
		stored.ID = uuid.New()
	}

	stored.TableID = in.TableID
	stored.ColumnName = in.Name
	stored.SourceType = in.SourceType
	stored.CanonicalType = in.CanonicalType
	stored.IsNullable = boolOf(in.Nullable)
	stored.Position = in.Position
	stored.LastSeenAt = dbTime(at)
	stored.RemovedAt = model.CatalogColumn{}.RemovedAt

	byName[in.Name] = stored

	return stored, nil
}

// MarkGone flags what the sync did not touch, exactly as the SQL sweep does.
func (f *fakeStore) MarkGone(
	_ context.Context, _ uuid.UUID, syncedAt time.Time,
) (int64, int64, error) {
	f.swept = syncedAt

	var tables, columns int64

	for key, t := range f.tables {
		if !t.RemovedAt.Valid && t.LastSeenAt.Before(syncedAt) {
			t.RemovedAt = nullTime(syncedAt)
			f.tables[key] = t
			tables++
		}
	}

	for id, byName := range f.columns {
		for name, c := range byName {
			if !c.RemovedAt.Valid && c.LastSeenAt.Before(syncedAt) {
				c.RemovedAt = nullTime(syncedAt)
				byName[name] = c
				columns++
			}
		}

		f.columns[id] = byName
	}

	return tables, columns, nil
}

// Relationships, stored the same way: one row per column of each key.
func (f *fakeStore) ForeignKeys(context.Context, uuid.UUID) ([]model.CatalogForeignKey, error) {
	out := make([]model.CatalogForeignKey, 0, len(f.foreignKeys))
	for _, k := range f.foreignKeys {
		out = append(out, k)
	}

	return out, nil
}

func (f *fakeStore) RecordForeignKey(
	_ context.Context, in repo.SeenForeignKey, at time.Time,
) (model.CatalogForeignKey, error) {
	f.recordedKeys++

	key := fmt.Sprintf("%s.%s.%s.%d", in.FromSchema, in.FromTable, in.Constraint, in.Ordinal)

	stored := f.foreignKeys[key]
	if stored.ID == uuid.Nil {
		stored.ID = uuid.New()
	}

	stored.ConstraintName = in.Constraint
	stored.FromSchema, stored.FromTable, stored.FromColumn = in.FromSchema, in.FromTable, in.FromColumn
	stored.ToSchema, stored.ToTable, stored.ToColumn = in.ToSchema, in.ToTable, in.ToColumn
	stored.Ordinal = in.Ordinal
	stored.LastSeenAt = dbTime(at)
	stored.RemovedAt = model.CatalogForeignKey{}.RemovedAt

	f.foreignKeys[key] = stored

	return stored, nil
}

func (f *fakeStore) MarkForeignKeysGone(
	_ context.Context, _ uuid.UUID, syncedAt time.Time,
) (int64, error) {
	var gone int64

	for key, k := range f.foreignKeys {
		if !k.RemovedAt.Valid && k.LastSeenAt.Before(syncedAt) {
			k.RemovedAt = nullTime(syncedAt)
			f.foreignKeys[key] = k
			gone++
		}
	}

	return gone, nil
}

// --- the tests ---------------------------------------------------------------

const (
	createOrders = `CREATE TABLE orders (
		id INTEGER NOT NULL PRIMARY KEY,
		total REAL NOT NULL,
		note TEXT
	)`
	createCustomers = `CREATE TABLE customers (id INTEGER NOT NULL PRIMARY KEY, email TEXT NOT NULL)`
)

// A first sync is all additions, and everything the source has is recorded.
func TestAFirstSyncIsAllAdditions(t *testing.T) {
	t.Parallel()

	src := newSource(t, createOrders, createCustomers)
	store := newFakeStore()

	report, err := syncOnce(t, store, src, 1)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if report.TablesSeen != 2 || report.ColumnsSeen != 5 {
		t.Errorf("saw %d tables and %d columns, want 2 and 5",
			report.TablesSeen, report.ColumnsSeen)
	}

	if got := report.Count(catalog.Added); got != 7 {
		t.Errorf("%d additions, want 7 (two tables and five columns):\n%s",
			got, changes(report))
	}

	if report.Count(catalog.Removed) != 0 || report.Count(catalog.Changed) != 0 {
		t.Errorf("a first sync reported removals or changes:\n%s", changes(report))
	}
}

/*
A second sync over an unchanged source reports nothing.

The property that makes change detection worth having. A sync that reported
every table every time would be a sync nobody reads, and "nothing changed" has
to be distinguishable from "nobody looked".
*/
func TestASyncOverAnUnchangedSourceIsSilent(t *testing.T) {
	t.Parallel()

	src := newSource(t, createOrders)
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

	// And it still saw everything -- silence is not the sync having skipped.
	if report.TablesSeen != 1 || report.ColumnsSeen != 3 {
		t.Errorf("saw %d tables and %d columns, want 1 and 3",
			report.TablesSeen, report.ColumnsSeen)
	}
}

// A column added between syncs is reported as one, and nothing else is.
func TestANewColumnIsReportedAlone(t *testing.T) {
	t.Parallel()

	src := newSource(t, createOrders)
	store := newFakeStore()

	if _, err := syncOnce(t, store, src, 1); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	src.apply("ALTER TABLE orders ADD COLUMN shipped_at TIMESTAMPTZ")

	report, err := syncOnce(t, store, src, 2)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if len(report.Changes) != 1 {
		t.Fatalf("want exactly one change:\n%s", changes(report))
	}

	got := report.Changes[0]
	if got.Kind != catalog.Added || got.Column != "shipped_at" {
		t.Errorf("change = %s, want the new column added", got)
	}
}

/*
A column whose type moves is reported as changed, with what moved.

The one that breaks things quietly: a chart built on a number keeps rendering
after the column becomes text, and renders nonsense. So the detail has to name
both the old spelling and the new, and say what Pivot now reads it as.
*/
func TestAChangedColumnTypeSaysWhatMoved(t *testing.T) {
	t.Parallel()

	src := newSource(t, createOrders)
	store := newFakeStore()

	if _, err := syncOnce(t, store, src, 1); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// SQLite cannot alter a column's type, so the table is rebuilt -- which is
	// what a real migration does too.
	src.apply(
		"DROP TABLE orders",
		`CREATE TABLE orders (
			id INTEGER NOT NULL PRIMARY KEY,
			total TEXT NOT NULL,
			note TEXT
		)`,
	)

	report, err := syncOnce(t, store, src, 2)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	var found *catalog.Change

	for i := range report.Changes {
		if report.Changes[i].Column == "total" {
			found = &report.Changes[i]
		}
	}

	if found == nil {
		t.Fatalf("the type change was not reported:\n%s", changes(report))
	}

	if found.Kind != catalog.Changed {
		t.Errorf("total is %q, want %q", found.Kind, catalog.Changed)
	}

	for _, want := range []string{"REAL", "TEXT", "string", "float"} {
		if !strings.Contains(found.Detail, want) {
			t.Errorf("detail %q does not mention %q", found.Detail, want)
		}
	}
}

// Nullability moving is reported too: an aggregate over a column that became
// nullable starts skipping rows, and nothing else says so.
func TestNullabilityChangesAreReported(t *testing.T) {
	t.Parallel()

	src := newSource(t, createOrders)
	store := newFakeStore()

	if _, err := syncOnce(t, store, src, 1); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	src.apply(
		"DROP TABLE orders",
		`CREATE TABLE orders (
			id INTEGER NOT NULL PRIMARY KEY,
			total REAL,
			note TEXT
		)`,
	)

	report, err := syncOnce(t, store, src, 2)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	for _, change := range report.Changes {
		if change.Column == "total" && strings.Contains(change.Detail, "became nullable") {
			return
		}
	}

	t.Errorf("a column becoming nullable was not reported:\n%s", changes(report))
}

/*
A dropped table is marked gone rather than deleted, and named.

Both halves matter. Marking keeps the history and the models pointing at it;
naming is what makes the report usable, because a count alone tells somebody a
table went and not which one.
*/
func TestADroppedTableIsMarkedGoneAndNamed(t *testing.T) {
	t.Parallel()

	src := newSource(t, createOrders, createCustomers)
	store := newFakeStore()

	if _, err := syncOnce(t, store, src, 1); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	src.apply("DROP TABLE customers")

	report, err := syncOnce(t, store, src, 2)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	var removed []string

	for _, change := range report.Changes {
		if change.Kind == catalog.Removed {
			removed = append(removed, change.String())
		}
	}

	// The table and both of its columns.
	if len(removed) != 3 {
		t.Fatalf("removals = %v, want the table and its two columns", removed)
	}

	for _, want := range []string{"main.customers", "main.customers.id", "main.customers.email"} {
		found := false

		for _, got := range removed {
			if strings.HasSuffix(got, want) {
				found = true
			}
		}

		if !found {
			t.Errorf("%s was not named among %v", want, removed)
		}
	}

	// Marked, not deleted: still in the store.
	stored, _ := store.Tables(t.Context(), uuid.Nil)
	for _, table := range stored {
		if table.TableName == "customers" {
			if !table.RemovedAt.Valid {
				t.Error("customers is not marked gone")
			}

			return
		}
	}

	t.Error("customers was deleted from the catalog rather than marked gone")
}

/*
A removal is reported once, not on every sync afterwards.

A catalog that re-reported every long-dead table forever would make the report
useless within a week, which is how change detection becomes something people
filter out of their alerts.
*/
func TestARemovalIsReportedOnce(t *testing.T) {
	t.Parallel()

	src := newSource(t, createOrders, createCustomers)
	store := newFakeStore()

	if _, err := syncOnce(t, store, src, 1); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	src.apply("DROP TABLE customers")

	if _, err := syncOnce(t, store, src, 2); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	report, err := syncOnce(t, store, src, 3)
	if err != nil {
		t.Fatalf("third sync: %v", err)
	}

	if !report.Unchanged() {
		t.Errorf("the third sync re-reported an old removal:\n%s", changes(report))
	}
}

// A table that comes back is an addition again, and keeps its history.
func TestATableThatComesBackIsReported(t *testing.T) {
	t.Parallel()

	src := newSource(t, createOrders, createCustomers)
	store := newFakeStore()

	if _, err := syncOnce(t, store, src, 1); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	src.apply("DROP TABLE customers")

	if _, err := syncOnce(t, store, src, 2); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	src.apply(createCustomers)

	report, err := syncOnce(t, store, src, 3)
	if err != nil {
		t.Fatalf("third sync: %v", err)
	}

	for _, change := range report.Changes {
		if change.Table == "customers" && change.Column == "" {
			if change.Kind != catalog.Added {
				t.Errorf("customers came back as %q, want %q", change.Kind, catalog.Added)
			}

			if !strings.Contains(change.Detail, "come back") {
				t.Errorf("detail = %q, want it to say it came back", change.Detail)
			}

			return
		}
	}

	t.Errorf("a table coming back was not reported:\n%s", changes(report))
}

/*
Repeated syncs write once per object and accumulate nothing.

Note what this does and does not prove. [catalog.Store] has no delete at all,
so a sync *cannot* replace the catalog -- that is settled by the interface
rather than by a test. What is checked here is the next thing down: three syncs
over an unchanged source write each object three times and leave one row each,
so nothing is duplicating and nothing is being skipped.

That the write really is an upsert -- same row, same id, first_seen_at intact
-- is real SQL, and is proven against both engines in
TestASecondSyncKeepsTheRowAndItsFirstSeenAt.
*/
func TestRepeatedSyncsAccumulateNothing(t *testing.T) {
	t.Parallel()

	src := newSource(t, createOrders)
	store := newFakeStore()

	for i := 1; i <= 3; i++ {
		if _, err := syncOnce(t, store, src, i); err != nil {
			t.Fatalf("sync %d: %v", i, err)
		}
	}

	// One table and three columns, three times.
	if store.recordedTables != 3 || store.recordedColumns != 9 {
		t.Errorf("recorded %d tables and %d columns over three syncs, want 3 and 9",
			store.recordedTables, store.recordedColumns)
	}

	// And the identity survived, which is what a rebuild would destroy.
	tables, _ := store.Tables(t.Context(), uuid.Nil)
	if len(tables) != 1 {
		t.Fatalf("%d tables stored, want 1", len(tables))
	}

	columns, _ := store.Columns(t.Context(), uuid.Nil)
	if len(columns) != 3 {
		t.Errorf("%d columns stored, want 3", len(columns))
	}
}

// The summary is one readable line, because that is what ends up in a log.
func TestTheSummaryReadsAsOneLine(t *testing.T) {
	t.Parallel()

	src := newSource(t, createOrders)
	store := newFakeStore()

	report, err := syncOnce(t, store, src, 1)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	summary := report.Summary()

	for _, want := range []string{"1 tables", "3 columns", "4 added"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q does not mention %q", summary, want)
		}
	}
}

// --- helpers ------------------------------------------------------------------

// syncOnce runs a sync at a distinct, increasing timestamp.
//
// The clock is stepped by whole minutes rather than left to time.Now, because
// the sweep compares against it: two syncs a microsecond apart would make
// "not seen this time" depend on how fast the machine is.
func syncOnce(t *testing.T, store catalog.Store, src *source, step int) (catalog.Report, error) {
	t.Helper()

	at := time.Date(2026, time.September, 28, 12, step, 0, 0, time.UTC)

	syncer := catalog.NewSyncer(store).WithClock(func() time.Time { return at })

	return syncer.Sync(t.Context(), uuid.New(), src.connector())
}

// dbTime, nullTime and boolOf spell the store's column types, which the fake
// has to produce because the syncer reads them back out of it.
func dbTime(t time.Time) dbtypes.Time { return dbtypes.NewTime(t) }

func nullTime(t time.Time) dbtypes.NullTime { return dbtypes.NewNullTime(t) }

func boolOf(b bool) dbtypes.Bool { return dbtypes.Bool(b) }

func changes(r catalog.Report) string {
	lines := make([]string, 0, len(r.Changes))
	for _, c := range r.Changes {
		lines = append(lines, "  "+c.String())
	}

	if len(lines) == 0 {
		return "  (none)"
	}

	return strings.Join(lines, "\n")
}

/*
The source connection is released before Pivot writes anything.

A sync reads somebody else's database and then writes its own, and the order
matters: interleaving them holds a pooled connection against a warehouse for as
long as Pivot's own store takes, which on a large schema is the difference
between a second and a minute. On a connection capped at one, it is also a
deadlock waiting for a second reader.

Proven by looking at the pool from inside the write rather than by reading the
code: the store below asserts, on every single record, that the connector has
no connection checked out.
*/
func TestTheSourceIsReleasedBeforeAnythingIsWritten(t *testing.T) {
	t.Parallel()

	src := newSource(t, createOrders, createCustomers)
	connector := src.connector()

	pooled, ok := connector.(*connectors.SQLConnector)
	if !ok {
		t.Fatalf("expected a SQLConnector, got %T", connector)
	}

	// Warm the pool, so "nothing in use" is a real observation rather than
	// "nothing has ever been opened".
	if _, err := connector.Query(t.Context(), "SELECT 1"); err != nil {
		t.Fatalf("warm: %v", err)
	}

	watching := &poolWatcher{Store: newFakeStore(), t: t, db: pooled}

	at := time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)

	report, err := catalog.NewSyncer(watching).
		WithClock(func() time.Time { return at }).
		Sync(t.Context(), uuid.New(), connector)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if watching.writes == 0 {
		t.Fatal("the sync wrote nothing, so this proves nothing")
	}

	if report.TablesSeen != 2 {
		t.Errorf("saw %d tables, want 2", report.TablesSeen)
	}

	t.Logf("%d writes, none of them holding a source connection", watching.writes)
}

// poolWatcher asserts the source is idle on every write.
type poolWatcher struct {
	catalog.Store

	t      *testing.T
	db     *connectors.SQLConnector
	writes int
}

func (p *poolWatcher) check(what string) {
	p.t.Helper()
	p.writes++

	if inUse := p.db.DB().Stats().InUse; inUse != 0 {
		p.t.Errorf("%s ran with %d source connections still checked out", what, inUse)
	}
}

func (p *poolWatcher) RecordTable(
	ctx context.Context, in repo.SeenTable, at time.Time,
) (model.CatalogTable, error) {
	p.check("recording a table")

	return p.Store.RecordTable(ctx, in, at)
}

func (p *poolWatcher) RecordColumn(
	ctx context.Context, in repo.SeenColumn, at time.Time,
) (model.CatalogColumn, error) {
	p.check("recording a column")

	return p.Store.RecordColumn(ctx, in, at)
}

func (p *poolWatcher) MarkGone(
	ctx context.Context, connectionID uuid.UUID, syncedAt time.Time,
) (int64, int64, error) {
	p.check("the sweep")

	return p.Store.MarkGone(ctx, connectionID, syncedAt)
}
