# Phase 0 — Foundations

**Months:** M0–M2 · **Effort:** 22 ew · **Team:** 3 · **Release:** none (internal)

---

## Goal

Build the platform the product will live on: repository, CI/CD, the metadata layer,
authentication, the authorization skeleton, the design system, and single-binary
packaging. Nothing user-facing ships. Everything after this phase depends on getting it
right.

The temptation in Phase 0 is to skip it and start on features. The cost of that shows up
in month 9, when adding row-level security means rewriting every handler.

## Exit criteria

- [ ] `./pivot` starts on a clean machine, serves a login page, and creates an admin user
- [ ] A developer clones, runs `make dev`, and has a working environment in under 5 minutes
- [ ] CI runs the full gate suite on every PR in under 10 minutes
- [ ] A release tag produces signed binaries for 6 platforms and a multi-arch container
- [ ] OIDC login works end to end against Keycloak in an integration test
- [ ] The authz layer answers a permission check and is enforced in a middleware test
- [ ] Design system Storybook covers 20+ components, light and dark

---

## 1. Repository & tooling

| ID | Feature | Pri | Size |
|---|---|---|---|
| P0-REPO-001 | Monorepo layout (`cmd/`, `internal/`, `web/`, `services/ai/`, `sdk/`, `deploy/`) | P0 | XS |
| P0-REPO-002 | `Makefile` with `dev`, `build`, `test`, `lint`, `migrate`, `gen` targets | P0 | XS |
| P0-REPO-003 | `golangci-lint` config with a strict, agreed rule set | P0 | XS |
| P0-REPO-004 | Biome for frontend lint + format; `ruff` + `mypy` for Python | P0 | XS |
| P0-REPO-005 | Pre-commit hooks (format, lint, secret scan) | P1 | XS |
| P0-REPO-006 | `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, PR and issue templates | P1 | XS |
| P0-REPO-007 | Dev container definition for one-command onboarding | P2 | S |
| P0-REPO-008 | `sqlc` configuration and code generation pipeline | P0 | S |

**Note on `sqlc`:** queries are written as SQL and compiled to typed Go. This is chosen
over an ORM deliberately — BI metadata queries get complex, and an ORM's abstraction
breaks exactly when the query gets interesting. It also makes the Postgres/SQLite dual
target verifiable at build time rather than at runtime.

## 2. CI/CD

| ID | Feature | Pri | Size |
|---|---|---|---|
| P0-CI-001 | GitHub Actions: lint, test, build on every PR | P0 | S |
| P0-CI-002 | Test matrix: Go tests against Postgres 16 **and** SQLite | P0 | S |
| P0-CI-003 | `testcontainers-go` harness for integration tests | P0 | M |
| P0-CI-004 | Coverage reporting with an 80% gate on changed packages | P0 | XS |
| P0-CI-005 | Dependency scanning (`govulncheck`, `osv-scanner`, `npm audit`) | P0 | XS |
| P0-CI-006 | Secret scanning (gitleaks) | P0 | XS |
| P0-CI-007 | GoReleaser: signed binaries for linux/darwin/windows × amd64/arm64 | P0 | M |
| P0-CI-008 | Multi-arch container build, distroless base, SBOM, cosign signature | P0 | M |
| P0-CI-009 | Preview environment per PR | P2 | M |
| P0-CI-010 | Nightly build with extended test suite and load tests | P1 | S |

## 3. Metadata layer

| ID | Feature | Pri | Size |
|---|---|---|---|
| P0-META-001 | `goose` migration framework, forward-only | P0 | S |
| P0-META-002 | Core schema: `organizations`, `users`, `groups`, `memberships` | P0 | M |
| P0-META-003 | Multi-tenancy primitives: tenant-scoped repository layer | P0 | M |
| P0-META-004 | SQLite ↔ Postgres portability test harness | P0 | M |
| P0-META-005 | `pivot migrate --from sqlite --to postgres` data migration | P1 | M |
| P0-META-006 | Soft-delete and `created_at`/`updated_at`/`created_by` conventions | P0 | S |
| P0-META-007 | Optimistic concurrency (version column) on editable entities | P1 | S |
| P0-META-008 | Entity-change event stream (feeds audit, search index, webhooks) | P1 | M |

**Tenant isolation (P0-META-003) is the most important item in this phase.** The
repository layer takes tenant context from a request-scoped value and injects the scope
into every query. It is structurally impossible to write a repository method that returns
cross-tenant rows. Retrofitting this later is a rewrite, and getting it wrong is the kind
of bug that ends a company.

## 4. Authentication

| ID | Feature | Pri | Size |
|---|---|---|---|
| P0-AUTH-001 | Email/password with Argon2id hashing | P0 | S |
| P0-AUTH-002 | Server-side sessions, secure cookies, configurable TTL | P0 | S |
| P0-AUTH-003 | OIDC (Authorization Code + PKCE) with auto-discovery | P0 | M |
| P0-AUTH-004 | SAML 2.0 service provider | P1 | M |
| P0-AUTH-005 | TOTP multi-factor | P1 | S |
| P0-AUTH-006 | Password reset and email verification | P0 | S |
| P0-AUTH-007 | Scoped API keys with prefix, hash storage, and expiry | P0 | S |
| P0-AUTH-008 | Session management UI (list and revoke active sessions) | P1 | S |
| P0-AUTH-009 | Brute-force protection: rate limiting and progressive lockout | P0 | S |
| P0-AUTH-010 | Just-in-time user provisioning from OIDC/SAML claims | P1 | S |

## 5. Authorization skeleton

| ID | Feature | Pri | Size |
|---|---|---|---|
| P0-AUTHZ-001 | OpenFGA integration, embedded and external modes | P0 | M |
| P0-AUTHZ-002 | Authorization model v1: org → group → user, role assignment | P0 | M |
| P0-AUTHZ-003 | Middleware: resource-scoped permission checks | P0 | S |
| P0-AUTHZ-004 | Decision cache in Valkey with write-triggered invalidation | P1 | M |
| P0-AUTHZ-005 | Built-in roles: Admin, Editor, Analyst, Viewer | P0 | S |
| P0-AUTHZ-006 | Authorization test harness (declarative permission assertions) | P0 | S |

The full permission model — collections, RLS, column masking — lands in
[Phase 4](phase-4-collaboration-governance.md). Phase 0 builds the machinery and proves
enforcement works, so that later phases add rules rather than plumbing.

## 6. API foundations

| ID | Feature | Pri | Size |
|---|---|---|---|
| P0-API-001 | HTTP server with graceful shutdown and request timeouts | P0 | S |
| P0-API-002 | OpenAPI 3.1 spec as source of truth; handler stubs generated | P0 | M |
| P0-API-003 | Standard error envelope with stable machine-readable codes | P0 | S |
| P0-API-004 | Request logging, trace IDs, panic recovery | P0 | S |
| P0-API-005 | Rate limiting (per user, per key, per endpoint class) | P0 | S |
| P0-API-006 | CORS, CSRF, and security headers | P0 | S |
| P0-API-007 | Generated TypeScript client consumed by the frontend | P0 | S |
| P0-API-008 | WebSocket infrastructure with auth and Valkey fan-out | P1 | M |
| P0-API-009 | Health, readiness, and liveness endpoints | P0 | XS |

## 7. Frontend foundations

| ID | Feature | Pri | Size |
|---|---|---|---|
| P0-FE-001 | Vite + React 19 + TypeScript scaffold | P0 | XS |
| P0-FE-002 | TanStack Router with type-safe routes and search params | P0 | S |
| P0-FE-003 | TanStack Query with auth-aware fetch and error handling | P0 | S |
| P0-FE-004 | Tailwind 4 + design tokens as CSS custom properties | P0 | S |
| P0-FE-005 | Component library on Radix: button, input, select, dialog, dropdown, tooltip, toast, tabs, table, popover, command palette | P0 | L |
| P0-FE-006 | Storybook with light/dark and accessibility addon | P0 | S |
| P0-FE-007 | App shell: navigation, breadcrumbs, user menu, search entry | P0 | M |
| P0-FE-008 | Auth flows: login, SSO redirect, password reset, MFA | P0 | M |
| P0-FE-009 | Theming engine (light, dark, system, custom) | P0 | S |
| P0-FE-010 | i18n scaffold (`react-i18next`), English complete, RTL-ready layout | P1 | M |
| P0-FE-011 | Error boundaries and an offline/disconnected state | P0 | S |
| P0-FE-012 | Keyboard shortcut system and a global command palette | P1 | M |

**Design tokens as CSS custom properties (P0-FE-004) is a load-bearing decision.**
White-label embedding in Phase 8 means a customer restyles Pivot to their brand. If theme
values are compiled into class names, that's impossible without a rebuild. Runtime CSS
variables make it a configuration change.

**RTL-ready from the start (P0-FE-010):** layout uses logical properties
(`margin-inline-start`, not `margin-left`). This costs nothing now and is a multi-week
retrofit later.

## 8. Observability

| ID | Feature | Pri | Size |
|---|---|---|---|
| P0-OBS-001 | OpenTelemetry tracing with OTLP export | P0 | S |
| P0-OBS-002 | Prometheus metrics at `/metrics` | P0 | S |
| P0-OBS-003 | Structured logging (`slog`) with trace correlation | P0 | S |
| P0-OBS-004 | Reference Grafana dashboard | P1 | S |
| P0-OBS-005 | Frontend error reporting (Sentry-compatible, self-hostable) | P1 | S |
| P0-OBS-006 | Opt-in, transparent, documented anonymous usage telemetry | P1 | M |

Telemetry is opt-**in**, and the exact payload is documented and printed on first run.
Self-hosted users are suspicious of phone-home behavior, correctly.

## 9. Packaging & deployment

| ID | Feature | Pri | Size |
|---|---|---|---|
| P0-PKG-001 | Frontend embedded via `embed.FS`; one binary serves everything | P0 | S |
| P0-PKG-002 | Zero-config first-run: SQLite created, admin setup wizard | P0 | M |
| P0-PKG-003 | Config precedence: flags > env > file > defaults | P0 | S |
| P0-PKG-004 | `pivot` CLI: `serve`, `migrate`, `admin`, `config`, `version`, `doctor` | P0 | M |
| P0-PKG-005 | Docker Compose reference stack | P0 | S |
| P0-PKG-006 | Helm chart with sane production defaults | P1 | M |
| P0-PKG-007 | Envelope encryption for secrets (local key; KMS in Phase 9) | P0 | M |
| P0-PKG-008 | Automatic backup of the SQLite metadata file | P1 | S |

`pivot doctor` (part of P0-PKG-004) diagnoses a broken install: connectivity, migration
state, disk, permissions, config. Support burden for self-hosted software is dominated by
"it doesn't work and I don't know why," and this is the cheapest possible answer.

---

## Technical decisions made in this phase

1. **`sqlc` over an ORM** — typed queries, verifiable dual-dialect support, no abstraction
   leak when queries get complex.
2. **OpenAPI-first** — the spec is written before the handler. This prevents the UI-driven
   API sprawl that makes a public API an afterthought.
3. **OpenFGA from day one** — even though Phase 0 only needs simple roles. Adding a
   permission system to a mature codebase means touching every handler.
4. **Tenant scoping in the repository layer** — structural, not conventional.

---

## Explicitly deferred

| Deferred | To | Why |
|---|---|---|
| SCIM provisioning | Phase 9 | Enterprise-only; OIDC JIT covers early needs |
| WebAuthn / passkeys | Phase 9 | TOTP is sufficient; WebAuthn is a larger surface |
| Full i18n translations | Phase 10 | Scaffold now, translate when there's demand |
| KMS-backed secrets | Phase 9 | Local master key is adequate for self-hosted v1 |
| Preview environments | Phase 1 | Nice to have; not blocking |

---

## Risks

| Risk | Mitigation |
|---|---|
| Phase 0 expands indefinitely — foundations are never "finished" | Hard 8-week timebox. Anything P2 that isn't done moves to Phase 1 |
| SQLite/Postgres portability proves more expensive than estimated | Measure the tax at the end of the phase; if migrations cost >15% extra, demote SQLite to dev-only |
| OpenFGA embedded mode is immature for our use | Spike in week 1. Fallback: a hand-rolled RBAC layer behind the same interface |
| Design system scope balloons | Build the 20 components Phase 1 and 2 need. Not a component library for its own sake |
