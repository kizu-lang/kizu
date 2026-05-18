# ADR-0053: checked index and slice syntax

## Status

Accepted.

## Context

Kizu uses `[]const u8` for string literals and read-only byte slices. The
current standard library exposes byte access through functions such as
`std::mem::byte_at(bytes, index)` and `std::mem::slice(bytes, start, end)`.

That is enough for prototypes, but it makes Kizu-written stdlib code noisy and
keeps pure byte helpers in Go longer than necessary. To thin `std::builtin`,
Kizu needs direct index and slice syntax.

Zig has direct indexing and slicing. Kizu follows the explicit, low-level shape
of that syntax, but safe Kizu must not allow unchecked bounds access.

## Decision

Kizu will add checked index and slice expressions:

```kizu
let byte = try bytes[index];
let part = try bytes[start..end];
let tail = try bytes[start..];
let head = try bytes[..end];
```

Safe indexing returns an error union:

```text
[]const u8 [ i64 ] -> !u8
[]const u8 [ i64 .. i64 ] -> ![]const u8
```

The bounds are half-open: `start..end` includes `start` and excludes `end`.

Rules:

- index and slice bounds must be integer values
- negative index, negative bound, `start > end`, and `end > len` return an error
- safe Kizu never performs unchecked index or slice access
- `bytes[i]` and `bytes[a..b]` require `try` unless the surrounding expression
  explicitly handles the `!T`
- direct indexing is initially read-only and supports `[]const u8`
- mutable indexed assignment is not part of this decision
- indexed borrow such as `&items[i]` remains deferred
- `std::mem::byte_at` and `std::mem::slice` remain wrappers during migration,
  but Kizu std source should prefer syntax once implemented

## Deferred

- indexing `std::array::Array<T>` directly
- mutable slice syntax
- indexed borrow
- compile-time proof that a bounds check is unnecessary
- unchecked indexing in `unsafe`

## Consequences

`std::builtin::mem_equal_bytes`, `std::builtin::mem_starts_with`, and similar
pure helpers can move to Kizu source once this syntax is implemented.

The parser, AST, type checker, ownership checker, interpreter, IR, LLVM, and
WASM backends need explicit support before examples can rely on this syntax.
