#!/usr/bin/env sh
# Checks and tests compiler/ with the pinned seed release, builds stage1 from
# it, and checks and tests compiler/ again with stage1: the path that builds
# the compiler without the Go one.
#
# With --check-only, both compilers only check compiler/; nothing is tested.
# Pull requests run that form, pushes to main the full one.
#
# The seed reads the std it was released with (lib/kizu in its archive), so
# KIZU_LIB_DIR is cleared for it. Stage1 is the current compiler and reads the
# current tree's lib/kizu.
set -eu

tests=1
case "${1:-}" in
  '') ;;
  --check-only) tests=0 ;;
  *) echo "usage: $0 [--check-only]" >&2; exit 2 ;;
esac

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

seed="$(scripts/fetch-seed.sh)"
echo "seed: $(KIZU_LIB_DIR='' "$seed" version)"

# The version module compiler/ imports is generated, not checked in.
scripts/gen-selfhost-version.sh

KIZU_LIB_DIR='' "$seed" check compiler
if [ "$tests" = 1 ]; then KIZU_LIB_DIR='' "$seed" test compiler; fi

stage1="$root/target/seed/stage1/bin/kizu"
rm -rf "$root/target/seed/stage1"
mkdir -p "$(dirname "$stage1")"
KIZU_LIB_DIR='' "$seed" build --target native -o "$stage1" compiler

export KIZU_LIB_DIR="$root/lib/kizu"
echo "stage1: $("$stage1" version)"
"$stage1" check compiler
if [ "$tests" = 1 ]; then "$stage1" test compiler; fi
