#!/usr/bin/env sh
# The merge gate of the reminder phrasing set (design D1c, ADR-0033 point 5),
# run as a step of ci.yml's required `lint` job. It fails while any phrasing
# still awaits its recording, so a branch that adds phrasings cannot merge
# before a maintainer has recorded them (`nooma doctor --record`). It also
# rejects vault-like content in any case: the set is synthetic only.
#
# It is NOT part of `make check-all`, on purpose: check-all runs before every
# push and must pass while cases still await recording. CLAUDE.md's Workflow
# section and docs/06-harness.md §6 state the exception. The script is tested
# directly (test/harness/recordings_complete_test.go), like docs-sync.sh.
#
# ci.yml runs it once per set: the reminders (the default) and the relation
# judge's set, testdata/llm/judge (doc 02 §4).
#
# Usage: scripts/recordings-complete.sh [dir]   (default testdata/llm/reminders)
set -u

dir=${1:-testdata/llm/reminders}
status=0

# A set that is not there is not a recorded set. Without this, renaming the
# directory would turn the gate into a pass that checks nothing.
if ! ls "$dir"/*.json >/dev/null 2>&1; then
  echo "FAIL: no phrasings found in $dir (the directory is missing, renamed or empty); this gate checks the phrasing set and must find it."
  exit 1
fi

pending=$(grep -lE '"record"[[:space:]]*:[[:space:]]*true' "$dir"/*.json 2>/dev/null || true)
if [ -n "$pending" ]; then
  echo "FAIL: these phrasings still await their recording (\"record\": true):"
  printf '%s\n' "$pending" | sed 's/^/  /'
  echo "Record them with: nooma doctor --record $dir <vault>   (docs/06-harness.md §5)"
  status=1
fi

# A home or system path, a Windows path, the vault directory, a ULID-shaped id
# (26 Crockford base32 characters) or an e-mail address.
leaks='/(home|Users|root|tmp|var|mnt)/[^[:space:]"]|[A-Za-z]:\\+[A-Za-z]|\.nooma|(^|[^0-9A-Za-z])[0-7][0-9A-HJKMNP-TV-Z]{25}([^0-9A-Za-z]|$)|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}'
leaky=$(grep -lE "$leaks" "$dir"/*.json 2>/dev/null || true)
if [ -n "$leaky" ]; then
  echo "FAIL: these phrasings hold vault-like content (a path, a ULID or an e-mail address); the set is synthetic only:"
  printf '%s\n' "$leaky" | sed 's/^/  /'
  status=1
fi

if [ "$status" -eq 0 ]; then
  echo "OK: every phrasing in $dir is recorded and synthetic."
fi
exit "$status"
