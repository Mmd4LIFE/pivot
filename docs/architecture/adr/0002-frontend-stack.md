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

### 2026-09-22 — Storybook pinned to 9, and where accessibility is actually checked

Part 10-a added the design system. Two things changed the shape of the
frontend toolchain.

#### Storybook 9, for the same reason Vite is 6

Storybook 10.6.0 installs cleanly on Node 20.16 and then refuses to run:

```
To run Storybook, you need Node.js version 20.19+ or 22.12+.
```

Worth noting how it refuses — it prints that and **exits 0**, so `npm run
storybook:build` reports success and produces nothing. A CI job that only
checked the exit status would have gone green on a build that never happened.

Storybook 9.1.20 has no such floor and builds and serves the whole gallery on
the installed Node. It is pinned across all four packages (`storybook`,
`@storybook/react`, `@storybook/react-vite`, `@storybook/addon-a11y`), which
npm requires: bumping them one at a time gives an ERESOLVE on the peer range,
and the packages have to be removed and reinstalled together to move majors.

So the frontend is now held one major behind in two places — Vite and
Storybook — by the same Node 20.16. Both are one-line bumps once **Part 12
pins a toolchain Node ≥ 20.19 in CI**, and that is now the single thing
blocking both.

#### Accessibility is checked in two halves, on purpose

Neither half is sufficient, and the split is not an accident of tooling.

**Color, in Go.** `web/tokens_test.go` parses `src/styles/tokens.css` and
computes WCAG relative luminance and contrast ratios for the 20 foreground /
background pairings the components actually produce, in both themes. It needs
no browser and no `node_modules`, so it runs in `make test` everywhere. It
found nine real failures in the tokens Part 9 shipped.

Checking the tokens rather than a rendering is the point: a scan of a page
covers the components someone wrote a story for, and the tokens cover every
component that will ever exist.

**Structure, in jsdom.** `src/ui/a11y.test.tsx` runs axe-core over every
story — 72 scans across 16 modules, 88 tests in all — scoped to the WCAG 2.1 A
and AA tags. Best
practice rules are excluded deliberately: `region` wants all content inside a
landmark, which is a property of a page and not of a button, and asserting it
on isolated components teaches people to ignore the output. `color-contrast`
is disabled explicitly because jsdom has no layout and no canvas; that is the
half the Go test owns.

It caught a real bug immediately: `Button`'s `asChild` threw on every render,
because the spinner beside `{children}` gave Radix's `Slot` two children to
merge onto. Nothing had rendered it until a story did.

`web/stories_test.go` keeps the suite honest — also in Go, also without
`node_modules`. It fails the build if a component has no story file, if a
story file has no component, or if a story file is missing from the suite's
import list.

A browser-based pass would add what jsdom cannot see: real focus order, real
computed styles, real assistive-technology behavior. That needs Playwright,
which **Part 11 installs anyway**, so the browser pass belongs there rather
than as a second install here.

### 2026-09-22 — cmdk, and what a browser-free accessibility check can and cannot reach

Part 10-b added the overlays. Two things are worth recording.

#### cmdk, for the command palette only

Radix has no combobox, and a filtered list driven by `aria-activedescendant` is
one of the easiest patterns in ARIA to get subtly wrong — "subtly wrong" here
meaning a screen reader reads nothing as the user arrows down. cmdk 1.1.1 is
the standard implementation, is built on Radix Dialog, and costs one direct
dependency.

It has a bug, found by the keyboard tests and reproduced against cmdk on its
own: **`aria-activedescendant` is set only when the selection changes.** So it
is absent when the palette opens — the first option is already highlighted and
nothing changed — and absent again whenever a search narrows to a single
result, because the selection stays where it was. Both are exactly the moments
the attribute is needed, and in both of them the highlight is plainly visible
to every sighted user.

`useActiveDescendant` in `CommandPalette.tsx` mirrors the DOM's `aria-selected`
onto the input. It reads the DOM rather than cmdk's state because the ids are
cmdk's, and deriving our own would mean two notions of "active" that have to be
kept in step.

The fix needed callback refs held in **state**, not `useRef`: Radix's portal
mounts the dialog's contents in a later commit, so a ref object is still null
when an effect keyed on `open` runs, and nothing ever looks at it again.

#### The scan target, and the limit of a browser-free check

`a11y.test.tsx` originally scanned `render()`'s container. Every overlay in the
design system portals to the end of `document.body`, so that scan covered the
triggers and none of the content — the half of a design system where
accessibility actually goes wrong. It now scans `document.body`.

`vitest.setup.ts` stubs `ResizeObserver`, `scrollIntoView`, pointer capture and
`matchMedia`. jsdom lays nothing out, so every popper-based component throws on
mount without them. None of the stubs returns a plausible measurement, and that
is deliberate: a fake that did would let a test assert a position no browser
would ever produce.

Which draws the line. In jsdom we can check names, roles, states, ARIA
validity, focus movement, focus trapping, key handling and live-region
politeness — everything that lives in the DOM. We cannot check that a focus
ring is *drawn*, where an overlay lands, or what a screen reader says out loud.
Those ten items are written out in `docs/design/keyboard-audit.md` for a person,
and **Part 11's Playwright install is what turns most of them into code.**

### 2026-09-22 — i18n from the start, and where the session boundary lives

Part 11-a put the application behind a login. Three decisions are worth
recording.

#### i18next before the strings, not after

`i18next` 26 and `react-i18next` 17, wired in Phase 0 with exactly one locale
translated. That looks premature and is not: the expensive part of
internationalizing an application is never the library, it is the four hundred
strings already written inline and the stylesheet built on `margin-left`. Both
are free today and neither is in Phase 3.

The catalog is TypeScript rather than JSON, and the catalog *is* the type —
`CustomTypeOptions` is built from it, so `t("auth.singIn")` fails `tsc` instead
of rendering the key onto the page. JSON was rejected for one reason: it cannot
carry a comment, and a translator needs to be told that "sign in" here is a
verb.

**Direction is part of the locale, not a setting.** `applyDirection` writes
`lang` and `dir` onto `<html>`, and `dir` is the single attribute every logical
property in the stylesheet resolves against — which is what makes the `ms-*`
and `ps-*` discipline in `src/ui` pay off as a one-attribute mirror rather than
a rewrite. A development-only pseudo-locale carries the English strings and
declares itself right-to-left, so the mirrored layout can be checked without
inventing translations nobody on this project can read; fabricated Arabic would
be worse than none, because it looks finished.

#### The guard is a layout route, and it is not the security boundary

`/login` hangs off the root and everything else hangs off a pathless
`authenticated` route, so a new page is protected by default and letting one
out is a deliberate edit. The opposite arrangement — a public tree with routes
opting in — relies on the next person remembering.

The check runs in `beforeLoad`, before a child route loads data. A guard in a
component's render happens *after* the requests it was supposed to prevent have
gone out, which is how a signed-out user generates a burst of 401s and a flash
of empty interface.

None of this is the security boundary. The server refuses every request without
a valid session cookie and would with these files deleted. What the guard buys
is not watching an empty shell fail to load, and remembering the destination.

#### The redirect parameter is attacker-controlled on both ends

ADR-0002 did not anticipate this, and it is the same bug Part 8-b fixed on the
server. The login page carries its destination in a search parameter, so anyone
can send a colleague a link to Pivot's own login page that bounces them
elsewhere the moment they authenticate — the victim really did land on the real
login page and really did sign in, which is what makes the link convincing.

`safeDestination` applies the same rule as `safeReturnPath` in
`internal/api/oidc.go`: a same-site absolute path, nothing else. Two copies on
purpose. The server cannot vet a redirect the client performs after a password
login, and the client cannot vet one the server performs on an SSO callback.
The rule is six lines; sharing it across the language boundary would cost more
than repeating it.
