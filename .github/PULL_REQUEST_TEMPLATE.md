<!--
Explain WHY this change exists. The diff already shows what it does.
If it closes an issue, say "Closes #123".
-->

## What & why

## Related

- Checklist part:
- Feature ID(s):
- Issue:

---

## Definition of Done

From [docs/roadmap/00-principles.md](../docs/roadmap/00-principles.md#2-definition-of-done).
Tick what applies; strike through what genuinely doesn't.

### Code
- [ ] Implements the spec, with its edge cases handled explicitly
- [ ] Lint passes with zero warnings
- [ ] No `TODO` without a linked issue number
- [ ] Errors wrapped with context across package boundaries
- [ ] Input validated at the API boundary

### Tests
- [ ] Unit tests for business logic (≥80% on new packages)
- [ ] Integration tests for anything touching a DB or external service
- [ ] Connectors tested against a real containerized instance, not a mock
- [ ] Semantic compiler changes have golden-file tests per dialect
- [ ] A regression test exists for every bug this fixes

### Security
- [ ] Authorization enforced in the query compiler, not the handler
- [ ] No secrets in logs, errors, traces, or API responses
- [ ] SQL parameterized or compiler-generated; never string concatenation
- [ ] Threat-model note below if this adds external input
- [ ] No new high/critical CVEs

### Performance
- [ ] Meets its budget in [non-functional-requirements.md](../docs/roadmap/non-functional-requirements.md)
- [ ] No N+1 queries
- [ ] Lists/grids over 50 rows virtualized
- [ ] Bundle delta under 20KB gzipped, or justified below

### Accessibility
- [ ] Keyboard navigable with a visible focus indicator
- [ ] Screen-reader tested on the primary path
- [ ] Contrast ≥4.5:1 text, ≥3:1 UI and chart elements
- [ ] Charts expose an equivalent accessible data table
- [ ] No information conveyed by color alone

### Observability
- [ ] OTel spans on significant operations
- [ ] Prometheus metrics for anything with a rate, duration, or error count
- [ ] Structured logs with trace correlation
- [ ] User-facing errors carry a documented error code

### Documentation
- [ ] User docs updated **in this PR**
- [ ] OpenAPI spec updated; clients regenerated
- [ ] Migration note if behavior changed for existing installs
- [ ] `CHANGELOG.md` entry

---

## Threat model note
<!-- Required if this PR accepts new external input. What's the attack surface? -->

## Performance / bundle-size justification
<!-- Required if a budget is exceeded. -->
