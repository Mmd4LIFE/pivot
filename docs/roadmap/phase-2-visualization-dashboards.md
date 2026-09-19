# Phase 2 — Visualize & Dashboard

**Months:** M4–M7 · **Effort:** 62 ew · **Team:** 6 · **Release:** **v0.3**

---

## Goal

Open the product to everyone who doesn't write SQL. A no-code query builder, a serious
visualization library, and dashboards good enough that a team runs its weekly review on
one.

This is the largest phase before Flows, and it's where Pivot either feels professional or
feels like a side project. The visualization layer is the most visible surface in a BI
tool, and it's judged in the first ten seconds.

## Exit criteria

- [ ] A user with no SQL knowledge builds a grouped, filtered, aggregated chart unassisted
- [ ] 30+ chart types render correctly across light/dark and at print resolution
- [ ] A 20-card dashboard loads with p95 under 2 seconds on warm cache
- [ ] Cross-filtering propagates across cards in under 300ms
- [ ] Every chart is keyboard navigable and exposes an accessible data table
- [ ] 3 external teams run a real recurring meeting from a Pivot dashboard

---

## 1. Visual query builder

| ID | Feature | Pri | Size |
|---|---|---|---|
| P2-QB-001 | Source picker (table or saved question) with search | P0 | M |
| P2-QB-002 | Column selection with reorder | P0 | M |
| P2-QB-003 | Filter builder with type-aware operators | P0 | L |
| P2-QB-004 | Aggregations: count, sum, avg, min, max, median, percentile, distinct, stddev | P0 | M |
| P2-QB-005 | Group-by with date truncation (hour → year) | P0 | M |
| P2-QB-006 | Sort and limit | P0 | S |
| P2-QB-007 | Joins with inferred relationships and manual override | P0 | L |
| P2-QB-008 | Custom expression editor with a formula language | P0 | XL |
| P2-QB-009 | Custom columns (computed, persisted on the question) | P0 | M |
| P2-QB-010 | Relative date filters ("last 30 days", "this quarter to date") | P0 | M |
| P2-QB-011 | Nested queries (build on a saved question) | P1 | M |
| P2-QB-012 | Notebook mode: multi-step visual pipeline | P1 | L |
| P2-QB-013 | View and edit the generated SQL, one-way handoff to the SQL editor | P0 | M |
| P2-QB-014 | Summarize sidebar for fast re-aggregation | P1 | M |
| P2-QB-015 | Drill-through from any result cell | P1 | L |

**The expression language (P2-QB-008) is an XL for a reason.** It needs a parser, a type
system, a function library (~80 functions across math, string, date, logic, and
aggregation), dialect-aware compilation, an autocompleting editor, and error messages a
non-programmer can act on. Underestimating this is the most common way BI query builders
end up feeling toy-like. It also becomes the foundation for metric definitions in
[Phase 3](phase-3-semantic-layer.md), so the investment compounds.

**P2-QB-013 is one-way by design.** Visual → SQL works; SQL → visual does not. Round-trip
editing requires parsing arbitrary SQL back into a structured query, which fails on
anything interesting and produces a confusing, lossy experience. Metabase made this choice
and it was correct.

## 2. Chart library

Built on Apache ECharts with a Pivot abstraction layer, so the underlying library stays
replaceable and chart configuration is portable.

| ID | Group | Charts | Pri | Size |
|---|---|---|---|---|
| P2-VIZ-001 | Core | Bar, column, line, area, combo | P0 | L |
| P2-VIZ-002 | Core | Stacked and 100% stacked variants | P0 | M |
| P2-VIZ-003 | Core | Pie, donut | P0 | S |
| P2-VIZ-004 | Core | Scatter, bubble | P0 | M |
| P2-VIZ-005 | Core | Table (with in-cell formatting) | P0 | M |
| P2-VIZ-006 | Single value | Big number, with trend and comparison | P0 | M |
| P2-VIZ-007 | Single value | Gauge, progress, bullet | P1 | M |
| P2-VIZ-008 | Distribution | Histogram, box plot, violin | P1 | M |
| P2-VIZ-009 | Comparison | Waterfall, funnel, pyramid | P1 | M |
| P2-VIZ-010 | Relationship | Heatmap, correlation matrix | P1 | M |
| P2-VIZ-011 | Composition | Treemap, sunburst, icicle | P1 | M |
| P2-VIZ-012 | Flow | Sankey, chord, network graph | P2 | L |
| P2-VIZ-013 | Time | Timeline, Gantt, calendar heatmap | P2 | M |
| P2-VIZ-014 | Geo | Choropleth (country, state, custom GeoJSON) | P1 | L |
| P2-VIZ-015 | Geo | Point, cluster, heatmap, hexbin via deck.gl | P1 | L |
| P2-VIZ-016 | Statistical | Trend line, regression, confidence band | P1 | M |
| P2-VIZ-017 | Specialized | Radar, parallel coordinates, word cloud | P2 | M |
| P2-VIZ-018 | **Pivot table** | Rows, columns, measures, subtotals, expand/collapse | P0 | XL |
| P2-VIZ-019 | Specialized | Cohort / retention grid | P2 | M |
| P2-VIZ-020 | Specialized | Sparkline and inline micro-charts | P1 | S |

**The pivot table (P2-VIZ-018) is XL and is the feature most likely to win a deal.**
Excel-literate business users evaluate a BI tool by whether its pivot table is real: nested
row and column groups, multiple measures, subtotals at every level, collapse and expand,
drill-to-detail, conditional formatting, and export that preserves structure. Most
open-source BI tools ship something that looks like a pivot table and isn't. This is
built on TanStack Table with custom virtualization because no off-the-shelf component does
this without an enterprise license.

### Chart configuration

| ID | Feature | Pri | Size |
|---|---|---|---|
| P2-VCF-001 | Axis configuration: scale, range, ticks, log, dual-axis | P0 | M |
| P2-VCF-002 | Series styling: color, line style, marker, width | P0 | M |
| P2-VCF-003 | Number, date, and currency formatting with locale support | P0 | M |
| P2-VCF-004 | Conditional formatting (color scales, icons, data bars, rules) | P0 | L |
| P2-VCF-005 | Legend placement, interaction, and visibility toggles | P0 | S |
| P2-VCF-006 | Tooltip customization with templating | P1 | M |
| P2-VCF-007 | Data labels with collision avoidance | P1 | M |
| P2-VCF-008 | Reference lines, bands, and annotations | P1 | M |
| P2-VCF-009 | Color palettes: qualitative, sequential, diverging; org-level defaults | P0 | M |
| P2-VCF-010 | Goal lines and target comparison | P1 | S |
| P2-VCF-011 | Null and zero handling policy per series | P1 | S |
| P2-VCF-012 | Smart defaults: chart type and encoding suggested from result shape | P0 | L |
| P2-VCF-013 | **AI: describe a chart change in words, apply it** | P2 | M |

**P2-VCF-012 is the difference between a tool and a toy.** When a query returns a date
column and a numeric column, the default should be a line chart with the date on X — not an
empty configuration panel. Smart defaults use the semantic types from
[P1-CAT-006](phase-1-connect-and-query.md#3-schema-catalog). Users should configure charts
to *refine* a good default, not to escape a bad one.

**Accessibility is built into the chart abstraction, not each chart.** The Pivot chart
layer emits ARIA structure, keyboard focus order across data points, and an equivalent
data table for every visualization. Palettes are validated for contrast and
colorblind-safety, and no chart encodes meaning by color alone without a redundant
channel.

## 3. Dashboards

| ID | Feature | Pri | Size |
|---|---|---|---|
| P2-DASH-001 | Responsive grid layout with drag, drop, and resize | P0 | L |
| P2-DASH-002 | Card types: chart, text/markdown, image, iframe, divider, spacer | P0 | M |
| P2-DASH-003 | Tabs within a dashboard | P0 | M |
| P2-DASH-004 | Independent per-card loading with skeletons | P0 | M |
| P2-DASH-005 | Auto-refresh with a configurable interval | P0 | S |
| P2-DASH-006 | Full-screen and TV/kiosk mode | P1 | S |
| P2-DASH-007 | Mobile-responsive layout with a separate breakpoint arrangement | P0 | L |
| P2-DASH-008 | Dashboard-level theming | P1 | M |
| P2-DASH-009 | Duplicate, template, and "save as" | P1 | S |
| P2-DASH-010 | Version history with restore | P1 | M |
| P2-DASH-011 | Print and PDF layout mode | P1 | M |
| P2-DASH-012 | Card-level caching policy override | P1 | S |

### Interactivity

| ID | Feature | Pri | Size |
|---|---|---|---|
| P2-INT-001 | Dashboard filters (date, list, search, numeric range, boolean) | P0 | XL |
| P2-INT-002 | Filter-to-card wiring with auto-mapping | P0 | L |
| P2-INT-003 | Cross-filtering: click a chart element to filter the dashboard | P0 | L |
| P2-INT-004 | Drill-down through a configured hierarchy | P0 | L |
| P2-INT-005 | Drill-through to underlying rows | P0 | M |
| P2-INT-006 | Click actions: navigate, open URL, update filter, custom | P1 | M |
| P2-INT-007 | Filter state encoded in the URL for shareable views | P0 | M |
| P2-INT-008 | Linked dashboards with filter pass-through | P1 | M |
| P2-INT-009 | Cascading filters (one filter narrows another's options) | P1 | L |
| P2-INT-010 | Saved filter presets per user | P2 | M |
| P2-INT-011 | Time-range comparison (vs. previous period, vs. last year) | P1 | L |

**P2-INT-001 is XL because filters are where dashboard complexity concentrates.** A filter
must resolve its option list (often via its own query), map to different columns on
different cards, handle defaults, persist in the URL, cascade, and stay fast. Filters are
also the most common source of "the dashboard is slow" — a filter dropdown that runs
`SELECT DISTINCT` on a billion-row table is a ubiquitous BI failure. Option lists are
cached, sampled, and search-driven above a cardinality threshold.

### Dashboard performance

| ID | Feature | Pri | Size |
|---|---|---|---|
| P2-DPF-001 | Parallel card execution with a concurrency ceiling | P0 | M |
| P2-DPF-002 | Viewport-priority loading (visible cards first) | P0 | M |
| P2-DPF-003 | Shared-subquery detection across cards | P1 | L |
| P2-DPF-004 | Background cache warming for scheduled dashboards | P1 | M |
| P2-DPF-005 | Per-card performance diagnostics in the UI | P1 | M |
| P2-DPF-006 | Progressive rendering for large result sets | P1 | M |

**P2-DPF-003** is where the warehouse bill gets cut. Twenty cards on a dashboard commonly
issue twenty near-identical queries differing only in aggregation. Detecting the shared
base query, running it once, and deriving the rest locally via DuckDB turns 20 warehouse
queries into 1. This is a direct consequence of having a local compute engine and is
something Metabase structurally cannot do.

## 4. Sharing & collaboration (foundations)

| ID | Feature | Pri | Size |
|---|---|---|---|
| P2-SHR-001 | Share link with permission inheritance | P0 | M |
| P2-SHR-002 | Public links with optional expiry and password | P1 | M |
| P2-SHR-003 | Export dashboard to PDF and PNG | P0 | M |
| P2-SHR-004 | Export all card data to Excel with one sheet per card | P1 | M |
| P2-SHR-005 | Embed via iframe with a signed token | P1 | M |
| P2-SHR-006 | Collection organization (folders, nesting, move) | P0 | M |
| P2-SHR-007 | Favorites, recents, and a personal home page | P0 | M |
| P2-SHR-008 | Global search across questions, dashboards, and tables | P0 | L |

Full permissions, verified content, and comments land in
[Phase 4](phase-4-collaboration-governance.md). Phase 2 ships the sharing mechanics that
make the product usable by a team, with a simple inherited permission model.

---

## Technical notes

### The chart abstraction layer
No component imports ECharts directly. All charts go through a Pivot chart spec — a
declarative, serializable description of data mapping, encoding, and style. This gives us
four things: the renderer stays swappable, chart configs are portable and diffable,
server-side rendering for PDF/email uses the same spec, and the custom-viz plugin API in
Phase 8 has a contract to target.

### Server-side rendering
PDF export, email subscriptions (Phase 5), and Slack unfurls need charts rendered without
a browser session. A headless Chromium worker renders the same React chart components from
the same spec. Rejected: a separate server-side chart library, which guarantees the email
chart eventually looks different from the dashboard chart.

### Client-side aggregation
Once a result set is in the browser as Arrow, re-aggregation, sorting, and cross-filtering
happen locally in a Web Worker rather than round-tripping to the server. This is what makes
cross-filtering feel instant. Above a row threshold (default 100k), operations fall back to
server-side execution.

---

## Explicitly deferred

| Deferred | To | Why |
|---|---|---|
| Custom visualization plugins | Phase 8 | Needs a stable chart spec and a plugin runtime |
| Real-time streaming charts | Phase 9 | Different architecture; not core to BI |
| Comments and annotations | Phase 4 | Belongs with the collaboration model |
| Scheduled dashboard delivery | Phase 5 | Needs the delivery infrastructure |
| Semantic-layer-driven charts | Phase 3 | Charts query tables in Phase 2, models in Phase 3 |

---

## Risks

| Risk | Mitigation |
|---|---|
| The expression language becomes a language design project | Ship a fixed function list, no user-defined functions, no control flow. Grammar frozen at phase start |
| Pivot table performance collapses on wide results | Virtualize both axes; cap materialized cells; server-side pivot above a threshold |
| Dashboard performance is unacceptable with real data volumes | Performance budgets enforced in CI from week 1, tested against a 50-card reference dashboard |
| Chart breadth consumes the phase and dashboards get shortchanged | P0 charts only until dashboards hit their exit criteria; P1/P2 charts are the flexible scope |
| Mobile layout is treated as an afterthought | Mobile is a P0 exit criterion with its own designs, not a media query added at the end |
