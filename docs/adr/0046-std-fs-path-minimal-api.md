# ADR 0046: std::fs / std::path minimal API

Status: 採用

## Context

The self-host compiler needs source loading, artifact path construction, and
basic diagnostic path decomposition. Kizu also keeps hidden I/O out of safe
standard-library APIs.

## Decision

`std::path` is pure and does not touch the filesystem.

```text
std::path::join(allocator: Allocator, left: []u8, right: []u8)
  -> !std::string::String
std::path::clean(allocator: Allocator, path: []u8) -> !std::string::String
std::path::basename(path: []u8) -> []u8 borrows path
std::path::dirname(path: []u8) -> []u8 borrows path
std::path::extension(path: []u8) -> []u8 borrows path
```

`join` and `clean` construct owned bytes, so allocation is explicit through the
caller-provided allocator. Callers pass `result.as_bytes()` to APIs that need
`[]u8` and deinitialize the returned `String` after the final view use.

`std::fs` always requires explicit `Io`.

```text
std::fs::read_file(io: Io, allocator: Allocator, path: &[]u8, limit: Limit)
  -> !std::string::String
std::fs::read_file_into(io: Io, allocator: Allocator, path: &[]u8,
  out: &var std::string::String) -> !void
std::fs::real_path(io: Io, allocator: Allocator, path: &[]u8)
  -> !std::string::String
std::fs::write_file(io: Io, path: &[]u8, bytes: &[]u8) -> !void
std::fs::rename(io: Io, from: &[]u8, to: &[]u8) -> !void
std::fs::exists(io: Io, path: &[]u8) -> !bool
std::fs::metadata(io: Io, path: &[]u8) -> !std::fs::Metadata
std::fs::read_dir(io: Io, allocator: Allocator, path: &[]u8)
  -> !std::array::Array<std::fs::DirEntry>
std::fs::create_dir(io: Io, path: &[]u8) -> !void
std::fs::remove_dir(io: Io, path: &[]u8) -> !void
std::fs::remove_file(io: Io, path: &[]u8) -> !void
```

The byte-slice parameters are read-only borrows. `std::fs` does not retain them,
so callers can pass string literals or local views such as `String.as_bytes()`
without transferring ownership.

`std::fs::Metadata` is intentionally narrow in v0.2:

```text
size: i64
is_dir: bool
```

`std::fs::DirEntry` owns the bytes returned by directory enumeration:

```text
name: std::string::String
path: std::string::String
is_dir: bool
```

`read_dir` names the allocator for both its Array and these Strings. The
Array's element cleanup calls `DirEntry.deinit`, so one
`entries.deinit(allocator)` releases the complete result.

## Rejected alternatives

| Alternative | Reason |
| --- | --- |
| Borrowed `name` / `path` in the returned Array | Host enumeration buffers expire, and Array growth can relocate storage; neither gives a stable source for the views. |
| No allocator argument | It hides allocation and leaves callers unable to name the allocator that must release each entry. |

## Consequences

- Compiler path construction can be tested without filesystem side effects.
- Filesystem APIs remain explicit and testable with `std::testing::failing_io()`.
- Directory results have one visible ownership and cleanup path.
- Metadata is not a stable platform abstraction yet; more fields require a
  later ADR.
