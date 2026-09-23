# Pivot Documentation

This is the design corpus for Pivot. It is written to be read in order the first time,
and grepped thereafter.

> **Building right now?** The work queue is [../CHECKLIST.md](../CHECKLIST.md) — it tracks
> what's done, what's next, and how each session picks up where the last left off.

## Read in this order

### 1. Why
- **[vision.md](vision.md)** — the problem, the market gap, the positioning, the wedge.

### 2. How it's built
- **[architecture/tech-stack.md](architecture/tech-stack.md)** — every technology
  decision with rationale, tradeoffs, and rejected alternatives.
- **[architecture/system-design.md](architecture/system-design.md)** — services, request
  lifecycle, query path, deployment topologies.
- **[architecture/data-model.md](architecture/data-model.md)** — the metadata schema that
  everything else hangs off.
- **[architecture/security-model.md](architecture/security-model.md)** — authentication,
  authorization, row/column-level security, tenancy, secrets.
- **[architecture/adr/](architecture/adr/)** — Architecture Decision Records. The
  irreversible calls, dated and justified.

### 3. Running it
- **[operations/backup-and-restore.md](operations/backup-and-restore.md)** — taking a
  backup that can actually be restored, and restoring it. Written for whoever runs the
  instance rather than for whoever built it.
- **[operations/secrets.md](operations/secrets.md)** — the key that makes a stolen
  database useless, where it lives, and how to rotate it without losing everything it
  protects.

### 4. Standing records
- **[design/keyboard-audit.md](design/keyboard-audit.md)** — how `web/src/ui` behaves
  without a mouse: what four layers of test prove, and the ten things only a person with
  a keyboard and a screen reader can check.

### 5. What gets built, when
- **[roadmap/README.md](roadmap/README.md)** — the phase map and timeline.
- **[roadmap/00-principles.md](roadmap/00-principles.md)** — engineering standards,
  definition of done, quality gates.
- **[roadmap/phase-0-foundations.md](roadmap/phase-0-foundations.md)** → **[phase-10](roadmap/phase-10-ecosystem.md)**
  — the detailed per-phase feature specifications.
- **[roadmap/feature-matrix.md](roadmap/feature-matrix.md)** — the full feature inventory,
  scored against Metabase, Superset, Looker, Power BI, and Tableau.
- **[roadmap/milestones.md](roadmap/milestones.md)** — release plan from v0.1 to v2.0.
- **[roadmap/non-functional-requirements.md](roadmap/non-functional-requirements.md)** —
  performance, scale, availability, and compliance targets with hard numbers.

---

## Documentation conventions

**Requirement keywords** follow RFC 2119: **MUST**, **SHOULD**, **MAY**.

**Feature IDs** are stable and referenced from issues and commits. Format:
`P<phase>-<area>-<number>` — e.g. `P2-VIZ-014` is the fourteenth visualization feature in
Phase 2. Once assigned, an ID is never reused.

**Effort sizing** uses engineer-weeks (ew) for one engineer of average familiarity with
the codebase:

| Size | Effort | Meaning |
|---|---|---|
| XS | < 0.5 ew | A focused change |
| S | 0.5–1 ew | A contained feature |
| M | 1–3 ew | A feature with a UI and a backend |
| L | 3–8 ew | A subsystem |
| XL | 8–20 ew | A product surface |
| XXL | 20+ ew | Split it; this is a phase, not a feature |

**Priority** is `P0` (the phase cannot ship without it), `P1` (the phase is diminished
without it), `P2` (valuable, cut first under pressure).

---

## Keeping this current

The roadmap is a living document, not a contract. When reality diverges:

1. Update the phase document — do not let it drift into fiction.
2. If the change is architectural and hard to reverse, write an ADR.
3. Note significant scope changes in [roadmap/milestones.md](roadmap/milestones.md).

A roadmap nobody edits is a roadmap nobody reads.
