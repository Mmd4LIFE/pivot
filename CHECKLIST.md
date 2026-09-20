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
| **Last completed** | Part 3-a — Migrations, schema v1, portability harness |
| **Next up** | **Part 3-b — sqlc typed queries** |
| **Current phase** | Phase 0 — Foundations |
| **Branch** | `main` |
| **Blockers** | None |
| **Repo** | https://github.com/Mmd4LIFE/pivot |

**Where the code stands:** `./bin/pivot serve` runs an HTTP server with structured JSON
logging, `/healthz`, `/readyz` (including a database check), and a graceful drain on
SIGTERM. `pivot migrate up|status|version|create` manages schema v1 on **both** SQLite and
Postgres. `pivot config show|env` reports configuration. Packages with real code:
`version`, `config`, `logging`, `api`, `cli`, `store`. Still `doc.go` stubs: `authz`,
`connectors`, `semantic`, `query`.

**Dependencies:** cobra, yaml.v3, goose, pgx/v5, modernc.org/sqlite (pure Go — no CGo, so
Part 13's six-platform cross-compile stays a single build matrix).

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
- `make tools` must be run once per clone to populate `./bin/golangci-lint`.
- **Lint enforces US spelling** (`misspell`, `locale: US`) and rejects both `err` shadowing
  (govet) and `err` reassignment (gocritic) — give the inner error a distinct name.

---

## Progress

```
Phase 0  Foundations        [███▊                ]  3/16   (Part 3 split into 3-a/3-b)
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

### - [ ] Part 3-b — sqlc typed queries

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

### - [ ] Part 4 — Tenant-scoped repository layer

**Deliverable:** A data access layer where returning another tenant's rows is structurally
impossible — not merely unlikely.

**Build:**
- Request-scoped tenant context, propagated via `context.Context`
- Repository base that **injects `org_id` into every query**; no method can be written that
  omits it
- Soft-delete filtering applied at the repository layer, not per call site
- Optimistic concurrency via the `version` column
- Entity-change event stream hook (feeds audit and search later)
- CRUD repositories for orgs, users, groups

**Done when:**
- A test creates two orgs with identical data and proves **every** repository method scoped
  to org A returns zero org-B rows
- A test proves a soft-deleted row is invisible to normal reads
- A test proves a stale-version update fails with a conflict error
- An attempt to construct an unscoped query fails to compile or panics in tests

**Notes:** [Phase 0 calls this the most important item in the phase](docs/roadmap/phase-0-foundations.md#3-metadata-layer).
Retrofitting tenant isolation later is a rewrite. Take the time here.

**Refs:** `P0-META-003`, `P0-META-006`, `P0-META-007`, `P0-META-008`

---

### - [ ] Part 5 — HTTP API foundations

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

### - [ ] Part 6 — Authentication: passwords & sessions

**Deliverable:** A user can log in and call an authenticated endpoint.

**Build:**
- Argon2id password hashing (m=64MB, t=3, p=4)
- Server-side sessions (**not JWTs** — see
  [security-model.md](docs/architecture/security-model.md#sessions-are-server-side-deliberately)),
  `HttpOnly` `Secure` `SameSite=Lax` cookies, configurable idle + absolute timeout
- `POST /auth/login`, `POST /auth/logout`, `GET /auth/me`
- Immediate session revocation; session list endpoint
- Brute-force protection: per-IP and per-account rate limiting, progressive lockout,
  identical responses for unknown-user and wrong-password
- `pivot admin create-user` CLI

**Done when:**
- Login sets a cookie; `/auth/me` returns the user; logout invalidates **immediately**
- A revoked session is rejected on the very next request
- 10 failed logins trigger lockout; the response is timing-safe and does not leak existence
- Password hashes never appear in logs or API responses (assert this in a test)

**Refs:** `P0-AUTH-001`, `P0-AUTH-002`, `P0-AUTH-006` … `P0-AUTH-009`

---

### - [ ] Part 7 — Authorization skeleton (OpenFGA)

**Deliverable:** Permission checks enforced in middleware, with a declarative test harness.

**Build:**
- OpenFGA integration — **embedded mode first** (single-binary requirement)
- Authorization model v1: `organization` → `group` → `user`, role assignment
- Built-in roles: Admin, Editor, Analyst, Viewer
- Resource-scoped permission middleware
- Decision caching (in-process for now; Valkey when multi-node arrives) with invalidation
  on relationship writes
- **Declarative permission test harness** — assertions as data, not as hand-written tests

**Done when:**
- A Viewer is denied an Admin endpoint; an Admin is allowed
- Permission changes take effect in under 5 seconds (assert with a test)
- The test harness runs a table of `(user, action, object) → allow|deny` assertions
- **Fail-closed verified:** with OpenFGA unavailable, all access is denied, not granted

**Notes:** [ADR-0009](docs/architecture/adr/0009-authorization.md) names a fallback —
hand-rolled RBAC behind the same interface — if embedded OpenFGA proves immature. **Spike
this first.** If it's not viable, take the fallback and write the ADR update rather than
fighting it.

**Refs:** `P0-AUTHZ-001` … `P0-AUTHZ-006`

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
| 2026-09-20 | 3-a | Store package with engine detection, goose migrations for both dialects, schema v1 (5 tables), `pivot migrate up/status/version/create`, DB readiness check on `/readyz`, portability harness, dev Postgres compose + `make test-all` | **Split Part 3** — migrations and sqlc codegen are a session each. Verified on both engines: SQLite and Postgres migrate from scratch, idempotent on re-run, 12 Postgres subtests ran with **0 skips**. **`go mod tidy` failed on geoblocking** — `proxy.golang.org` returned HTTP 403 *"this service is not available in your location"* for `modernc.org/sqlite` while serving cobra/goose fine; the user changed location and it worked at 420 KB/s (vs 20–80 before). **Go floor raised 1.23 → 1.26** (goose needs 1.26, modernc needs 1.25); recorded as an amendment in ADR-0001 rather than a new ADR, since the decision (Go) is unchanged. Guessed the goose v3 API wrong in three places — read the actual structs in the module cache to fix. **Portability tax so far: low** — one extra schema file and a `rebind` test helper; the real test is 3-b's type overrides. Docker made explicit across Parts 12/13/15 and the Phase 0 spec at the user's request. |
| 2026-09-20 | 2 | cobra CLI (`serve`, `version`, `config show`, `config env`), hand-rolled config precedence with YAML + `PIVOT_*` env table, slog JSON/text logging, HTTP server with health/readiness and graceful drain | All `Done when` checks verified live, including SIGTERM → exit 0. **A test caught a real design gap:** flipping readiness before `http.Shutdown` is decorative, because Shutdown stops accepting immediately, so a load balancer polling `/readyz` gets a connection refusal rather than a 503. Added `server.preShutdownDelay` (lame-duck period, default 0 so local Ctrl-C stays instant; set ~5s behind a load balancer) and verified 200 → SIGTERM → 503-while-accepting → clean exit. Also fixed `Duration` YAML parsing — yaml.v3 renders the scalar `120` as the string `"120"`, so the tag must be checked rather than attempting a string decode first. Chose cobra + yaml.v3 over viper: 4 modules instead of dozens, which matters on this network, and precedence is the property the tests must prove. |
| 2026-09-19 | 1 | Go module + package skeleton, Makefile (14 targets), strict golangci-lint config, `internal/version` with link-time stamping, `pivot version`/`help`, Apache 2.0 license, CONTRIBUTING / CoC / SECURITY / CHANGELOG, PR template with the full DoD | All four `Done when` checks pass from clean. **Go was not installed** — user installed 1.27.1 via snap after a direct download crawled at 18–28 KB/s. **`go install golangci-lint` failed** on `sum.golang.org` timeouts (~400 modules); switched `make tools` to the checksum-verified release archive, which is better for CI anyway. Corrected two planning errors: pinned golangci-lint `v2.6.2` doesn't exist (actual `v2.13.2`, and v2 uses a new config schema), and `run()` took `*os.File` despite its comment promising an injected writer — now `io.Writer`, which is what makes `main_test.go` possible. |
| 2026-09-19 | 0 | Full design corpus: vision, tech stack, system/security/data architecture, 9 ADRs, 11-phase roadmap, NFRs, feature matrix | 35 docs, ~6.6k lines. All internal links verified. Pushed to GitHub. |
