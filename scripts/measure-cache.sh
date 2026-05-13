#!/usr/bin/env sh
set -eu

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

export KIZU_CACHE_DIR="$tmp/cache"
cp examples/hello.kizu "$tmp/hello.kizu"

run() {
  name="$1"
  shift

  start="$(date +%s)"
  "$@" >/dev/null
  end="$(date +%s)"

  elapsed="$((end - start))"
  printf '%s\t%ss\n' "$name" "$elapsed"
}

run "cold llvm" go run ./cmd/kizu build --emit-llvm "$tmp/hello.kizu"
run "warm llvm" go run ./cmd/kizu build --emit-llvm "$tmp/hello.kizu"
run "cold wasm" go run ./cmd/kizu build --target wasm32-wasi "$tmp/hello.kizu"
run "warm wasm" go run ./cmd/kizu build --target wasm32-wasi "$tmp/hello.kizu"
run "no-op why" go run ./cmd/kizu why-rebuild "$tmp/hello.kizu"
printf '\n' >> "$tmp/hello.kizu"
run "small edit why" go run ./cmd/kizu why-rebuild "$tmp/hello.kizu"
run "cache status" go run ./cmd/kizu cache status
