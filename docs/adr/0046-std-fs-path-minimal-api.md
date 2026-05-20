# ADR 0046: std::fs / std::path minimal API

Status: 採用

## Context

The self-host compiler needs source loading, artifact path construction, and
basic diagnostic path decomposition. Kizu also keeps hidden I/O out of safe
standard-library APIs.

## Decision

`std::path` is pure and does not touch the filesystem.

```text
std::path::join(allocator: Allocator, left: []const u8, right: []const u8)
  -> !std::string::String
std::path::clean(allocator: Allocator, path: []const u8) -> !std::string::String
std::path::basename<'a>(path: []'a const u8) -> []'a const u8
std::path::dirname<'a>(path: []'a const u8) -> []'a const u8
std::path::extension<'a>(path: []'a const u8) -> []'a const u8
```

`join` and `clean` construct owned bytes, so allocation is explicit through the
caller-provided allocator. Callers pass `result.as_bytes()` to APIs that need
`[]const u8` and deinitialize the returned `String` after the final view use.

`std::fs` always requires explicit `Io`.

```text
std::fs::read_file(io: Io, path: &[]const u8) -> ![]const u8
std::fs::write_file(io: Io, path: &[]const u8, bytes: &[]const u8) -> !void
std::fs::exists(io: Io, path: &[]const u8) -> !bool
std::fs::metadata(io: Io, path: &[]const u8) -> !std::fs::Metadata
std::fs::read_dir(io: Io, path: &[]const u8) -> !std::array::Array<std::fs::DirEntry>
std::fs::create_dir(io: Io, path: &[]const u8) -> !void
std::fs::remove_dir(io: Io, path: &[]const u8) -> !void
std::fs::remove_file(io: Io, path: &[]const u8) -> !void
```

The byte-slice parameters are read-only borrows. `std::fs` does not retain them,
so callers can pass string literals or local views such as `String.as_bytes()`
without transferring ownership.

`std::fs::Metadata` is intentionally narrow in v0.2:

```text
size: i64
is_dir: bool
```

`std::fs::DirEntry` is also narrow in v0.2:

```text
name: []const u8
path: []const u8
is_dir: bool
```

## Consequences

- Compiler path construction can be tested without filesystem side effects.
- Filesystem APIs remain explicit and testable with `std::io::failing()`.
- Metadata is not a stable platform abstraction yet; more fields require a
  later ADR.
