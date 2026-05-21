# ADR 0042: std::array::Array<T> Owned Buffer

## Status

Accepted.

## Context

The Kizu self-host compiler frontend needs growable token and syntax-node lists.
Those lists require owned contiguous storage, but Kizu must avoid hidden default
allocators and unchecked pointer exposure in safe code.

## Decision

`std::array::Array<T>` is the first owned collection prototype.

```text
let allocator = std::mem::page_allocator();
let values = std::array::Array<i64>(allocator);
```

The v0.2 interpreter prototype supports:

```text
std::mem::page_allocator() -> Allocator
std::array::Array<T>(allocator: Allocator) -> std::array::Array<T>
array.append(value: T) -> !void
array.len() -> i64
array.capacity() -> i64
array.get(index: i64) -> !T
array.get_or_panic(index: i64) -> T
array.at(index: i64) -> !&T borrows self
array.at_mut(index: i64) -> !&mut T borrows self
array.set(index: i64, value: T) -> !void
array.deinit() -> void
```

`Allocator` is an explicit capability. Array construction must name an
allocator factory or binding; `std::array::Array<T>()` is rejected.

`append` moves non-copy values into the array. In v0.2, `get` is copy-only and
returns `!T` so out-of-bounds access is a recoverable checked error.
`get_or_panic` is the explicit trap variant for tests and invariant-checked
code where a bounds failure should stop execution instead of propagating.
Non-copy elements are read or updated through local borrow views returned by
`at` and `at_mut`.

While an element borrow is alive, `append`, `set`, and `deinit` are rejected.
While a mutable element borrow is alive, reads such as `get`, `len`, and
`capacity` are also rejected. This keeps Rust-style aliasing and reallocation
safety without exposing lifetime annotations.

Array element types are conservative in v0.2. Safe `Array<T>` rejects raw
pointers, `arena<T>`, `handle<T>`, nested arrays, and concurrency capability
types. The rejection is recursive through struct fields and union payloads.
These exclusions avoid storing values whose lifetime, provenance, or
thread-boundary rules are not yet fully specified for owned collections.

`deinit` invalidates the array binding in the ownership checker. Using an array
after `deinit` is a move/use-after-free style error in safe Kizu.

## Consequences

- Safe Kizu has an explicit allocator boundary before owned collections.
- Recoverable bounds failures stay readable `!T` errors through `get`.
- Test/invariant code can opt into a named trap with `get_or_panic`.
- `Array<T>` does not expose raw pointers.
- `Array<T>` cannot cross task/thread/channel boundaries in v0.2.
- Self-host work can use copy token enums with `get` and non-copy token structs
  with local `at` / `at_mut` borrows.
