# Contributing to Pivot

Thanks for considering it. This document covers how to get a working
environment, what we expect from a change, and how decisions get made.

> **Status:** Pivot is pre-alpha. The codebase changes shape weekly and the
> plugin and API contracts are explicitly unstable. If you're planning
> something substantial, open a discussion first so we don't waste your time.

---

## Getting set up

**Prerequisites:** Go 1.23+, Git, Make. (Node 20+ and Python 3.12 arrive with
Parts 9 and Phase 7 respectively — not needed yet.)

```bash
git clone https://github.com/Mmd4LIFE/pivot.git
cd pivot
make tools     # installs pinned dev tooling into ./bin
make build     # produces ./bin/pivot
make test
./bin/pivot version
```

`make help` lists every task.

`make tools` fetches the golangci-lint release archive and verifies it against the
published SHA-256 checksums. We deliberately don't `go install` it: building
it from source pulls ~400 modules, and the checksum-database lookups that
requires are the first thing to fail on a slow or flaky connection.

If `go` isn't on your PATH and you'd rather not install it system-wide:

```bash
curl -LO https://go.dev/dl/go1.27.1.linux-amd64.tar.gz
tar -C ~/.local -xzf go1.27.1.linux-amd64.tar.gz
export PATH="$HOME/.local/go/bin:$PATH"   # add to your shell profile
```

---

## Before you write code

**Read the design corpus.** Pivot is documentation-first, and most "why is it
like this?" questions are already answered:

| If you're touching… | Read |
|---|---|
| Anything | [docs/roadmap/00-principles.md](docs/roadmap/00-principles.md) |
| Architecture | [docs/architecture/README.md](docs/architecture/README.md) |
| A technology choice | [docs/architecture/adr/](docs/architecture/adr/) |
| Auth, permissions, RLS | [docs/architecture/security-model.md](docs/architecture/security-model.md) |
| The schema | [docs/architecture/data-model.md](docs/architecture/data-model.md) |
| What's being built now | [CHECKLIST.md](CHECKLIST.md) |

**Check the roadmap.** Features have stable IDs (`P0-AUTH-001`) that map to
specs in [docs/roadmap/](docs/roadmap/). Reference them in your PR.

---

## The rules that aren't negotiable

These are [architectural invariants](docs/roadmap/00-principles.md#3-architectural-rules).
Breaking one needs an ADR, not a PR comment.

1. **The query compiler is the only path to data.** No handler, job, or service
   issues SQL against a user's warehouse except through it. This is what makes
   permissions enforceable.
2. **The UI has no privileged API.** If the frontend needs something the public
   API can't do, the API is wrong.
3. **The metadata schema is portable.** Every query runs on Postgres *and*
   SQLite. Postgres-only features degrade; they don't break the SQLite build.
4. **Arrow is the internal data format.** Row-oriented conversion happens once,
   at the edge.
5. **The AI service is optional.** No core feature may hard-depend on it.
6. **Tenant isolation is structural.** Forgetting a `WHERE org_id = ?` must be
   impossible, not merely unlikely.
7. **Migrations are forward-only.** Roll forward, never down.

---

## Definition of Done

The full checklist lives in
[00-principles.md](docs/roadmap/00-principles.md#2-definition-of-done) and is
reproduced in the PR template. The short version:

- Tests for the logic, integration tests for anything touching a DB
- Zero lint warnings, zero new CVEs
- No secrets in logs, errors, traces, or responses
- Keyboard navigable, screen-reader tested, contrast-compliant
- Docs updated **in the same PR** — a feature without docs is not done
- A `CHANGELOG.md` entry

"Done except for tests" is not done. It's an unpaid debt with a due date you
don't control.

---

## Pull requests

- **Branch from `main`.** Name it `<type>/<short-description>`.
- **Conventional commits:** `feat:`, `fix:`, `docs:`, `refactor:`, `test:`,
  `chore:`, `perf:`, `ci:`. Breaking changes get a `!` and a footer.
- **Keep PRs under ~400 lines** of non-generated diff. Large PRs get worse
  reviews, so split them.
- **Explain *why* in the description.** The diff shows what.
- **One approval** to merge; **two** (one from a maintainer) for anything
  touching security, authorization, or migrations.

Reviewers: if you don't understand a change, ask. "LGTM" on code you didn't
follow is how bugs get consensus.

---

## Testing

```bash
make test      # race-enabled, no cache
make cover     # HTML coverage report
make check     # everything CI runs
```

Two areas get more rigor than the rest, because they're where wrong numbers
come from:

- **Connectors** are tested against real containerized databases, never mocks.
  A mocked Snowflake driver tests nothing.
- **The semantic compiler** gets golden-file tests: semantic query in, expected
  SQL per dialect out.

Every bug fix ships with a regression test.

---

## Reporting bugs

Include: what you expected, what happened, `pivot version` output, your
deployment shape (binary / Compose / K8s), and the metadata backend
(SQLite / Postgres). Redact credentials — and tell us if you think you
accidentally didn't.

**A wrong number is a P0 bug**, higher priority than downtime. If two Pivot
surfaces disagree about the same metric, say so prominently; that stops other
work.

---

## Security

**Do not open a public issue for a vulnerability.** See
[SECURITY.md](SECURITY.md) for the disclosure process. We respond within 48
hours and credit reporters who want it.

---

## How decisions get made

Significant technical decisions are recorded as
[ADRs](docs/architecture/adr/). If you want to change one, write a new ADR that
supersedes it — don't edit the old one. Editing history to look smarter than we
were defeats the purpose.

For product direction, open a discussion. The roadmap is a living document, not
a contract; if reality diverges, we update the docs rather than let them become
fiction.

---

## Code of Conduct

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).
