# ADR 0041: std::mem and Allocator Boundary

## Status

Accepted.

## Context

Kizu v0.2 starts building the standard library needed by the future self-host
compiler. The frontend needs to scan source buffers, compare byte ranges, and
take checked sub-slices before owned collections or a general allocator exist.

At the same time, Kizu must not introduce hidden allocation, unchecked raw
pointer access, or implicit runtime behavior through convenient byte helpers.

## Decision

`std::mem` starts as an allocation-free trusted std prototype for read-only byte
slices:

```text
std::mem::len(bytes: []const u8) -> i64
std::mem::byte_at(bytes: []const u8, index: i64) -> !u8
std::mem::equal_bytes(left: []const u8, right: []const u8) -> bool
std::mem::starts_with(bytes: []const u8, prefix: []const u8) -> bool
std::mem::slice(bytes: []const u8, start: i64, end: i64) -> ![]const u8
std::mem::trim_ascii(bytes: []const u8) -> []const u8
```

`byte_at` and `slice` return `!T` because an invalid index or range is a
recoverable failure with a reason. Absence-oriented APIs such as
`index_of -> ?usize` are deferred until `option<T>` has runtime helpers.

`std::mem` safe APIs return values or `[]const u8` views only. They do not return
raw pointers, do not allocate, and do not weaken borrow, move, or raw pointer
checks for callers.

The future mutable-buffer APIs are reserved as follows:

```text
std::mem::copy(dst: slice<u8>, src: []const u8) -> !void
std::mem::zero(dst: slice<u8>) -> void
std::mem::fill(dst: slice<u8>, byte: u8) -> void
```

These APIs require mutable slice and allocator-backed collection semantics, so
they must be implemented with `std::array::Array<T>` or general slice support.
They must remain safe wrappers around trusted stdlib internals and must not
expose raw pointer writes to safe Kizu.

## Consequences

- A self-host lexer can scan `[]const u8` with checked byte access and slicing.
- Byte compare and prefix checks are allocation-free.
- Hidden global allocator behavior is avoided.
- `index_of` and mutable byte operations have explicit blocking dependencies
  instead of placeholder behavior.
