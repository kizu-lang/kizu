# Kizu Examples Catalog

This directory is the user-visible catalog for Kizu language behavior.

Executable and negative examples are listed in
[`tests/conformance/`](../tests/conformance/). The Go test runner reads the
versioned manifests so future compiler implementations can reuse them as the
compatibility corpus.

Run the full catalog through the normal project gate:

```sh
just verify
```

or directly:

```sh
go test ./...
```

## Positive Examples

| Feature | Example | Expected behavior |
| --- | --- | --- |
| `fn`, `main`, `print`, implicit `void` | `hello.kizu` | prints `hello, kizu` |
| `let`, `var`, assignment | `variables.kizu` | updates mutable `age` |
| integer arithmetic | `arithmetic.kizu` | prints `7` |
| function call and explicit return type | `functions.kizu` | prints `3` |
| empty `return` in `void` function | `return.kizu` | exits early and prints `done` |
| `if` / `else` | `if.kizu` | prints `adult` |
| `if` / `match` expressions and optional semicolons | `control_expressions.kizu` | asserts expression branch values |
| `while` | `while.kizu` | prints `0`, `1`, `2` |
| `break` / `continue` | `break_continue.kizu` | controls a `while` loop |
| labeled loop branch | `labeled_loop.kizu` | exits an outer loop explicitly |
| bounded integer `for` | `for.kizu` | iterates an i64 half-open range |
| infinite loop spelling | `infinite_loop.kizu` | uses `while true` plus `break` |
| `struct` and field access | `struct.kizu` | prints `alice`, `30` |
| mutable struct field assignment | `field_assignment.kizu` | updates fields on a `var` binding |
| borrow parameter | `borrow.kizu` | borrow does not move the owner |
| last-use borrow | `last_use_borrow.kizu` | local borrow ends at its final use |
| borrow call then owner move | `borrow_call_then_move.kizu` | a call-scoped borrow does not block a later owner move |
| one-level field borrow | `field_borrow.kizu` | updates a disjoint field while a field is borrowed |
| copy through borrow dereference | `borrow_deref_copy.kizu` | copies an `i64` through `.*` |
| copy values after owner-like calls | `copy_after_move.kizu` | `i64` remains usable after passing to a function |
| mutable borrow parameter | `mutable_borrow.kizu` | `&mut` updates through explicit `.*` dereference |
| `arena<T>` / `handle<T>` | `arena.kizu` | stores and reads a struct through a handle |
| `!T`, `error`, `try` | `error_union_try.kizu` | propagates success and prints `1` |
| `!void` and `try` | `error_union_void.kizu` | propagates success without a payload |
| custom error type handling | `custom_error.kizu` | handles a domain error with `union` and `match` |
| typed custom error propagation | `typed_error.kizu` | uses `ConfigError!i64` with `try` |
| explicit typed error adaptation | `typed_error_cast.kizu` | maps `!T` into `ErrorType!T` with `cast` |
| limited `comptime` | `comptime.kizu` | evaluates compile-time expressions |
| Zig/C-style tag `enum` | `enum.kizu` | prints and compares enum tags |
| simple enum `match` | `match.kizu` | dispatches exhaustive enum arms |
| tagged `union` with payloads | `union.kizu` | binds payload values in `match` arms |
| unsafe wrapper boundary | `unsafe_wrapper.kizu` | check-only extern wrapper; caller owns the unsafe obligation |
| raw pointer spelling and unsafe pointer ops | `pointer_policy.kizu` | check-only pointer policy example |
| combined v0.1 application | `user_registry.kizu` | exercises multiple v0.1 features together |
| `contract`, `satisfy`, `&Dyn<Contract>` | `contract_writer.kizu` | dynamic dispatch through explicit satisfaction |
| `Io` capability and `TaskGroup` | `task_group.kizu` | spawns and awaits a structured task |
| selectable `Io` implementations | `io_runtime.kizu` | uses blocking, threaded, and failing Io constructors |
| task cancellation cleanup | `task_cancel.kizu` | waits for a threaded task and discards its result |
| explicit-Io file read | `fs_read.kizu` | reads a fixture through `std::fs::read_file` |
| task-based file read | `fs_task.kizu` | reads a fixture from a spawned task |
| pure path helpers | `std_path.kizu` | joins and cleans paths with explicit allocator-backed output |
| path edge cases | `std_path_edges.kizu` | covers root, empty path, repeated slash, parent segment, and extension behavior |
| explicit-Io fs helpers | `std_fs_path.kizu` | checks existence, metadata, create_dir, and remove_dir |
| explicit-Io directory read | `std_fs_read_dir.kizu` | lists deterministic directory entries |
| stdio and process helpers | `std_io_process.kizu` | writes stdout and reads argv/env/exit-code helpers |
| stderr helper shape | `std_io_stderr.kizu` | check-only diagnostic output through explicit Io |
| allocation-free byte helpers | `std_mem.kizu` | scans, compares, trims, and slices `[]const u8` safely |
| explicit lifetime slice view | `lifetime_view.kizu` | returns a `[]'a const u8` view tied to its source |
| explicit lifetime borrow return | `lifetime_borrow_return.kizu` | returns shared and mutable borrows tied to local owners |
| lifetime borrow fields | `lifetime_borrow_fields.kizu` | check-only struct and union borrow field declarations |
| checked index / slice syntax | `slice_syntax.kizu` | asserts trapping `[]const u8` indexing and slicing through `bytes[...]` |
| boolean logic | `logical.kizu` | asserts `and` / `or` precedence and short-circuit shape |
| owned array with explicit allocator | `std_array.kizu` | appends, reads, and deinitializes `Array<i64>` |
| token list shape | `std_array_token_list.kizu` | stores copy enum tokens in `Array<TokenKind>` |
| array element borrow | `std_array_borrow.kizu` | reads and updates non-copy elements through local borrows |
| owned string with explicit allocator | `std_string.kizu` | builds owned bytes, reserves capacity, and exposes local byte views |
| owned string storage boundary | `std_string_storage_boundary.kizu` | asserts reserve, append, truncate, clear, view, and deinit rules |
| owned string mutable borrow | `std_string_mut_borrow.kizu` | mutates owned bytes through `&mut String` |
| diagnostic formatting | `std_fmt.kizu` | appends deterministic i64, bool, and byte literal output |
| diagnostic byte escaping | `std_fmt_escapes.kizu` | escapes newline, tab, and backslash bytes |
| source artifact builder | `std_source_builder_artifact.kizu` | builds emitted text with `String` and `std::fmt` |
| owned map with explicit allocator | `std_map.kizu` | inserts, looks up, and deinitializes `Map<[]const u8, i64>` |
| symbol table map shape | `std_map_symbol_table.kizu` | maps byte keys to copy enum values |
| resolver scope map shape | `std_map_resolver_scope.kizu` | uses `Map<[]const u8, V>` for selfhost-style symbol lookup |
| loop-built string map key | `std_map_string_key_loop.kizu` | builds copied map keys from `String.as_bytes()` and deinitializes builders inside a loop |
| owned map mutable borrow | `std_map_mut_borrow.kizu` | mutates a map through `&mut Map` |
| minimal test assertions | `std_testing.kizu` | checks `std::testing` assertions through `kizu test` |
| owned message passing | `channel.kizu` | sends and receives owned values through `std::channel` |
| typed channel payload | `channel_string.kizu` | sends and receives `[]const u8` through `Channel<T>` |
| atomic stop flag | `atomic_flag.kizu` | uses `Atomic<bool>` as a low-level flag |
| deterministic deferred task queue | `task_queue.kizu` | queues work and drains it in FIFO order |
| safe data parallelism | `parallel_for.kizu` | runs structured workers and disjoint partition output |
| low-level concurrency boundary | `thread_boundary.kizu` | uses scoped thread, seq_cst atomic, and mutex prototypes |

## Package-Shaped Examples

These examples document behavior that needs a package/module root rather than a
single source file. Run them with `kizu check <package-root>`.

| Feature | Example | Backing fixture |
| --- | --- | --- |
| imported module type references | `modules/cross_module_types/` | `tests/conformance/modules/basic` |

## Negative Examples

| Safety rule | Example | Expected diagnostic substring |
| --- | --- | --- |
| moved values cannot be reused | `negative/moved_value.kizu` | `moved value` |
| root move error example | `move_error.kizu` | `moved value` |
| assignment moves non-copy values | `negative/assignment_move.kizu` | `moved value` |
| double move is rejected | `negative/double_move.kizu` | `moved value` |
| branch moves are visible after `if` | `negative/if_branch_move.kizu` | `moved value` |
| one-sided branch moves are visible after `if` | `negative/if_branch_partial_move.kizu` | `moved value` |
| loop body moves are visible after the loop | `negative/while_body_move.kizu` | `moved value` |
| borrowed non-copy values cannot be moved | `negative/move_while_borrowed.kizu` | `cannot be moved while borrowed` |
| local borrow blocks moves until last use | `negative/borrow_before_last_use_move.kizu` | `cannot be moved while borrowed` |
| loop body use does not end an outer borrow | `negative/borrow_loop_last_use.kizu` | `cannot be moved while borrowed` |
| field borrow blocks same-field assignment | `negative/field_borrow_same_field_assignment.kizu` | `cannot be assigned while borrowed` |
| field borrow blocks owner move | `negative/field_borrow_owner_move.kizu` | `cannot be moved while borrowed` |
| v0.1 rejects nested field borrow | `negative/nested_field_borrow.kizu` | `one direct field` |
| borrowed values cannot escape | `negative/borrow_escape.kizu` | `borrowed value` |
| borrow fields need explicit lifetimes | `negative/borrow_field.kizu` | `requires struct lifetime parameter` |
| borrow returns need explicit lifetimes | `negative/lifetime_return_missing.kizu` | `borrow return requires explicit lifetime` |
| borrowed parameters cannot be stored in owned locals | `negative/borrow_local_alias.kizu` | `borrowed value` |
| borrowed parameters cannot be passed as owned values | `negative/borrow_to_owner.kizu` | `borrowed value` |
| non-copy values cannot move out of borrow deref | `negative/borrow_deref_move.kizu` | `cannot be moved out of borrow` |
| mutable borrow conflicts are rejected | `negative/mut_borrow_conflict.kizu` | `mutably borrowed while borrowed` |
| non-copy values cannot move out of mutable borrow deref | `negative/mut_borrow_deref_move.kizu` | `cannot be moved out of borrow` |
| `&mut` requires mutable binding | `negative/mut_borrow_immutable.kizu` | `must be mutable` |
| runtime borrow cannot cross comptime | `negative/comptime_borrow_escape.kizu` | `runtime value cannot be used` |
| `arena.add` moves inserted values | `negative/arena_add_move.kizu` | `moved value` |
| `arena.get` returns a local borrow | `negative/arena_get_move.kizu` | `cannot be moved` |
| handles are tied to one arena | `negative/arena_wrong_handle.kizu` | `does not belong to arena` |
| inline handles are tied to one arena | `negative/arena_inline_wrong_handle.kizu` | `does not belong to arena` |
| unknown handle provenance is rejected | `negative/arena_unknown_handle.kizu` | `unknown provenance` |
| handles cannot outlive their arena | `negative/arena_handle_outlive.kizu` | `cannot outlive` |
| `let` bindings are immutable | `negative/immutable_assignment.kizu` | `cannot assign` |
| `break` outside loops is rejected | `negative/break_outside_loop.kizu` | `outside loop` |
| `continue` outside loops is rejected | `negative/continue_outside_loop.kizu` | `outside loop` |
| unknown loop labels are rejected | `negative/unknown_loop_label.kizu` | `unknown loop label` |
| labels only attach to loops | `negative/label_on_non_loop.kizu` | `must be attached` |
| fields on `let` bindings are immutable | `negative/immutable_field_assignment.kizu` | `cannot assign field` |
| unknown fields are rejected | `negative/invalid_field.kizu` | `unknown field` |
| non-`void` functions need returned values | `negative/empty_return_value.kizu` | `got void` |
| non-`void` functions require explicit return | `negative/missing_return.kizu` | `must return` |
| `try` requires an error-union-returning function | `negative/invalid_try.kizu` | `try requires` |
| typed errors must match across `try` | `negative/typed_error_mismatch.kizu` | `cannot propagate` |
| `error(message)` cannot build typed errors | `negative/typed_error_untyped_constructor.kizu` | `cannot construct typed error` |
| typed error casts need a message variant | `negative/typed_error_cast_missing_message.kizu` | `requires CompileError::Message` |
| invalid casts are rejected | `negative/invalid_cast.kizu` | `cannot cast` |
| unsafe-only calls require `unsafe` | `negative/unsafe_call.kizu` | `requires unsafe block` |
| pointer reads require `unsafe` | `negative/ptr_read_without_unsafe.kizu` | `requires unsafe block` |
| nullable raw pointers cannot be read directly | `negative/nullable_ptr_read.kizu` | `non-null raw pointer` |
| handles are not raw pointers | `negative/handle_as_pointer.kizu` | `cannot cast handle` |
| unsafe does not permit moved safe values | `negative/unsafe_moved_value.kizu` | `moved value` |
| unsafe does not permit borrow escape | `negative/unsafe_borrow_escape.kizu` | `borrowed value` |
| satisfy requires every contract method | `negative/missing_contract_method.kizu` | `missing method` |
| `Dyn<Contract>` requires explicit satisfy | `negative/unsatisfied_dyn.kizu` | `does not satisfy` |
| owned `Dyn<Contract>` is not v0.1 | `negative/owned_dyn.kizu` | `must be borrowed` |
| tasks must be awaited or canceled | `negative/unawaited_task.kizu` | `must be awaited or canceled` |
| task args move non-copy values | `negative/task_move.kizu` | `moved value` |
| tasks cannot capture borrow params | `negative/task_borrow_capture.kizu` | `cannot capture borrow` |
| tasks cannot capture safe raw pointers | `negative/task_spawn_pointer.kizu` | `raw pointer` |
| tasks cannot capture handles | `negative/task_spawn_handle.kizu` | `handle` |
| tasks cannot capture arenas | `negative/task_spawn_arena.kizu` | `arena` |
| tasks cannot capture mutex values | `negative/task_spawn_mutex.kizu` | `Mutex` |
| tasks cannot capture structs containing raw pointers | `negative/task_spawn_struct_pointer.kizu` | `struct` |
| spawned functions require owned Io | `negative/task_spawn_borrowed_io.kizu` | `owned Io` |
| spawned functions reject mutable Io borrow | `negative/task_spawn_mut_borrowed_io.kizu` | `owned Io` |
| task body errors propagate through `await` | `negative/task_await_error.kizu` | `channel is empty` |
| canceled tasks cannot be awaited | `negative/task_await_after_cancel.kizu` | `already completed` |
| awaited tasks cannot be canceled | `negative/task_cancel_after_await.kizu` | `already completed` |
| bare `Io()` constructor is rejected | `negative/io_builtin_constructor.kizu` | `std::io::blocking` |
| evented Io is not implemented in v0.1 | `negative/io_evented_unimplemented.kizu` | `not implemented` |
| task host primitives are reserved | `negative/std_task_builtin_direct_call.kizu` | `reserved; use std::task` |
| parallel task host primitives are reserved | `negative/std_task_parallel_for_builtin_direct_call.kizu` | `reserved; use std::task` |
| parallel map host primitives are reserved | `negative/std_task_parallel_map_builtin_direct_call.kizu` | `reserved; use std::task` |
| channel host primitives are reserved | `negative/std_channel_builtin_direct_call.kizu` | `reserved; use std::channel` |
| atomic host primitives are reserved | `negative/std_atomic_builtin_direct_call.kizu` | `reserved; use std::atomic` |
| mutex host primitives are reserved | `negative/std_mutex_builtin_direct_call.kizu` | `reserved; use std::sync` |
| array host primitives are reserved | `negative/std_array_builtin_direct_call.kizu` | `reserved; use std::array` |
| map host primitives are reserved | `negative/std_map_builtin_direct_call.kizu` | `reserved; use std::map` |
| channel method primitives are reserved | `negative/std_channel_send_builtin_direct_call.kizu` | `reserved` |
| atomic method primitives are reserved | `negative/std_atomic_load_builtin_direct_call.kizu` | `reserved` |
| mutex method primitives are reserved | `negative/std_mutex_get_builtin_direct_call.kizu` | `reserved` |
| array method primitives are reserved | `negative/std_array_append_builtin_direct_call.kizu` | `reserved` |
| map method primitives are reserved | `negative/std_map_insert_builtin_direct_call.kizu` | `reserved` |
| generic functions are std-only | `negative/generic_function_user_reserved.kizu` | `reserved for std` |
| `Function` parameters are std-only | `negative/function_parameter_runtime.kizu` | `reserved for std` |
| task groups require Io | `negative/task_group_without_io.kizu` | `expects io` |
| old spawn Io argument is rejected | `negative/task_spawn_old_io_arg.kizu` | `function name` |
| file read requires Io | `negative/fs_read_without_io.kizu` | `expects Io` |
| missing file returns `!T` error | `negative/fs_read_missing.kizu` | `no such file` |
| file write bytes must be `[]const u8` | `negative/fs_write_wrong_bytes.kizu` | `bytes` |
| failing Io returns deterministic error | `negative/fs_failing_io.kizu` | `io runtime is failing` |
| path helpers require byte paths | `negative/std_path_wrong_type.kizu` | `expects []const u8` |
| fs helpers require Io | `negative/std_fs_exists_without_io.kizu` | `expects Io` |
| stdio helpers surface failing Io | `negative/std_io_failing_write.kizu` | `io runtime is failing` |
| process arg access is bounds-checked | `negative/std_process_arg_bounds.kizu` | `process arg index out of bounds` |
| process exit code builtin is removed | `negative/std_process_exit_code_builtin_direct_call.kizu` | `was removed` |
| byte-slice helper args must be `[]const u8` | `negative/std_mem_wrong_type.kizu` | `expects []const u8` |
| byte access builtin is removed | `negative/std_mem_byte_at_builtin_direct_call.kizu` | `was removed` |
| byte slice builtin is removed | `negative/std_mem_slice_builtin_direct_call.kizu` | `was removed` |
| checked byte slices reject invalid ranges | `negative/std_mem_slice_out_of_bounds.kizu` | `range out of bounds` |
| checked byte access rejects invalid indexes | `negative/std_mem_byte_at_out_of_bounds.kizu` | `index out of bounds` |
| slice syntax rejects invalid ranges | `negative/slice_syntax_range_out_of_bounds.kizu` | `range out of bounds` |
| index syntax rejects invalid indexes | `negative/slice_syntax_index_out_of_bounds.kizu` | `index out of bounds` |
| index/slice syntax requires byte slices | `negative/slice_syntax_wrong_target.kizu` | `expects []const u8` |
| array access is bounds-checked | `negative/std_array_bounds.kizu` | `index out of bounds` |
| array construction requires explicit allocator | `negative/std_array_no_allocator.kizu` | `expects allocator` |
| arrays cannot be used after `deinit` | `negative/std_array_use_after_deinit.kizu` | `moved value` |
| array append element type must match `T` | `negative/std_array_wrong_type.kizu` | `Array.append` |
| array append moves non-copy values | `negative/std_array_append_moves.kizu` | `moved value` |
| array get is copy-only in v0.2 | `negative/std_array_get_non_copy.kizu` | `requires copy element` |
| array elements cannot be raw pointers | `negative/std_array_raw_pointer_element.kizu` | `raw pointer` |
| array elements cannot be handles | `negative/std_array_handle_element.kizu` | `handle` |
| arrays cannot cross channel boundary | `negative/std_array_channel_send.kizu` | `Array cannot cross concurrency boundary` |
| arrays cannot cross task boundary | `negative/std_array_task_spawn.kizu` | `Array cannot cross concurrency boundary` |
| borrowed array blocks append | `negative/std_array_append_while_borrowed.kizu` | `cannot run while array is borrowed` |
| borrowed array blocks deinit | `negative/std_array_deinit_while_borrowed.kizu` | `cannot run while array is borrowed` |
| borrowed array blocks set | `negative/std_array_set_while_borrowed.kizu` | `cannot run while array is borrowed` |
| mutable array borrow blocks reads | `negative/std_array_read_while_mut_borrowed.kizu` | `cannot read while mutably borrowed` |
| mutable array borrow requires `var` | `negative/std_array_at_mut_immutable.kizu` | `requires mutable array binding` |
| array element borrows cannot be passed as owned values | `negative/std_array_at_pass_to_owned_param.kizu` | `Array.at` must be bound |
| array element borrows cannot escape through return | `negative/std_array_at_return_escape.kizu` | `Array.at` must be bound |
| array borrow access is bounds-checked | `negative/std_array_at_out_of_bounds.kizu` | `index out of bounds` |
| array elements reject nested arrays through structs | `negative/std_array_struct_nested_array_element.kizu` | `nested array` |
| array elements reject raw pointers through structs | `negative/std_array_struct_raw_pointer_element.kizu` | `raw pointer` |
| array elements reject handles through unions | `negative/std_array_union_handle_element.kizu` | `handle` |
| array elements reject channels through structs | `negative/std_array_struct_channel_element.kizu` | `Channel` |
| array elements reject atomics | `negative/std_array_atomic_element.kizu` | `Atomic` |
| string construction requires explicit allocator | `negative/std_string_no_allocator.kizu` | `expects allocator` |
| string storage builtins are removed | `negative/std_string_builtin_direct_call.kizu` | `was removed` |
| string view builtin is removed | `negative/std_string_builtin_as_bytes_direct_call.kizu` | `was removed` |
| string storage field stays private | `negative/std_string_private_storage.kizu` | `is private` |
| string append bytes requires `[]const u8` | `negative/std_string_wrong_append_type.kizu` | `expects []const u8` |
| string append byte requires `u8` | `negative/std_string_append_byte_wrong_type.kizu` | `expects u8` |
| string reserve requires `i64` | `negative/std_string_reserve_wrong_type.kizu` | `expects i64` |
| string truncate requires `i64` | `negative/std_string_truncate_wrong_type.kizu` | `expects i64` |
| string truncate checks bounds | `negative/std_string_truncate_out_of_bounds.kizu` | `length out of bounds` |
| strings cannot be used after `deinit` | `negative/std_string_use_after_deinit.kizu` | `moved value` |
| string byte views block append | `negative/std_string_append_while_viewed.kizu` | `cannot run while string is borrowed` |
| string byte views block append byte | `negative/std_string_append_byte_while_viewed.kizu` | `cannot run while string is borrowed` |
| string byte views block clear | `negative/std_string_clear_while_viewed.kizu` | `cannot run while string is borrowed` |
| string byte views block reserve | `negative/std_string_reserve_while_viewed.kizu` | `cannot run while string is borrowed` |
| string byte views block truncate | `negative/std_string_truncate_while_viewed.kizu` | `cannot run while string is borrowed` |
| string byte views block deinit | `negative/std_string_deinit_while_viewed.kizu` | `cannot run while string is borrowed` |
| string byte views cannot escape through return | `negative/std_string_as_bytes_return_escape.kizu` | `String.as_bytes` must be bound |
| string byte views cannot be used directly | `negative/std_string_as_bytes_direct_use.kizu` | `String.as_bytes` must be bound |
| lifetime return must name its source | `negative/lifetime_return_unbound_source.kizu` | `return lifetime` |
| lifetime return cannot use local string view | `negative/lifetime_return_dangling_string_view.kizu` | `return lifetime` |
| lifetime borrow fields need type lifetimes | `negative/lifetime_borrow_field_missing_param.kizu` | `requires struct lifetime parameter` |
| lifetime views cannot cross channels | `negative/lifetime_channel_escape.kizu` | `lifetime view cannot cross concurrency boundary` |
| lifetime views cannot cross comptime | `negative/lifetime_comptime_escape.kizu` | `lifetime view cannot cross comptime boundary` |
| mutable lifetime view blocks parent read | `negative/lifetime_mut_return_alias.kizu` | `cannot be read while mutably borrowed` |
| shared string borrows cannot deinit | `negative/std_string_deinit_through_shared_borrow.kizu` | `requires owned String receiver` |
| mutable string borrows cannot deinit | `negative/std_string_deinit_through_mut_borrow.kizu` | `requires owned String receiver` |
| shared string borrows cannot append | `negative/std_string_append_through_shared_borrow.kizu` | `requires mutable String receiver` |
| fmt i64 formatting requires `i64` | `negative/std_fmt_wrong_i64_type.kizu` | `expects i64` |
| fmt byte literal formatting requires bytes | `negative/std_fmt_wrong_bytes_type.kizu` | `expects []const u8` |
| map missing key is checked | `negative/std_map_get_missing.kizu` | `key not found` |
| map construction requires explicit allocator | `negative/std_map_no_allocator.kizu` | `expects allocator` |
| map values are copy-only in v0.2 | `negative/std_map_non_copy_value.kizu` | `value type must be copy` |
| map keys are `[]const u8` in v0.2 | `negative/std_map_wrong_key_type.kizu` | `key type must be []const u8` |
| map insert value type must match `V` | `negative/std_map_wrong_insert_type.kizu` | `Map.insert` |
| maps cannot be used after `deinit` | `negative/std_map_use_after_deinit.kizu` | `moved value` |
| shared map borrows cannot insert | `negative/std_map_insert_through_shared_borrow.kizu` | `requires mutable Map receiver` |
| shared map borrows cannot deinit | `negative/std_map_deinit_through_shared_borrow.kizu` | `requires owned Map receiver` |
| mutable map borrows cannot deinit | `negative/std_map_deinit_through_mut_borrow.kizu` | `requires owned Map receiver` |
| maps cannot cross task boundaries | `negative/std_map_task_spawn.kizu` | `Map cannot cross concurrency boundary` |
| maps cannot cross channel boundaries | `negative/std_map_channel_send.kizu` | `Map cannot cross concurrency boundary` |
| arrays cannot store maps in v0.2 | `negative/std_array_map_element.kizu` | `std::map::Map` |
| testing assertion failure is readable | `negative/std_testing_failure.kizu` | `expected 4, got 3` |
| testing expect failure is readable | `negative/std_testing_expect_failure.kizu` | `expected condition to be true` |
| testing bool equality failure is readable | `negative/std_testing_bool_failure.kizu` | `expected true, got false` |
| testing bytes equality failure is readable | `negative/std_testing_bytes_failure.kizu` | `expected "token", got "lexer"` |
| testing fail uses caller message | `negative/std_testing_fail.kizu` | `custom failure` |
| testing helpers enforce argument types | `negative/std_testing_wrong_type.kizu` | `expects i64` |
| channel send moves non-copy values | `negative/channel_send_move.kizu` | `moved value` |
| channel cannot send borrows | `negative/channel_send_borrow.kizu` | `concurrency boundary` |
| channel cannot send safe raw pointers | `negative/channel_send_pointer.kizu` | `raw pointer` |
| empty channel receive is checked | `negative/channel_empty_recv.kizu` | `channel is empty` |
| channel send payload must match `T` | `negative/channel_send_wrong_type.kizu` | `channel.send` |
| channel constructor requires `T` | `negative/channel_untyped_constructor.kizu` | `Channel<T>` |
| queue cannot capture borrow params | `negative/queue_borrow_capture.kizu` | `queue cannot capture borrow` |
| queue cannot capture safe raw pointers | `negative/queue_enqueue_pointer.kizu` | `raw pointer` |
| parallel workers cannot require shared mutable state | `negative/parallel_shared_mutable.kizu` | `must accept i64` |
| parallel map workers must return slot values | `negative/parallel_map_wrong_worker.kizu` | `must return i64` |
| parallel map workers must accept indexes | `negative/parallel_map_wrong_worker_arg.kizu` | `must accept i64` |
| parallel map workers must exist | `negative/parallel_map_undefined_worker.kizu` | `undefined function` |
| parallel map workers must be names | `negative/parallel_map_non_function_name.kizu` | `function name` |
| partition initialization is copy-only | `negative/partition_mut_non_i64.kizu` | `partition init expects i64` |
| parallel worker errors propagate | `negative/parallel_for_error.kizu` | `parallel failed` |
| partition slot access is bounds-checked | `negative/partition_index_out_of_bounds.kizu` | `out of bounds` |
| parallel map ranges are bounds-checked | `negative/parallel_map_out_of_bounds.kizu` | `out of bounds` |
| parallel map requires mutable partition owner | `negative/parallel_map_immutable_partition.kizu` | `&mut argument` |
| local buffer access is bounds-checked | `negative/local_buffer_out_of_bounds.kizu` | `out of bounds` |
| scoped thread cannot capture borrow params | `negative/thread_borrow_capture.kizu` | `thread cannot capture borrow` |
| scoped thread cannot capture safe raw pointers | `negative/thread_scoped_pointer.kizu` | `raw pointer` |
| scoped thread cannot capture mutex values | `negative/thread_scoped_mutex.kizu` | `Mutex` |
| scoped thread workers must accept `T` | `negative/thread_scoped_wrong_worker_arg.kizu` | `must accept i64` |
| scoped thread workers must return `T` | `negative/thread_scoped_wrong_worker_return.kizu` | `must return i64` |
| scoped thread workers must exist | `negative/thread_scoped_undefined_worker.kizu` | `undefined function` |
| scoped thread workers must be names | `negative/thread_scoped_non_function_name.kizu` | `function name` |
| scoped thread moves owned args | `negative/thread_scoped_moves_arg.kizu` | `moved value` |
| scoped thread host primitives are reserved | `negative/std_thread_scoped_builtin_direct_call.kizu` | `reserved; use std::thread` |
| atomic store must match `T` | `negative/atomic_store_wrong_type.kizu` | `atomic.store` |
| old atomic name is rejected | `negative/atomic_old_name.kizu` | `Atomic<i64>` |
| atomic constructor requires `T` | `negative/atomic_untyped_constructor.kizu` | `Atomic<T>` |
| unsupported atomic payloads are rejected | `negative/atomic_unsupported_type.kizu` | `unsupported atomic type` |
| mutex cannot wrap safe raw pointers | `negative/mutex_pointer.kizu` | `raw pointer` |
| mutex constructor payload must match `T` | `negative/mutex_wrong_type.kizu` | `Mutex<i64>` |
| mutex payload must be copy in v0.1 | `negative/mutex_non_copy.kizu` | `requires copy value` |
| mutex constructor requires `T` | `negative/mutex_untyped_constructor.kizu` | `Mutex<T>` |
| shared borrows cannot be written through | `negative/shared_borrow_assignment.kizu` | `not a mutable borrow` |
| enum match must be exhaustive | `negative/match_non_exhaustive.kizu` | `not exhaustive` |
| duplicate match tags are rejected | `negative/match_duplicate_tag.kizu` | `duplicate match tag` |
| unknown match tags are rejected | `negative/match_unknown_tag.kizu` | `unknown match tag` |
| enum variants use `::`, not `.` | `negative/enum_dot_variant.kizu` | `use ::` |
| union variants use `::`, not `.` | `negative/union_dot_variant.kizu` | `use ::` |
