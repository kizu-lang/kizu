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
# A loop in a called function repeats the block names of the caller's loop, so
# this crosses the phi copies of two dispatch loops in one module.
run_example "loop_in_called_function" "examples/loop_in_called_function.kizu" "0
0
1
3"
# A function pointer reaches wasm as a table index, so this crosses the header's
# type declaration, the elem segment, and call_indirect.
run_example "function_pointer" "examples/function_pointer.kizu" "42
-21"
# Aggregates use one wasm32 memory layout across construction, field access,
# borrows, direct calls, indirect calls, returns, and recursive frames.
run_example "struct" "examples/struct.kizu" "alice
30"
run_example "field_assignment" "examples/field_assignment.kizu" "bob
31"
run_example "match" "examples/match.kizu" "blue"
run_example "union" "examples/union.kizu" "10
name
point"
run_example "mutable_borrow_nested_field" "examples/mutable_borrow_nested_field.kizu" "9
2"
run_example "aggregate_calls" "examples/aggregate_calls.kizu" "4
3"
