package connectors

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/Mmd4LIFE/pivot/internal/datatype"
)

/*
MySQL: the second connector, and the first real test of the interface.

PostgreSQL was written alongside the abstraction, so of course it fit. MySQL is
the one that says whether the abstraction was right, because it differs in
every way the interface claims to cover: `?` rather than `$1`, backticks rather
than double quotes, error numbers rather than SQLSTATEs, no generate_series,
and a driver that cancels by hanging up rather than by asking the server to
stop.

Three of those the interface already had a place for. The fourth needed
[Canceler], which is the one change this connector forced.

Targets MySQL 8.0 and later. The capabilities below declare CTEs, window
functions and lateral joins, all of which arrived in 8.0 — and the conformance
suite proves each of them against the server rather than taking this file's
word for it, so pointing this connector at 5.7 fails loudly rather than
producing wrong SQL later.
*/

// DefaultMySQLPort is used when a configuration leaves the port at zero.
const DefaultMySQLPort = 3306

func init() {
	Register(KindMySQL, func(cfg Config) (Connector, error) {
		return OpenSQL(mysqlDialect{}, cfg)
	})
}

type mysqlDialect struct{}

func (mysqlDialect) Kind() Kind { return KindMySQL }

func (mysqlDialect) DriverName() string { return "mysql" }

func (mysqlDialect) Validate(cfg Config) error {
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
			"a MySQL connection needs %s", strings.Join(missing, ", "))
	}

	if cfg.Port < 0 || cfg.Port > 65535 {
		return Errorf(ReasonUnknown, nil, "", "port %d is not a port", cfg.Port)
	}

	if _, err := mysqlTLS(cfg.SSLMode); err != nil {
		return err
	}

	for key := range cfg.Options {
		if reserved, why := mysqlReserved(key); reserved {
			return Errorf(ReasonUnknown, nil, why,
				"%q is not an option this connector will take", key)
		}
	}

	return nil
}

/*
mysqlReserved says whether an option is one this connector sets itself.

Refused rather than quietly overridden or quietly ignored. Each of these is
half of a pair that has to agree with the other half, and a configuration that
sets one of them without the other produces values that are wrong by a fixed
offset — the kind of wrong that looks like data rather than like a bug.
*/
func mysqlReserved(key string) (bool, string) {
	switch strings.ToLower(key) {
	case "time_zone", "loc":
		return true, "this connector pins the session to UTC and reads timestamps in UTC; " +
			"the two have to agree, so neither can be set on its own"

	case "parsetime":
		return true, "timestamps are read as time values, which the catalog and the " +
			"result grid both depend on"

	default:
		return false, ""
	}
}

/*
DSN builds a connection string with the driver's own formatter.

Through mysql.Config rather than by concatenation, for the same reason
PostgreSQL's goes through net/url: a generated password contains an at sign, a
colon or a slash often enough that string-building produces a DSN naming a
different host.
*/
func (mysqlDialect) DSN(cfg Config) (string, error) {
	port := cfg.Port
	if port == 0 {
		port = DefaultMySQLPort
	}

	tls, err := mysqlTLS(cfg.SSLMode)
	if err != nil {
		return "", err
	}

	dsn := mysql.NewConfig()
	dsn.User = cfg.Username
	dsn.Passwd = cfg.Password
	dsn.Net = "tcp"
	dsn.Addr = net.JoinHostPort(cfg.Host, strconv.Itoa(port))
	dsn.DBName = cfg.Database
	dsn.TLSConfig = tls

	// utf8mb4, so that a four-byte character survives. utf8 in MySQL is three
	// bytes and silently will not hold an emoji.
	dsn.Collation = "utf8mb4_general_ci"

	/*
		ParseTime and Loc are one decision, not two.

		The driver parses what the server sends using Loc, and the server sends
		TIMESTAMP values converted into the session's time_zone. Setting one
		without the other shifts every zoned value by the difference and
		nothing reports it -- the rows just quietly belong to a different hour,
		or a different day.

		So both are pinned here, and Validate refuses a configuration that
		tries to set either on its own.
	*/
	dsn.ParseTime = true
	dsn.Loc = time.UTC

	dsn.Params = map[string]string{
		// UTC on the wire, pinned rather than inherited. A MySQL server's
		// time_zone is whatever its operator set -- often the machine's local
		// zone -- and a connector that takes it as it finds it returns a
		// different instant for the same row depending on which replica
		// answered. Quoted, because this becomes `SET time_zone=...`.
		"time_zone": "'+00:00'",
	}

	for key, value := range cfg.Options {
		if reserved, _ := mysqlReserved(key); reserved {
			continue
		}

		dsn.Params[key] = value
	}

	return dsn.FormatDSN(), nil
}

/*
mysqlTLS maps the configured SSL mode onto the driver's vocabulary.

MySQL does not use PostgreSQL's words, and [Config.SSLMode] is documented as
the source's own. Both spellings of each mode are accepted because both are in
circulation: "require" is what somebody arriving from PostgreSQL will type and
"true" is what the driver's own documentation uses.
*/
func mysqlTLS(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "":
		// Not "false". A default that silently sends credentials in the clear
		// is the wrong default; preferred uses TLS when the server offers it.
		return "preferred", nil

	case "disable", "false":
		return "false", nil

	case "preferred", "prefer":
		return "preferred", nil

	case "skip-verify":
		return "skip-verify", nil

	case "require", "true", "verify-identity", "verify-full":
		// The driver's "true" verifies the chain and the server name, which is
		// what the stricter of these words means everywhere else.
		return "true", nil

	default:
		return "", Errorf(ReasonTLS, nil,
			"one of: disable, preferred, skip-verify, require, verify-identity",
			"%q is not a MySQL SSL mode", mode)
	}
}

func (mysqlDialect) Capabilities() Capabilities {
	return Capabilities{
		// All three arrived in MySQL 8.0 (lateral joins in 8.0.14). The
		// conformance suite runs each one rather than believing this.
		WindowFunctions: true,
		CTEs:            true,
		LateralJoins:    true,

		Placeholder: PlaceholderQuestion,

		QuoteIdentifier: quoteMySQLIdentifier,

		// 64 characters, and unlike PostgreSQL it errors rather than
		// truncating -- so a generated alias that is too long fails loudly,
		// which is the better of the two failures.
		MaxIdentifierLength: 64,

		// Not because the driver does it. The driver hangs up and leaves the
		// query running; [mysqlDialect.KillQuery] is what makes this true.
		SupportsCancel: true,
	}
}

// quoteMySQLIdentifier wraps an identifier in backticks.
//
// An embedded backtick is doubled, which is the whole of the escaping rule and
// the whole of the injection risk: an identifier that ends its own quoting can
// start a statement.
func quoteMySQLIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// --- stopping a query at the server -----------------------------------------

/*
SessionID asks MySQL what it calls this connection.

A round trip per query, which is not free and is the price of being able to
stop one. It has to run on the connection the query will use: the whole point
is to name that session and no other.
*/
func (mysqlDialect) SessionID(ctx context.Context, conn *sql.Conn) (int64, error) {
	var id int64

	if err := conn.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&id); err != nil {
		return 0, fmt.Errorf("ask MySQL for the connection id: %w", err)
	}

	return id, nil
}

/*
KillQuery stops the statement running on a session.

KILL QUERY rather than KILL CONNECTION: the statement stops and the session
survives, so the pooled connection is reusable rather than needing to be dialed
again. KILL CONNECTION would work and would throw away a connection on every
cancellation.
*/
func (mysqlDialect) KillQuery(ctx context.Context, db *sql.DB, session int64) error {
	// The session is an int64 this package read from the server itself a
	// moment ago, so there is no caller-supplied text in this statement --
	// which is what makes building it by hand acceptable here and nowhere
	// else.
	//nolint:gosec // G202: an int64 from the server; MySQL takes no placeholder for KILL
	statement := "KILL QUERY " + strconv.FormatInt(session, 10)

	if _, err := db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("stop the query on MySQL session %d: %w", session, err)
	}

	return nil
}

// --- errors -----------------------------------------------------------------

/*
Classify turns MySQL's errors into something actionable.

On the server's error numbers where there is one. They are stable across
versions and the messages are not, which is the same reason PostgreSQL's
classifier reads SQLSTATE. MySQL also carries a SQLSTATE, but the numbers are
finer grained: 1044 and 1142 are both "42000" and mean different things to the
person reading the message.
*/
func (mysqlDialect) Classify(err error) *Error {
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		if classified := classifyMySQLNumber(myErr, err); classified != nil {
			return classified
		}

		// A number nobody has mapped yet. Reported as unknown *with the
		// number*, so the next person to see it has something to look up
		// rather than a sentence somebody wrote once.
		return Errorf(ReasonUnknown, err, "",
			"MySQL reported error %d: %s", myErr.Number, myErr.Message)
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
			"check the port, and that MySQL is listening on it",
			"the connection was refused")
	}

	// TLS failures arrive as ordinary errors with recognizable text. Matching
	// on text is unlovely and is the only handle there is; getting it wrong
	// costs a less specific message rather than a wrong one.
	if text := err.Error(); strings.Contains(text, "tls") ||
		strings.Contains(text, "certificate") ||
		strings.Contains(text, "TLS") {
		return Errorf(ReasonTLS, err,
			"check the SSL mode, and whether the server's certificate is trusted here",
			"the TLS handshake failed")
	}

	// Not recognized. Returning nil hands it to the shared classifier, which
	// knows about cancellation and timeouts.
	return nil
}

// classifyMySQLNumber maps a server error number, or returns nil for one this
// build has not been taught.
func classifyMySQLNumber(myErr *mysql.MySQLError, cause error) *Error {
	switch myErr.Number {
	case 1045: // ER_ACCESS_DENIED_ERROR
		return Errorf(ReasonAuth, cause,
			"check the username and password",
			"MySQL refused those credentials")

	case 1049: // ER_BAD_DB_ERROR
		return Errorf(ReasonNoDatabase, cause,
			"check the database name; the server is reachable and the credentials work",
			"that database does not exist on this server")

	/*
		ER_DBACCESS_DENIED_ERROR, which is *also* what a database that does not
		exist looks like to an account without rights over it.

		MySQL refuses to say which, on purpose: telling an unprivileged user
		that a database exists is an information leak. So the message carries
		the ambiguity rather than picking one and being confidently wrong --
		the common cause is a typo in the database name, and the next most
		common is a missing grant, and the person needs to check both.

		1049 below is the same situation seen by an account that *can* see the
		database, where MySQL does answer plainly.
	*/
	case 1044:
		return Errorf(ReasonPermission, cause,
			"MySQL does not say which: check the database name first, then this account's grants",
			"MySQL refused access to that database -- it may not exist, "+
				"or this account may not be granted it")

	case 1142, 1143: // Table and column access denied
		return Errorf(ReasonPermission, cause,
			"the account authenticated but is not granted this",
			"permission denied by MySQL")

	case 1064: // ER_PARSE_ERROR
		return Errorf(ReasonSyntax, cause, "", "MySQL could not parse the query")

	case 1146: // ER_NO_SUCH_TABLE
		return Errorf(ReasonSyntax, cause,
			"check the database and the table name",
			"no such table")

	case 1054: // ER_BAD_FIELD_ERROR
		return Errorf(ReasonSyntax, cause, "", "no such column")

	case 1317, 1927: // ER_QUERY_INTERRUPTED, ER_CONNECTION_KILLED
		// What a KILL QUERY produces, including this connector's own.
		return Errorf(ReasonCanceled, cause, "", "the query was canceled")

	case 3024: // ER_QUERY_TIMEOUT
		return Errorf(ReasonTimeout, cause,
			"raise the connection's query timeout, or make the query cheaper",
			"MySQL stopped the query at its execution time limit")

	case 1040, 1203: // ER_CON_COUNT_ERROR, ER_TOO_MANY_USER_CONNECTIONS
		return Errorf(ReasonUnreachable, cause,
			"the server is out of connection slots; lower this connection's pool size",
			"MySQL refused the connection: too many clients")

	default:
		return nil
	}
}

/*
IntrospectQuery lists columns across the schemas worth showing.

The same information_schema shape as PostgreSQL's, which is the point of using
the standard catalog: two connectors, one query, one scanner.

In MySQL a schema *is* a database, so table_schema is the database name and a
connection sees every database the account can reach rather than only the one
in the DSN. The system schemas are excluded because nobody connects a BI tool
to browse performance_schema.
*/
func (mysqlDialect) IntrospectQuery() string {
	return `
SELECT c.table_schema,
       c.table_name,
       CASE t.table_type WHEN 'VIEW' THEN 'view' ELSE 'table' END AS table_type,
       c.column_name,
       -- column_type rather than data_type: data_type flattens TINYINT(1) to
       -- "tinyint", which is how MySQL's only boolean becomes indistinguishable
       -- from a small integer, and it drops "unsigned" as well.
       c.column_type,
       c.is_nullable = 'YES' AS nullable,
       c.ordinal_position
FROM information_schema.columns c
JOIN information_schema.tables t
  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
WHERE c.table_schema NOT IN ('mysql', 'information_schema', 'performance_schema', 'sys')
  AND t.table_type IN ('BASE TABLE', 'VIEW')
ORDER BY c.table_schema, c.table_name, c.ordinal_position`
}

/*
NormalizeType maps MySQL's type names onto Pivot's.

MySQL has no boolean. `BOOLEAN` is an alias for `TINYINT(1)`, and
`information_schema.columns.data_type` flattens it to plain "tinyint" -- so a
boolean column and a small integer are indistinguishable there.

[mysqlDialect.IntrospectQuery] selects `column_type` instead, which keeps the
width: "tinyint(1)". That is the signal, and treating it as a boolean is a
heuristic rather than a fact -- somebody *could* mean a one-digit integer. It
is the heuristic every MySQL client makes, including JDBC's `tinyInt1isBit`,
and the alternative is that every boolean column in every MySQL source renders
as 0 and 1 forever.

The driver cannot make the distinction at all: it reports "TINYINT" for both.
So a query result over a MySQL boolean says Integer where the catalog says
Boolean. That is a disagreement the driver leaves no way to avoid, and the
catalog is the richer of the two.
*/
func (mysqlDialect) NormalizeType(sourceType string) datatype.Type {
	// Checked before the name is reduced, because the width is the whole
	// signal and bareName drops it.
	if strings.EqualFold(strings.TrimSpace(sourceType), "tinyint(1)") {
		return datatype.Type{Kind: datatype.Boolean, Source: sourceType}
	}

	return datatype.Normalize(sourceType, func(name string) (datatype.Type, bool) {
		switch name {
		/*
			MySQL's names are inverted relative to everybody else's, and this
			is the single most consequential entry in this file.

			In the SQL standard and in PostgreSQL, TIMESTAMP is a wall-clock
			reading and TIMESTAMP WITH TIME ZONE is an instant. In MySQL,
			TIMESTAMP *is* the instant -- stored as UTC and converted into the
			session's zone on the way out, which is why this connector pins
			that zone -- and DATETIME is the wall-clock reading.

			So the shared table is exactly wrong here, in the direction that
			does the most damage: it would call MySQL's instants naive and its
			naive values instants, on every row of every MySQL source. The
			conformance suite caught it the first time it ran.
		*/
		case "timestamp":
			return datatype.Type{Kind: datatype.TimestampTZ}, true

		case "datetime":
			return datatype.Type{Kind: datatype.Timestamp}, true

		case "enum", "set":
			// A fixed vocabulary, which is text to everything downstream --
			// and worth knowing is low-cardinality when Phase 2 picks a chart.
			return datatype.Type{Kind: datatype.String}, true

		case "year":
			// Four digits. Not a Date: it has no month or day, and pretending
			// otherwise puts it on a timeline at the first of January.
			return datatype.Type{Kind: datatype.Integer, Bits: 16}, true

		case "bit":
			return datatype.Type{Kind: datatype.Binary}, true

		case "geometry", "point", "linestring", "polygon":
			return datatype.Type{Kind: datatype.Unknown}, true
		}

		return datatype.Type{}, false
	})
}
