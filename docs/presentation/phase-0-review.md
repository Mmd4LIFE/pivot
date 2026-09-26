# Pivot — end of the first phase

**A review of Phase 0 (Foundations), complete 2026-09-26.**

Twenty minutes: five to say what we set out to do, ten to show it running, five for
what is missing and what happens next.

---

## 1. What this phase was for

Pivot is a self-hostable, open-source BI platform. Phase 0 built none of that.

It built the thing the product stands on: the repository, the delivery pipeline, the
metadata layer, authentication, authorization, the design system, and single-binary
packaging. Nothing user-facing shipped.

> The temptation in Phase 0 is to skip it and start on features. The cost shows up in
> month nine, when adding row-level security means rewriting every handler.

The bet is that a phase spent on foundations makes every later phase cheaper. The rest of
this document is the evidence for whether that bet was paid.

---

## 2. What exists

**One binary. 30 MB. No runtime dependencies.** It serves the API and the React
application from the same process, against SQLite or PostgreSQL, on six platforms.

| | |
|---|---|
| **Identity** | Password login (Argon2id, escalating lockout), server-side sessions that revoke immediately, OIDC single sign-on verified against a real Keycloak |
| **Access** | Roles and permissions with group inheritance, multi-tenant scoping enforced in the data layer |
| **The application** | Login, app shell, command palette, light and dark themes, i18n with right-to-left, 24 design-system components |
| **Operating it** | Health probes, graceful drain, JSON logs, Prometheus metrics, a Grafana dashboard, OpenTelemetry tracing, browser error reporting, `doctor`, `backup`/`restore`, secrets encrypted at rest |
| **Shipping it** | Five required CI checks, signed releases for six platforms, a multi-arch container, an OpenAPI spec that tests keep honest |

---

## 3. The demo

Every command below was run while writing this, and the output is what came back —
except the setup token and the encrypted secret, which are placeholders. Real ones
do not belong in a document, even dead ones.

### 3.1 Thirty seconds, from nothing

```sh
mkdir demo && cd demo
./pivot serve
```

No configuration file. No database. No arguments.

```
  This Pivot has no administrator yet.

    Open:  http://localhost:8080/setup
    Token: EXAMPLE-TOKEN-yours-will-be-random
```

Open it, paste the token, fill in four fields, and you are inside Pivot as its
administrator.

**Measured:** 226 ms to serving, 399 ms to having an administrator. The thirty-second
promise is bounded by typing speed, not by software.

*Point to make:* the token exists because an unclaimed Pivot on a network is an instance
takeover waiting for a port scan. "It is only open for a few seconds" is not a security
argument.

### 3.2 It tells you what is wrong with it

```sh
pivot doctor
```

```
  ok    version                dev (6a37a8b)
  ok    configuration          no file; defaults and environment only
  ok    frontend               embedded
  ok    listener               :8080 was bindable
  warn  secrets                key c78cccd1, from the key file
                               -> the key is in the same directory as the database, so a
                                  copy of one is a copy of both
  ok    database               reachable
  ok    schema                 version 5, up to date
```

*Point to make:* that warning is real and it was found **by running this command against a
real install** during the part that built it. A diagnostic that only passes on a working
machine is worth nothing — so its tests break things on purpose, and the happy-path test
is the shortest one in the file.

### 3.3 A backup you can actually restore

```sh
pivot backup                    # safe while serving
pivot restore pivot-2026....db  # server stopped
```

Backup uses SQLite's `VACUUM INTO`, not a file copy. With WAL there are three files and no
atomic copy across them — a `cp` under load restores as `database disk image is malformed`,
months later, when somebody needs it.

*Point to make:* the test proves the difference by backing up a database that is **actively
being written to** and then running `PRAGMA integrity_check` on the copy.

### 3.4 Secrets that survive a stolen database

```sh
$ sqlite3 pivot.db "select slug, client_secret from identity_providers"
okta|pivot.v1.c78cccd1.WRAPPED-DATA-KEY....CIPHERTEXT...
```

Each secret gets its own data key, wrapped by a master key held outside the database.

```sh
pivot secrets status   # what is stored, under which key
pivot secrets rewrap   # move everything onto the current key
```

*Point to make:* rotation was **walked, not described** — new key primary with the old
retained, rewrap, drop the old key, everything still readable. And what this does *not*
protect against is written down as plainly as what it does: Pivot decrypts on demand, so
root on the host can read the plaintext.

### 3.5 Single sign-on against a real identity provider

```sh
docker run -d -p 8180:8080 -e KC_BOOTSTRAP_ADMIN_USERNAME=admin \
  -e KC_BOOTSTRAP_ADMIN_PASSWORD=admin quay.io/keycloak/keycloak:26.0 start-dev

PIVOT_TEST_KEYCLOAK_URL=http://localhost:8180/realms/master \
  go test ./internal/api/ -run TestKeycloakLoginRoundTrip
```

The test provisions its own client and user through Keycloak's admin API, drives
**Keycloak's own login form**, follows the callback, and asserts a Pivot session for an
account auto-provisioned from the OIDC claims.

*Point to make:* this test did not exist a week ago. The exit criterion said "end to end
against Keycloak", and what existed covered discovery and the shape of an authorization
request — and skipped unless an environment variable was set, so it had **never run**. The
exit review is what found that.

### 3.6 The container path

```sh
cd deploy && cp .env.example .env   # three values, no defaults
docker compose up -d
```

Pivot and PostgreSQL, both health-checked, Pivot waiting for the database to be *ready*
rather than merely listening.

*Point to make:* there is deliberately no Valkey and no MinIO, though the plan named
them. Nothing connects to them until Phases 9 and 5. A compose file that starts services
the product never talks to teaches operators they are required.

---

## 4. How it is built, in four decisions

**One binary, two databases.** SQLite for a laptop, PostgreSQL for a server, one migration
set, one generated query layer, and a test suite that runs against both. The alternative —
picking one — makes either the first run heavy or the tenth user impossible.

**Tenancy in the data layer.** Scope is resolved from the session and enforced where
queries are built, not remembered in each handler. A handler that forgets cannot compile.

**Tokens as runtime CSS custom properties.** Themes switch without rebuilding, which is
what Phase 8's white-label embedding needs — decided in Phase 0 because retrofitting it
later means touching every component.

**Evidence over assertion.** Every part's `Done when` was executed, not reasoned about.
The parts that went wrong are recorded with what went wrong, which is why the checklist is
1,700 lines and worth reading.

---

## 5. What went wrong, and what it cost

Worth presenting, because a review with no failures in it is a review nobody believes.

| What | How it was caught |
|---|---|
| Tracing's resource merge failed on conflicting schema URLs — **the server refused to start whenever tracing was enabled** | A test written because the coverage gate refused the PR |
| `X-Request-Id` extraction read OpenTelemetry's *global* propagator, so any process that had not called `Setup` silently dropped every incoming trace | A test for something else |
| The OpenAPI drift test **had never been able to fail** — a missing route answers 401 from the catch-all, and it only checked for 404 | Adding a route it could not see |
| A new SQLite database was mode 0644: every session token hash readable by any account on the machine | `pivot doctor`, on its first run against a real install |
| Two Pivots starting at once could **both refuse to start**, each reporting the other's key file as corrupt | Running the concurrency test twenty times under `-race` |
| Coverage that only existed when goroutines happened to interleave — absent on a two-core CI runner | The coverage gate, on the machine whose opinion counts |

The pattern: **the things that found these were the gates, and the tests written because a
gate refused something.** Not review, and not intuition.

---

## 6. What is deliberately absent

Pivot cannot connect to a data source, ask it a question, or draw a chart.

`internal/connectors`, `internal/query` and `internal/semantic` are empty stubs. That is
not an oversight — it is the whole shape of the bet. Phase 1 builds them without having to
relitigate authentication, authorization, tenancy, packaging or the design system.

Three known gaps are carried forward in writing rather than quietly:

1. The Keycloak round trip is opt-in and does not run in CI. It belongs in a scheduled
   job: what it protects against is Keycloak changing, not Pivot changing.
2. `X-Forwarded-For` is not trusted, so rate limiting behind a proxy keys on the proxy.
3. The container stack pins a release that predates the setup wizard. The release
   containing this phase has to be cut.

---

## 7. Next

**Phase 1 — Connect & Query, to v0.1.** Twelve parts, and the order is the interesting
part: the connector interface and its conformance suite come *before* the second
connector, and the execution engine comes *before* any UI that shows results.

The expensive mistake available here is building a SQL editor against one hard-coded
database and discovering the abstraction afterwards.

Two decisions in Phase 1 are worth watching from outside:

- **The cache key must include user identity** wherever row-level security could apply.
  Getting it wrong is a data breach delivered by a performance optimization — and it is
  designed now, though RLS does not ship until Phase 4, because retrofitting identity into
  a cache key invalidates everything built on top of it.
- **The bundle budget is at 185.7 KB of 200 KB** and CodeMirror does not fit in what is
  left. Phase 1 either code-splits the editor or renegotiates the NFR in writing. It does
  not quietly exceed it.

---

### Links

- Exit review, criterion by criterion — [../roadmap/phase-0-exit-review.md](../roadmap/phase-0-exit-review.md)
- Every part, with what went wrong — [../checklists/phase-0-checklist.md](../checklists/phase-0-checklist.md)
- Phases 0–2 as one table — [../roadmap/phases-0-to-2.md](../roadmap/phases-0-to-2.md)
- What is next — [../../CHECKLIST.md](../../CHECKLIST.md)
