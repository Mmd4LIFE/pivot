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
