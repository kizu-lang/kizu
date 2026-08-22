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

Add a minimal `std::fmt` module for deterministic diagnostic formatting. Its
heterogeneous entry points use the ordinary trailing runtime capture from
ADR-0066; formatting itself is not compiler-known.

```kizu
pub contract Display {
    fn append_display(out: &var std::string::String) -> !void;
}
```

```text
std::fmt::append(
    out: &var std::string::String,
    parts: ...,
) -> !void
std::fmt::format(allocator: Allocator, parts: ...) -> !std::string::String
std::fmt::append_i64(out: &var std::string::String, value: i64) -> !void
std::fmt::append_bool(out: &var std::string::String, value: bool) -> !void
std::fmt::append_bytes_literal(
    out: &var std::string::String,
    bytes: []u8,
) -> !void
```

`append` handles `[]u8`, `std::string::String`, `i64`, and `bool` canonically.
For any other concrete type it statically calls `part.append_display(out)`.
Users define that ordinary method and may write `impl std::fmt::Display for T;`
as the existing optional structural assertion. There is no runtime interface,
vtable, reflection, or erased argument. `std::fmt` calls the method through a
shared borrow, so the canonical method uses `self: &T` and does not consume or
mutate the formatted value.

```kizu
fn (self: &User) append_display(out: &var std::string::String) -> !void {
    return std::fmt::append(out, "User(", self.name, ")");
}

impl std::fmt::Display for User;
```

`format` returns a new owned `String` allocated from the explicit allocator.
The three typed append functions remain public low-level building blocks.
Types with multiple useful representations use an explicit separate helper or
formatter type. `Display` means one canonical human-readable representation;
formatting would imply caller-selected placeholders or specifiers, which this
API does not provide.

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

This does not add format strings, locale-aware formatting, UTF-8 validation,
runtime reflection, or implicit conversion to C strings.

When compiler code has already built an owned message, its internal
`diagnostic::new_owned` constructor takes that `String` by move. The borrowed
`diagnostic::new` constructor remains for source/static bytes. This prevents
the heterogeneous builder from being followed by a second full message copy.

## Consequences

- Caller code can construct value-aware test diagnostics in Kizu source and pass
  the resulting bytes to `std::testing::fail`.
- Diagnostic construction stays explicit about ownership and allocation.
- User-defined types participate through ordinary static method dispatch; the
  std module is not a closed primitive whitelist.
- Calls contain literal and value parts directly, without a format-string
  parser, erased argument array, runtime type dispatch, or hidden allocation.
- The Go implementation may keep only lower-level storage primitives and other
  host/runtime boundaries; scalar formatting should be Kizu-movable behavior.
- Future formatting expansion must remain explicit and should be motivated by
  self-host compiler needs before adding surface area.

Rejected alternatives are:

| Alternative | Reason |
| --- | --- |
| `{}` format templates plus a separate argument list | The placeholder-to-value mapping is positional twice, needs a template parser and diagnostics, and obscures which source expression produces each text fragment. |
| `f"..."` interpolation syntax | It makes formatting a privileged expression form and must define allocation, failure, escaping, and lifetime behavior in the language rather than the library. |
| A closed builtin type switch | It cannot display user-defined compiler data without adding cases to std or the compiler; static `append_display` methods provide the same direct calls without runtime reflection. |
| Formatting directly through a generic `Writer` | The current need produces an owned diagnostic `String`; combining representation with capability-bearing I/O adds an abstraction without removing this storage boundary. |
