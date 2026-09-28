package query

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

/*
The pipeline.

Every test here goes through [Executor.Execute] against a real SQLite source,
because the properties being checked are about ordering and accounting and
both are easy to satisfy in a mock and miss in the thing that ships.
*/

const emeaQuery = "SELECT id, region, total FROM orders WHERE region = 'emea'"

/*
Authorization runs before anything is opened.

Checked by counting opens rather than by reading the code. A denied request
that had already opened a connector would have cost the source a connection
and, on a warehouse that bills by the second, money -- and it would mean the
denial happened somewhere after the point this pipeline promises it happens.
*/
func TestADeniedQueryNeverReachesTheSource(t *testing.T) {
	t.Parallel()

	f := newFixture(t, fixtureOptions{})
	f.checker.allow = false

	_, err := f.executor.Execute(f.ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})

	if !errors.Is(err, authz.ErrDenied) {
		t.Fatalf("err = %v, want ErrDenied", err)
	}

	if opens := f.opens.Load(); opens != 0 {
		t.Errorf("a denied query opened %d connectors, want 0", opens)
	}

	// And nothing was written down, because nothing ran. A log entry here
	// would be a query that never happened.
	entries, err := f.repos.QueryLog.List(f.ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("a denied query left %d log entries", len(entries))
	}
}

// The permission asked for is native_query, not query.
//
// Row-level security is injected by the semantic compiler (ADR-0009) and raw
// SQL never passes through it, so the permission to ask a question of the
// semantic layer is not the permission to run this.
func TestRawSQLRequiresTheNativeQueryPermission(t *testing.T) {
	t.Parallel()

	f := newFixture(t, fixtureOptions{})

	ex, err := f.executor.Execute(f.ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	drain(t, ex)

	if f.checker.asked.Load() != 1 {
		t.Fatalf("the checker was asked %d times, want 1", f.checker.asked.Load())
	}

	if got := f.checker.last.Permission; got != authz.PermNativeQuery {
		t.Errorf("asked for %q, want %q", got, authz.PermNativeQuery)
	}

	if got := f.checker.last.Subject.ID; got != f.userID.String() {
		t.Errorf("asked about subject %q, want the caller %q", got, f.userID)
	}
}

/*
An execution is on record: user, SQL, duration, rows, bytes and error.

End to end through the pipeline against a real source, so the counts are the
ones the stream actually produced rather than ones a test arranged.
*/
func TestAnExecutionIsRecordedInFull(t *testing.T) {
	t.Parallel()

	f := newFixture(t, fixtureOptions{})

	ex, err := f.executor.Execute(f.ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// While it is running and before it is closed, the log already has it.
	running, err := f.repos.QueryLog.Running(f.ctx)
	if err != nil {
		t.Fatalf("running: %v", err)
	}

	if len(running) != 1 || running[0].ID != ex.LogID {
		t.Fatalf("the running query is not in the log: %v", running)
	}

	if rows := drain(t, ex); rows != 2 {
		t.Fatalf("read %d rows, want 2", rows)
	}

	entry, err := f.repos.QueryLog.Get(f.ctx, ex.LogID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	switch {
	case entry.State != repo.StateSucceeded:
		t.Errorf("State = %q, want succeeded", entry.State)
	case entry.SQLText != emeaQuery:
		t.Errorf("SQLText = %q", entry.SQLText)
	case !entry.UserID.Valid || entry.UserID.UUID != f.userID:
		t.Errorf("UserID = %v, want %v", entry.UserID, f.userID)
	case entry.ConnectionID != f.connID:
		t.Errorf("ConnectionID = %v, want %v", entry.ConnectionID, f.connID)
	case entry.RowsReturned != 2:
		t.Errorf("RowsReturned = %d, want 2", entry.RowsReturned)
	case entry.BytesEstimated <= 0:
		t.Errorf("BytesEstimated = %d on a result with rows in it", entry.BytesEstimated)
	case entry.CacheStatus != "uncached":
		t.Errorf("CacheStatus = %q, want uncached", entry.CacheStatus)
	case entry.ErrorMessage != "":
		t.Errorf("ErrorMessage = %q on a query that worked", entry.ErrorMessage)
	case !entry.FinishedAt.Valid:
		t.Error("FinishedAt is unset on a closed execution")
	case entry.DurationMs < 0:
		t.Errorf("DurationMs = %d", entry.DurationMs)
	}
}

// A query the source rejects is recorded as failed, with the source's own
// message, so the log answers "why did that fail" without a second round trip.
func TestAFailedExecutionIsRecordedWithItsError(t *testing.T) {
	t.Parallel()

	f := newFixture(t, fixtureOptions{})

	_, err := f.executor.Execute(f.ctx, Request{
		ConnectionID: f.connID, SQL: "SELECT nope FROM orders",
	})
	if err == nil {
		t.Fatal("a query against a column that does not exist succeeded")
	}

	entries, err := f.repos.QueryLog.List(f.ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("a failed query left %d log entries, want 1", len(entries))
	}

	if entries[0].State != repo.StateFailed {
		t.Errorf("State = %q, want failed", entries[0].State)
	}

	if !strings.Contains(entries[0].ErrorMessage, "nope") {
		t.Errorf("ErrorMessage = %q; it does not name the column", entries[0].ErrorMessage)
	}
}

// A result that hits the row cap is recorded as truncated, so a number in a
// dashboard that is quietly a partial answer is not quietly a partial answer.
func TestATruncatedResultSaysSoInTheLog(t *testing.T) {
	t.Parallel()

	f := newFixture(t, fixtureOptions{})

	ex, err := f.executor.Execute(f.ctx, Request{
		ConnectionID: f.connID, SQL: "SELECT id FROM orders", MaxRows: 1,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	drain(t, ex)

	entry, err := f.repos.QueryLog.Get(f.ctx, ex.LogID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if !bool(entry.Truncated) {
		t.Error("a result cut at the row cap is not recorded as truncated")
	}

	if entry.RowsReturned != 1 {
		t.Errorf("RowsReturned = %d, want 1", entry.RowsReturned)
	}
}

// The pipeline refuses a statement it cannot run, before authorization and
// before anything is opened.
func TestTheParseStageRejectsWhatCannotBeRun(t *testing.T) {
	t.Parallel()

	f := newFixture(t, fixtureOptions{})

	cases := map[string]struct {
		req  Request
		want error
	}{
		"no statement":       {Request{ConnectionID: f.connID, SQL: "   \n\t "}, ErrEmptySQL},
		"no connection":      {Request{SQL: emeaQuery}, ErrNoConnection},
		"neither of the two": {Request{}, ErrNoConnection},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := f.executor.Execute(f.ctx, tc.req); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}

	if opens := f.opens.Load(); opens != 0 {
		t.Errorf("an unrunnable request opened %d connectors", opens)
	}
}

// A request with no tenant scope is refused. The repositories would refuse it
// too, but by then a connector is open: this is the pipeline failing at the
// stage where failing is free.
func TestAScopelessRequestIsRefusedBeforeAnythingOpens(t *testing.T) {
	t.Parallel()

	f := newFixture(t, fixtureOptions{})

	_, err := f.executor.Execute(t.Context(), Request{ConnectionID: f.connID, SQL: emeaQuery})
	if !errors.Is(err, tenant.ErrNoScope) {
		t.Fatalf("err = %v, want ErrNoScope", err)
	}

	if opens := f.opens.Load(); opens != 0 {
		t.Errorf("a scopeless request opened %d connectors", opens)
	}
}

// A connection belonging to another tenant is not found, and nothing opens.
func TestAnotherTenantsConnectionCannotBeQueried(t *testing.T) {
	t.Parallel()

	f := newFixture(t, fixtureOptions{})

	other := tenant.WithScope(t.Context(),
		tenant.MustNewScope(uuid.New(), uuid.NullUUID{UUID: f.userID, Valid: true}))

	_, err := f.executor.Execute(other, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	if opens := f.opens.Load(); opens != 0 {
		t.Errorf("a cross-tenant request opened %d connectors", opens)
	}
}

/*
Background work runs with no actor, and the log says so.

A system scope is Pivot acting as itself. It is allowed through the
authorization stage because the graph has no subject to ask about, and the
decision that mattered was made when the job was scheduled. The log records
the absence rather than inventing a user, which is the difference between an
audit trail and a plausible one.
*/
func TestSystemWorkIsLoggedWithNoUser(t *testing.T) {
	t.Parallel()

	f := newFixture(t, fixtureOptions{})

	system := tenant.WithScope(t.Context(), tenant.MustNewScope(f.orgID, uuid.NullUUID{}))

	ex, err := f.executor.Execute(system, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	drain(t, ex)

	entry, err := f.repos.QueryLog.Get(system, ex.LogID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if entry.UserID.Valid {
		t.Errorf("UserID = %v on work with no person behind it", entry.UserID)
	}

	if f.checker.asked.Load() != 0 {
		t.Errorf("the graph was asked about a system scope %d times", f.checker.asked.Load())
	}
}

// Closing twice is safe, and does not write the outcome twice.
func TestClosingAnExecutionTwiceIsSafe(t *testing.T) {
	t.Parallel()

	f := newFixture(t, fixtureOptions{})

	ex, err := f.executor.Execute(f.ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	drain(t, ex)

	// The second close would fail against the log if it tried to write again:
	// Finish on an already-finished row still matches, but a second connector
	// close on a released pool does not, and neither should surface here.
	if err := ex.Stream.Close(); err != nil {
		t.Errorf("the second close returned %v", err)
	}
}
