# ADR 0043: std::string::String Owned Buffer

## Status

Accepted.

## Context

Kizu string literals are `[]const u8`. They are read-only byte slices, not owned
strings. The self-host compiler still needs an owned byte buffer for diagnostics
and generated messages without reintroducing a primitive `string` type or hidden
allocation.

## Decision

`std::string::String` is the v0.2 owned string prototype:

```text
std::string::String(allocator: Allocator) -> std::string::String
string.append_bytes(bytes: []const u8) -> !void
string.append_byte(byte: u8) -> !void
string.len() -> i64
string.as_bytes() -> []const u8
string.clear() -> void
string.deinit() -> void
```

The constructor requires an explicit allocator capability. `append_bytes` copies
from a read-only byte slice and does not move the source. `append_byte` appends
one byte. Allocation failure is represented as `!void`.

`as_bytes` returns a local read-only view into the owned buffer. To keep safe
Kizu memory-safe, `as_bytes` must be bound as a local view:

```text
let bytes = string.as_bytes();
```

Direct use such as `print(string.as_bytes())` or `return string.as_bytes()` is
rejected. While the local view is alive, `append_bytes`, `append_byte`, `clear`,
and `deinit` are rejected. Last-use borrow release allows mutation after the
view's final use.

`append_bytes`, `append_byte`, and `clear` may be called on an owned local
`String` or through `&mut std::string::String`. `deinit` requires an owned local
receiver because a borrowed callee cannot invalidate the caller's binding.

`std::string::String` is bytes-first in v0.2. It does not validate UTF-8, expose
raw pointers, or implicitly convert to a C ABI string.

`std::mem::index_of` remains deferred until `option<T>` runtime helpers are
implemented.

## Consequences

- `string` does not return as a primitive type.
- Owned string allocation is visible at construction.
- Safe byte views do not outlive or race with String mutation/deinit.
- The self-host compiler can build simple diagnostics without hidden runtime
  allocation behavior.
