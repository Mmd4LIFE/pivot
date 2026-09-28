package connectors

import (
	"errors"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Mmd4LIFE/pivot/internal/datatype"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

/*
SQLite: the connector with no server.

PostgreSQL and MySQL differ from each other in dialect. SQLite differs in
*shape*: there is no host, no port, no username, no password and no TLS, and
[Config] was designed around all five. A file path in Database is the whole
address.

That is the third real test of the Part 16 interface, and it passed more
cheaply than expected — because Validate was always the dialect's job, so a
source that needs none of those fields simply says so. What it did cost was the
CLI, which had been requiring a host and a username of its own accord and now
lets the dialect decide.

# Read-only, always

A connection opened here cannot write to the file. A BI source is something
Pivot reads, and a connector able to write to the file it was pointed at is one
stray statement away from modifying somebody's data — or, if pointed at Pivot's
own store, from modifying Pivot.

Reading such a file is a separate problem this does not solve. From the CLI it
is not an escalation: whoever can run `pivot admin` can already read the file.
From a browser it would be, which is why Part 26 owns an allowlist before
connection management reaches one.
*/

func init() {
	dialect := sqliteDialect{}

	Register(dialect.Kind(), func(cfg Config) (Connector, error) {
		return OpenSQL(dialect, cfg)
	})
}

type sqliteDialect struct{}

func (sqliteDialect) Kind() Kind { return KindSQLite }

// DriverName is modernc's pure-Go SQLite, which the metadata store already
// imports — so this connector costs no new dependency and no CGo.
func (sqliteDialect) DriverName() string { return "sqlite" }

/*
Validate checks the path, and refuses the fields this source does not have.

Refused rather than ignored. A connection carrying a username and a password
looks authenticated to anybody reading it back, and a SQLite file is protected
by its filesystem permissions and nothing else. Silently dropping the
credentials would leave somebody believing in a lock that is not there.
*/
func (sqliteDialect) Validate(cfg Config) error {
	path := strings.TrimSpace(cfg.Database)
	if path == "" {
		return Errorf(ReasonUnknown, nil,
			"pass the path as --database",
			"a SQLite connection needs the path to a database file")
	}

	// A URI rather than a path: modernc accepts one and it would smuggle in
	// query parameters this connector sets itself, mode=ro among them.
	if strings.Contains(path, "?") || strings.HasPrefix(path, "file:") {
		return Errorf(ReasonUnknown, nil,
			"give a plain filesystem path",
			"a SQLite path may not be a URI")
	}

	var unusable []string

	for field, value := range map[string]string{
		"host":     cfg.Host,
		"username": cfg.Username,
		"password": cfg.Password,
		"SSL mode": cfg.SSLMode,
	} {
		if strings.TrimSpace(value) != "" {
			unusable = append(unusable, field)
		}
	}

	if cfg.Port != 0 {
		unusable = append(unusable, "port")
	}

	if len(unusable) > 0 {
		// Sorted, because ranging a map is not: the same mistake has to read
		// the same way twice running.
		slices.Sort(unusable)

		return Errorf(ReasonUnknown, nil,
			"a SQLite database is a file, protected by its filesystem permissions and "+
				"nothing else -- a connection that carried these would look authenticated "+
				"and would not be",
			"a SQLite connection has no %s", strings.Join(unusable, ", "))
	}

	return nil
}

/*
DSN builds a read-only file URI.

Through net/url for the same reason the others do: a path can contain a
question mark, a hash or a space, and concatenating it into a URI produces one
that opens a different file, or none.
*/
func (sqliteDialect) DSN(cfg Config) (string, error) {
	path, err := filepath.Abs(strings.TrimSpace(cfg.Database))
	if err != nil {
		return "", Errorf(ReasonUnknown, err, "",
			"could not resolve the SQLite path %q", cfg.Database)
	}

	q := url.Values{}

	// The whole point. Not a default somebody can override: Options cannot
	// reach this because every value below is set after them.
	q.Set("mode", "ro")

	// Waits rather than failing instantly when another process holds a write
	// lock. Five seconds is long enough to ride out somebody else's
	// transaction and short enough to be inside any sensible query timeout.
	q.Set("_pragma", "busy_timeout(5000)")

	for key, value := range cfg.Options {
		if key == "mode" || key == "_pragma" {
			continue
		}

		q.Set(key, value)
	}

	q.Set("mode", "ro")

	// The opaque form, so a path with a leading slash is not read as a host.
	return "file:" + path + "?" + q.Encode(), nil
}

func (sqliteDialect) Capabilities() Capabilities {
	return Capabilities{
		// Window functions arrived in SQLite 3.25 and CTEs in 3.8.3. modernc
		// tracks a recent SQLite, and the conformance suite runs both rather
		// than believing this.
		WindowFunctions: true,
		CTEs:            true,

		// SQLite has no LATERAL. Declared false rather than left to chance:
		// the suite refuses a capability claimed without proof, which is how
		// this stays honest when somebody copies this file for the next
		// connector.
		LateralJoins: false,

		Placeholder: PlaceholderQuestion,

		QuoteIdentifier: quoteSQLiteIdentifier,

		// SQLite imposes no limit worth the name. A number is still required
		// -- generated aliases have to respect something -- so this is the
		// smallest limit among the sources Pivot speaks to, which keeps an
		// alias that works here working everywhere.
		MaxIdentifierLength: 63,

		// modernc interrupts the virtual machine when the context ends, and
		// the query stops. There is no second process to ask: for an embedded
		// database the client and the server are the same goroutine, which is
		// why this needs no Canceler.
		SupportsCancel: true,
	}
}

// quoteSQLiteIdentifier wraps an identifier in double quotes.
//
// SQLite also accepts backticks and square brackets, for compatibility with
// MySQL and SQL Server. Double quotes are the standard spelling and the one
// every other connector here uses, so generated SQL reads the same everywhere.
func quoteSQLiteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

/*
Classify turns SQLite's errors into something actionable.

On the result code, which is stable, rather than the message, which is not.
modernc reports the extended code, so the primary code is the low eight bits —
SQLITE_READONLY_DBMOVED and SQLITE_READONLY are both SQLITE_READONLY for the
purpose of telling somebody what to do about it.
*/
func (sqliteDialect) Classify(err error) *Error {
	var sqErr *sqlite.Error
	if !errors.As(err, &sqErr) {
		// Not recognized. Returning nil hands it to the shared classifier,
		// which knows about cancellation and timeouts.
		return nil
	}

	switch sqErr.Code() & 0xff {
	case sqlite3.SQLITE_CANTOPEN:
		return Errorf(ReasonNoDatabase, err,
			"check the path, and that the file is readable by the user Pivot runs as",
			"could not open that database file")

	case sqlite3.SQLITE_NOTADB:
		return Errorf(ReasonNoDatabase, err,
			"the path exists and is not a SQLite database",
			"that file is not a SQLite database")

	case sqlite3.SQLITE_PERM, sqlite3.SQLITE_AUTH:
		return Errorf(ReasonPermission, err,
			"check the filesystem permissions on the file and its directory",
			"the file is there and cannot be read")

	case sqlite3.SQLITE_READONLY:
		// Reached only by a statement that tried to write, which this
		// connector opens the file specifically to prevent.
		return Errorf(ReasonPermission, err,
			"Pivot opens a SQLite source read-only; a connection is for reading",
			"that statement would write to the database")

	case sqlite3.SQLITE_ERROR:
		// The catch-all for anything the parser or the planner rejected,
		// which is overwhelmingly a mistake in the query.
		return Errorf(ReasonSyntax, err, "", "SQLite rejected the query: %s", sqErr.Error())

	case sqlite3.SQLITE_INTERRUPT:
		return Errorf(ReasonCanceled, err, "", "the query was canceled")

	case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED:
		return Errorf(ReasonUnreachable, err,
			"another process holds a lock on the file; this connection waited and gave up",
			"the database file is locked")

	default:
		// A code nobody has mapped yet. Reported as unknown *with the code*,
		// so the next person to see it has something to look up.
		return Errorf(ReasonUnknown, err, "",
			"SQLite reported result code %d: %s", sqErr.Code(), sqErr.Error())
	}
}

/*
IntrospectQuery lists columns from SQLite's own catalog.

sqlite_master joined to pragma_table_info, which is a table-valued function
since 3.16 and the only way to get columns without a statement per table.

SQLite has no schemas in the PostgreSQL sense. The attached database is called
"main" and that is reported as the schema name, so the catalog has the same
shape for every connector rather than a special case for this one.

A column declared without a type is real in SQLite and reported as "ANY" rather
than as an empty string: a column with no type at all is indistinguishable from
a catalog that failed to read one, and everything above here has to tell those
apart.
*/
func (sqliteDialect) IntrospectQuery() string {
	return `
SELECT 'main' AS table_schema,
       m.name AS table_name,
       CASE m.type WHEN 'view' THEN 'view' ELSE 'table' END AS table_type,
       p.name AS column_name,
       CASE WHEN TRIM(p.type) = '' THEN 'ANY' ELSE p.type END AS data_type,
       NOT p."notnull" AS nullable,
       p.cid + 1 AS ordinal_position
FROM sqlite_master m
JOIN pragma_table_info(m.name) p
WHERE m.type IN ('table', 'view')
  AND m.name NOT LIKE 'sqlite_%'
ORDER BY m.name, p.cid`
}

/*
NormalizeType maps SQLite's declared types onto Pivot's.

SQLite is the odd one: a column's type is a *declaration*, not a constraint,
and what comes back is whatever was written in the CREATE TABLE. So the names
are whatever the author felt like -- which in practice means the names every
other database uses, because that is what people type.

Hence almost nothing here: [datatype.Base] already covers it. What is below is
the two answers SQLite gives that nobody else does.
*/
func (sqliteDialect) NormalizeType(sourceType string) datatype.Type {
	return datatype.Normalize(sourceType, func(name string) (datatype.Type, bool) {
		switch name {
		case "any", "":
			// A column declared with no type at all, which SQLite allows and
			// the introspection query reports as ANY. Genuinely unknown: the
			// column can hold anything, and saying otherwise would be a guess.
			return datatype.Type{Kind: datatype.Unknown}, true

		case "numeric":
			// SQLite's NUMERIC affinity is not the exact decimal the name
			// implies -- it stores whatever fits and falls back to a float.
			// Calling it Decimal would promise exactness SQLite does not
			// provide, which is the promise this package exists to keep.
			return datatype.Type{Kind: datatype.Float, Bits: 64}, true
		}

		return datatype.Type{}, false
	})
}
