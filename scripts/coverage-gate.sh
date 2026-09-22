#!/usr/bin/env bash
#
# Coverage on changed packages, which is the gate 00-principles.md names.
#
# Not overall coverage. A repository-wide percentage moves so slowly that it
# stops being a signal: one well-tested package can carry a badly-tested one for
# months, and the number never drops enough for anyone to notice. Per-package on
# what the branch actually touched is the version that can fail.
#
# Usage: scripts/coverage-gate.sh [base-ref] [threshold]
set -euo pipefail

BASE="${1:-origin/main}"
THRESHOLD="${2:-80}"
PROFILE="${PROFILE:-coverage.out}"

MODULE=$(go list -m)

# Packages the gate does not measure, and why.
#
#   internal/store/gen/   sqlc output. Nobody writes tests for generated code,
#                         and `make gen-check` already fails if it drifts from
#                         the SQL it came from -- which is the check that
#                         actually means something here.
#   cmd/pivot             six statements of main(), whose only job is to call
#                         internal/cli. That package is measured.
#
# Deliberately short. Every entry here is a place the gate cannot see, so the
# list is a liability and adding to it needs a reason in the same commit.
is_excluded() {
  case "$1" in
    */internal/store/gen/*) return 0 ;;
    */cmd/pivot)            return 0 ;;
    *)                      return 1 ;;
  esac
}

if [ ! -f "$PROFILE" ]; then
  echo "coverage-gate: $PROFILE does not exist; run the tests with -coverprofile first" >&2
  exit 1
fi

if ! git rev-parse --verify --quiet "$BASE" >/dev/null; then
  echo "coverage-gate: $BASE is not a ref this clone knows about." >&2
  echo "  In CI, fetch the base branch first. Locally, try: git fetch origin main" >&2
  exit 1
fi

# The packages this branch touched. Three-dot, so it is the branch's own
# changes rather than everything that has landed on the base since.
changed_dirs=$(
  git diff --name-only "$BASE...HEAD" -- '*.go' \
    | xargs -r -n1 dirname \
    | sort -u
)

if [ -z "$changed_dirs" ]; then
  echo "coverage-gate: no Go files changed against $BASE; nothing to check."
  exit 0
fi

# Per-package coverage, derived from the profile. `go tool cover -func` reports
# per function, so the statements are summed per package rather than averaging
# percentages -- averaging would let a one-line uncovered file outweigh a
# thousand covered ones.
coverage_for() {
  local pkg="$1"

  awk -v pkg="$pkg/" '
    NR == 1 { next }                       # the "mode:" line

    {
      # A profile line is "file.go:startLine.col,endLine.col statements count".
      split($1, location, ":")
      file = location[1]

      if (index(file, pkg) != 1) next
      if (substr(file, length(pkg) + 1) ~ /\//) next   # a subdirectory is its own package

      # The same block appears once per test binary that instrumented it, which
      # is what -coverpkg produces. Keyed and merged by taking the highest
      # count, because a block covered by any binary is covered. Summing the
      # duplicates instead inflates the statement total and reports a package
      # as an order of magnitude less covered than it is -- which is exactly
      # what this script did before it was fixed.
      block = $1
      statements[block] = $2
      if (!(block in count) || $3 > count[block]) count[block] = $3
    }

    END {
      for (block in statements) {
        total += statements[block]
        if (count[block] > 0) covered += statements[block]
      }

      if (total == 0) { print "none"; exit }
      printf "%.1f %d\n", (covered / total) * 100, total
    }
  ' "$PROFILE"
}

failed=0
untested=()

echo "Coverage on packages changed against $BASE (threshold ${THRESHOLD}%):"
echo

for dir in $changed_dirs; do
  [ -d "$dir" ] || continue

  pkg="$MODULE"
  [ "$dir" != "." ] && pkg="$MODULE/$dir"

  if is_excluded "$pkg"; then
    printf '  skip            %-60s (excluded; see the list in this script)\n' "$pkg"
    continue
  fi

  read -r percent statements < <(coverage_for "$pkg")

  if [ "$percent" = "none" ]; then
    # No statements in the profile: either the package has no test files, or it
    # has no executable code at all (a doc.go stub). Reported rather than
    # failed, because failing a documentation-only package helps nobody -- but
    # reported loudly, because "delete the test file" must not be a way past
    # this gate.
    if compgen -G "$dir/*_test.go" >/dev/null; then
      continue
    fi

    if compgen -G "$dir/*.go" >/dev/null; then
      untested+=("$pkg")
    fi

    continue
  fi

  if awk -v p="$percent" -v t="$THRESHOLD" 'BEGIN { exit !(p < t) }'; then
    printf '  FAIL  %6s%%  %-60s (%s statements)\n' "$percent" "$pkg" "$statements"
    failed=1
  else
    printf '  ok    %6s%%  %-60s (%s statements)\n' "$percent" "$pkg" "$statements"
  fi
done

if [ ${#untested[@]} -gt 0 ]; then
  echo
  echo "  Changed packages with no test file at all:"
  printf '    %s\n' "${untested[@]}"
  echo "  These are not measured. If any of them holds real logic, it needs tests."
fi

echo

if [ "$failed" -ne 0 ]; then
  echo "coverage-gate: at least one changed package is below ${THRESHOLD}%." >&2
  exit 1
fi

echo "coverage-gate: every changed package meets ${THRESHOLD}%."
