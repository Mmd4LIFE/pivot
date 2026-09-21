# ADR-0002: React SPA with Vite, no SSR framework

**Status:** Accepted
**Date:** 2026-09-19

## Context

Pivot's frontend is an authenticated application with unusually demanding component needs:
virtualized grids over a million rows, forty-plus chart types, a drag-and-drop dashboard
layout engine, a node-based pipeline canvas, and a code editor with schema-aware
autocomplete.

It must also embed into the Go binary as static assets, because
[ADR-0001](0001-backend-language.md) commits to single-binary distribution.

## Decision

React 19 + TypeScript 5.6, built with Vite 6 to static assets, embedded in the Go binary
via `embed.FS` and served by the Go process. No Next.js, Remix, or any framework requiring
a Node runtime in production.

## Rationale

**React for ecosystem depth, not framework elegance.** Every hard component we need —
TanStack Table, ECharts, deck.gl, React Flow, CodeMirror, dnd-kit — has its most mature
version in React. For a product whose difficulty is concentrated in complex interactive
components, that matters more than the framework's abstractions. React is also what our
embedding customers use, which directly affects the Phase 8 SDK.

**SPA because SSR buys nothing here.** Pivot is behind a login. There is no SEO, no
first-paint-for-crawlers requirement, and no content to stream. What SSR would cost is
precise: a Node process in production, which breaks the single-binary commitment. Vite
builds static assets, Go embeds and serves them, and there is no Node anywhere at runtime.

## Alternatives considered

### Next.js
Excellent for content sites and marketing pages. For an authenticated SPA it adds a server
runtime, a build complexity tax, and framework opinions about routing and data fetching
that fight a query-heavy application. We will likely use it for the *marketing* site, which
is a separate deployment with genuinely different requirements.

### Vue / Svelte / SolidJS
All fine frameworks, some technically superior. Rejected on component ecosystem: we would
be building or porting the data grid, the chart bindings, the flow canvas, and the editor
integration. That is many engineer-months spent re-solving solved problems, and it would
make the embedding SDK less useful to the React-majority market.

### Server-rendered HTML (htmx, Go templates)
Genuinely appealing for the CRUD surfaces, and it would eliminate an entire build pipeline.
Rejected because the core of the product is not CRUD — it's a dashboard with cross-filtering
that must update twenty charts in 300ms, a drag-and-drop canvas, and a grid virtualizing a
million rows. Those need client-side state.

## Consequences

**Positive**
- No Node in production; one binary still holds
- The deepest component ecosystem for exactly our hard problems
- The embedding SDK is React-native, matching most host applications
- Vite's dev server makes iteration fast

**Negative**
- Client bundle size is a permanent discipline; enforced by CI budgets in the
  [NFRs](../../roadmap/non-functional-requirements.md#14-frontend-budgets)
- Initial load is slower than server-rendered HTML would be, mitigated by code splitting
- No progressive enhancement; JavaScript is required
- React's re-render model requires active memoization discipline in a data-heavy UI

**Neutral**
- Two build toolchains (Go and Vite), unified behind `make`
- Routing and data fetching are assembled from libraries rather than given by a framework

## Revisit if

- Bundle size cannot be kept within budget despite splitting
- The embedding market shifts decisively away from React
- We add a public, unauthenticated content surface where SEO matters

---

## Amendments

### 2026-09-21 — Versions pinned, and what the toolchain now requires of Node

Part 9 built the scaffold. Three things are worth recording, because each one
is a decision somebody will otherwise re-derive.

#### The stack, as actually pinned

| | ADR | Pinned | Why |
|---|---|---|---|
| Vite | 6 | **6.4.3** | as decided; see the Node constraint below |
| React | 19 | **19.3.0** | current |
| TypeScript | 5.6 | **5.9.3** | last 5.x |
| Tailwind | 4 | **4.3.3** | current |
| `@vitejs/plugin-react` | — | **4.7.0** | the last line supporting Vite 6 |

#### Vite 7 and 8 are gated on a Node upgrade, not on a decision

Vite 7 and 8 both require `node: ^20.19.0 || >=22.12.0`, as do
`@vitejs/plugin-react` 5 and 6. The development machine runs Node 20.16.0, so
they cannot be installed there at all.

The failure is worth describing because it is not obvious. Vite 8 builds with
rolldown, whose platform binary ships as an *optional* dependency carrying that
same engine constraint. npm skips an optional dependency whose engines do not
match — silently — and the build then dies with
`Cannot find module '../rolldown-binding.linux-x64-gnu.node'`, which points at
npm rather than at Node. `package.json` now declares `engines`, so the next
person gets an engine warning instead of a missing-binary stack trace.

Vite 6 is what this ADR chose, it is supported, and it works on the installed
Node today. Moving to 7 or 8 is a routine bump once the toolchain Node is
≥ 20.19 — **Part 12 should pin that version in CI**, which is the natural place
to make it a property of the project rather than of one machine.

TypeScript 7.0 exists and is the native rewrite. Deferred deliberately: Parts
10 and 11 add Storybook and Playwright, and taking a just-released compiler
rewrite at the same time is an avoidable risk for a cheap upgrade later.

#### Two bugs the scaffold shipped with, and the tests that now catch them

Both were invisible — no error, no failed request, just a feature quietly
absent — which is why each has a test rather than a comment.

`<alpha-value>` is Tailwind 3 syntax. Tailwind 4 emits it verbatim, so
`rgb(var(--pivot-surface) / <alpha-value>)` reached the browser, which
discards the whole declaration. Every themed utility did nothing. Found by
reading the compiled CSS, not by looking at the page.

The theme bootstrap was an inline `<script>`, and the application shell is
served with `script-src 'self'`, which blocks inline scripts. It is now
`/theme-init.js`, loaded render-blocking so it still runs before first paint.

The tests are in `web/embed_test.go`: the built CSS must contain
`var(--pivot-` and no `<alpha-value>`, and the shell must contain no inline
script at all.
