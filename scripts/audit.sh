#!/usr/bin/env bash
#
# Known vulnerabilities, from two tools that see different things.
#
# The gate 00-principles.md names is "zero high/critical", and getting there
# honestly needs both of these and needs them pointed at the right thing.
#
# govulncheck owns Go. It analyses reachability against the real toolchain, so
# it reports a vulnerability only when the vulnerable symbol is actually called.
# Anything it finds fails this script outright: a reachable vulnerability in a
# dependency we call is not a judgement call.
#
# osv-scanner owns npm, and only npm. Pointing it at go.mod looks appealing and
# is a trap: it reads the `go` directive as the standard library's version and
# reports every stdlib advisory fixed after it. That directive is a *minimum
# language version*, not the toolchain anyone builds with -- it currently
# produces twenty-five findings that govulncheck, which knows the difference,
# reports as zero. A gate that cries wolf twenty-five times is a gate people
# route around.
set -euo pipefail

cd "$(dirname "$0")/.."

TOOLS="${TOOLS_DIR:-$PWD/bin}"
GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.8.0}"

echo "==> govulncheck (Go, reachability-based)"
go run "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}" ./...

echo
echo "==> osv-scanner (npm)"

report=$(mktemp)
trap 'rm -f "$report"' EXIT

# osv-scanner exits non-zero when it finds anything at all, including the lows
# and the dev-only ones. The severity decision is this script's to make, so its
# exit code is swallowed and the JSON is what gets judged.
"$TOOLS/osv-scanner" --format=json --lockfile=web/package-lock.json > "$report" 2>/dev/null || true

python3 - "$report" <<'PY'
import json, sys
from collections import defaultdict

with open(sys.argv[1]) as handle:
    report = json.load(handle)

# Anything at or above this fails. Everything below is printed and lives to be
# dealt with deliberately rather than under merge pressure.
BLOCKING = 7.0

blocking, other = [], []

for result in report.get("results", []):
    for package in result.get("packages", []):
        name = package["package"]["name"]
        version = package["package"]["version"]

        for vulnerability in package.get("vulnerabilities", []):
            identifier = vulnerability["id"]

            # CVSS v3/v4 base score, where the advisory carries one.
            score = 0.0
            for severity in vulnerability.get("severity", []) or []:
                try:
                    score = max(score, float(severity.get("score", 0)))
                except (TypeError, ValueError):
                    pass

            for group in package.get("groups", []):
                if identifier in group.get("ids", []):
                    try:
                        score = max(score, float(group.get("max_severity") or 0))
                    except (TypeError, ValueError):
                        pass

            entry = (score, identifier, f"{name}@{version}")
            (blocking if score >= BLOCKING else other).append(entry)

def show(title, entries):
    if not entries:
        return
    print(f"\n  {title}:")
    for score, identifier, package in sorted(entries, reverse=True):
        label = f"{score:.1f}" if score else "  -"
        print(f"    {label}  {identifier:<24} {package}")

show("Below the blocking threshold", other)
show(f"At or above CVSS {BLOCKING}", blocking)

if blocking:
    print(
        f"\naudit: {len(blocking)} vulnerability/vulnerabilities at or above "
        f"CVSS {BLOCKING}. The gate is zero high or critical.",
        file=sys.stderr,
    )
    raise SystemExit(1)

print(
    f"\n  No npm vulnerability at or above CVSS {BLOCKING}. "
    f"{len(other)} below it."
)
PY
