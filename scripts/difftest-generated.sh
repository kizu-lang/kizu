#!/usr/bin/env sh
# Builds one generated program with the direct backend and through LLVM, runs
# both, and requires the same output and exit status:
#
#   scripts/difftest-generated.sh <kizu> [seed] [cases]
#
# scripts/difftest-gen writes the program. The seed is fixed by default so that
# a pull request is held to the same program every time; a failure names the
# seed and prints the first case that differs.
set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 3 ]; then
  echo "usage: $0 <kizu> [seed] [cases]" >&2
  exit 2
fi
kizu="$1"
seed="${2:-1}"
cases="${3:-4000}"

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
export KIZU_LIB_DIR="$root/lib/kizu"
work="$(mktemp -d "${TMPDIR:-/tmp}/kizu-difftest-generated.XXXXXX")"
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/program/src/data"
"$kizu" run scripts/difftest-gen/main.kizu -- "$seed" "$cases" "$work/program"

run_backend() {
  backend="$1"
  mkdir -p "$work/$backend.tmp"
  if ! TMPDIR="$work/$backend.tmp" "$kizu" build --target native --backend "$backend" -o "$work/$backend" "$work/program" \
    >"$work/$backend.build" 2>&1; then
    echo "1" >"$work/$backend.build_failed"
    return 1
  fi
  status=0
  TMPDIR="$work/$backend.tmp" "$work/$backend" >"$work/$backend.out" 2>"$work/$backend.err" </dev/null || status=$?
  echo "$status" >"$work/$backend.status"
}

run_backend direct &
direct_pid=$!
run_backend llvm &
llvm_pid=$!

direct_ok=0
wait "$direct_pid" || direct_ok=$?
llvm_ok=0
wait "$llvm_pid" || llvm_ok=$?

for backend in direct llvm; do
  if [ -f "$work/$backend.build_failed" ]; then
    echo "difftest generated: seed $seed does not build with $backend" >&2
    sed 's/^/  /' "$work/$backend.build" >&2
    exit 1
  fi
done
if [ "$direct_ok" -ne 0 ] || [ "$llvm_ok" -ne 0 ]; then
  exit 1
fi

if cmp -s "$work/direct.out" "$work/llvm.out" && cmp -s "$work/direct.status" "$work/llvm.status"; then
  echo "difftest generated: seed $seed, $cases cases match"
  exit 0
fi

echo "difftest generated: seed $seed differs (direct exit $(cat "$work/direct.status"), llvm exit $(cat "$work/llvm.status"))" >&2
line="$(cmp "$work/direct.out" "$work/llvm.out" 2>/dev/null | sed -n 's/.*line \([0-9]*\).*/\1/p')"
if [ -n "$line" ]; then
  case_line=$(((line + 1) / 2 * 2 - 1))
  id="$(sed -n "${case_line}p" "$work/llvm.out")"
  echo "first difference: case $id" >&2
  echo "  direct: $(sed -n "$((case_line + 1))p" "$work/direct.out")" >&2
  echo "  llvm:   $(sed -n "$((case_line + 1))p" "$work/llvm.out")" >&2
  sed -n "/^\/\/ case $id\$/,/^\/\/ end $id\$/p" "$work/program/src/main.kizu" >&2
fi
exit 1
