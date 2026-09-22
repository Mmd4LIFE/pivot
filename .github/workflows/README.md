# Workflows

`ci.yml` is the gate. It runs on every pull request and every push to `main`,
and every job in it blocks merge.

## What runs, and why it is a separate job

Five jobs in parallel, so the wall-clock is the slowest one rather than the sum.
Each one is a different kind of failure, and keeping them apart means the
summary says which kind without anyone opening a log.

| Job | Catches |
|---|---|
| **Go** | Formatting, vet, lint, stale generated code, an untidy `go.mod`, a spec that no longer matches the code, a failing test on **either** engine, and a changed package under 80% covered. |
| **Frontend** | Type errors, a failing component or accessibility test, a broken build, a bundle over the NFR budget, and a story that no longer compiles. |
| **End to end** | Anything that only appears in a real browser against the real binary: the SPA fallback, the session cookie, the document CSP, contrast as actually painted. |
| **Security** | Known vulnerabilities in Go *and* npm dependencies, and secrets anywhere in history. |
| **Image** | A Dockerfile that stopped building, or an image that builds and cannot start. |

## Decisions worth not re-deriving

**Postgres is a service container, and a step asserts it was really used.** The
portability harness skips its Postgres half when `PIVOT_TEST_POSTGRES_URL` is
unset, and a skipped suite looks exactly like a passing one — which is the
single failure mode ADR-0003's dual-engine promise is most exposed to. The
tests run with the variable set, and a following step greps the log for skips
and fails if it finds any.

**`-p 1` on the Go tests is not a preference.** Without it Go starts one test
binary per package at once and several hash with Argon2 at 64 MiB under the
race detector, which is enough to get the run killed on a two-core runner.

**Node is pinned to 22, not to the 20.16 on the development machine.** 20.16 is
what currently holds Vite and Storybook a major version behind; running CI on 22
proves the code is ready for that bump before anybody makes it. `engines` in
`web/package.json` permits both, deliberately.

**Every security tool is a pinned Go module run through `go run`, or a pinned
container image** — never a third-party action. An action is code that runs with
the workflow's token; a module version is something the Go checksum database
already verifies.

**The image is built and never pushed here.** Publishing is Part 13's. What this
catches is a Dockerfile that broke, on the pull request that broke it rather
than on the release that needed it.

## Running the same checks locally

    make check        # formatting, vet, lint, generated code, spec, tests, coverage
    make web-test     # the component and accessibility suites
    make e2e          # the browser suite, against the built binary
    make bundle-size  # the initial bundle against its budget
    make image        # the container image

`make check` runs `test-all`, which needs the development Postgres: `make dev-db`.
