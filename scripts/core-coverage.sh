#!/usr/bin/env sh
# Enforces the >=90% statement-coverage floor on internal/core/**.
# docs/06-harness.md §3, spec R8.1.
#
# Why this parses coverage.out directly instead of `go tool cover -func`:
# -func emits a formatted human report whose columns and trailing "total:" line
# are a presentation detail, not a contract — but building on that format is
# not what this script does. What it needs from coverage.out is documented and
# stable: every non-header line is
#
#   name.go:startLine.col,endLine.col numStmt count
#
# What is NOT documented as "one line per range" is coverage.out's *shape*
# under `-coverpkg`: `go test -coverpkg=X ./Y/...` writes one profile
# fragment per test binary, and when `./Y/...` expands to sibling packages
# that share code, the same source range appears once per fragment, each
# with its own count. `go tool cover -func` merges these correctly — a range
# counts once toward the total, and counts as covered if ANY fragment
# covered it. A flat per-line sum over the raw file does not: it double
# (or N-times) counts a shared range's numStmt for every fragment it
# appears in, and gives it only partial coverage credit when fragments
# disagree (e.g. sibling A's tests exercise a shared helper's branch that
# sibling B's tests do not) — undercounting the true, merged percentage.
# That bias fails closed (this script never over-reports coverage), but it
# is still wrong: a genuinely 90%+-covered package can read as failing.
#
# The fix: deduplicate by range key (the `file:start,end` field) before
# summing. Each unique range counts once toward total (its numStmt, which is
# identical across every fragment reporting that range); toward covered if
# any occurrence had count > 0. That is the same rule `go tool cover -func`
# applies — reimplemented here, not delegated to its text output, because
# this script needs the two summed integers, not a formatted table.
#
# The vacuity case matters as much as the floor. internal/core/ has no
# statements yet, so this gate currently passes because there is nothing to
# measure — which is indistinguishable, from a green CI badge, from passing
# because the code is well covered. It says which one happened.
#
# Field-splitting note: awk's default whitespace split assumes the range key
# (field 1) never contains a space. Go import paths cannot contain spaces,
# so a coverage.out line's file:range field is structurally safe here — this
# is a documented invariant of the input, not an unchecked assumption.
set -u

FLOOR=90

# Test mode: a caller (test/harness/) passes a pre-built profile as $1 and
# this script skips running `go test` entirely, so the arithmetic below can
# be exercised against synthetic fixtures without a real internal/core
# package to compile against. Production (`make cover`, CI) passes no
# argument.
if [ "$#" -ge 1 ]; then
  PROFILE=$1
else
  PROFILE=coverage.out
  out=$(go test -coverprofile="$PROFILE" -coverpkg=./internal/core/... ./internal/core/... 2>&1)
  status=$?
  if [ "$status" -ne 0 ]; then
    echo "FAIL: could not produce $PROFILE for ./internal/core/..."
    printf '%s\n' "$out"
    exit 1
  fi
fi

if [ ! -f "$PROFILE" ]; then
  echo "FAIL: $PROFILE was not written."
  exit 1
fi

# Single pass: for each unique range (field 1), remember its numStmt (field
# 2 — identical across every fragment reporting that range) and the highest
# count (field 3) seen for it across all fragments. A range is covered if
# ANY fragment covered it — matching go tool cover's merge rule.
result=$(awk '
  NR > 1 {
    key = $1
    if (!(key in stmt)) {
      stmt[key] = $2
      order[++n] = key
    }
    if ($3 + 0 > best[key]) {
      best[key] = $3 + 0
    }
  }
  END {
    total = 0
    covered = 0
    for (i = 1; i <= n; i++) {
      k = order[i]
      total += stmt[k]
      if (best[k] > 0) {
        covered += stmt[k]
      }
    }
    print total, covered
  }
' "$PROFILE")

total=${result%% *}
covered=${result#* }

if [ "$total" -eq 0 ]; then
  echo "internal/core has no statements yet — the >=$FLOOR% floor is armed but vacuous (docs/06-harness.md §3)"
  exit 0
fi

# Integer percentage, floored. A run at 89.9% must fail, so compare covered*100
# against floor*total rather than rounding first.
pct=$((covered * 100 / total))

if [ $((covered * 100)) -lt $((FLOOR * total)) ]; then
  echo "FAIL: internal/core statement coverage is ${pct}% ($covered/$total), below the $FLOOR% floor."
  echo "docs/06-harness.md §3 sets this floor on the cognitive core specifically:"
  echo "there is no global floor, because a global number is satisfied by writing"
  echo "useless getter tests. Cover the decision logic, or change the floor in"
  echo "doc 06 and say why."
  exit 1
fi

echo "OK: internal/core statement coverage is ${pct}% ($covered/$total), at or above the $FLOOR% floor."
