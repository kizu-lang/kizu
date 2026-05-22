# ADR 0055: std::fmt diagnostic formatting

Status: 採用

## Context

Caller-owned diagnostics should move out of Go-backed builtins without adding
hidden formatting or allocation. `std::testing::expect` intentionally keeps a
fixed failure message in v0.2, but domain-specific tests still need an explicit
way to build messages before returning `std::testing::fail(...)`.

Kizu already has `std::string::String` as the explicit allocator-backed owned
byte buffer. Diagnostic formatting should build on that buffer instead of adding
hidden allocation or keeping ordinary formatting behavior in Go.

## Decision

Add a minimal `std::fmt` module for deterministic diagnostic formatting into an
existing `std::string::String` buffer.

```text
std::fmt::append_i64(out: &var std::string::String, value: i64) -> !void
std::fmt::append_bool(out: &var std::string::String, value: bool) -> !void
std::fmt::append_bytes_literal(
    out: &var std::string::String,
    bytes: []u8,
) -> !void
```

The functions append to caller-owned storage. They do not allocate implicitly;
any allocation comes from the `String` allocator and is reported through `!void`.

Output is deterministic ASCII:

- `append_i64` emits decimal digits, with `-` for negative values and no leading
  plus sign or leading zeroes except for zero itself.
- `append_bool` emits `true` or `false`.
- `append_bytes_literal` emits a quoted byte string for diagnostics. Printable
  ASCII bytes except `"` and `\` are emitted directly. `"` and `\` are escaped
  as `\"` and `\\`. Newline, carriage return, and tab are escaped as `\n`,
  `\r`, and `\t`. Other bytes are escaped as `\xNN` using uppercase
  hexadecimal.

This is not a general formatting framework. v0.2 does not add format strings,
locale-aware formatting, UTF-8 validation, generic display traits, reflection,
or implicit conversion to C strings.

## Consequences

- Caller code can construct value-aware test diagnostics in Kizu source and pass
  the resulting bytes to `std::testing::fail`.
- Diagnostic construction stays explicit about ownership and allocation.
- The Go implementation may keep only lower-level storage primitives and other
  host/runtime boundaries; scalar formatting should be Kizu-movable behavior.
- Future formatting expansion must remain explicit and should be motivated by
  self-host compiler needs before adding surface area.
