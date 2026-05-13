#!/usr/bin/env sh
set -eu

if ! command -v wasmtime >/dev/null 2>&1; then
  echo "wasmtime is required; run inside nix develop" >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
export KIZU_CACHE_DIR="$tmp/cache"

run_example() {
  name="$1"
  file="$2"
  want="$3"
  wat="$tmp/$name.wat"

  go run ./cmd/kizu build --target wasm32-wasi "$file" > "$wat"
  got="$(wasmtime "$wat")"
  if [ "$got" != "$want" ]; then
    printf '%s: got:\n%s\nwant:\n%s\n' "$name" "$got" "$want" >&2
    exit 1
  fi
}

run_example "hello" "examples/hello.kizu" "hello, kizu"
run_example "functions" "examples/functions.kizu" "3"
run_example "variables" "examples/variables.kizu" "alice
31"
run_example "if" "examples/if.kizu" "adult"
run_example "while" "examples/while.kizu" "0
1
2"
