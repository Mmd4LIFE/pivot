package repo

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

/*
QueryLogRepo records what was executed against a connected database.

Written in two phases: Start when the query goes out, Finish when it comes
back. A single row written at the end would be simpler and would lose the two
things the log is for -- a query that is still running is invisible until it
ends, and a query that kills the process is never recorded at all. With two
phases, an abandoned row in state 'running' is itself the evidence.

Storage only, like every repository here. [query.Executor] owns the deciding;
this owns the writing down.
*/
type QueryLogRepo struct {
	base
}

// QueryStart is an execution about to begin.
type QueryStart struct {
	ConnectionID uuid.UUID
	UserID       uuid.NullUUID
	SQL          string
	At           time.Time

	// Owner names the process that will run it, so that a kill issued
	// anywhere can be routed to the only place that can deliver it. Empty
	// means unowned, and an unowned query cannot be killed.
	Owner string
}

// QueryOutcome is how an execution ended.
type QueryOutcome struct {
	State          string
	At             time.Time
	Duration       time.Duration
	Rows           int64
	BytesEstimated int64
	Truncated      bool

	// CacheStatus is hit, miss or uncached. Recorded on the outcome rather
	// than at the start because at the start it is not yet known: a query
	// becomes a miss by running and a hit by not having to.
	CacheStatus string

	Err string
}

// The states a logged query can be in. A row stays Running until something
// finishes it, which is what makes an abandoned row detectable.
const (
	StateRunning   = "running"
	StateSucceeded = "succeeded"
	StateFailed    = "failed"
	StateCanceled  = "canceled"
)

// Start records a query that is about to run and returns the row that will be
// completed later.
func (r *QueryLogRepo) Start(ctx context.Context, in QueryStart) (model.QueryLogEntry, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.QueryLogEntry{}, err
	}

	entry, err := r.q.StartQueryLog(ctx, model.StartQueryLogParams{
		ID:           newID(),
		OrgID:        s.OrgID(),
		ConnectionID: in.ConnectionID,
		UserID:       in.UserID,
		SQLText:      in.SQL,
		StartedAt:    dbtypes.NewTime(in.At),
		Owner:        in.Owner,
	})
	if err != nil {
		return model.QueryLogEntry{}, translate(err)
	}

	return entry, nil
}

/*
Finish completes a started row.

A finish that matches nothing returns [ErrNotFound]. That is not pedantry: the
statement is scoped by org as well as id, so a miss means either the row was
never started or somebody is completing another tenant's query, and both are
worth failing on rather than silently writing nothing.
*/
func (r *QueryLogRepo) Finish(ctx context.Context, id uuid.UUID, out QueryOutcome) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	n, err := r.q.FinishQueryLog(ctx, model.FinishQueryLogParams{
		State:          out.State,
		FinishedAt:     dbtypes.NewNullTime(out.At),
		DurationMs:     out.Duration.Milliseconds(),
		RowsReturned:   out.Rows,
		BytesEstimated: out.BytesEstimated,
		Truncated:      dbtypes.Bool(out.Truncated),
		CacheStatus:    cacheStatusOr(out.CacheStatus),
		ErrorMessage:   out.Err,
		ID:             id,
		OrgID:          s.OrgID(),
	})
	if err != nil {
		return translate(err)
	}

	if n == 0 {
		return ErrNotFound
	}

	return nil
}

/*
cacheStatusOr keeps the column's default meaning when a caller says nothing.

The column is NOT NULL, so the finishing UPDATE has to write something. An
empty string would be a fourth value that means the same as "uncached" and
sorts differently in every query anybody writes against this table.
*/
func cacheStatusOr(status string) string {
	if status == "" {
		return "uncached"
	}

	return status
}

// List returns the most recent executions for the tenant, newest first.
func (r *QueryLogRepo) List(ctx context.Context, limit int64) ([]model.QueryLogEntry, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	entries, err := r.q.ListQueryLog(ctx, model.ListQueryLogParams{
		OrgID: s.OrgID(),
		Limit: limit,
	})
	if err != nil {
		return nil, translate(err)
	}

	return entries, nil
}

// Running returns the tenant's queries that have started and not finished.
func (r *QueryLogRepo) Running(ctx context.Context) ([]model.QueryLogEntry, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	entries, err := r.q.ListRunningQueries(ctx, s.OrgID())
	if err != nil {
		return nil, translate(err)
	}

	return entries, nil
}

/*
UsageByUser reports what each person has cost since a point in time.

Grouped in SQL rather than by reading the log and totalling in Go, because the
question is asked over a window that may hold every query an organization has
ever run and the answer is a handful of rows.

`since` rather than a page: usage is a question about a period, and a caller
who asked for "the last day" and got the most recent thousand rows would be
given a different question's answer.
*/
func (r *QueryLogRepo) UsageByUser(ctx context.Context, since time.Time) ([]model.QueryUsage, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	usage, err := r.q.QueryUsageByUser(ctx, model.QueryUsageByUserParams{
		OrgID:     s.OrgID(),
		StartedAt: dbtypes.NewTime(since),
	})
	if err != nil {
		return nil, translate(err)
	}

	return usage, nil
}

/*
RequestCancel asks for a running query to be stopped.

Tenant-scoped, and the scope is the security boundary rather than a
convenience: a kill is an instruction to stop somebody's work, and one that
could name a query in another organization would be a denial of service with a
tenant boundary drawn in the wrong place.

Returns [ErrNotFound] when nothing matched, which covers three cases the caller
has to tell apart by other means: the query finished a moment ago, it belongs
to another tenant, or it never existed. All three mean "there is nothing to
stop", which is what this layer knows.

The timestamp comes from the database rather than from Go, because whether a
kill is stale is judged against the heartbeat and both must be measured by one
clock. A cancel stamped by a machine running four seconds fast would otherwise
look like it had been ignored.
*/
func (r *QueryLogRepo) RequestCancel(ctx context.Context, id uuid.UUID) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	n, err := r.q.RequestQueryCancel(ctx, model.RequestQueryCancelParams{
		CancelRequestedBy: s.ActorID(),
		ID:                id,
		OrgID:             s.OrgID(),
	})
	if err != nil {
		return translate(err)
	}

	if n == 0 {
		return ErrNotFound
	}

	return nil
}

// Get returns one logged execution.
func (r *QueryLogRepo) Get(ctx context.Context, id uuid.UUID) (model.QueryLogEntry, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.QueryLogEntry{}, err
	}

	entry, err := r.q.GetQueryLogEntry(ctx, model.GetQueryLogEntryParams{
		ID:    id,
		OrgID: s.OrgID(),
	})
	if err != nil {
		return model.QueryLogEntry{}, translate(err)
	}

	return entry, nil
}
