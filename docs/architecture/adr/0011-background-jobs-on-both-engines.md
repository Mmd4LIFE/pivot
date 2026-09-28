# ADR-0011: River runs on SQLite too, so background work is not Postgres-only

**Status:** Accepted
**Date:** 2026-09-29
**Amends:** [ADR-0007](0007-job-orchestration.md)

## Context

[ADR-0007](0007-job-orchestration.md) chose River for background jobs, and its reasoning
holds. It rejected a homegrown queue with a sentence worth repeating:

> This is a solved problem, and the failure modes (visibility timeouts, poison messages,
> leader election) are exactly the ones that are subtle and expensive to get wrong.

What it never confronted is [ADR-0003](0003-metadata-database.md). ADR-0007 justified River
on the grounds that *"it uses the Postgres we already require"* — and ADR-0003 says Postgres
is **not** required:

> PostgreSQL 16+ as the production metadata database. SQLite 3.45+ as the zero-config
> default for new installs and development. Every schema change and query must work on
> both, verified in CI.

Two accepted ADRs, one of which assumes something the other denies. Taken at face value, a
SQLite instance gets no catalog syncs, no alert evaluation ([Phase 8](../../roadmap/)), and
no flows ([Phase 6](../../roadmap/)) — which is not a degraded install, it is a different
product sharing a name. Part 19-d is where that had to be answered.

## Measurements

Taken on 2026-09-29 against `github.com/riverqueue/river@v0.47.0`.

**River publishes a SQLite driver.** `riverdriver/riversqlite` exists, and — the part that
matters — it takes a plain `*sql.DB` rather than naming a SQLite package. That is what
`modernc.org/sqlite` already provides, so it needs no CGo and no new driver.

**It works.** A spike ran an inserted job *and* a periodic one to completion on both
engines before any of this was written:

```
sqlite:   2 jobs ran (1 inserted + periodic)
postgres: 2 jobs ran (1 inserted + periodic)
```

**It costs almost nothing.** Six River modules plus `tidwall/gjson` and `sjson`, all pure
Go:

| Build | Size |
|---|---|
| without River | 42.76 MB |
| with River and both drivers | 42.94 MB |

**+0.18 MB.** For contrast, [ADR-0010](0010-duckdb-is-an-opt-in-build.md) measured DuckDB at
+59 MB and a loss of static linking.

**The same five tables on both engines** — `river_job`, `river_leader`, `river_migration`,
`river_notification`, `river_queue` — applied by River's own migrator.

## Decision

**Background jobs run on both engines.** River is the job system on PostgreSQL *and* on
SQLite, via `riverdatabasesql` and `riversqlite` respectively. ADR-0007's choice stands
unchanged; what this adds is the answer it was missing.

Three consequences of the measurements shape the implementation:

- **The runner opens its own pool** ([`store.DB.OpenSibling`](../../../internal/store/db.go)).
  On SQLite the store's pool is one connection by design, and a job runner polling on it
  would sit between every request and the database. The `riversqlite` documentation
  independently asks for `SetMaxOpenConns(1)`, so the runner's pool is one connection there
  and four on PostgreSQL.
- **River's migrations are run by River**, from `pivot migrate up`, not copied into
  `internal/store/migrations`. They are not Pivot's schema: River versions them itself and
  changes them on its own cadence, and hand-porting somebody else's schema to two dialects
  on every upgrade is a standing cost for no benefit.
- **The portability harness needs no exclusion**, which was worth checking rather than
  assuming. It asserts the exact set of tables *Pivot's* migrations produce, and River's are
  applied separately — so the `river_*` tables never appear in the databases the harness
  builds. A real deployment has them; the schema contract does not, which is correct: they
  are not Pivot's schema to keep portable.

## Consequences

**Positive**

- The quickstart path is not a lesser product. A SQLite install gets scheduled catalog
  syncs, and will get alerts and flows on the same runner
- Leader election comes free and is what keeps two Pivots from both syncing one connection
  — proven on both engines by running two
- One job system rather than two behind an interface: no second code path to keep honest

**Negative**

- **`riversqlite` is young.** Its own package documentation says it "is currently in early
  testing… has minimal real world use as of yet". The mitigation is that Pivot's own tests
  run the whole job suite against both engines, so a regression is ours to find rather than
  a customer's
- **It is slower on SQLite**, doing completion and `InsertMany` a row at a time because of
  sqlc limitations in that driver. Irrelevant at the volumes a SQLite instance implies, and
  a reason not to run a large deployment on one — which ADR-0003 already says
- A second connection pool per process, which on SQLite means a second connection to the
  same file. Safe because the store already opens SQLite with WAL and a five-second busy
  timeout, and stated here because it would not be safe without them
- Two migration systems in one database. `pivot migrate up` runs both, and a reader of
  `internal/store/migrations` will not find the `river_*` tables there

**Neutral**

- Transactional enqueueing — which ADR-0007 called the decisive feature — is available
  through River's `InsertTx` and is not yet used. Nothing so far has a transaction to join:
  the catalog sync is scheduled rather than triggered by a data change

## Revisit if

- `riversqlite` proves unreliable under real use, in which case the honest answer is that
  SQLite instances lose background work and the documentation says so plainly
- Job volume on SQLite becomes a measured problem, which is the signal to move that
  deployment to PostgreSQL rather than to change the job system
