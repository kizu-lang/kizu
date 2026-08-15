#!/usr/bin/env sh
set -eu

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

export KIZU_CACHE_DIR="$tmp/cache"
cp examples/hello.kizu "$tmp/hello.kizu"

run() {
  name="$1"
  shift

  out="$tmp/time.txt"
  if /usr/bin/time -p "$@" >/dev/null 2>"$out"; then
    elapsed="$(awk '/^real / { print $2 }' "$out")"
    printf '%s\t%ss\n' "$name" "$elapsed"
  else
    cat "$out" >&2
    return 1
  fi
}

run "cold llvm" go run ./cmd/kizu build --emit-llvm "$tmp/hello.kizu"
run "warm llvm" go run ./cmd/kizu build --emit-llvm "$tmp/hello.kizu"
run "cold wasm" go run ./cmd/kizu build --target wasm32-wasi "$tmp/hello.kizu"
run "warm wasm" go run ./cmd/kizu build --target wasm32-wasi "$tmp/hello.kizu"
run "cold run" go run ./cmd/kizu run "$tmp/hello.kizu"
run "warm run" go run ./cmd/kizu run "$tmp/hello.kizu"
printf '\n' >> "$tmp/hello.kizu"
run "single-file edit llvm" go run ./cmd/kizu build --emit-llvm "$tmp/hello.kizu"
run "single-file edit run" go run ./cmd/kizu run "$tmp/hello.kizu"
run "cache status" go run ./cmd/kizu cache status
printf 'cache size\t%s\n' "$(du -sh "$KIZU_CACHE_DIR" | awk '{ print $1 }')"
