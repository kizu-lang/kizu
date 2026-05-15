# Kizu Memory Safety Contract

This document is the working memory-safety contract for safe Kizu v0.1.

ADR files explain why decisions were made. `SPEC.md` defines the language. This
document lists the safety invariants that the checker and interpreter must
preserve, and maps them to regression coverage.

## Scope

The memory-safety guarantee applies to safe Kizu.

Safe Kizu means code that does not use operations whose safety cannot be proven by
the compiler, such as raw pointer dereference, unchecked access, or actual C ABI
calls.

`unsafe` marks a trusted boundary. It does not disable type checking, move
checking, borrow checking, arena provenance checking, or structured concurrency
rules.

## Non-Goals

The following are not guaranteed by safe Kizu v0.1:

- absence of memory leaks
- absence of deadlock
- absence of logic bugs
- absence of panics or runtime errors
- real OS-thread parallel execution
- raw pointer safety
- C ABI call safety
- allocator primitive safety
- full numeric overflow, truncation, or float semantics
- LLVM/WASM backend parity with the interpreter

## Core Invariants

### Ownership

- Every non-copy value has one owner.
- Passing a non-copy value to an owning parameter moves it.
- Assigning a non-copy value moves it.
- A moved non-copy value cannot be used again.
- Copy types can be reused after copy-like operations.

Current copy types include:

```text
bool
void
Io
i8 i16 i32 i64
u8 u16 u32 u64
usize isize
f32 f64
[]const u8
raw pointer types
tag enum values
```

Struct values are non-copy unless the language later adds an explicit copy
policy.

### Borrowing

- `&T` is a shared local borrow.
- `&mut T` is a mutable local borrow.
- Borrowing does not move ownership.
- A borrowed non-copy value cannot be moved while the borrow is active.
- A local borrow binding is active until its last use in straight-line code.
- A borrow argument is active only for the call statement.
- A borrowed parameter cannot be returned as an owned value.
- A borrowed parameter cannot be stored in a local owned binding.
- A borrowed parameter cannot be passed to an owning parameter.
- A borrow cannot be stored in a struct field.
- A non-copy value cannot be moved out through `value.*`.
- Copy values may be copied through `value.*`.
- `&T` cannot be used for mutation.
- `&mut T` requires a mutable local binding.
- `&T` and `&mut T` cannot overlap in a way that creates mutable aliasing.
- v0.1 supports one-level direct field borrow such as `&user.name`.
- A field borrow allows disjoint field assignment, such as assigning `user.age`
  while `user.name` is borrowed.
- A field borrow blocks owner moves and conflicting access to the same field.
- v0.1 rejects nested field borrow, such as `&user.profile.name`.
- v0.1 does not implement indexed borrow syntax. If indexed access is added later,
  `&items[0]` must get explicit safety rules and regression coverage first.

### Arena and Handle

- `arena<T>.add(value)` moves `value` into the arena.
- `arena<T>.add(value)` returns `handle<T>`.
- `handle<T>` is an opaque ID, not a raw pointer.
- `arena<T>.get(handle<T>)` returns a local borrow-like value.
- Values read through `arena.get` cannot be moved out.
- A handle can only be used with the arena that produced it.
- A handle cannot outlive its arena.
- A handle cannot be cast to a raw pointer in safe Kizu.

### Unsafe and Raw Pointers

- Raw pointer types are distinct from safe borrows.
- Raw pointer operations require `unsafe`.
- Nullable raw pointers cannot be read as non-null pointers without an explicit
  check or conversion policy.
- `unsafe` code carries the memory-safety obligation for raw pointer operations,
  C ABI calls, and unchecked operations.
- `unsafe` does not permit moved values, borrow escape, or safe-borrow lifetime
  extension.

| Operation | Safe Kizu | `unsafe` Kizu |
| --- | --- | --- |
| call `extern "c" fn` | rejected | allowed, caller owns ABI and memory obligation |
| raw pointer read/write | rejected | allowed by explicit operation policy |
| nullable raw pointer read as non-null | rejected | rejected until an explicit conversion policy exists |
| use moved safe value | rejected | rejected |
| return or store safe borrow | rejected | rejected |
| extend safe borrow lifetime | rejected | rejected |

### Concurrency Boundaries

- v0.1 fixes the async and multi-threading stdlib API shape and checker rules,
  not a real asynchronous runtime.
- The v0.1 interpreter may execute task, queue, thread, and data-parallel APIs
  synchronously.
- Tasks must be awaited or canceled.
- Task, queue, channel, and thread boundaries cannot capture or transport local
  borrows.
- Raw pointers cannot cross concurrency boundaries in safe Kizu.
- Sending a non-copy value through a channel moves it.
- Safe Kizu does not allow implicit shared mutable state across tasks.
- Data parallel mutation is restricted to trusted structured APIs.
- `std::channel::Channel<T>` sends and receives owned `T` values.
- `std::sync::Mutex<T>` is the explicit shared-mutable-state wrapper.
- `std::atomic::AtomicI64` is seq_cst-only in v0.1.
- Kizu does not adopt Rust `Send`; boundary-crossing types are explicit checker
  rules.
- Copy primitives and owned values may cross concurrency boundaries.
- Local borrows, mutable borrows, and raw pointers may not cross safe Kizu
  concurrency boundaries.
- Arena / handle thread-safe sharing is not part of v0.1.
- `std::task::partition_mut(init, count)` creates checked disjoint output slots.
- `partition.at(i)` bounds-checks slot access.
- `std::task::parallel_map(io, partition, start, end, worker)` writes only into the
  checked slot range.

These language runtime boundaries are separate from GitHub Actions or CI event
boundaries. For example, `pull_request_target` is a repository automation
security concern, while Kizu task, channel, queue, and thread boundaries are
language-level ownership boundaries. Both must be treated conservatively, but
they are enforced by different systems.

### Comptime

- Runtime borrows cannot cross a `comptime` boundary.
- `comptime` does not disable ownership or borrow checks.

### Runtime Bounds

Safe Kizu may reject programs at runtime when a dynamic safety precondition is not
known statically.

Current runtime safety checks include:

- partition index bounds
- `parallel_map` range bounds
- channel empty receive
- arena handle access at interpreter runtime

Runtime failure is acceptable. Silent undefined behavior is not.

## Trusted Boundaries

The following components are trusted in v0.1:

- Go implementation of the parser, type checker, ownership checker, and interpreter
- built-in functions and std prototype APIs
- arena / handle runtime representation
- task, queue, channel, partition, atomic, and mutex prototype runtimes
- future backend lowering from checked programs to IR, LLVM, or WASM

Each trusted boundary must stay small and must have negative examples or unit tests
for safe-side misuse.

## Regression Coverage

Every `.kizu` example is listed in `tests/conformance/v0_1.json`. This table maps
memory-safety invariants to representative examples.

| Invariant | Positive coverage | Negative coverage |
| --- | --- | --- |
| move after ownership transfer is rejected | `examples/functions.kizu` | `examples/move_error.kizu`, `examples/negative/moved_value.kizu`, `examples/negative/double_move.kizu` |
| branch and loop moves remain visible after control flow | `examples/if_expression.kizu` | `examples/negative/if_branch_move.kizu`, `examples/negative/if_branch_partial_move.kizu`, `examples/negative/if_expression_branch_move.kizu`, `examples/negative/while_body_move.kizu` |
| copy values can be reused after owner-like calls | `examples/copy_after_move.kizu` | |
| assignment moves non-copy values | `examples/variables.kizu` | `examples/negative/assignment_move.kizu` |
| borrow does not move owner | `examples/borrow.kizu`, `examples/last_use_borrow.kizu`, `examples/borrow_call_then_move.kizu` | `examples/negative/borrow_escape.kizu` |
| non-copy value cannot move while borrowed | | `examples/negative/move_while_borrowed.kizu`, `examples/negative/borrow_before_last_use_move.kizu`, `examples/negative/borrow_loop_last_use.kizu` |
| one-level field borrow permits disjoint fields | `examples/field_borrow.kizu` | `examples/negative/field_borrow_same_field_assignment.kizu`, `examples/negative/field_borrow_owner_move.kizu`, `examples/negative/nested_field_borrow.kizu` |
| borrow cannot be stored or passed as owned | | `examples/negative/borrow_field.kizu`, `examples/negative/borrow_local_alias.kizu`, `examples/negative/borrow_to_owner.kizu` |
| copy value can be copied through borrow deref | `examples/borrow_deref_copy.kizu` | |
| non-copy value cannot move out of borrow deref | `examples/borrow_deref_copy.kizu` | `examples/negative/borrow_deref_move.kizu`, `examples/negative/mut_borrow_deref_move.kizu` |
| mutable borrow requires mutable binding | `examples/mutable_borrow.kizu` | `examples/negative/mut_borrow_immutable.kizu` |
| shared and mutable borrows cannot conflict | `examples/mutable_borrow.kizu` | `examples/negative/mut_borrow_conflict.kizu` |
| shared borrow cannot mutate | | `examples/negative/shared_borrow_assignment.kizu` |
| arena add moves values | `examples/arena.kizu` | `examples/negative/arena_add_move.kizu` |
| arena get is local-borrow-like | `examples/arena.kizu` | `examples/negative/arena_get_move.kizu` |
| handle provenance is enforced | `examples/arena.kizu` | `examples/negative/arena_wrong_handle.kizu`, `examples/negative/arena_inline_wrong_handle.kizu`, `examples/negative/arena_unknown_handle.kizu`; invalid-index handles are covered by `internal/interp` unit tests |
| handles cannot outlive their arena | | `examples/negative/arena_handle_outlive.kizu` |
| handle is not a raw pointer | | `examples/negative/handle_as_pointer.kizu` |
| unsafe is explicit | `examples/unsafe_wrapper.kizu` | `examples/negative/unsafe_call.kizu`, `examples/negative/ptr_read_without_unsafe.kizu` |
| unsafe does not disable safe rules | | `examples/negative/unsafe_moved_value.kizu`, `examples/negative/unsafe_borrow_escape.kizu` |
| nullable raw pointer reads are rejected | `examples/pointer_policy.kizu` | `examples/negative/nullable_ptr_read.kizu` |
| runtime borrow cannot cross comptime | `examples/comptime.kizu` | `examples/negative/comptime_borrow_escape.kizu` |
| task ownership is structured | `examples/task_group.kizu` | `examples/negative/unawaited_task.kizu`, `examples/negative/task_move.kizu`, `examples/negative/task_borrow_capture.kizu`, `examples/negative/task_spawn_pointer.kizu` |
| channel sends owned values | `examples/channel.kizu` | `examples/negative/channel_send_move.kizu`, `examples/negative/channel_send_borrow.kizu`, `examples/negative/channel_send_pointer.kizu`, `examples/negative/channel_empty_recv.kizu` |
| queued work cannot capture borrows or raw pointers | `examples/task_queue.kizu` | `examples/negative/queue_borrow_capture.kizu`, `examples/negative/queue_enqueue_pointer.kizu` |
| structured data parallelism uses disjoint output | `examples/parallel_for.kizu` | `examples/negative/parallel_shared_mutable.kizu`, `examples/negative/parallel_map_wrong_worker.kizu`, `examples/negative/partition_mut_non_i64.kizu` |
| partition bounds are checked | `examples/parallel_for.kizu` | `examples/negative/partition_index_out_of_bounds.kizu`, `examples/negative/parallel_map_out_of_bounds.kizu` |
| scoped thread boundary rejects borrows and raw pointers | `examples/thread_boundary.kizu` | `examples/negative/thread_borrow_capture.kizu`, `examples/negative/thread_scoped_pointer.kizu` |
| `AtomicI64` is i64-only and seq_cst-only | `examples/thread_boundary.kizu` | `examples/negative/atomic_store_wrong_type.kizu` |
| `Mutex<T>` rejects raw pointer sharing | `examples/thread_boundary.kizu` | `examples/negative/mutex_pointer.kizu` |

## Release Gate

Before declaring v0.1 memory-safe for safe Kizu:

1. `pre-commit run --all-files` must pass.
2. `go test ./...` must pass.
3. `go test ./cmd/kizu -run TestV01ConformanceManifest -count=1` must pass.
4. Every invariant in this document must have regression coverage.
5. New trusted std APIs must document their safe-side preconditions here.
6. New `unsafe` capabilities must add negative tests proving safe checks remain
   active around the unsafe boundary.

## Open Risks

These are known areas to keep conservative:

- Backend code generation is experimental and not yet the source of v0.1 safety.
- Numeric casts and integer-width runtime semantics are incomplete.
- Arrays, slices, allocators, and general std containers are not implemented.
- Real OS threads and async runtime semantics are not implemented.
- Raw pointer runtime operations are not implemented as a safe guarantee.

Do not describe these areas as memory-safe until their invariants and regression
coverage are added to this document.
