#!/usr/bin/env sh
# Runs the Unicode conformance files against std::unicode natively and on
# wasm32-browser, and requires both to report the same lines:
#
#   scripts/check-unicode.sh <kizu>
#
# scripts/bootstrap.sh runs it with stage1, from the repository root.
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <kizu>" >&2
  exit 2
fi
kizu="$1"

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

out="$root/target/unicode"
mkdir -p "$out"

"$kizu" run tests/unicode >"$out/native.txt"
"$kizu" build --target wasm32-browser --emit wasm -o "$out/unicode.wasm" tests/unicode
node scripts/run-browser-wasm.mjs "$out/unicode.wasm" >"$out/wasm.txt"

if ! cmp -s "$out/native.txt" "$out/wasm.txt"; then
  echo "std::unicode reports differently on native and wasm32-browser:" >&2
  diff "$out/native.txt" "$out/wasm.txt" >&2 || true
  exit 1
fi
