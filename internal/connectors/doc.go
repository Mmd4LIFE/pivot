// Package connectors holds the drivers for every data source Pivot reads.
//
// A connector connects, introspects a schema, executes a compiled query,
// cancels it, and declares its dialect capabilities. It accepts compiled query
// objects, never SQL strings — that restriction is what keeps the semantic
// compiler the only path to data.
//
// Because ADR-0001 chose Go over the JVM, we forgo JDBC's universal driver
// coverage and hand-write each connector. The conformance suite is the
// mitigation: one shared test suite covering type mapping, NULL and timezone
// semantics, large-result streaming, cancellation propagation, error
// classification, and unicode. A connector that does not pass it does not
// ship. Write it once and every later connector is a week instead of a month.
//
// Built across Parts 16-19.
package connectors
