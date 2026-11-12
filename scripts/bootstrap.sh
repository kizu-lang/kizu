#!/usr/bin/env sh
# Checks and tests compiler/ with the pinned seed release, builds stage1 from
# it, and checks and tests compiler/ again with stage1: the path that builds
# the compiler without the Go one.
#
# The seed reads the std it was released with (lib/kizu in its archive), so
# KIZU_LIB_DIR is cleared for it. Stage1 is the current compiler and reads the
# current tree's lib/kizu.
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

seed="$(scripts/fetch-seed.sh)"
echo "seed: $(KIZU_LIB_DIR='' "$seed" version)"

# The version module compiler/ imports is still written by the Go generator.
go run ./scripts/gen-selfhost-version

KIZU_LIB_DIR='' "$seed" check compiler
KIZU_LIB_DIR='' "$seed" test compiler

stage1="$root/target/seed/stage1/bin/kizu"
rm -rf "$root/target/seed/stage1"
mkdir -p "$(dirname "$stage1")"
KIZU_LIB_DIR='' "$seed" build --target native -o "$stage1" compiler

export KIZU_LIB_DIR="$root/lib/kizu"
echo "stage1: $("$stage1" version)"
"$stage1" check compiler
"$stage1" test compiler
