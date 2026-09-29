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
	"github.com/Mmd4LIFE/pivot/internal/policy"
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

	/*
		ErrKilled is the cause attached when an administrator stops a query.

		Distinct from a plain cancellation, which is what a caller walking away
		looks like. Both arrive as a canceled context through the same channel
		and they are different facts: one is somebody's decision about this
		query and the other is a closed browser tab. The query log records
		which, because "why did my dashboard stop" has two very different
		answers.
	*/
	ErrKilled = errors.New("query: stopped by an administrator")
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

	/*
		cache is the L1 result cache, and granter is what a caller's policy set
		is resolved from. Both may be nil, and a nil cache is not a degraded
		mode -- it is the cache being off, which every execution then reports
		as uncached.

		They are separate fields because they fail separately. A cache with no
		granter cannot fingerprint anybody, and rather than cache them together
		under a blank policy set it caches nothing at all: the whole point of
		ADR-0006's key is that callers who cannot be told apart must not share.
	*/
	cache   *Cache
	granter authz.Granter

	/*
		governor decides who gets to run. Nil means no admission control, which
		is not a degraded mode: an instance with one user and one connection
		has nothing to govern, and a queue in front of it would only add a
		place for queries to wait.
	*/
	governor *Governor

	// queryTimeout is an organization-wide ceiling on how long any query may
	// run. Zero leaves each connection's own timeout in charge.
	queryTimeout time.Duration

	// monitor holds the cancel for every query this process is running, so
	// that something other than the caller can stop one. Nil means nothing
	// can, which is what an executor built for a test wants.
	monitor *Monitor

	// owner names this process on the rows it writes, so a kill issued
	// anywhere can be routed to the only place that can deliver it.
	owner string

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

/*
WithCache turns the result cache on.

The granter is required with it, and passing a nil one leaves the cache off
rather than running it without fingerprints. That is the fail-closed direction:
an executor that cached without resolving the caller would serve one caller's
rows to another, and an executor that quietly declines to cache is merely slow.
*/
func WithCache(cache *Cache, granter authz.Granter) Option {
	return func(e *Executor) {
		if cache == nil || granter == nil {
			return
		}

		e.cache = cache
		e.granter = granter
	}
}

/*
WithGovernor turns admission control on.

Separate from [WithCache] because they answer different questions and fail
differently: the cache decides whether the source needs to be asked at all, and
the governor decides whether this caller may ask it right now.
*/
func WithGovernor(g *Governor) Option {
	return func(e *Executor) {
		if g != nil {
			e.governor = g
		}
	}
}

/*
WithQueryTimeout sets an organization-wide ceiling on how long a query may run.

A ceiling, not a setting: the effective timeout is the smaller of this and the
connection's own, so an operator can bound every query without editing every
connection and cannot accidentally *extend* one that was deliberately made
short.
*/
func WithQueryTimeout(d time.Duration) Option {
	return func(e *Executor) {
		if d > 0 {
			e.queryTimeout = d
		}
	}
}

/*
WithMonitor lets queries be stopped by somebody other than their caller.

Without it a query can only be canceled by the context its caller holds, which
is fine for a caller who closed a browser tab and useless for an administrator
watching a warehouse burn.
*/
func WithMonitor(m *Monitor) Option {
	return func(e *Executor) {
		if m != nil {
			e.monitor = m
		}
	}
}

/*
WithOwner stamps this process's token on every query it starts.

Without it a row is written unowned, and an unowned row is one no supervisor
will ever claim -- so the query runs normally and simply cannot be stopped from
another process. That is the honest degradation: not a failure, and not a
pretense that the kill worked.
*/
func WithOwner(owner string) Option {
	return func(e *Executor) { e.owner = owner }
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

	// CacheStatus is hit, miss or uncached -- the same value the query log
	// records, exposed so a caller can show it without reading the log back.
	CacheStatus string
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

	// 4. The cache sits here, between planning and execution, because it needs
	// what planning resolved -- the connection and the row cap are both part
	// of the key -- and because a hit must cost the source nothing.
	key, cacheable := e.cacheKey(ctx, scope, plan)

	if cacheable {
		if cached, ok := e.cache.Get(ctx, key); ok {
			return e.serveFromCache(ctx, scope, plan, cached)
		}
	} else if e.cache != nil {
		e.cache.countUncached(ctx)
	}

	// 5. Admission, and it sits *after* the cache on purpose: a hit opens
	// nothing and uses none of the source's capacity, so making it queue for
	// capacity it will not use would be a queue that punishes the fast path.
	release, err := e.admit(ctx, scope, plan)
	if err != nil {
		return nil, err
	}

	// 6 and 7. Execute and stream.
	execution, err := e.execute(ctx, scope, plan, key, cacheable, release)
	if err != nil {
		release()

		return nil, err
	}

	return execution, nil
}

/*
admit takes a slot for this query, or explains why it may not run yet.

A no-op when no governor is configured, returning a release that does nothing,
so that everything downstream has exactly one shape to handle rather than a
nil check at every exit.
*/
func (e *Executor) admit(
	ctx context.Context, scope tenant.Scope, plan Plan,
) (func(), error) {
	if e.governor == nil {
		return func() {}, nil
	}

	// Background work has no actor, so it is governed as one caller rather
	// than as a different caller each time -- otherwise every scheduled job
	// would get a fresh per-user allowance and the per-user limit would mean
	// nothing for exactly the traffic that runs unattended.
	actor := scope.ActorID().UUID

	release, err := e.governor.Admit(ctx, scope.OrgID(), actor, plan.Connection.ID)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	return release, nil
}

/*
cacheKey resolves the caller's policy set and derives the key for this plan.

Returns false whenever anything at all is unclear -- the cache is off, the
fingerprint would not resolve, the key could not be built. Every one of those
costs a cache miss and nothing else, which is the cheap side of a decision
whose expensive side is somebody reading rows that were computed for a
different policy set.

A fingerprint that fails to resolve is logged rather than returned. The query
is still a perfectly good query; it simply will not be cached, and turning an
authorization-cache problem into a failed query would be a worse trade than
running it.
*/
func (e *Executor) cacheKey(ctx context.Context, scope tenant.Scope, plan Plan) (string, bool) {
	if e.cache == nil || e.granter == nil {
		return "", false
	}

	fingerprint, err := policy.Resolve(ctx, e.granter, scope)
	if err != nil {
		e.log.Warn("not caching: the caller's policy set could not be resolved",
			"connection", plan.Connection.Slug, "error", err)

		return "", false
	}

	return e.cache.Key(fingerprint, scope.OrgID(), plan.Connection.ID, plan.SQL, plan.MaxRows)
}

/*
serveFromCache answers without opening anything.

The execution is logged exactly as a miss is -- a row when it starts and the
outcome when the caller closes it -- so the query log accounts for every
execution rather than for the ones that happened to be slow. A cache that made
queries disappear from the log would make the log useless for the question it
exists to answer.
*/
func (e *Executor) serveFromCache(
	ctx context.Context, scope tenant.Scope, plan Plan, cached connectors.Stream,
) (*Execution, error) {
	started := e.now()

	entry, err := e.repos.QueryLog.Start(ctx, repo.QueryStart{
		ConnectionID: plan.Connection.ID,
		UserID:       scope.ActorID(),
		SQL:          plan.SQL,
		At:           started,
		Owner:        e.owner,
	})
	if err != nil {
		_ = cached.Close()

		return nil, fmt.Errorf("query: record the start: %w", err)
	}

	return &Execution{
		LogID:       entry.ID,
		Plan:        plan,
		CacheStatus: CacheHit,
		Stream: &loggedStream{
			Stream:      cached,
			executor:    e,
			entry:       entry,
			started:     started,
			cacheStatus: CacheHit,
			ctx:         ctx,
		},
	}, nil
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

	// Resolved here rather than left for the connector, because zero and
	// [connectors.DefaultMaxRows] are the same cap and the cache key cannot
	// tell: a connection edited from 0 to 100000 would change every key it
	// derives while changing nothing about any answer.
	if maxRows <= 0 {
		maxRows = connectors.DefaultMaxRows
	}

	return Plan{SQL: statement, Connection: conn, MaxRows: maxRows}, nil
}

// execute opens the source, records the start and hands back the stream.
func (e *Executor) execute(
	ctx context.Context, scope tenant.Scope, plan Plan,
	key string, cacheable bool, release func(),
) (*Execution, error) {
	cfg := configFor(plan.Connection)
	cfg.MaxRows = plan.MaxRows
	cfg.QueryTimeoutSeconds = e.effectiveTimeout(plan.Connection)

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
		Owner:        e.owner,
	})
	if err != nil {
		_ = connector.Close()

		// Refused rather than run unlogged. A query Pivot cannot account for
		// is the one an operator most needs accounted for, and "the audit
		// trail is optional when the database is busy" is not a property
		// anybody can rely on afterwards.
		return nil, fmt.Errorf("query: record the start: %w", err)
	}

	/*
		A context of this query's own, derived from the caller's.

		The caller's cancellation still reaches it -- that is what derived
		means -- and now so does anybody holding the monitor. Canceling with a
		cause is what lets the log tell "an administrator stopped this" from
		"the caller left", which arrive identically otherwise.

		Registered before the statement is sent, so a kill that arrives while
		the source is still deciding whether to answer is not too early to
		land.
	*/
	queryCtx, cancel := context.WithCancelCause(ctx)
	unwatch := e.monitor.watch(entry.ID, cancel)

	stream, err := connector.Stream(queryCtx, plan.SQL)
	if err != nil {
		unwatch()
		cancel(nil)
		e.finish(ctx, entry, started, nil, err)
		_ = connector.Close()

		return nil, fmt.Errorf("query: execute on %s: %w", plan.Connection.Slug, err)
	}

	status := CacheUncached
	if cacheable {
		status = CacheMiss
	}

	return &Execution{
		LogID:       entry.ID,
		Plan:        plan,
		CacheStatus: status,
		Stream: &loggedStream{
			Stream:      stream,
			executor:    e,
			entry:       entry,
			started:     started,
			connector:   connector,
			cacheStatus: status,
			cacheKey:    key,
			caching:     cacheable,
			release:     release,
			unwatch:     unwatch,
			cancel:      cancel,
			// The context the query actually ran under. Derived from the
			// caller's, so it carries the same tenant scope, and unlike the
			// caller's it carries the *cause* -- which is the only way to
			// tell an administrator's kill from a caller walking away once
			// the connector has wrapped both into its own canceled error.
			ctx: queryCtx,
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
		State:       repo.StateSucceeded,
		At:          finished,
		Duration:    finished.Sub(started),
		CacheStatus: CacheUncached,
	}

	if s != nil {
		out.Rows = s.rows
		out.BytesEstimated = s.bytes
		out.Truncated = s.Truncated()
		out.CacheStatus = s.cacheStatus
	}

	switch {
	case cause != nil && errors.Is(context.Cause(ctx), ErrKilled):
		/*
			Killed, and recorded as its own thing.

			Read from the context's cause rather than from the error the stream
			returned, because by the time a cancellation has been through the
			connector it is a connectors.Error saying "the query was canceled"
			-- true of both a kill and a closed browser tab, and useless for
			telling them apart. The cause is the only place the distinction
			survives.
		*/
		out.State = repo.StateCanceled
		out.Err = ErrKilled.Error()
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

/*
effectiveTimeout is the smaller of the organization's ceiling and the
connection's own, in whole seconds.

The smaller, always. An operator setting an instance-wide bound is saying "no
query may run longer than this", and a ceiling that could be overridden upward
by editing a connection would not be a bound at all. A connection that was
deliberately given a *shorter* timeout keeps it.

Whole seconds because that is what [connectors.Config] takes, and a ceiling
rounded down is still a ceiling. A sub-second ceiling rounds to zero, which
would mean "no limit" -- so it is clamped to one second instead, since an
operator who asked for less than a second did not mean unlimited.
*/
func (e *Executor) effectiveTimeout(conn model.Connection) int {
	ceiling := int(e.queryTimeout.Seconds())

	if e.queryTimeout > 0 && ceiling == 0 {
		ceiling = 1
	}

	own := int(conn.QueryTimeoutSeconds)

	switch {
	case ceiling == 0:
		return own
	case own == 0 || ceiling < own:
		return ceiling
	default:
		return own
	}
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

/*
Column describes one column of a result, without the caller having to know a
connector exists.

The pipeline is the single door (Part 20-b), and a door that hands back
connector types is not one: every caller then imports the package the door was
built to stand in front of, and the structural test that keeps them out fails
for the one caller that is supposed to be there.

So this is a small, deliberate copy rather than an alias. It carries both the
canonical kind and the source's own spelling, because a client formats on the
first and a person debugging wants the second -- which is why Part 19-a kept
the spelling at all.
*/
type Column struct {
	Name       string
	Type       string
	SourceType string
	Nullable   bool
}

// Columns describes the result's shape. Valid before the first read.
func (e *Execution) Columns() []Column {
	source := e.Stream.Columns()
	out := make([]Column, 0, len(source))

	for _, c := range source {
		out = append(out, Column{
			Name:       c.Name,
			Type:       string(c.Type.Kind),
			SourceType: c.SourceType,
			Nullable:   c.Nullable,
		})
	}

	return out
}

/*
SourceMessage extracts what the source itself said about a failure.

"syntax error at or near FROM" is the whole answer to why a query failed, and
anything that paraphrases it loses the only useful part. Exported here for the
same reason [Column] is: the caller must be able to get at it without reaching
past the pipeline into the connector package.

Falls back to the error's own text when the failure came from somewhere other
than a source, so a caller always has something to show.
*/
func SourceMessage(err error) string {
	var connErr *connectors.Error
	if errors.As(err, &connErr) && connErr.Message != "" {
		return connErr.Message
	}

	if err == nil {
		return ""
	}

	return err.Error()
}
