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
| **Last completed** | Part 15-a — The first run |
| **Next up** | **Part 15-b — Your own account** |
| **Current phase** | Phase 0 — Foundations |
| **Branch** | `main` |
| **Blockers** | None |
| **Repo** | https://github.com/Mmd4LIFE/pivot |

**Where the code stands:** `./bin/pivot serve` runs an HTTP server with structured JSON
logging, `/healthz`, `/readyz` (including a database check), and a graceful drain on
SIGTERM. **A user can log in over HTTP and call an authenticated endpoint.**
`pivot migrate up|status|version|create` manages schema v5 on **both** SQLite and
Postgres. `pivot config show|env` reports configuration. **`make all` produces one 20 MB
binary that serves the React application and the API together**, with no Node at runtime.
Packages with real code: `version`, `config`, `logging`, `api`, `auth`, `authz`, `oidc`,
`cli`, `store`, `web`. Still `doc.go` stubs: `connectors`, `semantic`, `query`.

**Dependencies:** cobra, yaml.v3, goose, pgx/v5, modernc.org/sqlite (pure Go — no CGo, so
Part 13's six-platform cross-compile stays a single build matrix), google/uuid,
x/crypto (argon2), x/term (no-echo password prompt), coreos/go-oidc (ID token
verification — two direct requires, and not something to hand-roll), and **OpenTelemetry**
(Part 14-b: four direct requires that bring 52 more, **+7 MB of binary**, measured and
justified in [ADR-0001](docs/architecture/adr/0001-backend-language.md#amendments)).

**Metrics are on by default**, unlike tracing: `/metrics` needs no collector to exist,
costs nothing until something scrapes it, and an operator who must first go and enable
metrics before they can find out why the thing is slow has been failed already. Three
instruments, and **errors are a label rather than a counter** — rate and error rate come
from one series filtered by status class, so they cannot disagree with each other. Labels
are bounded by construction: the route label is the mux's *pattern*, so a scanner probing
for `/wp-admin` cannot create a series per guess. `deploy/grafana/pivot-overview.json` is
the reference dashboard, and a test matches every query in it against a real exposition.

**A fresh binary can be set up from a browser.** `pivot serve` with no configuration
creates `pivot.db`, migrates it, prints a setup token, and serves a `/setup` page that
turns somebody with no account into a signed-in administrator — claiming the instance is
the login, not a step before it. The token is what stops whoever reaches an unclaimed
instance first from claiming it. Setup closes permanently once an organization exists,
and the CLI (`pivot admin create-user`) remains the way to recover an instance nobody can
log into.

**A browser error reaches the server log**, carrying the trace and request ID of the API
call that failed. The server hands those back in a `traceresponse` header rather than
letting the browser invent them, and the reporter attaches them only when the reported
error *is* that failed call — exact or absent, because a guessed trace ID sends an
operator somewhere else. `POST /api/v1/telemetry/errors` is unauthenticated on purpose
and defended by shape: strict rate limit, 16 KiB cap, control characters stripped, one
log line and no response. **Not Sentry's SDK — 0.6 KB gzipped against roughly 30**, and
no third-party script on the login page of a product people self-host.

**Tracing is off by default and no-ops when off.** A laptop install should not dial a
collector that does not exist, and OpenTelemetry's no-op tracer makes a span two pointer
assignments — so the instrumentation is unconditional and honest rather than guarded by
`if enabled` at every call site. Turn it on with `PIVOT_TRACING_ENABLED=true` and an OTLP
endpoint. A trace spans **HTTP → authz → database**, every log line inside it carries
`trace_id`, and a trace arriving from another service is continued rather than restarted.
The 63 `Querier` methods are decorated by a **generated** file — 63 identical wrappers is
what a person types wrong once and nobody notices.

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

**SSO:** `internal/oidc` does Authorization Code + PKCE with auto-discovery, and verifies
every ID token rather than decoding it. **A returning user is matched on the provider's
`sub` claim and never on their email**: directories reassign addresses, and matching on
one hands the next holder of ada@example.com the previous Ada's account. Adopting an
existing local account by address is **opt-in per provider and off by default**
(`link_by_email`), and even then requires `email_verified` — it exists for the window in
which an organization migrates onto SSO. Group membership follows the directory in both
directions; groups are matched by name and **never created**, because a directory with
hundreds of them would otherwise fill the organization with empty ones. Attributes are
written with `source = 'oidc'`, so a sync replaces exactly what the provider owns and
leaves anything set by hand alone. **`identity_providers.client_secret` is stored in
plaintext** — Part 15 owns envelope encryption for this column and Phase 1's connection
credentials together; the API never returns it, and `hasClientSecret` reports presence
instead.

`/api/v1/auth/oidc/{provider}/{start,callback}` and `/auth/providers` are live, plus
admin CRUD gated on `manage_organization`. The flow's `state`, `nonce` and PKCE verifier
live in a short-lived `HttpOnly` cookie scoped to the SSO path. **`SameSite=Lax` is
required there and `Strict` would break every login** — the callback is a top-level
navigation *from the provider's origin*, which is exactly what Strict withholds. The
post-login `return` path accepts only a same-site absolute path: an open redirect on the
login route is what turns a phishing link into a convincing one. SSO issues a session
through the same `auth.Service.StartSession` a password login uses, so there is one
expiry policy rather than two. **`server.baseURL`** sets the redirect URI; empty derives
it from the request, which is right locally and wrong behind a proxy that rewrites the
scheme or Host.

**The browser application is behind a login.** `/login` hangs off the root; everything
else hangs off a pathless `authenticated` layout route, so **a new page is protected by
default** and letting one out takes a deliberate edit. The guard runs in `beforeLoad`,
before a child route fetches anything — a check in a component's render happens after the
requests it was meant to prevent have already gone out. It is not the security boundary:
the server refuses every request without a valid session cookie and would with this file
deleted. What it buys is not watching an empty shell fail to load, and remembering where
you were going.

**A session that ends mid-visit is handled in one place.** Every 401 from any query or
mutation reaches `createQueryClient`'s error handler, which nulls the cached session,
sends the user to `/login` carrying their destination, and says `expired` so the page
explains itself. Per-call-site handling would mean every future feature has to remember.

**The redirect destination is attacker-controlled and treated that way.**
`safeDestination` honors only a same-site absolute path — the same rule the server applies
to the SSO `return` parameter, applied again on the client, because neither end can vet
the other's redirect. `//evil.example` and `/\evil.example` are the cases that catch
people who only checked for a leading slash.

**i18n is in place before the strings are.** Every user-visible string comes from
`src/i18n/en.ts`, and the catalog *is* the type — `t("auth.singIn")` is a compile error
rather than a key rendered onto the page. Direction is part of the locale, not a separate
setting: `applyDirection` writes `lang` and `dir` onto `<html>`, which is the one
attribute every logical property in the stylesheet resolves against. A development-only
pseudo-locale carries the English strings and declares itself RTL, so the mirrored layout
can be checked without inventing translations nobody here can read. The signed-in user's
stored `locale` drives both, so an `ar` profile gets a right-to-left layout immediately,
reading English until somebody writes the Arabic.

**Frontend:** Vite 6 + React 19 + TypeScript 5.9 + Tailwind 4, with TanStack Router and
Query. `web/embed.go` embeds `web/dist` and serves it as the catch-all outside the API
prefix. **`web/dist` ships with a committed placeholder**, so `go build ./...` and the
whole Go suite work on a machine with no Node — a backend-only contributor never runs the
frontend build, and the server explains itself rather than 404ing when none is embedded.
Hashed assets under `/assets/` get a year and `immutable`; the shell is never cached,
because it names those hashed files and a stale copy is the white screen only a hard
refresh fixes. A missing file is a **404, never the shell** — answering HTML for a missing
`.js` produces a MIME error rather than a missing-file one, which is a confusing hour.

**The application gets its own CSP.** The API's `default-src 'none'` is right for JSON and
would render a blank page, which Part 5 anticipated. The document policy allows `'self'`
scripts and no inline ones — so the theme bootstrap is `/theme-init.js`, render-blocking
so it still beats first paint.

**Design tokens are runtime CSS custom properties** (`--pivot-*`), mapped into Tailwind
with `@theme inline`. That is what makes Phase 8's white-label embedding a configuration
change rather than a rebuild, and it is asserted: the built CSS must contain `var(--pivot-`
and must not contain Tailwind 3's `<alpha-value>` placeholder, which Tailwind 4 emits
verbatim and browsers silently discard.

**The design system is `web/src/ui`**, exported through one barrel. 71 components across
24 modules — many are parts of a family, so a Table is six of them — all on Radix where
there is keyboard behavior to get right, all built on the tokens and never on a
hard-coded color. The command palette adds cmdk, because a filtered combobox with
`aria-activedescendant` is not something to hand-roll.

Conventions worth knowing before adding one. A label is a **required prop** on Checkbox,
Radio and Switch; `CardTitle` takes an explicit heading level with no default; a Dialog's
`title` and a Table's `caption` are required and can be hidden but not omitted — each is
a case where an optional argument produces a defect nobody notices, so the type system
asks instead. And two pairs are easy to confuse, where choosing wrong changes what a
screen reader says rather than only how it looks: **Select** picks a value and
**DropdownMenu** runs a command; **Popover** takes focus and can hold controls,
**Tooltip** never takes focus and must not.

**Accessibility is checked in five layers, and no layer covers another.**
[docs/design/keyboard-audit.md](docs/design/keyboard-audit.md) is the map.

1. **Color, in Go.** `web/tokens_test.go` computes WCAG contrast from the token file — no
   browser, no `node_modules`, runs in `make test` anywhere. Found **nine real failures**
   in the tokens Part 9 shipped.
2. **Structure, in jsdom.** `web/src/ui/a11y.test.tsx` runs axe over every story (106
   scans, 130 tests), scoped to the WCAG 2.1 A/AA tags, `color-contrast` off because
   jsdom has no layout. It scans `document.body` rather than the render container —
   **every overlay portals out of the container**, so the narrower scan would have
   skipped exactly the half where accessibility goes wrong. Caught `Button`'s `asChild`
   throwing on every render.
3. **Behavior, in jsdom.** `web/src/ui/keyboard.test.tsx` drives the keyboard with
   user-event (21 tests): focus trapping, Escape, focus restoration, arrow keys, live
   region politeness. Caught the cmdk `aria-activedescendant` bug below.
4. **Contrast as painted, in a browser.** `web/e2e/a11y.spec.ts` runs axe over six pages
   plus the login page, the palette and the account menu, **in both themes**. This is the
   only layer where `color-contrast` actually runs — jsdom has no layout and no canvas, so
   the rule is disabled there. On its first outing it found **two real failures the Go
   pairing list had no entry for**: an avatar's initials on the inset surface, and an
   accent badge in the dark theme. A list of token pairs is only as good as the
   combinations somebody thought of.
5. **The rest, by hand.** What a screen reader says, where an overlay lands, whether the
   page behind a dialog is locked. Six items left in the audit document; **not yet run**.

Two Go tests keep the jsdom layers honest, and they run without `node_modules`:
`TestEveryComponentHasStories` / `TestAccessibilitySuiteImportsEveryStory` fail the build
if a component has no stories or a story file is missing from the suite's import list,
and `TestNothingRemovesTheFocusIndicator` fails it if a component writes `outline-none`
without naming a replacement.

**A real bug in cmdk, fixed here.** It sets `aria-activedescendant` only when the
selection *changes* — so it is absent when the palette opens, and absent again whenever a
search narrows to a single result. Both are exactly when a screen reader user needs to be
told what is active. Reproduced against cmdk alone; axe cannot see it, because an absent
attribute is not an invalid one. `useActiveDescendant` in `CommandPalette.tsx` mirrors the
DOM's `aria-selected` onto the input, mutation-verified.

**`web/vitest.setup.ts` stubs the layout APIs jsdom lacks** — `ResizeObserver`,
`scrollIntoView`, pointer capture, `matchMedia`. Without them every overlay throws on
mount. None of the stubs returns a plausible measurement, deliberately: a fake that did
would let a test assert a position no browser would produce.

**The shell is `src/components/shell`.** One navigation model in `navigation.ts` is read
by the sidebar, the breadcrumbs *and* the command palette — three copies is how a renamed
page ends up correct in two places and wrong in the palette, which nobody opens during
review. Four labeled landmarks, one `<main>` carrying `id="content"`, and a skip link as
the first tab stop, because without one a keyboard user tabs the whole sidebar on every
page before reaching what they came for. `PageHeader` owns the `<h1>` so a page cannot
grow a second one.

**`make e2e` runs Playwright against the built binary**, never the dev server: what ships
is one Go process serving the embedded bundle, and the SPA fallback, the caching headers,
the document CSP and a same-origin session cookie all behave differently under Vite. 31
tests in ~20s. The suite provisions a throwaway SQLite instance on :8099 through the real
`pivot admin create-user`, so the documented first-run path is covered by the same suite
that covers the login page.

**Browsers are a separate one-off download**: `cd web && npx playwright install chromium`
(~650 MB). Pin `@playwright/test` to the version those browsers belong to — **1.63.0**
today — or the next run re-downloads. `npx playwright install-deps` fails on Ubuntu 24.10
and can be skipped there; its package list is keyed to 24.04 and the libraries are already
present.

**`make check` is the whole gate, locally.** Formatting, vet, lint, stale generated code,
`go.mod` tidiness, the OpenAPI spec, both engines, coverage, and the vulnerability audit.
`.github/workflows/ci.yml` runs the same commands as five parallel jobs.

**Two coverage facts worth knowing before touching Go code.** The gate is **80% on
changed packages**, not overall — a repository-wide number moves too slowly to ever fail.
It runs with `-coverpkg=./...` **and Postgres**, which matters: `internal/store/repo` reads
65.8% without Postgres and 82.5% with it. Generated code and `cmd/pivot` are excluded, and
that list is meant to stay short.

**Three packages are below the line today**, and the first pull request to touch one has
to bring it up. Three others have been paid off, each because a part needed to touch them:
`internal/cli` 30.4% → 86.1% (Part 13-a), `internal/api` 70.0% → 80.5% and
`internal/config` 65.7% → 87.3% (Part 14-a). The remaining three are all small:

| Package | Coverage | Statements |
|---|---|---|
| `internal/store/model` | 50.0% | 6 |
| `internal/store/dbtypes` | 61.1% | 95 |
| `internal/version` | 68.4% | 19 |
| `internal/auth` | 69.3% | 264 |

That is the gate working as designed and it will not feel like it. `internal/api` is the
expensive one.

**The bundle budget is nearly spent, at Phase 0.** The NFRs allow 200 KB gzipped for
login-to-shell and the build is at **185 KB — 93%**. Of that, **our own code is 11.5 KB**;
the rest is React (67.8), the design system and its Radix/cmdk/i18next dependencies (60.3),
and TanStack Router and Query (38.2). Phase 2 wants charts. Something will have to be
lazily loaded, and `make bundle-size` is what will say so.

**Vulnerability scanning is two tools pointed at different things.** govulncheck owns Go
and is reachability-based, so anything it reports fails outright. osv-scanner owns npm
**only** — pointing it at `go.mod` reads the `go` directive as the standard library's
version and produces **25 findings that govulncheck reports as zero**, because that
directive is a minimum language version and not a toolchain. The npm side blocks at CVSS
7.0 and reports everything below; there are 3 at 5.9 today, all dev-only.

**Pivot is released.** `v0.0.1-alpha` is on GitHub with six signed archives and six
SBOMs, and on GHCR as `ghcr.io/mmd4life/pivot` — multi-architecture, signed, with an SBOM
attestation. Both install paths are verified from outside, not merely built: a downloaded
binary runs and serves the application, and `docker run` with no arguments does the same.

**Signing is keyless, through Sigstore.** There is no private key in this project to leak
or rotate. The signature is bound to the workflow identity that produced it, which is a
stronger claim than "somebody with the key signed this" — and it means verification needs
no public key either, just the identity regexp in the release notes. The image is signed
**by digest**, because a tag can be moved afterwards.

**Storybook is a review tool and never ships.** `make storybook` serves it on :6006;
nothing it builds reaches `web/dist`, so nothing it builds can reach the binary.

**Tenant isolation:** `internal/tenant.Scope` has unexported fields and no usable zero
value. Repositories take **no org parameter at all** — they read the scope from the
context — so a caller cannot pass the wrong tenant because there is nothing to pass.
`OrganizationRepo` acts on the caller's own org; unscoped provisioning lives on
`SystemRepo`, named so every call site says what it is doing. A reflection test walks
`Repositories`' exported fields, so **a repository is covered the moment it is registered
in that struct** — it asserts all 48 methods refuse an unscoped context, and it is
mutation-verified. `api.WithTenant` rejects an unresolvable request with 401 before any
handler runs, and the scope now comes from `api.SessionTenantResolver` — the organization
is read off the session row and from nowhere in the request, so a caller cannot name a
tenant they have not authenticated against. `SingleTenantResolver` survives for tests
only and is wired nowhere.

**Schema is at v5.** 00003 added `sessions` and `login_attempts`; 00004 added
`role_assignments`; 00005 added `identity_providers` and `federated_identities`.

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
- **Stop Storybook before `make test-all`.** The dev server holds ~1.5 GB, and the Go
  suite runs Argon2 at 64 MiB per hash under `-race`; together they exhausted this
  14 GB machine and the run was killed with a bare `Terminated`, which reads like a flake.
  Same family as the `-p 1` discovery in Part 6-b. `pkill -f "storybook dev"`.
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
- **Node is 20.16.0, and that is the toolchain's binding constraint.** Vite 7/8 and
  `@vitejs/plugin-react` 5/6 require `^20.19 || >=22.12`. npm skips an optional dependency
  whose engines do not match **silently**, so Vite 8 fails with
  `Cannot find module '../rolldown-binding.linux-x64-gnu.node'` — which points at npm
  rather than at Node. `web/package.json` declares `engines` so the next person sees a
  warning instead. Upgrading Node to 20.19+ or 22 LTS unblocks the newer Vite.
- **Storybook is pinned to 9 for the same reason, and it fails in a nastier way.**
  Storybook 10 installs on Node 20.16 and then refuses to run — and prints its refusal
  while **exiting 0**, so `npm run storybook:build` reports success and produces nothing.
  Moving majors also needs all four packages (`storybook`, `@storybook/react`,
  `@storybook/react-vite`, `@storybook/addon-a11y`) uninstalled and reinstalled together;
  bumping them in place gives an ERESOLVE on the peer range. **Part 12 pinning a
  toolchain Node ≥ 20.19 unblocks both Vite and Storybook in one move.** Pinning to 9
  also carries **9 moderate advisories, all dev-only** — `npm audit --omit=dev` reports
  **0**, so nothing reaches the shipped bundle. Re-check after the Node bump; do not run
  `npm audit fix --force`, which would drag Storybook back to 10 on a Node that cannot
  run it.
- **`make web-install` once per clone** (~220 MB in `web/node_modules`). `make build`
  deliberately does *not* depend on it; `make all` is the target that builds the frontend
  and embeds it.
- **`make web-build`, never `npm run build` directly.** Vite's `emptyOutDir` deletes
  `web/dist/.gitkeep` on every build and only the make target puts it back; without it
  `//go:embed all:dist` fails on a fresh clone. `make web-keep` repairs it.
- **`go 1.26.0` is the contributor floor; `toolchain go1.26.8` is what anything builds
  with.** The distinction is not academic: CI's first run installed exactly 1.26.0 from the
  `go` directive and govulncheck found **nineteen reachable standard-library
  vulnerabilities**, crypto/x509 panicking on malformed certificates among them, all fixed
  in 1.26.1. The workflow now derives its version from the `toolchain` line, the Dockerfile
  pins the same patch, and a CI step fails if the two disagree. **Bumping Go is one line in
  go.mod plus one in the Dockerfile.**
- **`main` is protected but does not require a pull request.** All five checks are
  **Required** by a ruleset and force pushes are blocked, so a red build is visible
  immediately — but a session can still end with a direct push, which is how they do end.
  Changing that is a one-tick decision in Settings → Rules.
- **CI runs on Node 22, the development machine on 20.16.** Deliberate: 20.16 is what
  holds Vite and Storybook a major behind, and a green CI on 22 is what proves the bump is
  safe before anybody makes it. `engines` in `web/package.json` permits both.
- **`make check` needs the development Postgres.** It runs `test-all`, which starts it.
- **Lint enforces US spelling** (`misspell`, `locale: US`) and rejects both `err` shadowing
  (govet) and `err` reassignment (gocritic) — give the inner error a distinct name.

---

## Progress

```
Phase 0  Foundations        [█████████████████████████ ] 27/30   (Parts 3, 4, 6, 7, 8, 10, 11, 12, 13, 14 and 15 each split)
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

### - [x] Part 8-a — OIDC protocol, claim mapping, and JIT provisioning ✅ 2026-09-21

*Part 8 was split: the protocol and provisioning domain is a session; the HTTP
endpoints and Keycloak conformance are another. The same domain-then-surface split as
Parts 6 and 7.*

**Deliverable:** A verified OpenID Connect identity becomes a Pivot user, with no HTTP
surface of Pivot's own yet.

**Built:**
- `internal/oidc`: Authorization Code + PKCE, auto-discovery with caching, ID token
  verification, nonce checking, claim mapping
- Schema v5: `identity_providers` and `federated_identities`
- `IdentityProviderRepo` (7 scoped methods) plus the unscoped lookups login needs
- JIT provisioning: user creation, group sync, attribute sync with `source = 'oidc'`,
  default role grant
- A fake identity provider in the tests that signs real RS256 tokens

**Done when:**
- A new user is JIT-provisioned on first login with correct group mapping ✅
- Claims land in `user_attributes` with `source = 'oidc'` ✅
- *(The Keycloak testcontainer check moves to 8-b, with the endpoints it exercises.)*

**The protocol checks are tested by making tokens wrong on purpose** — a bad signature, a
foreign issuer, a replayed nonce, a mismatched PKCE verifier. A real identity provider
will not issue you a token it has broken, which is why the in-process fake exists
alongside the Keycloak test rather than instead of it. Signature verification is
mutation-verified.

**Refs:** `P0-AUTH-003`, `P0-AUTH-010`

---

### - [x] Part 8-b — SSO endpoints and Keycloak conformance ✅ 2026-09-21

**Deliverable:** SSO login working end to end against a real identity provider.

**Build:**
- `GET /api/v1/auth/oidc/{provider}/start` and `/callback`
- Flow state across the redirect: `state`, `nonce` and the PKCE verifier in a short-lived
  `HttpOnly` cookie. `SameSite=Lax` is required rather than `Strict` — the callback is a
  top-level navigation *from the identity provider*, and Strict would drop the cookie and
  break every login
- Issue a Pivot session on success, reusing `auth.Service` so SSO and password logins
  produce the same thing
- `GET /api/v1/auth/providers` — the login page needs the buttons before anyone is
  authenticated, so this is unauthenticated and must expose **only** slug and display
  name
- Admin CRUD for providers, gated on `manage_organization`; the client secret is
  write-only in the API
- `pivot admin add-provider`, so a first provider can be configured before anyone can log
  in to configure one
- Multi-provider: resolve the organization before the slug
- Extend `api/openapi.yaml` and regenerate the TS client

**Done when:**
- An integration test runs against a real identity provider ✅ *(see the note below —
  this landed differently from the plan, and better in one respect)*
- The callback rejects a mismatched `state`, and a second use of the same code fails ✅
  *(mutation-verified: deleting the state check fails the test by name)*
- A provider response never contains `clientSecret` ✅ *(asserted on the JSON of both the
  public and the administrative list; `hasClientSecret` reports presence instead)*
- Logging in through SSO produces a session indistinguishable from a password login ✅
  *(same `auth.Service.StartSession`, same cookie attributes, asserted)*

**The Keycloak test is opt-in, and something better happened live.** The container test
exists and skips unless `PIVOT_TEST_KEYCLOAK_URL` is set — the image is a large pull, and
a suite that cannot run without a container is a suite people stop running. But the live
check went further than planned: **discovery ran against Google's real OIDC issuer** and
produced a complete authorization request (`code_challenge` S256, `nonce`, `state`,
`response_type=code`, `scope=openid`). That is third-party conformance from a genuinely
independent implementation.

**Also added:** an open-redirect guard on the post-login `return` path, which was not in
this part's plan and should have been. Mutation-verified.

**Refs:** `P0-AUTH-003`, `P0-AUTH-010`

---

### - [x] Part 9 — Frontend scaffold, embedded in the binary ✅ 2026-09-21

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
- `make all && ./bin/pivot serve` serves the React app from one 20 MB binary ✅
  *(verified live: shell, hashed assets, deep link, and the API all from one process)*
- `ldd ./bin/pivot` shows libc and nothing else — no Node anywhere ✅
- Deep-linking to a client route works ✅ *(`/settings/users` returns the shell;
  `/assets/nope.js` returns 404 rather than HTML, which is what stops a bad deploy
  producing a baffling MIME error)*
- `make dev` reloads both sides ✅ *(Vite HMR for the frontend; the Go server restarts on
  a `.go` change, polled with `find` rather than adding a watcher dependency)*

**Two bugs shipped and were caught by reading the compiled output, not the page.**
`<alpha-value>` is Tailwind 3 syntax — Tailwind 4 emits it verbatim, the browser discards
the declaration, and every themed utility silently did nothing. And the theme bootstrap
was an inline `<script>`, which `script-src 'self'` blocks outright. Both are now tests:
the built CSS must contain `var(--pivot-` and no `<alpha-value>`, and the shell must
contain no inline script. Both mutation-verified.

**Node 20.16 is the constraint on the toolchain.** Vite 7/8 and `@vitejs/plugin-react`
5/6 all require `^20.19 || >=22.12`, so the stack is pinned to Vite 6 — which is what
[ADR-0002](docs/architecture/adr/0002-frontend-stack.md#amendments) chose anyway. **Part
12 should pin a Node version in CI**, which makes this a property of the project rather
than of one machine.

**Refs:** `P0-FE-001` … `P0-FE-004`, `P0-API-007`, `P0-PKG-001`

---

### - [x] Part 10-a — Design system: in-flow components and the accessibility harness ✅ 2026-09-22

**Deliverable:** The components that render in the document flow, every one of them
checked by a test rather than by review.

**Build:** `web/src/ui` — Button, Spinner, Field, Input, Textarea, Checkbox, RadioGroup,
Switch, Badge, Avatar, Skeleton, Separator, Alert, Card, Table, EmptyState (16 modules,
27 exported components). Storybook 9 with the a11y addon and a theme toolbar.
`web/tokens_test.go` for contrast, `web/src/ui/a11y.test.tsx` for axe,
`web/stories_test.go` to keep the second one honest.

**Done when:**
- [x] Storybook runs; every component has stories for its states — 16 story files, 72
      stories, served at `:6006` and built to `storybook-static`
- [x] **Zero axe violations** across all stories — 72 scans plus a presence check per
      module, 88 tests in all, `make web-test`
- [x] Theme switching works at runtime via CSS custom properties (no rebuild) — the
      Storybook toolbar sets `data-theme` on `<html>`, exactly as the application does

**Notes:** The split. The in-flow components are one session; the overlays are another,
and they are the half with the hard accessibility work — focus trapping, escape
handling, restoring focus on close, scroll locking.

**Refs:** `P0-FE-005`, `P0-FE-006`, `P0-FE-009`

---

### - [x] Part 10-b — Design system: overlays and the keyboard audit ✅ 2026-09-22

**Deliverable:** The components that float above the page, and a keyboard pass over all
of them.

**Build:** Dialog, DropdownMenu, Select, Popover, Tooltip, Tabs, Toast and the command
palette (Radix, plus cmdk for the palette's combobox), in the same `src/ui` barrel.
`web/src/ui/keyboard.test.tsx` drives the keyboard with `@testing-library/user-event`.
`web/vitest.setup.ts` supplies the layout APIs jsdom lacks.
`docs/design/keyboard-audit.md` is the written record.

**Done when:**
- [x] Every overlay traps focus, closes on Escape, and returns focus to whatever opened
      it — driven, not asserted in a comment: eight consecutive Tabs never leave an open
      dialog, and Escape puts focus back on the trigger for Dialog, DropdownMenu, Select
      and Popover
- [x] **Zero axe violations** across all stories, 10-a's included — 106 story scans, 130
      tests. The scan target moved from `render()`'s container to `document.body`,
      without which every overlay in this part would have gone unscanned
- [x] Toasts announce without stealing focus — `role="status"` in every tone, never
      `role="alert"`, polite for everything except an error, and focus provably unmoved
- [~] Every component is keyboard navigable with a visible focus indicator —
      **partly.** 21 tests prove where focus *goes*; `TestNothingRemovesTheFocusIndicator`
      proves nothing strips the ring without naming a replacement; `TestTokenContrast`
      proves the ring's color clears 3:1. **Whether it is drawn is unverified**, because
      jsdom paints nothing. The manual pass is written out in
      [docs/design/keyboard-audit.md](docs/design/keyboard-audit.md) and **has not been
      run** — 10 items, ~20 minutes with a keyboard and a screen reader

**Notes:** The manual pass is the one criterion this part could not close, and pretending
otherwise would have made the checklist worth less than the tests. Part 11 installs
Playwright, which is what turns most of that list into code.

**Refs:** `P0-FE-005`, `P0-FE-006`, `P0-FE-009`

---

### - [x] Part 11-a — i18n, the login page, and the session boundary ✅ 2026-09-22

**Deliverable:** A user signs in through the browser, and a signed-out one cannot get
past the door.

**Build:** `src/i18n` (i18next + react-i18next, English complete, typed keys, direction
from the locale), `src/routes/login.tsx`, `src/routes/authenticated.tsx` (the guard),
`src/lib/redirect.ts`, `src/components/OfflineBanner.tsx`, `LocalePicker`, login/logout
mutations and a central 401 handler in `src/api/queries.ts`.

**Done when:**
- [x] Sign in and sign out work against the real backend — verified live against
      `./bin/pivot serve` on SQLite: 401 for a wrong password, 200 + `HttpOnly` cookie
      for a right one, 204 logout, 401 after
- [x] 401 responses redirect to login and preserve the intended destination — the guard
      does it before a route loads, and a session that ends mid-visit does it from
      anywhere, query string and fragment included
- [x] The destination cannot be turned into somebody else's website — the same rule the
      server applies to the SSO `return` parameter, applied again on the client
- [x] Every string a user can read comes from the catalog, and a wrong key is a compile
      error rather than a key rendered onto the page
- [x] Switching locale flips `lang` and `dir` on `<html>` with no rebuild

**Notes:** i18n came first on purpose. The expensive part of internationalization is
never the library — it is the four hundred strings already written inline and the
stylesheet full of `margin-left`. Both are free now.

**Refs:** `P0-FE-007`, `P0-FE-010`, `P0-FE-011`

---

### - [x] Part 11-b — The app shell, and the browser proof ✅ 2026-09-22

**Deliverable:** A signed-in user lands in the application proper, and a real browser
says so.

**Build:** `src/components/shell` — `AppShell` (skip link, four labeled landmarks,
`PageHeader`), `SideNav`, `Breadcrumbs`, `UserMenu` (theme, language, sign out),
`CommandMenu` (the palette on Ctrl/Cmd+K), one navigation model in `navigation.ts` read by
all three. Five placeholder routes that say which phase builds them. Playwright against
the built binary: `web/e2e/auth.spec.ts` (15) and `web/e2e/a11y.spec.ts` (16).

**Done when:**
- [x] Full browser login → shell → logout works against the real backend, driven by
      **Playwright against the built binary** — 31 tests, 21s
- [x] The shell's accessibility holds — one `<main>`, one `<h1>`, a skip link that is the
      first tab stop and lands in the content, breadcrumbs and sidebar as separately
      labeled `<nav>`s, the account menu reachable and operable by keyboard
- [x] Layout uses logical properties throughout, **verified by setting `dir="rtl"`** —
      the sidebar's bounding box is measured on both sides of the switch, so this is real
      layout rather than an attribute
- [x] **Zero axe violations in a real browser**, six pages plus the login page, the
      palette and the account menu, **in both themes** — the first time `color-contrast`
      has ever actually run, and it found two failures the Go pairing list had no entry
      for
- [~] The manual items in [docs/design/keyboard-audit.md](docs/design/keyboard-audit.md)
      — **four of the ten are now tests**; the remaining six still need a person and
      **have not been run**

**Cut, and moved rather than dropped:** the change-password page and the
`POST /auth/password` endpoint it needs. It is a backend feature — a new service method
that verifies the current password and revokes every *other* session — not app shell, and
it belongs with Part 15's account and first-run work. Listed there.

**Refs:** `P0-FE-007`, `P0-FE-008`, `P0-FE-009`, `P0-FE-011`

---

### - [x] Part 12-a — The quality gate, written and locally proven ✅ 2026-09-22

**Deliverable:** Every gate in
[00-principles.md](docs/roadmap/00-principles.md#5-quality-gates-in-ci) exists, runs, and
has been watched to fail.

**Build:** `.github/workflows/ci.yml` — five parallel jobs (Go, Frontend, End to end,
Security, Image), `.github/workflows/README.md` explaining each. `scripts/coverage-gate.sh`
(80% on changed packages), `scripts/audit.sh` (govulncheck + osv-scanner with one severity
decision), `web/scripts/bundle-size.mjs` (the NFR budget), `Dockerfile` + `.dockerignore`,
and `make check | audit | coverage-gate | bundle-size | image`.

**Done when:**
- [x] Every command the workflow runs has been run locally and passes — `make check`
      exits 0, covering formatting, vet, lint, stale generated code, `go.mod` tidiness,
      the spec, both engines, and coverage
- [x] The Postgres half cannot silently skip — the tests run with
      `PIVOT_TEST_POSTGRES_URL` set and a following step fails the build if it finds a
      skip in the log
- [x] Each gate was watched to fail, not just to pass — the coverage gate at a raised
      threshold, the audit at a lowered one, the bundle budget's own arithmetic
- [x] The image builds and starts — 22.9 MB distroless, `docker run pivot:ci version`
- [x] No third-party actions at all: only `actions/*`, pinned binaries, and pinned images
- [x] **Green on GitHub.** The first run was four of five: Security went red because CI was
      building with the `go` directive rather than a patched toolchain, which is a real
      finding and is fixed. The second run is **five of five in ~4 minutes wall-clock on a
      cold cache** — Go 4m, End to end 1m, Image 1m, Security 47s, Frontend 43s — against a
      ten-minute budget.

**Refs:** `P0-CI-001` … `P0-CI-006`

---

### - [x] Part 12-b — The gate, proven on GitHub ✅ 2026-09-22

**Deliverable:** The workflow is green on `main`, blocks a broken pull request, and
finishes inside ten minutes.

**Build:** One real fix the runner found, and a throwaway branch that broke each job on
purpose.

**Done when:**
- [x] All five jobs pass on `main` — three consecutive green runs
- [x] **A deliberately broken pull request is correctly blocked.** `ci/prove-the-gate-blocks`
      carried a `gofmt` violation, a type error and a Dockerfile typo. Result: **Go,
      Frontend, End to end and Image red; Security green; *Merge pull request* disabled.**
      Security staying green is the control that makes the other four mean something — the
      failures were targeted, not a blanket collapse
- [x] **Wall-clock well under 10 minutes** — ~4 minutes cold, ~3 warm, jobs in parallel
- [x] Branch protection requires all five checks — every check shows **Required** on the
      pull request, which is the ruleset proving itself

**What the first run caught, which nothing local could:** `actions/setup-go` with
`go-version-file` installs exactly the `go` directive, so CI built with 1.26.0 and
govulncheck found **nineteen reachable standard-library vulnerabilities**. The development
machine runs 1.27.1 and the container used `golang:1.26` → 1.26.8, so only the runner was
exposed. See the toolchain note in Current state.

**Notes:** `main` does **not** require a pull request to merge, by decision — sessions
still end with a direct push, and the checks run on it. The six packages below the
coverage line are listed in Current state; the first change to one of them has to bring
it up.

**Refs:** `P0-CI-001` … `P0-CI-006`, plus the gate table in
[00-principles.md](docs/roadmap/00-principles.md#5-quality-gates-in-ci)

---

### - [x] Part 13-a — The container image, and the coverage debt it collected ✅ 2026-09-22

**Deliverable:** A multi-architecture image that starts with no configuration at all and
tells an orchestrator it is healthy.

**Build:** `pivot healthcheck` (`internal/cli/healthcheck.go`), the Dockerfile
cross-compiling for `linux/amd64` and `linux/arm64` with a `HEALTHCHECK`, and
`internal/cli/commands_test.go` — the tests the coverage gate demanded, driving every
command an operator runs against a real SQLite file.

**Done when:**
- [x] **The image builds for both architectures** — cross-compiled from the build host
      rather than emulated, because qemu makes an arm64 build about ten times slower and
      Go cross-compiles natively
- [x] **`HEALTHCHECK` works in a distroless image** — there is no shell to run curl from,
      so the binary probes itself. `docker inspect` reports `health=healthy`
- [x] **Zero configuration start, verified**: `docker run pivot` created its SQLite
      database, applied all five migrations, and served — which is Part 15's promise,
      working early
- [x] **`internal/cli` is 30.4% → 86.1%**, which the 80% gate required before any change
      to the package would merge

**Notes:** The coverage work was not optional and not planned: `HEALTHCHECK` needs a
command, a command lives in `internal/cli`, and that package was the worst-covered in the
repository. This is the gate doing exactly what Part 12 said it would.

**Refs:** `P0-CI-007`, `P0-CI-008`

---

### - [x] Part 13-b — The release pipeline ✅ 2026-09-22

**Deliverable:** A git tag produces signed, verifiable artifacts for 6 platforms and
publishes the image.

**Build:**
- GoReleaser (linux/darwin/windows × amd64/arm64)
- Image published to GHCR as `ghcr.io/mmd4life/pivot`, tagged `:latest`, `:vX.Y.Z`, `:sha`
- SBOM generation, cosign signing of **both** binaries and image
- `CHANGELOG.md` automation, GitHub Release publishing

**Done when:**
- [x] **Six binaries, six archives, six SBOMs and a checksum file** — proven by
      `make release-snapshot`, which runs the whole pipeline locally without publishing
- [x] **A downloaded binary runs**, is correctly stamped, and is **statically linked** —
      extracted from the archive and executed. Static only because ADR-0003 chose
      `modernc.org/sqlite`; a cgo driver would have meant a build per distribution
- [x] **The released binary serves the real frontend**, not the committed placeholder —
      GoReleaser's `before` hook builds it, and a release that shipped the placeholder
      would fail nothing at all
- [x] The signing invocation is correct for cosign 3 — see the note below
- [x] **Tagging `v0.0.1-alpha` published everything, and it was verified from outside.**
      Not "the jobs went green" — the artifacts were downloaded and checked:
      `sha256sum -c` passes; `cosign verify-blob` on `checksums.txt` returns **Verified
      OK**; the extracted binary reports `0.0.1-alpha` and serves the real application on
      `/` and `/login`; `docker pull ghcr.io/mmd4life/pivot:0.0.1-alpha` works and the
      manifest carries **linux/amd64 and linux/arm64**; `cosign verify` on the image
      validates against the transparency log; the SBOM attestation verifies; and the
      container runs with no configuration at all

**What the snapshot caught before the tag did:** cosign 3 rejects the 2.x signing flags —
`--output-signature` and `--output-certificate` are deprecated and it now fails with
*"must specify --bundle with --new-bundle-format"*. Written against the old invocation and
never run, that would have failed **on the release**, after the tag was already pushed and
half the artifacts uploaded. It uses the bundle format now, which also means verifying
takes one download instead of three.

**Notes:** The Dockerfile, `.dockerignore` and the multi-architecture build are 13-a's.
GoReleaser builds the binaries; the image stays on buildx in the workflow, because
GoReleaser's Docker support wants to copy a prebuilt binary into an image and this
Dockerfile also builds the frontend — bending it to suit the release tool would make what
ships differ from what CI tests on every pull request.

**Refs:** `P0-CI-007`, `P0-CI-008`

---

### - [x] Part 14-a — The API's untested half ✅ 2026-09-22

**Deliverable:** The endpoints that decide who can get in, tested doing their job rather
than only refusing to.

**Build:** `internal/api/admin_test.go` and `internal/config/env_test.go`.

**Why this is a part and not a footnote:** Part 14 adds tracing, which means touching
`internal/api` and `internal/config` — both under the 80% gate. Paying that revealed the
debt was not a number. The endpoint assertion harness proves an unpermitted caller gets a
403, and **a 403 never reaches the handler** — so `handleGrant`, `handleAdminCreate`,
`handleAdminUpdate` and `handleAdminDelete` had **zero** coverage between them, and every
test that needed a role granted called the repository directly and went around the
endpoint. The administrative surface that writes role assignments and identity providers
had never once been driven.

**Done when:**
- [x] **`internal/api` 70.0% → 80.5%**, by testing grant, revoke, list, the role catalog,
      and provider create/update/delete on their happy paths, plus the validation each
      refuses on
- [x] **`internal/config` 65.7% → 87.3%**, by walking the whole environment table: every
      `PIVOT_*` variable is set and the resolved configuration is asked whether it took.
      Mutation-verified — renaming one binding fails from both directions, as an
      undocumented variable and as a documented one nothing reads
- [x] Optimistic concurrency on providers is proven: a second update at a stale version
      loses, so one administrator cannot silently overwrite another
- [x] The client secret is never echoed back, asserted rather than assumed

**Refs:** `P0-OBS-001` … `P0-OBS-005` (the observability work itself is 14-b)

---

### - [x] Part 14-b — Tracing, and log correlation ✅ 2026-09-22

**Deliverable:** A request can be traced end to end.

**Build:** `internal/observability` (OTel setup, OTLP/HTTP export, parent-respecting
sampling, flush on shutdown), an `observability.tracing` config section with five
environment variables and validation, `logging.WithTrace`, `api.WithTracing`, a span on
the authorization resolver, and a **generated** tracing decorator over `repo.Querier`.

**Done when:**
- [x] **A single trace spans HTTP → authz → database**, asserted in a test with an
      in-memory exporter: three spans, one trace ID, correctly nested. A collector would
      only confirm the wire format, which is OpenTelemetry's problem; what breaks is the
      nesting
- [x] **Every log line carries its trace ID** — and a derived logger keeps it, which is
      the silent failure: `With(...)` returning an unwrapped handler still logs perfectly
      and is simply no longer connected to anything. Mutation-verified
- [x] **An incoming trace is continued rather than restarted**, so a request arriving
      from another traced service is one story and not two
- [x] The authorization span records **its decision**, because "why was this a 403" is the
      question a trace gets opened to answer
- [ ] `/metrics`, the Grafana dashboard and frontend error reporting — **14-c**

**The repository layer is not separately instrumented, deliberately.** Its span would sit
between the HTTP span and the database span carrying nothing the database span does not
already say. The 63 `Querier` methods are decorated instead, which is where the useful
name is (`db.GetUserByEmail`).

**What it cost, measured:** 119 → **175 modules**, 21 → **28 MB**. Recorded with the
reasoning in [ADR-0001](docs/architecture/adr/0001-backend-language.md#amendments),
including the fact that choosing OTLP over HTTP to avoid the gRPC tree **did not avoid
it** — `proto/otlp` depends on gRPC either way.

**The gate caught this part's own new package**, which is the first time it has fired on
code rather than on debt: `internal/observability` went in at 37.5% and CI refused it. The
tests that followed found a bug that would have broken tracing for everyone who enabled
it — see the session log.

**Refs:** `P0-OBS-001` … `P0-OBS-005`

---

### - [x] Part 14-c — Metrics, and the dashboard ✅ 2026-09-22

**Deliverable:** The numbers an operator watches.

**Build:** `observability.SetupMetrics` (OTel metrics, Prometheus exporter, a private
registry), `api.WithMetrics`, the `/metrics` route, an `observability.metrics` config
section, and `deploy/grafana/pivot-overview.json`.

**Done when:**
- [x] **`/metrics` exposes request rate, duration and in-flight**, verified by scraping
      the real binary. Three instruments and no more: errors are a **label** on the
      request counter rather than a counter of their own, so rate and error rate cannot
      drift apart — there is only one count
- [x] **Labels are bounded by construction.** The route label is the mux's pattern, never
      the path, so `/auth/sessions/{id}` is one series rather than one per session; a
      scanner probing for `/wp-admin` collapses into the catch-all. Both asserted
- [x] **The dashboard is checked against the metrics that exist** — four panels, every
      query's metric and label matched against a real exposition. "Renders against real
      data" needs a Grafana; what a test can own is the way a reference dashboard actually
      rots, which is a rename leaving four silently empty panels
- [x] Scraping needs no session: Prometheus cannot hold one, and every scrape would
      otherwise cost an Argon2 verification

**Cost:** 175 → **193 modules**, 28 → **30 MB**.

**Refs:** `P0-OBS-001` … `P0-OBS-005`

---

### - [x] Part 14-d — Frontend error reporting ✅ 2026-09-23

**Deliverable:** An error in somebody's browser reaches the operator, with its trace.

**Build:** `POST /api/v1/telemetry/errors`, `web/src/lib/report.ts`, the `traceresponse`
response header, and the error boundary and `window` listeners wired to them.

**Done when:**
- [x] **A thrown error in the browser is logged by the server with the trace of the API
      call that failed.** The browser does not invent that trace: the server returns it
      in a `traceresponse` header (W3C Trace Context Level 2) and the client reads it off
      the failed response. A client-generated trace ID would also be a client-generated
      *sampling decision*, and would name something the trace store has never heard of
- [x] **The request ID is sent alongside it**, because the two are available at different
      times — a trace ID exists only when tracing is on, which by default it is not, while
      a request ID is on every response Pivot has ever sent
- [x] **Exact or absent, never guessed.** The identifiers are attached only when the
      reported error *is* the failed request. Remembering the last failure and stapling
      it to whatever comes next points an operator at an unrelated trace, which costs
      more than having none
- [x] Rate limited (`LimitTelemetry`, burst 10 so one broken render's three events all
      get through), size limited to **16 KiB** rather than the API's 1 MiB, and
      unauthenticated by necessity — the errors worth having are the ones on the page
      where logging in was supposed to work
- [x] Every field is treated as hostile: control characters stripped so nothing can forge
      a log line or paint a terminal, fields truncated rather than rejected, trace IDs
      checked for shape, and the browser's trace logged under `browser_trace_id` so it
      cannot overwrite the request's real one
- [x] **Proved in a real browser**, not only in jsdom: three Playwright tests throw from
      a page served by the real binary and assert the report arrives and is accepted. That
      is the only layer that can check the listeners are actually installed, that a real
      error event carries what the reporter expects, and that the request survives the
      application's own Content-Security-Policy
- [x] **The bundle budget still passes: 185.1 → 185.7 KB of 200 KB.** The reporter costs
      **0.6 KB gzipped** against roughly 30 KB for Sentry's SDK

**Notes:** What this gives up is real — no source maps, no grouping, no release tracking,
no offline queue. Those are the reasons to reach for a service, and Phase 9 can add one.
What it buys is that an air-gapped Pivot still reports its own errors, with no account
anywhere and no third-party script on the login page.

**Refs:** `P0-OBS-001` … `P0-OBS-005`

---

**Part 15 is split into four.** It carried the setup wizard, `pivot doctor`, SQLite
backup, envelope encryption, the account page, the whole container stack *and* the Phase 0
exit review. That is four sessions wearing one number, and the first of them — somebody
downloading a binary and getting to a working Pivot — is the one the product is judged on.

### - [x] Part 15-a — The first run: nobody has an account yet ✅ 2026-09-23

**Deliverable:** **Download, run, open a browser, become the administrator.** No CLI.

**Build:** `internal/setup`, `GET /api/v1/setup/status`, `POST /api/v1/setup`, a one-time
setup token printed at startup, and the `/setup` page the browser is sent to when the
instance has no accounts.

**Done when:**
- [x] **A binary with no config, no database and no arguments takes somebody from nothing
      to signed-in as an administrator**, in one form. Claiming the instance *is* the
      login — the response is the same session envelope `/auth/login` returns, and it sets
      the same cookie, because a password typed twice should not have to be typed a third
      time into a login page
- [x] **Setup closes permanently.** Keyed on organizations existing, checked against the
      database inside the lock rather than cached at startup — an instance that decided at
      boot that it was unclaimed would stay claimable for as long as it ran
- [x] **The window between starting and claiming is not open to whoever reaches the port
      first.** A token is generated per process and printed in a banner; an unclaimed
      Pivot on a network is otherwise an instance takeover waiting for a port scan, and
      "only for a few seconds" is not a security argument
- [x] **Two concurrent claims produce one administrator**, and the test is
      mutation-verified: with the lock removed, all 8 concurrent claims succeed and create
      8 organizations
- [x] **The first-user-becomes-admin rule now exists once**, shared by the CLI and the
      browser. It is a rule about privilege, and a rule about privilege written twice is
      one that is eventually only true in one of them
- [x] **Four Playwright tests walk the whole thing** against a binary they spawn
      themselves, with its own empty database — including parsing the token out of the
      process's real output, which is the only instruction a first-time user gets

**Found on the way:**
- **The spec-drift test had never been able to fail.** It asserted a documented path does
  not 404, but everything under the API prefix falls through to a catch-all that answers
  **401** first — so a documented route that was never registered looked exactly like one
  that was. It now asks the mux which pattern it would match, and immediately found that
  the test fixture had never registered the single sign-on routes either
- **`pivot serve` briefly became fatal when the schema was behind**, because the new
  banner queried a table that did not exist yet. Caught by the test that asserts serve
  warns rather than refuses. A banner must never stop a server from serving
- **A too-long password had no sentinel error**, so it would have surfaced from the setup
  form as an unexplained 500. `auth.ErrPasswordTooLong` now exists

**Notes:** Zero-config first run already worked — `pivot serve` with no arguments creates
`pivot.db`, applies five migrations and listens. Verified rather than assumed. What did
not exist was any way to create the first user except `pivot admin create-user`, and a
product whose first step is a CLI flag has not made the 30-second promise.

The mutex is per-process, which is the honest scope: a second Pivot pointed at the same
fresh Postgres could still race it. That window is one request wide on an instance nobody
has claimed, and closing it properly needs a database-level lock the repository layer does
not have. Written down in the code rather than hidden.

**Refs:** `P0-PKG-002`, `P0-PKG-004`

---

### - [ ] Part 15-b — Your own account

**Deliverable:** Changing your own password, from the product.

**Build:** `auth.Service.ChangePassword`, `POST /api/v1/auth/password`, and the account
page.

**Done when:**
- Changing a password verifies the current one and **ends every other session**, leaving
  the caller signed in on the device they changed it from
- The account page shows the sessions Part 6 already lists, and can end them

**Notes:** Moved here from Part 11-b. It needs a service method that verifies the current
password and revokes the others, which is backend work rather than shell work, and the API
has none today — only `pivot admin reset-password`.

**Refs:** `P0-PKG-002`

---

### - [ ] Part 15-c — Operating it: doctor, backup, secrets

**Deliverable:** The three things an operator needs that no feature asks for.

**Build:** `pivot doctor` diagnostics, automatic SQLite backup, and envelope encryption for
stored secrets with a local master key.

**Done when:**
- `pivot doctor` correctly diagnoses a **broken** install, not just a working one
- A SQLite instance takes a consistent backup while serving, and the restore path is
  documented and walked
- `identity_providers.client_secret` is encrypted at rest, and the key rotation story is
  written down before it is needed

**Notes:** The plaintext `client_secret` has been on the watch list since Part 7. This is
the part that owes it.

**Refs:** `P0-PKG-005`, `P0-PKG-007`

---

### - [ ] Part 15-d — The container stack, and Phase 0 close-out

**Deliverable:** The other install path, and the honest review of everything before it.

**Build:** The full containerized stack:
- `deploy/docker-compose.yml` — Pivot + Postgres + Valkey + MinIO, production-shaped
- Uses the published image from Part 13, with a pinned tag
- Health checks and `depends_on: service_healthy` so startup ordering is correct
- `.env.example` documenting every setting
- Volumes for data persistence; a documented backup/restore path

**Done when:**
- `docker compose up` brings up the full stack and serves a working Pivot
- **Both install paths verified:** the single binary *and* the container
- On a **clean machine**: download → run → browser → admin created → logged in, **in under
  30 seconds, timed**
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
| 2026-09-23 | 15-a | `internal/setup`, `POST /api/v1/setup` and its status endpoint, the setup token and its startup banner, the `/setup` page, and a shared first-user rule | **A binary with no configuration, no database and no arguments now takes somebody from nothing to signed-in administrator in one form.** Claiming the instance *is* the login: the response is the same session envelope `/auth/login` returns and sets the same cookie, because a password typed twice should not then be typed into a login page. **The window between starting and being claimed is the security problem of a first run**, and it is answered with a token generated per process and printed in a banner — an unclaimed Pivot on a network is otherwise an instance takeover waiting for a port scan, and "only for a few seconds" is not an argument. **Setup closes permanently**, checked against the database inside a lock rather than cached at startup, because an instance that decided at boot that it was unclaimed would stay claimable for as long as it ran. **Two concurrent claims produce one administrator, mutation-verified**: with the mutex removed, all 8 concurrent claims succeed and create 8 organizations. **The first-user-becomes-admin rule now exists once**, shared by the CLI and the browser — it is a rule about privilege, and one written twice is eventually only true in one of them. **Three things were found on the way.** The spec-drift test *had never been able to fail*: everything under the API prefix falls through to a catch-all that answers 401 before the 404 handler, so a documented route that was never registered looked exactly like one that was — it now asks the mux which pattern it would match, and immediately found that the fixture had never registered the SSO routes either. `pivot serve` briefly became **fatal when the schema was behind**, because the banner queried a table that did not exist yet; caught by the test that asserts serve warns rather than refuses. And a too-long password had no sentinel error, so it would have surfaced from the setup form as an unexplained 500. **Four Playwright tests spawn their own binary against their own empty database**, including parsing the token out of its real output — the only instruction a first-time user is given. |
| 2026-09-23 | 14-d | `internal/api/telemetry.go`, `web/src/lib/report.ts`, the `traceresponse` response header, `LimitTelemetry`, and the boundary and window listeners wired to them | **An error in a browser now lands in the same log as the request that caused it, carrying that request's identifiers.** The mechanism is the part worth keeping: the server returns its trace in a `traceresponse` header and the client reads it off the failed response, because a browser that *generates* a trace ID also generates the sampling decision and names a trace the store has never heard of. The request ID goes with it, since a trace ID only exists when tracing is on — which by default it is not — and a request ID is on every response Pivot has ever sent. **Exact or absent, never guessed**: the identifiers are attached only when the reported error *is* the failed request, because the tempting version — remember the last failure, staple it to whatever comes next — points an operator at an unrelated trace, which costs more time than having none. **Unauthenticated by necessity**, since the errors most worth having happen on the page where logging in was supposed to work, so it is defended by shape instead: a strict limiter with a burst of 10 (one broken render fires three events and a limit that hid two of them would hide the explanation), a 16 KiB cap rather than the API's 1 MiB, and a handler that writes one log line and nothing else. **Every field is treated as hostile** — control characters stripped so nothing can forge a log line or paint somebody's terminal, fields truncated rather than rejected, and the browser's trace logged as `browser_trace_id` so a report cannot overwrite the trace of the request carrying it. **Unknown fields are ignored here and rejected everywhere else**, because the client is a cached bundle in somebody's browser and losing its report over an unrecognised field would lose it exactly when a deploy has gone wrong. **Three layers, because each can only see its own**: jsdom for the reporter's restraint, Go for the endpoint's, and three Playwright tests throwing from a page served by the real binary — which is the only place that can prove the listeners are installed at all and that the request survives the application's own CSP. **Deliberately not Sentry's SDK: 0.6 KB gzipped against roughly 30, and the budget went 185.1 → 185.7 KB of 200.** What that gives up is real — no source maps, no grouping, no offline queue — and what it buys is that an air-gapped Pivot reports its own errors with no third-party script on the login page. |
| 2026-09-22 | 14-c | `internal/observability/metrics.go`, `internal/api/metrics.go`, the `observability.metrics` config section, the `/metrics` route, `deploy/grafana/pivot-overview.json`, and three test files | **Three instruments, and errors are a label rather than a counter** — rate and error rate are one series filtered by status class, so the two numbers on the dashboard cannot drift apart. **Cardinality is bounded by construction, not by care**: the route label is the mux's *pattern*, so `/auth/sessions/{id}` is one series rather than one per session, and a scanner probing for `/wp-admin` collapses into the catch-all — asserted in a test named for the scanner. My first attempt read `r.Pattern` before the mux had dispatched, so **every request was labelled `unmatched`** and the whole dashboard would have been one flat line; asking `mux.Handler(r)` is what actually resolves it. **The dashboard has a test**, which sounds absurd until you ask how a reference dashboard rots: a metric gets renamed and four panels go quietly empty, so the test matches every query's metric and label against a real exposition. **Metrics default to on**, unlike tracing — an operator who must first enable metrics before they can find out why the thing is slow has already been failed — and `/metrics` is unauthenticated because Prometheus cannot hold a session and every scrape would otherwise cost an Argon2 verification. **The race detector found my own global**: `SetupMetrics` wrote package-level state that tests read concurrently, the fourth defect global mutable state has caused in Part 14 alone, so I deleted the global and returned the handler instead of protecting it with a mutex. **The cost is +18 modules and +2 MB.** Frontend error reporting moved to **14-d**: it is a backend endpoint plus a browser reporter, and bolting it onto a metrics session would have got both done badly. |
| 2026-09-22 | 14-b | `internal/observability`, the `observability.tracing` config section, `logging.WithTrace`, `api.WithTracing`, a span on the authorization resolver, and a generated tracing decorator over all 63 `Querier` methods | **A trace spans HTTP → authz → database, asserted rather than demonstrated.** An in-memory exporter in a unit test checks the part that actually breaks — three spans sharing one trace ID, correctly nested — because a collector would only confirm the wire format, which is OpenTelemetry's problem and not ours. **A test found real fragility in my own design**: extraction of an incoming trace read OpenTelemetry's *global* propagator, which is a no-op until something sets it, so any process that had not called `Setup` would silently drop every incoming trace — the request still served, still traced, and belonging to the wrong story. `observability.Propagator()` is explicit now. **Log correlation's silent failure is forwarding**: a `traceHandler` that does not override `WithAttrs` and `WithGroup` returns the embedded handler unwrapped, so every derived logger — which is almost all of them — keeps logging perfectly and stops being connected to anything. Mutation-verified. **I put the tracing validation inside `if len(errs) > 0`**, which made it dead code in the only case that matters; the test caught it. **The repository layer is deliberately not instrumented**: its span would carry nothing the database span does not. The 63 decorator methods are generated, because that many identical wrappers is what somebody types wrong once and nobody notices. **The cost is +56 modules and +7 MB**, measured and written into ADR-0001 — including that choosing OTLP over HTTP to dodge the gRPC tree **did not dodge it**, since `proto/otlp` depends on gRPC either way. **Then CI refused the pull request** — the coverage gate fired on `internal/observability` at 37.5%, the first time it has caught new code rather than old debt, and it was right: I wrote the package and no tests for it. Writing them found **a bug that would have broken tracing for everyone who turned it on**: `resource.Merge` fails outright on conflicting schema URLs, and pinning semconv v1.26.0 against an SDK defaulting to v1.43.0 meant `Setup` returned an error and the server refused to start. Nothing else would have caught it, because the entire path is skipped when tracing is off. `resource.NewSchemaless` has no version to disagree about. **And I wrote a flaky test about global state while fixing a bug about global state**: the Setup tests ran `t.Parallel()` and raced over the global tracer provider, so the no-op provider intermittently decided whether another test's span was sampled. |
| 2026-09-22 | 14-a | `internal/api/admin_test.go` and `internal/config/env_test.go` — the administrative endpoints and the environment table, both driven for the first time | **Split Part 14**, because paying its coverage debt turned out to be the more valuable half. **The gate found a real hole, not a number.** The endpoint assertion harness from Part 7-b proves an unpermitted caller gets a 403 — and a 403 never reaches the handler, so `handleGrant`, `handleAdminCreate`, `handleAdminUpdate` and `handleAdminDelete` had **zero** coverage between them. Every test that needed a role granted called the repository directly and went around the endpoint entirely. The surface that writes role assignments and identity providers — the two things that decide who can get in — had never once been driven. It is **80.5%** now, tested granting, revoking, listing, and creating/updating/deleting a provider, plus what each refuses. **The environment table was 33% covered**, which meant a binding could point at the wrong field and nothing would notice — an operator sets a variable, the server reports success, the setting does nothing. `internal/config` is **87.3%** now, by setting every `PIVOT_*` variable and asking the resolved configuration whether it took. Mutation-verified: renaming one binding fails from both directions, as a variable that is documented and unsampled *and* as one that is sampled and unread. **Two properties are now asserted rather than assumed**: a provider update at a stale version loses, so one administrator cannot silently overwrite another's change; and the client secret is never echoed back in a response. |
| 2026-09-22 | 13-b | `.goreleaser.yaml`, `.github/workflows/release.yml`, goreleaser/syft/cosign pinned with verified checksums, and `make release-snapshot` | **Running the pipeline locally caught the bug that would have broken the release.** cosign 3 rejects the 2.x signing flags — `--output-signature` and `--output-certificate` are deprecated and it now fails with *"must specify --bundle with --new-bundle-format"*. Written and never run, that would have failed **on the tag**, after half the artifacts were uploaded. The bundle format also means verifying takes one download instead of three. **`v0.0.1-alpha` is published and was verified from outside rather than declared green**: checksum OK, `cosign verify-blob` **Verified OK**, the extracted binary serves the real application, the image pulls, carries both architectures, verifies against the transparency log, and runs with no configuration. **The release binary embeds the frontend because a `before` hook builds it** — without that it would have shipped the committed placeholder and served "the frontend has not been built" to every download, failing nothing at all, which is the worst kind of release bug. **Signing is keyless**: no private key exists in this project, the signature is bound to the workflow identity, and the image is signed by digest because a tag can be moved. **A prerelease never takes `latest`** — somebody's install script pulls that tag. **GoReleaser builds binaries, buildx builds the image**: GoReleaser's Docker support wants to copy a prebuilt binary in, and this Dockerfile also builds the frontend, so bending it would make what ships differ from what CI tests on every pull request. |
| 2026-09-22 | 13-a | `pivot healthcheck`, the Dockerfile cross-compiling for amd64 and arm64 with a working `HEALTHCHECK`, and `internal/cli/commands_test.go` — 33 tests driving every operator command against a real SQLite file | **Split Part 13**: the image is one session, publishing and signing another. **The coverage gate collected its first debt, immediately and unavoidably.** A distroless image has no shell to write a `HEALTHCHECK` with, so the binary has to probe itself; a command lives in `internal/cli`; and `internal/cli` was the worst-covered package in the repository at 30.4%. Adding one file meant bringing the package to 80% first. It is now **86.1%**, and the tests are worth more than the number — they run `migrate up`, `create-user`, `grant-role` and `serve` against a real database, which is the path every new install takes and none of it had end-to-end coverage. **Two of my assumptions were wrong and the code was right, again.** I asserted `serve` refuses to start on an unmigrated database; `warnIfBehind` documents why it warns instead — a rolling deploy legitimately runs old code against a newer schema, and refusing would turn that window into an outage. And I assumed auto-migration was opt-in; it defaults to **on**, deliberately, which is what makes Part 15's zero-configuration first run possible. **The container start is the strongest single result here**: `docker run pivot` with no arguments created its SQLite database, applied all five migrations, served, and `docker inspect` reported `health=healthy`. That is Part 15's promise working four parts early. **Both architectures cross-compile rather than emulate** — qemu makes an arm64 build roughly ten times slower and Go does not need it, so the build stages pin `$BUILDPLATFORM` and pass `GOOS`/`GOARCH` through. |
| 2026-09-22 | 12-b | The toolchain fix the runner found, a throwaway branch that broke each job on purpose, and a ruleset making all five checks required | **The first run on GitHub found something no local check could.** `actions/setup-go` with `go-version-file` installs exactly the `go` directive, and that directive is a floor for contributors rather than an instruction about what to build with — so CI picked the oldest permitted toolchain, 1.26.0, and govulncheck found **nineteen reachable standard-library vulnerabilities** in it, a crypto/x509 panic on malformed certificates among them, every one fixed in 1.26.1. It was invisible locally: this machine runs 1.27.1 and the container used `golang:1.26`, which resolves to 1.26.8. go.mod now carries `toolchain go1.26.8`, the workflow derives its version from that line so no version is duplicated into YAML, the Dockerfile pins the same patch, and a CI step fails if the two disagree. **The broken-PR test came out exactly as designed**: a `gofmt` violation, a type error and a Dockerfile typo turned Go, Frontend, End to end and Image red while **Security stayed green** — and that green is the control that makes the four reds mean something, because it shows the failures were targeted rather than a blanket collapse. *Merge pull request* was disabled and every check showed **Required**, which is the ruleset proving itself. The type error tripping two jobs is correct: the End to end job builds the application too. **Wall-clock is ~4 minutes cold and ~3 warm** against a ten-minute budget, because the jobs run in parallel. `main` is protected but deliberately does **not** require a pull request, so sessions still end with a direct push. |
| 2026-09-22 | 12-a | `.github/workflows/ci.yml` (five parallel jobs) and its README, `scripts/coverage-gate.sh`, `scripts/audit.sh`, `web/scripts/bundle-size.mjs`, `Dockerfile` + `.dockerignore`, osv-scanner added to the pinned tooling, and `make check \| audit \| coverage-gate \| bundle-size \| image` | **Split Part 12**: a workflow that has never run on a runner is a guess, so writing it and proving it are separate pieces of work. Every command it runs is verified locally — `make check` exits 0 — and every gate was watched to *fail* as well as pass. **osv-scanner pointed at `go.mod` is a false-positive machine**: it reads the `go` directive as the standard library's version and reported **25 stdlib advisories that govulncheck reports as zero**, because that directive is a minimum language version and not a toolchain. A gate that cries wolf twenty-five times is one people route around, so osv-scanner owns npm only and govulncheck owns Go. **The coverage gate needed two fixes before it was honest.** It summed duplicate profile blocks, which `-coverpkg` produces one per test binary — reporting `web` at 8.5% when it is 93.3%. And the measurement only means anything with Postgres running: `internal/store/repo` is 65.8% without it and 82.5% with it, so a gate run without the service container would fail for the wrong reason. **Six packages are below 80% today** and are listed in Current state; the next pull request touching one has to bring it up, which is the gate working as designed. **The bundle-size gate found a Part 9 chunking bug on its first run**: `manualChunks: { vendor: ["react", "react-dom"] }` names entry *specifiers*, and the application imports `react-dom/client` — so React ended up in the chunk labeled `tanstack` and `vendor` was 4 KB. The names were lying. Fixed with the function form. **The budget is at 93% in Phase 0**: 185 KB of 200, of which our own code is 11.5 KB. Phase 2 wants charts. **govulncheck v1.1.4 panics** on modern syntax (`unexpected expr: *ast.KeyValueExpr` out of x/tools v0.29.0); v1.8.0 is clean. **No third-party actions at all** — only `actions/*`, pinned release binaries with verified checksums, and a pinned gitleaks image — because an action is code running with the workflow's token. The image is 22.9 MB distroless and starts. **The first run on GitHub was four green out of five, and the red one was right.** `actions/setup-go` with `go-version-file` installs exactly the `go` directive — 1.26.0 — and govulncheck found **nineteen reachable standard-library vulnerabilities** in it, crypto/x509 panicking on a malformed certificate among them, every one fixed in 1.26.1. The `go` line is a floor for contributors, not an instruction to build with, and nothing in the repository had ever said what to build with. go.mod now carries `toolchain go1.26.8`, the workflow derives its version from that line so no version is duplicated into YAML, the Dockerfile pins the same patch, and a CI step fails if the two disagree. Confirmed clean under 1.26.8 locally before pushing. |
| 2026-09-22 | 11-b | The application shell — `AppShell` with a skip link and four labeled landmarks, `SideNav`, `Breadcrumbs`, `UserMenu`, the command palette on Ctrl/Cmd+K, one navigation model read by all three, five honest placeholder routes — plus Playwright against the built binary: 31 tests including axe over 16 page states | **A browser found two contrast failures that the Go pairing list had no entry for.** axe's `color-contrast` rule does not run in jsdom at all — no layout, no canvas — so this was the first time it had ever executed. An avatar's initials were `text-muted` on `surface-sunken` at 4.40:1, and an accent badge was 3.97:1 in the dark theme. Both pairings are now in `tokens_test.go`, which is the lasting fix: **a list of token pairs is only as good as the combinations somebody thought of**, and that is exactly why the browser pass is not redundant with the Go one. **Three traps in the E2E harness itself, each of which produced a convincing wrong answer.** Playwright starts `webServer` *before* `globalSetup`, so provisioning afterwards left the server holding an open descriptor on a deleted inode — it kept reading an empty database while the new one filled, and every login failed with "that email address and password do not match". Moving the cleanup to config module scope did not fix it either, because Playwright evaluates the config once per worker and it deleted the database mid-run. globalSetup now owns provisioning *and* the server. Then switching theme and scanning immediately measured colors **mid-transition** — axe saw a link fading from light to dark and called it 2.29:1; `reducedMotion: "reduce"` fixed it, using a rule the stylesheet already had. **The application's own CSP blocked the test harness, which is the CSP working.** `addScriptTag` appends a real `<script>` and `script-src 'self'` refuses it, so axe would not load; `addInitScript` goes in over the debugging protocol instead, leaving the policy in force — and there is now a test asserting an injected script is still refused. **Radix's modal menu trips `aria-hidden-focus`**: it marks the page outside `aria-hidden` while the skip link and sidebar remain focusable. Focus is trapped so nobody could reach them, but the markup claims they are not there. The account menu is `modal={false}` now, which is what a menu should be anyway. **Cut and moved rather than dropped:** the change-password page and the endpoint it needs went to Part 15, because verifying a current password and revoking every *other* session is backend work and not shell work. **Still not run:** six manual audit items, all screen reader and rendering. Four of the original ten are now tests. |
| 2026-09-22 | 11-a | `src/i18n` (i18next, English complete, typed keys, direction from the locale, a dev-only RTL pseudo-locale), the login page on the design system, a pathless `authenticated` guard, `src/lib/redirect.ts`, `OfflineBanner`, `LocalePicker`, login/logout mutations and a central 401 handler; 28 new tests | **Split Part 11** — the door is a session, the room behind it is another. **The open-redirect rule is now enforced on both ends.** Part 8-b closed it on the server for the SSO `return` parameter; the client does its own redirect after a password login, and the server cannot vet that one. `safeDestination` repeats the rule and is tested against six hostile shapes — `//evil.example` and `/\\evil.example` are the ones that get past a check for a leading slash. **A real bug in my own helper, caught by a test I nearly did not write.** `currentDestination` concatenated the router's `hash`, which omits the leading `#`, turning `/?owner=me#chart` into `/?owner=mechart` — a destination that still looks plausible and still navigates, somewhere else. `window.location.hash` includes the `#` and the router's does not, and both shapes are now handled. **Every 401 is handled in one place**, not per call site, so a session that ends mid-visit sends the user to the login page with their destination intact whatever they were doing — and the cached session is nulled *first*, or the guard would read a stale session, let them back in, and bounce them again. **The organization field is revealed by the server, not guessed**: a single-organization instance never shows it, and a multi-organization one returns 422 naming the field, which is what makes it appear and take focus. Verified live, both branches. **i18n came first deliberately** — the expensive part of internationalization is never the library, it is the four hundred strings already written inline and the stylesheet full of `margin-left`. The catalog is the type, so a wrong key fails `tsc`. **Top-level `await` does not build**: Vite targets es2020, and raising the target for one line of syntax would have quietly dropped browsers the NFRs still name — `initI18n().then` instead. **The Go suite got killed mid-run and it was memory, not a flake**: Storybook was still serving from an earlier turn while `-race` tests hashed Argon2 at 64 MiB. Same family as Part 6-b's `-p 1`. Verified live against the built binary on SQLite: 401 wrong password, 200 + `HttpOnly` cookie, 204 logout, 401 after, 422-with-field for the ambiguous email, 200 once the organization is named. **Not verified: anything visual.** No browser ran. Part 11-b's Playwright is what closes that. |
| 2026-09-22 | 10-b | Dialog, DropdownMenu, Select, Popover, Tooltip, Tabs, Toast and the command palette (Radix + cmdk), 34 more stories, `web/src/ui/keyboard.test.tsx` (21 tests driving the keyboard with user-event), `web/vitest.setup.ts`, `TestNothingRemovesTheFocusIndicator`, and [docs/design/keyboard-audit.md](docs/design/keyboard-audit.md) | **The axe suite had a hole big enough to drive this whole part through.** It scanned `render()`'s container — and every overlay portals to the end of `document.body`, so the scan would have covered the triggers and none of the content. Moved to `document.body` before writing a single overlay, which is the only reason the numbers below mean anything. **Found and fixed a real bug in cmdk**: it sets `aria-activedescendant` only when the selection *changes*, so it is missing when the palette opens and missing again whenever a search narrows to one result — precisely when a screen reader user needs to be told what is active, and precisely where everyone else can see the highlight. Reproduced against cmdk on its own. axe cannot catch it, because an absent attribute is not an invalid one; the keyboard suite did, and the fix is mutation-verified. **Two of my own findings were wrong and the code was right.** I read cmdk's minified source, concluded it put `aria-activedescendant` on the listbox rather than the input, and wrote a mirroring hook for a bug that did not exist; reading further showed it sets it on the input correctly, and the real bug was the *timing*. Then I asserted Tabs gives `tabindex=0` to the selected tab — it gives it to the *list* and forwards focus, which is equally correct and which a behavioral assertion would not have cared about. Rewrote it to assert one Tab in and one Tab out. **A ref that is always null.** The palette's fix did nothing at first because Radix's portal mounts the dialog's contents in a *later* commit, so a `useRef` was still null when an effect keyed on `open` ran, and nothing ever looked again. Callback refs held in state, so the effect fires when the elements exist. **jsdom needed four stubs before any overlay would mount** — `ResizeObserver`, `scrollIntoView`, pointer capture, `matchMedia`. None of them returns a plausible measurement on purpose: a fake that did would let a test assert a position no browser would produce. **One criterion is not met, and is marked `[~]` rather than ticked.** Whether the focus ring is *drawn* cannot be checked by anything here, because jsdom paints nothing. The manual pass is written out as 10 items in the audit document and has not been run; Part 11's Playwright is what turns most of it into code. 106 story scans, 130 axe tests, 21 keyboard tests, 0 violations. |
| 2026-09-22 | 10-a | `web/src/ui` — 27 components in 16 modules on Radix and the design tokens, one barrel export; Storybook 9 with the a11y addon and a theme toolbar; 72 stories; `web/tokens_test.go` (WCAG contrast, in Go), `web/src/ui/a11y.test.tsx` (axe over every story, 88 tests), `web/stories_test.go` (coverage guard); `make web-test` / `storybook` / `storybook-build` | **Accessibility is checked in two halves and each half found a real bug the other could not see.** The Go contrast harness parses the token file and computes WCAG relative luminance for the 20 pairings the components actually produce — no browser, no `node_modules`, runs in `make test` anywhere — and it found **nine real failures in the tokens Part 9 shipped**: the light status colors, `border-strong` in both themes, and white-on-blue for a primary button's label. I fixed the tokens rather than the thresholds, and `TestDarkFallbackMatchesTheExplicitTheme` then caught me updating only one of the two dark blocks. Checking the *tokens* rather than a rendering is the point: a scan covers the components someone wrote a story for, the tokens cover every component that will ever exist. **The axe half caught `Button`'s `asChild` throwing on every render** — the spinner beside `{children}` gives Radix's `Slot` two children to merge onto, so the feature was broken from the moment it was written and nothing had rendered it until a story did. `Slottable` fixes it. **Scope was rules, not taste:** axe is scoped to the WCAG 2.1 A/AA tags, because best-practice rules include `region`, which wants content inside a landmark — a property of a page, not of a button — and asserting it on isolated components teaches people to ignore the output. `color-contrast` is disabled explicitly, with the comment saying where the check actually lives. **`web/stories_test.go` keeps the suite honest**, also in Go: a component with no stories, or a story file missing from the import list, fails the build. Mutation-verified both ways. **Storybook 10 is unusable on Node 20.16 and lies about it** — it prints "you need Node 20.19+" and **exits 0**, so the build reports success and produces nothing; a CI job checking only the exit status would go green forever. Pinned to 9.1.20, which needs all four packages reinstalled together (npm ERESOLVEs on an in-place bump). That is now **two** majors held back by Node 20.16 — Vite and Storybook — and Part 12 pinning Node ≥ 20.19 unblocks both at once. **Split Part 10**: the in-flow components are a session, the overlays are another, and the overlays are where the hard work is (focus trapping, escape, restoring focus, scroll lock) and where jsdom stops helping. **Not verified this session:** keyboard navigation by hand. 10-b owns it, for all 27 components, and says so. |
| 2026-09-21 | 9 | Vite 6 + React 19 + TS 5.9 + Tailwind 4 scaffold, TanStack Router and Query, a typed API client over the generated schema, design tokens as runtime CSS variables, `web/embed.go` with SPA fallback and caching, a document CSP, `make all` / `web-*` / `dev` targets, and 12 Go tests over the serving rules | **Two bugs shipped and were found by reading the compiled output, not the page.** `<alpha-value>` is Tailwind 3 syntax; Tailwind 4 emits it verbatim, browsers discard the whole declaration, and every themed utility silently did nothing — no error anywhere. And the theme bootstrap was an inline `<script>`, which the shell's `script-src 'self'` blocks outright, so the stored theme preference was never applied. Both are now mutation-verified tests. **A test of mine was wrong twice in one session:** first it passed against a mutation that does not actually break the property (`@theme` vs `@theme inline` — both preserve the indirection, contrary to my comment), then the strengthened version failed on a *clean* build because Tailwind 4's own `@property` rules legitimately contain `syntax:"<length>"`. Narrowed to the literal `<alpha-value>`. **Node 20.16 turned out to be the binding constraint on the whole toolchain.** Vite 7/8 and plugin-react 5/6 all need `^20.19 \|\| >=22.12`; npm skips the mismatched optional native binding *silently*, so the failure reads as an npm bug rather than a Node one. Pinned to Vite 6 — which is what ADR-0002 chose anyway — with `engines` declared so the next person gets a legible warning, and a note for Part 12 to pin Node in CI. The Go tests run against a synthetic asset tree, so they pass on a clean checkout where no frontend has been built, which is exactly the case they most need to protect. |
| 2026-09-21 | 8-b | `/auth/oidc/{provider}/{start,callback}`, the public provider list, admin CRUD for providers gated on `manage_organization`, `auth.Service.StartSession` shared with password login, `pivot admin add-provider`, `server.baseURL`, spec + TS client, and an opt-in Keycloak conformance test | **Discovery ran against Google's real OIDC issuer, live.** The Keycloak container test exists and skips unless `PIVOT_TEST_KEYCLOAK_URL` is set — the image is a large pull, and a suite that needs a container is a suite people stop running — but the live check turned out stronger than the plan: a complete authorization request against a genuinely independent implementation, with S256 challenge, nonce, state and `scope=openid` all present. **Added an open-redirect guard that was not in the plan.** The post-login `return` path accepted anything; `//evil.example` and `/\evil.example` both redirect off-site in real browsers, and an open redirect on the login route is what makes a phishing link convincing. Only a same-site absolute path is honored now, mutation-verified. **`SameSite=Lax` on the flow cookie is load-bearing, not a default.** Strict would withhold it on the callback, which arrives as a top-level navigation from the provider's origin — every login would break. A test asserts Lax specifically. **Session creation was factored, not duplicated:** SSO calls the same `StartSession` the password path does, so there is one expiry policy rather than two, and only one of two would have been covered by the tests that matter. Mutation-verified twice: deleting the state check and deleting the protocol-relative guard each fail tests by name. gosec flagged the start redirect as a taint-analysis open redirect; it is not — the destination is the configured provider's own discovered endpoint — and the nolint says why rather than just silencing it. |
| 2026-09-21 | 8-a | `internal/oidc` (discovery with caching, PKCE, ID token verification, claim mapping), schema v5 `identity_providers` + `federated_identities`, `IdentityProviderRepo`, JIT provisioning with group and attribute sync, and an in-process identity provider that signs real RS256 tokens | **Split Part 8** — the protocol and provisioning domain is a session, the endpoints and Keycloak conformance are another. **A test of mine found a real hole in my own design.** I asserted that a recycled email address must not hand over the original account; it did, because my provisioning linked by email unconditionally. Closing the front door (match on `sub`) while leaving the side door open (link on email) is worth exactly nothing. Fixed by making `link_by_email` **opt-in per provider and off by default**, and requiring `email_verified` even when it is on. Two new tests pin the default down. The migration was edited in place rather than amended, because it had not been committed or applied anywhere. Also added email sync for returning users, which is safe precisely because identity was already settled by subject — the address moves, the account cannot. **The rejection paths are tested by breaking tokens on purpose:** bad signature, foreign issuer, replayed nonce, mismatched PKCE verifier. That is the whole reason for the in-process provider — a real Keycloak will not issue you a token it has broken — and it sits alongside 8-b's container test rather than replacing it. Signature verification is mutation-verified: turning it off fails two tests by name. `go-oidc` was chosen over hand-rolling because ID token verification is not something to hand-roll; it costs two direct requires. 12 Postgres subtests, 0 skips. |
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
