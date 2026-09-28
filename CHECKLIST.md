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
| **Last completed** | Part 19-b — The stored catalog, and a sync that diffs |
| **Next up** | **Part 19-c — Foreign keys, and a sync that runs itself** |
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

**There is now a bar every connector has to clear.**
`internal/connectors/conformance` is sixteen named properties — NULL against empty, unicode
byte-for-byte, zoned against naive timestamps, streaming order, truncation, cancellation,
timeouts, error classification, identifier quoting, and every declared capability
demonstrated rather than believed. It is a library, not a runner: a connector supplies one
file and calls `conformance.Run`.

**PostgreSQL, MySQL and SQLite all pass all seventeen** — in CI, against real databases.
Each new connector has cost the abstraction exactly one change, which is the shape you want:
MySQL wanted unsigned integers in the readers and [`Canceler`](internal/connectors/sqlbase.go)
in the interface; SQLite wanted a subject to be allowed to build its own fixture, because a
connector opened read-only cannot. The seventeenth property is concurrency, added when
cancellation grew a per-query connection worth proving safe.

**SQLite's suite needs no container and no environment variable**, so it runs on a bare
`go test ./...`. The conformance suite is now exercised against a real database on every
run rather than only when somebody remembers to start one.

**Per-connection limits are proven rather than declared.** Row cap, query timeout and pool
size, checked against every connector the run can reach — and the pool check asserts
`WaitCount`, so it fails if the cap never actually bound instead of passing on a fast
machine where nothing overlapped.

**Every column carries a canonical type.** `internal/datatype` is sixteen kinds with a
consumer each, and the two distinctions that pay for the package: exact against approximate
numbers, and an instant against a clock reading. A type nobody has mapped is `unknown`
carrying the source's own spelling, which is a real answer and the way the gaps stay
findable. Two conformance properties check it against four real databases, on the query
path *and* the catalog path — which was worth doing, because PostgreSQL's two vocabularies
disagree on 13 of 23 columns and MySQL's `TIMESTAMP`/`DATETIME` naming is **inverted**
relative to the standard.

**Pivot remembers what is in a connected database, and can say what changed.**
`catalog_tables` and `catalog_columns` hold it; `internal/catalog` reconciles it. A sync
upserts what the source reports, sweeps what it did not, and prints the difference —
`changed public.orders.total: type numeric became text` rather than "Synced.". Nothing is
deleted: a table that disappears is marked gone, because a source drops one for reasons that
are not "somebody dropped it", and a delete would take its history and every model pointing
at it. `pivot admin sync-catalog <slug>` runs one.

**DuckDB exists and is not in the shipped binary.**
[ADR-0010](docs/architecture/adr/0010-duckdb-is-an-opt-in-build.md) measured what ADR-0004's
CGo clause actually costs — the binary goes 42.5 MB → 101.8 MB, stops being statically
linked, stops cross-compiling at all, and loses `windows/arm64` for want of published
bindings — and inverted the default. `make test-duckdb` and a CI job build and exercise the
tagged variant; asking a default build for a DuckDB connection explains that it was compiled
out and what to do instead.

**Carried in from Phase 0**, and owned by parts in this phase or named in them:
- **The container stack pins a superseded release.** `v0.0.2-alpha` was cut on
  2026-09-26 and is the first tag containing the setup wizard, but
  `deploy/docker-compose.yml` and `deploy/.env.example` still default to `0.0.1-alpha`.
  A one-line change in each, not owned by any part in this phase.
- The Keycloak round trip is opt-in and does not run in CI. It belongs in a scheduled job:
  what it protects against is Keycloak changing, not Pivot changing.
- `X-Forwarded-For` is not trusted, so rate limiting behind a proxy keys on the proxy.
  Phase 9 owns it; it matters more as soon as an instance is worth exposing.
- **MySQL error 1049 is mapped but never exercised.** It is the plain "no such database"
  that an account with wide grants gets; the test account gets 1044 instead, because MySQL
  will not tell an unprivileged user whether a database exists. Causing it needs a
  privileged connection the suite deliberately does not hold.
- **A file-backed connection can name any path the Pivot process can read.** Harmless from
  the CLI — whoever runs `pivot admin` can read the file anyway — and an escalation from a
  browser, where an organization administrator could point SQLite at Pivot's own store.
  Part 26 owns the allowlist and says so in its Done when. DuckDB's `read_parquet` and
  `read_csv_auto` widen the same hole, so the allowlist has to cover them too.
- **CI has a sixth job now** (`DuckDB build`). `main`'s ruleset requires five checks, so it
  needs adding there or it will not block a merge that breaks the tagged build.
- **Acceleration and federation are not in the default binary.** ADR-0010's cost: Phase 2's
  shared-subquery consolidation ([P2-DPF-003](docs/roadmap/phase-2-visualization-dashboards.md))
  needs a build most people will not have. Phase 2 decides whether that ships as a second
  artifact, with a real workload to measure instead of a prediction.

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
Phase 1  Connect & Query    [██████████████            ]  7/16
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

### - [x] Part 17 — The conformance suite ✅ 2026-09-27

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

### - [x] Part 18-a — MySQL, the second connector ✅ 2026-09-28

> **Split from Part 18** on 2026-09-28: three deliverables — two connectors and resource
> governance — is more than one session, and the rule is to split in place rather than
> silently half-finish. 18-a is the second connector; 18-b is the third plus governance.

**Deliverable:** MySQL through the same door as PostgreSQL, passing the conformance suite,
with every change the suite needed written down.

**Build:** The MySQL dialect in `internal/connectors`, its `conformance.Subject`, a dev
MySQL container and a CI service for it.

**Done when:**
- MySQL passes the conformance suite, and any change the suite needed is justified in
  writing rather than made to accommodate a connector
- The failure modes are classified from MySQL's own error numbers and caused for real,
  not mocked — a refused password, an unknown database, a missing table, bad syntax
- **Cancellation reaches the server**, proven from a *second* connection: the query is
  gone from `information_schema.processlist`, not merely abandoned by the client
- CI runs it against a real containerized MySQL, and a skipped MySQL suite fails the build
  the same way a skipped Postgres one does

**Notes:** This is where the Part 16 interface gets its first real test. MySQL differs from
PostgreSQL in every way the abstraction claims to cover: `?` rather than `$1`, backticks
rather than double quotes, its own error numbers, no `generate_series`, and a driver that
cancels by dropping the connection rather than by asking the server.

Expect to change the interface. That is what this part is for — but a change made to
accommodate a connector, rather than because the abstraction was wrong, is the failure mode
to watch for.

**Refs:** `P1-DB-002`, `P1-QE-004`

---

### - [x] Part 18-b — SQLite, the file-backed connector, and resource governance ✅ 2026-09-28

> **Split again** on 2026-09-28. DuckDB is not another connector, it is a build decision:
> CGo, a per-platform release matrix, and the first real use of the `nocgo` tag — which is
> named in `.golangci.yml` and implemented nowhere. Bundling that with SQLite would have
> meant doing neither properly, so it is Part 18-c.
>
> SQLite alone already satisfies "a connector that reads a file", which is what the
> original Done-when was reaching for.

**Deliverable:** a third connector, reading a file rather than a socket — and the
per-connection limits proven to hold on every connector rather than declared once.

**Build:** The SQLite dialect in `internal/connectors`, its `conformance.Subject`, and
whatever the interface turns out to be missing for a source with no host, no port and no
credentials.

**Done when:**
- SQLite passes the conformance suite, and any change the suite needed is written down
- A query that returns more rows than the limit is **truncated with a signal**, not
  silently cut — on all three connectors
- A query that runs longer than its timeout is stopped *at the source*, verified per
  connector rather than assumed from a context deadline
- **A SQLite connection is read-only.** A BI source is something Pivot reads; a connector
  that can write to the file it was pointed at is a bug waiting for a stray statement
- The pool limit holds under concurrency, on every connector, proven rather than declared

**Notes:** The interesting part is that `Config` was designed for a network source — host,
port, username, password, TLS. SQLite has none of them. Whatever that costs is the third
real test of the Part 16 interface, after MySQL's placeholders and its cancellation.

**Refs:** `P1-DB-003`, `P1-CONN-010`, `P1-QE-005`

---

### - [x] Part 18-c — DuckDB, and the CGo bill ✅ 2026-09-28

**Deliverable:** embedded analytical compute, and an honest answer to what it costs the
single-binary story.

**Build:** The DuckDB connector, the `nocgo` build tag that ADR-0004 promised, the release
matrix that CGo forces, and the per-query memory cap NFR §1.3 requires.

**Done when:**
- DuckDB passes the conformance suite, and reads a Parquet or CSV file directly — which is
  what makes the Part 27 sample dataset possible
- **The `nocgo` build produces a working binary** without DuckDB, and says so when somebody
  asks for a DuckDB connection rather than failing obscurely. The tag is named in
  `.golangci.yml` today and used by no file
- The release workflow builds every platform it claims to support, and the binary size
  change is recorded rather than discovered
- A query over its memory budget is killed rather than allowed to take the host down

**Notes:** ADR-0004 accepted CGo with its eyes open and listed the mitigations. This is the
part that finds out whether they were right, and its "Revisit if" clause — *CGo build
complexity outweighs the benefit* — is a live option rather than a formality.

**Refs:** `P1-DB-003`, ADR-0004

---

### - [x] Part 19-a — The canonical type system ✅ 2026-09-28

> **Split from Part 19** on 2026-09-28. Part 19 was a type system, a stored catalog, a sync
> that diffs, and streaming introspection. The notes on it called the type system "the
> load-bearing decision" — Phase 2 picks chart types from it, Phase 3 builds the semantic
> layer on it, Phase 7 grounds the AI in it, Part 24 formats from it. A decision with that
> many dependents gets a session, not a corner of one.

**Deliverable:** every column any connector reports carries a canonical type, and a type
nobody has mapped says so rather than guessing.

**Build:** `internal/datatype` — the canonical types and what they promise — plus a
normalization per dialect and a conformance property that checks it against four real
databases.

**Done when:**
- Every source type across all four connectors maps to a canonical type, and an unmapped
  one is an explicit *unknown* **carrying the source's own spelling**, so nothing is lost
  and the gap is findable
- **Exact and approximate numbers are different types.** `DECIMAL(10,2)` is money and
  `DOUBLE` is not, and a system that conflates them is how a total renders as
  `0.30000000000000004`
- **A zoned timestamp and a naive one are different types**, because the conformance suite
  already treats them as different properties and a type system that disagrees with it is
  wrong somewhere
- The canonical type is carried on query results *and* on introspection, and the two agree
  — they come from different vocabularies and nothing has checked they match
  - **Refined while building it.** MySQL cannot satisfy exact agreement and no code change
    would fix that: its catalog can report `tinyint(1)` for a boolean, and its driver
    reports plain `TINYINT` for the same column. The criterion is that the two agree
    wherever the driver can express the distinction, and that where it cannot, the
    difference is written down rather than discovered. `flag` is therefore not among the
    columns the conformance property asserts a type for

**Notes:** The trap is a type system that says "string" for everything, which makes chart
selection, join inference and AI grounding all worse at once. The other trap is inventing
distinctions nothing consumes; every type here should have a caller that would be wrong
without it.

**Refs:** `P1-CAT-003`

---

### - [x] Part 19-b — The stored catalog, and a sync that diffs ✅ 2026-09-28

> **Split again** on 2026-09-28, twice over.
>
> *Foreign keys* moved to 19-c: they need a query per dialect and a second pair of tables,
> and the part of this worth getting right is the diff.
>
> *Scheduling* moved with them, for a harder reason: **there is no job system.** ADR-0007
> chose River and nothing has needed it yet, so "a scheduled sync" means building a job
> runner first. A sync that runs when asked is the whole of the value here; running it on a
> timer is a scheduling problem, and pretending otherwise would have meant a `time.Ticker`
> in a package that has no business owning one.

**Deliverable:** Pivot remembers what is in the database it is connected to, and can say
exactly what changed since last time.

**Build:** The catalog tables, their repositories, and a sync that compares rather than
replaces.

**Done when:**
- A sync detects added, removed and changed columns **rather than replacing the catalog**,
  and reports what it found
- A table or column that disappears is **marked gone, not deleted** — a permissions blip or
  a migration caught mid-flight should not destroy history that Phase 3's models point at
- Introspecting a large schema does not hold the source connection for the whole of it —
  the write to Pivot's own store happens after the source has been released
- The catalog is tenant-scoped like everything else, and the isolation harness proves it

**Refs:** `P1-CAT-001`, `P1-CAT-002`

---

### - [ ] Part 19-c — Foreign keys, and a sync that runs itself

**Deliverable:** the catalog knows how tables relate, and keeps itself current without
somebody typing a command.

**Build:** Foreign key and constraint discovery per dialect, plus the job runner ADR-0007
called for and the scheduled sync on top of it.

**Done when:**
- Foreign keys are discovered on every connector that has them, which is what Phase 3's
  join inference reads
- A sync runs on a schedule without a person, and a failed one is visible rather than silent
- Two Pivots against one database do not both sync the same connection at the same time

**Notes:** The job system is the larger half. ADR-0007 chose River, which needs PostgreSQL —
and ADR-0003 supports SQLite too, so this part decides what a SQLite instance gets instead.
That is a real decision, not a detail.

**Refs:** `P1-CAT-002`, `P1-CAT-004`, ADR-0007

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
- **A file-backed connection cannot name an arbitrary path.** From the CLI this is not an
  escalation — anyone who can run `pivot admin` can already read the file. From a browser
  it is: an organization administrator could point a SQLite connection at Pivot's own store
  and read every other tenant's rows. The form needs an allowlist, and the connector needs
  to be told about it
- Users and groups can be managed without the CLI — the CLI stays for the cases where
  nobody can log in
- The SMTP test send actually sends, and its failure says which part failed

**Refs:** `P1-ADM-001`, `P1-ADM-002`, `P1-ADM-004`, `P1-ADM-005`

---

### - [ ] Part 27 — The sample dataset, and Phase 1 close-out

**Deliverable:** a new Pivot has something to show before anybody finds their warehouse
credentials — and the phase is reviewed honestly.

**Build:** An embedded sample dataset (**SQLite**, a realistic e-commerce schema), a guided
first query, and the Phase 1 exit review.

> **Changed by [ADR-0010](docs/architecture/adr/0010-duckdb-is-an-opt-in-build.md)** on
> 2026-09-28. This said DuckDB, and a default binary no longer has one. SQLite reads a file,
> is always present, and is entirely adequate for browsing a schema and running a query —
> which is all a first run needs. The analytical reasons to want DuckDB here do not apply to
> a dataset small enough to ship.

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
| 2026-09-28 | 19-b | Migration 00008 and the catalog tables on both engines, `CatalogRepo`, `internal/catalog` — the sync that reconciles rather than replaces — and `pivot admin sync-catalog` | **Split twice more.** Foreign keys went to 19-c because they need a query per dialect and a second pair of tables, and the part of this worth getting right is the diff. **Scheduling went with them for a harder reason: there is no job system.** ADR-0007 chose River and nothing has needed it yet, so \"a scheduled sync\" means building a job runner first — and 19-c has to decide what a SQLite instance gets, since River needs PostgreSQL and ADR-0003 supports both. Putting a `time.Ticker` in this package to tick the box would have been the wrong answer twice. **The design turns on one word: reconcile.** The obvious implementation deletes everything for a connection and inserts what it just read, and that is wrong three ways — it destroys `first_seen_at`, the descriptions and the identity every Phase 3 model will point at; it cannot answer \"what changed since yesterday\", which is the only reason to sync on a timer; and a source that answers with half its tables because a permission was revoked takes the other half of the catalog with it. So a sync **upserts what it sees, sweeps what it did not, and reports the difference**. Two statements per object and one at the end, no temporary table, no transaction held across a slow source. **Nothing is deleted.** A table that vanishes gets `removed_at` and keeps everything, and comes back unmarked if the source reports it again — reported as an addition, because that is what it is downstream, while the row keeps its original `first_seen_at`. A removal is reported **once**: re-reporting every long-dead table forever is how change detection becomes something people filter out of their alerts. **The report names things rather than counting them.** The sweep returns row counts; the snapshot turns them back into names, and the two are cross-checked — a disagreement is the signature of a concurrent sync on one connection, which is worth saying rather than hiding. **Only type and nullability count as a change.** A comment or a position moving is nothing any consumer can be wrong about, and reporting it would bury the two that are: a column whose type changed is a chart about to render nonsense, and one that became nullable is an aggregate about to skip rows. Both spellings are compared, because either can move alone — `varchar(50)` to `varchar(100)` changes the source and not the kind. **The source is released before Pivot writes**, proven by looking at the pool from *inside* the write: a fake store asserts on every record that the connector has nothing checked out — 8 writes, 0 held. Interleaving would hold a pooled connection against somebody's warehouse for as long as Pivot's own store takes. **The diff is tested against a fake store and the storage against real SQL**, deliberately: the diffing deserves exhaustive cases and a database per case would buy no confidence, while \"the upsert keeps the row's id and its `first_seen_at`\" is a claim only real SQL can settle — and it is checked on both engines. **The portability harness caught the new tables** before I declared them, for the second time in this phase, and `sqlc.yaml` needed 17 per-column SQLite overrides or the two models diverged on every timestamp. Two query shapes had to change for portability: PostgreSQL parameter *reuse* (`$8, $8`) becomes two separate SQLite parameters, and a redundant `org_id` in a subquery made sqlc emit `OrgID_2` — both removed rather than worked around. |
| 2026-09-28 | 19-a | `internal/datatype` — the canonical type system — `NormalizeType` on the connector interface and all four dialects, and two conformance properties checking it against four real databases | **Split from Part 19**: a type system, a stored catalog, a sync that diffs and streaming introspection is four things, and the notes called the type system \"the load-bearing decision\" — Phase 2 picks charts from it, Phase 3 builds the semantic layer on it, Phase 7 grounds the AI in it, Part 24 formats from it. **The probe came before the design.** Creating a wide table in each source and reading its types back both ways showed what a normalizer is actually up against: **PostgreSQL's two vocabularies disagree on 13 of 23 columns** — `integer`/`INT4`, `timestamp with time zone`/`TIMESTAMPTZ`, `character varying`/`VARCHAR` — and for `timetz` and `money` pgx has no name at all, reporting the raw OIDs **\"1266\" and \"790\"**. Both are mapped, because a column whose canonical type depends on which code path asked is worse than one nobody has mapped: the disagreement is invisible. **Two distinctions justify the package.** Exact against approximate — `DECIMAL(10,2)` is money and `DOUBLE` is not, and conflating them is how a total renders as `0.30000000000000004`. And zoned against naive, which the conformance suite already treats as two separate properties, so a type system that merged them would have disagreed with checks two files away. **Unknown is a real answer**, carrying the source's spelling — a fallback to String is indistinguishable from knowledge at exactly the point where somebody charts the column, and the spelling is the search term for whoever adds the mapping. **The suite caught the biggest error immediately.** MySQL's naming is **inverted**: its `TIMESTAMP` is the *instant* (stored UTC, converted on read) and `DATETIME` is the wall-clock reading — the opposite of the standard and of PostgreSQL. The shared table was therefore exactly wrong for MySQL in the most damaging direction, on every row of every MySQL source, and the new property failed on the first run. **MySQL also has no boolean.** `BOOLEAN` is `TINYINT(1)`, and `data_type` flattens it to `tinyint`; the introspect query now selects `column_type`, which keeps the width and the `unsigned` it was also dropping. Treating `tinyint(1)` as boolean is a heuristic — the one JDBC makes as `tinyInt1isBit`, and the alternative is every MySQL boolean rendering as 0 and 1 forever. The driver cannot make the distinction at all, so **the Done-when I wrote this morning demanding the two paths agree exactly was too strong**: it now says they agree wherever the driver can express the distinction, and `flag` is not among the columns asserted. **SQLite has no types, only declarations** — so the fixture declares `TIMESTAMPTZ`, which SQLite accepts and hands back unchanged. Nothing about the storage distinguishes an instant from a clock reading there, which makes the author's declared name the best information anyone will ever have. `NormalizeType` went on `Connector` rather than only `Dialect` for a real future caller: when Pivot learns a mapping it lacked, the stored catalog can be re-normalized from the spellings it kept without going back to somebody's warehouse. A nineteenth defect in `broken_test.go` — a connector that guesses every type as text — proves both new properties can fail. Coverage 97.9% on the new package. |
| 2026-09-28 | 18-c | The DuckDB connector behind a `duckdb` build tag, `RegisterAbsent` for connectors compiled out, [ADR-0010](docs/architecture/adr/0010-duckdb-is-an-opt-in-build.md), `make test-duckdb` and a CI job for it | **This part was a measurement, and the measurement decided it.** ADR-0004 accepted CGo and named the escape: *revisit if CGo build complexity outweighs the benefit*. It did. Built the same tree both ways: the binary goes **42.5 MB → 101.8 MB** (ADR-0004 predicted \"roughly 30MB\"), stops being **statically linked** — it pulls `libstdc++`, `libgcc_s`, `libm` and `libc`, and the container base is `distroless/static`, which has none of them — and **stops cross-compiling at all**: `darwin/arm64` and `linux/arm64` both die with `undefined: bindings.Type`, where the default build makes all six targets from one runner. **`windows/arm64` has no published bindings**, so a mandatory DuckDB drops the release from six platforms to five. Two more signals: the module is **deprecated** in favour of `duckdb/duckdb-go`, which **cannot be required under that path** because v1.8.5 still declares itself as `marcboeker/go-duckdb`; and fetching **327 MB** of prebuilt libraries failed once with a connection reset before succeeding on retry. So ADR-0010 **inverts ADR-0004's default**: the shipped binary is pure Go and DuckDB is opt-in. The release matrix is unchanged, which is the point. **DuckDB still works and is still held to the bar** — it passes all seventeen conformance properties, reads Parquet and CSV directly (the reason it is worth having), opens a file read-only like SQLite, and enforces the NFR 1.3 memory cap rather than suggesting it: a sort far over a 128MB budget is refused, and a connection that says nothing gets 1GB rather than DuckDB's own default of most of the host. **The fourth connector found a latent bug in the other three.** DuckDB reports the *same interrupt* for a cancellation and a timeout, so a query killed by its own deadline came back classified \"canceled\". Only the context knows which it was — and PostgreSQL's 57014 and MySQL's 1317 have exactly the same ambiguity, passing until now only because their drivers happened to surface the context error instead. `classify` now upgrades a dialect's \"canceled\" to \"timeout\" when the deadline expired, for every connector. The distinction is the operator's: a timeout means raise the limit, a cancellation means somebody walked away. **A build tag nothing compiles has already broken**, so `make test-duckdb` builds, **lints** and tests the tagged half — lints because `.golangci.yml` pins its own build tags and a single run never sees both sides of a tagged pair, which would have left `duckdb.go` the one file in the repository nothing checked. A CI job runs it and records the size table in the run summary. **Asking a default build for DuckDB explains itself**: `RegisterAbsent` distinguishes *compiled out* from *does not exist*, and the message names ADR-0010, `make build-duckdb`, and sqlite as the always-present alternative. It is not listed in `--kind`, because offering a connector that cannot be opened turns one clear failure into a confusing one later. **Knock-on:** Part 27's sample dataset was specified as DuckDB and a default binary has none, so it is SQLite now — which reads a file, is always present, and is entirely adequate for a first run. And ADR-0004 gained an `Amended by:` line: the ADR conventions had only *supersede*, which is for a decision reversed outright, so amending is now written down as its own thing — otherwise a reader arriving at 0004 follows advice the project no longer takes. |
| 2026-09-28 | 18-b | The SQLite connector (read-only), migration 00007 dropping the connector-kind enumeration, a `concurrent_queries_all_succeed` conformance property, and per-connector governance tests | **Split again**: DuckDB is not a fourth connector, it is a build decision — CGo, a per-platform release matrix, and the first real use of the `nocgo` tag, which is named in `.golangci.yml` and used by no file. It is Part 18-c. SQLite alone already satisfies \"a connector that reads a file\". **SQLite passes all seventeen properties.** It is the first source with no host, no port and no credentials, and `Config` was built around all five — which cost less than expected, because `Validate` was always the dialect's job. **Two deliberate suite changes.** A subject may now supply no DDL: a connector opened read-only cannot build its own fixture, and that is not an edge case — read-only is the *correct* way to open a BI source, and an account granted SELECT and nothing else is how a careful warehouse administrator hands out access. And a seventeenth property, `concurrent_queries_all_succeed`, because 18-a gave every query its own connection and a watcher goroutine, and a pool that hands one connection to two queries shows up nowhere else in a suite that runs one query at a time. **SQLite is opened read-only, always.** `mode=ro` is set *after* anything `Options` supplied, so a configuration cannot turn it off — tested by trying. Proven by causing INSERT, UPDATE, DELETE, DROP and CREATE to fail *and* by reading the file back through a separate handle, because an error that arrived after the write would satisfy the first half and none of the intent. The fields SQLite cannot use are **refused rather than ignored**: a connection carrying a username and password looks authenticated in every listing, and a SQLite file is protected by its filesystem permissions and nothing else. **The CLI was overreaching.** It required `--db-host` and `--username` of its own accord, which made a file-backed connector impossible to configure; what a connection needs is the dialect's business now. And `--no-test` used to skip validation entirely, so it would store a configuration the connector would refuse — it now skips *dialing*, not checking. **The real find was a bug shipped in 18-a.** The connections table carried `CHECK (kind IN ('postgres'))`, and its own comment called the resulting migration-per-connector deliberate. The very next connector was added without one, so **a MySQL connection could be configured, tested, and then refused by the database on the way in** — the connector tests never reached storage and the storage tests only ever named \"postgres\", so nothing looked. 00007 drops the enumeration on both engines (SQLite needs a full table rebuild; it cannot alter a CHECK). The deeper reason to drop rather than widen it: which connectors exist is a property of the **binary**, not the data — 18-c puts DuckDB behind a build tag, so two Pivots from one commit will disagree about which kinds are valid and no schema can be right for both. **The guard that would have caught it** is `TestEveryRegisteredConnectorCanBeStored`, driven off the registry so a fourth connector is covered by existing. Verified in both directions: with 00007 removed it fails on both engines and names the kind. **Governance is proven rather than declared.** The pool test from Part 16 asserted `MaxOpenConnections` — the setting, which says only that it was applied. It now asserts `WaitCount > 0`, which is the number of times a goroutine actually queued for a connection: 14 waits out of 16 queries, on all three connectors. Without that a pool test passes on a fast machine where nothing ever overlapped. **SQLite cancellation is proven from the pool**, not from a second connection — an embedded database has no second place to look, so the check is that a query issued immediately afterwards on a one-connection pool returns in milliseconds rather than queueing behind a recursive CTE counting to six hundred million. |
| 2026-09-28 | 18-a | The MySQL connector, `Canceler` in the connector interface, unsigned integers in the conformance readers, a dev MySQL container and a CI service for it | **Part 18 was split into 18-a and 18-b**: two connectors plus resource governance is more than one session. **MySQL passes all sixteen conformance properties**, and the \"adding a connector means writing one file\" claim held — `mysql_conformance_test.go` is the whole integration, written before anything in the suite was touched. **The suite needed exactly one change**, and the connector caught it rather than the other way round: MySQL's `ROW_NUMBER()` returns `uint64`, which the readers had never seen because PostgreSQL has no unsigned integers. Added with a ceiling check — a `uint64` above `MaxInt64` is refused rather than wrapped, because a row count that reads `-9223372036854775808` looks like data rather than like a bug. **The interface needed exactly one change, and it was the interesting one.** Measured first: `go-sql-driver` cancels by hanging up, so a canceled `SELECT SLEEP(20)` returned to the client in 301ms and was *still running on the server two seconds later* — it would have held a thread for the full twenty. So `Canceler` is now an optional interface a dialect implements, and MySQL's sends `KILL QUERY`. Three details that matter: it is **KILL QUERY, not KILL CONNECTION**, so the pooled connection survives instead of being thrown away on every cancel; the kill goes over a **separate one-connection pool**, because a kill that queues behind the queries it is trying to kill is a deadlock and the moment it matters most is exactly when the query pool is empty; and the watcher's teardown **waits for the goroutine**, because a query finishing at the same moment its context ends would otherwise race its own `KILL` onto whatever the pool hands out next. Proven from a *second connection* watching `information_schema.processlist`, not from the client returning promptly — the client returned promptly before any of this existed. **Timestamps are pinned on both halves at once.** The driver parses what the server sends using `Loc`, and the server converts `TIMESTAMP` into the session's `time_zone`; setting one without the other shifts every zoned value silently. So the connector pins both to UTC and **refuses** an option that would move one of them. The dev and CI MySQL both run on **Asia/Kathmandu (+05:45)** on purpose — not UTC, not a whole hour — so a connector that inherited the server's zone would be wrong on every row and the test that proves the pinning could actually fail. **MySQL will not say whether a database exists**: an unprivileged account gets 1044 access-denied rather than 1049, because answering would be an information leak. The message carries both possibilities instead of picking one and sending half the people who hit it in the wrong direction. **The biggest find was in CI, not in MySQL.** The step named \"The Postgres half actually ran\" grepped `test.log` for `SKIP.*PIVOT_TEST_POSTGRES_URL` — and `go test` without `-v` prints nothing at all for a skipped test, so it was searching a log containing neither word. **It had never been able to fail, and had been green since Phase 0**, guarding seven packages that opt in on that variable. Fixed with `-v` plus a grep for the variable names, and verified in both directions before being trusted: 15 hits with the variables unset, 0 with them set. The dev compose passes **no command line** to MySQL, because a GitHub Actions service container cannot be given one and a dev database configured differently from CI's produces failures that only reproduce where you cannot debug them — so `cte_max_recursion_depth` (MySQL has no `generate_series`; the suite's rows come from a recursive CTE, and 1000 is the default ceiling) is set per-session through `Options`. |
| 2026-09-27 | 17 | `internal/connectors/conformance` — sixteen named properties, a breakable reference connector that proves each one can fail, and `postgres_conformance_test.go` as the worked example | **The suite is a library, not a runner.** A connector supplies a `Subject` — its fixture DDL, a sleep and a series expression, two statements it rejects, an identifier that needs quoting — and calls `conformance.Run`. Nothing in the package names a connector, so adding one is writing one file. **PostgreSQL passes all sixteen against the containerized database, in CI** (`ci.yml` sets `PIVOT_TEST_POSTGRES_URL` and fails the build if a Postgres test skips, so this cannot quietly stop running). **The part that makes the rest worth anything is `broken_test.go`**: seventeen deliberate defects — a NULL arriving as `\"\"`, unicode normalized on the way out, a zone applied to a naive timestamp, a result cut at the cap without the flag, a row repeated mid-stream so the count still comes out right, errors returned unclassified, a capability declared and not delivered — each wired into a working connector one at a time, each asserted to fail *its own named property*. A suite that passes is worth exactly the confidence that it would have failed, and that cannot come from reading it. **The fake's dialect is deliberately nothing like PostgreSQL** (`series 40`, `sleep 3`): if the suite only passed against something Postgres-shaped it would be a regression test in a conformance suite's clothes, and this is how that gets caught. **Capabilities are demonstrated, not believed** — declaring `CTEs` means a CTE runs, and declaring `LateralJoins` without supplying a query to prove it is a *failure*, because the compiler reading that field in Phase 3 will emit SQL the source rejects in front of whoever built the dashboard. **The fixture runs on Asia/Tehran**, +03:30: row 1 is stored at 23:30Z and comes back as March **16** at 03:00 local — verified by hand — so a connector confusing zoned for naive lands on the wrong *day*, and the half-hour offset also catches anything assuming whole hours. **Quoting is checked through a real round trip**, aliasing a column to `a \"quoted\" name` with the dialect's own `QuoteIdentifier`: a rule that fails to escape the inner quote is a syntax error, which is the injection this catches. **`Check.Failure` was extracted so the promise — a failure opens with the property, not the assertion — is one tested function rather than a convention.** The readers are permissive about the Go type a driver returns and strict about the value; Part 19 is where normalization becomes a contract. Coverage 88.9% here and 91.3% on `internal/connectors`. **The repo's own gate turned out to be lying locally**: `make coverage-gate` built its profile without `PIVOT_TEST_POSTGRES_URL`, so it reported `FAIL 54.0%` for a package CI measures at 91.3% — every opt-in Postgres test skipped. A local guard that disagrees with CI in either direction is one people learn to ignore, so the target now depends on `dev-db` and sets the URL, exactly as `test-all` and CI do. **Lint caught three things I would not have**: `catalogued` (misspell wants US spelling), `text, _ := asString(v)` in four places (errcheck's check-blank), and two `%v`s that should have been `%w`. |
| 2026-09-27 | 16 | `internal/connectors` (interface, registry, `SQLConnector`, PostgreSQL), migration 00006 and the `connections` table, `ConnectionRepo`, and three `pivot admin` commands | **Pivot connects to a database somebody else owns.** Created, tested and stored against a real PostgreSQL, with the password landing as `pivot.v1.4a44cfc0...` rather than a string — checked with SQL against the file on both engines, because asking the repository whether it encrypted something is asking the guard whether the door is locked. **All four failure modes were caused for real rather than mocked**: a refused password, a host that does not resolve, a closed port, a missing database. Each says what to do and none contains the password. **The interface is the decision this part exists for.** `Dialect` is not `Connector`: pooling, scanning, truncation and timeouts are identical for every database/sql source, so they live in `SQLConnector` once and a driver supplies the DSN, the capabilities, the catalog query and the error classification — with BigQuery in mind, which is not database/sql-shaped, so this is a helper implementing the interface rather than the interface itself. **A truncated result carries a flag**, because a silently cut result is a wrong answer presented as a right one and the chart Phase 2 draws from it is wrong in a way nobody can see. **The DSN is built with `net/url`**: a generated password contains a colon, an at sign or a slash about a third of the time, and concatenation turns that into a DSN naming a different host. **`Query` still takes a string** — the package comment promised compiled query objects and Phase 3 owns the compiler that makes them; inventing the type now would be designing against an imaginary caller, so Part 20 replaces it. **Three things bit me.** `--host` and `--port` collided with the root command's persistent flags, so `--port 5433` set the *server* port to zero and the command failed before it ran; they are `--db-host` and `--db-port` now. The pgx driver was not registered in this package and had been reaching it by luck of import order. And my own error message hid its cause — `sql.Open` failing said "could not prepare a postgres connection" and nothing else, which sent me looking in the wrong place for ten minutes; at that point nothing secret can be in the error, so it carries the driver's text. **The portability harness caught the new table** before I remembered to declare it, and `sqlc.yaml`'s own warning caught the SQLite type overrides I had not added — the two models had silently diverged on eleven columns. |
