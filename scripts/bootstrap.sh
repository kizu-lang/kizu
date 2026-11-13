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
#      boundary (examples/, tests/behavior);
#   4. stage1 builds stage2, and the harness requires stage2 to rebuild
#      itself byte for byte. Stage2 is the one asked: stage1 was compiled by
#      the seed, and only a compiler the current source compiled is expected
#      to reproduce itself. Both builds name --backend llvm: bootstrap stays
#      on LLVM, which `kizu build` defaults to only where the direct backend
#      is not the target's.
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
if [ "$mode" = full ]; then KIZU_LIB_DIR='' "$seed" test compiler; fi

stage1="$root/target/seed/stage1/bin/kizu"
rm -rf "$root/target/seed/stage1"
mkdir -p "$(dirname "$stage1")"
KIZU_LIB_DIR='' "$seed" build --target native -o "$stage1" compiler

export KIZU_LIB_DIR="$root/lib/kizu"
echo "stage1: $("$stage1" version)"
if [ "$mode" = stage1 ]; then exit 0; fi
"$stage1" check compiler
if [ "$mode" = check ]; then exit 0; fi
"$stage1" test compiler
scripts/check-sources.sh "$stage1"

"$stage1" test compiler/tests/harness
"$stage1" run compiler/tests/harness -- behavior "$stage1"

stage2="$root/target/seed/stage2/bin/kizu"
rm -rf "$root/target/seed/stage2"
mkdir -p "$(dirname "$stage2")"
"$stage1" build --target native --backend llvm -o "$stage2" compiler
"$stage1" run compiler/tests/harness -- bootstrap "$stage2"
