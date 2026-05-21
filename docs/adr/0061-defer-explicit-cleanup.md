# ADR-0061: defer explicit cleanup statement

## Status

Accepted.

## Context

Kizu stdlib owners such as `std::array::Array<T>`, `std::string::String`,
`std::map::Map<K, V>`, `std::mem::Box<T>`, and `arena<T>` require explicit
cleanup. Manual cleanup at every return site is noisy and easy to miss, but
implicit destructors would hide runtime work and move Kizu toward RAII.

Kizu needs a cleanup aid that keeps ownership, allocator, and failure boundaries
visible, and that can still be lowered mechanically by a future self-host
compiler.

## Decision

Kizu supports Zig-style `defer <expr-stmt>;` as a lexical block cleanup
registration. A function body is a block. Deferred cleanups run in reverse
registration order when control leaves that block, including explicit `return`
and error-return paths.

The first supported source form is a cleanup method call expression statement:

```kizu
defer values.deinit();
defer text.deinit();
defer users.deinit();
```

`defer let ...;`, `defer return ...;`, `defer { ... }`, and nested deferred
statements are rejected. The compiler does not discover cleanup automatically.
There is no Drop trait, RAII destructor, or implicit resource cleanup.

Deferred cleanup calls use the same ownership checks as explicit cleanup calls.
The receiver must be readable when the defer is registered, and the cleanup is
checked again at block exit. Moving, explicitly deinitializing, or keeping the
receiver borrowed until block exit is a compile-time error.

## Consequences

Cleanup intent stays visible at the declaration site without adding hidden
runtime behavior. The implementation remains a thin block-level lowering:
collect expression statements and execute them in last-in-first-out order on
each block exit path.

The native lowering subset may reject `defer` until explicit cleanup lowering
is added. That is preferable to silently dropping cleanup in generated code.

Future expansion, such as block bodies or non-cleanup callbacks, needs a new
issue with examples, conformance coverage, and explicit ownership rules.
