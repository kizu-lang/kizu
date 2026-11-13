#!/usr/bin/env sh
# Builds every `run` example twice, with the direct backend and through LLVM,
# runs both, and requires the two to print the same stdout and stderr and
# exit the same way:
#
#   scripts/difftest-direct.sh <kizu>
#
# Both builds name their backend, since the direct one is the default where
# the target has it. The bootstrap gate runs the default, so it runs the
# direct backend on compiler/ and the cases; this is what holds it to LLVM
# on the examples. LLVM is the reference rather than each case's `output:` block:
# that block does not hold stderr or the exit status, and the run-fails cases
# promise only a substring.
#
# Each binary runs the way the behavior harness runs a case: from the
# repository root, with the case's `-- args` and `env:` bindings, stdin
# closed, and a TMPDIR of the backend's own. The target has to be one the direct
# backend takes (Darwin or Linux arm64, Linux or Windows x86_64).
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <kizu>" >&2
  exit 2
fi
kizu="$1"

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
# The compiler under test is the current one, so it reads the current std.
export KIZU_LIB_DIR="$root/lib/kizu"
work="$(mktemp -d "${TMPDIR:-/tmp}/kizu-difftest.XXXXXX")"
trap 'rm -rf "$work"' EXIT

# One example: build both, run both, compare. Prints MATCH, MISMATCH, or
# FAIL (a build that did not succeed) with the name.
check() {
  file="$1"
  name="$(basename "$file" .kizu)"
  directive="$(grep -m1 -E '^// run( |$|-fails)' "$file")"
  args="$(printf '%s\n' "$directive" | sed -n 's/^.* -- //p')"
  for backend in direct llvm; do
    mkdir -p "$work/$backend/tmp"
    if ! "$kizu" build --target native --backend "$backend" -o "$work/$backend/$name" "$file" \
      >"$work/$backend/$name.build" 2>&1; then
      echo "FAIL $name ($backend build)"
      sed 's/^/  /' "$work/$backend/$name.build"
      return 1
    fi
    # The case's env: bindings, then the binary and its -- args. The args
    # are words with no quoting in them, so the shell splits them as the
    # harness does. The group's stderr is dropped so that the shell's report
    # of a binary that aborts, as the run-fails cases do, stays out of the
    # log; the binary's stderr is kept in its file. A subshell would not
    # do: dash reports the abort on the stderr it started with.
    # shellcheck disable=SC2046,SC2086
    {
      env TMPDIR="$work/$backend/tmp" PWD="$root" \
        $(sed -n 's/^\/\/ env: //p' "$file") \
        "$work/$backend/$name" $args </dev/null \
        >"$work/$backend/$name.out" 2>"$work/$backend/$name.err"
    } 2>/dev/null && status=0 || status=$?
    echo "$status" >"$work/$backend/$name.status"
  done
  for part in out err status; do
    if ! cmp -s "$work/direct/$name.$part" "$work/llvm/$name.$part"; then
      echo "MISMATCH $name ($part)"
      diff "$work/llvm/$name.$part" "$work/direct/$name.$part" | head -20 | sed 's/^/  /'
      return 1
    fi
  done
  echo "MATCH $name (exit $(cat "$work/direct/$name.status"))"
}

matched=0
failed=0
for file in examples/*.kizu; do
  grep -q -E '^// run( |$|-fails)' "$file" || continue
  if check "$file"; then matched=$((matched + 1)); else failed=$((failed + 1)); fi
done
echo "difftest direct: $matched match, $failed differ or fail"
[ "$failed" -eq 0 ]
