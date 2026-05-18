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
std::path::basename(path: []const u8) -> []const u8
std::path::dirname(path: []const u8) -> []const u8
std::path::extension(path: []const u8) -> []const u8
```

`join` and `clean` construct owned bytes, so allocation is explicit through the
caller-provided allocator. Callers pass `result.as_bytes()` to APIs that need
`[]const u8` and deinitialize the returned `String` after the final view use.

`std::fs` always requires explicit `Io`.

```text
std::fs::read_file(io: Io, path: []const u8) -> ![]const u8
std::fs::write_file(io: Io, path: []const u8, bytes: []const u8) -> !void
std::fs::exists(io: Io, path: []const u8) -> !bool
std::fs::metadata(io: Io, path: []const u8) -> !std::fs::Metadata
std::fs::create_dir(io: Io, path: []const u8) -> !void
std::fs::remove_dir(io: Io, path: []const u8) -> !void
std::fs::remove_file(io: Io, path: []const u8) -> !void
```

`std::fs::Metadata` is intentionally narrow in v0.2:

```text
size: i64
is_dir: bool
```

## Consequences

- Compiler path construction can be tested without filesystem side effects.
- Filesystem APIs remain explicit and testable with `std::io::failing()`.
- Metadata is not a stable platform abstraction yet; more fields require a
  later ADR.
