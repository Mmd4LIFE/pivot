// Package authz answers "can this user perform this action on this object?".
//
// Backed by OpenFGA — a Zanzibar-style relationship graph — because BI
// permissions are recursive: collections nest, groups nest, dashboards inherit
// from collections unless overridden, and a card querying an invisible model
// must be denied even on a shared dashboard. Expressed as SQL that becomes a
// recursive CTE nobody can debug; expressed as relationships it stays
// declarative, testable, and explainable. See ADR-0009.
//
// Two rules that are not negotiable:
//
// Enforcement lives in the semantic query compiler, not in handlers. A new
// surface must not be able to introduce a bypass. Handler middleware checks
// object-level access; row and column access is applied when SQL is generated.
//
// This package fails closed. If the authorization backend is unreachable,
// every decision is deny. An authorization system that fails open is worse
// than none, because it creates a false belief that access is controlled.
//
// Part 7 builds the contract and the model: authz.Checker, Zanzibar-shaped
// role tuples, the four built-in roles, and a declarative assertion table in
// testdata that is the specification rather than a test of it. The backing
// implementation resolves the graph locally; ADR-0009's amendment records the
// embedded-OpenFGA spike and when to take it up. Phase 4 adds RLS and masking.
package authz
