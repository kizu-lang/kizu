# ADR 0055: std::fmt diagnostic formatting

Status: 採用

## Context

`std::testing` diagnostics should move out of Go-backed builtins without losing
useful assertion messages. A shortcut such as replacing value-aware diagnostics
with fixed messages would reduce the trusted Go surface, but it would also make
the test runner less useful for self-host compiler component tests.

Kizu already has `std::string::String` as the explicit allocator-backed owned
byte buffer. Diagnostic formatting should build on that buffer instead of adding
hidden allocation or keeping ordinary formatting behavior in Go.

## Decision

Add a minimal `std::fmt` module for deterministic diagnostic formatting into an
existing `std::string::String` buffer.

```text
std::fmt::append_i64(out: &mut std::string::String, value: i64) -> !void
std::fmt::append_bool(out: &mut std::string::String, value: bool) -> !void
std::fmt::append_bytes_literal(
    out: &mut std::string::String,
    bytes: []const u8,
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

- `std::testing` can construct value-aware assertion diagnostics in Kizu source
  and delete `std::builtin::testing_*` formatting behavior after this API exists.
- Diagnostic construction stays explicit about ownership and allocation.
- The Go implementation may keep only the `String` storage primitives and other
  host/runtime boundaries; scalar formatting should be Kizu-movable behavior.
- Future formatting expansion must remain explicit and should be motivated by
  self-host compiler needs before adding surface area.
