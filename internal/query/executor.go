package query

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// Errors an Executor returns before anything reaches a source.
var (
	// ErrEmptySQL is a request with nothing to run.
	ErrEmptySQL = errors.New("query: no statement to run")

	// ErrNoConnection is a request naming no connection.
	ErrNoConnection = errors.New("query: no connection named")
)

/*
Executor is the single door into a connected database.

Every execution goes through the same five stages, in this order and no other:

	parse -> authorize -> plan -> execute -> stream

The order is the point. Authorization happens in stage two, before a connector
is opened, before a configuration is read, before anything touches the network
-- so a denied caller cannot even be observed by the source, and a pipeline
that fails between stages fails without side effects. ADR-0009 makes the
semantic compiler the only place row-level security is injected, and that
guarantee is worth nothing if some handler can open a connector itself. This
type is what makes that not a code review convention.

Today [Plan] carries the SQL through unchanged. Phase 3 replaces the string
with a compiled query, and because every caller already goes through here it
is a change to one function rather than an audit of every call site.
*/
type Executor struct {
	repos   *repo.Repositories
	checker authz.Checker
	log     *slog.Logger

	// open is [connectors.Open], swapped in tests. Unexported and with no
	// setter outside this package: a caller that could substitute it could
	// substitute the source, and this is the type whose whole job is that
	// nobody can.
	open func(connectors.Config) (connectors.Connector, error)

	now func() time.Time
}

// Option configures an Executor.
type Option func(*Executor)

// WithLogger sets where the pipeline reports. Without it the pipeline is
// silent, which is right for tests and wrong for a server.
func WithLogger(log *slog.Logger) Option {
	return func(e *Executor) {
		if log != nil {
			e.log = log
		}
	}
}

// withOpener substitutes the connector factory. Test-only, and unexported so
// it stays that way.
func withOpener(open func(connectors.Config) (connectors.Connector, error)) Option {
	return func(e *Executor) { e.open = open }
}

// NewExecutor builds the pipeline over the repositories and the permission
// checker.
//
// The checker may be nil only in the sense that [authz.Enforce] treats nil as
// deny: an Executor wired without one refuses every request rather than
// serving them unchecked.
func NewExecutor(repos *repo.Repositories, checker authz.Checker, opts ...Option) *Executor {
	e := &Executor{
		repos:   repos,
		checker: checker,
		log:     slog.New(slog.DiscardHandler),
		open:    connectors.Open,
		now:     time.Now,
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Request is a question to put to a connected database.
type Request struct {
	// ConnectionID names the source.
	ConnectionID uuid.UUID

	// SQL is the statement. A string until Phase 3; see [Executor].
	SQL string

	// MaxRows caps the result. Zero uses the connection's own limit.
	MaxRows int64
}

/*
Plan is what stage three produced: what will actually be sent.

A struct rather than a string because the string is temporary. When Phase 3's
compiler lands, the compiled form goes here and every caller of the pipeline
keeps working -- which is the entire reason this stage exists now, while it
has nothing to do.
*/
type Plan struct {
	// SQL is the statement to send.
	SQL string

	// Connection is the resolved source.
	Connection model.Connection

	// MaxRows is the effective row cap.
	MaxRows int64
}

/*
Execution is a query that is running, and the stream of its result.

The caller reads [Execution.Stream] to exhaustion and then closes the
Execution. Closing is what completes the log row, releases the pooled
connection and closes the connector, and it is safe to call more than once.

Not closing leaves a row in state 'running' forever. That is deliberate: an
abandoned row is visible evidence in the query log, where a pipeline that
tidied up after a caller who forgot would leave none.
*/
type Execution struct {
	// LogID identifies the query log row, so a caller can cancel or explain a
	// query it started.
	LogID uuid.UUID

	// Stream is the result. Reading it is the caller's job.
	Stream connectors.Stream

	// Plan is what was sent, for a caller that wants to show it.
	Plan Plan
}

/*
Execute runs a request through the pipeline and returns the result as a stream.

The stages run in the order [Executor] documents, and each one's failure is
returned before the next begins. Nothing is opened until authorization has
answered yes, so a denial costs the source nothing.
*/
func (e *Executor) Execute(ctx context.Context, req Request) (*Execution, error) {
	scope, err := tenant.FromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	// 1. Parse: reject what cannot be a statement, before anyone is asked
	// whether they may run it.
	statement, err := parse(req)
	if err != nil {
		return nil, err
	}

	// 2. Authorize: before the connection is read, before a connector exists.
	if aerr := e.authorize(ctx, scope); aerr != nil {
		return nil, aerr
	}

	// 3. Plan.
	plan, err := e.plan(ctx, statement, req)
	if err != nil {
		return nil, err
	}

	// 4 and 5. Execute and stream.
	return e.execute(ctx, scope, plan)
}

// parse validates the statement. Today that means rejecting an empty one; the
// stage exists so Phase 3 has somewhere to put a parser.
func parse(req Request) (string, error) {
	if req.ConnectionID == uuid.Nil {
		return "", ErrNoConnection
	}

	statement := strings.TrimSpace(req.SQL)
	if statement == "" {
		return "", ErrEmptySQL
	}

	return statement, nil
}

/*
authorize asks whether this caller may run raw SQL here.

[authz.PermNativeQuery] rather than [authz.PermQuery], and the distinction is
load-bearing: row-level security is injected by the semantic compiler, and a
raw statement never goes through it. Someone permitted to ask questions of the
semantic layer is not thereby permitted to write SELECT * against the table
underneath it.

Background work has no actor and is not covered by the graph, so a system
scope is allowed through -- it is Pivot acting as itself, having already been
authorized when the job was scheduled.
*/
func (e *Executor) authorize(ctx context.Context, scope tenant.Scope) error {
	actor := scope.ActorID()
	if !actor.Valid {
		return nil
	}

	return authz.Enforce(ctx, e.checker, authz.Request{
		Subject:    authz.User(actor.UUID.String()),
		Permission: authz.PermNativeQuery,
		Object: authz.Object{
			Type: authz.TypeOrganization,
			ID:   scope.OrgID().String(),
		},
	})
}

// plan resolves the connection and settles what will be sent.
func (e *Executor) plan(ctx context.Context, statement string, req Request) (Plan, error) {
	conn, err := e.repos.Connections.Get(ctx, req.ConnectionID)
	if err != nil {
		return Plan{}, fmt.Errorf("query: resolve the connection: %w", err)
	}

	maxRows := conn.MaxRows
	if req.MaxRows > 0 && (maxRows == 0 || req.MaxRows < maxRows) {
		maxRows = req.MaxRows
	}

	return Plan{SQL: statement, Connection: conn, MaxRows: maxRows}, nil
}

// execute opens the source, records the start and hands back the stream.
func (e *Executor) execute(
	ctx context.Context, scope tenant.Scope, plan Plan,
) (*Execution, error) {
	cfg := configFor(plan.Connection)
	cfg.MaxRows = plan.MaxRows

	connector, err := e.open(cfg)
	if err != nil {
		return nil, fmt.Errorf("query: open %s: %w", plan.Connection.Slug, err)
	}

	started := e.now()

	entry, err := e.repos.QueryLog.Start(ctx, repo.QueryStart{
		ConnectionID: plan.Connection.ID,
		UserID:       scope.ActorID(),
		SQL:          plan.SQL,
		At:           started,
	})
	if err != nil {
		_ = connector.Close()

		// Refused rather than run unlogged. A query Pivot cannot account for
		// is the one an operator most needs accounted for, and "the audit
		// trail is optional when the database is busy" is not a property
		// anybody can rely on afterwards.
		return nil, fmt.Errorf("query: record the start: %w", err)
	}

	stream, err := connector.Stream(ctx, plan.SQL)
	if err != nil {
		e.finish(ctx, entry, started, nil, err)
		_ = connector.Close()

		return nil, fmt.Errorf("query: execute on %s: %w", plan.Connection.Slug, err)
	}

	return &Execution{
		LogID: entry.ID,
		Plan:  plan,
		Stream: &loggedStream{
			Stream:    stream,
			executor:  e,
			entry:     entry,
			started:   started,
			connector: connector,
			// The context the query ran under, not the caller's next one: the
			// completing write has to happen even when the reason it is
			// happening is that this context was canceled.
			ctx: ctx,
		},
	}, nil
}

/*
finish completes the log row.

Detached from the caller's context on purpose. The most interesting thing to
record is a cancellation, and a cancellation means the context the query ran
under is already dead -- writing the outcome through it would lose exactly the
rows worth keeping.
*/
func (e *Executor) finish(
	ctx context.Context, entry model.QueryLogEntry, started time.Time, s *loggedStream, cause error,
) {
	finished := e.now()

	out := repo.QueryOutcome{
		State:    repo.StateSucceeded,
		At:       finished,
		Duration: finished.Sub(started),
	}

	if s != nil {
		out.Rows = s.rows
		out.BytesEstimated = s.bytes
		out.Truncated = s.Truncated()
	}

	switch {
	case cause != nil && isCanceled(cause):
		out.State = repo.StateCanceled
		out.Err = cause.Error()
	case cause != nil:
		out.State = repo.StateFailed
		out.Err = cause.Error()
	}

	// A detached scope, for the reason in the doc comment. Same tenant, same
	// actor: only the cancellation and the deadline are dropped.
	scope, err := tenant.FromContext(ctx)
	if err != nil {
		e.log.Error("a query finished with no tenant scope to record it under", "error", err)

		return
	}

	detached := tenant.WithScope(context.WithoutCancel(ctx), scope)

	if err := e.repos.QueryLog.Finish(detached, entry.ID, out); err != nil {
		// Logged, not returned. The query itself succeeded or failed on its
		// own terms, and turning "the log write failed" into the caller's
		// error would replace a real answer with a bookkeeping one.
		e.log.Error("could not complete the query log entry",
			"query_log_id", entry.ID, "state", out.State, "error", err)
	}
}

// isCanceled reports whether an error is the caller having stopped waiting,
// as opposed to the source having failed.
func isCanceled(err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}

	return errors.Is(err, &connectors.Error{Reason: connectors.ReasonCanceled})
}

// configFor turns a stored connection into a connector configuration.
func configFor(c model.Connection) connectors.Config {
	return connectors.Config{
		Kind:                connectors.Kind(c.Kind),
		Host:                c.Host,
		Port:                int(c.Port),
		Database:            c.Database,
		Username:            c.Username,
		Password:            c.Password,
		SSLMode:             c.SslMode,
		MaxOpenConns:        int(c.MaxOpenConns),
		QueryTimeoutSeconds: int(c.QueryTimeoutSeconds),
		MaxRows:             c.MaxRows,
	}
}
