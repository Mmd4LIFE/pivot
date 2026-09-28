package connectors

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/datatype"
)

/*
The database/sql half of a connector.

Most sources Pivot will ever read speak database/sql: PostgreSQL, MySQL,
SQLite, Snowflake, Redshift, ClickHouse. Pooling, row scanning, truncation and
timeout handling are identical for all of them, and writing them per connector
is how six connectors end up with six subtly different ideas of what a
truncated result is.

So a driver supplies only what actually differs — the DSN, the capabilities,
how to read this source's catalog, and how to turn its errors into something a
person can act on — and this type does the rest.

It is not the Connector interface. BigQuery is not database/sql-shaped, and an
interface that assumed it was would have to be broken to add one. This is a
helper that implements the interface, not the interface itself.
*/

// Defaults applied when a Config leaves a limit at zero.
//
// A connection created before a limit existed should not pin itself to
// whatever that limit happened to be on the day, so zero means "ask now"
// rather than being resolved at creation.
const (
	DefaultMaxOpenConns = 5
	DefaultQueryTimeout = 30 * time.Second
	DefaultMaxRows      = 100_000
)

// Dialect is what a database/sql-backed connector has to supply.
type Dialect interface {
	// Kind identifies it.
	Kind() Kind

	// DriverName is the registered database/sql driver.
	DriverName() string

	// Validate rejects a configuration this source cannot use, before anything
	// tries to connect. A missing host is worth saying plainly rather than as
	// a dial error.
	Validate(Config) error

	// DSN builds the connection string. It receives the password and must not
	// log or return it in an error.
	DSN(Config) (string, error)

	// Capabilities is what the dialect can do.
	Capabilities() Capabilities

	// Classify turns a driver error into one a person can act on. Returning
	// nil means "I do not recognize this", and the shared classifier has a go.
	Classify(error) *Error

	// IntrospectQuery returns SQL listing columns across the schemas worth
	// showing, and the order its rows arrive in. Part 19 replaces this with
	// something richer; it is here so Introspect is real rather than a stub.
	IntrospectQuery() string

	/*
		NormalizeType turns one of this source's type names into a canonical
		one.

		Per dialect because the names are, and because the same source says it
		two ways: PostgreSQL's catalog reports "timestamp with time zone" and
		its driver reports "TIMESTAMPTZ" for the same column. Both arrive here
		and both have to come out the same.

		A name the dialect does not recognize should fall through to
		[datatype.Normalize], which knows the spellings everybody shares.
	*/
	NormalizeType(sourceType string) datatype.Type

	/*
		ForeignKeyQuery returns SQL listing every declared relationship, one row
		per column of each key, in this column order:

			constraint name, from schema, from table, from column,
			to schema, to table, to column, ordinal

		Ordered by constraint and then ordinal, so a composite key's columns
		arrive together and in key order.

		The ordinal is not decoration. Every one of these sources exposes a
		foreign key as two column lists, and the obvious join pairs every column
		of one with every column of the other -- which for a two-column key
		yields four rows instead of two, and a relationship that joins on the
		wrong columns. Measured, not imagined: the standard information_schema
		query does exactly that on PostgreSQL.

		An empty string means the source cannot report relationships, and
		[SQLConnector.ForeignKeys] answers [ErrNoForeignKeys].
	*/
	ForeignKeyQuery() string
}

/*
Canceler is a dialect that can stop a query at the server.

database/sql cancellation only reaches the driver. For most drivers ending the
context closes the client's socket and returns; whether the server notices is
between the server and the protocol, and it is not a given. PostgreSQL's driver
sends a cancellation request on a connection of its own, so the query stops.
MySQL's does not: a canceled `SELECT SLEEP(20)` was measured still holding a
server thread two seconds after the client had gone, and it would have held it
for the full twenty.

That is the difference between cancellation and walking away, and it is the
difference a BI tool feels most -- somebody closes a tab and the warehouse
keeps the query. A dialect that has to say so out of band implements this.

A dialect that does not implement it is not broken; it declares
[Capabilities.SupportsCancel] false and the cost is understood rather than
assumed away.
*/
type Canceler interface {
	// SessionID is what the server calls this connection.
	//
	// It runs on the connection the query is about to use, which costs one
	// round trip per query. That is the price of being able to stop one, and
	// it is paid only by dialects that need it.
	SessionID(ctx context.Context, conn *sql.Conn) (int64, error)

	// KillQuery stops whatever that session is running, over a connection of
	// its own.
	//
	// It must stop the statement and not the session: the connection is
	// pooled and is expected to be usable afterwards.
	KillQuery(ctx context.Context, db *sql.DB, session int64) error
}

// killTimeout caps how long a cancellation may take to deliver.
//
// Short on purpose. The caller has already stopped waiting, and the connection
// being killed is held open until the kill returns.
const killTimeout = 5 * time.Second

// querier is the part of *sql.DB that running a query needs. *sql.Conn
// satisfies it too, which is what lets one code path serve both the pool and a
// connection held for cancellation.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// SQLConnector implements [Connector] over database/sql.
type SQLConnector struct {
	dialect Dialect
	cfg     Config
	db      *sql.DB

	// control is a second, one-connection pool used only to deliver
	// cancellations, and nil for a dialect that does not need one. See
	// OpenSQL for why it is not simply another connection from db.
	control *sql.DB
}

// OpenSQL builds a pooled connector for a dialect.
//
// It does not dial: database/sql pools are lazy. That is deliberate — an Open
// that reached the network would make rendering a list of connections a round
// trip to every warehouse in the organization.
func OpenSQL(dialect Dialect, cfg Config) (*SQLConnector, error) {
	if err := dialect.Validate(cfg); err != nil {
		return nil, err
	}

	dsn, err := dialect.DSN(cfg)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(dialect.DriverName(), dsn)
	if err != nil {
		// The DSN is never included -- it carries the password -- but the
		// driver's own text is, because at this point the only thing that can
		// have failed is the driver name, and "could not prepare a connection"
		// with nothing after it sent me looking in the wrong place for ten
		// minutes.
		return nil, Errorf(ReasonUnknown, err, "",
			"could not prepare a %s connection: %s", dialect.Kind(), err)
	}

	maxOpen := cfg.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = DefaultMaxOpenConns
	}

	db.SetMaxOpenConns(maxOpen)

	// Half the pool idle, so a burst does not pay the handshake every time and
	// a quiet connection does not hold twenty sockets open against somebody's
	// warehouse.
	db.SetMaxIdleConns(max(1, maxOpen/2))

	// Recycled well inside the hour that most managed databases and load
	// balancers drop an idle connection at. A pooled connection killed at the
	// far end surfaces as a random failure on an unrelated query.
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	connector := &SQLConnector{dialect: dialect, cfg: cfg, db: db}

	if _, cancels := dialect.(Canceler); cancels {
		// A separate pool, holding one connection, rather than borrowing from
		// db. A kill that has to queue behind the queries it is trying to kill
		// is a deadlock, and the moment cancellation matters most -- every
		// slot busy with something somebody wants stopped -- is exactly when
		// the query pool has nothing left to give.
		control, cerr := sql.Open(dialect.DriverName(), dsn)
		if cerr != nil {
			return nil, Errorf(ReasonUnknown, cerr, "",
				"could not prepare a %s control connection: %s", dialect.Kind(), cerr)
		}

		control.SetMaxOpenConns(1)
		control.SetMaxIdleConns(1)

		// Released after a minute of nobody canceling anything, so a quiet
		// connection does not hold a socket open against somebody's server
		// forever for a cancellation that never comes.
		control.SetConnMaxIdleTime(time.Minute)
		control.SetConnMaxLifetime(30 * time.Minute)

		connector.control = control
	}

	return connector, nil
}

// Kind is which implementation this is.
func (c *SQLConnector) Kind() Kind { return c.dialect.Kind() }

// Capabilities is what its dialect can do.
func (c *SQLConnector) Capabilities() Capabilities { return c.dialect.Capabilities() }

// NormalizeType says what one of this source's type names means.
func (c *SQLConnector) NormalizeType(sourceType string) datatype.Type {
	return c.dialect.NormalizeType(sourceType)
}

// DB exposes the pool, for a dialect that needs to run something of its own.
func (c *SQLConnector) DB() *sql.DB { return c.db }

// Test reaches the source and comes back.
func (c *SQLConnector) Test(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()

	if err := c.db.PingContext(ctx); err != nil {
		return c.classify(ctx, err)
	}

	return nil
}

// Introspect lists what is in the source.
func (c *SQLConnector) Introspect(ctx context.Context) ([]Table, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()

	conn, release, err := c.borrow(ctx)
	if err != nil {
		return nil, c.classify(ctx, err)
	}

	defer release()

	rows, err := conn.QueryContext(ctx, c.dialect.IntrospectQuery())
	if err != nil {
		return nil, c.classify(ctx, err)
	}

	defer func() { _ = rows.Close() }()

	// Accumulated in order, which the query guarantees, so a table's columns
	// arrive together and position is the order they were declared in.
	var (
		tables  []Table
		current *Table
	)

	for rows.Next() {
		var (
			schema, table, tableType, column, sourceType string
			nullable                                     bool
			position                                     int
		)

		if serr := rows.Scan(
			&schema, &table, &tableType, &column, &sourceType, &nullable, &position,
		); serr != nil {
			return nil, c.classify(ctx, serr)
		}

		if current == nil || current.Schema != schema || current.Name != table {
			tables = append(tables, Table{
				Schema: schema, Name: table, Type: TableType(tableType),
			})
			current = &tables[len(tables)-1]
		}

		current.Columns = append(current.Columns, Column{
			Name:       column,
			SourceType: sourceType,
			Type:       c.dialect.NormalizeType(sourceType),
			Nullable:   nullable,
			Position:   position,
		})
	}

	if rerr := rows.Err(); rerr != nil {
		return nil, c.classify(ctx, rerr)
	}

	return tables, nil
}

/*
Query runs SQL and returns the whole result.

A loop over [SQLConnector.Stream], rather than a second implementation. The two
would otherwise drift on exactly the things that matter and are easy to get
subtly different -- what a truncated result is, whether a byte slice was copied
-- and the drift would show up as one code path being right.

Right for a catalog query and wrong for a large answer, which is what Stream is
for: this holds the entire result in memory on top of whatever the driver is
already holding.
*/
func (c *SQLConnector) Query(ctx context.Context, query string, args ...any) (*Result, error) {
	stream, err := c.Stream(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer func() { _ = stream.Close() }()

	result := &Result{Columns: stream.Columns()}

	for stream.Next() {
		// Copied, because Row is only valid until the next call to Next.
		row := make([]any, len(stream.Row()))
		copy(row, stream.Row())

		result.Rows = append(result.Rows, row)
	}

	if serr := stream.Err(); serr != nil {
		return nil, serr
	}

	result.Truncated = stream.Truncated()

	return result, nil
}

/*
ForeignKeys lists the relationships the source declares.

Read through the same borrowed connection as everything else, so a slow catalog
on a large schema is cancellable and does not outlive its context.
*/
func (c *SQLConnector) ForeignKeys(ctx context.Context) ([]ForeignKey, error) {
	query := c.dialect.ForeignKeyQuery()
	if query == "" {
		return nil, ErrNoForeignKeys
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()

	conn, release, err := c.borrow(ctx)
	if err != nil {
		return nil, c.classify(ctx, err)
	}

	defer release()

	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, c.classify(ctx, err)
	}

	defer func() { _ = rows.Close() }()

	var keys []ForeignKey

	for rows.Next() {
		var key ForeignKey

		if serr := rows.Scan(
			&key.Name,
			&key.FromSchema, &key.FromTable, &key.FromColumn,
			&key.ToSchema, &key.ToTable, &key.ToColumn,
			&key.Ordinal,
		); serr != nil {
			return nil, c.classify(ctx, serr)
		}

		keys = append(keys, key)
	}

	if rerr := rows.Err(); rerr != nil {
		return nil, c.classify(ctx, rerr)
	}

	return keys, nil
}

// Close releases the pools.
func (c *SQLConnector) Close() error {
	if c.control != nil {
		if err := c.control.Close(); err != nil {
			return fmt.Errorf("close %s control connection: %w", c.dialect.Kind(), err)
		}
	}

	if err := c.db.Close(); err != nil {
		return fmt.Errorf("close %s connection: %w", c.dialect.Kind(), err)
	}

	return nil
}

/*
borrow returns something to run a query on, and the teardown that releases it.

For a dialect that cannot stop a query at the server there is nothing to
arrange, so this is the pool itself and releasing it is a no-op. For one that
can, it is a connection of our own with a watcher bound to ctx -- because the
thing being killed is identified by session, and a session is only knowable if
the query is pinned to a connection we chose.
*/
func (c *SQLConnector) borrow(ctx context.Context) (querier, func(), error) {
	canceler, cancels := c.dialect.(Canceler)
	if !cancels {
		return c.db, func() {}, nil
	}

	conn, err := c.db.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}

	stop, err := c.watchCancel(ctx, canceler, conn)
	if err != nil {
		ignore(conn.Close())

		return nil, nil, err
	}

	return conn, func() {
		// Stopped before the connection goes back, which is what keeps a kill
		// already in flight from landing on whatever the pool hands out next.
		stop()
		ignore(conn.Close())
	}, nil
}

/*
watchCancel arranges for the server to be told when ctx ends.

The returned function tears the watcher down and waits for it. The wait is the
subtle part: a query that finishes at the same moment its context ends would
otherwise race its own cancellation, and a KILL that arrives after the
connection has gone back to the pool stops a query somebody else is running.
*/
func (c *SQLConnector) watchCancel(
	ctx context.Context, canceler Canceler, conn *sql.Conn,
) (func(), error) {
	session, err := canceler.SessionID(ctx, conn)
	if err != nil {
		return nil, err
	}

	var (
		done     = make(chan struct{})
		finished = make(chan struct{})
	)

	go func() {
		defer close(finished)

		select {
		case <-done:
			// The query finished on its own. Nothing to stop.

		case <-ctx.Done():
			// A context of its own. The caller's has just ended, and this is
			// the one piece of work that has to outlive it -- WithoutCancel
			// rather than Background so the trace still joins up.
			killCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), killTimeout)
			defer cancel()

			// Nothing useful to do with a failure: the caller has gone, there
			// is no request left to fail, and a server that just refused a
			// kill will refuse the retry.
			ignore(canceler.KillQuery(killCtx, c.control, session))
		}
	}()

	return func() {
		close(done)
		<-finished
	}, nil
}

// ignore states in one word that an error is deliberately discarded. errcheck
// rejects `_ = f()` across this repository, which is the right default; a call
// to this is the exception made visible rather than hidden behind a nolint.
func ignore(error) {}

func (c *SQLConnector) timeout() time.Duration {
	if c.cfg.QueryTimeoutSeconds > 0 {
		return time.Duration(c.cfg.QueryTimeoutSeconds) * time.Second
	}

	return DefaultQueryTimeout
}

func (c *SQLConnector) maxRows() int64 {
	if c.cfg.MaxRows > 0 {
		return c.cfg.MaxRows
	}

	return DefaultMaxRows
}

// classify turns a driver error into one a person can act on.
//
// The dialect goes first, because it knows its own SQLSTATE codes. What is
// left is the handful of failures that look the same everywhere: a context
// that ended, and whatever nobody recognized.
func (c *SQLConnector) classify(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	if specific := c.dialect.Classify(err); specific != nil {
		/*
			A source that reports its own cancellation cannot know why it was
			canceled, because only the context knows. The difference is the
			operator's: a timeout means raise the limit or make the query
			cheaper, and a cancellation means somebody walked away -- and
			reporting one as the other sends whoever is on call in the wrong
			direction.

			DuckDB is what surfaced this. It reports the same interrupt for
			both, so a query killed by its own deadline came back as
			"canceled". PostgreSQL's 57014 and MySQL's 1317 have exactly the
			same ambiguity; they passed only because their drivers happened to
			surface the context error instead.
		*/
		if specific.Reason == ReasonCanceled && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Errorf(ReasonTimeout, err,
				"raise the connection's query timeout, or make the query cheaper",
				"the query ran longer than %s", c.timeout())
		}

		return specific
	}

	// Checked after the dialect, because a source that reports its own
	// cancellation says something more useful than "context canceled".
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		return Errorf(ReasonTimeout, err,
			"raise the connection's query timeout, or make the query cheaper",
			"the query ran longer than %s", c.timeout())

	case errors.Is(err, context.Canceled), errors.Is(ctx.Err(), context.Canceled):
		return Errorf(ReasonCanceled, err, "", "the query was canceled")
	}

	return Errorf(ReasonUnknown, err, "", "the %s connection failed", c.dialect.Kind())
}
