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
  preopen="${4-}"
  wat="$tmp/$name.wat"

  go run ./cmd/kizu build --target wasm32-wasi "$file" > "$wat"
  if [ -n "$preopen" ]; then
    got="$(wasmtime run --dir "$preopen" "$wat")"
  else
    got="$(wasmtime run "$wat")"
  fi
  if [ "$got" != "$want" ]; then
    printf '%s: got:\n%s\nwant:\n%s\n' "$name" "$got" "$want" >&2
    exit 1
  fi
}

run_failure() {
  name="$1"
  file="$2"
  want="$3"
  want_stdout="${4-}"
  preopen="${5-}"
  wat="$tmp/$name.wat"
  stdout="$tmp/$name.stdout"
  stderr="$tmp/$name.stderr"

  go run ./cmd/kizu build --target wasm32-wasi "$file" > "$wat"
  set +e
  if [ -n "$preopen" ]; then
    wasmtime run --dir "$preopen" "$wat" > "$stdout" 2> "$stderr"
  else
    wasmtime run "$wat" > "$stdout" 2> "$stderr"
  fi
  status="$?"
  set -e
  got="$(cat "$stderr")"
  got_stdout="$(cat "$stdout")"
  if [ "$status" -ne 1 ] || [ "$got_stdout" != "$want_stdout" ] || [ "$got" != "$want" ]; then
    printf '%s: status=%s, stdout:\n%s\nstderr:\n%s\nwant status=1, stdout:\n%s\nstderr:\n%s\n' \
      "$name" "$status" "$got_stdout" "$got" "$want_stdout" "$want" >&2
    exit 1
  fi
}

run_status() {
  name="$1"
  file="$2"
  want_code="$3"
  shift 3
  wat="$tmp/$name.wat"
  stdout="$tmp/$name.stdout"
  stderr="$tmp/$name.stderr"

  go run ./cmd/kizu build --target wasm32-wasi "$file" > "$wat"
  set +e
  wasmtime run "$wat" "$@" > "$stdout" 2> "$stderr"
  actual_code="$?"
  set -e
  got_stdout="$(cat "$stdout")"
  got_stderr="$(cat "$stderr")"
  if [ "$actual_code" -ne "$want_code" ] || [ -n "$got_stdout" ] || [ -n "$got_stderr" ]; then
    printf '%s: status=%s, stdout:\n%s\nstderr:\n%s\nwant status=%s and empty output\n' \
      "$name" "$actual_code" "$got_stdout" "$got_stderr" "$want_code" >&2
    exit 1
  fi
}

run_stderr() {
  name="$1"
  file="$2"
  want="$3"
  wat="$tmp/$name.wat"
  stdout="$tmp/$name.stdout"
  stderr="$tmp/$name.stderr"

  go run ./cmd/kizu build --target wasm32-wasi "$file" > "$wat"
  wasmtime run "$wat" > "$stdout" 2> "$stderr"
  got_stdout="$(cat "$stdout")"
  got_stderr="$(cat "$stderr")"
  if [ -n "$got_stdout" ] || [ "$got_stderr" != "$want" ]; then
    printf '%s: stdout:\n%s\nstderr:\n%s\nwant empty stdout and stderr:\n%s\n' \
      "$name" "$got_stdout" "$got_stderr" "$want" >&2
    exit 1
  fi
}

run_io_process() {
  wat="$tmp/std_io_process.wat"
  stdout="$tmp/std_io_process.stdout"
  stderr="$tmp/std_io_process.stderr"
  want="stdout:
1
input.kizu
env-ok
7"

  go run ./cmd/kizu build --target wasm32-wasi examples/std_io_process.kizu > "$wat"
  wasmtime run --env KIZU_TEST_ENV=env-ok "$wat" input.kizu > "$stdout" 2> "$stderr"
  got_stdout="$(cat "$stdout")"
  got_stderr="$(cat "$stderr")"
  if [ "$got_stdout" != "$want" ] || [ -n "$got_stderr" ]; then
    printf 'std_io_process: stdout:\n%s\nstderr:\n%s\nwant stdout:\n%s\nand empty stderr\n' \
      "$got_stdout" "$got_stderr" "$want" >&2
    exit 1
  fi
}

run_example "hello" "examples/hello.kizu" "hello, kizu"
run_example "functions" "examples/functions.kizu" "3"
run_example "slice_checked_access" "examples/slice_checked_access.kizu" "98
98
99"
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
run_example "std_path_edges" "examples/std_path_edges.kizu" ".
/b
.
a/c
.."
# Optionals and recoverable errors share the tagged memory ABI across calls,
# captures, match, orelse, try propagation, and main's success boundary.
run_example "optional_error_flow" "examples/optional_error_flow.kizu" "7
-1
9
-2
11
15
11
15
5
0
-2
9
41
1
42
-2"
run_example "std_mem_box_take" "examples/std_mem_box_take.kizu" "payload"
run_example "std_mem_box_ast" "examples/std_mem_box_ast.kizu" ""
run_example "std_mem_box_cleanup" "examples/std_mem_box_cleanup.kizu" "42
7"
run_example "arena" "examples/arena.kizu" "alice"
run_example "arena_at_mut" "examples/arena_at_mut.kizu" "1
30"
run_example "arena_owner_elements" "examples/arena_owner_elements.kizu" "released
released"
run_example "arena_allocator_refusal" \
  "examples/arena_add_recovers_from_a_full_allocator.kizu" \
  "-1
the buffer ran out, and the program chose what to do"
run_example "std_map" "examples/std_map.kizu" "true
main
1
2"
run_example "std_map_integer_key" "examples/std_map_integer_key.kizu" "101
3
205
2
377
1"
run_example "std_map_owner_value" "examples/std_map_owner_value.kizu" "alice
first
last
smithson"
run_example "map_allocator_refusal" \
  "examples/map_insert_recovers_from_a_full_allocator.kizu" \
  "-1
the buffer ran out, and the program chose what to do"
run_example "std_json_decode_map" "examples/std_json_decode_map.kizu" "alice
bob
carol
7
{\"counts\":{\"alice\":2,\"bob\":1,\"carol\":4},\"total\":7}"
run_example "enum" "examples/enum.kizu" "Color::Green
true"
run_example "std_array_token_list" "examples/std_array_token_list.kizu" "2
TokenKind::Ident
TokenKind::Number"
run_example "std_map_symbol_table" "examples/std_map_symbol_table.kizu" "SymbolKind::Function
false"
run_example "bytes_iter" "examples/bytes_iter.kizu" "10
20
30"
run_example "std_string_join_trim" "examples/std_string_join_trim.kizu" "first  text

 last
0"
run_example "main_exit_status_success" \
  "examples/main_exit_status.kizu" \
  "exiting with success"
run_status "main_exit_status_failure" "examples/main_exit_status.kizu" 1 failure
run_status "main_exit_status_specific" "examples/main_exit_status.kizu" 7 specific code
run_io_process
run_stderr "std_io_stderr" "examples/std_io_stderr.kizu" "diagnostic"
run_example "fs_read" "examples/fs_read.kizu" "kizu fs fixture" "."
run_example "fs_read_dir" "examples/fs_read_dir.kizu" "alpha.txt
examples/fixtures/read_dir/alpha.txt
false
beta.txt
examples/fixtures/read_dir/beta.txt
false
nested
examples/fixtures/read_dir/nested
true" "."
run_example "std_fs_path" "examples/std_fs_path.kizu" "true
16
false
true
true
false
updated
false" "."

# Checked access reports the same source position and dynamic values as the
# native runtime, writes only to stderr, and terminates through WASI proc_exit.
run_failure "slice_index_out_of_bounds" \
  "examples/negative/slice_syntax_index_out_of_bounds.kizu" \
  "runtime error: index out of bounds at 2:21
note: index is 3, length is 3"
run_failure "slice_range_out_of_bounds" \
  "examples/negative/slice_syntax_range_out_of_bounds.kizu" \
  "runtime error: range out of bounds at 2:22
note: range is 0..4, length is 3"
run_failure "arena_handle_from_another_instance" \
  "examples/negative/arena_handle_from_another_instance.kizu" \
  "runtime error: invalid arena handle" \
  "1
2"
run_failure "std_map_owner_value_overwrite" \
  "examples/std_map_owner_value_overwrite.kizu" \
  "runtime error: std::map::Map: insert would drop the owner value already stored for this key"
run_failure "testing_fail" \
  "examples/negative/std_testing_run_fail.kizu" \
  "runtime error: custom failure"
run_failure "testing_expect_equal_int" \
  "examples/negative/std_testing_run_expect_equal.kizu" \
  "runtime error: expected 4, got 3"
run_failure "testing_expect_equal_bool" \
  "examples/negative/std_testing_run_expect_equal_bool.kizu" \
  "runtime error: expected true, got false"
run_failure "testing_expect_equal_bytes" \
  "examples/negative/std_testing_run_expect_equal_bytes.kizu" \
  "runtime error: expected \"token\", got \"lexer\""
run_failure "std_process_arg_bounds" \
  "examples/negative/std_process_arg_bounds.kizu" \
  "runtime error: std::process::Error::ArgIndexOutOfBounds"
run_failure "std_io_failing_write" \
  "examples/negative/std_io_failing_write.kizu" \
  "runtime error: std::io::Error::IoFailing"
run_failure "fs_read_without_preopen" \
  "examples/fs_read.kizu" \
  "runtime error: std::fs::Error::PermissionDenied"
run_failure "fs_read_limit_exceeded" \
  "examples/negative/fs_read_limit_exceeded.kizu" \
  "runtime error: std::fs::Error::LimitExceeded" \
  "" \
  "."
run_failure "fs_read_missing" \
  "examples/negative/fs_read_missing.kizu" \
  "runtime error: std::fs::Error::NotFound" \
  "" \
  "."
run_failure "fs_failing_io" \
  "examples/negative/fs_failing_io.kizu" \
  "runtime error: std::fs::Error::IoFailing" \
  "" \
  "."
