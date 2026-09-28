# ADR-0010: DuckDB is an opt-in build, not the default binary

**Status:** Accepted
**Date:** 2026-09-28
**Amends:** [ADR-0004](0004-query-engine.md)

## Context

ADR-0004 chose DuckDB for local analytical compute and accepted CGo with its eyes open. It
listed the cost plainly:

> **CGo.** Complicates cross-compilation, raises build complexity, and requires a
> per-platform CI matrix. Mitigated by a `nocgo` build tag that disables acceleration and
> federation for environments needing a pure-Go binary

and it named the condition for changing course:

> **Revisit if** — CGo build complexity outweighs the benefit (fall back to `nocgo` as the
> default)

Part 18-c is where that bill came due. By then the pure-Go build was not a hypothetical
fallback — it was everything Phase 0 shipped:

- `.goreleaser.yaml` builds **six** targets (linux, darwin, windows × amd64, arm64) from a
  single Linux runner with `CGO_ENABLED=0`, and says why: *"Static, so a binary downloaded
  onto any Linux runs. Possible only because ADR-0003 chose modernc.org/sqlite."*
- The container is `gcr.io/distroless/static-debian12`, chosen for *"no shell, no package
  manager, no libc. Nothing to exploit."*

## Measurements

Taken on 2026-09-28 against `github.com/marcboeker/go-duckdb/v2@v2.4.3`, comparing the
default build with the same tree built `-tags duckdb`.

| | Default (`CGO_ENABLED=0`) | With DuckDB |
|---|---|---|
| Binary size | 42.5 MB | **101.8 MB** (+59.1 MB) |
| Linkage | statically linked | dynamic: `libstdc++`, `libgcc_s`, `libm`, `libc` |
| Cross-compile `darwin/arm64` | builds | **fails** — `undefined: bindings.Type` |
| Cross-compile `linux/arm64` | builds | **fails** — same |
| `windows/arm64` | builds | **no bindings published** (404 on the module proxy) |
| Clean build time | 18.3 s | 20.5 s |
| Module cache | — | **+327 MB** of prebuilt static libraries |

Four of those are decisive on their own.

**The binary stops being static.** It links `libstdc++` and `libc`, and the container base
has neither. Shipping DuckDB by default means moving off distroless-static onto an image
with a C++ runtime — trading a documented security property for a feature nobody has asked
for yet.

**Cross-compilation stops working.** Not "gets slower": the build fails. Six targets from
one runner becomes a native runner per platform, or a full cross-toolchain matrix.

**A platform disappears.** `duckdb-go-bindings` publishes nothing for `windows/arm64`, so a
mandatory DuckDB drops the release from six platforms to five.

**The size estimate was off by ~2×.** ADR-0004 predicted *"Binary size grows by roughly
30MB"*. It is 59 MB, and 30 MB was already a lot for a product whose pitch is one download.

Two further signals, not decisive but worth recording: the upstream module is **deprecated**
in favour of `github.com/duckdb/duckdb-go`, which **cannot be required under that path** —
`duckdb/duckdb-go@v1.8.5` still declares its module path as `marcboeker/go-duckdb`. And
fetching 327 MB of prebuilt libraries failed once with a connection reset on an ordinary
connection before succeeding on a retry.

## Decision

**Invert ADR-0004's default.** The default build is pure Go, statically linked, and contains
no DuckDB. DuckDB lives behind a `duckdb` build tag.

Concretely:

- `internal/connectors/duckdb.go` carries `//go:build duckdb`.
- `internal/connectors/duckdb_absent.go` carries `//go:build !duckdb` and registers the kind
  as *absent*, so asking for a DuckDB connection in a default binary explains that it was
  compiled out and what to do — rather than saying the connector does not exist.
- `make build-duckdb` and `make test-duckdb` build and exercise the tagged variant, and CI
  runs the latter on every push.
- The release matrix is **unchanged**: six static targets, one runner, no C toolchain.

This is the same mitigation ADR-0004 described, with the polarity reversed. ADR-0004
expected `nocgo` to be the exception for constrained environments; in practice the pure-Go
build is the product and DuckDB is the exception.

## Consequences

**Positive**

- Everything Phase 0 shipped keeps working, unchanged: six platforms, static binaries,
  distroless-static, no per-platform runners
- The default download stays 42.5 MB rather than 102 MB
- DuckDB is still available, tested, and conformance-passing — `make test-duckdb` runs the
  full suite plus Parquet and CSV reads against it
- The decision is reversible in one line if the trade changes

**Negative**

- **Acceleration and federation are not in the default binary.** ADR-0004's strategic payoff
  — [P2-DPF-003](../../roadmap/phase-2-visualization-dashboards.md), running one query for
  twenty dashboard cards — needs a build most people will not have. Phase 2 has to decide
  whether that ships as a separate artifact or the trade is taken then, with a real workload
  to measure instead of a prediction
- **Two build configurations** to keep honest. CI builds and tests both; without that, the
  tagged one rots
- **The sample dataset cannot be DuckDB.** Part 27 wanted an embedded DuckDB dataset for a
  first run; a default binary has no DuckDB, so it uses SQLite — which reads a file, is
  always present, and is entirely adequate for browsing a schema and running a query

**Neutral**

- The per-query memory cap NFR §1.3 requires is implemented for DuckDB regardless, since a
  connector that can take the host down is not shippable in either configuration

## Revisit if

- Phase 2 measures the shared-subquery payoff against a real dashboard and it is large
  enough to justify a second release artifact
- `duckdb-go-bindings` publishes `windows/arm64` and cross-compilation becomes possible,
  which would remove two of the four decisive costs at once
- A pure-Go analytical engine becomes credible, which would remove all of them
