# Kizu Standard Library Plan

This document tracks the current Go-backed standard-library prototypes and the
path toward a Kizu-written `std`.

Active migration work is tracked by #95.

The goal is to avoid letting `std::...` behavior remain an implicit collection
of hard-coded Go branches. Every public `std` API should have a specification,
examples, conformance coverage, and a clear migration path from Go builtin code
to Kizu source.

## Current Model

Kizu v0.2 implements `std` as trusted compiler/interpreter builtins.

The implementation is intentionally split by compiler responsibility:

- `internal/types`: type signatures, return types, and static type errors.
- `internal/ownership`: move, borrow, arena, concurrency, and std safety rules.
- `internal/interp`: runtime behavior for the interpreter.
- `examples`: positive and negative user-facing behavior.
- `tests/conformance`: reusable behavior corpus for future compiler implementations.

This is acceptable for v0.2, but new `std` APIs must not be added only as local
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
under `std/src`.

## Builtin Thinning Policy

`std::builtin::*` is not a permanent home for ordinary library behavior. It is a
temporary trusted boundary used while the Go interpreter remains the execution
oracle.

Every builtin falls into one of three classes:

| Class | Meaning | Action |
| --- | --- | --- |
| Kizu-movable | Pure logic or validation that can be written in Kizu with existing language features | Move to `std/src/*.kizu` and delete the Go branch |
| Blocked-by-language | Logic that should be Kizu eventually, but needs missing language/runtime features | Keep temporarily and track the blocker |
| Host primitive | OS, process, file, allocator, thread, atomic, or backend boundary | Keep as a small trusted primitive |

Rules:

- public `std::...` APIs should live in Kizu source
- Go branches should use `std::builtin::*` names, not public `std::...` names
- Kizu-movable builtins should not gain new behavior in Go
- host primitives must stay small, explicit, and capability-shaped
- deleting a builtin requires examples and conformance to keep behavior stable
- native/self-host work must treat remaining host primitives as the explicit
  runtime boundary, not as general stdlib implementation

Current builtin thinning candidates:

| Builtin | Class | Next action |
| --- | --- | --- |
| `std::builtin::mem_equal_bytes` | Kizu-movable | Move after byte indexing / loops are sufficient in std source |
| `std::builtin::mem_starts_with` | Kizu-movable | Move after byte indexing / loops are sufficient in std source |
| `std::builtin::mem_trim_ascii` | Kizu-movable | Move after slice construction and byte loops are sufficient |
| `std::builtin::path_basename` | Kizu-movable | Move after `std::mem` byte search helpers are Kizu-side |
| `std::builtin::path_dirname` | Kizu-movable | Move after `std::mem` byte search helpers are Kizu-side |
| `std::builtin::path_extension` | Kizu-movable | Move after `std::mem` byte search helpers are Kizu-side |
| `std::builtin::testing_expect*` | Kizu-movable | Move formatting once string/diagnostic construction exists in Kizu |
| `std::builtin::path_clean` | Blocked-by-language | Keep until path normalization can be implemented without hidden allocation |
| `std::builtin::path_join` | Blocked-by-language | Keep until owned string/buffer construction exists |
| `std::builtin::mem_len` | Host primitive for now | Keep as slice metadata access |
| `std::builtin::mem_byte_at` | Host primitive for now | Recoverable alternative to trapping index syntax |
| `std::builtin::mem_slice` | Host primitive for now | Recoverable alternative to trapping slice syntax |
| `std::builtin::mem_page_allocator` | Host primitive | Keep as allocator capability boundary |
| `std::builtin::io_*` | Host primitive | Keep as explicit Io / host stream boundary |
| `std::builtin::process_*` | Host primitive | Keep as host process boundary |

## Builtin Registry

| Module | Current APIs | Current Go responsibility | Kizu migration target |
| --- | --- | --- | --- |
| `std::mem` | `page_allocator`, `len`, `byte_at`, `equal_bytes`, `starts_with`, `slice`, `trim_ascii` | Kizu wrappers in `std/src/mem.kizu`; byte_at/slice use checked syntax | migrated wrapper module; keep allocator and remaining byte predicates trusted until loops/indexing are sufficient |
| `std::array` | `Array<T>`, `append`, `len`, `capacity`, `get`, `at`, `at_mut`, `set`, `deinit` | owned storage, bounds checks, element borrow tracking, deinit state | keep allocation/storage primitives trusted; move ergonomic wrappers and tests to `std/array.kizu` after module resolver supports std sources |
| `std::string` | `String`, `append_bytes`, `append_byte`, `clear`, `len`, `as_bytes`, `deinit` | owned byte storage, view borrow tracking, deinit state | build on Array/slice primitives; keep raw allocation hidden |
| `std::map` | `Map<[]const u8, V>`, `insert`, `get`, `contains`, `len`, `deinit` | owned key/value storage, key copy, copy-only value rule, boundary checks | keep hash table primitive until Kizu has arrays/slices robust enough; move wrapper and symbol-table shape to Kizu first |
| `std::testing` | `expect`, `expect_equal_i64`, `expect_equal_bool`, `expect_equal_bytes`, `fail` | Kizu wrappers in `std/src/testing.kizu` over `std::builtin::testing_*` primitives | migrated wrapper module; keep assertion formatting and `!void` error construction trusted |
| `std::fs` | `read_file`, `write_file`, `exists`, `metadata`, `create_dir`, `remove_dir`, `remove_file`, `Metadata` | host filesystem calls through explicit `Io` | keep host calls primitive; move path/type validation and error shaping to Kizu wrappers |
| `std::path` | `join`, `clean`, `basename`, `dirname`, `extension` | Kizu wrappers in `std/src/path.kizu` over `std::builtin::path_*` primitives | first migrated std module; keep Go public `std::path::*` branches out |
| `std::io` | `blocking`, `threaded`, `failing`, `write_stdout`, `write_stderr`, `read_stdin` | Kizu wrappers in `std/src/io.kizu` over `std::builtin::io_*` primitives | migrated wrapper module; keep host I/O and explicit capability construction trusted |
| `std::process` | `arg_count`, `arg`, `env`, `exit_code` | Kizu wrappers in `std/src/process.kizu` over `std::builtin::process_*` primitives | migrated wrapper module; keep host process access and bounds checks trusted |
| `std::task` | `Group`, `Queue`, `partition_mut`, `LocalBuffer`, `parallel_for`, `parallel_map` | structured task state, runtime scheduling, safety boundaries | keep scheduling primitives trusted; move high-level structured wrappers once module and borrow diagnostics are mature |
| `std::channel` | `Channel<T>`, `send`, `recv` | owned message queue and boundary checks | keep queue primitive; move typed wrapper after owned generic std sources are supported |
| `std::thread` | `scoped` | host thread boundary and join semantics | trusted primitive; expose only through Kizu safe wrapper |
| `std::sync` | `Mutex<T>` | shared mutable state primitive and copy-value restrictions | trusted primitive; safe wrapper remains Kizu-facing |
| `std::atomic` | `Atomic<T>` | atomic storage, seq_cst operations, supported type set | trusted primitive; ordering API should be designed before expanding |

## Source Layout Target

The eventual Kizu-written stdlib should live under `std/`:

```text
std/
  README.md
  kizu.toml
  src/
    builtin.kizu
    mem.kizu
    array.kizu
    string.kizu
    map.kizu
    testing.kizu
    fs.kizu
    path.kizu
    io.kizu
    process.kizu
    task.kizu
    channel.kizu
    thread.kizu
    sync.kizu
    atomic.kizu
```

The compiler still reserves the root namespace `std`. User packages cannot be
named `std`.

Do not create a Kizu compiler tree until the Go implementation is ready to be
ported. The compiler migration should happen later as a deliberate 1:1 port from
the Go packages, not as a long-lived parallel scaffold.

## Acceptance Rules For New Std APIs

New std APIs require all of the following in the same change:

- SPEC or ADR entry for signature, ownership, error, and capability rules.
- This registry updated with the current implementation boundary.
- Positive example under `examples/`.
- Negative example when the API has safety or error boundaries.
- `tests/conformance/v0_1.json` entry while that manifest remains the active
  reusable corpus.
- No hidden global allocator, hidden global runtime, implicit blocking I/O, or
  silent fallback behavior.

## Migration Gates

Before replacing a Go builtin with Kizu source, the following must be true:

- module resolver can load `std` sources deterministically
- diagnostics can report spans in both user source and std source
- conformance tests pass through the Go implementation and any future compiler path
- cache keys include std source hashes and public interface hashes
- host primitives remain explicit and small
- safe wrappers preserve the memory-safety contract in `docs/memory-safety.md`
