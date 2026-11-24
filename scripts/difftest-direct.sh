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
  worker_work="$1"
  file="$2"
  name="$(basename "$file" .kizu)"
  directive="$(grep -m1 -E '^// run( |$|-fails)' "$file")"
  args="$(printf '%s\n' "$directive" | sed -n 's/^.* -- //p')"
  for backend in direct llvm; do
    tmp_dir="$worker_work/$backend/tmp/$name"
    mkdir -p "$tmp_dir"
    if ! "$kizu" build --target native --backend "$backend" -o "$worker_work/$backend/$name" "$file" \
      >"$worker_work/$backend/$name.build" 2>&1; then
      echo "FAIL $name ($backend build)"
      sed 's/^/  /' "$worker_work/$backend/$name.build"
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
      env TMPDIR="$tmp_dir" PWD="$root" \
        $(sed -n 's/^\/\/ env: //p' "$file") \
        "$worker_work/$backend/$name" $args </dev/null \
        >"$worker_work/$backend/$name.out" 2>"$worker_work/$backend/$name.err"
    } 2>/dev/null && status=0 || status=$?
    echo "$status" >"$worker_work/$backend/$name.status"
  done
  for part in out err status; do
    if ! cmp -s "$worker_work/direct/$name.$part" "$worker_work/llvm/$name.$part"; then
      echo "MISMATCH $name ($part)"
      diff "$worker_work/llvm/$name.$part" "$worker_work/direct/$name.$part" | head -20 | sed 's/^/  /'
      return 1
    fi
  done
  echo "MATCH $name (exit $(cat "$worker_work/direct/$name.status"))"
}

default_jobs() {
  n=''
  if command -v nproc >/dev/null 2>&1; then
    n="$(nproc 2>/dev/null || true)"
  elif command -v getconf >/dev/null 2>&1; then
    n="$(getconf _NPROCESSORS_ONLN 2>/dev/null || true)"
  elif command -v sysctl >/dev/null 2>&1; then
    n="$(sysctl -n hw.ncpu 2>/dev/null || true)"
  fi
  case "$n" in
    '' | *[!0-9]* | 0) n=4 ;;
  esac
  if [ "$n" -gt 8 ]; then n=8; fi
  echo "$n"
}

jobs="${KIZU_DIFFTEST_JOBS:-$(default_jobs)}"
case "$jobs" in
  '' | *[!0-9]* | 0)
    echo "error: KIZU_DIFFTEST_JOBS must be a positive integer (got '$jobs')" >&2
    exit 2
    ;;
esac

# Partition examples round-robin across workers so alphabetically grouped
# heavy cases (e.g. http_*) spread evenly instead of bunching on one worker.
total=0
for file in examples/*.kizu; do
  grep -q -E '^// run( |$|-fails)' "$file" || continue
  shard=$((total % jobs))
  printf '%s\n' "$file" >>"$work/shard-$shard.list"
  total=$((total + 1))
done

if [ "$total" -eq 0 ]; then
  echo "difftest direct: 0 match, 0 differ or fail"
  exit 0
fi

if [ "$jobs" -gt "$total" ]; then
  jobs="$total"
fi

cache_base="${KIZU_CACHE_DIR:-$work/cache}"

run_worker() {
  wid="$1"
  worker_work="$work/w$wid"
  worker_cache="$cache_base/worker-$wid"
  mkdir -p "$worker_work/direct" "$worker_work/llvm" "$worker_work/build-tmp" "$worker_cache"
  export KIZU_CACHE_DIR="$worker_cache"
  export TMPDIR="$worker_work/build-tmp"
  w_matched=0
  w_failed=0
  while IFS= read -r file; do
    if check "$worker_work" "$file"; then
      w_matched=$((w_matched + 1))
    else
      w_failed=$((w_failed + 1))
    fi
  done <"$work/shard-$wid.list"
  echo "$w_matched" >"$worker_work/matched"
  echo "$w_failed" >"$worker_work/failed"
}

pids=''
w=0
while [ "$w" -lt "$jobs" ]; do
  run_worker "$w" >"$work/w$w.log" 2>&1 &
  pids="$pids $!"
  w=$((w + 1))
done

# shellcheck disable=SC2086
trap 'kill $pids 2>/dev/null || true; rm -rf "$work"' INT TERM

for pid in $pids; do
  wait "$pid" || true
done
trap - INT TERM

matched=0
failed=0
w=0
while [ "$w" -lt "$jobs" ]; do
  cat "$work/w$w.log"
  w_m="$(cat "$work/w$w/matched" 2>/dev/null || echo 0)"
  w_f="$(cat "$work/w$w/failed" 2>/dev/null || echo 1)"
  matched=$((matched + w_m))
  failed=$((failed + w_f))
  w=$((w + 1))
done
echo "difftest direct: $matched match, $failed differ or fail"
[ "$failed" -eq 0 ]
