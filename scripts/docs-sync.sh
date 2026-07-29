#!/usr/bin/env sh
# Core logic for .github/workflows/docs-sync.yml, extracted so it is testable
# without GitHub Actions (test/harness/docs_sync_test.go). CLAUDE.md
# non-negotiable #1, mechanized for the brain's behavior: "docs/02-cognitive-
# core.md governs. If the code and that doc diverge, either the code gets
# fixed or the doc gets updated in the same PR. Never left to drift
# silently."
#
# Blocks when internal/core/** changed without docs/02-cognitive-core.md also
# changing, unless the PR carries the `no-spec-change` label.
#
# Usage: scripts/docs-sync.sh <labels-json>
#   <labels-json>  a JSON array of label names, e.g. '["a","b"]' — exactly
#                   the shape toJSON(github.event.pull_request.labels.*.name)
#                   produces.
#   changed files   one path per line, read from stdin (git diff --name-only
#                   output). The workflow computes this diff; this script
#                   only classifies it.
#
# Label matching uses jq on the parsed JSON array, not a substring grep on
# the raw toJSON() text. A grep for '"no-spec-change"' is fooled by a label
# literally named `foo"no-spec-change`: toJSON escapes the embedded quote as
# \", and the raw text still contains "no-spec-change" contiguously —
# measured: LABELS='["foo\"no-spec-change"]' makes that grep match. jq -e
# 'index("no-spec-change")' operates on the decoded array, so a label is
# only a match if it IS "no-spec-change", not if it merely contains that
# substring after escaping. This is not privilege escalation — creating such
# a label needs the same repo permission as applying the real one — but it
# defeats the point of the gate: that skipping the doc is visible and
# attributable, not silently spoofable.
set -eu

LABELS=${1:?"usage: docs-sync.sh <labels-json> < changed-files"}

if ! command -v jq >/dev/null 2>&1; then
  echo "FAIL: this gate needs jq to parse the PR label list safely — a plain"
  echo "grep over toJSON() output is fooled by a label crafted to contain the"
  echo "escaped target string (see this script's header comment). ubuntu-latest"
  echo "runners ship jq by default; install it if running this elsewhere."
  exit 1
fi

changed=$(cat)

core_changed=$(printf '%s\n' "$changed" | grep -c '^internal/core/' || true)
doc_changed=$(printf '%s\n' "$changed" | grep -c '^docs/02-cognitive-core\.md$' || true)

if [ "$core_changed" -eq 0 ]; then
  echo "OK: this PR does not touch internal/core/** — nothing for this gate to check."
  exit 0
fi

if [ "$doc_changed" -gt 0 ]; then
  echo "OK: internal/core/** changed and docs/02-cognitive-core.md changed with it."
  exit 0
fi

if printf '%s' "$LABELS" | jq -e 'index("no-spec-change")' >/dev/null 2>&1; then
  echo "OK: internal/core/** changed with no doc-02 change, and the PR carries"
  echo "the no-spec-change label — an explicit, attributable decision."
  exit 0
fi

echo "FAIL: this PR changes internal/core/** but not docs/02-cognitive-core.md."
echo
echo "internal/core/ holds the brain's decisions, and doc 02 is the source of"
echo "truth for those decisions (CLAUDE.md non-negotiable #1). If the behavior"
echo "changed, doc 02 changes in this same PR. If it did not — a refactor, a"
echo "rename, a pure-performance change — add the no-spec-change label and this"
echo "check will re-run and pass."
echo
echo "Files under internal/core/ in this PR:"
printf '%s\n' "$changed" | grep '^internal/core/' | sed 's/^/  /'
exit 1
