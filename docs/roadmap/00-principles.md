# Engineering Principles & Definition of Done

These apply to every phase. They exist so that quality is a property of the process rather
than a thing we schedule time for later.

---

## 1. Product principles

### 1.1 The 30-second rule
A new user must go from download to a rendered query result in under 30 seconds, with no
configuration file, no external service, and no documentation. Every architectural
decision is checked against this. It is why we chose Go, why SQLite is a supported
metadata backend, and why the AI service is optional.

### 1.2 One number, one definition
A metric computed for a dashboard, an alert, the API, an export, and an AI answer MUST be
produced by the same compiled query. If two surfaces can disagree about revenue, that is a
P0 bug, not a configuration issue.

### 1.3 Show the work
Every computed result MUST be traceable to the SQL that produced it, the model it came
from, and the cache generation it was served from. This applies doubly to AI: an answer
without inspectable SQL is not an answer.

### 1.4 Governed by default, not by upgrade
Row-level security, audit logging, and lineage are in the open-source core. We do not
build a product where the safe configuration costs extra.

### 1.5 Fail loud, degrade soft
Errors that risk showing *wrong data* MUST fail loudly and visibly. Errors that only
reduce capability SHOULD degrade quietly: AI service down means AI features are disabled,
not that dashboards break; cache miss means a slower query, not an error.

---

## 2. Definition of Done

A feature is not done until every box is checked. "Done except for tests" is not done;
it's an unpaid debt with a due date you don't control.

### Code
- [ ] Implements the specified feature, with edge cases from the spec handled explicitly
- [ ] Passes `golangci-lint` / Biome / `ruff` with zero warnings
- [ ] No TODO without a linked issue number
- [ ] Errors are wrapped with context; no bare `err` returns across package boundaries
- [ ] All user input is validated at the API boundary, not in the handler body

### Tests
- [ ] Unit tests for business logic; ≥80% coverage on new packages
- [ ] Integration tests for any code path touching a database or external service
- [ ] Connectors tested against a **real** containerized instance — never a mock
- [ ] Semantic compiler changes have golden-file tests for every supported dialect
- [ ] A regression test exists for every bug fixed
- [ ] E2E (Playwright) coverage for any new primary user flow

### Security
- [ ] Authorization enforced in the query compiler, not the handler
- [ ] No secrets in logs, error messages, traces, or API responses
- [ ] SQL built via parameterized queries or the semantic compiler; never string concat
- [ ] New external input has a threat-model note in the PR description
- [ ] Dependencies scanned; no new high or critical CVEs

### Performance
- [ ] Meets the budget in [non-functional-requirements.md](non-functional-requirements.md)
- [ ] No N+1 queries (verified by the query-count assertion in integration tests)
- [ ] Frontend lists and grids over 50 rows are virtualized
- [ ] Bundle size delta under 20KB gzipped, or justified in the PR

### Accessibility
- [ ] Keyboard navigable end to end, with a visible focus indicator
- [ ] Screen-reader tested for the primary path
- [ ] Contrast ≥ 4.5:1 for text, ≥ 3:1 for UI and chart elements
- [ ] Charts expose an equivalent accessible data table
- [ ] No information conveyed by color alone

### Observability
- [ ] OpenTelemetry spans on all significant operations
- [ ] Prometheus metrics for anything with a rate, a duration, or an error count
- [ ] Structured logs with trace correlation; no `fmt.Println`
- [ ] User-facing errors carry an error code that maps to a docs page

### Documentation
- [ ] User docs in the same PR as the feature
- [ ] OpenAPI spec updated; generated clients regenerated
- [ ] A migration note if behavior changed for existing installs
- [ ] `CHANGELOG.md` entry

---

## 3. Architectural rules

These are the invariants. Breaking one requires an ADR, not a PR comment.

1. **The query compiler is the only path to data.** No handler, job, or service issues SQL
   against a user's warehouse except through it. This is how permissions stay enforceable.
2. **The UI has no privileged API.** Everything the frontend does is a documented public
   endpoint. If the UI needs something the API can't do, the API is wrong.
3. **The metadata schema is portable.** Every query runs on Postgres and SQLite. Anything
   that can't degrades gracefully rather than breaking the SQLite build.
4. **Arrow is the internal data format.** Result sets are Arrow from connector to
   response. Row-oriented conversion happens once, at the edge.
5. **The AI service is optional.** No core feature may hard-depend on it. Every call site
   handles "unavailable" as a normal state.
6. **Tenant isolation is structural.** Every metadata query is scoped by tenant at the
   repository layer. There is no query that could return another tenant's rows if a
   `WHERE` clause were forgotten, because forgetting it is impossible.
7. **Migrations are forward-only.** No down migrations in production. Roll forward.

---

## 4. Code review standards

- Every PR needs one approval; security-, authz-, or migration-touching PRs need two, one
  from a maintainer.
- PRs over 400 lines of non-generated diff SHOULD be split. Large PRs get worse reviews.
- The PR description explains **why**, not what. The diff shows what.
- A reviewer who doesn't understand the change requests clarification rather than
  approving. "LGTM" on code you didn't follow is how bugs get consensus.

---

## 5. Quality gates in CI

Every one of these blocks merge.

| Gate | Threshold |
|---|---|
| Unit + integration tests | 100% pass |
| Coverage on changed packages | ≥ 80% |
| Lint (Go, TS, Python) | Zero warnings |
| Type check (`tsc`, `mypy --strict`) | Zero errors |
| Dependency vulnerability scan | Zero high/critical |
| Secret scanning | Zero findings |
| Bundle size | Within budget or explicit override |
| Performance regression suite | No metric >10% worse than baseline |
| E2E suite | 100% pass |
| Accessibility scan (axe) | Zero violations on changed pages |
| Migration test (Postgres + SQLite) | Both pass, forward and idempotent |

---

## 6. Release discipline

- **Semantic versioning.** Breaking API changes bump major. Period.
- **Release cadence:** minor every 4 weeks, patch as needed, security within 48 hours of a
  verified report.
- **Every release is upgradeable from the previous two minors** without manual steps.
- **Migrations are tested on a restored production-scale snapshot** before release.
- **Feature flags** gate anything shipped incomplete. Flags have owners and removal dates;
  a flag older than two minors is a bug.
- **Rollback is tested, not assumed.** Each release candidate is rolled back in staging as
  part of the checklist.

---

## 7. How we handle the things that go wrong

**Wrong numbers are P0.** Higher priority than downtime. An outage is visible and erodes
patience; a wrong number is invisible and erodes trust permanently. Any report of
inconsistent results between two Pivot surfaces stops other work.

**Data leaks are P0 and get a postmortem regardless of impact.** Including near misses.

**Performance regressions are bugs with owners.** The CI regression suite files them
automatically against the author of the causing commit.

**Postmortems are blameless and public** (internally at minimum). The output is a
prevention change, not an action item to "be more careful."
