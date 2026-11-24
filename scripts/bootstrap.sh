#!/usr/bin/env sh
# Builds the compiler from the pinned seed release and runs the gate on it:
# the only path that builds compiler/.
#
#   scripts/bootstrap.sh [--check-only | --stage1-only]
#
# The full form, which CI runs on every platform:
#   1. the seed checks and tests compiler/, which keeps compiler/ inside what
#      the seed can build (docs/bootstrap.md);
#   2. the seed builds stage1, and stage1 checks and tests compiler/ and
#      checks the sources the tree derives from it (scripts/check-sources.sh);
#   3. stage1 runs the test harness (compiler/tests/harness): its own tests,
#      and the behavior suite, the conformance cases that cross the CLI
#      boundary (examples/, tests/behavior), and the Unicode conformance
#      files on native and wasm32-browser (scripts/check-unicode.sh);
#   4. stage1 builds stage2, and the harness requires stage2 to rebuild
#      itself byte for byte. Stage2 is the one asked: stage1 was compiled by
#      the seed, and only a compiler the current source compiled is expected
#      to reproduce itself. Both builds name --backend llvm: bootstrap stays
#      on LLVM, which `kizu build` defaults to only where the direct backend
#      is not the target's.
#
# Step 1's test and steps 2-4 use nothing of each other, and once stage1 is
# built and passes `check compiler`, `stage1 test compiler` uses nothing of
# the remaining gates (check-sources, harness, unicode, stage2 bootstrap).
# The full form therefore runs three lanes side by side:
#   - seed-test:    `seed test compiler` (started immediately)
#   - stage1-test:  `stage1 test compiler` (started once stage1 builds & checks)
#   - stage1-gates: check-sources, harness, unicode, stage2 build & bootstrap
# Each lane writes its own log, shown once all are done, and the run fails if
# any lane does. Every lane gets a TMPDIR and a build cache of its own
# (subdirectories of KIZU_CACHE_DIR, which the cache does not read, or under
# target/seed/ when KIZU_CACHE_DIR is unset): concurrent lanes may build the
# same inputs, and the scratch names they write are stamped with the time,
# not the process.
#
# --check-only stops after the seed and stage1 have checked compiler/; nothing
# is tested. --stage1-only builds stage1 and nothing else. Either way stage1
# is left at target/seed/stage1/bin/kizu.
#
# The seed reads the std it was released with (lib/kizu in its archive), so
# KIZU_LIB_DIR is cleared for it. Stage1 and stage2 are the current compiler
# and read the current tree's lib/kizu.
set -eu

mode=full
case "${1:-}" in
  '') ;;
  --check-only) mode=check ;;
  --stage1-only) mode=stage1 ;;
  *) echo "usage: $0 [--check-only | --stage1-only]" >&2; exit 2 ;;
esac

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

seed="$(scripts/fetch-seed.sh)"
echo "seed: $(KIZU_LIB_DIR='' "$seed" version)"

# The version module compiler/ imports is generated, not checked in.
scripts/gen-selfhost-version.sh

if [ "$mode" != stage1 ]; then KIZU_LIB_DIR='' "$seed" check compiler; fi

stage1="$root/target/seed/stage1/bin/kizu"
stage2="$root/target/seed/stage2/bin/kizu"
harness="$root/target/seed/harness/bin/harness"

build_stage1() {
  rm -rf "$root/target/seed/stage1"
  mkdir -p "$(dirname "$stage1")"
  KIZU_LIB_DIR='' "$seed" build --target native -o "$stage1" compiler

  export KIZU_LIB_DIR="$root/lib/kizu"
  echo "stage1: $("$stage1" version)"
}

if [ "$mode" != full ]; then
  build_stage1
  if [ "$mode" = stage1 ]; then exit 0; fi
  "$stage1" check compiler
  exit 0
fi

seed_test_lane() {
  export KIZU_CACHE_DIR="$seed_test_cache"
  export TMPDIR="$lanes/seed-test.tmp"
  mkdir -p "$KIZU_CACHE_DIR" "$TMPDIR"
  KIZU_LIB_DIR='' "$seed" test compiler
}

stage1_test_lane() {
  export KIZU_LIB_DIR="$root/lib/kizu"
  export TMPDIR="$lanes/stage1-test.tmp"
  mkdir -p "$TMPDIR"
  "$stage1" test compiler
}

stage1_gates_lane() {
  export KIZU_LIB_DIR="$root/lib/kizu"
  export KIZU_CACHE_DIR="$stage1_gates_cache"
  export TMPDIR="$lanes/stage1-gates.tmp"
  mkdir -p "$KIZU_CACHE_DIR" "$TMPDIR"

  KIZU_FMT_JOBS="${KIZU_FMT_JOBS:-2}" scripts/check-sources.sh "$stage1"

  "$stage1" test compiler/tests/harness
  rm -rf "$root/target/seed/harness"
  mkdir -p "$(dirname "$harness")"
  "$stage1" build --target native -o "$harness" compiler/tests/harness
  "$harness" behavior "$stage1"
  "$harness" rebuild "$stage1"
  "$harness" lsp "$stage1"
  "$harness" lint "$stage1"
  scripts/check-unicode.sh "$stage1"

  rm -rf "$root/target/seed/stage2"
  mkdir -p "$(dirname "$stage2")"
  "$stage1" build --target native --backend llvm -o "$stage2" compiler
  "$harness" bootstrap "$stage2"
}

lanes="$root/target/seed/lanes"
rm -rf "$lanes"
mkdir -p "$lanes"
if [ -n "${KIZU_CACHE_DIR:-}" ]; then
  seed_test_cache="$KIZU_CACHE_DIR/seed-test"
  stage1_gates_cache="$KIZU_CACHE_DIR/stage1-gates"
else
  seed_test_cache="$root/target/seed/seed-test-cache"
  stage1_gates_cache="$root/target/seed/stage1-gates-cache"
fi

(set -x; seed_test_lane) >"$lanes/seed-test.log" 2>&1 &
seed_test_pid=$!
(
  set -x
  build_stage1
  "$stage1" check compiler
) >"$lanes/stage1-setup.log" 2>&1 &
stage1_setup_pid=$!
trap 'kill "$seed_test_pid" "$stage1_setup_pid" 2>/dev/null' INT TERM

stage1_setup_status=0
wait "$stage1_setup_pid" || stage1_setup_status=$?
cat "$lanes/stage1-setup.log"
if [ "$stage1_setup_status" -ne 0 ]; then
  kill "$seed_test_pid" 2>/dev/null || true
  wait "$seed_test_pid" 2>/dev/null || true
  exit "$stage1_setup_status"
fi

(set -x; stage1_test_lane) >"$lanes/stage1-test.log" 2>&1 &
stage1_test_pid=$!
(set -x; stage1_gates_lane) >"$lanes/stage1-gates.log" 2>&1 &
stage1_gates_pid=$!
trap 'kill "$seed_test_pid" "$stage1_test_pid" "$stage1_gates_pid" 2>/dev/null' INT TERM

seed_test_status=0
wait "$seed_test_pid" || seed_test_status=$?
stage1_test_status=0
wait "$stage1_test_pid" || stage1_test_status=$?
stage1_gates_status=0
wait "$stage1_gates_pid" || stage1_gates_status=$?
trap - INT TERM

for lane in seed-test stage1-test stage1-gates; do
  echo "==> lane $lane ($lanes/$lane.log)"
  cat "$lanes/$lane.log"
done
failed=0
if [ "$seed_test_status" -ne 0 ]; then
  echo "lane seed-test failed with status $seed_test_status; its last command is the one above that failed" >&2
  failed=1
fi
if [ "$stage1_test_status" -ne 0 ]; then
  echo "lane stage1-test failed with status $stage1_test_status; its last command is the one above that failed" >&2
  failed=1
fi
if [ "$stage1_gates_status" -ne 0 ]; then
  echo "lane stage1-gates failed with status $stage1_gates_status; its last command is the one above that failed" >&2
  failed=1
fi
exit "$failed"
