# Changelog

All notable changes to Pivot are recorded here.

Format based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html) —
breaking API changes bump major, no exceptions.

## [Unreleased]

Nothing has been released yet. Everything below is Phase 0 groundwork, listed
newest first within each section.

### Added

**OpenID Connect — Part 8-a**
- `internal/oidc`: Authorization Code with PKCE, auto-discovery, and ID token
  verification. Tokens are **verified**, never merely decoded — signature
  against the provider's published keys, issuer, audience, expiry, and the
  nonce this login issued. A decoded-but-unverified token is a string the
  caller supplied
- **A returning user is matched on the provider's `sub` claim, never on their
  email.** Directories reassign addresses; matching on one hands the next
  holder of an address the previous holder's account
- Adopting an existing local account by address is opt-in per provider and off
  by default, and requires `email_verified` even when enabled. It exists for
  the window in which an organization migrates its users onto SSO
- Just-in-time provisioning, with a default role so a new user can do something
- Group membership follows the directory both ways. Groups are matched by name
  and never created: a directory with hundreds of them would otherwise fill the
  organization with empty ones. Unmatched names are reported
- Claims land in `user_attributes` with `source = 'oidc'`, so a later sync
  replaces exactly what the provider owns and leaves manual entries alone
- Schema v5: `identity_providers` and `federated_identities`

**Permission enforcement — Part 7-b**
- `api.RequirePermission` gates a route after the tenant chain, so a handler
  never runs without both an identity and a decision
- Role endpoints: the catalog of built-in roles, who holds what, grant, and
  revoke. Plus an administrative "end any session in this organization",
  separate from the self-service one because they need different permissions
- **403 and 503 mean different things.** A 403 is a decision — you may not. A
  503 is the absence of one — the authorization backend could not be reached.
  Both deny, only one is an outage, and answering 403 during one would send a
  properly-permitted user to argue with an administrator
- `/auth/me` reports the caller's effective permissions, so a UI can hide what
  it cannot do. Advisory only: every endpoint enforces independently
- `pivot admin grant-role` and `revoke-role`, the recovery path for an
  organization whose administrators are all locked out
- **The first user created in an organization becomes its administrator.**
  Without it a fresh install has nobody who can grant a role, so nobody can
  ever be granted one. The second user gets nothing
- Removing the last administrator is refused over the API
- Endpoint-level assertions in the same specification file, so the model and
  the surface that enforces it are described in one place

**Authorization — Part 7-a**
- `authz.Checker` answers "may this user do this to this object?", and
  `authz.Enforce` reduces it to an error, so a caller has no boolean to
  misread. **It fails closed**: an unreachable backend denies rather than
  allows, because an authorization system that fails open is worse than none —
  it creates the false belief that access is controlled
- Authorization model v1 — `organization` → `group` → `user`, with group
  nesting followed upward so a role granted to a parent reaches a sub-team
- Built-in roles: Admin, Editor, Analyst, Viewer. `native_query` is Analyst's
  and not Editor's, deliberately: raw SQL bypasses semantic row-level security,
  so it is a separate grant rather than something bundled into "can edit"
- Role grants stored as Zanzibar tuples — `(subject, relation, object)`, with
  usersets written `group:analysts#member` — rather than a roles table, so the
  data migrates to OpenFGA as an export and a write
- **The permission model is specified as data**, in
  `internal/authz/testdata/model_v1.yaml`. That file is the contract; the Go
  beside it is a loader. Every assertion runs against both engines
- Decision caching with immediate invalidation and a 3-second TTL, so a
  permission change takes effect well inside the five-second budget whether or
  not the write happened in this process
- `TestQueryFilesAreASCII` and `TestSQLiteQueriesAvoidNamedArguments` — guards
  against two sqlc generation bugs that produce unintelligible errors

**Authentication over HTTP — Part 6-b**
- `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/me`,
  `GET /api/v1/auth/sessions`, and `DELETE /api/v1/auth/sessions/{id}`
- Session cookie: `HttpOnly` and `SameSite=Lax`, neither configurable.
  `HttpOnly` is what keeps a cross-site scripting bug from becoming a stolen
  session; `SameSite=Lax` is the CSRF defense, since a browser will not attach
  the cookie to a cross-site `POST` or `DELETE`. `Secure` is set on any TLS
  request and otherwise only when `auth.cookieSecure` is on — a browser
  discards a `Secure` cookie sent over plain HTTP, which would leave the
  30-second local install at a login form that never logs anyone in
- **Requests are now scoped by their session**, not by a single-tenant stand-in.
  The organization comes from the session row and from nowhere in the request,
  so a caller cannot name a tenant they have not authenticated against
- Login is throttled per address *outside* the handler, so a throttled request
  never reaches Argon2 — an unauthenticated endpoint that allocates 64 MiB per
  call is a denial-of-service surface
- `PIVOT-AUTH-005` for a locked account. It shares its 429 with
  `PIVOT-RATE-001` and means something entirely different, which is the case
  the code registry exists for
- `auth` configuration section: session timeouts, lockout thresholds, and the
  cookie's name, domain and `Secure` flag
- Expired sessions and stale login-attempt records are swept at startup

**Authentication — Part 6-a**
- Argon2id password hashing (m=64MB, t=3), PHC-encoded so the cost can be
  raised later without invalidating existing hashes
- Server-side sessions with two expiries: a sliding idle timeout, and an
  absolute cap that is never extended. Tokens are 256-bit and stored only as a
  SHA-256 hash, so a database dump yields no working sessions
- Progressive lockout after repeated failures, keyed by the **attempted**
  email so it covers addresses that do not exist — locking out only real
  accounts would make the lockout a user-enumeration oracle
- Every failed login costs the same: a nonexistent account still pays for an
  Argon2 verification, because matching the response but not the timing leaves
  the oracle open
- `pivot admin create-user` and `pivot admin reset-password`, reading the
  password from the terminal without echoing

**HTTP foundations — Part 5**
- Standard error envelope on every non-2xx response, carrying a stable
  machine-readable code (`PIVOT-<AREA>-<NNN>`), a correlation ID, and a
  documentation link. Codes are independent of HTTP status, so two failures
  sharing a status stay distinguishable
- Middleware chain: request ID, structured access logging, panic recovery,
  security headers, CORS (closed by default), body-size limit, rate limiting
- Per-endpoint-class rate limiting with `Retry-After`. Health probes are
  exempt, so an orchestrator never kills a healthy instance for being busy
- Request decoding that validates at the boundary and rejects unknown fields,
  rather than silently discarding a misspelled one
- `api/openapi.yaml` as the source of truth, with tests asserting the spec
  against the code, and `make gen-client` for the TypeScript client

**Repository layer — Parts 4-a and 4-b**
- Tenant-scoped repositories that take no organization parameter: the tenant
  comes from `tenant.Scope` in the request context, so a caller cannot pass the
  wrong one. A context without a scope is an error before any SQL runs
- Soft-delete filtering, optimistic concurrency and change events applied in
  the repository base, so no call site has to remember them
- `SystemRepo` for operations that legitimately have no tenant (provisioning,
  slug lookup), named so every call site is explicit about it
- Group repository with nesting and membership, and a user-attribute repository
  that tracks provenance so an identity-provider sync can replace what it owns
  without disturbing manual assignments
- Change event bus carrying the acting user — something database triggers
  cannot know — and an audit subscriber. The foundation for Phase 4's audit log
- `api.WithTenant` middleware: a request whose tenant cannot be resolved is
  rejected with 401 before any handler runs

**Metadata store — Parts 3-a and 3-b**
- Schema v1 (`organizations`, `users`, `user_attributes`, `groups`,
  `group_members`) on **both** PostgreSQL and SQLite
- `pivot migrate up | status | version | create` — forward-only migrations,
  embedded in the binary, idempotent on re-run. `create` scaffolds both
  dialects at once, since a one-sided migration is how schemas drift
- Typed query layer generated by sqlc for both engines, over 22 queries
- `internal/store/dbtypes`: column types that behave identically on both
  engines, so the two generated packages are byte-identical apart from the
  package clause
- Database readiness check on `/readyz`; a database outage fails readiness but
  deliberately not liveness
- `deploy/docker-compose.dev.yml`, `make dev-db`, and `make test-all`, which
  runs the suite against both engines
- `make gen` and `make gen-check`; generated code is committed and reproducible

**Server and CLI — Part 2**
- `pivot serve` — HTTP server with structured logging, `/healthz` liveness,
  `/readyz` readiness, and a graceful drain on SIGTERM/SIGINT
- Configuration with strict precedence — defaults → file → `PIVOT_*`
  environment → flags — validated at startup with errors that name the field
  and say what is acceptable
- `server.preShutdownDelay`, a lame-duck period that fails readiness while
  still accepting, so load balancers observe a 503 rather than a refused
  connection. Defaults to 0; set ~5s behind a load balancer
- `pivot config show` and `pivot config env`
- `pivot version`, with `--json`

**Project foundations — Parts 0 and 1**
- Go module, package skeleton, and build tooling (`make build`, `lint`, `test`,
  `fmt`, `check`)
- `internal/version` with link-time stamping and a VCS-revision fallback
- Strict `golangci-lint` configuration: correctness and security linters
  enabled, style-opinion linters deliberately off
- Apache 2.0 license, contributor guide, code of conduct, security policy, and
  a PR template carrying the Definition of Done
- Design corpus: vision, tech stack, system/security/data architecture, nine
  ADRs, an 11-phase roadmap, NFRs, and the competitive feature matrix
- `CHECKLIST.md` — the session-by-session build plan

### Changed

- **Minimum Go version raised from 1.23 to 1.26**, required by `goose` and
  `modernc.org/sqlite`. See the amendment in
  [ADR-0001](docs/architecture/adr/0001-backend-language.md#amendments)

### Fixed

- **Role grants could outlive their subject.** `role_assignments.subject_id` is
  polymorphic — a subject is a user or a group — so no foreign key could
  enforce it and nothing cascaded. A grant naming a deleted or foreign user
  stored fine and dangled, and an identifier reused by a later import would
  have inherited it. Granting now verifies the subject exists in the caller's
  organization, and deleting a user or group revokes its grants

- **Any member could have ended any other member's session.** The session
  repository's revoke was scoped to the organization but not to the user, which
  is the right power for an administrator and the wrong one for the endpoint
  that manages your own devices. `RevokeSessionForUser` names the user in the
  `WHERE` clause, so a session belonging to someone else is simply not found —
  enforced in SQL rather than by a check a handler could forget

- **Cross-tenant write hole in the v1 schema.** Foreign keys on `group_members`
  and `user_attributes` referenced `groups(id)` and `users(id)` alone, so each
  key was satisfied independently and a row could name one organization while
  pointing at another's user. Migration 00002 makes them composite on
  `(id, org_id)`, closing it at the database level on both engines

[Unreleased]: https://github.com/Mmd4LIFE/pivot/commits/main
