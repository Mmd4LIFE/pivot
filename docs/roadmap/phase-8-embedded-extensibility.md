# Phase 8 — Embed & Extend

**Months:** M19–M21 · **Effort:** 46 ew · **Team:** 12 · **Release:** **v1.8**

---

## Goal

Let other people build on Pivot. Embedded analytics that SaaS companies ship to their own
customers, a plugin system for custom visualizations and connectors, and a public API
complete enough that someone can build a product on it.

This phase changes the business model. Embedded analytics is where BI companies make
money, and it's the use case Metabase serves poorly and Looker prices out of reach.

## Exit criteria

- [ ] 3 design partners ship Pivot embedded in their own product to real customers
- [ ] An embedded dashboard loads in under 1.5s p95 with a cold browser cache
- [ ] A customer restyles Pivot to their brand with no forked code and no rebuild
- [ ] A third-party visualization plugin installs and renders without a Pivot release
- [ ] Multi-tenant embedding verified: tenant A cannot reach tenant B's data

---

## 1. Embedded analytics

| ID | Feature | Pri | Size |
|---|---|---|---|
| P8-EMB-001 | Signed-JWT embedding with user attributes for RLS | P0 | L |
| P8-EMB-002 | Embed a dashboard, question, or the full explorer | P0 | L |
| P8-EMB-003 | Locked parameters the embedded user cannot change | P0 | M |
| P8-EMB-004 | Interactive embedding with drill-down and filters | P0 | L |
| P8-EMB-005 | Static embedding for public content | P1 | M |
| P8-EMB-006 | Embedded self-service authoring | P1 | XL |
| P8-EMB-007 | Per-embed feature toggles (hide export, hide SQL, hide edit) | P0 | M |
| P8-EMB-008 | Embedded session lifecycle and token refresh | P0 | M |
| P8-EMB-009 | Cross-origin, CSP, and iframe security hardening | P0 | M |
| P8-EMB-010 | Per-tenant usage metering for embedded deployments | P1 | M |

**P8-EMB-006 is the premium capability.** Most embedded BI shows customers a fixed
dashboard. Letting *their* customers build their own questions — inside the host
application, scoped to their own data — is what companies actually want and what justifies
enterprise pricing. It's XL because every authoring surface must work in a constrained,
branded, permission-locked context.

**P8-EMB-001 carries the security weight of the phase.** The host application signs a JWT
asserting who the end user is and what attributes they carry; Pivot maps those to RLS
policies from [Phase 4](phase-4-collaboration-governance.md). A forged or replayed token is
a cross-tenant data breach, so: short expiry, required `jti` with replay detection,
per-tenant signing keys with rotation, and mandatory audience and issuer validation.

## 2. SDKs

| ID | Feature | Pri | Size |
|---|---|---|---|
| P8-SDK-001 | React SDK with `<PivotDashboard>`, `<PivotQuestion>`, `<PivotExplorer>` | P0 | L |
| P8-SDK-002 | Vanilla JS SDK for non-React hosts | P0 | M |
| P8-SDK-003 | Event API (filter changed, drill, error, load complete) | P0 | M |
| P8-SDK-004 | Imperative control (set filters, refresh, navigate) | P0 | M |
| P8-SDK-005 | Python SDK for the management API | P1 | M |
| P8-SDK-006 | Go SDK | P2 | S |
| P8-SDK-007 | CLI for CI/CD content deployment | P1 | M |
| P8-SDK-008 | Terraform provider | P1 | M |
| P8-SDK-009 | Server-side token-signing helpers in 4 languages | P0 | M |

**P8-SDK-009 exists because JWT signing is the step integrators get wrong.** Shipping
tested helpers for Node, Python, Go, and Ruby eliminates the most likely source of an
embedding security incident — a partner writing their own signing code.

## 3. White-labeling

| ID | Feature | Pri | Size |
|---|---|---|---|
| P8-WL-001 | Full theming via CSS custom properties (colors, fonts, radii, spacing) | P0 | L |
| P8-WL-002 | Logo, favicon, and product name replacement | P0 | S |
| P8-WL-003 | Custom chart palettes per tenant | P0 | M |
| P8-WL-004 | Custom domain with managed TLS | P1 | M |
| P8-WL-005 | Branded email templates | P1 | M |
| P8-WL-006 | Remove all Pivot branding (licensed) | P1 | S |
| P8-WL-007 | Theme builder UI with live preview | P1 | M |
| P8-WL-008 | Per-tenant theme resolution in multi-tenant deployments | P0 | M |

This works with no forked code because of the
[Phase 0 decision](phase-0-foundations.md#7-frontend-foundations) to express design tokens
as runtime CSS custom properties. A theme is configuration, not a build.

## 4. Plugin system

| ID | Feature | Pri | Size |
|---|---|---|---|
| P8-PLG-001 | Plugin architecture: manifest, lifecycle, versioning, sandbox | P0 | XL |
| P8-PLG-002 | **Custom visualization plugins** | P0 | XL |
| P8-PLG-003 | Custom connector plugins | P1 | L |
| P8-PLG-004 | Custom delivery channel plugins | P1 | M |
| P8-PLG-005 | Custom flow node plugins | P2 | L |
| P8-PLG-006 | Plugin permission model and capability declaration | P0 | M |
| P8-PLG-007 | Plugin registry and install UI | P1 | M |
| P8-PLG-008 | Local plugin development kit with hot reload | P1 | M |
| P8-PLG-009 | Plugin signing and verification | P1 | M |

**P8-PLG-002 resolves [open question #3](../architecture/tech-stack.md#13-open-questions).**
The recommendation is a **framework-agnostic contract**: a plugin exports a render function
receiving an Arrow table, a config object, and a container element. It may use React, D3,
or anything else. A React-specific API would be easier and would permanently couple plugin
authors to our framework version — an unacceptable constraint on a plugin ecosystem that
should outlive our frontend choices.

Visualization plugins run in a sandboxed iframe with a `postMessage` bridge. This costs
some performance and buys the guarantee that a third-party chart cannot read session
tokens or exfiltrate data.

## 5. Public API completeness

| ID | Feature | Pri | Size |
|---|---|---|---|
| P8-API-001 | Full CRUD coverage for every entity | P0 | L |
| P8-API-002 | Query execution API with Arrow and JSON responses | P0 | M |
| P8-API-003 | Semantic layer API (list metrics, execute a metric query) | P0 | M |
| P8-API-004 | Bulk operations and batch endpoints | P1 | M |
| P8-API-005 | Webhooks for content, alert, and flow events | P0 | M |
| P8-API-006 | Arrow Flight SQL endpoint for third-party BI tools | P1 | L |
| P8-API-007 | Interactive API docs and a sandbox | P0 | M |
| P8-API-008 | Documented rate limits with headers | P0 | S |
| P8-API-009 | API versioning and deprecation policy | P0 | S |
| P8-API-010 | Content-as-code: export/import an entire workspace | P1 | L |

**P8-API-006 is a strategic hedge.** Exposing the semantic layer over Arrow Flight SQL
means Tableau, Excel, Hex, and Jupyter can query Pivot's governed metrics directly. Rather
than fighting tools already in the building, Pivot becomes the metric authority *for* them.
It converts a competitive threat into a distribution channel.

**P8-API-010 enables real CI/CD for analytics.** A workspace serializes to files, commits
to Git, and deploys to another environment. Combined with the
[Phase 3 Git sync](phase-3-semantic-layer.md#7-versioning--git), the full analytics stack
— models, questions, dashboards, alerts, flows — becomes reviewable, versioned code.

## 6. Integrations

| ID | Integration | Pri | Size |
|---|---|---|---|
| P8-INT-001 | Slack app (search, query, unfurl, subscribe) | P0 | L |
| P8-INT-002 | Microsoft Teams app | P1 | M |
| P8-INT-003 | Notion and Confluence embeds | P1 | M |
| P8-INT-004 | Google Sheets add-on (pull governed metrics into a sheet) | P1 | L |
| P8-INT-005 | Excel add-in | P2 | L |
| P8-INT-006 | Jupyter / Python client for the semantic layer | P1 | M |
| P8-INT-007 | VS Code extension for semantic layer authoring | P2 | M |
| P8-INT-008 | Zapier / Make connector | P2 | M |
| P8-INT-009 | MCP server exposing Pivot to AI coding agents | P1 | M |

**P8-INT-004 is a Trojan horse worth building.** Everyone exports to spreadsheets. An
add-on that pulls *governed metrics* into Sheets — live, permission-aware, refreshable —
means the spreadsheet stops being where governance goes to die. It directly attacks the
shadow-spreadsheet problem named in
[the vision's success criteria](../vision.md#8-what-success-looks-like).

**P8-INT-009** exposes Pivot's semantic layer over the Model Context Protocol, so an AI
coding agent can query governed business metrics as a tool. Low effort, and it puts Pivot
where AI-assisted development is heading.

---

## Technical notes

### Embedded performance is a different problem
An embedded dashboard inside someone else's app competes with their page load, and they
will measure it. Budget: 1.5s p95 to first meaningful render, cold cache. Requires an
embed-specific bundle (no admin UI, no authoring code, no unused chart types), aggressive
code splitting, preconnect hints, and a token-exchange flow that doesn't add a round trip.
The embed bundle has its own CI size budget, separate from the main app.

### Multi-tenant isolation under embedding
Embedding is where multi-tenancy gets real: one Pivot instance serving many host
customers, each with their own end users. Isolation must hold at every layer — metadata
(Phase 0 tenant scoping), query (Phase 3 compiler), cache (Phase 4 policy partitioning),
and now rendering and theming. The Phase 8 test suite includes explicit cross-tenant
attack scenarios.

### Plugin security posture
Plugins are third-party code. Visualization plugins are sandboxed in an iframe with no
credentials. Connector and flow-node plugins run server-side and therefore require
capability declaration, signing, and admin approval before install. We will not ship a
one-click marketplace for server-side code.

---

## Explicitly deferred

| Deferred | To | Why |
|---|---|---|
| Public plugin marketplace with payments | Post-v2.0 | Registry first; commerce needs a community |
| Mobile SDK (native iOS/Android embedding) | Phase 10 | Web embedding covers the demand |
| GraphQL API | — | Revisit only if embedding partners demand it |
| Plugin-authored UI surfaces beyond viz | Post-v2.0 | Large security surface, unclear demand |

---

## Risks

| Risk | Mitigation |
|---|---|
| Embedded token forgery causes a cross-tenant breach | Per-tenant keys, short expiry, replay detection, official signing helpers, external pentest as an exit gate |
| Embedded bundle size makes load times unacceptable | Separate bundle with its own CI budget; measured against real partner apps |
| The plugin API ossifies before it's right | Explicitly v0 and unstable for two minor releases; breaking changes allowed and announced |
| Self-service embedded authoring is far larger than estimated | P1, and the first thing cut. Interactive embedding without authoring still ships a complete phase |
| Public API surface becomes an unbreakable compatibility burden | Versioned from day one with a published 12-month deprecation policy |
