# Kizu v0.1 Examples Catalog

This directory is the user-visible catalog for Kizu v0.1 behavior.

Executable examples are verified by `TestV01PositiveExamples` in
`cmd/kizu/conformance_test.go`. Negative examples are verified by
`TestV01NegativeExamples` with expected diagnostic substrings.

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
| `while` | `while.kizu` | prints `0`, `1`, `2` |
| `struct` and field access | `struct.kizu` | prints `alice`, `30` |
| borrow parameter | `borrow.kizu` | borrow does not move the owner |
| `arena<T>` / `handle<T>` | `arena.kizu` | stores and reads a struct through a handle |
| `result<T>`, `ok`, `try` | `result_try.kizu` | propagates success and prints `1` |
| `result<void>` and `ok(void)` | `result_void.kizu` | propagates success without a payload |
| limited `comptime` | `comptime.kizu` | evaluates compile-time expressions |
| Zig/C-style tag `enum` | `enum.kizu` | prints and compares enum tags |
| simple enum `match` | `match.kizu` | dispatches exhaustive enum arms |
| unsafe wrapper boundary | `unsafe_wrapper.kizu` | check-only extern wrapper example |
| raw pointer spelling and unsafe pointer ops | `pointer_policy.kizu` | check-only pointer policy example |
| combined v0.1 application | `user_registry.kizu` | exercises multiple v0.1 features together |
| `contract`, `satisfy`, `borrow Dyn<Contract>` | `contract_writer.kizu` | dynamic dispatch through explicit satisfaction |

## Negative Examples

| Safety rule | Example | Expected diagnostic substring |
| --- | --- | --- |
| moved values cannot be reused | `negative/moved_value.kizu` | `moved value` |
| borrowed values cannot escape | `negative/borrow_escape.kizu` | `borrowed value` |
| borrow fields are forbidden | `negative/borrow_field.kizu` | `cannot store borrow` |
| `arena.get` returns a local borrow | `negative/arena_get_move.kizu` | `cannot be moved` |
| `let` bindings are immutable | `negative/immutable_assignment.kizu` | `cannot assign` |
| unknown fields are rejected | `negative/invalid_field.kizu` | `unknown field` |
| non-`void` functions need returned values | `negative/empty_return_value.kizu` | `got void` |
| non-`void` functions require explicit return | `negative/missing_return.kizu` | `must return` |
| `try` requires a result-returning function | `negative/invalid_try.kizu` | `try requires` |
| invalid casts are rejected | `negative/invalid_cast.kizu` | `cannot cast` |
| unsafe-only calls require `unsafe` | `negative/unsafe_call.kizu` | `requires unsafe block` |
| nullable raw pointers cannot be read directly | `negative/nullable_ptr_read.kizu` | `non-null raw pointer` |
| satisfy requires every contract method | `negative/missing_contract_method.kizu` | `missing method` |
| `Dyn<Contract>` requires explicit satisfy | `negative/unsatisfied_dyn.kizu` | `does not satisfy` |
| owned `Dyn<Contract>` is not v0.1 | `negative/owned_dyn.kizu` | `must be borrowed` |
| enum match must be exhaustive | `negative/match_non_exhaustive.kizu` | `not exhaustive` |

## v0.1 Features Still Tracked By Issues

The following v0.1 target features are intentionally listed here so the catalog
cannot silently look complete before the implementation is complete.

| Feature | Tracking issue |
| --- | --- |
| `Io` capability and `TaskGroup` | #17 |
