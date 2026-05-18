# ADR 0056: allocator-backed byte storage boundary

Status: 提案

## Context

`std::string::String` is now a Kizu-facing wrapper, but its storage, capacity,
view, and cleanup operations remain trusted Go primitives. Moving more of
`String` or future byte-building compiler components into Kizu source requires
an allocator-backed byte storage boundary that does not expose raw pointers or
untracked lifetimes to safe Kizu.

The same need appears in the self-host compiler path:

- lexer diagnostic and token text construction
- path and formatting output buffers
- future byte-oriented builders
- eventual `std::array::Array<T>` storage design, after generic storage rules
  are mature

## Decision

Do not expose a general safe `std::mem::OwnedBytes` type yet.

Instead, keep byte storage as a restricted trusted storage boundary underneath
Kizu-written public wrappers. The first public consumer remains
`std::string::String`; future byte-builder APIs may share the same primitive
only after their ownership and view rules are specified.

The trusted boundary may provide primitives equivalent to:

```text
std::builtin::string_new(allocator: Allocator) -> std::string::String
std::builtin::string_append_byte(self: std::string::String, byte: u8) -> !void
std::builtin::string_append_bytes(
    self: std::string::String,
    bytes: []const u8,
) -> !void
std::builtin::string_reserve(self: std::string::String, additional: i64) -> !void
std::builtin::string_truncate(self: std::string::String, length: i64) -> !void
std::builtin::string_len(self: std::string::String) -> i64
std::builtin::string_capacity(self: std::string::String) -> i64
std::builtin::string_as_bytes(self: std::string::String) -> []const u8
std::builtin::string_clear(self: std::string::String) -> void
std::builtin::string_deinit(self: std::string::String) -> void
```

Public APIs must remain in Kizu source and call these primitives through
explicit wrappers. The trusted primitive set is storage-only; formatting,
testing diagnostics, path normalization, equality, scanning, and other ordinary
logic must stay in Kizu source when the language can express it.

## Safety Rules

- Construction requires an explicit `Allocator`.
- There is no hidden default allocator.
- The owned storage is move-only and non-copy.
- `append_bytes` copies source bytes and does not retain the source slice.
- `as_bytes` returns a local read-only view into the owned storage.
- While a view is alive, mutation and `deinit` are rejected.
- `deinit` invalidates the owner binding.
- `truncate` validates the target length and returns `!void` on invalid input.
- Allocation failure is returned as `!void`, not converted into a trap.
- Safe Kizu does not receive a raw pointer, address, or mutable slice into the
  backing allocation.

## Rejected Alternatives

### Public `std::mem::OwnedBytes`

A public owned byte allocation type would make the underlying storage reusable,
but it also creates a new low-level lifetime and aliasing surface. It should not
be introduced until mutable slice, raw storage provenance, and generic container
rules are specified together.

### Hidden Global Byte Builder

A global or implicit byte builder would hide allocation and cleanup. That
conflicts with Kizu's explicit allocator and cleanup policy.

### Kizu `String` Over `std::array::Array<u8>`

This is attractive long term, but v0.2 `Array<T>` is itself Go-backed storage and
has element and borrow restrictions that are not yet suitable as the foundation
for `String`. It would also risk circular dependencies between `std::string` and
`std::array`.

## Migration Path

1. Keep `std::string::String` public API in Kizu source over trusted storage
   primitives.
2. Add conformance for every storage safety rule before expanding the primitive
   set.
3. Introduce a byte-builder or internal byte-storage wrapper only if a self-host
   compiler component needs it and the wrapper has explicit allocator and
   cleanup semantics.
4. Revisit a public `std::mem::OwnedBytes` or `std::bytes::Buffer` only after
   mutable slice and raw storage provenance rules are specified.
5. Revisit `std::array::Array<T>` storage after byte storage is stable and
   generic storage rules can preserve element move/drop and borrow safety.

## Consequences

- Go remains the trusted backing allocation boundary for now.
- Ordinary stdlib logic continues moving to Kizu source.
- The next implementation step is narrow: #363 hardens and tests the existing
  `std::string::String` storage boundary instead of adding a broad memory API.
- This avoids introducing a public memory abstraction that would be difficult to
  make memory-safe retroactively.
