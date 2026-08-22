# Kizu Examples Catalog

This directory is the user-visible catalog for Kizu language behavior.

Every example ends with the case it declares -- the command to run it with, the
feature tags it covers, and what that has to produce. The Go test runner walks
this directory and reads those blocks, so an example is covered by existing. The
grammar is documented in the `internal/conformance` package doc.

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
| `if` / `match` expressions with required statement semicolons | `control_expressions.kizu` | asserts expression branch values |
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
| nested field path borrow and receiver | `nested_field_path.kizu` | borrows and mutates through `s.current.budget` |
| copy through borrow dereference | `borrow_deref_copy.kizu` | copies an `i64` through `.*` |
| copy values after owner-like calls | `copy_after_move.kizu` | `i64` remains usable after passing to a function |
| mutable borrow parameter | `mutable_borrow.kizu` | `&var` updates through checked field access |
| `std::arena::Arena<T>` / `std::arena::Handle<T>` | `arena.kizu` | stores and reads a struct through a handle with an explicit allocator |
| `arena.at_mut` writes elements in place | `arena_at_mut.kizu` | bumps a struct field behind a handle through a capture borrow |
| handles are copy IDs | `arena_dag.kizu` | two parents share one child and the handle stays usable |
| arena owner-element cleanup | `arena_owner_elements.kizu` | borrows an owned element, then deinitializes it before arena storage |
| `std::mem::Box<T>` recursive payloads | `std_mem_box_ast.kizu` | builds a boxed AST, borrows a child, and closes owners with `deinit` |
| `Box.take` payload transfer | `std_mem_box_take.kizu` | consumes the box cell and continues with its owned payload |
| `at_mut` through a `&var` parameter | `std_map_at_mut_helper.kizu` | moves counter-update logic into a helper taking the map by `&var` |
| `at_mut` on an owner's container field | `std_array_at_mut_field.kizu` | a `&var self` method captures `self.users.at_mut(id)` and writes in place |
| borrow-optional return accessors | `borrow_accessor.kizu` | `fn (self: &var Registry) user(id) -> ?&var User` feeds the caller's capture |
| `!T`, `error`, `try` | `error_union_try.kizu` | propagates success and prints `1` |
| `!void` and `try` | `error_union_void.kizu` | propagates success without a payload |
| custom error type handling | `custom_error.kizu` | handles a domain error with `union` and `match` |
| typed custom error propagation | `typed_error.kizu` | uses `ConfigError!i64` with `try` |
| limited `comptime` | `comptime.kizu` | evaluates compile-time expressions |
| Zig/C-style tag `enum` | `enum.kizu` | prints and compares enum tags |
| simple enum `match` | `match.kizu` | dispatches exhaustive enum arms |
| wildcard `match` fallback | `match_wildcard.kizu` | groups remaining enum/union tags with `_` |
| tagged `union` with payloads | `union.kizu` | binds payload values in `match` arms |
| unsafe wrapper boundary | `unsafe_wrapper.kizu` | check-only extern wrapper; caller owns the unsafe obligation |
| caller-obligation function | `requires_unsafe.kizu` | `unsafe fn` called through an `unsafe` marker |
| raw pointer spelling and unsafe pointer ops | `pointer_policy.kizu` | check-only pointer policy example |
| raw pointer explicit dereference | `raw_pointer_deref.kizu` | check-only `unsafe p.*.field` pointer access |
| raw pointer field invariant | `unsafe_struct.kizu` | `unsafe struct` documents its invariant; reads are free, writes and construction are marked |
| combined application | `user_registry.kizu` | exercises multiple features together |
| `contract`, `impl Contract for Type`, static generic dispatch | `contract_static_dispatch.kizu` | explicit contract assertion with concrete generic instantiations |
| explicit-Io file read | `fs_read.kizu` | reads a fixture through `std::fs::read_file` |
| pure path helpers | `std_path.kizu` | joins and cleans paths with explicit allocator-backed output |
| path edge cases | `std_path_edges.kizu` | covers root, empty path, repeated slash, parent segment, and extension behavior |
| explicit-Io fs helpers | `std_fs_path.kizu` | checks existence, metadata, create_dir, and remove_dir |
| explicit-Io directory read | `std_fs_read_dir.kizu` | lists deterministic directory entries |
| stdio and process helpers | `std_io_process.kizu` | writes stdout and reads argv/env/exit-code helpers |
| stderr helper shape | `std_io_stderr.kizu` | check-only diagnostic output through explicit Io |
| stable page allocator capability | `std_page_allocator.kizu` | reuses one explicit allocator across Array, String, Map, arena, and another Array |
| allocation-free byte helpers | `std_mem.kizu` | scans, compares, trims, and slices `[]u8` safely |
| borrowed-return provenance slice view | `borrow_return_provenance.kizu` | returns a `[]u8` view tied to its source |
| borrowed-return provenance | `borrow_provenance_return.kizu` | returns shared and mutable borrows tied to local owners |
| checked index / slice syntax | `slice_syntax.kizu` | asserts trapping `[]u8` indexing and slicing through `bytes[...]` |
| boolean logic | `logical.kizu` | asserts `and` / `or` precedence and short-circuit shape |
| contextual integer literals | `contextual_integer_literals.kizu` | narrows integer literals in explicit `u8` / `i32` std and user API contexts |
| owned array with explicit allocator | `std_array.kizu` | appends, reads, and deinitializes `Array<i64>` |
| explicit copy-element array clone | `std_array.kizu` | clones into a caller-selected allocator without aliasing the source buffer |
| token list shape | `std_array_token_list.kizu` | stores copy enum tokens in `Array<TokenKind>` |
| array element borrow | `std_array_borrow.kizu` | reads and updates non-copy elements through `at`/`at_mut` captures |
| owner-safe string sorting | `std_sort_strings.kizu` | sorts owned strings in byte-lexical order without allocation or owner replacement |
| owned string with explicit allocator | `std_string.kizu` | builds owned bytes, reserves capacity, and exposes local byte views |
| owned string storage boundary | `std_string_storage_boundary.kizu` | asserts reserve, append, truncate, clear, view, and deinit rules |
| owned string mutable borrow | `std_string_mut_borrow.kizu` | mutates owned bytes through `&var String` |
| diagnostic formatting | `std_fmt.kizu` | appends deterministic i64, bool, and byte literal output |
| diagnostic byte escaping | `std_fmt_escapes.kizu` | escapes newline, tab, and backslash bytes |
| source artifact builder | `std_source_builder_artifact.kizu` | builds emitted text with `String` and `std::fmt` |
| owned map with explicit allocator | `std_map.kizu` | inserts, looks up, and deinitializes `Map<[]u8, i64>` |
| symbol table map shape | `std_map_symbol_table.kizu` | maps byte keys to copy enum values |
| resolver scope map shape | `std_map_resolver_scope.kizu` | uses `Map<[]u8, V>` for compiler-style symbol lookup |
| loop-built string map key | `std_map_string_key_loop.kizu` | builds copied map keys from `String.as_bytes()` and deinitializes builders inside a loop |
| owned map mutable borrow | `std_map_mut_borrow.kizu` | mutates a map through `&var Map` |
| map value borrow | `std_map_borrow.kizu` | reads and updates values in place through `at`/`at_mut` captures |
| deferred cleanup | `defer_cleanup.kizu` | registers explicit cleanup for Array, String, Map, and arena owners |
| deferred cleanup order | `defer_order.kizu` | runs nested block cleanups and function cleanups in reverse registration order |
| minimal test assertions | `std_testing.kizu` | checks `std::testing` assertions and typed equality through `kizu test` |
| minimal explicit generics | `minimal_generics.kizu` | checks explicit static type arguments and `comptime if T == type<i64>` |

## Package-Shaped Examples

These examples document behavior that needs a package/module root rather than a
single source file. Run them with `kizu check <package-root>`.

| Feature | Example | Case declared in |
| --- | --- | --- |
| multi-module error phases | `modules/compiler_phases/` | `src/main.kizu` |
| package test blocks and helper lookup | `modules/same_module_helper_lookup/` | `src/main.kizu` |

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
| nested field borrow blocks overlapping-path assignment | `negative/nested_field_borrow_overlap.kizu` | `cannot be assigned while borrowed` |
| rejects nested field cleanup | `negative/nested_field_cleanup.kizu` | `one direct field` |
| borrowed values cannot escape | `negative/borrow_escape.kizu` | `borrowed value` |
| borrow fields cannot be stored | `negative/borrow_field.kizu` | `cannot store borrow` |
| borrowed parameters cannot be stored in owned locals | `negative/borrow_local_alias.kizu` | `borrowed value` |
| borrowed parameters cannot be passed as owned values | `negative/borrow_to_owner.kizu` | `borrowed value` |
| non-copy values cannot move out of borrow deref | `negative/borrow_deref_move.kizu` | `cannot be moved out of borrow` |
| mutable borrow conflicts are rejected | `negative/mut_borrow_conflict.kizu` | `mutably borrowed while borrowed` |
| non-copy values cannot move out of mutable borrow deref | `negative/mut_borrow_deref_move.kizu` | `cannot be moved out of borrow` |
| `&var` requires mutable binding | `negative/mut_borrow_immutable.kizu` | `must be mutable` |
| runtime borrow cannot cross comptime | `negative/comptime_borrow_escape.kizu` | `runtime value cannot be used` |
| `arena.add` moves inserted values | `negative/arena_add_move.kizu` | `moved value` |
| `arena.at` returns a local borrow | `negative/arena_at_move.kizu` | `cannot be moved` |
| `arena.at_mut` must be consumed by a capture | `negative/arena_at_mut_requires_capture.kizu` | `must be consumed by a capture` |
| `arena.at_mut` requires a mutable binding | `negative/arena_at_mut_immutable.kizu` | `requires mutable arena binding` |
| `arena.add` waits for element borrows | `negative/arena_add_while_borrowed.kizu` | `cannot run while arena is borrowed` |
| `arena.at` waits for mutable borrows | `negative/arena_at_while_mut_borrowed.kizu` | `cannot read while mutably borrowed` |
| arena construction requires an allocator | `negative/arena_missing_allocator.kizu` | `allocator argument` |
| arena construction accepts one allocator | `negative/arena_extra_allocator_arg.kizu` | `allocator argument` |
| arena allocator argument must be `Allocator` | `negative/arena_non_allocator_arg.kizu` | `expects Allocator` |
| arena cannot be used after deinit | `negative/arena_double_deinit.kizu`, `negative/arena_add_after_deinit.kizu`, `negative/arena_at_after_deinit.kizu` | `deinitialized` |
| borrowed arenas cannot be deinitialized | `negative/arena_deinit_while_borrowed.kizu` | `cannot run while arena is borrowed` |
| arena deinit needs an owned local receiver | `negative/arena_deinit_wrong_receiver.kizu`, `negative/arena_deinit_borrowed_receiver.kizu`, `negative/arena_deinit_temporary_receiver.kizu`, `negative/arena_deinit_moved_receiver.kizu` | `receiver` |
| defer only registers cleanup expression statements | `negative/defer_invalid_statement.kizu`, `negative/defer_non_cleanup_expr.kizu` | `defer expects` |
| deferred cleanup obeys ownership at block exit | `negative/defer_after_move.kizu`, `negative/defer_after_explicit_deinit.kizu`, `negative/defer_cleanup_while_borrowed.kizu` | `moved value` / `deinitialized` / `borrowed` |
| handles die with their arena | `negative/arena_handle_after_deinit.kizu`, `negative/handle_copy_after_deinit.kizu` | `cannot be used after arena` |
| handles are tied to one arena | `negative/arena_wrong_handle.kizu`, `negative/handle_copy_wrong_arena.kizu` | `does not belong to arena` |
| inline handles are tied to one arena | `negative/arena_inline_wrong_handle.kizu` | `does not belong to arena` |
| unknown handle provenance is rejected | `negative/arena_unknown_handle.kizu` | `unknown provenance` |
| handles cannot outlive their arena | `negative/arena_handle_outlive.kizu` | `cannot outlive` |
| `let` bindings are immutable | `negative/immutable_assignment.kizu` | `cannot assign` |
| `break` outside loops is rejected | `negative/break_outside_loop.kizu` | `outside loop` |
| `continue` outside loops is rejected | `negative/continue_outside_loop.kizu` | `outside loop` |
| unknown loop labels are rejected | `negative/unknown_loop_label.kizu` | `unknown loop label` |
| labels only attach to loops | `negative/label_on_non_loop.kizu` | `must be attached` |
| fields on `let` bindings are immutable | `negative/immutable_field_assignment.kizu` | `cannot assign field` |
| nested fields inherit base immutability | `negative/immutable_nested_field_assignment.kizu` | `cannot assign field` |
| call results are not assignment targets | `negative/call_result_field_assignment.kizu` | `invalid assignment target` |
| capture context covers one call only | `negative/std_array_at_in_argument.kizu` | `must be consumed by a capture` |
| `at_mut` needs a mutable place, not a shared borrow | `negative/std_array_at_mut_shared_param.kizu` | `requires mutable array binding` |
| fields of immutable owners are not mutable places | `negative/std_array_field_at_mut_immutable.kizu` | `requires mutable array binding` |
| a field capture borrows the owner's field | `negative/std_array_field_read_while_mut_borrowed.kizu` | `cannot be read while mutably borrowed` |
| owners cannot move while a field is borrowed | `negative/owner_move_while_field_borrowed.kizu` | `cannot be moved while borrowed` |
| arguments cannot mutate a `&var self` receiver | `negative/method_arg_mutates_receiver.kizu` | `mutates its receiver while it is borrowed` |
| borrow arguments cannot overlap a `&var self` receiver | `negative/method_arg_borrows_receiver_field.kizu` | `cannot be mutably borrowed while field is borrowed` |
| returned borrows must root in a borrowed parameter | `negative/borrow_accessor_local_escape.kizu` | `must borrow a borrowed parameter` |
| borrow-optional call results are capture-only | `negative/borrow_accessor_store.kizu` | `must be consumed by a capture` |
| unknown fields are rejected | `negative/invalid_field.kizu` | `unknown field` |
| non-`void` functions need returned values | `negative/empty_return_value.kizu` | `got void` |
| non-`void` functions require explicit return | `negative/missing_return.kizu` | `must return` |
| `try` requires an error-union-returning function | `negative/invalid_try.kizu` | `try requires` |
| typed errors must match across `try` | `negative/typed_error_mismatch.kizu` | `cannot propagate` |
| `error(message)` was removed | `negative/typed_error_untyped_constructor.kizu` | `` `error(message)` was removed `` |
| invalid casts are rejected | `negative/invalid_cast.kizu` | `cannot cast` |
| extern calls require `unsafe` | `negative/unsafe_call.kizu` | ``requires `unsafe` `` |
| caller-obligation calls require `unsafe` | `negative/requires_unsafe_call.kizu` | ``requires `unsafe` `` |
| pointer reads require `unsafe` | `negative/ptr_read_without_unsafe.kizu` | ``requires `unsafe` `` |
| nullable raw pointers cannot be read directly | `negative/nullable_ptr_read.kizu` | `non-null raw pointer` |
| raw pointer dereference requires `unsafe` | `negative/raw_pointer_deref_without_unsafe.kizu` | ``requires `unsafe` `` |
| an `unsafe` marker must cover something | `negative/unsafe_marker_covers_nothing.kizu`, `negative/redundant_nested_unsafe_marker.kizu` | ``covers no operation that needs it`` |
| raw pointer fields require `unsafe struct` | `negative/unsafe_struct_required.kizu` | ``must be declared `unsafe struct` `` |
| `unsafe struct` cannot expose fields | `negative/unsafe_struct_pub_field.kizu` | ``cannot have `pub` field`` |
| `unsafe struct` field writes require `unsafe` | `negative/unsafe_struct_field_write.kizu` | ``requires `unsafe` `` |
| `unsafe struct` construction requires `unsafe` | `negative/unsafe_struct_construction.kizu` | ``requires `unsafe` `` |
| `unsafe fn` / `unsafe struct` need a `///` comment | `negative/unsafe_fn_without_doc.kizu`, `negative/unsafe_struct_without_doc.kizu` | ``needs a `///` comment`` |
| `unsafe` needs a `// SAFETY:` comment | `negative/unsafe_without_safety_comment.kizu`, `negative/unsafe_safety_comment_is_empty.kizu`, `negative/unsafe_safety_comment_does_not_reach_nested.kizu` | ``needs a `// SAFETY:` comment`` |
| const raw pointer dereference cannot be written | `negative/raw_pointer_const_write.kizu` | `const raw pointer` |
| nullable raw pointer dereference is rejected | `negative/raw_pointer_nullable_deref.kizu` | `nullable raw pointer` |
| raw pointer field access needs explicit dereference | `negative/raw_pointer_direct_field.kizu` | `has no fields` |
| handles are not raw pointers | `negative/handle_as_pointer.kizu` | `cannot cast handle` |
| unsafe does not permit moved safe values | `negative/unsafe_moved_value.kizu` | `moved value` |
| unsafe does not permit borrow escape | `negative/unsafe_borrow_escape.kizu` | `borrowed value` |
| contract impl requires every contract method | `negative/missing_contract_method.kizu` | `missing method` |
| bare `Io()` constructor is rejected | `negative/io_builtin_constructor.kizu` | `std::io::blocking` |
| evented Io is not implemented | `negative/io_evented_unimplemented.kizu` | `not implemented` |
| array host primitives are reserved | `negative/std_array_builtin_direct_call.kizu` | `reserved; use std::array` |
| map host primitives are reserved | `negative/std_map_builtin_direct_call.kizu` | `reserved; use std::map` |
| array method primitives are reserved | `negative/std_array_append_builtin_direct_call.kizu` | `reserved` |
| map method primitives are reserved | `negative/std_map_insert_builtin_direct_call.kizu` | `reserved` |
| generic calls require explicit static args | `negative/generic_function_missing_type_args.kizu` | `requires explicit static arguments` |
| non-type static args are reserved | `negative/generic_function_non_type_static_arg.kizu` | `expected static type argument` |
| `Function` parameters are std-only | `negative/function_parameter_runtime.kizu` | `reserved for std` |
| file read requires Io | `negative/fs_read_without_io.kizu` | `expects Io` |
| missing file returns `!T` error | `negative/fs_read_missing.kizu` | `no such file` |
| file write bytes must be `[]u8` | `negative/fs_write_wrong_bytes.kizu` | `bytes` |
| failing Io returns deterministic error | `negative/fs_failing_io.kizu` | `io runtime is failing` |
| path helpers require byte paths | `negative/std_path_wrong_type.kizu` | `expects []u8` |
| fs helpers require Io | `negative/std_fs_exists_without_io.kizu` | `expects Io` |
| stdio helpers surface failing Io | `negative/std_io_failing_write.kizu` | `io runtime is failing` |
| process arg access is bounds-checked | `negative/std_process_arg_bounds.kizu` | `process arg index out of bounds` |
| process exit code builtin is removed | `negative/std_process_exit_code_builtin_direct_call.kizu` | `was removed` |
| byte-slice helper args must be `[]u8` | `negative/std_mem_wrong_type.kizu` | `expects []u8` |
| byte access builtin is removed | `negative/std_mem_byte_at_builtin_direct_call.kizu` | `was removed` |
| byte slice builtin is removed | `negative/std_mem_slice_builtin_direct_call.kizu` | `was removed` |
| checked byte slices reject invalid ranges | `negative/std_mem_slice_out_of_bounds.kizu` | `range out of bounds` |
| checked byte access rejects invalid indexes | `negative/std_mem_byte_at_out_of_bounds.kizu` | `index out of bounds` |
| slice syntax rejects invalid ranges | `negative/slice_syntax_range_out_of_bounds.kizu` | `range out of bounds` |
| index syntax rejects invalid indexes | `negative/slice_syntax_index_out_of_bounds.kizu` | `index out of bounds` |
| index/slice syntax requires byte slices | `negative/slice_syntax_wrong_target.kizu` | `expects []u8` |
| array access is bounds-checked | `negative/std_array_bounds.kizu` | `index out of bounds` |
| array construction requires explicit allocator | `negative/std_array_no_allocator.kizu` | `expects allocator` |
| arrays cannot be used after `deinit` | `negative/std_array_use_after_deinit.kizu` | `moved value` |
| array append element type must match `T` | `negative/std_array_wrong_type.kizu` | `Array.append` |
| array append moves non-copy values | `negative/std_array_append_moves.kizu` | `moved value` |
| array get is copy-only | `negative/std_array_get_non_copy.kizu` | `requires copy element` |
| array clone is copy-only | `negative/std_array_clone_non_copy.kizu` | `Array.clone` rejects owner elements that need per-type deep copy |
| array get_or_panic traps on invalid indexes | `negative/std_array_get_or_panic_bounds.kizu` | `Array.get_or_panic index out of bounds` |
| array swap checks both indexes | `negative/std_array_swap_bounds.kizu` | `std::array::Error::OutOfBounds` |
| array swap requires mutable access | `negative/std_array_swap_shared_param.kizu`, `negative/std_array_swap_shared_field.kizu` | `` `Array.swap` requires mutable Array receiver `` |
| box allocation failure is a recoverable error | `negative/std_mem_box_oom.kizu` | `runtime error: std::mem::Error::OutOfMemory` |
| `Box.take` consumes the box and waits for borrows | `negative/std_mem_box_take_after_take.kizu`, `negative/std_mem_box_take_while_borrowed.kizu` | `moved value` / `cannot run while box is borrowed` |
| array elements cannot be raw pointers | `negative/std_array_raw_pointer_element.kizu` | `raw pointer` |
| borrowed array blocks append | `negative/std_array_append_while_borrowed.kizu` | `cannot run while array is borrowed` |
| borrowed array blocks deinit | `negative/std_array_deinit_while_borrowed.kizu` | `cannot run while array is borrowed` |
| borrowed array blocks set | `negative/std_array_set_while_borrowed.kizu` | `cannot run while array is borrowed` |
| mutable array borrow blocks reads | `negative/std_array_read_while_mut_borrowed.kizu` | `cannot read while mutably borrowed` |
| mutable array borrow requires `var` | `negative/std_array_at_mut_immutable.kizu` | `requires mutable array binding` |
| array element borrows cannot be passed as owned values | `negative/std_array_at_pass_to_owned_param.kizu` | `` borrowed value `first` cannot escape `` |
| array borrow optionals are capture-only | `negative/std_array_at_requires_capture.kizu` | `` `Array.at` must be consumed by a capture `` |
| array elements reject raw pointers through structs | `negative/std_array_struct_raw_pointer_element.kizu` | `raw pointer` |
| string construction requires explicit allocator | `negative/std_string_no_allocator.kizu` | `expects allocator` |
| string storage builtins are removed | `negative/std_string_builtin_direct_call.kizu` | `was removed` |
| string view builtin is removed | `negative/std_string_builtin_as_bytes_direct_call.kizu` | `was removed` |
| string storage field stays private | `negative/std_string_private_storage.kizu` | `is private` |
| string append bytes requires `[]u8` | `negative/std_string_wrong_append_type.kizu` | `expects []u8` |
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
| borrowed return cannot use local string view | `negative/borrow_return_dangling_string_view.kizu` | `borrowed value` |
| mutable borrowed return blocks parent read | `negative/borrow_return_mut_alias.kizu` | `cannot be read while mutably borrowed` |
| shared string borrows cannot deinit | `negative/std_string_deinit_through_shared_borrow.kizu` | `requires owned String receiver` |
| mutable string borrows cannot deinit | `negative/std_string_deinit_through_mut_borrow.kizu` | `requires owned String receiver` |
| shared string borrows cannot append | `negative/std_string_append_through_shared_borrow.kizu` | `requires mutable String receiver` |
| fmt i64 formatting requires `i64` | `negative/std_fmt_wrong_i64_type.kizu` | `expects i64` |
| fmt byte literal formatting requires bytes | `negative/std_fmt_wrong_bytes_type.kizu` | `expects []u8` |
| map missing key is checked | `negative/std_map_get_missing.kizu` | `key not found` |
| map construction requires explicit allocator | `negative/std_map_no_allocator.kizu` | `expects allocator` |
| map values are copy-only | `negative/std_map_non_copy_value.kizu` | `value type must be copy` |
| map keys are `[]u8` | `negative/std_map_wrong_key_type.kizu` | `key type must be []u8` |
| map insert value type must match `V` | `negative/std_map_wrong_insert_type.kizu` | `Map.insert` |
| map borrow optionals are capture-only | `negative/std_map_at_requires_capture.kizu` | `` `Map.at` must be consumed by a capture `` |
| borrowed map blocks insert | `negative/std_map_insert_while_borrowed.kizu` | `cannot run while map is borrowed` |
| mutable map borrow requires `var` | `negative/std_map_at_mut_immutable.kizu` | `requires mutable map binding` |
| mutable map borrow blocks reads | `negative/std_map_read_while_mut_borrowed.kizu` | `cannot read while mutably borrowed` |
| maps cannot be used after `deinit` | `negative/std_map_use_after_deinit.kizu` | `moved value` |
| shared map borrows cannot insert | `negative/std_map_insert_through_shared_borrow.kizu` | `requires mutable Map receiver` |
| shared map borrows cannot deinit | `negative/std_map_deinit_through_shared_borrow.kizu` | `requires owned Map receiver` |
| mutable map borrows cannot deinit | `negative/std_map_deinit_through_mut_borrow.kizu` | `requires owned Map receiver` |
| testing equality failure is readable | `negative/std_testing_failure.kizu` | `expected 4, got 3` |
| testing expect failure is readable | `negative/std_testing_expect_failure.kizu` | `expected condition to be true` |
| testing bool condition failure is readable | `negative/std_testing_bool_failure.kizu` | `expected condition to be true` |
| testing bytes equality failure is readable | `negative/std_testing_bytes_failure.kizu` | `expected "token", got "lexer"` |
| testing fail uses caller message | `negative/std_testing_fail.kizu` | `custom failure` |
| testing helpers enforce argument types | `negative/std_testing_wrong_type.kizu` | `expects bool` |
| testing equality builtin is std-only | `negative/std_testing_equal_builtin_direct_call.kizu` | `reserved` |
| shared borrows cannot be written through | `negative/shared_borrow_assignment.kizu` | `not a mutable borrow` |
| enum match must be exhaustive | `negative/match_non_exhaustive.kizu` | `not exhaustive` |
| duplicate match tags are rejected | `negative/match_duplicate_tag.kizu` | `duplicate match tag` |
| unknown match tags are rejected | `negative/match_unknown_tag.kizu` | `unknown match tag` |
| wildcard match arm must be last | `negative/match_wildcard_not_last.kizu` | `wildcard match arm must be last` |
| wildcard match arm cannot bind payload | `negative/match_wildcard_binding.kizu` | `wildcard match arm cannot bind payload` |
| enum variants use `::`, not `.` | `negative/enum_dot_variant.kizu` | `use ::` |
| union variants use `::`, not `.` | `negative/union_dot_variant.kizu` | `use ::` |
| a function cannot take a type's name | `negative/function_named_after_type.kizu` | `is a type and cannot name a function` |
| declaration order does not change the answer | `negative/function_named_after_enum.kizu` | `is a type and cannot name a function` |
