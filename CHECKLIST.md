# Pivot Build Checklist — Phase 1

**The single source of truth for what to build next.** One part per session.

Phase 0 is complete and closed: its checklist is
[docs/checklists/phase-0-checklist.md](docs/checklists/phase-0-checklist.md),
and what it produced and what it left behind are in
[docs/roadmap/phase-0-exit-review.md](docs/roadmap/phase-0-exit-review.md).

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
   (e.g. `P1-CONN-001`) map to specs in [docs/roadmap/](docs/roadmap/).
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
- **`main` is protected.** Five CI checks are required, so every part ends as a pull
  request the user merges — not a push to `main`.

---

## 📍 Current state

| | |
|---|---|
| **Last completed** | Part 16 — The connector interface |
| **Next up** | **Part 17 — The conformance suite** |
| **Current phase** | Phase 1 — Connect & Query → v0.1 |
| **Branch** | `main` |
| **Blockers** | None |
| **Repo** | https://github.com/Mmd4LIFE/pivot |

**Where the code stands.** One binary, 30 MB, no runtime dependencies, serving an API and
a React application from one process against SQLite or PostgreSQL.

A person can download it, run it with no arguments, open a browser, become the
administrator and sign in. They can change their own password, see every device they are
signed in on and end any of them. An organization can point Pivot at an OIDC provider and
log in through it — verified against a real Keycloak, form and all. Roles and permissions
resolve through direct grants and group inheritance, and every query is tenant-scoped in
the data layer rather than in whatever the handler remembered.

Operationally: `/healthz`, `/readyz`, a graceful drain, structured JSON logs, Prometheus
metrics with a reference Grafana dashboard, OpenTelemetry tracing, browser errors reported
back with the trace of the call that failed, `pivot doctor`, `pivot backup`/`restore`, and
stored secrets encrypted at rest with a rotation procedure that has been walked.

**Pivot can now connect to an external PostgreSQL**, store its credentials encrypted,
test it with errors somebody can act on, and read its schema. That is `internal/connectors`
and the `connections` table; `pivot admin add-connection` is the way in until Part 26
builds the screens. `internal/query` and `internal/semantic` are still `doc.go` stubs, so
there is no way to *run* a question and nothing to show the answer in.

**Carried in from Phase 0**, and owned by parts in this phase or named in them:
- **The container stack now pins a release that predates nothing** — `v0.0.2-alpha` was
  cut on 2026-09-26. `deploy/docker-compose.yml` and `deploy/.env.example` still default
  to `0.0.1-alpha` and should move to it.
- The Keycloak round trip is opt-in and does not run in CI. It belongs in a scheduled job:
  what it protects against is Keycloak changing, not Pivot changing.
- `X-Forwarded-For` is not trusted, so rate limiting behind a proxy keys on the proxy.
  Phase 9 owns it; it matters more as soon as an instance is worth exposing.
- The container stack pins `0.0.1-alpha`, which predates the setup wizard. **The release
  containing Phase 0 has to be cut**, and `deploy/docker-compose.yml` and
  `deploy/.env.example` updated to it.

**Environment.** Go 1.27.1 locally with a `toolchain go1.26.8` directive — the floor in
`go.mod` is for contributors, the toolchain line is what builds, and CI derives its Go
version from it. Node 20.16. `-p 1` on test targets, because several packages hash with
Argon2 at 64 MiB and a parallel run under `-race` gets killed on a two-core machine.
**CI counts fewer coverage statements than Go 1.27 does**, so the local percentage reads a
few points high; leave margin above 80%.

---

## Progress

```
Phase 0  Foundations        [██████████████████████████] 31/31   COMPLETE
Phase 1  Connect & Query    [██                        ]  1/12
Phase 2+ ...                                            (expanded as we approach)
```

---

# The parts

## Phase 1 — Connect & Query → **v0.1**

**Goal:** make Pivot useful for exactly one person — an analyst who can write SQL.
Connect to a database, browse the schema, write a query, see results, save it, share it.

**Spec:** [docs/roadmap/phase-1-connect-and-query.md](docs/roadmap/phase-1-connect-and-query.md)

**The order is deliberate.** The connector interface and its conformance suite come before
any second connector, and the execution engine comes before any UI that shows results. The
expensive mistake in this phase is building a SQL editor against one hard-coded database
and discovering the abstraction afterwards.

---

### - [x] Part 16 — The connector interface, and one connector behind it ✅ 2026-09-27

**Deliverable:** Pivot can connect to a PostgreSQL somebody else owns, and the shape of
that is an interface rather than a special case.

**Build:** `internal/connectors` (the `Connector` interface, a registry, `SQLConnector`
over database/sql, the PostgreSQL dialect), migration 00006 with the `connections` table,
`ConnectionRepo`, and `pivot admin add-connection|test-connection|list-connections`.

**Done when:**
- [x] **A connection to a real external PostgreSQL is created, tested and stored**, and
      the stored password is `pivot.v1.4a44cfc0.…` rather than a string — checked with SQL
      against the file, on both engines
- [x] **All four failure modes say what to do**, each caused for real rather than mocked:
      a refused password, a host that does not resolve, nothing listening on the port, and
      a database that does not exist. None of them contains the password
- [x] Capabilities are declared rather than assumed, including the identifier quoting
      rule — which is the injection surface, and is tested with `"; DROP TABLE x; --`
- [x] **Pooling is per connection and capped**: twenty concurrent queries through a pool
      of two all succeed, and `Close` releases it so a deleted connection does not hold a
      pool against somebody's warehouse until Pivot restarts
- [x] Cancellation reaches the server and a timeout is reported as one, both verified
      against a real `pg_sleep`

**Decisions worth keeping:**
- **`Dialect` is not `Connector`.** Pooling, row scanning, truncation and timeouts are
  identical for every database/sql source, so they live in `SQLConnector` once and a
  driver supplies only the DSN, the capabilities, the catalog query and the error
  classification. BigQuery is not database/sql-shaped, so this is a helper that implements
  the interface rather than the interface itself
- **A truncated result carries a flag.** A silently cut result is a wrong answer presented
  as a right one, and the chart Phase 2 draws from it is wrong in a way nobody can see
- **The DSN is built with `net/url`.** A generated password contains a colon, an at sign
  or a slash about a third of the time, and concatenation turns that into a DSN naming a
  different host
- **`Query` still takes a string.** The package comment promised compiled query objects,
  and Phase 3 owns the compiler that produces them. Part 20 replaces the string; inventing
  the type now would be designing against an imaginary caller

**Found on the way:**
- **`--host` and `--port` collided with the root command's persistent flags**, so
  `--port 5433` set the *server* port to zero and the command failed validation before it
  ran. They are `--db-host` and `--db-port`, with the reason in the code
- **The pgx driver was not registered** in this package — it was reaching `internal/store`
  by luck of import order. It is imported where it is used now
- **My own error message hid its cause.** `sql.Open` failing said "could not prepare a
  postgres connection" and nothing else, and sent me looking in the wrong place. At that
  point nothing secret can be in the error, so it carries the driver's text

**Refs:** `P1-CONN-001`, `P1-CONN-002`, `P1-CONN-003`, `P1-CONN-004`, `P1-CONN-007`,
`P1-DB-001`

---

### - [ ] Part 17 — The conformance suite

**Deliverable:** one test suite every connector must pass, and PostgreSQL passing it.

**Build:** `internal/connectors/conformance`: type mapping across every source type, NULL
handling, timezone and timestamp semantics, large result streaming, cancellation
propagation, error classification, unicode, identifier quoting.

**Done when:**
- The suite runs against a real containerized PostgreSQL and passes
- It is a *library* a connector's own test file invokes, not a suite that knows about
  connectors — so adding a connector means writing one file
- A deliberately broken connector fails it, and the failure names the property rather
  than the assertion

**Notes:** **This is the highest-leverage item in the phase.** Written once, every
subsequent connector is a week instead of a month, and the long tail of "dates are off by
one in Redshift" never happens.

It goes before the second connector on purpose: a suite written after two connectors exist
gets shaped to what those two already do.

**Refs:** `P1-CONN-008`

---

### - [ ] Part 18 — Two more connectors, and resource governance

**Deliverable:** MySQL and SQLite/DuckDB, both through the same door — and no query can
take the instance down.

**Build:** The MySQL and SQLite/DuckDB connectors against the conformance suite, plus
per-connection resource governance: max rows, statement timeout, concurrency.

**Done when:**
- Both connectors pass the conformance suite unmodified, or the suite changed for a
  reason that is written down
- A query that returns more rows than the limit is **truncated with a signal**, not
  silently cut
- A query that runs longer than its timeout is cancelled *at the source*, verified per
  connector rather than assumed from a context deadline
- The DuckDB connector reads a file, which is what makes the sample dataset possible

**Notes:** The second and third connectors are where the interface from Part 16 gets its
first real test. Expect to change it; that is what this part is for.

**Refs:** `P1-DB-002`, `P1-DB-003`, `P1-CONN-010`, `P1-QE-004`, `P1-QE-005`

---

### - [ ] Part 19 — The schema catalog

**Deliverable:** Pivot knows what is in the database it is connected to.

**Build:** Introspection (databases, schemas, tables, columns, types), normalization to a
canonical Pivot type system, a scheduled sync with change detection, and the stored
catalog.

**Done when:**
- Every source type maps to a canonical type, and an unmapped type is an explicit
  "unknown" rather than a guess that is wrong later
- A sync detects added, removed and changed columns rather than replacing the catalog
- Introspecting a large schema does not hold a connection for the whole of it
- The catalog is tenant-scoped like everything else

**Notes:** The type system is the load-bearing decision. Phase 2 picks chart types from
it, Phase 3 builds the semantic layer on it, and Phase 7 grounds the AI in it. A type
system that says "string" for everything makes all three worse.

**Refs:** `P1-CAT-001`, `P1-CAT-002`, `P1-CAT-003`, `P1-CAT-004`

---

### - [ ] Part 20 — The execution pipeline

**Deliverable:** a query goes in, rows stream out, and it can be stopped.

**Build:** `internal/query`: parse → authorize → plan → execute → stream, Arrow-native
result streaming, cancellation propagated to the source, and the query log.

**Done when:**
- A large result streams rather than being assembled in memory — demonstrated against a
  result bigger than the server's budget
- Cancellation reaches the source database, verified per connector
- Every execution is logged with user, SQL, duration, rows, bytes, cache status and error
- Authorization happens **before** execution, in the pipeline, not in the handler

**Notes:** This is the hardest infrastructure problem in the phase and everything after it
depends on the shape. Arrow is chosen so that Phase 2's charts and Phase 6's flows do not
each invent a result format.

**Refs:** `P1-QE-001`, `P1-QE-002`, `P1-QE-003`, `P1-QE-007`

---

### - [ ] Part 21 — The result cache

**Deliverable:** the same question asked twice costs once.

**Build:** L1 in-process cache, cache key derivation, invalidation, and the metrics to see
whether it is working.

**Done when:**
- **The cache key includes the identity of the requesting user** wherever row-level
  security could apply, and a test proves two users with different policies cannot share
  an entry
- A cached result is returned without touching the source, and the query log says so
- p95 for a cached query is under 200 ms, measured

**Notes:** **`P1-QE-009` is the one to be careful about.** Getting the key wrong means user
A sees user B's rows — a data breach delivered by a performance optimization. The key is
derived from the compiled query *plus* the resolved policy set, so identical policies share
and different ones never can.

It is designed now, in Phase 1, although row-level security does not ship until Phase 4,
because retrofitting identity into a cache key means invalidating every assumption built on
top of it. L2 (Valkey) and L3 (object storage) wait until there is something to measure.

**Refs:** `P1-QE-008`, `P1-QE-009`

---

### - [ ] Part 22 — Governance and the query monitor

**Deliverable:** an administrator can see what is running and stop it.

**Build:** Concurrent query governance (per-user and per-connection queues), org-level
timeouts, and the query monitor: running queries, kill, per-user usage.

**Done when:**
- One user cannot exhaust a connection's capacity for everybody else
- An administrator can see a running query and kill it, and the kill reaches the source
- The limits are visible in the product rather than only in a config file

**Refs:** `P1-QE-006`, `P1-QE-010`, `P1-ADM-003`

---

### - [ ] Part 23 — The SQL editor

**Deliverable:** somebody can write a query in Pivot and run it.

**Build:** CodeMirror 6 with per-dialect highlighting, schema-aware autocomplete, execute /
cancel / run-selection, a multi-tab workspace with persisted state, inline errors mapped to
line and column, and query history.

**Done when:**
- Autocomplete knows the tables and columns of the connection in the current tab
- An error from the source is shown at the line it came from, not in a toast
- Tabs survive a reload, because losing a half-written query to a refresh is the thing
  people never forgive
- **The bundle budget still passes.** CodeMirror is the largest frontend dependency this
  product will take; it is lazy-loaded or the budget is renegotiated in writing

**Notes:** The budget is 200 KB gzipped for the initial load and Phase 0 left it at
185.7 KB. CodeMirror does not fit in 14 KB, so this part either code-splits the editor
route or changes the NFR deliberately. It does not quietly exceed it.

**Refs:** `P1-SQL-001` … `P1-SQL-004`, `P1-SQL-006`, `P1-SQL-007`, `P1-SQL-010`

---

### - [ ] Part 24 — Results and export

**Deliverable:** the answer, on screen and out of the building.

**Build:** A virtualized result grid, type-aware cell formatting, column operations, and
export to CSV, TSV, JSON, Excel and Parquet — streaming for results larger than memory.

**Done when:**
- The grid handles a million rows without the tab becoming unusable
- **A 10M-row export streams to CSV without the server exceeding its memory budget**,
  measured rather than assumed
- Formatting is driven by the canonical types from Part 19, not by guessing from values

**Refs:** `P1-RES-001` … `P1-RES-005`

---

### - [ ] Part 25 — Saved questions

**Deliverable:** a query somebody wrote once can be found again by somebody else.

**Build:** Save with name, description and tags; personal and shared collections; search;
share by link with permission inheritance; run-on-open with a freshness policy.

**Done when:**
- Sharing a question shares the *permission* to run it, and running it uses the sharer's
  connection rather than granting the reader new access
- Search finds a question by its name, its description and its SQL
- A shared link respects the same authorization as the application

**Notes:** The full collection model is Phase 4's. This is the minimum that makes a saved
question findable, and the permission inheritance is the part not to improvise.

**Refs:** `P1-Q-001`, `P1-Q-002`, `P1-Q-005`, `P1-Q-006`, `P1-Q-007`

---

### - [ ] Part 26 — Administration

**Deliverable:** the screens an administrator needs to run this for other people.

**Build:** Connection management (CRUD, test, permissions), user and group management,
instance settings, SMTP configuration with a test send.

**Done when:**
- A connection can be created, tested and permissioned from the browser, and its
  credentials never come back out of the API
- Users and groups can be managed without the CLI — the CLI stays for the cases where
  nobody can log in
- The SMTP test send actually sends, and its failure says which part failed

**Refs:** `P1-ADM-001`, `P1-ADM-002`, `P1-ADM-004`, `P1-ADM-005`

---

### - [ ] Part 27 — The sample dataset, and Phase 1 close-out

**Deliverable:** a new Pivot has something to show before anybody finds their warehouse
credentials — and the phase is reviewed honestly.

**Build:** An embedded sample dataset (DuckDB, a realistic e-commerce schema), a guided
first query, and the Phase 1 exit review.

**Done when:**
- A first run has a connection, a schema to browse and a question to run, without
  credentials
- **Phase 1 exit criteria all walked**, one at a time, with what was actually run
  recorded — the same way Phase 0 exited
- The release for v0.1 is cut, signed, and the container stack points at it
- Phase 2 parts are expanded in a new checklist, and this one is archived beside Phase 0's

**Notes:** **This matters more than its size suggests.** A BI tool with no data is an empty
form, and the sample dataset is what makes the thirty-second promise demonstrable rather
than merely true.

**Refs:** `P1-ADM-006`, plus the
[exit criteria](docs/roadmap/phase-1-connect-and-query.md#exit-criteria)

---

## Phase 2+ — expanded as we approach

[Phase 2 — Visualization & Dashboards](docs/roadmap/phase-2-visualization-dashboards.md)
is summarized in [docs/roadmap/phases-0-to-2.md](docs/roadmap/phases-0-to-2.md) and gets
its own checklist at Part 27, on the same evidence-first terms: a part is not done because
the code was written.

---

## Session log

Newest first. Record what **actually** shipped, including what didn't work.

Phase 0's log is in
[docs/checklists/phase-0-checklist.md](docs/checklists/phase-0-checklist.md#session-log) —
31 rows, kept because the failures in it are the useful part.

| Date | Part | Shipped | Notes |
|---|---|---|---|
| 2026-09-27 | 16 | `internal/connectors` (interface, registry, `SQLConnector`, PostgreSQL), migration 00006 and the `connections` table, `ConnectionRepo`, and three `pivot admin` commands | **Pivot connects to a database somebody else owns.** Created, tested and stored against a real PostgreSQL, with the password landing as `pivot.v1.4a44cfc0...` rather than a string — checked with SQL against the file on both engines, because asking the repository whether it encrypted something is asking the guard whether the door is locked. **All four failure modes were caused for real rather than mocked**: a refused password, a host that does not resolve, a closed port, a missing database. Each says what to do and none contains the password. **The interface is the decision this part exists for.** `Dialect` is not `Connector`: pooling, scanning, truncation and timeouts are identical for every database/sql source, so they live in `SQLConnector` once and a driver supplies the DSN, the capabilities, the catalog query and the error classification — with BigQuery in mind, which is not database/sql-shaped, so this is a helper implementing the interface rather than the interface itself. **A truncated result carries a flag**, because a silently cut result is a wrong answer presented as a right one and the chart Phase 2 draws from it is wrong in a way nobody can see. **The DSN is built with `net/url`**: a generated password contains a colon, an at sign or a slash about a third of the time, and concatenation turns that into a DSN naming a different host. **`Query` still takes a string** — the package comment promised compiled query objects and Phase 3 owns the compiler that makes them; inventing the type now would be designing against an imaginary caller, so Part 20 replaces it. **Three things bit me.** `--host` and `--port` collided with the root command's persistent flags, so `--port 5433` set the *server* port to zero and the command failed before it ran; they are `--db-host` and `--db-port` now. The pgx driver was not registered in this package and had been reaching it by luck of import order. And my own error message hid its cause — `sql.Open` failing said "could not prepare a postgres connection" and nothing else, which sent me looking in the wrong place for ten minutes; at that point nothing secret can be in the error, so it carries the driver's text. **The portability harness caught the new table** before I remembered to declare it, and `sqlc.yaml`'s own warning caught the SQLite type overrides I had not added — the two models had silently diverged on eleven columns. |
