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
#
# Matching must be exact-identifier, not substring: `grep -F "undefined: X"`
# also matches `undefined: XZZZUnrelated`, so a future symbol that merely
# starts with a tracked name (e.g. unit.StatusHistory next to unit.Status)
# would mask that tracked name's own check and let the gate report OK while
# the real anchor is gone. Extract every `undefined: <ident>` the compiler
# actually reported into a set, and require exact membership.
undefined_syms=$(printf '%s\n' "$out" | grep -oE 'undefined: [A-Za-z_][A-Za-z0-9_.]*' | sed 's/^undefined: //')
tracked_syms=$(grep -v '^#' "$SYMBOLS" | grep -v '^$')

missing=0
while IFS= read -r sym; do
  case "$sym" in ''|\#*) continue ;; esac
  matched=0
  for got in $undefined_syms; do
    if [ "$got" = "$sym" ]; then
      matched=1
      break
    fi
  done
  if [ "$matched" -eq 0 ]; then
    echo "FAIL: expected the compiler to report 'undefined: $sym'. It did not."
    missing=1
  fi
done < "$SYMBOLS"

# Reverse direction: every symbol the compiler actually reported as undefined
# must be listed in $SYMBOLS too. Without this, a line quietly deleted from
# $SYMBOLS (while the test still references the symbol) shrinks what gets
# checked with no signal — the forward check above only proves every LISTED
# symbol is undefined, it says nothing about whether the list is complete.
extra=0
for got in $undefined_syms; do
  listed=0
  for sym in $tracked_syms; do
    if [ "$got" = "$sym" ]; then
      listed=1
      break
    fi
  done
  if [ "$listed" -eq 0 ]; then
    echo "FAIL: the compiler reported 'undefined: $got', which is not listed in $SYMBOLS."
    extra=1
  fi
done

if [ "$missing" -ne 0 ] || [ "$extra" -ne 0 ]; then
  echo "--- compiler output ---"; printf '%s\n' "$out"
  echo "Fix the test, or update $SYMBOLS in the same commit."
  exit 1
fi

echo "OK: $PKG is pending-red for every symbol in $SYMBOLS."
