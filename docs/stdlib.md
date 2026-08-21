# Kizu Standard Library Plan

This document tracks the current Go-backed standard-library prototypes and the
path toward a Kizu-written `std`.

Active migration work is tracked by #95.

各 module の API リファレンスは [docs/std/](std/README.md) です。この文書は
移行計画と builtin registry を持ちます。

The goal is to avoid letting `std::...` behavior remain an implicit collection
of hard-coded Go branches. Every public `std` API should have a specification,
examples, conformance coverage, and a clear migration path from Go builtin code
to Kizu source.

## Current Model

Kizu implements `std` as trusted compiler builtins.

The implementation is intentionally split by compiler responsibility:

- `internal/types`: type signatures, return types, and static type errors.
- `internal/ownership`: move, borrow, arena, concurrency, and std safety rules.
- `internal/ir` and the backends: runtime behavior.
- `examples`: positive and negative user-facing behavior.
- `tests/behavior`: one package of behavior assertions, linked and run once.

This is acceptable for now, but new `std` APIs must not be added only as local
Go branches. They need a row in this document, examples, and conformance tests.

## Migration Policy

Move stdlib behavior from Go to Kizu in this order:

1. Pure functions that do not allocate or touch host resources.
2. Owned containers whose trusted primitive operations are small and explicit.
3. Formatting, diagnostics, and test helpers.
4. File, process, and I/O wrappers around explicit host capabilities.
5. Concurrency wrappers around trusted runtime primitives.

Go remains responsible for primitive host boundaries that Kizu cannot implement
without a lower-level backend or trusted runtime support:

- allocator primitive creation and raw memory allocation
- host file reads and writes
- process argv/env access
- stdout/stderr/stdin host I/O
- thread creation and joining
- atomic and mutex runtime primitives
- future backend-specific intrinsics

Kizu source should wrap these primitives with safe APIs. The safe wrapper owns
argument validation, error shaping, capability visibility, and conformance
behavior whenever possible.

Trusted Go primitives live under `internal/stdprim`. New host or runtime
boundaries should be added there first, then exposed through Kizu `std` wrappers
under `lib/kizu/std/src`.

## Allocator Capability

`std::mem::page_allocator() -> Allocator` and
`std::mem::fixed_buffer(bytes: &var []u8) -> Allocator` are the stable
allocator factories. `Allocator` is a visible opaque capability type, not a
user-facing contract or struct. An untied allocator is copyable: passing it to
`Array<T>`, `String`, `Map<K, V>`, `Box<T>`, or `std::arena::Arena<T>` reads
the capability and does not move the binding.

`fixed_buffer` returns a **tied** allocator: it holds the buffer's writable
view exclusively, must be bound with `let`, cannot be aliased or escape the
frame, and every owner allocated from it is tied to the buffer the same way
(SPEC §15.3, ADR-0099). Freeing is a no-op; the memory comes back when the
allocator and its owners are gone or the buffer's frame ends. Exhaustion is
`OutOfMemory`.

Owned storage created with an allocator keeps whatever it needs for allocation
and `deinit` internally. The allocator value itself has no public cleanup
method. Allocating operations continue to report allocation failure as
`!T` or `!void`.

There is no hidden default allocator and no implicit fallback to
`page_allocator()`. Safe `std::mem` APIs do not expose raw pointer allocation
methods, allocator metadata, mutable backing slices, or deallocation primitives.

## Builtin Thinning Policy

`std::internal::builtin::*` is not a permanent home for ordinary library behavior. It is a
temporary trusted boundary for what Kizu source cannot yet express.

Every builtin falls into one of three classes:

| Class | Meaning | Action |
| --- | --- | --- |
| Kizu-movable | Pure logic or validation that can be written in Kizu with existing language features | Move to `lib/kizu/std/src/*.kizu` and delete the Go branch |
| Blocked-by-language | Logic that should be Kizu eventually, but needs missing language/runtime features | Keep temporarily and track the blocker |
| Host primitive | OS, process, file, allocator, thread, atomic, or backend boundary | Keep as a small trusted primitive |

Rules:

- public `std::...` APIs should live in Kizu source
- Go branches should use `std::internal::builtin::*` names, not public `std::...` names
- Kizu-movable builtins should not gain new behavior in Go
- host primitives must stay small, explicit, and capability-shaped
- deleting a builtin requires examples and conformance to keep behavior stable
- native work must treat remaining host primitives as the explicit runtime
  boundary, not as general stdlib implementation

Current builtin thinning candidates:

| Builtin | Class | Next action |
| --- | --- | --- |
| `std::internal::builtin::mem_equal_bytes` | Removed | Implemented in `lib/kizu/std/src/mem.kizu` |
| `std::internal::builtin::mem_starts_with` | Removed | Implemented in `lib/kizu/std/src/mem.kizu` |
| `std::internal::builtin::mem_trim_ascii` | Removed | Implemented in `lib/kizu/std/src/mem.kizu` |
| `std::internal::builtin::path_basename` | Removed | Implemented in `lib/kizu/std/src/path.kizu` |
| `std::internal::builtin::path_dirname` | Removed | Implemented in `lib/kizu/std/src/path.kizu` |
| `std::internal::builtin::path_extension` | Removed | Implemented in `lib/kizu/std/src/path.kizu` |
| `std::internal::builtin::testing_*` | Removed | Replaced by the single explicit `std::internal::builtin::test_fail` trap primitive |
| `std::internal::builtin::test_fail` | Host primitive | Keep as the explicit trap boundary behind `std::testing::expect` |
| `std::internal::builtin::test_fail_equal<T>` | Host primitive | Keep as the typed diagnostic trap behind `std::testing::expect_equal<T>`; direct user calls are rejected |
| `std::internal::builtin::path_clean` | Removed | Implemented in `lib/kizu/std/src/path.kizu` with explicit allocator-backed `String` output |
| `std::internal::builtin::path_join` | Removed | Implemented in `lib/kizu/std/src/path.kizu` with explicit allocator-backed `String` output |
| `std::internal::builtin::fs_*` | Host primitive | Keep as explicit-Io filesystem boundary; public wrappers live in `lib/kizu/std/src/fs.kizu` |
| `std::internal::builtin::mem_len` | Host primitive for now | Keep as slice metadata access |
| `std::internal::builtin::mem_byte_at` | Removed | Implemented in `lib/kizu/std/src/mem.kizu` using checked index syntax |
| `std::internal::builtin::mem_slice` | Removed | Implemented in `lib/kizu/std/src/mem.kizu` using checked slice syntax |
| `std::internal::builtin::mem_page_allocator` | Host primitive | Keep as the small runtime primitive behind stable `std::mem::page_allocator() -> Allocator`; custom allocators are deferred to #549 |
| `std::internal::builtin::box<T>`, `std::internal::builtin::box_borrow<T>`, `std::internal::builtin::box_borrow_mut<T>`, `std::internal::builtin::box_deinit<T>` | Runtime primitive | Public constructor and methods live in `lib/kizu/std/src/mem.kizu`; direct user calls are rejected |
| `std::internal::builtin::string_*` | Removed | `std::string::String` behavior lives in `lib/kizu/std/src/string.kizu`; storage uses the lower-level `std::array::Array<u8>` runtime boundary. |
| `std::internal::builtin::io_*` | Host primitive | Keep as explicit Io / host stream boundary |
| `std::internal::builtin::process_arg_count`, `std::internal::builtin::process_arg`, `std::internal::builtin::process_env` | Host primitive | Keep as host process boundary |
| `std::internal::builtin::process_exit_code` | Removed | Implemented in `lib/kizu/std/src/process.kizu` as a pure value helper |
| `std::internal::builtin::array<T>`, `std::internal::builtin::array_*<T>` | Runtime primitive | Public constructor and methods live in `lib/kizu/std/src/array.kizu`; direct user calls are rejected |
| `std::internal::builtin::map<K, V>`, `std::internal::builtin::map_*<K, V>` | Runtime primitive | Public constructor and methods live in `lib/kizu/std/src/map.kizu`; direct user calls are rejected |

`std::testing` now keeps the public assertion surface in `lib/kizu/std/src/testing.kizu`.
`expect(condition)` returns `void` and uses the single explicit
`std::internal::builtin::test_fail` trap on failure, so normal assertions do not require
`try`. `expect_equal<T>(expected, actual)` is a minimal explicit generic helper
over `std::internal::builtin::test_fail_equal<T>` so callers get expected/got diagnostics
without per-type assertion families. The builtin remains a std-only trap
boundary, not a general formatting or reflection API.

Stateful runtime APIs such as `std::array::Array` and `std::map::Map` still keep
Go runtime primitives where they own runtime storage and borrow-safety rules.
Treat those primitives as explicit runtime boundaries, not ordinary stdlib logic.
The first wrapper split was tracked by #360 and must not leave dual public paths
behind. `std::mem::box<T>(allocator, value)`, `std::array::new<T>(allocator)`,
and `std::map::new<K, V>(allocator)` use source-level type-argument forwarding
through Kizu std source.

The concurrency modules (`std::task`, `std::channel`, `std::thread`,
`std::sync`, `std::atomic`) and `std::io::threaded()` were withdrawn by
ADR-0025. They had checker rules but no lowering and no runtime. Threads return
for parallel work once a real execution path exists.

## Builtin Registry

| Module | Current APIs | Current Go responsibility | Kizu migration target |
| --- | --- | --- | --- |
| `std::mem` | `page_allocator`, `Box<T>`, `borrow`, `borrow_mut`, `deinit`, `len`, `byte_at`, `equal_bytes`, `starts_with`, `slice`, `trim_ascii` | Kizu module in `lib/kizu/std/src/mem.kizu`; allocator, Box storage, Box local borrow, Box deinit, and len use trusted primitives | keep only allocator capability, Box storage/local-borrow boundary, and slice metadata primitives trusted |
| `std::array` | `Array<T>`, `append`, `len`, `capacity`, `reserve`, `clone`, `pop`, `pop_or_panic`, `get`, `get_or_panic`, `at`, `at_mut`, `set`, `deinit` | Kizu constructor and method wrappers over reserved `std::internal::builtin::array_*`; Go owned storage, bounds checks, element borrow tracking, deinit state | keep allocation/storage and local element borrow primitives trusted |
| `std::string` | `String`, `from_bytes`, `join`, `trim_space_in_place`, `append_bytes`, `append_byte`, `append_string`, `reserve`, `truncate`, `clear`, `len`, `capacity`, `as_bytes`, `as_mut_bytes`, `deinit` | Kizu implementation in `lib/kizu/std/src/string.kizu` backed by private `std::array::Array<u8>` storage | use as the explicit owned byte buffer for path construction and diagnostics; keep raw storage private and mutable access exclusive |
| `std::fmt` | `append_i64`, `append_bool`, `append_bytes_literal` | Kizu source over `String` | no hidden allocation or Go scalar formatting |
| `std::json` | `encoder`, `begin_object`, `end_object`, `begin_array`, `end_array`, `begin_object_field`, `begin_array_field`, `write_i64`, `write_bool`, `write_null`, `write_bytes`, `write_*_field`, `finish_into`, `deinit`, `encode<T>`, `encode_value<T>` | Kizu source over `String` and `std::meta`; API misuse traps through `std::internal::builtin::panic` | keep only the trap primitive trusted; `decode<T>` and `Value` still to come |
| `std::map` | `Map<[]u8, V>`, `insert`, `get`, `contains`, `len`, `deinit` | Kizu constructor and method wrappers over reserved `std::internal::builtin::map_*`; Go owned key/value storage, key copy, copy-only value rule, boundary checks | keep hash table primitive until Kizu has arrays/slices robust enough |
| `std::testing` | `expect`, `expect_equal<T>`, `fail` | Kizu source over explicit `std::internal::builtin::test_fail` and `std::internal::builtin::test_fail_equal<T>` traps | keep Go limited to the runner, assertion trap, typed equality diagnostic trap, and error-union reporting boundary |
| `std::fs` | `read_file`, `read_file_into`, `write_file`, `rename`, `exists`, `metadata`, `read_dir`, `create_dir`, `remove_dir`, `remove_file`, `Metadata`, `DirEntry` | Kizu wrappers in `lib/kizu/std/src/fs.kizu` over `std::internal::builtin::fs_*` host filesystem primitives | migrated wrapper module; keep host filesystem calls primitive |
| `std::path` | `join`, `clean`, `basename`, `dirname`, `extension` | Kizu module in `lib/kizu/std/src/path.kizu`; `join` and `clean` return allocator-backed `std::string::String` | keep only allocator and Array storage primitives trusted |
| `std::io` | `blocking`, `failing`, `write_stdout`, `write_stderr`, `read_stdin`, `read_stdin_into` | Kizu wrappers in `lib/kizu/std/src/io.kizu` over `std::internal::builtin::io_*` primitives | migrated wrapper module; keep host I/O and explicit capability construction trusted |
| `std::process` | `arg_count`, `arg`, `env`, `exit_code` | Kizu wrappers in `lib/kizu/std/src/process.kizu`; only arg count, arg, and env use host primitives | keep host process access primitives trusted |

## Source Layout

std is a package. `internal/project` loads it the same way it loads a program's
own package, and `internal/stdlib` only says which directory the library tree
is.

```text
lib/kizu/std/
  README.md
  kizu.toml
  src/
    internal/
      builtin.kizu      std::internal::builtin -- reachable from std only
    path/
      internal/
        bits.kizu       std::path::internal::bits -- reachable from std::path only
    arena.kizu
    mem.kizu
    array.kizu
    string.kizu
    map.kizu
    testing.kizu
    fs.kizu
    path.kizu
    io.kizu
    process.kizu
```

A module below an `internal` directory is reachable from the subtree that
directory hangs off and nowhere else. That is the whole visibility rule: the
manifest lists no exports.

The compiler still reserves the root namespace `std`. User packages cannot be
named `std`.

The non-shipping compiler port lives under `compiler/`. It uses these public std
wrappers and explicit runtime primitives; it must not add a second std surface
or reach around them into Go helpers.

## Acceptance Rules For New Std APIs

New std APIs require all of the following in the same change:

- The relevant `docs/std/` reference updated with signature, ownership, error,
  capability, and observable behavior.
- An ADR only when a separate design decision has rationale or rejected
  alternatives worth preserving. Do not duplicate the current API contract or
  implementation comments in an ADR.
- This registry updated with the current implementation boundary.
- Positive example under `examples/`.
- Negative example when the API has safety or error boundaries.
- A case block at the end of that example saying what running it produces.
- No hidden global allocator, hidden global runtime, implicit blocking I/O, or
  silent fallback behavior.

## Migration Gates

Before replacing a Go builtin with Kizu source, the following must be true:

- module resolver can load `std` sources deterministically
- diagnostics can report spans in both user source and std source
- conformance cases pass through the Go implementation
- cache keys include std source hashes and public interface hashes
- host primitives remain explicit and small
- safe wrappers preserve the memory-safety contract in `docs/memory-safety.md`

## Byte Storage Boundary

`std::string::String` is the current Kizu-facing owned byte buffer. ADR 0057
moves its behavior into Kizu source over private `std::array::Array<u8>`
storage. The remaining trusted boundary is Array storage, not string-specific
Go logic. ADR 0056 still rejects a public `std::mem::OwnedBytes` type until
mutable slice, raw storage provenance, and generic container rules are
specified.

The Kizu `String` implementation uses std-only Array storage helpers for
reserve, truncate, clear, and byte-slice exposure. Those helpers are not public
`std::array` API.

The migration path is:

- keep `String` public behavior in Kizu source over private Array storage
- keep `std::internal::builtin::string_*` removed so user code cannot bypass wrapper
  borrow and move checks
- add regression coverage for each storage safety rule before expanding the
  primitive set
- use `String` for diagnostic and byte-building needs
- revisit a public byte storage type only when it can preserve allocator,
  cleanup, and view-borrow safety without raw pointer exposure
