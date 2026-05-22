# ADR-0053: checked index and slice syntax

## Status

Accepted.

## Context

Kizu uses `[]u8` for string literals and byte slices. The current standard
library exposes read-only byte access through functions such as
`std::mem::byte_at(bytes, index)` and `std::mem::slice(bytes, start, end)`.

That is enough for prototypes, but it makes Kizu-written stdlib code noisy and
keeps pure byte helpers in Go longer than necessary. To thin `std::builtin`,
Kizu needs direct index and slice syntax.

Zig has direct indexing and slicing. Kizu follows the explicit, low-level shape
of that syntax, but safe Kizu must not allow unchecked bounds access.

## Decision

Kizu will add bounds-checked index and slice expressions:

```kizu
let byte = bytes[index];
let part = bytes[start..end];
let tail = bytes[start..];
let head = bytes[..end];
```

Index and slice syntax returns plain values:

```text
[]u8 [ i64 ] -> u8
[]u8 [ i64 .. i64 ] -> []u8
```

The bounds are half-open: `start..end` includes `start` and excludes `end`.

Rules:

- index and slice syntax is limited to one-dimensional contiguous sequences
- index and slice bounds must be integer values
- negative index, negative bound, `start > end`, and `end > len` trap with a
  safety check failure
- safe Kizu never performs unchecked index or slice access
- `bytes[i]` and `bytes[a..b]` do not return error unions
- recoverable bounds handling remains an explicit library API such as
  `std::mem::byte_at` and `std::mem::slice`
- direct indexing is initially read-only and supports `[]u8`
- mutable indexed assignment is not part of this decision
- indexed borrow such as `&items[i]` remains deferred
- `std::mem::byte_at` and `std::mem::slice` remain the recoverable alternatives
  that return error unions

## Deferred

- indexing `std::array::Array<T>` directly
- mutable slice syntax
- indexed borrow
- multi-dimensional slicing such as `matrix[rows, cols]`
- strided views and matrix views
- unchecked indexing in `unsafe`

## Consequences

`std::builtin::mem_equal_bytes`, `std::builtin::mem_starts_with`, and similar
pure helpers can move to Kizu source once this syntax is implemented.

This follows Zig's direct indexing shape more closely than Rust's `get` API:
safe Kizu preserves memory safety with mandatory bounds checks, but an out of
bounds syntax access is a safety violation rather than a recoverable value.
This keeps hot stdlib helpers such as equality and prefix checks as `bool`
returning functions.

The parser, AST, type checker, ownership checker, interpreter, IR, LLVM, and
WASM backends need explicit support before examples can rely on this syntax.
