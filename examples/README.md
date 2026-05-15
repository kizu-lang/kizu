# Kizu v0.1 Examples Catalog

This directory is the user-visible catalog for Kizu v0.1 behavior.

Executable and negative examples are listed in
[`tests/conformance/v0_1.json`](../tests/conformance/v0_1.json). The Go test
runner reads that manifest, and the future self-host compiler should reuse it
as the v0.1 compatibility corpus.

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
| `if` expression | `if_expression.kizu` | selects and prints `1` |
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
| owned message passing | `channel.kizu` | sends and receives owned values through `std::channel` |
| typed channel payload | `channel_string.kizu` | sends and receives `[]const u8` through `Channel<T>` |
| atomic stop flag | `atomic_flag.kizu` | uses `Atomic<bool>` as a low-level flag |
| deterministic deferred task queue | `task_queue.kizu` | queues work and drains it in FIFO order |
| safe data parallelism | `parallel_for.kizu` | runs structured workers and disjoint partition output |
| low-level concurrency boundary | `thread_boundary.kizu` | uses scoped thread, seq_cst atomic, and mutex prototypes |

## Negative Examples

| Safety rule | Example | Expected diagnostic substring |
| --- | --- | --- |
| moved values cannot be reused | `negative/moved_value.kizu` | `moved value` |
| root move error example | `move_error.kizu` | `moved value` |
| assignment moves non-copy values | `negative/assignment_move.kizu` | `moved value` |
| double move is rejected | `negative/double_move.kizu` | `moved value` |
| branch moves are visible after `if` | `negative/if_branch_move.kizu` | `moved value` |
| one-sided branch moves are visible after `if` | `negative/if_branch_partial_move.kizu` | `moved value` |
| `if` expression branch value can move | `negative/if_expression_branch_move.kizu` | `moved value` |
| `if` expression requires `else` | `negative/if_expression_missing_else.kizu` | `expected next token else` |
| `if` expression branch types must match | `negative/if_expression_type_mismatch.kizu` | `branch types differ` |
| loop body moves are visible after the loop | `negative/while_body_move.kizu` | `moved value` |
| statement semicolons are required | `negative/missing_semicolon.kizu` | `expected ;` |
| borrowed non-copy values cannot be moved | `negative/move_while_borrowed.kizu` | `cannot be moved while borrowed` |
| local borrow blocks moves until last use | `negative/borrow_before_last_use_move.kizu` | `cannot be moved while borrowed` |
| loop body use does not end an outer borrow | `negative/borrow_loop_last_use.kizu` | `cannot be moved while borrowed` |
| field borrow blocks same-field assignment | `negative/field_borrow_same_field_assignment.kizu` | `cannot be assigned while borrowed` |
| field borrow blocks owner move | `negative/field_borrow_owner_move.kizu` | `cannot be moved while borrowed` |
| v0.1 rejects nested field borrow | `negative/nested_field_borrow.kizu` | `one direct field` |
| borrowed values cannot escape | `negative/borrow_escape.kizu` | `borrowed value` |
| borrow fields are forbidden | `negative/borrow_field.kizu` | `cannot store borrow` |
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
| task groups require Io | `negative/task_group_without_io.kizu` | `expects io` |
| old spawn Io argument is rejected | `negative/task_spawn_old_io_arg.kizu` | `function name` |
| file read requires Io | `negative/fs_read_without_io.kizu` | `expects Io` |
| missing file returns `!T` error | `negative/fs_read_missing.kizu` | `no such file` |
| file write bytes must be `[]const u8` | `negative/fs_write_wrong_bytes.kizu` | `bytes` |
| failing Io returns deterministic error | `negative/fs_failing_io.kizu` | `io runtime is failing` |
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
| partition initialization is copy-only | `negative/partition_mut_non_i64.kizu` | `partition init expects i64` |
| parallel worker errors propagate | `negative/parallel_for_error.kizu` | `parallel failed` |
| partition slot access is bounds-checked | `negative/partition_index_out_of_bounds.kizu` | `out of bounds` |
| parallel map ranges are bounds-checked | `negative/parallel_map_out_of_bounds.kizu` | `out of bounds` |
| local buffer access is bounds-checked | `negative/local_buffer_out_of_bounds.kizu` | `out of bounds` |
| scoped thread cannot capture borrow params | `negative/thread_borrow_capture.kizu` | `thread cannot capture borrow` |
| scoped thread cannot capture safe raw pointers | `negative/thread_scoped_pointer.kizu` | `raw pointer` |
| scoped thread cannot capture mutex values | `negative/thread_scoped_mutex.kizu` | `Mutex` |
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
