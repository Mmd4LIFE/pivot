package repo_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

/*
The query log, on both engines.

The two-phase write is the property here. A row exists while the query is
still running, which is what makes "what is running right now" answerable and
what leaves evidence when a process dies mid-query -- and it is the part a
single insert-at-the-end would quietly lose.
*/

const loggedSQL = "SELECT * FROM orders WHERE region = 'emea'"

// A started query is visible before it finishes, and finishing records what
// happened.
func TestAQueryIsOnRecordWhileItIsStillRunning(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f := newConnectionFixture(t, db)
		conn := seedConnection(t, f)

		user := uuid.NullUUID{UUID: uuid.New(), Valid: true}
		start := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)

		entry, err := f.repos.QueryLog.Start(f.ctx, repo.QueryStart{
			ConnectionID: conn, UserID: user, SQL: loggedSQL, At: start,
		})
		if err != nil {
			t.Fatalf("start: %v", err)
		}

		if entry.State != repo.StateRunning {
			t.Errorf("a started query is in state %q, want %q", entry.State, repo.StateRunning)
		}

		// Uncached until Part 21 has a cache to report on. Recorded now so
		// the column is part of the contract rather than added under a
		// feature that then has to backfill it.
		if entry.CacheStatus != "uncached" {
			t.Errorf("CacheStatus = %q, want uncached", entry.CacheStatus)
		}

		running, err := f.repos.QueryLog.Running(f.ctx)
		if err != nil {
			t.Fatalf("running: %v", err)
		}

		if len(running) != 1 || running[0].ID != entry.ID {
			t.Fatalf("a query that has started and not finished is not in Running: %v", running)
		}

		if ferr := f.repos.QueryLog.Finish(f.ctx, entry.ID, repo.QueryOutcome{
			State: repo.StateSucceeded, At: start.Add(1500 * time.Millisecond),
			Duration: 1500 * time.Millisecond, Rows: 42, BytesEstimated: 8192,
		}); ferr != nil {
			t.Fatalf("finish: %v", ferr)
		}

		after, err := f.repos.QueryLog.Running(f.ctx)
		if err != nil {
			t.Fatalf("running after finish: %v", err)
		}

		if len(after) != 0 {
			t.Errorf("a finished query is still listed as running: %v", after)
		}

		got, err := f.repos.QueryLog.Get(f.ctx, entry.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}

		// Every field the part promises to record, checked together: a log
		// that drops one of them is a log somebody cannot answer a question
		// with, and which one is missing is not predictable in advance.
		switch {
		case got.UserID != user:
			t.Errorf("UserID = %v, want %v", got.UserID, user)
		case got.SQLText != loggedSQL:
			t.Errorf("SQLText = %q", got.SQLText)
		case got.State != repo.StateSucceeded:
			t.Errorf("State = %q", got.State)
		case got.DurationMs != 1500:
			t.Errorf("DurationMs = %d, want 1500", got.DurationMs)
		case got.RowsReturned != 42:
			t.Errorf("RowsReturned = %d, want 42", got.RowsReturned)
		case got.BytesEstimated != 8192:
			t.Errorf("BytesEstimated = %d, want 8192", got.BytesEstimated)
		case bool(got.Truncated):
			t.Error("Truncated is set on a result that was not truncated")
		case got.ErrorMessage != "":
			t.Errorf("ErrorMessage = %q on a successful query", got.ErrorMessage)
		case !got.FinishedAt.Valid:
			t.Error("FinishedAt is not set on a finished query")
		}
	})
}

// A failure is recorded as one, with the message.
func TestAFailedQueryRecordsWhyItFailed(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f := newConnectionFixture(t, db)
		conn := seedConnection(t, f)

		entry, err := f.repos.QueryLog.Start(f.ctx, repo.QueryStart{
			ConnectionID: conn, SQL: "SELECT nope", At: time.Now(),
		})
		if err != nil {
			t.Fatalf("start: %v", err)
		}

		// No user: scheduled work has no person behind it, and the log says
		// so rather than attributing it to somebody.
		if entry.UserID.Valid {
			t.Errorf("UserID = %v on a query with no actor", entry.UserID)
		}

		if ferr := f.repos.QueryLog.Finish(f.ctx, entry.ID, repo.QueryOutcome{
			State: repo.StateFailed, At: time.Now(), Err: `column "nope" does not exist`,
		}); ferr != nil {
			t.Fatalf("finish: %v", ferr)
		}

		got, err := f.repos.QueryLog.Get(f.ctx, entry.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}

		if got.State != repo.StateFailed {
			t.Errorf("State = %q, want %q", got.State, repo.StateFailed)
		}

		if got.ErrorMessage == "" {
			t.Error("a failed query recorded no error message")
		}
	})
}

/*
Another tenant cannot finish, read or list this tenant's queries.

The finish path is the one worth checking by hand. It is an UPDATE scoped by
org as well as id, and an UPDATE that matches nothing succeeds at the SQL
level -- so without the row count check it would look like a write that
worked, against a row belonging to somebody else.
*/
func TestTheQueryLogIsTenantScoped(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f := newConnectionFixture(t, db)
		conn := seedConnection(t, f)

		entry, err := f.repos.QueryLog.Start(f.ctx, repo.QueryStart{
			ConnectionID: conn, SQL: loggedSQL, At: time.Now(),
		})
		if err != nil {
			t.Fatalf("start: %v", err)
		}

		if _, gerr := f.repos.QueryLog.Get(f.otherCtx, entry.ID); !errors.Is(gerr, repo.ErrNotFound) {
			t.Errorf("another tenant read the entry: err = %v", gerr)
		}

		err = f.repos.QueryLog.Finish(f.otherCtx, entry.ID, repo.QueryOutcome{
			State: repo.StateSucceeded, At: time.Now(),
		})
		if !errors.Is(err, repo.ErrNotFound) {
			t.Errorf("another tenant finished the entry: err = %v", err)
		}

		still, err := f.repos.QueryLog.Running(f.ctx)
		if err != nil {
			t.Fatalf("running: %v", err)
		}

		if len(still) != 1 {
			t.Errorf("the entry was changed by the other tenant: running = %v", still)
		}

		theirs, err := f.repos.QueryLog.List(f.otherCtx, 10)
		if err != nil {
			t.Fatalf("list: %v", err)
		}

		if len(theirs) != 0 {
			t.Errorf("another tenant listed %d of our queries", len(theirs))
		}
	})
}

// The list is newest first, so "what just happened" is the first page.
func TestTheQueryLogListsNewestFirst(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f := newConnectionFixture(t, db)
		conn := seedConnection(t, f)

		base := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)

		for i := range 3 {
			if _, err := f.repos.QueryLog.Start(f.ctx, repo.QueryStart{
				ConnectionID: conn,
				SQL:          loggedSQL,
				At:           base.Add(time.Duration(i) * time.Minute),
			}); err != nil {
				t.Fatalf("start %d: %v", i, err)
			}
		}

		entries, err := f.repos.QueryLog.List(f.ctx, 2)
		if err != nil {
			t.Fatalf("list: %v", err)
		}

		if len(entries) != 2 {
			t.Fatalf("limit 2 returned %d entries", len(entries))
		}

		if !entries[0].StartedAt.After(entries[1].StartedAt.Time) {
			t.Errorf("the list is not newest first: %v then %v",
				entries[0].StartedAt.Time, entries[1].StartedAt.Time)
		}
	})
}

// Finishing something that was never started is an error, not a silent no-op.
func TestFinishingAnUnknownQueryIsAnError(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f := newConnectionFixture(t, db)

		err := f.repos.QueryLog.Finish(f.ctx, uuid.New(), repo.QueryOutcome{
			State: repo.StateSucceeded, At: time.Now(),
		})
		if !errors.Is(err, repo.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
}

// seedConnection creates the connection a log entry has to point at.
func seedConnection(t *testing.T, f connectionFixture) uuid.UUID {
	t.Helper()

	conn, err := f.repos.Connections.Create(f.ctx, repo.CreateConnection{
		Slug: "warehouse", Name: "Analytics warehouse", Kind: "postgres",
		Host: "db.internal", Port: 5432, Database: "analytics",
		Username: "pivot", Password: connectionPassword, SSLMode: "require",
		IsEnabled: true,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	return conn.ID
}

/*
Per-user usage, on both engines.

The aggregate is computed in SQL, which is the only interesting thing about it:
the numbers are summed by a database rather than by Go, so the two engines have
to agree, and SQLite needed explicit CASTs to stop sqlc emitting `interface{}`
where PostgreSQL gives int64.
*/
func TestUsageIsReportedPerPerson(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f := newConnectionFixture(t, db)
		conn := seedConnection(t, f)

		base := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)
		ada := uuid.NullUUID{UUID: uuid.New(), Valid: true}
		grace := uuid.NullUUID{UUID: uuid.New(), Valid: true}

		record := func(user uuid.NullUUID, at time.Time, state string, ms, rows int64, cache string) {
			t.Helper()

			entry, err := f.repos.QueryLog.Start(f.ctx, repo.QueryStart{
				ConnectionID: conn, UserID: user, SQL: loggedSQL, At: at,
			})
			if err != nil {
				t.Fatalf("start: %v", err)
			}

			if ferr := f.repos.QueryLog.Finish(f.ctx, entry.ID, repo.QueryOutcome{
				State: state, At: at.Add(time.Duration(ms) * time.Millisecond),
				Duration: time.Duration(ms) * time.Millisecond,
				Rows:     rows, BytesEstimated: rows * 10, CacheStatus: cache,
			}); ferr != nil {
				t.Fatalf("finish: %v", ferr)
			}
		}

		record(ada, base, repo.StateSucceeded, 1000, 10, "miss")
		record(ada, base.Add(time.Minute), repo.StateFailed, 500, 0, "uncached")
		record(ada, base.Add(2*time.Minute), repo.StateSucceeded, 100, 10, "hit")
		record(grace, base.Add(3*time.Minute), repo.StateSucceeded, 50, 1, "miss")

		// And one before the window, which must not be counted.
		record(grace, base.Add(-time.Hour), repo.StateSucceeded, 9999, 999, "miss")

		usage, err := f.repos.QueryLog.UsageByUser(f.ctx, base)
		if err != nil {
			t.Fatalf("usage: %v", err)
		}

		byUser := make(map[uuid.UUID]model.QueryUsage, len(usage))
		for _, u := range usage {
			byUser[u.UserID.UUID] = u
		}

		mine := byUser[ada.UUID]

		switch {
		case mine.Queries != 3:
			t.Errorf("ada ran %d queries in the window, want 3", mine.Queries)
		case mine.TotalMs != 1600:
			t.Errorf("ada's total is %dms, want 1600", mine.TotalMs)
		case mine.TotalRows != 20:
			t.Errorf("ada returned %d rows, want 20", mine.TotalRows)
		case mine.TotalBytes != 200:
			t.Errorf("ada's bytes = %d, want 200", mine.TotalBytes)
		case mine.Failures != 1:
			t.Errorf("ada had %d failures, want 1", mine.Failures)
		case mine.CacheHits != 1:
			t.Errorf("ada had %d cache hits, want 1", mine.CacheHits)
		}

		// The window is honored: grace's earlier query is not in her total.
		if theirs := byUser[grace.UUID]; theirs.Queries != 1 {
			t.Errorf("grace has %d queries in the window, want 1; the window is not being applied",
				theirs.Queries)
		}

		// Ordered by cost, so the first row is the person to talk to.
		if len(usage) > 0 && usage[0].UserID.UUID != ada.UUID {
			t.Error("the most expensive user is not listed first")
		}
	})
}

// Work with no person behind it is its own row, not attributed and not dropped.
func TestUnattendedWorkIsItsOwnUsageRow(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f := newConnectionFixture(t, db)
		conn := seedConnection(t, f)

		at := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

		entry, err := f.repos.QueryLog.Start(f.ctx, repo.QueryStart{
			ConnectionID: conn, SQL: loggedSQL, At: at,
		})
		if err != nil {
			t.Fatalf("start: %v", err)
		}

		if ferr := f.repos.QueryLog.Finish(f.ctx, entry.ID, repo.QueryOutcome{
			State: repo.StateSucceeded, At: at, Duration: time.Second,
		}); ferr != nil {
			t.Fatalf("finish: %v", ferr)
		}

		usage, err := f.repos.QueryLog.UsageByUser(f.ctx, at.Add(-time.Hour))
		if err != nil {
			t.Fatalf("usage: %v", err)
		}

		if len(usage) != 1 {
			t.Fatalf("got %d usage rows, want 1", len(usage))
		}

		if usage[0].UserID.Valid {
			t.Errorf("unattended work was attributed to %v", usage[0].UserID)
		}
	})
}
