# Phase 0 — exit review

**Reviewed:** 2026-09-26 · **Verdict:** exits, with three items carried into Phase 1.

This walks the seven exit criteria in
[phase-0-foundations.md](phase-0-foundations.md#exit-criteria) and says, for
each, what was actually run. A criterion nobody exercised is not met, however
much code exists behind it.

---

## The seven criteria

### 1. `./pivot` starts on a clean machine, serves a login page, and creates an admin user

**Met.** Timed on an empty directory with a binary and nothing else:

```
start -> serving:        226 ms
start -> administrator:  399 ms
```

No configuration file, no database, no arguments. It creates `pivot.db`,
applies five migrations, writes an encryption key to `pivot.key` at `0600`, and
prints a setup token. Posting the setup form makes the first administrator and
signs them in.

The thirty-second promise is therefore bounded by how fast somebody types, not
by the software. Four Playwright tests walk the same path in a real browser
against a binary they spawn themselves (`web/e2e/first-run.spec.ts`).

### 2. A developer clones, runs `make dev`, and has a working environment in under 5 minutes

**Met.** The Go side is a module download and a build; the frontend is one
`npm ci`. Measured on this machine's link, the frontend install — the slowest
step by a wide margin — took **38 seconds**, and everything else is seconds.

The number is this connection's, not a claim about everybody's. What the
criterion is really protecting against is a setup with undocumented steps, and
there are none: `make dev` starts Vite and the Go server together, and
`make dev-db` brings up PostgreSQL when somebody wants it.

### 3. CI runs the full gate suite on every PR in under 10 minutes

**Met.** Five jobs in parallel, wall clock set by the slowest:

| Job | Duration |
|---|---|
| Go (lint, tidy, gen-check, spec, tests on SQLite **and** Postgres, coverage gate) | ~5 min |
| End to end (Playwright against the real binary) | ~2 min |
| Frontend (typecheck, jsdom suite, bundle budget) | ~1 min |
| Image (build, run, healthcheck) | ~1 min |
| Security (govulncheck, osv-scanner, npm audit, gitleaks) | ~30 s |

All five are required by a ruleset on `main`, and the gate has refused real
pull requests three times in this phase — twice for coverage, once for a
toolchain mismatch.

### 4. A release tag produces signed binaries for 6 platforms and a multi-arch container

**Met.** `v0.0.1-alpha` produced binaries for linux, darwin and windows on amd64
and arm64, each with a cosign bundle, plus a multi-arch image at
`ghcr.io/mmd4life/pivot:0.0.1-alpha`. Signatures were verified from outside the
build, with no key to trust — keyless Sigstore, identity pinned to this
repository's workflow.

Cross-compiled rather than emulated, which is what keeps the matrix a single
build.

### 5. OIDC login works end to end against Keycloak in an integration test

**Met, and it was not before this review.** The test that existed covered
discovery and the shape of the authorization request — real, useful, and not
what the criterion says. It also skipped unless an environment variable was
set, so it had never run in CI.

There is now a second test, `TestKeycloakLoginRoundTrip`, which walks the whole
thing against Keycloak 26: discovery, the authorization request, **Keycloak's
own login form**, the callback, the code exchange, the ID token, and a Pivot
session at the end. It provisions its own client and user through Keycloak's
admin API, because the redirect URI has to name a server whose port is chosen
when the test starts.

It passes. It is still opt-in — see [what is carried forward](#carried-into-phase-1).

### 6. The authz layer answers a permission check and is enforced in a middleware test

**Met.** `internal/authz` resolves permissions through direct grants and group
inheritance, and `internal/api/authz_test.go` drives the middleware over the
real HTTP surface. The assertion files in `internal/authz` state the model's
answers as data, so a change in the resolver that changes a decision fails a
test rather than a review.

Enforcement is fail-closed: an unconfigured checker denies rather than passes
through, and a 503 from the authorization backend is distinguishable from a 403
so the UI can say "try again" rather than "ask an administrator".

### 7. Design system Storybook covers 20+ components, light and dark

**Met.** 24 components with stories, each rendered in both themes. The
accessibility regime around them is five layers, and the split is deliberate:
Go checks the token contrast pairs, jsdom checks structure and keyboard
behaviour, a real browser checks contrast as painted, and the last layer is a
person with a screen reader.

---

## Carried into Phase 1

Three things are true and small, and pretending otherwise would be the point of
this document failing.

1. **The Keycloak round trip does not run in CI.** It is opt-in on an
   environment variable, because the image is a large pull and a suite that
   cannot run without a container is a suite people stop running. It should
   become a scheduled job rather than a per-PR one: the thing it protects
   against is Keycloak changing, not Pivot changing.

2. **`X-Forwarded-For` is not trusted, so rate limiting behind a proxy keys on
   the proxy.** Per-IP limits are effectively global in that deployment. It is
   documented in `docs/operations/installing.md` and owned by Phase 9's
   deployment work; it matters more once Pivot is exposed to the internet,
   which Phase 1 starts to make worthwhile.

3. **The container stack pins a release that predates the setup wizard.**
   `0.0.1-alpha` was cut after Part 13, and parts 14 and 15 came afterwards —
   so `docker compose up` on that tag serves an instance with no `/setup` page.
   Closing this phase means cutting the release that contains it.

---

## What the phase actually produced

One binary, 30 MB, no runtime dependencies, serving an API and a React
application from the same process against SQLite or PostgreSQL.

- **Identity** — password login with Argon2id and per-account lockout,
  server-side sessions that revoke immediately, OIDC single sign-on, roles and
  permissions with group inheritance, multi-tenant scoping enforced in the data
  layer rather than remembered in handlers.
- **The application** — login, an app shell with a command palette, light and
  dark themes on runtime CSS custom properties, i18n with RTL, and an
  accessibility regime with four automated layers.
- **Operating it** — `/healthz`, `/readyz`, graceful drain, structured JSON
  logs, Prometheus metrics with a reference Grafana dashboard, OpenTelemetry
  tracing, browser errors reported back with the trace of the call that failed,
  `pivot doctor`, `pivot backup`/`restore`, and secrets encrypted at rest.
- **Shipping it** — five required CI checks, signed multi-platform releases, an
  OpenAPI spec that tests keep honest, and a first run that needs no CLI.

What it deliberately does not have: any way to connect to a data source, ask a
question, or draw a chart. That is Phase 1, and Phase 0 exists so that Phase 1
does not have to relitigate authentication, authorization, tenancy, packaging
or the design system while it is being built.
