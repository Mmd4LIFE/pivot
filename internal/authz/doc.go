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
// Built in Part 7; extended with RLS and masking in Phase 4.
package authz
