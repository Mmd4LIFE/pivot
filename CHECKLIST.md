# Pivot Build Checklist

**The single source of truth for what to build next.** One part per session.

---

## ⚙️ HOW TO USE THIS — read this first, every session

When the user says **"do next part"**, follow this procedure exactly:

1. **Read the [Current state](#-current-state) block below.** It tells you where things
   stand in one glance.
2. **Find the first unchecked `- [ ]` part** in [The parts](#the-parts). That is the one to
   build. Do not skip ahead, do not pick a different one, do not bundle two parts together
   because they seem small.
3. **Read that part's `Deliverable`, `Done when`, and `Notes`.** They are written to be
   actionable without asking the user questions.
4. **Read the linked roadmap doc** for that part before writing code. The feature IDs
   (e.g. `P0-AUTH-001`) map to specs in [docs/roadmap/](docs/roadmap/).
5. **Build it.** Follow [docs/roadmap/00-principles.md](docs/roadmap/00-principles.md) —
   the Definition of Done applies to every part.
6. **Verify `Done when` honestly.** Run the commands. If something fails, say so and fix
   it. A part is not done because the code was written.
7. **Close out the part** (see below).
8. **Stop.** One part per session. Report what shipped and what's next.

### Closing out a part

At the end of every part, in this order:

1. Tick the box: `- [ ]` → `- [x]`, and append ` ✅ <YYYY-MM-DD>` to the part title.
2. Update the [Current state](#-current-state) block.
3. Add a row to the [Session log](#session-log) — what actually shipped, not what was
   planned.
4. If you discovered something that changes future parts, edit those parts now. This
   document must never become fiction.
5. Commit with `feat(<area>): <what> [Part N]` and **push to origin**.

### Rules

- **One part per session.** If the user wants more, they'll say so.
- **If a part turns out to be more than one session of work**, split it in place into
  `N-a` / `N-b`, do the first half, and note the split in the session log. Don't silently
  half-finish.
- **If a part is blocked**, mark it `- [!]`, write the blocker in Current state, and move
  to the next unblocked part.
- **Never mark a part done without running its verification commands.**
- **Every part ends pushed to GitHub.** No local-only work carries between sessions.

---

## 📍 Current state

| | |
|---|---|
| **Last completed** | Part 7-b — Permission middleware and the administrative surface |
| **Next up** | **Part 8 — OIDC single sign-on** |
| **Current phase** | Phase 0 — Foundations |
| **Branch** | `main` |
| **Blockers** | None |
| **Repo** | https://github.com/Mmd4LIFE/pivot |

**Where the code stands:** `./bin/pivot serve` runs an HTTP server with structured JSON
logging, `/healthz`, `/readyz` (including a database check), and a graceful drain on
SIGTERM. **A user can log in over HTTP and call an authenticated endpoint.**
`pivot migrate up|status|version|create` manages schema v4 on **both** SQLite and
Postgres. `pivot config show|env` reports configuration. Packages with real code:
`version`, `config`, `logging`, `api`, `auth`, `authz`, `cli`, `store`. Still `doc.go`
stubs: `connectors`, `semantic`, `query`.

**Dependencies:** cobra, yaml.v3, goose, pgx/v5, modernc.org/sqlite (pure Go — no CGo, so
Part 13's six-platform cross-compile stays a single build matrix), google/uuid,
x/crypto (argon2), x/term (no-echo password prompt).

**Authentication:** `internal/auth` owns credentials and sessions. Argon2id at m=64MB,
PHC-encoded. Sessions are server-side with two expiries — a sliding idle timeout and an
absolute cap that is never extended. **Every failed login costs the same**: a nonexistent
account still pays for an Argon2 verification against a dummy hash, because matching the
response but not the timing leaves the enumeration oracle open. Lockout is keyed by the
**attempted** email, so it covers addresses that do not exist.

`/api/v1/auth/{login,logout,me,sessions}` are live. The session cookie is `HttpOnly` and
`SameSite=Lax`, neither configurable — `HttpOnly` stops an XSS bug becoming a stolen
session, and `Lax` is the CSRF defense, since a browser does not attach the cookie to a
cross-site POST or DELETE. `Secure` is set on any TLS request and otherwise only when
`auth.cookieSecure` is on, because a browser discards a `Secure` cookie over plain HTTP
and the local install would never log anyone in. **Behind a TLS-terminating proxy, set
`auth.cookieSecure`** — Pivot only sees plain HTTP and cannot tell. Login is throttled
per IP *outside* the handler, so a throttled request never reaches Argon2.

**Authorization:** `authz.Checker` answers `can(user, action, object)`; `authz.Enforce`
reduces it to an error so a handler has no boolean to misread. It **fails closed** —
an unreachable backend denies — and that is mutation-verified. Role grants are stored as
Zanzibar tuples in `role_assignments`, not a `user_roles` table, so migrating to OpenFGA
is an export and a `Write` rather than a translation. Group nesting is followed **upward**
by a depth-capped walk in Go, not a recursive CTE. **The model is specified as data** in
`internal/authz/testdata/model_v1.yaml` — that file is the contract, and any future
checker must satisfy it unchanged. `native_query` is a separate permission from
`create_content` on purpose: raw SQL bypasses semantic RLS, so it is Analyst's and not
Editor's. **Embedded OpenFGA was spiked and works** (see
[ADR-0009's amendment](docs/architecture/adr/0009-authorization.md#amendments) for the
numbers); deferred to Phase 4, where the recursion it exists for actually appears.

`api.RequirePermission` gates a route *after* the tenant chain, so "who are you?" is
already answered and only "may you?" is left. It draws a distinction that matters
operationally: **403 means a decision was made and it was no; 503 means no decision could
be reached.** Both deny, only one is an outage, and answering 403 during one would send a
properly-permitted user to argue with an administrator. A nil checker denies too.
`/auth/me` reports effective permissions, computed by asking the checker the same
questions the middleware asks — so the advisory list and the enforced answer cannot
drift. **The first user created in an organization becomes its admin**, or a fresh
install would have nobody who could ever grant anything; the second user gets nothing.
Removing the last administrator is refused over the API and merely warned about in the
CLI, which is the recovery path.

**Tenant isolation:** `internal/tenant.Scope` has unexported fields and no usable zero
value. Repositories take **no org parameter at all** — they read the scope from the
context — so a caller cannot pass the wrong tenant because there is nothing to pass.
`OrganizationRepo` acts on the caller's own org; unscoped provisioning lives on
`SystemRepo`, named so every call site says what it is doing. A reflection test walks
`Repositories`' exported fields, so **a repository is covered the moment it is registered
in that struct** — it asserts all 41 methods refuse an unscoped context, and it is
mutation-verified. `api.WithTenant` rejects an unresolvable request with 401 before any
handler runs, and the scope now comes from `api.SessionTenantResolver` — the organization
is read off the session row and from nowhere in the request, so a caller cannot name a
tenant they have not authenticated against. `SingleTenantResolver` survives for tests
only and is wired nowhere.

**Schema is at v4.** Migration 00003 added `sessions` and `login_attempts`; 00004 added
`role_assignments`.

**Polymorphic subjects, closed in 7-b.** `role_assignments.subject_id` and `object_id`
are polymorphic — a subject is a user *or* a group — so they carry no foreign key and
migration 00002's composite-key rule cannot apply. Tenant isolation was never at risk
(every read filters `org_id`), but referential integrity was: `RoleRepo.Grant` now
verifies the subject exists in the caller's organization, and deleting a user or group
revokes its grants, so a reused identifier cannot inherit a stranger's permissions.

**Schema history.** Migration 00002 made the `group_members` and `user_attributes`
foreign keys composite on `(id, org_id)`. The v1 single-column keys let a row name one
organization while pointing at another's user — **a real cross-tenant write hole**, found
by a test. Any new child table must use composite keys for the same reason.

**HTTP surface:** `api.NewRouter` composes the middleware chain — request ID, logging,
recovery, security headers, CORS, body limit, rate limit, tenant — in that order, and the
order is a security property documented on the function. Every non-2xx response uses one
envelope with a stable `PIVOT-<AREA>-<NNN>` code; `api.FromError` is the single
translation point from repository and tenancy errors. Codes live in a registry that a test
checks for uniqueness, format, and a sane status. **`api/openapi.yaml` is the source of
truth** and tests assert it against the code: documented paths must not 404, and the
spec's code pattern must match every registered code. `make gen-client` regenerates the
TypeScript client (needs Node, which is installed).

**Generated code:** `make gen` runs sqlc; output in `internal/store/gen/{pg,lite}` is
committed. The two packages are byte-identical apart from the package clause, so Go allows
direct struct conversion between them — Part 4 needs one conversion per type, not a
hand-written mapping per engine. **A new column of UUID / timestamp / bool / JSON type
needs an entry in `sqlc.yaml`'s SQLite override list**, or the packages silently diverge.

**Environment notes for future sessions:**
- Go 1.27.1 via snap (`/snap/bin/go`). **`go.mod` floor is 1.26.0** — raised in Part 3-a
  because goose requires it; see the amendment in
  [ADR-0001](docs/architecture/adr/0001-backend-language.md#amendments).
- **Network speed depends on the user's location.** From one location `proxy.golang.org`
  geoblocks some modules outright (HTTP 403, *"not available in your location"*) and
  throughput is 20–80 KB/s; from another it is ~420 KB/s and everything works. **If a
  module 403s or crawls, ask the user to switch location before redesigning around it.**
- **Postgres for development is `make dev-db`** (container on :5433). `make test-all` runs
  the suite against both engines; plain `make test` **silently skips** Postgres.
- **The test targets pass `-p 1`.** Without it `go test ./...` starts one binary per
  package at once, and several packages hash with Argon2 at 64 MiB under the race
  detector — enough to get the run killed outright on a 22-core machine, and worse on a
  2-core CI runner. Serialized, the whole suite is ~30s. Do not remove it in Part 12.
- `make tools` must be run once per clone to populate `./bin/golangci-lint`.
- **`make gen-check` only means anything on a clean tree.** It diffs `internal/store/`
  against `HEAD`, so run on a dirty one it reports every uncommitted change as stale
  generated code. Run it after committing, not before.
- **Query files must be pure ASCII.** sqlc's SQLite generator rewrites queries by byte
  offset and miscounts on multibyte characters, corrupting output into tokens like
  `SELECid` while pointing at valid SQL. An em dash in a comment cost this part an hour.
  `TestQueryFilesAreASCII` now fails by name instead. Same mechanism as Part 3-b's
  `?`-in-a-comment bug, and `sqlc.arg` is avoided in SQLite files for a third variant of
  it — `TestSQLiteQueriesAvoidNamedArguments` guards that one.
- **Lint enforces US spelling** (`misspell`, `locale: US`) and rejects both `err` shadowing
  (govet) and `err` reassignment (gocritic) — give the inner error a distinct name.

---

## Progress

```
Phase 0  Foundations        [███████████         ] 11/19   (Parts 3, 4, 6 and 7 each split)
Phase 1  Connect & Query    [                    ]  0/12   (detailed at Part 15)
Phase 2+ ...                                            (expanded as we approach)
```

---

# The parts

## Phase 0 — Foundations

> Goal: a single binary that starts, authenticates a user, enforces permissions, and
> serves a React app. Spec: [docs/roadmap/phase-0-foundations.md](docs/roadmap/phase-0-foundations.md)

---

### - [x] Part 0 — Documentation & design corpus ✅ 2026-09-19

**Deliverable:** Vision, tech stack, architecture, security model, data model, 9 ADRs, and
an 11-phase roadmap.

**Done when:** 35 docs committed, all internal links resolve, pushed to GitHub. ✅

---

### - [x] Part 1 — Repository scaffold & toolchain ✅ 2026-09-19

**Deliverable:** A Go module that builds, lints, and tests — with the directory structure
from [README.md](README.md#repository-layout-planned) in place.

**Build:**
- `go.mod` (module `github.com/Mmd4LIFE/pivot`, Go 1.23)
- Directory skeleton: `cmd/pivot/`, `internal/{api,config,store,connectors,semantic,query,authz}/`,
  `web/`, `services/ai/`, `deploy/` — each with a `doc.go` or `.gitkeep` so they're tracked
- `Makefile` with `build`, `run`, `test`, `lint`, `fmt`, `clean`, `help`
- `.golangci.yml` — strict but not hostile (errcheck, govet, staticcheck, revive,
  gosec, ineffassign, misspell)
- `.editorconfig`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `LICENSE` (Apache 2.0)
- `.github/PULL_REQUEST_TEMPLATE.md` with the Definition of Done checklist
- A trivial `cmd/pivot/main.go` that prints the version

**Done when:**
```bash
make build   # produces ./bin/pivot
make lint    # zero warnings
make test    # passes (even with no tests yet)
./bin/pivot  # prints version
```

**Refs:** `P0-REPO-001` … `P0-REPO-006`

---

### - [x] Part 2 — Config, CLI, and a running server ✅ 2026-09-20

**Deliverable:** `./pivot serve` starts an HTTP server with structured logging and shuts
down gracefully.

**Build:**
- CLI (cobra): `serve`, `version`, `config show`, with `--help` that reads well
- Config loader with precedence **flags > env (`PIVOT_*`) > file > defaults**; validation
  with clear errors on bad values
- `slog` structured logging, configurable level and format (json|text)
- HTTP server with graceful shutdown (drain in-flight, 30s timeout), read/write timeouts
- `/healthz` (liveness) and `/readyz` (readiness)

**Done when:**
```bash
./bin/pivot serve &                      # starts, logs structured JSON
curl -s localhost:8080/healthz           # 200
PIVOT_LOG_LEVEL=debug ./bin/pivot serve  # env override works
./bin/pivot serve --port 9000            # flag beats env
kill -TERM <pid>                         # drains and exits 0, no dropped requests
```

**Refs:** `P0-API-001`, `P0-API-009`, `P0-PKG-003`, `P0-PKG-004`, `P0-OBS-003`

---

### - [x] Part 3-a — Migrations, schema v1, and the portability harness ✅ 2026-09-20

*Part 3 was split: it is a full session of migrations plus a full session of sqlc codegen.*

**Deliverable:** Schema v1 running on both Postgres and SQLite, with a harness that proves
they agree.

**Build:**
- Database connectivity with engine detection (`internal/store/db.go`)
- `goose` migrations, forward-only, per dialect in `internal/store/migrations/`
- Schema v1 per [data-model.md](docs/architecture/data-model.md#1-identity--tenancy):
  `organizations`, `users`, `user_attributes`, `groups`, `group_members`
- `pivot migrate up` / `status` / `version` / `create`
- Database readiness check wired into `/readyz`
- Portability harness (`P0-META-004`): the same tests run against both engines
- `deploy/docker-compose.dev.yml` + `make dev-db` / `make test-all`

**Done when:**
```bash
./bin/pivot migrate up                                    # SQLite, from scratch ✅
PIVOT_DATABASE_URL=postgres://... ./bin/pivot migrate up   # Postgres, from scratch ✅
make test-all                                             # BOTH engines ✅
```
Plus: migrations idempotent ✅, foreign keys enforced on both ✅.

**Refs:** `P0-META-001`, `P0-META-002`, `P0-META-004`, `P0-META-006`, `P0-PKG-010`

---

### - [x] Part 3-b — sqlc typed queries ✅ 2026-09-20

**Deliverable:** Typed Go query functions generated from SQL, for both dialects.

**Build:**
- `sqlc.yaml` with a `postgresql` and a `sqlite` output package
- Query sets for orgs, users, user attributes, groups, group members
- Type overrides so both dialects surface `uuid.UUID` and `time.Time`, not `string`
  — this is where the portability tax concentrates, so measure it
- UUID v7 generation helper (`github.com/google/uuid`)
- `make gen` runs `sqlc generate`; generated code is committed
- Round-trip tests on both engines: write a row, read it back, types match

**Done when:**
```bash
make gen        # clean, no diff afterward
make test-all   # round-trip tests pass on BOTH engines
```

**Notes:** `sqlc` needs installing — use the release archive like `make tools` does for
golangci-lint, not `go install`. **Measure the portability tax here** and record it in the
session log: [ADR-0003](docs/architecture/adr/0003-metadata-database.md) sets a 15%
threshold for demoting SQLite to dev-only. Part 3-a's tax was low (one extra schema file,
one `rebind` test helper); the type-override work in 3-b is the real test.

**Refs:** `P0-REPO-008`, `P0-META-002`

---

### - [x] Part 4-a — Tenant scoping, orgs & users repositories ✅ 2026-09-20

*Part 4 was split: the scoping machinery plus two repositories is a session; groups,
membership and the HTTP wiring is another.*

**Deliverable:** A data access layer where returning another tenant's rows is structurally
impossible — not merely unlikely.

**Build:**
- `internal/tenant` — `Scope` with unexported fields, context put/get, fail-closed lookup
- `internal/store/model` — domain types independent of either engine's generated code
- `internal/store/repo` — `Querier` interface + both engine adapters, repository base
- Soft-delete filtering, optimistic concurrency, and change events applied in the base
- `OrganizationRepo` (operates on the caller's own tenant, takes no org ID) and
  `SystemRepo` (explicitly unscoped provisioning)
- `UserRepo` with full CRUD

**Done when:**
- Two orgs with identical data; every scoped method returns zero cross-tenant rows ✅
- Soft-deleted rows invisible to reads ✅
- Stale-version update returns `ErrConflict` ✅
- Every method refuses an unscoped context ✅ *(reflection-driven, mutation-verified)*

**Refs:** `P0-META-003`, `P0-META-006`, `P0-META-007`, `P0-META-008`

---

### - [x] Part 4-b — Groups, membership, and request scoping ✅ 2026-09-20

**Deliverable:** The remaining repositories, and a real `tenant.Scope` on every request.

**Build:**
- `GroupRepo`: CRUD, nesting, membership add/remove/list, `IsMember`
- `UserAttributeRepo`: upsert with provenance, list, delete by source *(feeds Phase 4 RLS)*
- Adapter methods for the remaining 17 generated queries
- **HTTP middleware that builds a `tenant.Scope` and puts it in the request context** —
  stubbed to a single org until Part 6 provides a real session
- An audit subscriber on the change event bus, logging actor + entity + kind

**Done when:**
- The cross-tenant and unscoped-context tests extend to groups and attributes, unchanged
  in shape — the reflection walk should pick the new methods up automatically
- A request without a resolvable tenant is rejected by middleware, not by the repository
- The mutation check still fails when scoping is removed from any new method

**Notes:** The reflection test in `isolation_test.go` finds methods automatically, so new
repositories are covered the moment they are registered in `Repositories`. Verify that by
mutating one, exactly as Part 4-a did — a test that cannot fail is worth nothing.

**Refs:** `P0-META-003`, `P0-META-008`

---

### - [x] Part 5 — HTTP API foundations ✅ 2026-09-21

**Deliverable:** A middleware stack and error contract that every future endpoint inherits.

**Build:**
- Router with a composed middleware chain: request ID → trace → logging → panic recovery →
  CORS → security headers → rate limit → auth (stub for now)
- **Standard error envelope** with stable machine-readable codes (`PIVOT-AUTH-001` style)
  that map to future docs pages
- Request validation at the boundary, not in handlers
- Rate limiting per IP / user / endpoint class
- OpenAPI 3.1 spec skeleton + the generation pipeline (spec is the source of truth)

**Done when:**
```bash
curl -s localhost:8080/api/v1/nonexistent   # structured error JSON with a code
# panic in a handler → 500 with an error code, server stays up, panic is logged
# 100 rapid requests → 429 with Retry-After
```
Plus: `make gen` produces the TS client from the OpenAPI spec without errors.

**Refs:** `P0-API-002` … `P0-API-007`

---

### - [x] Part 6-a — Passwords, sessions, and lockout ✅ 2026-09-21

*Part 6 was split: the security core is a session; the HTTP surface is another.*

**Deliverable:** The authentication domain — hashing, sessions, lockout — with no HTTP.

**Build:**
- Schema v3: `sessions` and `login_attempts`, composite FKs per migration 00002
- Argon2id (m=64MB, t=3), PHC-encoded so cost can be raised without invalidating hashes
- `auth.Service`: login, authenticate, logout, set-password, sweep
- Progressive lockout keyed by **attempted** email, so it covers unknown accounts too
- Session token: 256 bits, stored only as a SHA-256 hash
- `pivot admin create-user` and `admin reset-password`

**Done when:**
- Logout invalidates on the very next call ✅
- Unknown / wrong-password / disabled all return the same error at the same cost ✅
- 10 failures lock the account; lockout expires ✅
- Password hashes and tokens never appear in logs ✅ *(asserted across the whole login path)*

**Refs:** `P0-AUTH-001`, `P0-AUTH-002`, `P0-AUTH-009`

---

### - [x] Part 6-b — Auth endpoints and session-backed scoping ✅ 2026-09-21

**Deliverable:** A user logs in over HTTP and calls an authenticated endpoint.

**Build:**
- `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/me`
- `GET /api/v1/auth/sessions` and `DELETE /api/v1/auth/sessions/{id}`
- `HttpOnly` `Secure` `SameSite=Lax` session cookie; `Secure` omitted on plain HTTP
  so the 30-second local install still works
- **Replace `SingleTenantResolver` with a session-backed `TenantResolver`** — this is the
  swap Part 4-b left a placeholder for
- Apply `LimitAuth` (already defined in Part 5) to the login endpoint, so per-IP throttling
  complements the per-account lockout already built
- Extend `api/openapi.yaml` with every new endpoint and regenerate the TS client

**Done when:**
- `curl` login → cookie set → `/auth/me` returns the user → logout → next call 401 ✅
  *(verified live, not only in tests)*
- A response body never contains a password hash ✅ *(asserted on the JSON of every
  auth response, for `$argon2id$`, `passwordHash`, `tokenHash`, and the password itself)*
- Rapid login attempts from one IP are throttled before Argon2 runs ✅
  *(mutation-verified: removing the limiter from the login chain fails the test)*
- The spec-drift tests still pass with the new paths ✅ *(all 7 documented paths served)*

**Notes:** Login needs an organization before a scope exists. The handler resolves it
from an `organization` slug in the body, or from the only organization when exactly one
exists (the self-hosted case); with several it returns 422 naming the field rather than
picking one.

**Refs:** `P0-AUTH-006` … `P0-AUTH-008`, `P0-API-007`

---

### - [x] Part 7-a — Authorization model, checker, and the assertion harness ✅ 2026-09-21

*Part 7 was split: the permission model and the decision engine are a session; the
middleware and the administrative surface are another.*

**Deliverable:** `authz.Checker` answers `can(user, action, object)`, and the model it
answers from is written down as data.

**Built:**
- **Spiked embedded OpenFGA first**, as this part instructed — see the outcome below
- `internal/authz`: `Checker`, `Request`, `Decision`, `Explanation`, `Enforce`
- Authorization model v1: `organization` → `group` → `user`, with nesting followed upward
- Built-in roles Admin / Editor / Analyst / Viewer, and a permission registry
- Schema v4: `role_assignments`, stored as Zanzibar tuples
- `RoleRepo` (7 methods, picked up by the reflection isolation walk automatically)
- Decision cache: immediate invalidation plus a 3s TTL backstop
- **Declarative harness** — `internal/authz/testdata/model_v1.yaml`, 26 assertions

**Done when:**
- Permission changes take effect in under 5 seconds ✅ *(both mechanisms tested: immediate
  invalidation, and the TTL bounding a stale **allow** when the write happened elsewhere)*
- The harness runs a table of `(user, action, object) → allow|deny` ✅ *(26 assertions ×
  both engines; a test also fails if any registered permission is never asserted)*
- **Fail-closed verified** ✅ *(mutation-verified: making `Enforce` swallow an unavailable
  backend fails the suite by name)*
- A Viewer is denied what an Admin is allowed ✅ *(at the checker; at an endpoint in 7-b)*

**The OpenFGA spike — it works, and Part 7-b decides whether to adopt it.**
`openfga v1.21.0` embeds in-process, pure Go, on `modernc.org/sqlite` and `pgx/v5` — the
same drivers Pivot already uses. It is **not** immature, so ADR-0009's fallback trigger
was not met. Cost: 115 → 244 modules, 16 MB → 26 MB. Phase 0's model is flat and
exercises none of Zanzibar's recursion, which arrives in Phase 4. Deferred behind
`Checker`, with grants stored as tuples and the assertion table as the contract a future
OpenFGA checker must satisfy unchanged. Full reasoning and numbers:
[ADR-0009 amendment](docs/architecture/adr/0009-authorization.md#amendments).

**Refs:** `P0-AUTHZ-002`, `P0-AUTHZ-005`, `P0-AUTHZ-006`

---

### - [x] Part 7-b — Permission middleware and the administrative surface ✅ 2026-09-21

**Deliverable:** An endpoint refuses a caller who lacks the permission, and roles can be
granted over the API.

**Build:**
- `api.RequirePermission(perm)` middleware, composed after the tenant chain so a handler
  never runs without both a scope and a decision
- Role assignment endpoints: grant, revoke, list who holds what
- `GET /api/v1/auth/me` extended with the caller's effective permissions, so the UI can
  hide what it cannot do rather than discovering it by being refused
- Gate the two things Part 6-b deliberately left open:
  - `SessionRepo.Revoke` — the organization-scoped form — has no caller. It is the
    administrative "end someone else's session", and wants `manage_sessions`
  - `SystemRepo` is entirely ungated. Every method on it is unscoped by design, which was
    right while only the CLI and login reached it, and stops being right the moment an
    endpoint does
- `pivot admin grant-role` / `revoke-role`, and **the first user created on a fresh
  install becomes an admin** — otherwise a new instance has nobody who can grant anything
- Refuse removing the last administrator (`RoleRepo.CountHolders` exists for this)
- **Close the referential gap** described in Current state: validate on grant that the
  subject exists in this organization, and call `RoleRepo.RevokeAllForSubject` when a user
  or group is deleted, so an identifier reused by a later import cannot inherit a
  stranger's permissions
- Extend `api/openapi.yaml` and regenerate the TS client

**Done when:**
- A Viewer gets 403 `PIVOT-AUTH-002` on an admin endpoint; an Admin gets through ✅
  *(verified live, and mutation-verified: removing the gate fails the endpoint table)*
- The declarative harness is extended with endpoint-level assertions, unchanged in
  shape ✅ *(13 rows in the same file; both halves read one specification)*
- With the checker unavailable, every gated endpoint returns 503 and **none** returns
  200 ✅ *(and a nil checker denies too — an instance that booted without authorization
  must not serve as though everyone were an administrator)*
- Demoting the last admin is refused ✅ *(verified live: 422, and the admin keeps access)*

**OpenFGA: deferred, as 7-a recommended.** The spike's numbers stand and nothing here
changed them — Phase 0's model is flat and never asks Zanzibar a question only Zanzibar
can answer. The swap remains a Phase 4 decision, and `model_v1.yaml` is what will make it
verifiable rather than hopeful.

**Refs:** `P0-AUTHZ-001`, `P0-AUTHZ-003`, `P0-AUTHZ-004`

---

### - [ ] Part 8 — OIDC single sign-on

**Deliverable:** SSO login working end to end against a real identity provider.

**Build:**
- OIDC Authorization Code + PKCE with auto-discovery
- `identity_providers` table and admin configuration
- Attribute and group claim mapping → `user_attributes` (this feeds RLS in Phase 4)
- Just-in-time user provisioning
- Multi-provider support scaffolding

**Done when:**
- An integration test logs in via a **Keycloak testcontainer**, end to end
- A new user is JIT-provisioned on first login with correct group mapping
- Claims land in `user_attributes` with `source = 'oidc'`

**Refs:** `P0-AUTH-003`, `P0-AUTH-010`

---

### - [ ] Part 9 — Frontend scaffold, embedded in the binary

**Deliverable:** `./pivot serve` serves a React app from the single binary. No Node at
runtime.

**Build:**
- Vite 6 + React 19 + TypeScript 5.6
- TanStack Router (type-safe routes and search params) + TanStack Query
- Tailwind 4 with **design tokens as CSS custom properties** — not compiled class names;
  [white-label embedding depends on this](docs/architecture/adr/0002-frontend-stack.md)
- Generated TS API client wired to the OpenAPI spec
- `embed.FS` integration + SPA fallback routing in Go
- `make dev` runs Vite dev server + Go with hot reload; `make build` embeds the built assets

**Done when:**
```bash
make build && ./bin/pivot serve   # serves the React app from ONE binary
ldd ./bin/pivot                    # no Node runtime anywhere
```
Plus: deep-linking to a client route works (SPA fallback), and `make dev` hot-reloads both
sides.

**Notes:** Node isn't installed on this machine yet, and `npm install` for this dependency
set is a large download on a slow link (see the environment notes in Current state). Hand
the user the install commands rather than running them in-session. Consider `pnpm` for a
smaller, faster install.

**Refs:** `P0-FE-001` … `P0-FE-004`, `P0-API-007`, `P0-PKG-001`

---

### - [ ] Part 10 — Design system core

**Deliverable:** 20+ accessible components in Storybook, light and dark.

**Build:** Radix-based primitives — button, input, select, checkbox, radio, switch, dialog,
dropdown, tooltip, popover, toast, tabs, table, badge, avatar, skeleton, spinner, alert,
card, command palette. Plus Storybook with the a11y addon and a light/dark toggle.

**Done when:**
- Storybook runs; every component has stories for its states
- **Zero axe violations** across all stories
- Every component is keyboard navigable with a visible focus indicator
- Theme switching works at runtime via CSS custom properties (no rebuild)

**Notes:** Build the 20 components Phases 1–2 actually need. This is not a component
library for its own sake — [Phase 0 names scope creep here as a risk](docs/roadmap/phase-0-foundations.md#risks).

**Refs:** `P0-FE-005`, `P0-FE-006`, `P0-FE-009`

---

### - [ ] Part 11 — Auth UI & app shell

**Deliverable:** A user logs in through the browser and lands in the application.

**Build:** Login page, SSO redirect flow, password reset, app shell (nav, breadcrumbs, user
menu, search entry point), error boundaries, offline/disconnected state, i18n scaffold
(`react-i18next`, English complete, **RTL-ready layout using logical properties**).

**Done when:**
- Full browser login → shell → logout flow works against the real backend
- A Playwright E2E test covers it
- 401 responses redirect to login and preserve the intended destination
- Layout uses `margin-inline-start` etc., verified by setting `dir="rtl"`

**Refs:** `P0-FE-007`, `P0-FE-008`, `P0-FE-010`, `P0-FE-011`

---

### - [ ] Part 12 — CI pipeline

**Deliverable:** Every PR runs the full quality gate in under 10 minutes.

**Build:** GitHub Actions — lint (Go/TS), type check, unit + integration tests on a
**Postgres AND SQLite matrix** (Postgres via a service container; `PIVOT_TEST_POSTGRES_URL`
must be set so the Postgres half never silently skips), coverage gate (80% on changed
packages), `govulncheck` + `osv-scanner`, gitleaks secret scanning, bundle-size budget,
axe accessibility scan, Playwright E2E, plus a **Docker image build on every PR** (build
only, push only on `main` and tags) so a broken Dockerfile is caught before release.

**Done when:** A PR triggers everything, all gates pass on `main`, a deliberately broken
PR is correctly blocked, and total wall-clock is under 10 minutes.

**Notes:** Use `make tools` (release archive + checksum verify) for golangci-lint in CI, not
`go install` — building it from source pulls ~400 modules and would blow the 10-minute
budget on its own. Cache `~/go/pkg/mod` and `./bin` between runs.

**Refs:** `P0-CI-001` … `P0-CI-006`, plus the gate table in
[00-principles.md](docs/roadmap/00-principles.md#5-quality-gates-in-ci)

---

### - [ ] Part 13 — Release pipeline & container image

**Deliverable:** A git tag produces signed, verifiable artifacts for 6 platforms **and a
production container image**.

**Build:**
- `Dockerfile` — multi-stage, distroless runtime, non-root user, `HEALTHCHECK` hitting
  `/healthz`, built for `linux/amd64` **and** `linux/arm64`
- Image published to GHCR as `ghcr.io/mmd4life/pivot`, tagged `:latest`, `:vX.Y.Z`, `:sha`
- `.dockerignore` so build context stays small
- GoReleaser (linux/darwin/windows × amd64/arm64)
- SBOM generation, cosign signing of **both** binaries and image
- `CHANGELOG.md` automation, GitHub Release publishing

**Done when:** Tagging `v0.0.1-alpha` produces 6 binaries + a multi-arch image;
`cosign verify` passes on the image; the SBOM is attached; `docker run ghcr.io/mmd4life/pivot`
serves a working instance; a downloaded binary runs on a clean machine.

**Refs:** `P0-CI-007`, `P0-CI-008`

---

### - [ ] Part 14 — Observability

**Deliverable:** A request can be traced end to end.

**Build:** OpenTelemetry tracing with OTLP export, Prometheus metrics at `/metrics`,
`slog` trace correlation, a reference Grafana dashboard, frontend error reporting
(Sentry-compatible, self-hostable).

**Done when:** A single trace spans HTTP → authz → repository → DB; `/metrics` exposes
request rate, duration, and error count; every log line carries its trace ID; the Grafana
dashboard renders against real data.

**Refs:** `P0-OBS-001` … `P0-OBS-005`

---

### - [ ] Part 15 — First-run experience & Phase 0 close-out

**Deliverable:** **The 30-second promise, proven on a clean machine.**

**Build:** Zero-config first run (SQLite auto-created, no config file), admin setup wizard,
`pivot doctor` diagnostics, automatic SQLite backup, envelope encryption for secrets
(local master key), and the **full containerized stack**:
- `deploy/docker-compose.yml` — Pivot + Postgres + Valkey + MinIO, production-shaped
- Uses the published image from Part 13, with a pinned tag
- Health checks and `depends_on: service_healthy` so startup ordering is correct
- `.env.example` documenting every setting
- Volumes for data persistence; a documented backup/restore path

**Done when:**
- On a **clean machine**: download → run → browser → admin created → logged in, **in under
  30 seconds, timed**
- `pivot doctor` correctly diagnoses a broken install
- `docker compose up` brings up the full stack and serves a working Pivot
- **Both install paths verified:** the single binary *and* the container
- **Phase 0 exit criteria all verified** — walk
  [the list](docs/roadmap/phase-0-foundations.md#exit-criteria) and confirm each one
- Expand Phase 1 parts (16–27) in this checklist with the same detail as Phase 0

**Refs:** `P0-PKG-002`, `P0-PKG-004`, `P0-PKG-005`, `P0-PKG-007`, `P0-PKG-008`

---

## Phase 1 — Connect & Query → **v0.1**

> Spec: [docs/roadmap/phase-1-connect-and-query.md](docs/roadmap/phase-1-connect-and-query.md)
> **These are outlined, not yet detailed.** Part 15 expands them before Part 16 starts.

- [ ] **Part 16** — Connector interface, pooling, credential encryption, capability model
- [ ] **Part 17** — Connector conformance test suite *(the highest-leverage item in Phase 1)*
- [ ] **Part 18** — PostgreSQL + MySQL + SQLite/DuckDB connectors
- [ ] **Part 19** — Snowflake + BigQuery connectors
- [ ] **Part 20** — Schema introspection, sync, and type normalization
- [ ] **Part 21** — Column profiling & semantic type inference *(grounds AI in Phase 7)*
- [ ] **Part 22** — Query execution pipeline with Arrow streaming & cancellation
- [ ] **Part 23** — Three-tier cache with **RLS-ready cache keys** *(see [ADR-0006](docs/architecture/adr/0006-caching-strategy.md))*
- [ ] **Part 24** — SQL editor: CodeMirror 6, schema autocomplete, execution
- [ ] **Part 25** — Virtualized result grid (1M+ rows) & exports
- [ ] **Part 26** — Saved questions, collections, search, sharing
- [ ] **Part 27** — Admin UI, sample dataset, **v0.1 release**

---

## Phase 2+ — expanded as we approach

Detailed parts are written at the close-out of the preceding phase, so they reflect what we
actually learned rather than what we guessed. Phase specs already exist in
[docs/roadmap/](docs/roadmap/).

| Phase | Release | Spec |
|---|---|---|
| 2 — Visualize & Dashboard | v0.3 | [phase-2](docs/roadmap/phase-2-visualization-dashboards.md) |
| 3 — Semantic Layer | v0.5 | [phase-3](docs/roadmap/phase-3-semantic-layer.md) |
| 4 — Collaborate & Govern | v0.7 | [phase-4](docs/roadmap/phase-4-collaboration-governance.md) |
| 5 — Alert & Distribute | v0.9 | [phase-5](docs/roadmap/phase-5-alerting-distribution.md) |
| 6 — Flows | **v1.0 GA** | [phase-6](docs/roadmap/phase-6-flows.md) |
| 7 — Pivot AI | v1.5 | [phase-7](docs/roadmap/phase-7-ai.md) |
| 8 — Embed & Extend | v1.8 | [phase-8](docs/roadmap/phase-8-embedded-extensibility.md) |
| 9 — Enterprise & Scale | v2.0 | [phase-9](docs/roadmap/phase-9-enterprise-scale.md) |
| 10 — Ecosystem | continuous | [phase-10](docs/roadmap/phase-10-ecosystem.md) |

---

## Session log

Newest first. Record what **actually** shipped, including what didn't work.

| Date | Part | Shipped | Notes |
|---|---|---|---|
| 2026-09-21 | 7-b | `api.RequirePermission`, the role catalog and role-assignment endpoints, administrative session revoke, effective permissions on `/auth/me`, `pivot admin grant-role` / `revoke-role`, first-user-becomes-admin, last-admin protection, and 13 endpoint-level rows added to the same assertion file | **The endpoint table is the part worth keeping.** `internal/authz` asserts what the checker *decides*; this asserts that the HTTP surface actually *asks* it — a different failure, and the likelier one, since a model can be perfectly correct while a route forgets to be gated. Both halves read one specification file. **Mutation-verified:** dropping `RequirePermission` from the role routes fails three rows by name with `= 200, want 403`. **403 and 503 are deliberately different answers.** A denial is a decision; an unreachable checker is not. Answering 403 during an outage would send a properly-permitted user to argue with an administrator about a permission they already have. A nil checker denies as well, so an instance that booted without authorization wired up cannot serve as though everyone were an admin. **Closed the referential gap 7-a recorded**: grants now verify the subject exists in the caller's organization, and deleting a user or group revokes its grants — the columns are polymorphic so nothing cascades on its own. **The first user in an organization becomes its admin**, without which a fresh install has nobody who can grant anything and is complete and unusable; the second user gets nothing, so it is not a standing escalation. All four `Done when` items verified against a live server, including the last-admin refusal (422, and the admin keeps access afterwards). **US spelling caught me again** — `catalogue` failed lint three times; the environment note exists and I still wrote it. |
| 2026-09-21 | 7-a | `internal/authz` (Checker, Resolver, Cache, Enforce), authorization model v1 with four built-in roles, schema v4 `role_assignments` as Zanzibar tuples, `RoleRepo`, and a declarative assertion file of 26 rows run against both engines | **Split Part 7** — the model and the decision engine are a session, the middleware and admin surface are another. **The OpenFGA spike came back positive, which is not what this part expected.** `openfga v1.21.0` embeds in-process, pure Go, on `modernc.org/sqlite` and `pgx/v5` — our own drivers — so ADR-0009's "if embedded mode proves immature" trigger was *not* met. Measured: 115 → 244 modules, 16 MB → 26 MB. Deferred anyway, on the narrower ground that Phase 0's model is flat and exercises none of Zanzibar's recursion, with the reasoning and numbers recorded as a dated ADR amendment rather than a silent choice. **This is a judgment call worth the user's review**, which is why the spike output is in the ADR rather than only in a commit message. The deferral is made safe by three things, not by hope: grants are stored as tuples so migration is an export plus a `Write`; everything asks through `authz.Checker`; and the model is specified as *data* in `testdata/model_v1.yaml`, which a future OpenFGA checker must satisfy unchanged. **Mutation-verified twice:** making `Enforce` swallow an unavailable backend fails the fail-closed suite by name, and deleting group expansion fails the harness on exactly the inherited-role rows. **A test of mine was wrong and the code was right:** I asserted that a cycle in group nesting should error, but the visited set already resolves it correctly — a cycle means membership in both groups. Split into two honest tests: cycles terminate with an answer, unbounded *chains* hit the depth cap. **An em dash cost an hour.** sqlc's SQLite generator rewrites queries by byte offset and miscounts on multibyte characters, corrupting output into `SELECid` while pointing at valid SQL — the same mechanism as Part 3-b's `?`-in-a-comment bug. `TestQueryFilesAreASCII` and `TestSQLiteQueriesAvoidNamedArguments` now fail by name instead. |
| 2026-09-21 | 6-b | The five `/api/v1/auth/*` endpoints, the session cookie, `api.SessionTenantResolver` replacing the single-tenant stand-in, an `auth` configuration section, spec + TS client, startup sweep of expired sessions | **A test found a real authorization hole.** `SessionRepo.Revoke` is scoped to the organization but not to the user, so any member could have ended any other member's session — the right power for an administrator, the wrong one for the endpoint that manages your own devices. Added `RevokeSessionForUser`, which names the user in the `WHERE` clause, so someone else's session is simply not found. Fixed in SQL rather than with a check in the handler, for the same reason Part 4-b fixed its hole in the schema. **Mutation-verified twice:** swapping `RevokeOwn` back to `Revoke` produces `returned 204, want 404` *and* ends the victim's session; removing the limiter from the login chain makes the throttling test fail. That second test asserts indirectly and is stronger for it — every login that reaches the service records a failed attempt, so the recorded count *is* the number of requests that got past the limiter. **`make test-all` was being killed outright**, which I had assumed was a timeout: `go test ./...` starts one binary per package at once, and several hash with Argon2 at 64 MiB under the race detector. Test targets now pass `-p 1`; the suite runs in ~30s and this matters more on a 2-core CI runner than it did here. All five `Done when` items verified against a live server, including the lockout: 429 `PIVOT-RATE-001` and 429 `PIVOT-AUTH-005` are distinguishable on the wire, which is the concrete case the error registry's "codes are independent of status" rule was written for. |
| 2026-09-21 | 6-a | Schema v3 (`sessions`, `login_attempts`), Argon2id hashing, session tokens, `auth.Service` (login / authenticate / logout / set-password / sweep), progressive lockout, `pivot admin create-user` and `reset-password` | **Split Part 6** — security core is a session, HTTP surface is another. 16 Postgres subtests, 0 skips. Three security properties are tested rather than asserted in a comment: every failed login costs the same (a nonexistent account pays for a dummy Argon2 verification, and the test compares timing ratios); lockout is keyed by the **attempted** email so it applies to addresses that do not exist, which is what stops it being an enumeration oracle; and case variation cannot reset the counter. Two expiries — sliding idle plus a never-extended absolute cap — because with only the idle timeout a stolen token stays valid forever as long as the thief keeps using it. **Two test-fixture bugs found by the code, not by me:** the schema contract test caught the new tables missing from `expectedTables`, and the sessions unique index caught a fixture that truncated UUIDs to 8 characters — UUID v7 is time-sortable, so rows created milliseconds apart share their prefix. Also fixed a portability bug flagged by `unconvert`: `int(syscall.Stdin)` is redundant on Unix but `syscall.Stdin` is a Handle on Windows, so it became `int(os.Stdin.Fd())`. |
| 2026-09-21 | 5 | Error envelope with a 15-code registry, middleware chain (request ID, logging, recovery, security headers, CORS, body limit, rate limit), hand-rolled token-bucket limiter, boundary decoding with validation, `api/openapi.yaml` + TS client generation | All `Done when` checks verified live: unknown API path returns the envelope with a code and request ID; 100 rapid requests produced 43 × 429 with `Retry-After`; a panicking handler returns a coded 500 without leaking the panic value, and the server serves the next request. **Health probes are deliberately exempt from rate limiting** — throttling a readiness probe makes an orchestrator kill a healthy instance exactly when it is busiest. **CORS defaults to closed**, and a wildcard origin combined with credentials is refused rather than silently downgraded, since that combination turns any website into an authenticated client. The request-ID middleware sanitizes and length-bounds a client-supplied value: it lands in every log line for that request, so an unvalidated one is log injection. Spec-drift tests keep `openapi.yaml` honest — a documented path that 404s fails the build. Needed a `Router` type, so `Server.routes` moved and the shutdown tests were rewired to `router.Mux()`. |
| 2026-09-20 | 4-b | `GroupRepo` (CRUD, nesting, membership), `UserAttributeRepo` (provenance-aware upsert), 34 adapter methods, `api.WithTenant` middleware, audit subscriber, **schema v2** | **A test found a real cross-tenant write hole.** The v1 foreign keys on `group_members` and `user_attributes` referenced `groups(id)` and `users(id)` alone, so each key was satisfied independently and `(org_id=A, group_id=A's, user_id=B's)` was accepted — every ID existed, nothing tied the user to the organization the row claimed. Migration 00002 makes the keys composite on `(id, org_id)`; SQLite needed full table rebuilds since it cannot alter a constraint. Fixed at the database level rather than with a check in Go, because "structural, not conventional" is the whole point of Part 4. **Also corrected fiction in this checklist:** the reflection test claimed to pick up new repositories automatically but hardcoded its target list. It now walks `Repositories`' exported fields — 30 methods across 4 repositories — and was mutation-verified on a *newly added* method (`GroupRepo.IsMember`) to prove the discovery works. |
| 2026-09-20 | 4-a | `internal/tenant` scope, `internal/store/model` domain types, `repo` package with both engine adapters, base (scoping + soft delete + version + change events), organizations and users repositories, isolation suite | **Split Part 4** — scoping machinery plus two repositories is a session; groups and HTTP wiring is another. All four `Done when` criteria verified on both engines, 19 Postgres subtests with 0 skips. **The key test was mutation-verified:** removing the scope check from `UserRepo.Get` made `TestEveryMethodRefusesAnUnscopedContext` fail by name on both engines, so the reflection walk genuinely catches drift rather than passing vacuously. Three Go subtleties cost time: struct conversion requires field types to be *identical*, so `model.NullString` had to become an alias for `sql.NullString` rather than an equivalent struct; the limit/offset field-order difference I called cosmetic in 3-b actually **breaks** conversion, so those two adapter methods construct params by name; and `sqlc`'s `rename:` was needed to emit `AvatarURL`, since staticcheck rejects `AvatarUrl` but renaming only in `model` would have broken every conversion. |
| 2026-09-20 | 3-b | sqlc wired for both dialects: 22 queries x 2, generated packages in `internal/store/gen/{pg,lite}`, `dbtypes` custom column types, `make gen` / `gen-check`, round-trip tests on both engines | **Two sqlc bugs cost most of the session.** (1) A literal `?` inside a SQL *comment* is counted as a placeholder, shifting substitution offsets and corrupting output into tokens like `RETURNINid` — the comment explaining the placeholder rule was itself breaking generation. (2) A placeholder in a SQLite `DO UPDATE` clause is emitted in the SQL but *omitted from the bound arguments*, so the upsert would have failed at runtime with an argument-count mismatch; fixed by routing `updated_at` through the INSERT column list and reading it back via `excluded`. Also: numbered params are mis-substituted, and `LIMIT` infers `int32` on Postgres vs `int64` on SQLite (fixed with `sqlc.arg(...)::bigint`). **Portability tax measured: ~16%**, marginally over ADR-0003's threshold — recorded as a dated measurement in the ADR with the reasoning for keeping SQLite. The `dbtypes` overrides make both generated packages byte-identical apart from the package clause, so Go permits direct struct conversion and Part 4 needs one conversion per type rather than a per-engine mapping. |
| 2026-09-20 | 3-a | Store package with engine detection, goose migrations for both dialects, schema v1 (5 tables), `pivot migrate up/status/version/create`, DB readiness check on `/readyz`, portability harness, dev Postgres compose + `make test-all` | **Split Part 3** — migrations and sqlc codegen are a session each. Verified on both engines: SQLite and Postgres migrate from scratch, idempotent on re-run, 12 Postgres subtests ran with **0 skips**. **`go mod tidy` failed on geoblocking** — `proxy.golang.org` returned HTTP 403 *"this service is not available in your location"* for `modernc.org/sqlite` while serving cobra/goose fine; the user changed location and it worked at 420 KB/s (vs 20–80 before). **Go floor raised 1.23 → 1.26** (goose needs 1.26, modernc needs 1.25); recorded as an amendment in ADR-0001 rather than a new ADR, since the decision (Go) is unchanged. Guessed the goose v3 API wrong in three places — read the actual structs in the module cache to fix. **Portability tax so far: low** — one extra schema file and a `rebind` test helper; the real test is 3-b's type overrides. Docker made explicit across Parts 12/13/15 and the Phase 0 spec at the user's request. |
| 2026-09-20 | 2 | cobra CLI (`serve`, `version`, `config show`, `config env`), hand-rolled config precedence with YAML + `PIVOT_*` env table, slog JSON/text logging, HTTP server with health/readiness and graceful drain | All `Done when` checks verified live, including SIGTERM → exit 0. **A test caught a real design gap:** flipping readiness before `http.Shutdown` is decorative, because Shutdown stops accepting immediately, so a load balancer polling `/readyz` gets a connection refusal rather than a 503. Added `server.preShutdownDelay` (lame-duck period, default 0 so local Ctrl-C stays instant; set ~5s behind a load balancer) and verified 200 → SIGTERM → 503-while-accepting → clean exit. Also fixed `Duration` YAML parsing — yaml.v3 renders the scalar `120` as the string `"120"`, so the tag must be checked rather than attempting a string decode first. Chose cobra + yaml.v3 over viper: 4 modules instead of dozens, which matters on this network, and precedence is the property the tests must prove. |
| 2026-09-19 | 1 | Go module + package skeleton, Makefile (14 targets), strict golangci-lint config, `internal/version` with link-time stamping, `pivot version`/`help`, Apache 2.0 license, CONTRIBUTING / CoC / SECURITY / CHANGELOG, PR template with the full DoD | All four `Done when` checks pass from clean. **Go was not installed** — user installed 1.27.1 via snap after a direct download crawled at 18–28 KB/s. **`go install golangci-lint` failed** on `sum.golang.org` timeouts (~400 modules); switched `make tools` to the checksum-verified release archive, which is better for CI anyway. Corrected two planning errors: pinned golangci-lint `v2.6.2` doesn't exist (actual `v2.13.2`, and v2 uses a new config schema), and `run()` took `*os.File` despite its comment promising an injected writer — now `io.Writer`, which is what makes `main_test.go` possible. |
| 2026-09-19 | 0 | Full design corpus: vision, tech stack, system/security/data architecture, 9 ADRs, 11-phase roadmap, NFRs, feature matrix | 35 docs, ~6.6k lines. All internal links verified. Pushed to GitHub. |
