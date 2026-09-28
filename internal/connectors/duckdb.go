//go:build duckdb

package connectors

import (
	"errors"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/marcboeker/go-duckdb/v2"

	"github.com/Mmd4LIFE/pivot/internal/datatype"
)

/*
DuckDB: the connector that is not in the default binary.

ADR-0004 chose DuckDB for local analytical compute and accepted CGo with its
eyes open. It also listed, under "Revisit if", the case that turned out to be
true: CGo build complexity outweighing the benefit. So this file is behind a
build tag and the default binary does not contain it. ADR-0010 records the
measurements and the decision.

Everything here is the same shape as the other three dialects. The interesting
part of this connector is not its SQL, it is what compiling it costs -- see the
Makefile's `build-duckdb` target and the CI job that exercises it.

# Read-only, like SQLite

A DuckDB file is opened read-only for the same reason SQLite's is: a BI source
is something Pivot reads. The in-memory mode is the exception, because there is
no file to protect -- it exists so that a connection can read Parquet and CSV,
which is the thing DuckDB is here for.

# The memory cap is not advisory

NFR 1.3 requires a per-query memory cap that is enforced rather than suggested,
because DuckDB under a concurrent load can take the host with it. Every
connection sets one, and a configuration that does not say gets the NFR's
default of 1GB rather than DuckDB's own, which is a fraction of system memory.
*/

func init() {
	dialect := duckdbDialect{}

	Register(dialect.Kind(), func(cfg Config) (Connector, error) {
		return OpenSQL(dialect, cfg)
	})
}

// InMemory is the Database value that opens a scratch database rather than a
// file. It is how a connection reads Parquet and CSV without owning a
// database of its own.
const InMemory = ":memory:"

// DefaultDuckDBMemoryLimit is NFR 1.3's per-query cap, applied when a
// configuration does not set one.
const DefaultDuckDBMemoryLimit = "1GB"

type duckdbDialect struct{}

func (duckdbDialect) Kind() Kind { return KindDuckDB }

func (duckdbDialect) DriverName() string { return "duckdb" }

func (duckdbDialect) Validate(cfg Config) error {
	path := strings.TrimSpace(cfg.Database)
	if path == "" {
		return Errorf(ReasonUnknown, nil,
			"pass a file path as --database, or "+InMemory+" to query files directly",
			"a DuckDB connection needs a database")
	}

	if path != InMemory && (strings.Contains(path, "?") || strings.Contains(path, "\x00")) {
		return Errorf(ReasonUnknown, nil,
			"give a plain filesystem path",
			"a DuckDB path may not carry query parameters")
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
		// Sorted, because ranging a map is not.
		slices.Sort(unusable)

		return Errorf(ReasonUnknown, nil,
			"DuckDB is embedded -- there is no server to authenticate to, and a "+
				"connection carrying these would look authenticated and would not be",
			"a DuckDB connection has no %s", strings.Join(unusable, ", "))
	}

	return nil
}

/*
DSN builds a path plus the settings this connector insists on.

go-duckdb takes `path?setting=value`, where each setting becomes a SET on the
connection. The two that are not negotiable go on last, so an Options entry
cannot move them.
*/
func (duckdbDialect) DSN(cfg Config) (string, error) {
	path := strings.TrimSpace(cfg.Database)

	if path != InMemory {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", Errorf(ReasonUnknown, err, "",
				"could not resolve the DuckDB path %q", cfg.Database)
		}

		path = absolute
	}

	q := url.Values{}

	for key, value := range cfg.Options {
		switch strings.ToLower(key) {
		case "access_mode", "memory_limit":
			// Set below. Ignored here rather than refused, because unlike
			// SQLite's mode these are settings somebody could reasonably try
			// to tune -- and the answer is that the limit is the connection's,
			// not the query's.
			continue
		default:
			q.Set(key, value)
		}
	}

	// A source is read. The in-memory database has no file to protect and has
	// to be writable, because reading a Parquet file means creating temporary
	// state.
	if path != InMemory {
		q.Set("access_mode", "read_only")
	}

	limit := strings.TrimSpace(cfg.Options["memory_limit"])
	if limit == "" {
		limit = DefaultDuckDBMemoryLimit
	}

	// Enforced rather than advisory: DuckDB refuses the query rather than
	// letting it take the host down with it.
	q.Set("memory_limit", limit)

	if len(q) == 0 {
		return path, nil
	}

	return path + "?" + q.Encode(), nil
}

func (duckdbDialect) Capabilities() Capabilities {
	return Capabilities{
		WindowFunctions: true,
		CTEs:            true,
		LateralJoins:    true,

		// DuckDB accepts both `?` and `$1`. The positional form is the one its
		// own documentation leads with.
		Placeholder: PlaceholderQuestion,

		QuoteIdentifier: quoteDuckDBIdentifier,

		// DuckDB imposes no limit worth the name, so this matches the smallest
		// among the sources Pivot speaks to -- an alias that works here works
		// everywhere.
		MaxIdentifierLength: 63,

		SupportsCancel: true,
	}
}

// quoteDuckDBIdentifier wraps an identifier in double quotes, doubling any
// inside. The same rule as PostgreSQL, whose dialect DuckDB follows.
func quoteDuckDBIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Classify turns DuckDB's errors into something actionable, on the error type
// the driver reports rather than on its message.
func (duckdbDialect) Classify(err error) *Error {
	var dbErr *duckdb.Error
	if !errors.As(err, &dbErr) {
		return nil
	}

	switch dbErr.Type {
	case duckdb.ErrorTypeParser, duckdb.ErrorTypeSyntax:
		return Errorf(ReasonSyntax, err, "", "DuckDB could not parse the query")

	case duckdb.ErrorTypeCatalog, duckdb.ErrorTypeBinder:
		// A name that does not resolve: a table, a column, a function.
		return Errorf(ReasonSyntax, err, "",
			"DuckDB does not know that name: %s", dbErr.Msg)

	case duckdb.ErrorTypePermission:
		return Errorf(ReasonPermission, err,
			"Pivot opens a DuckDB file read-only; a connection is for reading",
			"that statement would write to the database")

	case duckdb.ErrorTypeIO:
		return Errorf(ReasonNoDatabase, err,
			"check the path, and that the file is readable by the user Pivot runs as",
			"DuckDB could not read that: %s", dbErr.Msg)

	case duckdb.ErrorTypeOutOfMemory:
		return Errorf(ReasonUnknown, err,
			"raise the connection's memory_limit, or make the query cheaper",
			"the query ran out of its memory budget")

	case duckdb.ErrorTypeInterrupt:
		return Errorf(ReasonCanceled, err, "", "the query was canceled")

	default:
		return Errorf(ReasonUnknown, err, "",
			"DuckDB reported a %v error: %s", dbErr.Type, dbErr.Msg)
	}
}

/*
IntrospectQuery lists columns across the schemas worth showing.

The same information_schema shape as PostgreSQL's and MySQL's. DuckDB's
catalog follows PostgreSQL closely enough that this is the third copy of one
query rather than a third query.
*/
func (duckdbDialect) IntrospectQuery() string {
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
WHERE c.table_schema NOT IN ('information_schema', 'pg_catalog')
ORDER BY c.table_schema, c.table_name, c.ordinal_position`
}

/*
NormalizeType maps DuckDB's type names onto Pivot's.

DuckDB follows PostgreSQL closely, so most of this is [datatype.Base]. What is
below is where it went its own way -- chiefly the explicit integer widths and
the nested types, which are the reason DuckDB is worth having and the reason a
flat type system has to say so rather than pretend.
*/
func (duckdbDialect) NormalizeType(sourceType string) datatype.Type {
	return datatype.Normalize(sourceType, func(name string) (datatype.Type, bool) {
		switch name {
		case "tinyint", "int1":
			return datatype.Type{Kind: datatype.Integer, Bits: 8}, true
		case "utinyint", "usmallint", "uinteger", "ubigint", "hugeint", "uhugeint":
			// Unsigned and 128-bit. Integer, with the width left unstated:
			// claiming 64 bits for a hugeint would be a lie a consumer could
			// act on.
			return datatype.Type{Kind: datatype.Integer}, true

		case "varchar", "bpchar":
			return datatype.Type{Kind: datatype.String}, true

		case "blob", "bit", "bitstring":
			return datatype.Type{Kind: datatype.Binary}, true

		case "timestamp_s", "timestamp_ms", "timestamp_ns", "timestamp_us":
			// The same instant-less wall clock at different resolutions.
			return datatype.Type{Kind: datatype.Timestamp}, true

		case "list":
			return datatype.Type{Kind: datatype.Array}, true

		case "struct", "map", "union":
			return datatype.Type{Kind: datatype.Struct}, true

		case "enum":
			return datatype.Type{Kind: datatype.String}, true
		}

		return datatype.Type{}, false
	})
}

/*
ForeignKeyQuery lists relationships from duckdb_constraints().

DuckDB hands back two parallel lists per constraint -- constraint_column_names
and referenced_column_names -- so this walks them by index rather than joining
them, for the same reason PostgreSQL's does: crossing the lists would pair
every local column with every referenced one.

DuckDB's lists are one-based and `range` excludes its upper bound, hence the
+ 1. referenced_table carries no schema, so the target is assumed to be in the
same schema as the table declaring the key, which is the only place DuckDB
allows it.
*/
func (duckdbDialect) ForeignKeyQuery() string {
	return `
SELECT c.constraint_name,
       c.schema_name                       AS from_schema,
       c.table_name                        AS from_table,
       c.constraint_column_names[i]        AS from_column,
       c.schema_name                       AS to_schema,
       c.referenced_table                  AS to_table,
       c.referenced_column_names[i]        AS to_column,
       CAST(i AS INTEGER)                  AS ordinal
FROM duckdb_constraints() c,
     range(1, len(c.constraint_column_names) + 1) AS t(i)
WHERE c.constraint_type = 'FOREIGN KEY'
  AND c.schema_name NOT IN ('information_schema', 'pg_catalog')
ORDER BY c.schema_name, c.table_name, c.constraint_name, i`
}
