// Package connectors holds the drivers for every data source Pivot reads.
//
// A connector connects to a source, reports what its dialect can do,
// introspects a schema, runs a query and can be told to stop. The interface is
// the load-bearing decision of Phase 1: the schema catalog, the execution
// pipeline, the SQL editor's autocomplete and every later connector are written
// against it, and the eighth connector is where a wrong shape is discovered and
// can no longer be changed.
//
// # Why hand-written drivers
//
// ADR-0001 chose Go over the JVM, which means forgoing JDBC's universal driver
// coverage. The mitigation is the conformance suite (Part 17): one shared test
// suite covering type mapping, NULL and timezone semantics, large-result
// streaming, cancellation propagation, error classification and unicode. A
// connector that does not pass it does not ship. Written once, every later
// connector is a week instead of a month.
//
// # What is deliberately not here yet
//
// Compiled query objects. The package comment promised connectors would accept
// those and never SQL strings, because that restriction is what keeps the
// semantic compiler the only path to data. The semantic layer is Phase 3, and
// building its object model now — with nothing to compile from and no query to
// run — would be inventing an abstraction against an imaginary caller. Part 20
// introduces a query type this interface takes instead of a string, and Phase 3
// makes the compiler the only thing that produces one.
package connectors

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Mmd4LIFE/pivot/internal/datatype"
)

// Kind identifies a connector implementation.
//
// It is stored in the database and named in configuration, so a value here is
// permanent: renaming one orphans every row that used it.
type Kind string

const (
	// KindPostgres is PostgreSQL, and by extension anything speaking its wire
	// protocol closely enough — Redshift gets its own Kind rather than reusing
	// this one, because the dialects differ where it matters.
	KindPostgres Kind = "postgres"

	// KindMySQL is MySQL 8.0 and later. MariaDB will get its own Kind for the
	// same reason: the wire protocol is shared and the dialects are not.
	KindMySQL Kind = "mysql"

	// KindSQLite is a SQLite file, opened read-only. The first source with no
	// server, and so the first whose Config is a path rather than an address.
	KindSQLite Kind = "sqlite"

	// KindDuckDB is an embedded DuckDB. It is behind the `duckdb` build tag
	// and is not in the default binary -- see ADR-0010 and [Absent].
	KindDuckDB Kind = "duckdb"
)

func (k Kind) String() string { return string(k) }

// Config is what it takes to reach a source.
//
// Separate fields rather than one DSN string. A DSN cannot be validated, cannot
// be redacted for display, cannot be shown half-filled in a form, and cannot
// have one field changed without parsing and rebuilding it. Settings that do
// not generalize across sources live in Options.
type Config struct {
	Kind Kind

	Host     string
	Port     int
	Database string
	Username string
	Password string

	// SSLMode is the source's own vocabulary — "require", "verify-full" — and
	// is validated by the driver rather than by a shared enum, because the
	// sources do not agree on the words.
	SSLMode string

	// Options carries what does not generalize: a Snowflake warehouse, a
	// BigQuery project. Unknown keys are the driver's business.
	Options map[string]string

	// MaxOpenConns caps the pool for this connection. Zero uses the default.
	//
	// Per connection rather than per instance, because the limit belongs to the
	// database at the other end: a warehouse with 20 slots and a laptop
	// Postgres do not want the same number.
	MaxOpenConns int

	// QueryTimeout caps a single query. Zero uses the default.
	QueryTimeoutSeconds int

	// MaxRows caps a result. Zero uses the default. A result that reaches it is
	// truncated *with a signal* rather than silently cut — see [Result].
	MaxRows int64
}

// Redacted returns the config with the password removed, for logs and errors.
//
// A method rather than a convention, because "remember not to log the config"
// is a convention that holds until somebody is debugging at 2am.
func (c Config) Redacted() Config {
	c.Password = ""

	return c
}

// Capabilities is what a dialect can do.
//
// Declared by each driver rather than discovered, because discovery means
// running probe queries against somebody's warehouse on every connect, and
// inferred means guessing. The query compiler reads this to decide what it may
// emit; a capability that lies produces SQL the source rejects.
type Capabilities struct {
	// WindowFunctions, CTEs and LateralJoins gate whole classes of generated
	// SQL.
	WindowFunctions bool
	CTEs            bool
	LateralJoins    bool

	// Placeholder is how a bound parameter is written: "$1" for PostgreSQL,
	// "?" for MySQL and SQLite. Held as a format rather than a boolean because
	// there are more than two answers.
	Placeholder PlaceholderStyle

	// QuoteIdentifier wraps an identifier for this dialect. It is a function
	// rather than a quote character because escaping the quote character
	// inside an identifier is part of the rule, and getting that wrong is an
	// injection.
	QuoteIdentifier func(string) string

	// MaxIdentifierLength is where the source truncates names. Postgres is 63.
	// Generated aliases have to respect it or two distinct columns collide
	// into one name.
	MaxIdentifierLength int

	// SupportsCancel says whether cancellation reaches the source rather than
	// merely abandoning the client. A source without it needs the timeout
	// enforced somewhere else, and pretending otherwise leaves queries running
	// after everybody has stopped waiting.
	SupportsCancel bool
}

// PlaceholderStyle is how a dialect writes a bound parameter.
type PlaceholderStyle string

const (
	// PlaceholderDollar is $1, $2 — PostgreSQL.
	PlaceholderDollar PlaceholderStyle = "dollar"

	// PlaceholderQuestion is ? — MySQL, SQLite.
	PlaceholderQuestion PlaceholderStyle = "question"
)

// Format renders the nth placeholder, counting from one.
func (p PlaceholderStyle) Format(n int) string {
	if p == PlaceholderDollar {
		return fmt.Sprintf("$%d", n)
	}

	return "?"
}

// Connector is an open, pooled handle to one data source.
//
// Obtained from [Open] and closed when the connection is deleted or the process
// ends. It is safe for concurrent use: the pool underneath it is the point.
type Connector interface {
	// Kind is which implementation this is.
	Kind() Kind

	// Capabilities is what its dialect can do. Constant for the lifetime of
	// the connector.
	Capabilities() Capabilities

	// Test reaches the source and comes back, or explains why it could not in
	// terms somebody can act on. See [Error].
	Test(ctx context.Context) error

	// Introspect lists what is in the source. Part 19 builds the catalog on
	// top of this; here it is deliberately shallow — schemas, tables, columns
	// and their source types.
	Introspect(ctx context.Context) ([]Table, error)

	// Query runs SQL and returns the rows.
	//
	// It takes a string today. Part 20 replaces that with a compiled query
	// type, and Phase 3 makes the semantic compiler the only thing that can
	// produce one. Leaving it a string until then is honest about what exists;
	// inventing the type now would be designing against an imaginary caller.
	Query(ctx context.Context, sql string, args ...any) (*Result, error)

	/*
		NormalizeType says what one of this source's type names means.

		Already applied to every [Column] this connector returns, so nothing
		reading a result or a catalog needs to call it. It is on the interface
		for the case that does: when Pivot learns a mapping it did not have,
		the stored catalog can be re-normalized from the source spellings it
		kept, without going back to somebody's warehouse to ask again.

		That is also why [Column.SourceType] is kept verbatim. The pair is the
		whole mechanism for fixing a type system in the field.
	*/
	NormalizeType(sourceType string) datatype.Type

	// Close releases the pool. A connector that is not closed when its
	// connection is deleted is a pool held against somebody's warehouse
	// forever.
	Close() error
}

// Table is one relation in the source.
type Table struct {
	Schema  string
	Name    string
	Type    TableType
	Comment string
	Columns []Column
}

// TableType distinguishes what can be read from what can be written.
type TableType string

const (
	TableTypeTable TableType = "table"
	TableTypeView  TableType = "view"
)

// Column is one column, with the source's own type name and Pivot's.
//
// SourceType is kept verbatim as well as normalized, because the normalization
// will be wrong about something and the original is the only way to find out
// what. [datatype.Type] carries it too, so the pair travels together.
type Column struct {
	Name       string
	SourceType string

	// Type is the canonical type. Unknown for a source type nobody has
	// mapped, which is a real answer rather than a failure -- see
	// [datatype.Unknown].
	Type datatype.Type

	Nullable bool
	Position int
	Comment  string
}

// Result is the outcome of a query.
type Result struct {
	Columns []Column
	Rows    [][]any

	// Truncated says the result hit MaxRows and is not the whole answer.
	//
	// A signal rather than a silent cut. A chart drawn from a truncated result
	// is a wrong chart, and the only thing worse than refusing to draw it is
	// drawing it with no indication.
	Truncated bool
}

// --- errors -----------------------------------------------------------------

// Reason classifies a connector failure into something a person can act on.
//
// The driver's own error text is written for whoever wrote the driver. "dial
// tcp: lookup db.internal: no such host" tells somebody familiar with Go
// exactly what happened and tells everybody else nothing.
type Reason string

const (
	// ReasonUnreachable is the host not resolving, or refusing, or timing out.
	ReasonUnreachable Reason = "unreachable"

	// ReasonAuth is credentials the source refused.
	ReasonAuth Reason = "auth"

	// ReasonNoDatabase is a database that does not exist on a reachable host.
	ReasonNoDatabase Reason = "no_database"

	// ReasonTLS is a TLS configuration the source or this client rejected.
	ReasonTLS Reason = "tls"

	// ReasonPermission is authentication that succeeded and authorization that
	// did not.
	ReasonPermission Reason = "permission"

	// ReasonSyntax is SQL the source would not parse.
	ReasonSyntax Reason = "syntax"

	// ReasonCanceled is a query stopped on purpose.
	ReasonCanceled Reason = "canceled"

	// ReasonTimeout is a query that ran out of time.
	ReasonTimeout Reason = "timeout"

	// ReasonUnknown is everything else, and is reported as such rather than
	// guessed at.
	ReasonUnknown Reason = "unknown"
)

// Error is a connector failure with a reason and something to do about it.
type Error struct {
	Reason Reason

	// Message is for the person who configured the connection.
	Message string

	// Hint is what to try. Empty when there is nothing honest to suggest.
	Hint string

	// Err is the driver's original error, kept for the log and never shown to
	// a caller: it can carry the host, the user, and occasionally the password
	// in a DSN.
	Err error
}

func (e *Error) Error() string {
	if e.Hint == "" {
		return e.Message
	}

	return e.Message + " (" + e.Hint + ")"
}

func (e *Error) Unwrap() error { return e.Err }

// Is lets errors.Is match on the reason alone: errors.Is(err, &Error{Reason: ReasonAuth}).
func (e *Error) Is(target error) bool {
	var other *Error
	if !errors.As(target, &other) {
		return false
	}

	return other.Reason == e.Reason && other.Message == ""
}

// Errorf builds a connector error.
func Errorf(reason Reason, cause error, hint, format string, args ...any) *Error {
	return &Error{
		Reason:  reason,
		Message: fmt.Sprintf(format, args...),
		Hint:    hint,
		Err:     cause,
	}
}

// --- registry ---------------------------------------------------------------

// Factory opens a connector for a configuration.
type Factory func(Config) (Connector, error)

// registry is the set of implementations this build has.
//
// A package-level map written only by init functions and read afterwards. It is
// not guarded, and does not need to be: registration happens during package
// initialization, which is single-threaded, and there is no path that adds a
// connector at runtime. If one ever appears, this needs a mutex and the comment
// is how the next person finds that out.
var registry = map[Kind]Factory{}

// Register adds an implementation. Called from a driver's init.
//
// Panics on a duplicate, because two factories for one kind means whichever
// package happened to initialize last wins — a coin toss decided by import
// order, discovered in production.
func Register(kind Kind, factory Factory) {
	if _, taken := registry[kind]; taken {
		panic("connectors: " + kind.String() + " is registered twice")
	}

	registry[kind] = factory
}

/*
absent records connectors this build was compiled without.

A kind can be missing for two very different reasons: nobody has written it, or
it exists and this binary does not contain it. Telling somebody "no connector
for \"duckdb\"" when the answer is "not in this build, and here is the build
that has it" wastes an afternoon.

Written only from init functions, like the registry above, and for the same
reason needs no lock.
*/
var absent = map[Kind]string{}

// RegisterAbsent records that this build does not contain a connector, and
// what to do about it. Called from the stub half of a build-tagged driver.
func RegisterAbsent(kind Kind, reason string) {
	absent[kind] = reason
}

// Kinds lists what this build can connect to, sorted.
func Kinds() []Kind {
	out := make([]Kind, 0, len(registry))
	for kind := range registry {
		out = append(out, kind)
	}

	// Sorted, so a help message and an error message list them the same way
	// twice running.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}

	return out
}

// Open builds a connector for a configuration.
//
// It does not connect: database/sql pools are lazy, and an Open that reached
// the network would make listing connections in a UI a round trip to every
// warehouse. [Connector.Test] is what reaches the source.
func Open(cfg Config) (Connector, error) {
	factory, ok := registry[cfg.Kind]
	if !ok {
		// Compiled out is not the same as nonexistent, and the difference is
		// the whole of what somebody needs to hear.
		if reason, known := absent[cfg.Kind]; known {
			return nil, Errorf(ReasonUnknown, nil, reason,
				"this build of Pivot was compiled without the %s connector", cfg.Kind)
		}

		return nil, Errorf(ReasonUnknown, nil,
			"this build supports "+kindList(),
			"no connector for %q", cfg.Kind)
	}

	return factory(cfg)
}

func kindList() string {
	kinds := Kinds()

	names := make([]string, 0, len(kinds))
	for _, k := range kinds {
		names = append(names, k.String())
	}

	return strings.Join(names, ", ")
}
