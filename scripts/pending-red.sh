#!/usr/bin/env sh
# Asserts that test/conformance's pendingimpl tests FAIL to compile, and fail for
# the expected reason. docs/06-harness.md §8 point 5, made executable.
set -u

PKG=./test/conformance/
SYMBOLS=test/conformance/pending_symbols.txt

out=$(go test -c -o /dev/null -gcflags=-e -tags pendingimpl "$PKG" 2>&1)
status=$?

# Failure mode 1: it compiles. The symbols now exist.
if [ "$status" -eq 0 ]; then
  echo "FAIL: $PKG compiles under -tags pendingimpl."
  echo "The anchor symbols now exist. Promote I01/I03/I21 into the untagged L2"
  echo "suite (docs/06-harness.md §4) and drop the pendingimpl tag, in the same PR"
  echo "that created them."
  exit 1
fi

# Failure mode 2: it fails, but not for the expected reason. A typo also fails to
# compile, and a gate that accepts any failure proves nothing.
missing=0
while IFS= read -r sym; do
  case "$sym" in ''|\#*) continue ;; esac
  if ! printf '%s\n' "$out" | grep -qF "undefined: $sym"; then
    echo "FAIL: expected the compiler to report 'undefined: $sym'. It did not."
    missing=1
  fi
done < "$SYMBOLS"

if [ "$missing" -ne 0 ]; then
  echo "--- compiler output ---"; printf '%s\n' "$out"
  echo "Fix the test, or update $SYMBOLS in the same commit."
  exit 1
fi

echo "OK: $PKG is pending-red for every symbol in $SYMBOLS."
