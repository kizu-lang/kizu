# Kizu Memory Safety Contract

This document is the working memory-safety contract for safe Kizu.

ADR files explain why decisions were made. `SPEC.md` defines the language. This
document lists the safety invariants that the checker and the generated code
must preserve, and maps them to regression coverage.

## Scope

The memory-safety guarantee applies to safe Kizu.

Safe Kizu means code that does not use operations whose safety cannot be proven by
the compiler, such as raw pointer dereference, unchecked access, or actual C ABI
calls.

`unsafe` marks a trusted boundary. It does not disable type checking, move
checking, borrow checking, arena provenance checking, or structured concurrency
rules.

## Non-Goals

The following are not guaranteed by safe Kizu:

- absence of memory leaks
- absence of deadlock
- absence of logic bugs
- absence of panics or runtime errors
- real OS-thread parallel execution (ADR-0025)
- raw pointer safety
- C ABI call safety
- allocator primitive safety
- full numeric overflow, truncation, or float semantics
- WASM backend coverage of the language the native path already runs

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
[]u8
raw pointer types
tag enum values
```

Struct values are non-copy unless the language later adds an explicit copy
policy.

### Borrowing

- `&T` is a shared local borrow.
- `&var T` is a mutable local borrow.
- Borrowing does not move ownership.
- A borrowed non-copy value cannot be moved while the borrow is active.
- A local borrow binding is active until its last use in straight-line code.
- A borrow argument is active only for the call statement.
- A borrowed parameter cannot be returned as an owned value.
- A borrowed parameter cannot be stored in a local owned binding.
- A borrowed parameter cannot be passed to an owning parameter.
- A borrow cannot be stored in a struct or union field.
- A borrowed return's sources are derived from the signature: every view or
  borrow argument backs it conservatively (ADR-0098).
- A non-copy value cannot be moved out through `value.*`.
- Copy values may be copied through `value.*`.
- `&T` cannot be used for mutation.
- `&var T` requires a mutable local binding.
- `&T` and `&var T` cannot overlap in a way that creates mutable aliasing.
- Field-path borrow such as `&user.name` or `&user.profile.name` is supported.
- A field borrow allows disjoint field assignment, such as assigning `user.age`
  while `user.name` is borrowed.
- A field borrow blocks owner moves and any access to an aliasing path: a
  borrow of `user.profile.name` conflicts with `user.profile` and with
  `user.profile.name`, but not with `user.age`.
- Indexed borrow syntax is not implemented. If indexed access is added later,
  `&items[0]` must get explicit safety rules and regression coverage first.

### Arena and Handle

- `std::arena::new<T>(allocator)` requires an explicit `Allocator` capability.
- Arena construction reads the allocator capability; it does not move it.
- `std::arena::Arena<T>.add(value)` moves `value` into the arena.
- `std::arena::Arena<T>.add(value)` returns `std::arena::Handle<T>`.
- `std::arena::Handle<T>` is an opaque ID, not a raw pointer.
- `std::arena::Arena<T>.at(std::arena::Handle<T>)` returns a local borrow-like value.
- `std::arena::Arena<T>` may own elements that themselves own resources.
- `std::arena::Arena<T>.deinit()` consumes every initialized owner element before
  releasing arena storage and invalidating the binding.
- Values read through `arena.at` cannot be moved out.
- A handle can only be used with the arena that produced it.
- A handle cannot outlive its arena.
- A known handle cannot be used after its source arena is deinitialized.
- A handle cannot be cast to a raw pointer in safe Kizu.
- Runtime arena diagnostics cover mismatched or out-of-range handles with
  unknown provenance; known invalid handle use remains a static checker
  responsibility.

### Deferred Cleanup

- `defer <expr-stmt>;` registers an explicit cleanup call for the current
  lexical block; function bodies are blocks.
- Deferred cleanup runs in reverse registration order when the block exits,
  including explicit return and error-return paths.
- The first supported form is a cleanup method call such as `defer x.deinit();`.
- Deferred cleanup does not discover resources automatically and does not add
  Drop / RAII semantics.
- The cleanup receiver must satisfy the same ownership and borrow rules as an
  explicit cleanup call when the block exits.
- Owned containers should register `defer x.deinit();` in the same
  lexical block once the owner is established, unless the owner is returned or a
  narrower compiler subset does not yet support `defer` for that path.

### Unsafe and Raw Pointers

- Raw pointer types are distinct from safe borrows.
- Raw pointer operations require an explicit `unsafe` marker on the expression.
- Nullable raw pointers cannot be read as non-null pointers without an explicit
  check or conversion policy.
- `unsafe` code carries the memory-safety obligation for raw pointer operations,
  C ABI calls, and unchecked operations.
- `unsafe` does not permit moved values, borrow escape, or safe-borrow lifetime
  extension.
- An `unsafe` marker that covers no unproven operation is rejected, so the word
  states what happens rather than what would be allowed.
- A struct with a raw pointer field must be declared `unsafe struct`. It cannot
  expose a `pub` field, so the code that can break its invariant is confined to
  the file that declares it.
- Establishing the invariant of an `unsafe struct` — constructing it, or writing
  one of its fields — requires `unsafe`. Reading a field does not: a raw pointer
  taken out of a struct does nothing until it is used, and every use is marked.
- `unsafe fn` and `unsafe struct` must carry a `///` comment. What the
  obligation says cannot be written in code, so the comment is the only place it
  can live; the compiler checks that one was written, not what it says.
- A statement containing `unsafe` must carry a `// SAFETY:` comment. The unit is
  the statement, so one comment answers for every marker inside it, and a
  comment on an enclosing statement does not reach a nested one.

| Operation | Safe Kizu | under `unsafe` |
| --- | --- | --- |
| call `extern "c" fn` | rejected | allowed, caller owns ABI and memory obligation |
| call `unsafe fn` | rejected | allowed, caller owns API-specific obligation |
| construct or write a field of an `unsafe struct` | rejected | allowed, writer owns the declared invariant |
| read a field of an `unsafe struct` | allowed | allowed |
| raw pointer read/write/dereference | rejected | allowed |
| nullable raw pointer read as non-null | rejected | rejected until an explicit conversion policy exists |
| use moved safe value | rejected | rejected |
| return or store safe borrow | rejected | rejected |
| extend safe borrow lifetime | rejected | rejected |

### Concurrency Boundaries

Kizu currently has no concurrency API. `std::task`, `std::channel`,
`std::thread`, `std::sync`, `std::atomic`, and `std::io::threaded()` were
withdrawn by ADR-0025: they carried ~1,900 lines of checker rules with no IR
lowering and no runtime behind them, so no rule here was ever confirmed by
execution.

Threads return, for parallel work. The order changes: the execution path comes
first, and safety rules are written against threads that actually run. Two
constraints are already fixed.

- The shape follows Zig (ADR-0039): no hidden global runtime, `Io` and allocator
  passed explicitly, no function coloring.
- Data-race freedom is not negotiable. Zig does not prevent data races in its
  type system; Kizu must. An API that lets safe Kizu write a data race is not
  adopted, however convenient.

What a returning thread API must demonstrate is listed as an acceptance table in
ADR-0025. It is deliberately kept out of the Regression Coverage table below:
every row there cites an example file that runs today, and a claim with no
example is what this section used to be.

`std::fs`, `std::io`, and `std::process` keep requiring an explicit `Io`
capability and return I/O failures as `!T` values that propagate through `try`.
That boundary is unaffected by the withdrawal.

### Comptime

- Runtime borrows cannot cross a `comptime` boundary.
- `comptime` does not disable ownership or borrow checks.

### Runtime Bounds

Safe Kizu may reject programs at runtime when a dynamic safety precondition is not
known statically.

Current runtime safety checks include:

- arena handle access

Runtime failure is acceptable. Silent undefined behavior is not.

## Trusted Boundaries

The following components are trusted:

- Go implementation of the parser, type checker, and ownership checker
- built-in functions and std prototype APIs
- arena / handle runtime representation
- backend lowering from checked programs to IR, LLVM, or WASM

Each trusted boundary must stay small and must have negative examples or unit tests
for safe-side misuse.

## Regression Coverage

Every `.kizu` example declares its own case at the end of the file. This table maps
memory-safety invariants to representative examples.

| Invariant | Positive coverage | Negative coverage |
| --- | --- | --- |
| move after ownership transfer is rejected | `examples/functions.kizu` | `examples/move_error.kizu`, `examples/negative/moved_value.kizu`, `examples/negative/double_move.kizu` |
| branch and loop moves remain visible after control flow | `examples/if_expression.kizu` | `examples/negative/if_branch_move.kizu`, `examples/negative/if_branch_partial_move.kizu`, `examples/negative/if_expression_branch_move.kizu`, `examples/negative/while_body_move.kizu` |
| copy values can be reused after owner-like calls | `examples/copy_after_move.kizu` | |
| assignment moves non-copy values | `examples/variables.kizu` | `examples/negative/assignment_move.kizu` |
| borrow does not move owner | `examples/borrow.kizu`, `examples/last_use_borrow.kizu`, `examples/borrow_call_then_move.kizu` | `examples/negative/borrow_escape.kizu` |
| non-copy value cannot move while borrowed | | `examples/negative/move_while_borrowed.kizu`, `examples/negative/borrow_before_last_use_move.kizu`, `examples/negative/borrow_loop_last_use.kizu` |
| field-path borrow permits disjoint fields | `examples/field_borrow.kizu`, `examples/nested_field_path.kizu` | `examples/negative/field_borrow_same_field_assignment.kizu`, `examples/negative/field_borrow_owner_move.kizu`, `examples/negative/nested_field_borrow_overlap.kizu` |
| borrow cannot be stored or passed as owned | | `examples/negative/borrow_field.kizu`, `examples/negative/borrow_local_alias.kizu`, `examples/negative/borrow_to_owner.kizu` |
| copy value can be copied through borrow deref | `examples/borrow_deref_copy.kizu` | |
| non-copy value cannot move out of borrow deref | `examples/borrow_deref_copy.kizu` | `examples/negative/borrow_deref_move.kizu`, `examples/negative/mut_borrow_deref_move.kizu` |
| mutable borrow requires mutable binding | `examples/mutable_borrow.kizu` | `examples/negative/mut_borrow_immutable.kizu` |
| shared and mutable borrows cannot conflict | `examples/mutable_borrow.kizu` | `examples/negative/mut_borrow_conflict.kizu` |
| shared borrow cannot mutate | | `examples/negative/shared_borrow_assignment.kizu` |
| `&var self` method requires a mutable receiver | `examples/mutable_self_method.kizu`, `tests/behavior/src/mutable_self_method.kizu` | `examples/negative/mutable_self_method_let_receiver.kizu` |
| arena construction requires explicit allocator | `examples/arena.kizu` | `examples/negative/arena_missing_allocator.kizu`, `examples/negative/arena_extra_allocator_arg.kizu`, `examples/negative/arena_non_allocator_arg.kizu` |
| arena add moves values | `examples/arena.kizu` | `examples/negative/arena_add_move.kizu` |
| arena at is local-borrow-like | `examples/arena.kizu` | `examples/negative/arena_at_move.kizu` |
| arena cleanup invalidates arena and handles | `examples/arena.kizu` | `examples/negative/arena_double_deinit.kizu`, `examples/negative/arena_add_after_deinit.kizu`, `examples/negative/arena_at_after_deinit.kizu`, `examples/negative/arena_deinit_while_borrowed.kizu`, `examples/negative/arena_deinit_wrong_receiver.kizu`, `examples/negative/arena_deinit_borrowed_receiver.kizu`, `examples/negative/arena_deinit_temporary_receiver.kizu`, `examples/negative/arena_deinit_moved_receiver.kizu`, `examples/negative/arena_handle_after_deinit.kizu` |
| arena cleanup consumes owner elements before storage | `examples/arena_owner_elements.kizu` | |
| Box take transfers its payload and consumes the cell | `examples/std_mem_box_take.kizu` | `examples/negative/std_mem_box_take_after_take.kizu`, `examples/negative/std_mem_box_take_while_borrowed.kizu` |
| handle provenance is enforced | `examples/arena.kizu` | `examples/negative/arena_wrong_handle.kizu`, `examples/negative/arena_inline_wrong_handle.kizu`, `examples/negative/arena_unknown_handle.kizu`; invalid-index handles are covered by `internal/interp` unit tests |
| handles cannot outlive their arena | | `examples/negative/arena_handle_outlive.kizu` |
| fixed-buffer allocator ties owners to the buffer frame | `examples/fixed_buffer.kizu`, `tests/behavior/src/fixed_buffer_allocator.kizu` | `examples/negative/fixed_buffer_owner_escape.kizu`, `examples/negative/fixed_buffer_allocator_escape.kizu`, `examples/negative/fixed_buffer_alias.kizu`, `examples/negative/fixed_buffer_reborrow.kizu`, `examples/negative/fixed_buffer_unbound_result.kizu`, `examples/negative/fixed_buffer_inline_factory.kizu`, `examples/negative/fixed_buffer_struct_capture.kizu` |
| view-capturing struct stays tied to its view sources | `examples/bytes_iter.kizu`, `tests/behavior/src/view_capture.kizu` | `examples/negative/view_capture_escape.kizu`, `examples/negative/view_capture_alias.kizu`, `examples/negative/view_capture_mutate.kizu`, `examples/negative/view_capture_inline_arg.kizu`, `examples/negative/view_capture_field_escape.kizu`, `examples/negative/view_capture_smuggle.kizu` |
| handle is not a raw pointer | | `examples/negative/handle_as_pointer.kizu` |
| deferred cleanup is explicit and ownership-checked | `examples/defer_cleanup.kizu`, `examples/defer_order.kizu` | `examples/negative/defer_non_cleanup_expr.kizu`, `examples/negative/defer_invalid_statement.kizu`, `examples/negative/defer_after_move.kizu`, `examples/negative/defer_after_explicit_deinit.kizu`, `examples/negative/defer_cleanup_while_borrowed.kizu` |
| `unsafe` is explicit | `examples/unsafe_wrapper.kizu` | `examples/negative/unsafe_call.kizu`, `examples/negative/ptr_read_without_unsafe.kizu` |
| `unsafe` does not disable safe rules | | `examples/negative/unsafe_moved_value.kizu`, `examples/negative/unsafe_borrow_escape.kizu` |
| `unsafe` states what happens | | `examples/negative/unsafe_marker_covers_nothing.kizu`, `examples/negative/redundant_nested_unsafe_marker.kizu` |
| raw pointer fields carry a declared invariant | `examples/unsafe_struct.kizu` | `examples/negative/unsafe_struct_required.kizu`, `examples/negative/unsafe_struct_pub_field.kizu`, `examples/negative/unsafe_struct_field_write.kizu`, `examples/negative/unsafe_struct_construction.kizu` |
| obligations are stated where they are created | `examples/requires_unsafe.kizu`, `examples/unsafe_struct.kizu` | `examples/negative/unsafe_fn_without_doc.kizu`, `examples/negative/unsafe_struct_without_doc.kizu` |
| obligations are justified where they are met | `examples/pointer_policy.kizu` | `examples/negative/unsafe_without_safety_comment.kizu`, `examples/negative/unsafe_safety_comment_is_empty.kizu`, `examples/negative/unsafe_safety_comment_does_not_reach_nested.kizu` |
| nullable raw pointer reads are rejected | `examples/pointer_policy.kizu` | `examples/negative/nullable_ptr_read.kizu` |
| runtime borrow cannot cross comptime | `examples/comptime.kizu` | `examples/negative/comptime_borrow_escape.kizu` |
| file I/O uses explicit Io and `!T` errors | `examples/fs_read.kizu` | `examples/negative/fs_read_missing.kizu`, `examples/negative/fs_read_without_io.kizu`, `examples/negative/fs_write_wrong_bytes.kizu`, `examples/negative/fs_failing_io.kizu` |

## Release Gate

Before declaring safe Kizu memory-safe:

1. `pre-commit run --all-files` must pass.
2. `go test ./...` must pass.
3. `go test ./cmd/kizu -run TestConformance -count=1` must pass.
4. Every invariant in this document must have regression coverage.
5. New trusted std APIs must document their safe-side preconditions here.
6. New kinds of unproven operation must add negative tests proving safe checks
   remain active around the `unsafe` marker.

## Open Risks

These are known areas to keep conservative:

- Numeric casts and integer-width runtime semantics are incomplete.
- General std containers beyond `Array` / `Map` / `String` are not implemented.
- Real OS threads and async runtime semantics are not implemented (ADR-0025).
- Raw pointer runtime operations are not implemented as a safe guarantee.

Do not describe these areas as memory-safe until their invariants and regression
coverage are added to this document.
