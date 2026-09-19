// Package semantic is the spine of the product: models, dimensions, measures,
// metrics, the join graph, and the compiler that turns a semantic query into
// dialect SQL.
//
// The compiler is the single security boundary. Every query in Pivot — UI,
// API, alert, flow, export, embed, AI — passes through it, and it injects the
// requesting user's row-level filters and column masks before emitting SQL.
// There is no second code path. This is why it is built before the permission
// model rather than after.
//
// The hardest correctness problem here is fan-out. Selecting a measure from
// one model and a dimension from another across a one-to-many join inflates
// the measure: SUM(orders.revenue) joined to order_items silently multiplies
// revenue by the item count. Symmetric aggregates fix it; where a dialect
// cannot express them the compiler pre-aggregates in a subquery and records
// why. Fan-out that cannot be made safe is a hard error, never a quiet wrong
// number.
//
// Golden-file tests — semantic query in, expected SQL per dialect out — are
// the safety net. Compiler changes are terrifying without them, because a
// subtle change in join ordering produces different numbers and no test
// failure.
//
// Built in Phase 3.
package semantic
