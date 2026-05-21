# ADR-0065: contextual integer literals

## Status

Accepted.

## Context

Kizu removed ambiguous `int` and kept integer literals defaulting to `i64`.
That keeps ABI and lowering predictable, but it made low-level APIs noisy:

- `std::array::Array<u8>.append(65)`
- `std::string::String.append_byte(47)`
- `ptr_write(ptr<u8>, 1)`
- `fn take(x: i32); take(1)`

Before this ADR, callers had to write `cast<T>(literal)` even when the target
type was already explicit and the literal value could be checked at compile time.

## Decision

Integer literals may be contextually typed by an explicit expected integer type.
The checker accepts the literal only when its value fits the target type.

The accepted contexts are existing typed contexts, not new syntax:

- function arguments
- return values, including `!T` success payloads
- assignment to an already typed binding
- struct literal fields
- union payload constructors
- typed std/container APIs such as `Array<u8>.append`, `String.append_byte`,
  `Map<[]const u8, u8>.insert`, `Channel<u8>.send`, `Mutex<u8>`,
  `Box<u8>`, and `ptr_write(ptr<u8>, value)`

`let x = 1;` still gives `x` type `i64`. Passing `x` to a narrower API still
requires `cast<T>(x)`.

Integer suffix syntax such as `1_u8` is not introduced by this ADR. A future ADR
may add suffixes if typed local constants need a compact spelling.

## Consequences

- Small literals in explicit low-level APIs no longer need `cast<T>(literal)`.
- Kizu still has no implicit numeric promotion for variables or expressions.
- Overflow remains explicit: `take_u8(256)` is rejected before execution.
- The Go type checker and ownership checker share the same contextual literal rule.
- The self-host parser does not need a new syntax path for this change.
