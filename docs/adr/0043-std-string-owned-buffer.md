# ADR 0043: std::string::String Owned Buffer

## Status

Accepted.

## Context

Kizu string literals are `[]u8`. They are read-only byte slices, not owned
strings. The self-host compiler still needs an owned byte buffer for diagnostics
and generated messages without reintroducing a primitive `string` type or hidden
allocation.

## Decision

`std::string::String` is the v0.2 owned string prototype:

```text
std::string::String(allocator: Allocator) -> std::string::String
string.append_bytes(bytes: []u8) -> !void
string.append_byte(byte: u8) -> !void
string.reserve(additional: i64) -> !void
string.truncate(length: i64) -> !void
string.len() -> i64
string.capacity() -> i64
string.as_bytes() -> []u8
string.clear() -> void
string.deinit() -> void
```

ADR 0057 moves this behavior into `std/src/string/string.kizu` over private
`std::array::Array<u8>` storage. Go no longer owns string-specific runtime
behavior.

The constructor requires an explicit allocator capability. `append_bytes` copies
from a read-only byte slice and does not move the source. `append_byte` appends
one byte. `reserve` requests capacity for at least `additional` more bytes.
`truncate` shortens the buffer while preserving capacity and fails if `length`
is outside `0..len`. Allocation failure is represented as `!void`.

`std::string::String` is non-copy and move-only. Copying a `String` would require
either hidden allocation or shared ownership, so safe Kizu treats it as an owned
resource like `std::array::Array<T>`.

`as_bytes` returns a local read-only view into the owned buffer. To keep safe
Kizu memory-safe, `as_bytes` must be bound as a local view:

```text
let bytes = string.as_bytes();
```

Direct use such as `print(string.as_bytes())` or `return string.as_bytes()` is
rejected. While the local view is alive, `append_bytes`, `append_byte`,
`truncate`, `clear`, and `deinit` are rejected. Last-use borrow release allows
mutation after the view's final use.

`append_bytes`, `append_byte`, `reserve`, `truncate`, and `clear` may be called
on an owned local `String` or through `&var std::string::String`. `clear` sets
length to zero while keeping capacity for reuse. `capacity` exposes current
capacity for allocation planning. `deinit` requires an owned local receiver
because a borrowed callee cannot invalidate the caller's binding. After
`deinit`, the binding is treated as moved/deinitialized and cannot be used
again.

`std::string::String` is bytes-first in v0.2. It does not validate UTF-8, expose
raw pointers, or implicitly convert to a C ABI string.

v0.2 deliberately does not include:

- `into_bytes` or other owned-byte extraction APIs
- String-specific equality helpers
- String-specific indexing or slicing APIs
- operator overloads for String comparison

Code should use `string.as_bytes()` plus `std::mem` helpers for read-only byte
operations. This keeps `String` focused on owned byte-buffer construction.

`std::mem::index_of` remains deferred until `option<T>` runtime helpers are
implemented.

## Consequences

- `string` does not return as a primitive type.
- Owned string allocation is visible at construction.
- Safe byte views do not outlive or race with String mutation/deinit.
- The self-host compiler can build simple diagnostics without hidden runtime
  allocation behavior.
- `std::path::join` and `std::path::clean` can be Kizu-owned by returning
  `!std::string::String` from an explicit allocator-backed buffer.
- testing diagnostics can move from Go after byte formatting helpers are
  implemented on top of `String`.
