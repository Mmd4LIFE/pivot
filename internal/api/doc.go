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
// Built across Parts 2 (server), 5 (middleware and error envelope), and 6
// (authentication endpoints).
package api
