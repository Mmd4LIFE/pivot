# Phases 0–2, step by step

One table per phase: every step, its sub-steps, what each one is, and what counts as
done. Phase 0 is the record of what was built; Phases 1 and 2 are the plan.

**A note on the numbering.** Phases run from 0, so **Phase 0 is the first phase** — the
one that is complete. Phase 1 is the second and is in progress. Phase 2 is the third.

| | Phase | Status | Parts | Release |
|---|---|---|---|---|
| **0** | Foundations | ✅ complete 2026-09-26 | 31 | none (internal) |
| **1** | Connect & Query | in progress | 12 planned | v0.1 |
| **2** | Visualization & Dashboards | planned | to be expanded | v0.2 |

**Definition of done** for every row, from
[00-principles.md](00-principles.md): the code is written, the tests it needs exist and
pass, it is documented where somebody would look for it, it runs in CI, and its `Done
when` was *executed* rather than reasoned about. A criterion nobody ran is not met.

---

## Phase 0 — Foundations · complete

31 parts. Full record: [../checklists/phase-0-checklist.md](../checklists/phase-0-checklist.md).
Exit review: [phase-0-exit-review.md](phase-0-exit-review.md).

| Phase | Step | Sub-step | Description | Done when |
|---|---|---|---|---|
| 0 | 1 | — | Repository, Makefile, lint, module layout | `make` targets exist for every routine task and the tree matches the documented layout |
| 0 | 2 | — | Configuration: files, environment, flags, precedence | Precedence is defaults → file → env → flags, and `pivot config show` says where each value came from |
| 0 | 3 | a | Structured logging and the HTTP server | JSON logs, `/healthz`, graceful drain on SIGTERM |
| 0 | 3 | b | Error contract and middleware chain | Every error is a coded envelope; the middleware order is a security property, written down |
| 0 | 4 | a | Metadata schema and migrations | Schema v1 applies on **both** SQLite and PostgreSQL from one migration set |
| 0 | 4 | b | Typed queries via sqlc, dual-engine | Generated code compiles for both engines; drift fails `make gen-check` |
| 0 | 5 | — | Repository layer with tenant scoping | Scope is enforced in the data layer, not remembered by handlers; an unscoped query cannot compile |
| 0 | 6 | a | Password authentication | Argon2id, per-account lockout with escalating windows, timing-equalized failures |
| 0 | 6 | b | Sessions | Server-side, revocable immediately, idle and absolute expiry |
| 0 | 7 | a | OIDC single sign-on | Discovery, PKCE, state and nonce in an `HttpOnly` cookie; auto-provisioning |
| 0 | 7 | b | Identity provider administration | CRUD gated on `manage_organization`; the client secret never leaves the server |
| 0 | 8 | a | Authorization model | Permissions resolve through direct grants and group inheritance; answers stated as data |
| 0 | 8 | b | Enforcement middleware | Fail-closed: no checker means no permission; 503 is distinguishable from 403 |
| 0 | 9 | — | Frontend scaffold and design tokens | Vite + React + TS + Tailwind; tokens are runtime CSS custom properties, which Phase 8 needs |
| 0 | 10 | a | Design system components | 24 components with stories, light and dark |
| 0 | 10 | b | Accessibility regime | Four automated layers: Go contrast, jsdom axe, jsdom keyboard, browser axe |
| 0 | 11 | a | i18n, login page, session boundary | Catalog-as-type; direction from locale; open-redirect refused on both ends |
| 0 | 11 | b | Application shell | Four labeled landmarks, skip link, command palette; Playwright against the real binary |
| 0 | 12 | a | CI pipeline | Five parallel jobs; no third-party actions; pinned tools with verified checksums |
| 0 | 12 | b | Branch protection, proven | A deliberately broken PR was refused by each job in turn |
| 0 | 13 | a | Container image and healthcheck | Distroless, multi-arch, cross-compiled; the binary probes itself |
| 0 | 13 | b | Signed releases | Six platforms, keyless Sigstore, verified from outside the build |
| 0 | 14 | a | API test coverage | The administrative endpoints driven for the first time |
| 0 | 14 | b | Tracing and log correlation | HTTP → authz → database in one trace, asserted rather than demonstrated |
| 0 | 14 | c | Metrics and a dashboard | Three instruments, errors as a label; the dashboard has a test |
| 0 | 14 | d | Frontend error reporting | A browser error reaches the log with the trace of the call that failed; 0.6 KB |
| 0 | 15 | a | First run | Download, run, browser, administrator — no CLI. Setup closes permanently |
| 0 | 15 | b | Your own account | Change your password: proves the current one, ends every *other* session |
| 0 | 15 | c | Doctor and backup | Diagnoses broken installs; `VACUUM INTO` backup proven consistent under writes |
| 0 | 15 | d | Secrets at rest | Envelope encryption, rotation walked, plaintext migration path |
| 0 | 15 | e | Containers and close-out | `docker compose up` walked; seven exit criteria walked one at a time |

---

## Phase 1 — Connect & Query · in progress

12 parts. Working checklist: [../../CHECKLIST.md](../../CHECKLIST.md).
Spec: [phase-1-connect-and-query.md](phase-1-connect-and-query.md).

**Goal:** make Pivot useful for exactly one person — an analyst who can write SQL.

| Phase | Step | Sub-step | Description | Done when |
|---|---|---|---|---|
| 1 | 16 | — | Connector interface, pooling, credentials, PostgreSQL | An external PostgreSQL connects; its password is an envelope; errors are actionable; capabilities are declared |
| 1 | 17 | — | The conformance suite | One suite every connector must pass: types, NULLs, timezones, streaming, cancellation, unicode. A broken connector fails it by name |
| 1 | 18 | — | MySQL, SQLite/DuckDB, resource governance | Both pass the suite unmodified; row limits truncate *with a signal*; timeouts cancel **at the source** |
| 1 | 19 | — | Schema catalog | Every source type maps to a canonical type; sync detects change rather than replacing; unknown is explicit |
| 1 | 20 | — | Execution pipeline | Large results stream; cancellation reaches the source; authorization happens in the pipeline, before execution |
| 1 | 21 | — | Result cache | **The key includes user identity wherever RLS could apply**; two users with different policies provably cannot share |
| 1 | 22 | — | Governance and query monitor | One user cannot exhaust a connection; an administrator can kill a running query and the kill lands |
| 1 | 23 | — | SQL editor | Schema-aware autocomplete; errors at the line; tabs survive a reload; **the bundle budget passes or is renegotiated in writing** |
| 1 | 24 | — | Results grid and export | A million rows stay usable; **a 10M-row CSV export stays inside the memory budget, measured** |
| 1 | 25 | — | Saved questions | Sharing shares the permission to run, not new access to the source |
| 1 | 26 | — | Administration | Connections, users and groups manageable from the browser; credentials never come back out |
| 1 | 27 | — | Sample dataset and close-out | A first run has data to explore; exit criteria walked; v0.1 cut and the stack pointed at it |

### Phase 1 exit criteria

| # | Criterion |
|---|---|
| 1 | 8 connectors pass the conformance suite against real containerized instances |
| 2 | A 10M-row result streams to CSV without the server exceeding its memory budget |
| 3 | Query cancellation propagates to the source database, verified **per connector** |
| 4 | p95 for a cached query is under 200 ms |
| 5 | 5 external testers connect their own database and save a question unassisted |
| 6 | A malicious query cannot escape its connection's credentials or resource limits |

**Criterion 1 says eight and the v0.1 connector set is ten**, of which four are P0. The
parts above build four; the remainder are a repetition of Part 18 once the suite exists,
which is the entire argument for building the suite first.

---

## Phase 2 — Visualization & Dashboards · planned

Not yet expanded into parts; that happens at Part 27, on the same terms. Spec:
[phase-2-visualization-dashboards.md](phase-2-visualization-dashboards.md).

**Goal:** turn a result set into something somebody can look at, and a collection of those
into something a team looks at every morning.

| Phase | Step | Sub-step | Description | Done when |
|---|---|---|---|---|
| 2 | — | — | Chart types: line, bar, area, scatter, pie, table, number, map | Each renders from the canonical types Phase 1 established, with no per-chart type guessing |
| 2 | — | — | Automatic chart selection | A result set gets a sensible default from its column types and cardinality — the reason Phase 1 profiles columns |
| 2 | — | — | Visualization settings: axes, series, colors, formatting | Settings are data, so a chart can be recreated exactly from what was saved |
| 2 | — | — | Dashboard canvas with layout | Grid layout, resize, reorder; a dashboard survives a reload exactly as left |
| 2 | — | — | Dashboard filters and parameters | One filter drives many cards; parameters flow into the underlying queries |
| 2 | — | — | Cross-filtering and drill-through | Clicking a bar filters the rest; drill-through reaches the rows behind a number |
| 2 | — | — | Scheduled refresh and caching | A dashboard is fast because its cards are cached, and the cache respects identity |
| 2 | — | — | Export and share a dashboard | PDF and image export; a shared link respects the same authorization as the app |

**The dependency worth naming:** almost everything here reads from decisions made in
Phase 1. Canonical types drive chart selection, column profiling drives the defaults, and
the identity-aware cache key is what lets a dashboard be fast without being a data breach.
Phase 2 is cheap if Phase 1 was done properly and expensive if it was not.
