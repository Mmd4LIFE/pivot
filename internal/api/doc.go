// Package api hosts the HTTP surface: the REST handlers, the middleware
// chain, the WebSocket hub, and the error contract every endpoint shares.
//
// The OpenAPI spec is the source of truth. Handlers are written against
// generated types, never the other way around — see docs/architecture/tech-stack.md.
//
// Architectural rule: the UI has no privileged API. Everything the frontend
// does is a documented public endpoint. If the UI needs something the public
// API cannot express, the API is wrong.
//
// Liveness and readiness mean different things here and the distinction is
// load-bearing. /healthz reports only that the process is alive and never
// consults dependencies, because a database outage must not cause an
// orchestrator to kill every pod. /readyz reports whether this instance should
// receive traffic, and fails while shutting down or when a registered check
// fails.
//
// Part 5 adds the middleware chain, the error envelope, and the versioned
// router; Part 6 adds authentication endpoints.
package api
