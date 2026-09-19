# Security Model

**Status:** Proposed · **Last reviewed:** 2026-09-19

How Pivot decides who can see what, and why the design makes certain failures impossible
rather than merely unlikely.

---

## 1. The central principle

> **Authorization is enforced in the semantic query compiler. There is no other path to
> data.**

Every data request — from the web UI, the public API, a scheduled alert, a flow, an
embedded iframe, a PDF export, or the AI analyst — converges on one component that applies
the requesting identity's row filters and column masks before emitting SQL.

This is not a convention enforced by code review. It's structural: connectors accept
compiled query objects, not SQL strings, and only the compiler can produce them.

The payoff is that adding a new surface cannot introduce an authorization bypass. When
Phase 7 added an AI analyst, it inherited every RLS policy automatically — because the only
thing the AI can do is hand a semantic query to the same compiler.

---

## 2. Authentication

### Methods

| Method | Use | Notes |
|---|---|---|
| Email + password | Small deployments | Argon2id (m=64MB, t=3, p=4) |
| OIDC | Primary for organizations | Auth Code + PKCE, auto-discovery |
| SAML 2.0 | Enterprise IdPs | SP-initiated and IdP-initiated |
| API key | Automation | Scoped, prefixed, hashed, expiring |
| OAuth 2.0 client credentials | Service-to-service | Phase 8 |
| Signed JWT | Embedding only | Short-lived, per-tenant key |

### Sessions are server-side, deliberately

Browser sessions are opaque tokens referencing server state, not JWTs.

A stateless JWT cannot be revoked before expiry. "Remove this person's access" must mean
*now* — when someone is terminated, when a laptop is stolen, when a key leaks. A 15-minute
JWT lifetime is a 15-minute window of unauthorized access, and shortening it trades the
problem for a refresh-token problem.

Cookies are `HttpOnly`, `Secure`, `SameSite=Lax`, with a configurable idle and absolute
timeout. Session revocation is immediate and propagates across nodes via Valkey.

JWTs appear in exactly one place — embedding — where the token is short-lived, scoped to a
single resource, and replay-protected.

### Brute force and enumeration

Rate limiting per IP and per account, progressive lockout, constant-time credential
comparison, and identical responses for "user not found" and "wrong password". Failed
attempts are audited and can trigger alerts (P4-AUD-008).

---

## 3. Authorization

### The relationship model

OpenFGA answers `can(user, action, object)` over a relationship graph. The Pivot model:

```
organization
  └─ member, admin

group
  └─ member, parent (another group)

collection
  ├─ viewer, editor, manager
  └─ parent (another collection) → permissions inherit down

dashboard / question
  ├─ viewer, editor, manager
  └─ parent (collection) → inherits unless overridden

semantic_model
  ├─ viewer, editor
  └─ query (can run queries against it)

connection
  ├─ viewer (can see it exists)
  ├─ query (can run builder queries)
  └─ native_query (can run arbitrary SQL)  ◄── separate, deliberately
```

### Why relationships rather than a permissions table

BI permissions are recursive: nested collections, nested groups, inheritance with
overrides, and objects whose accessibility depends on other objects (a dashboard card
querying a model the viewer can't see must be denied even when the dashboard is shared).

Expressed as SQL, this is a recursive CTE nobody can debug or reason about. Expressed as a
relationship graph, it's declarative, testable, and explainable — which is what makes the
permission debugger (P4-PRM-008) possible at all.

### Native SQL is a distinct permission

A user who can write arbitrary SQL against a connection can read anything that connection's
credentials can reach. **Semantic-layer RLS does not and cannot constrain raw SQL.**

So `native_query` is a separate grant, the UI states this plainly when granting it, and the
documentation recommends least-privilege database credentials per connection. Pretending
otherwise would be the most dangerous thing in this document.

### Download is a distinct permission

"Can view an aggregate on screen" and "can export the underlying rows to a spreadsheet that
leaves the building" are different decisions. They're modeled as different permissions.

### Failing closed

If OpenFGA is unreachable, Pivot denies all access. An authorization system that fails open
is worse than none, because it creates a false belief that access is controlled. This is
listed as a total-outage failure mode in
[system-design.md](system-design.md#7-failure-domains) and is addressed with HA, not with
degradation.

---

## 4. Row-level security

### How a policy becomes a predicate

```yaml
# Attached to a semantic model
rls_policies:
  - name: regional_access
    type: attribute
    condition: "{{ model.region }} = {{ user.region }}"

  - name: manager_hierarchy
    type: lookup
    lookup_table: employee_hierarchy
    match: "{{ model.employee_id }} = {{ lookup.employee_id }}"
    where: "{{ lookup.manager_id }} = {{ user.employee_id }}"
```

At compile time:

1. Resolve the user's attributes (from IdP claims, SCIM sync, or manual assignment).
2. Evaluate which policies apply to this user and this model.
3. Compile each into a SQL predicate, with values **bound as parameters**, never
   interpolated.
4. Inject predicates into the generated query, at the correct nesting level so they cannot
   be defeated by an outer aggregation.
5. Hash the resolved policy set into the cache key.

### The failure modes this design prevents

| Attack | Why it fails |
|---|---|
| Craft a query that skips the filter | Only the compiler emits SQL, and it always injects |
| Read another user's cached result | Cache key includes the policy-set hash |
| Export bypasses RLS | Export runs the same compiled query |
| A subscription emails unfiltered data | Rendering is per policy set, not per author |
| AI returns restricted rows | AI emits a semantic query; the compiler applies policies |
| Injection via an attribute value | Attribute values are bound parameters |
| Widen scope via an outer query | Predicates are injected at the innermost safe level |

**The one thing it does not prevent** is a user with `native_query` on a connection whose
credentials are over-privileged. That's a deployment decision, and it's documented as such.

### Policies are tested code

RLS policies are declarative assertions in CI:

```yaml
tests:
  - user: { region: "EU" }
    model: orders
    expect_rows: 1204
  - user: { region: "US" }
    model: orders
    expect_rows: 3891
  - user: {}                      # no region attribute
    model: orders
    expect_rows: 0                # default deny
```

A policy change that breaks isolation fails the build rather than reaching production.

---

## 5. Column masking

Applied in the same compile step as RLS.

| Type | Behavior | Use |
|---|---|---|
| `redact` | Replace with a constant | Free-text with PII |
| `hash` | Deterministic hash | Join-able but unreadable identifiers |
| `partial` | Show a portion (`****1234`) | Last-4 verification |
| `null` | Return NULL | Complete removal |

Masks are conditional on user attributes, so one column can be visible to the owning team
and masked for everyone else. Masking happens in generated SQL, not in the application
layer — masked values never leave the source database.

---

## 6. Secrets

```
Master key (from env, file, or KMS)
    │
    └─ wraps ─► Data encryption keys (per org, rotatable)
                    │
                    └─ encrypt ─► connection credentials
                                  IdP client secrets
                                  channel webhook URLs
                                  AI provider API keys
```

Envelope encryption with AES-256-GCM. The master key comes from AWS KMS, GCP KMS, Vault, or
a local file, depending on deployment.

Rules enforced by test, not by convention:

- Encrypted values are **never** returned by the API, including to admins
- Secrets are **never** logged, traced, or included in error messages or support bundles
- Credentials are decrypted only at connection time, in memory, never written to disk
- Key rotation re-wraps data keys without re-encrypting the data
- A secret-scanning test asserts no credential appears in any log output

---

## 7. Embedded analytics

The highest-risk surface, because the host application asserts identity on Pivot's behalf.

```
Host app                            Pivot
   │
   │ signs JWT with per-tenant key
   │ { sub, tenant, attributes, exp, jti, aud, iss }
   │──────────────────────────────────►
   │                                   verify signature (tenant key)
   │                                   verify aud, iss, exp
   │                                   check jti not replayed
   │                                   map attributes → RLS
   │                                   issue scoped session
   │◄──────────────────────────────────
```

Controls:

- **Per-tenant signing keys**, rotatable, so one compromised key affects one tenant
- **Short expiry** (default 5 minutes) with refresh
- **`jti` replay detection** in Valkey for the token's lifetime
- **Mandatory `aud` and `iss` validation** — omitting these is the classic JWT flaw
- **Locked parameters** that the embedded session cannot modify
- **Official signing helpers** in four languages, because integrators writing their own
  signing code is the likeliest source of an incident
- **Strict CSP and frame-ancestors** allowlisting per embed configuration

---

## 8. AI-specific security

Three controls, in order of importance:

**1. Permission-filtered retrieval.** The AI's context is assembled *after* permission
filtering. A model the user can't query never enters the context — because a model that
knows about a table it can't access will describe it, name its columns, or reason about it
in prose. The leak happens in the narration, not the result set. This is the AI security
failure most implementations have.

**2. Structured output, not SQL.** The model emits a semantic query — a selection from
known objects — which the compiler validates and compiles. Even a fully prompt-injected
model cannot exceed the user's permissions, because it never produces executable code.

**3. Data is never instruction.** Column values, user-authored descriptions, and glossary
entries are attacker-controllable in a multi-tenant product. They enter the model in a
separate channel from instructions, and generated output is validated against permitted
objects before anything runs.

Plus: PII redaction policy before model calls (configurable from "metadata only" to "full
values"), complete audit logging of every prompt and output, per-org token budgets, and
local model support for organizations that cannot send data anywhere.

---

## 9. Audit

Every one of these produces a record: authentication events, authorization denials, data
access (with the query), content CRUD, permission changes, connection changes, secret
access, admin actions, and every AI interaction.

Properties:

- **Tamper-evident** via a hash chain (`hash_prev` → `hash`)
- **Non-blocking** — written to a disk-backed buffer, flushed asynchronously, so a slow
  sink never slows a query
- **Not lossy** — if the buffer fills, audit-required operations fail closed rather than
  proceeding unaudited. Configurable; the default is the safe one.
- **Exportable** to syslog, webhook, or S3 for SIEM ingestion
- **Retention-managed** with configurable policies

---

## 10. Threat model summary

| Threat | Primary control |
|---|---|
| Credential theft | MFA, short sessions, immediate revocation, anomaly alerting |
| Session hijacking | `HttpOnly`/`Secure`/`SameSite`, IP binding option, rotation |
| SQL injection (Pivot-generated) | Compiler only; parameters bound, never interpolated |
| SQL injection (user SQL) | By design; contained by least-privilege credentials and limits |
| Privilege escalation | Compiler-enforced authz; no handler-level bypass exists |
| Cross-tenant access | Four independent isolation layers (see [system-design](system-design.md#4-multi-tenancy)) |
| Cache poisoning / cross-user leak | Policy-set hash in every cache key |
| Embedded token forgery | Per-tenant keys, short expiry, replay detection, aud/iss checks |
| Prompt injection | Structured output + compiler enforcement + filtered retrieval |
| Malicious plugin | iframe sandbox for viz; signing and admin approval for server-side |
| Python sandbox escape | Container + gVisor, no network, hard limits, disabled by default |
| Supply chain | Dependency scanning, SBOM, signed releases, reproducible builds |
| Insider access | Audit everything, break-glass with mandatory review, least privilege |

---

## 11. Security process

- **Threat model per new surface**, recorded in the PR
- **Dependency scanning on every build**; no new high or critical CVEs merge
- **Quarterly external penetration tests** from Phase 4, with published summaries
- **Vulnerability disclosure program** with a published SLA
- **Patch SLAs:** critical within 48 hours, high within 7 days
- **Blameless postmortems for every security incident**, including near misses
- **Annual access review** of internal systems

---

## 12. Known limitations, stated plainly

A security document that claims no gaps isn't a security document.

1. **Native SQL bypasses semantic RLS.** Inherent. Mitigated by making it a separate
   permission and recommending least-privilege connection credentials.
2. **A compromised warehouse credential exposes everything that credential can reach.**
   Pivot cannot constrain what it doesn't mediate.
3. **Column masking doesn't prevent inference.** A user who can see aggregates over a
   masked column may deduce values. Differential privacy is out of scope.
4. **Local LLMs have weaker safety properties** than hosted frontier models. Documented,
   with per-model benchmark scores published.
5. **The Python transform node is remote code execution by design.** Sandboxed, disabled by
   default, and carries its own threat model.
6. **Metadata database compromise is total.** It holds the encrypted secrets and the
   definition of everything. Defense is standard database security, not application design.
