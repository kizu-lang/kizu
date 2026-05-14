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
| `if` / `else` | `if.kizu` | prints `adult` |
| `while` | `while.kizu` | prints `0`, `1`, `2` |
| `struct` and field access | `struct.kizu` | prints `alice`, `30` |
| borrow parameter | `borrow.kizu` | borrow does not move the owner |
| `arena<T>` / `handle<T>` | `arena.kizu` | stores and reads a struct through a handle |
| `result<T>`, `ok`, `try` | `result_try.kizu` | propagates success and prints `1` |
| limited `comptime` | `comptime.kizu` | evaluates compile-time expressions |
| Zig/C-style tag `enum` | `enum.kizu` | prints and compares enum tags |
| simple enum `match` | `match.kizu` | dispatches exhaustive enum arms |
| unsafe wrapper boundary | `unsafe_wrapper.kizu` | check-only extern wrapper example |

## Negative Examples

| Safety rule | Example | Expected diagnostic substring |
| --- | --- | --- |
| moved values cannot be reused | `negative/moved_value.kizu` | `moved value` |
| borrowed values cannot escape | `negative/borrow_escape.kizu` | `borrowed value` |
| borrow fields are forbidden | `negative/borrow_field.kizu` | `cannot store borrow` |
| `let` bindings are immutable | `negative/immutable_assignment.kizu` | `cannot assign` |
| unknown fields are rejected | `negative/invalid_field.kizu` | `unknown field` |
| `try` requires a result-returning function | `negative/invalid_try.kizu` | `try requires` |
| invalid casts are rejected | `negative/invalid_cast.kizu` | `cannot cast` |
| unsafe-only calls require `unsafe` | `negative/unsafe_call.kizu` | `requires unsafe block` |
| enum match must be exhaustive | `negative/match_non_exhaustive.kizu` | `not exhaustive` |

## v0.1 Features Still Tracked By Issues

The following v0.1 target features are intentionally listed here so the catalog
cannot silently look complete before the implementation is complete.

| Feature | Tracking issue |
| --- | --- |
| `Io` capability and `TaskGroup` | #17 |
| `contract` and `satisfy` | #18 |
| `borrow Dyn<Contract>` | #19 |
