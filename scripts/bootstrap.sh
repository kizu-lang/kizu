#!/usr/bin/env sh
# Builds the compiler from the pinned seed release and runs the gate on it:
# the only path that builds compiler/.
#
#   scripts/bootstrap.sh [--check-only | --stage1-only [--opt]]
#
# The full form, which CI runs on every platform:
#   1. the seed checks and tests compiler/, which keeps compiler/ inside what
#      the seed can build (docs/bootstrap.md);
#   2. the seed builds stage1, and stage1 checks and tests compiler/, builds
#      the test harness, and checks the sources the tree derives from it
#      (scripts/check-sources.sh);
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
# built, passes `check compiler`, and builds the harness, `stage1 test compiler`,
# the harness/source gates, and stage2 bootstrap use nothing of each other.
# The full form therefore runs four lanes side by side:
#   - seed-test:        `seed test compiler` (started immediately)
#   - stage1-test:      `stage1 test compiler` (started once stage1 setup ends)
#   - stage1-gates:     check-sources, harness tests/behavior/rebuild/lsp/lint, unicode
#   - stage1-bootstrap: stage2 build & fixed-point bootstrap check
# Each lane writes its own log, shown once all are done, and the run fails if
# any lane does. Every lane gets a TMPDIR of its own. The stage1 lanes share
# KIZU_CACHE_DIR (stage1 stamps scratch/temp files with its PID and renames
# metadata atomically); seed-test gets its own cache directory so the pinned
# seed release never races with stage1.
#
# --check-only stops after the seed and stage1 have checked compiler/; nothing
# is tested. --stage1-only builds stage1 and nothing else (pass --opt to build
# an optimized ThinLTO stage1). Either way stage1 is left at
# target/seed/stage1/bin/kizu.
#
# The seed reads the std it was released with (lib/kizu in its archive), so
# KIZU_LIB_DIR is cleared for it. Stage1 and stage2 are the current compiler
# and read the current tree's lib/kizu.
set -eu

mode=full
stage1_opt="${KIZU_STAGE1_OPT:-}"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --check-only)
      if [ "$mode" != full ]; then echo "usage: $0 [--check-only | --stage1-only [--opt]]" >&2; exit 2; fi
      mode=check
      ;;
    --stage1-only)
      if [ "$mode" != full ]; then echo "usage: $0 [--check-only | --stage1-only [--opt]]" >&2; exit 2; fi
      mode=stage1
      ;;
    --opt)
      stage1_opt=1
      ;;
    *)
      echo "usage: $0 [--check-only | --stage1-only [--opt]]" >&2
      exit 2
      ;;
  esac
  shift
done

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

can_thinlto() {
  case "$(uname -s)" in
    Darwin) return 0 ;;
    Linux) command -v ld.lld >/dev/null 2>&1 ;;
    *) return 1 ;;
  esac
}

build_stage1() {
  mkdir -p "$(dirname "$stage1")"
  use_opt=0
  if [ "$stage1_opt" = "1" ]; then
    use_opt=1
  fi
  if [ "$use_opt" -eq 1 ]; then
    if can_thinlto; then
      echo "stage1 build: --backend llvm --opt --thinlto"
      KIZU_LIB_DIR='' "$seed" build --target native --backend llvm --opt --thinlto -o "$stage1" compiler
    else
      echo "stage1 build: --backend llvm --opt"
      KIZU_LIB_DIR='' "$seed" build --target native --backend llvm --opt -o "$stage1" compiler
    fi
  else
    KIZU_LIB_DIR='' "$seed" build --target native -o "$stage1" compiler
  fi

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
  export TMPDIR="$lanes/stage1-gates.tmp"
  mkdir -p "$TMPDIR"

  KIZU_FMT_JOBS="${KIZU_FMT_JOBS:-2}" scripts/check-sources.sh "$stage1"

  "$stage1" test compiler/tests/harness
  "$harness" behavior "$stage1"
  "$harness" rebuild "$stage1"
  "$harness" lsp "$stage1"
  "$harness" lint "$stage1"
  scripts/check-unicode.sh "$stage1"
}

stage1_bootstrap_lane() {
  export KIZU_LIB_DIR="$root/lib/kizu"
  export TMPDIR="$lanes/stage1-bootstrap.tmp"
  mkdir -p "$TMPDIR" "$(dirname "$stage2")"
  "$stage1" build --target native --backend llvm -o "$stage2" compiler
  "$harness" bootstrap "$stage2"
}

lanes="$root/target/seed/lanes"
rm -rf "$lanes"
mkdir -p "$lanes"
if [ -n "${KIZU_CACHE_DIR:-}" ]; then
  seed_test_cache="$KIZU_CACHE_DIR/seed-test"
else
  seed_test_cache="$root/target/seed/seed-test-cache"
fi

(set -x; seed_test_lane) >"$lanes/seed-test.log" 2>&1 &
seed_test_pid=$!
(
  set -x
  build_stage1
  "$stage1" check compiler
  mkdir -p "$(dirname "$harness")"
  "$stage1" build --target native -o "$harness" compiler/tests/harness
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
(set -x; stage1_bootstrap_lane) >"$lanes/stage1-bootstrap.log" 2>&1 &
stage1_bootstrap_pid=$!
trap 'kill "$seed_test_pid" "$stage1_test_pid" "$stage1_gates_pid" "$stage1_bootstrap_pid" 2>/dev/null' INT TERM

seed_test_status=0
wait "$seed_test_pid" || seed_test_status=$?
stage1_test_status=0
wait "$stage1_test_pid" || stage1_test_status=$?
stage1_gates_status=0
wait "$stage1_gates_pid" || stage1_gates_status=$?
stage1_bootstrap_status=0
wait "$stage1_bootstrap_pid" || stage1_bootstrap_status=$?
trap - INT TERM

for lane in seed-test stage1-test stage1-gates stage1-bootstrap; do
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
if [ "$stage1_bootstrap_status" -ne 0 ]; then
  echo "lane stage1-bootstrap failed with status $stage1_bootstrap_status; its last command is the one above that failed" >&2
  failed=1
fi
exit "$failed"
