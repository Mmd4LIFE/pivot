//go:build !duckdb

package connectors

/*
DuckDB, in the builds that do not have it -- which is all of them by default.

The other half of duckdb.go. Its job is to make a missing connector explain
itself: without this, asking for a DuckDB connection produces "no connector for
\"duckdb\"", which reads like the connector does not exist rather than like it
was left out of this binary on purpose.

It registers nothing, so `Kinds()` and `--kind` list only what can really be
opened. Offering a connector that cannot be opened is worse than not offering
it.
*/

func init() {
	RegisterAbsent(KindDuckDB,
		"DuckDB needs CGo, which the default build does not use -- see ADR-0010. "+
			"Build it with `make build-duckdb`, or use the sqlite connector, "+
			"which reads a file and is always present")
}
