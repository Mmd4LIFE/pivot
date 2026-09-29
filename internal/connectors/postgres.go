package connectors

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/Mmd4LIFE/pivot/internal/datatype"

	"github.com/jackc/pgx/v5/pgconn"

	// The database/sql driver this connector opens. Imported here rather than
	// relied upon from internal/store: a driver registers itself on import,
	// and depending on another package happening to import it means this
	// package works until somebody stops using that one.
	_ "github.com/jackc/pgx/v5/stdlib"
)

/*
PostgreSQL: the reference connector.

It is first because it is the one whose behavior everything else is measured
against, and because Pivot's own metadata store already speaks it — the driver
is in the binary either way.

What this file has to get right is the part that is not database/sql: the DSN,
the capability declarations the query compiler will read, and turning
PostgreSQL's errors into something the person who typed the hostname can act
on.
*/

// DefaultPostgresPort is used when a configuration leaves the port at zero.
const DefaultPostgresPort = 5432

func init() {
	Register(KindPostgres, func(cfg Config) (Connector, error) {
		return OpenSQL(postgresDialect{}, cfg)
	})
}

type postgresDialect struct{}

func (postgresDialect) Kind() Kind { return KindPostgres }

// DriverName is pgx's database/sql shim, which the metadata store already
// imports — so this connector costs no new dependency.
func (postgresDialect) DriverName() string { return "pgx" }

func (postgresDialect) Validate(cfg Config) error {
	var missing []string

	if strings.TrimSpace(cfg.Host) == "" {
		missing = append(missing, "host")
	}

	if strings.TrimSpace(cfg.Database) == "" {
		missing = append(missing, "database")
	}

	if strings.TrimSpace(cfg.Username) == "" {
		missing = append(missing, "username")
	}

	if len(missing) > 0 {
		return Errorf(ReasonUnknown, nil, "",
			"a PostgreSQL connection needs %s", strings.Join(missing, ", "))
	}

	if cfg.Port < 0 || cfg.Port > 65535 {
		return Errorf(ReasonUnknown, nil, "", "port %d is not a port", cfg.Port)
	}

	switch cfg.SSLMode {
	case "", "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return Errorf(ReasonTLS, nil,
			"one of: disable, allow, prefer, require, verify-ca, verify-full",
			"%q is not a PostgreSQL sslmode", cfg.SSLMode)
	}

	return nil
}

// DSN builds a connection URL.
//
// Built with net/url rather than by concatenation, so a password containing a
// colon, an at sign or a slash is escaped rather than producing a DSN that
// parses as a different host entirely. That is not hypothetical: a generated
// password contains those characters roughly a third of the time.
func (postgresDialect) DSN(cfg Config) (string, error) {
	port := cfg.Port
	if port == 0 {
		port = DefaultPostgresPort
	}

	sslMode := cfg.SSLMode
	if sslMode == "" {
		// Not "disable". A default that silently sends credentials in the
		// clear is the wrong default; `prefer` uses TLS when the server offers
		// it, which every managed PostgreSQL does.
		sslMode = "prefer"
	}

	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.Username, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(port)),
		Path:   "/" + cfg.Database,
	}

	q := url.Values{}
	q.Set("sslmode", sslMode)

	// Named so that somebody reading `pg_stat_activity` on the far side can
	// tell which product is holding their connections.
	q.Set("application_name", "pivot")

	for key, value := range cfg.Options {
		q.Set(key, value)
	}

	u.RawQuery = q.Encode()

	return u.String(), nil
}

func (postgresDialect) Capabilities() Capabilities {
	return Capabilities{
		WindowFunctions: true,
		CTEs:            true,
		LateralJoins:    true,
		Placeholder:     PlaceholderDollar,

		QuoteIdentifier: quotePostgresIdentifier,

		// 63 bytes, and it truncates silently rather than erroring — so two
		// generated aliases that differ after the 63rd character become one
		// column, and the second overwrites the first.
		MaxIdentifierLength: 63,

		// pgx sends a cancellation request on its own connection when the
		// context ends, which stops the query at the server rather than only
		// abandoning the client.
		SupportsCancel: true,
	}
}

// quotePostgresIdentifier wraps an identifier in double quotes.
//
// An embedded double quote is doubled, which is the whole of the escaping rule
// and the whole of the injection risk: an identifier that ends its own quoting
// can start a statement.
func quotePostgresIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

/*
Classify turns PostgreSQL's errors into something actionable.

On SQLSTATE where there is one, because the codes are stable across versions
and the messages are not. The network failures have no SQLSTATE — the
connection never got far enough to have one — so those are classified from the
error types net gives us, which is the closest thing to a code available.
*/
func (postgresDialect) Classify(err error) *Error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		/*
			The position, attached once for everything PostgreSQL classified.

			Once rather than per branch, because which codes carry a position
			is PostgreSQL's business and not a list worth maintaining here.
			This was originally wired into the two branches that looked like
			parse errors, and it missed 42703 -- an undefined column, which is
			the most common typo there is and the case the underline is most
			useful for. Found by running it.

			Position is zero unless the server set it, so this is a no-op for
			the errors that have no position, which is most of them.
		*/
		return at(pgErr, classifyPostgres(pgErr, err))
	}

	return classifyPostgresNetwork(err)
}

/*
at attaches the position PostgreSQL reported, if it reported one.

Separate from [Errorf] because it is a PostgreSQL-only fact. Every other source
Pivot speaks to gives no position at all -- MySQL's protocol has no field for
it, and SQLite and DuckDB parse in this process and still do not offer one --
so building it into the shared constructor would suggest a generality that does
not exist.
*/
func at(pgErr *pgconn.PgError, e *Error) *Error {
	if pgErr.Position > 0 {
		e.Position = int(pgErr.Position)
	}

	return e
}

// classifyPostgres maps a SQLSTATE onto something actionable.
func classifyPostgres(pgErr *pgconn.PgError, err error) *Error {
	switch pgErr.Code {
	case "28P01", "28000":
		return Errorf(ReasonAuth, err,
			"check the username and password",
			"PostgreSQL refused those credentials")

	case "3D000":
		return Errorf(ReasonNoDatabase, err,
			"check the database name; the server is reachable and the credentials work",
			"that database does not exist on this server")

	case "42501":
		return Errorf(ReasonPermission, err,
			"the account authenticated but is not allowed to do this",
			"permission denied by PostgreSQL")

	case "42601":
		// The only place any source tells Pivot *where* it stopped
		// reading, so the editor can underline it rather than saying
		// something went wrong somewhere in ten lines.
		return Errorf(ReasonSyntax, err, "",
			"PostgreSQL could not parse the query")

	case "42P01":
		return Errorf(ReasonSyntax, err,
			"check the schema and the search_path",
			"no such table")

	case "57014":
		return Errorf(ReasonCanceled, err, "", "the query was canceled")

	case "53300":
		return Errorf(ReasonUnreachable, err,
			"the server is out of connection slots; lower this connection's pool size",
			"PostgreSQL refused the connection: too many clients")
	}

	// A code nobody has mapped yet. Reported as unknown *with the code*, so
	// the next person to see it has something to look up rather than a
	// sentence somebody wrote once.
	return Errorf(ReasonUnknown, err, "",
		"PostgreSQL reported %s: %s", pgErr.Code, pgErr.Message)
}

// classifyPostgresNetwork handles the failures that never reached a server, so
// have no SQLSTATE to classify from.
func classifyPostgresNetwork(err error) *Error {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return Errorf(ReasonUnreachable, err,
			"check the hostname, and whether this Pivot can resolve it",
			"the host %q does not resolve", dnsErr.Name)
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Timeout() {
			return Errorf(ReasonUnreachable, err,
				"check the port and any firewall between this Pivot and the server",
				"the connection timed out")
		}

		return Errorf(ReasonUnreachable, err,
			"check the port, and that PostgreSQL is listening on it",
			"the connection was refused")
	}

	// TLS failures arrive as ordinary errors with recognizable text. Matching
	// on the text is unlovely and is the only handle there is -- and getting
	// it wrong costs a less specific message rather than a wrong one.
	if text := err.Error(); strings.Contains(text, "tls") ||
		strings.Contains(text, "certificate") ||
		strings.Contains(text, "SSL") {
		return Errorf(ReasonTLS, err,
			"check sslmode, and whether the server's certificate is trusted here",
			"the TLS handshake failed")
	}

	// Not recognized. Returning nil hands it to the shared classifier, which
	// knows about cancellation and timeouts.
	return nil
}

/*
IntrospectQuery lists columns across the schemas worth showing.

From information_schema rather than the pg_catalog tables: it is standard, it
is what the next three connectors will also use, and the performance difference
does not matter for a query that runs on a schedule.

The system schemas are excluded because nobody connects a BI tool to browse
pg_catalog, and including them buries the twelve tables somebody cares about
under four hundred they do not.

Ordered so that a table's columns arrive together and in declaration order,
which is what lets the scanner group them without buffering the whole result.
*/
func (postgresDialect) IntrospectQuery() string {
	return `
SELECT c.table_schema,
       c.table_name,
       CASE t.table_type WHEN 'VIEW' THEN 'view' ELSE 'table' END AS table_type,
       c.column_name,
       c.data_type,
       c.is_nullable = 'YES' AS nullable,
       c.ordinal_position
FROM information_schema.columns c
JOIN information_schema.tables t
  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
WHERE c.table_schema NOT IN ('pg_catalog', 'information_schema')
  AND t.table_type IN ('BASE TABLE', 'VIEW')
ORDER BY c.table_schema, c.table_name, c.ordinal_position`
}

/*
NormalizeType maps PostgreSQL's type names onto Pivot's.

Two vocabularies reach here for the same column. `information_schema` says
"timestamp with time zone" and "double precision"; pgx says "TIMESTAMPTZ" and
"FLOAT8". Most of both are in [datatype.Base]; what is below is what
PostgreSQL alone spells, plus the two cases where pgx gives up.

For `timetz` and `money` pgx has no name at all and reports the type's OID --
"1266" and "790". Mapped here rather than left Unknown, because a column whose
canonical type depends on which code path asked is worse than one nobody has
mapped: the catalog would say Time and a query result would say Unknown for
the same column, and the disagreement would be invisible.
*/
func (postgresDialect) NormalizeType(sourceType string) datatype.Type {
	return datatype.Normalize(sourceType, func(name string) (datatype.Type, bool) {
		switch name {
		case "time with time zone", "timetz", "1266":
			return datatype.Type{Kind: datatype.Time}, true

		case "790":
			// money. Exact, and PostgreSQL formats it by locale -- which is
			// the reason it is Decimal rather than String.
			return datatype.Type{Kind: datatype.Decimal}, true

		case "name", "citext", "xml":
			return datatype.Type{Kind: datatype.String}, true

		case "serial", "serial4":
			return datatype.Type{Kind: datatype.Integer, Bits: 32}, true

		case "bigserial", "serial8":
			return datatype.Type{Kind: datatype.Integer, Bits: 64}, true

		case "oid":
			return datatype.Type{Kind: datatype.Integer, Bits: 32}, true

		case "user-defined", "composite", "record":
			// An enum, a domain or a composite. Not Unknown: the catalog is
			// telling us it is a shape rather than refusing to say.
			return datatype.Type{Kind: datatype.Struct}, true
		}

		return datatype.Type{}, false
	})
}

/*
ForeignKeyQuery lists relationships from pg_catalog rather than information_schema.

The standard query -- joining table_constraints to key_column_usage and
constraint_column_usage -- is wrong, and wrong in the worst way: it *crosses*
the two column lists instead of pairing them. A two-column key comes back as
four rows, each local column paired with each referenced one, and a
relationship built from it joins on columns that were never related. It
returns rows, so nothing looks broken.

Measured on a real PostgreSQL before this was written. pg_catalog carries the
two lists as parallel arrays, and `unnest(conkey, confkey) WITH ORDINALITY`
walks them together -- which is the whole fix.
*/
func (postgresDialect) ForeignKeyQuery() string {
	return `
SELECT c.conname                AS constraint_name,
       child_ns.nspname         AS from_schema,
       child.relname            AS from_table,
       child_col.attname        AS from_column,
       parent_ns.nspname        AS to_schema,
       parent.relname           AS to_table,
       parent_col.attname       AS to_column,
       pair.ord::int            AS ordinal
FROM pg_constraint c
JOIN pg_class child            ON child.oid = c.conrelid
JOIN pg_namespace child_ns     ON child_ns.oid = child.relnamespace
JOIN pg_class parent           ON parent.oid = c.confrelid
JOIN pg_namespace parent_ns    ON parent_ns.oid = parent.relnamespace
JOIN LATERAL unnest(c.conkey, c.confkey) WITH ORDINALITY AS pair(local, ref, ord) ON TRUE
JOIN pg_attribute child_col    ON child_col.attrelid = c.conrelid AND child_col.attnum = pair.local
JOIN pg_attribute parent_col   ON parent_col.attrelid = c.confrelid AND parent_col.attnum = pair.ref
WHERE c.contype = 'f'
  AND child_ns.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY child_ns.nspname, child.relname, c.conname, pair.ord`
}
