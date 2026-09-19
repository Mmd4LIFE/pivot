# Architecture

## Documents

| Document | Read it for |
|---|---|
| [tech-stack.md](tech-stack.md) | Every technology choice, its rationale, and what we rejected |
| [system-design.md](system-design.md) | Components, the query path, deployment topologies, failure domains |
| [data-model.md](data-model.md) | The metadata schema |
| [security-model.md](security-model.md) | AuthN, authZ, RLS, masking, secrets, threat model |
| [adr/](adr/) | The decisions, dated and justified |

---

## The three ideas that determine everything else

If you read nothing else, read this.

### 1. The semantic compiler is the only path to data

Every query — UI, API, alert, flow, export, AI — is compiled by one component that applies
the requesting user's row filters and column masks before emitting SQL. Connectors accept
compiled query objects, not SQL strings.

This makes authorization bypass structurally impossible rather than merely unlikely, and
it's why a new surface inherits the entire permission model for free.

### 2. The AI generates semantic queries, not SQL

The model selects from governed metrics and dimensions; the compiler turns that selection
into SQL. Consequences: the AI cannot invent a metric definition, cannot exceed the user's
permissions even if fully prompt-injected, and produces answers that match dashboards by
construction.

This is the bet the entire product is organized around, and it's why AI ships in Phase 7
rather than Phase 3.

### 3. Compute is local as well as remote

Embedding DuckDB means Pivot can join across databases, materialize accelerations to
Parquet, consolidate a dashboard's twenty near-identical queries into one, and run pipeline
transforms — none of which is possible in a tool that only pushes queries down to the
source.

This is the source of the cost-governance story and most of the performance story.

---

## How to read the diagrams

`system-design.md` uses ASCII diagrams rather than image files, deliberately: they diff in
Git, they're editable by anyone, and they don't rot in a separate design tool. If a diagram
becomes too complex for ASCII, that's usually a signal the design is too complex, not that
we need a better diagramming tool.

---

## Writing an ADR

Write one when a decision is **hard to reverse** and **someone will ask why later**.

Not every choice needs an ADR. Choosing a linter doesn't. Choosing Go does.

Copy [adr/0000-template.md](adr/0000-template.md), take the next number, and submit it as a
PR — the discussion belongs in the review. Once merged, an ADR is immutable: superseding
decisions get a new ADR that links back, and the old one is marked `Superseded`. Editing
history to look smarter than you were defeats the purpose.
