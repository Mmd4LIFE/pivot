# ADR-0005: Build our own semantic layer, dbt-compatible

**Status:** Accepted
**Date:** 2026-09-19

## Context

The semantic layer is the spine of the product. [The vision](../../vision.md#2-the-insight)
argues that governance, cost control, self-service, and trustworthy AI are one problem with
one solution: a single versioned, permission-aware definition of what the data means.

Mature options exist — Cube, dbt's MetricFlow, Malloy — and adopting one would save months
on the largest phase in the roadmap.

## Decision

Build Pivot's own semantic layer: YAML-defined models, dimensions, measures, metrics, a
declared join graph, and RLS policies, compiled to dialect SQL by a Pivot-owned compiler.
Remain **compatible** by making dbt a first-class import and sync target.

## Rationale

Three reasons, in order of weight.

**1. RLS must be enforced in our compiler.** [ADR-0009](0009-authorization.md) and
[the security model](../security-model.md) rest on a single invariant: every query passes
through one component that injects row filters and column masks. If the semantic layer is
an external service that generates SQL, authorization enforcement moves outside our
control, and the invariant that makes every surface safe by construction is gone. This
reason alone is close to decisive.

**2. Single-binary distribution.** Cube is a separate service with its own datastore,
deployment, and configuration. Requiring it breaks the 30-second install.

**3. It's the differentiator.** Per
[the feature matrix](../../roadmap/feature-matrix.md#3-semantic-layer), the combination of
semantic layer + self-hosted + open source is the product's core position. Outsourcing the
differentiating component is strategically odd, and it would leave the roadmap's most
important capability subject to someone else's priorities.

**dbt compatibility rather than dbt dependency.** dbt is
[the wedge](../../vision.md#6-the-wedge) — a company running dbt points Pivot at its repo
and gets a governed BI layer immediately. That requires reading dbt's manifest and metric
definitions faithfully, not running dbt.

## Alternatives considered

### Cube
Mature, good product, active community, saves 6+ engineer-months. Rejected primarily on
reason 1 — authorization enforcement would leave our compiler — and secondarily on
deployment. Cube's model format remains an import target.

### dbt MetricFlow as the engine
Attractive given the wedge. Rejected: it's Python, tightly coupled to dbt's project
structure, and would make dbt a hard dependency for users who don't run dbt. We import its
definitions instead, which gets the compatibility benefit without the coupling.

### Malloy
An elegant language with a genuinely better model of nested and aggregate-aware queries.
Rejected as too novel — adopting it means asking users to learn a new language, and the
ecosystem is small. Worth watching.

### No semantic layer (table-based, like Metabase)
Simplest, and it would compress the roadmap significantly. Rejected because it makes
[the AI phase](../../roadmap/phase-7-ai.md) impossible as designed. Without governed
metrics, the AI generates SQL over raw schemas — which is the failure mode described in
[the vision](../../vision.md#14-the-ai-disappointment) and the exact thing we're
differentiating against.

## Consequences

**Positive**
- Authorization stays in one enforceable place
- The AI compiles to semantic queries, which is the architecture the whole product bets on
- No external service; the single-binary story survives
- We control the roadmap of our most important component
- dbt import gives compatibility without dependency

**Negative**
- **This is the largest and riskiest phase in the roadmap.** A semantic layer is a
  compiler, and compilers are harder than they look. Join path resolution and fan-out
  safety are the specific hazards
- 6+ engineer-months we could have spent elsewhere
- Our semantic layer will be less mature than Cube's or LookML's for years — stated plainly
  in [the feature matrix](../../roadmap/feature-matrix.md#where-competitors-are-ahead-honestly)
- Scope creep toward LookML is a standing risk, mitigated by freezing the YAML schema at
  phase start and requiring an ADR to extend it

**Neutral**
- We own the correctness burden. Golden-file tests per dialect
  ([P3-CMP-009](../../roadmap/phase-3-semantic-layer.md#3-query-compiler)) are the primary
  defense

## Revisit if

- Fan-out safety or join resolution proves intractable at the required correctness level
- An open standard for semantic layers emerges with real adoption and permission
  delegation support
- Cube or an equivalent becomes embeddable as a library with pluggable authorization
