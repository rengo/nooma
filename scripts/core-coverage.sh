#!/usr/bin/env sh
# Enforces the >=90% statement-coverage floor on internal/core/**.
# docs/06-harness.md §3, spec R8.1.
#
# Why this parses coverage.out directly instead of `go tool cover -func`:
# -func emits a formatted human report whose columns and trailing "total:" line
# are a presentation detail, not a contract. coverage.out's format is stable and
# documented — every non-header line is:
#
#   name.go:startLine.col,endLine.col numStmt count
#
# so the floor is (sum of numStmt where count > 0) / (sum of numStmt).
#
# The vacuity case matters as much as the floor. internal/core/ has no
# statements yet, so this gate currently passes because there is nothing to
# measure — which is indistinguishable, from a green CI badge, from passing
# because the code is well covered. It says which one happened.
set -u

PROFILE=coverage.out
FLOOR=90

go test -coverprofile="$PROFILE" -coverpkg=./internal/core/... ./internal/core/... >/dev/null 2>&1 || {
  echo "FAIL: could not produce $PROFILE for ./internal/core/..."
  go test -coverprofile="$PROFILE" -coverpkg=./internal/core/... ./internal/core/...
  exit 1
}

if [ ! -f "$PROFILE" ]; then
  echo "FAIL: $PROFILE was not written."
  exit 1
fi

# Sum numStmt (field 2) and covered numStmt (field 2 where field 3 > 0),
# skipping the "mode:" header.
total=$(awk 'NR > 1 { t += $2 } END { print t + 0 }' "$PROFILE")
covered=$(awk 'NR > 1 && $3 > 0 { c += $2 } END { print c + 0 }' "$PROFILE")

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
