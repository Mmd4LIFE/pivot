# Changelog

All notable changes to Pivot are recorded here.

Format based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html) —
breaking API changes bump major, no exceptions.

## [Unreleased]

### Added
- `pivot serve` — HTTP server with structured logging, `/healthz` liveness,
  `/readyz` readiness, and a graceful drain on SIGTERM/SIGINT — *Part 2*
- Configuration with strict precedence — defaults → file → `PIVOT_*`
  environment → flags — validated at startup with errors that name the field
  and say what is acceptable
- `server.preShutdownDelay`, a lame-duck period that fails readiness while
  still accepting, so load balancers observe a 503 rather than a refused
  connection. Defaults to 0; set ~5s behind a load balancer
- `pivot config show` (resolved configuration and its source file) and
  `pivot config env` (supported environment variables)
- `pivot version --json`
- Repository scaffold: Go module, package skeleton, and build tooling
  (`make build`, `lint`, `test`, `fmt`, `check`) — *Part 1*
- `internal/version` package with link-time stamping and VCS-revision fallback
- `pivot version` command
- Strict `golangci-lint` configuration (correctness and security linters
  enabled; style-opinion linters deliberately off)
- Apache 2.0 license, contributor guide, code of conduct, security policy, and
  PR template carrying the Definition of Done
- Design corpus: vision, tech stack, system/security/data architecture, nine
  ADRs, an 11-phase roadmap, NFRs, and the competitive feature matrix — *Part 0*
- `CHECKLIST.md` — the session-by-session build plan

[Unreleased]: https://github.com/Mmd4LIFE/pivot/commits/main
