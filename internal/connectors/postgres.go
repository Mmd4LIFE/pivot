package connectors

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

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
			return Errorf(ReasonSyntax, err, "", "PostgreSQL could not parse the query")

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

		// A code nobody has mapped yet. Reported as unknown *with the code*,
		// so the next person to see it has something to look up rather than a
		// sentence somebody wrote once.
		return Errorf(ReasonUnknown, err, "",
			"PostgreSQL reported %s: %s", pgErr.Code, pgErr.Message)
	}

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
