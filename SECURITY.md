# Security Policy

## Reporting a vulnerability

**Please do not open a public issue.**

Report privately through
[GitHub Security Advisories](https://github.com/Mmd4LIFE/pivot/security/advisories/new),
which lets us coordinate a fix before disclosure.

Include what you can:

- The type of issue and the affected component
- Steps to reproduce, or a proof of concept
- Version and deployment shape (binary / Compose / Kubernetes)
- What an attacker could achieve

You do not need a working exploit to report something. A credible description of
a weakness is enough.

## What to expect

| Stage | Target |
|---|---|
| Acknowledgement | 48 hours |
| Initial assessment | 5 business days |
| Critical fix released | 48 hours from confirmation |
| High fix released | 7 days from confirmation |
| Public advisory | After a fix ships, coordinated with you |

We credit reporters in the advisory unless you'd rather stay anonymous.

## Scope

In scope: the Pivot server, CLI, web frontend, SDKs, official container images,
and the Helm chart.

Particularly interested in anything touching these
[security invariants](docs/roadmap/non-functional-requirements.md#security-invariants):

1. A query path that bypasses the semantic compiler's authorization
2. A cached result served to a user under a different policy set
3. A secret appearing in logs, traces, error messages, or API responses
4. A metadata query returning another tenant's rows
5. An AI-generated query executing without compiler validation
6. An embedded token accepted after expiry or on replay

Out of scope: findings against third-party services, social engineering,
physical attacks, DoS by resource exhaustion on a self-hosted instance the
reporter controls, and issues requiring an already-compromised host.

## Known limitations

Some things are limitations by design rather than vulnerabilities. They're
documented in
[the security model](docs/architecture/security-model.md#12-known-limitations-stated-plainly) —
most notably that **native SQL access bypasses semantic-layer row-level
security**, which is inherent and is why it's a separately granted permission.

Please read that section before reporting; if you think one of those
limitations is worse than we've described, we do want to hear it.

## Supported versions

Pivot is pre-alpha. Until v1.0, only `main` receives security fixes. From v1.0,
the current minor and the two preceding it are supported.
