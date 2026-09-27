package connectors

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
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
}

// SQLConnector implements [Connector] over database/sql.
type SQLConnector struct {
	dialect Dialect
	cfg     Config
	db      *sql.DB
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

	return &SQLConnector{dialect: dialect, cfg: cfg, db: db}, nil
}

// Kind is which implementation this is.
func (c *SQLConnector) Kind() Kind { return c.dialect.Kind() }

// Capabilities is what its dialect can do.
func (c *SQLConnector) Capabilities() Capabilities { return c.dialect.Capabilities() }

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

	rows, err := c.db.QueryContext(ctx, c.dialect.IntrospectQuery())
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
			Name: column, SourceType: sourceType, Nullable: nullable, Position: position,
		})
	}

	if rerr := rows.Err(); rerr != nil {
		return nil, c.classify(ctx, rerr)
	}

	return tables, nil
}

// Query runs SQL and returns the rows.
func (c *SQLConnector) Query(ctx context.Context, query string, args ...any) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()

	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, c.classify(ctx, err)
	}

	defer func() { _ = rows.Close() }()

	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, c.classify(ctx, err)
	}

	result := &Result{Columns: make([]Column, 0, len(types))}

	for i, t := range types {
		nullable, known := t.Nullable()

		result.Columns = append(result.Columns, Column{
			Name: t.Name(), SourceType: t.DatabaseTypeName(),
			// A driver that will not say is reported as nullable, because
			// assuming NOT NULL and being wrong is a panic on a nil scan.
			Nullable: nullable || !known,
			Position: i + 1,
		})
	}

	limit := c.maxRows()

	for rows.Next() {
		if int64(len(result.Rows)) >= limit {
			// Stopped *and* flagged. A result that is silently cut is a wrong
			// answer presented as a right one, and the chart drawn from it is
			// wrong in a way nobody can see.
			result.Truncated = true

			break
		}

		row, serr := scanRow(rows, len(types))
		if serr != nil {
			return nil, c.classify(ctx, serr)
		}

		result.Rows = append(result.Rows, row)
	}

	if rerr := rows.Err(); rerr != nil {
		return nil, c.classify(ctx, rerr)
	}

	return result, nil
}

// Close releases the pool.
func (c *SQLConnector) Close() error {
	if err := c.db.Close(); err != nil {
		return fmt.Errorf("close %s connection: %w", c.dialect.Kind(), err)
	}

	return nil
}

// scanRow reads one row into a slice of any.
//
// Through *any rather than typed destinations, because the column types are
// not known until runtime. Byte slices are copied to string: the driver may
// reuse the buffer on the next call to Next, which turns a retained []byte
// into a value that changes underneath its owner.
func scanRow(rows *sql.Rows, n int) ([]any, error) {
	cells := make([]any, n)
	targets := make([]any, n)

	for i := range cells {
		targets[i] = &cells[i]
	}

	if err := rows.Scan(targets...); err != nil {
		return nil, err
	}

	for i, cell := range cells {
		if raw, ok := cell.([]byte); ok {
			cells[i] = string(raw)
		}
	}

	return cells, nil
}

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
