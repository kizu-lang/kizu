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
| `std::builtin::mem_equal_bytes` | Removed | Implemented in `std/src/mem.kizu` |
| `std::builtin::mem_starts_with` | Removed | Implemented in `std/src/mem.kizu` |
| `std::builtin::mem_trim_ascii` | Removed | Implemented in `std/src/mem.kizu` |
| `std::builtin::path_basename` | Removed | Implemented in `std/src/path.kizu` |
| `std::builtin::path_dirname` | Removed | Implemented in `std/src/path.kizu` |
| `std::builtin::path_extension` | Removed | Implemented in `std/src/path.kizu` |
| `std::builtin::testing_*` | Removed | Implemented in `std/src/testing.kizu` using `std::fmt` |
| `std::builtin::path_clean` | Removed | Implemented in `std/src/path.kizu` with explicit allocator-backed `String` output |
| `std::builtin::path_join` | Removed | Implemented in `std/src/path.kizu` with explicit allocator-backed `String` output |
| `std::builtin::fs_*` | Host primitive | Keep as explicit-Io filesystem boundary; public wrappers live in `std/src/fs.kizu` |
| `std::builtin::mem_len` | Host primitive for now | Keep as slice metadata access |
| `std::builtin::mem_byte_at` | Removed | Implemented in `std/src/mem.kizu` using checked index syntax |
| `std::builtin::mem_slice` | Removed | Implemented in `std/src/mem.kizu` using checked slice syntax |
| `std::builtin::mem_page_allocator` | Host primitive | Keep as allocator capability boundary |
| `std::builtin::box<T>`, `std::builtin::box_borrow<T>`, `std::builtin::box_borrow_mut<T>`, `std::builtin::box_deinit<T>` | Runtime primitive | Public constructor and methods live in `std/src/mem.kizu`; direct user calls are rejected |
| `std::builtin::string_*` | Removed | `std::string::String` behavior lives in `std/src/string.kizu`; storage uses the lower-level `std::array::Array<u8>` runtime boundary |
| `std::builtin::io_*` | Host primitive | Keep as explicit Io / host stream boundary |
| `std::builtin::process_arg_count`, `std::builtin::process_arg`, `std::builtin::process_env` | Host primitive | Keep as host process boundary |
| `std::builtin::process_exit_code` | Removed | Implemented in `std/src/process.kizu` as a pure value helper |
| `std::builtin::task_group`, `std::builtin::task_queue`, `std::builtin::task_partition_mut`, `std::builtin::task_local_buffer`, `std::builtin::task_parallel_for`, `std::builtin::task_parallel_map` | Host primitive | Public constructors, `parallel_for`, and `parallel_map` live in `std/src/task.kizu`; direct user calls are rejected |
| `std::builtin::channel<T>`, `std::builtin::channel_send<T>`, `std::builtin::channel_recv<T>` | Runtime primitive | Public constructor and methods live in `std/src/channel.kizu`; direct user calls are rejected |
| `std::builtin::atomic<T>`, `std::builtin::atomic_load<T>`, `std::builtin::atomic_store<T>` | Runtime primitive | Public constructor and methods live in `std/src/atomic.kizu`; direct user calls are rejected |
| `std::builtin::mutex<T>`, `std::builtin::mutex_get<T>` | Runtime primitive | Public constructor and methods live in `std/src/sync.kizu`; direct user calls are rejected |
| `std::builtin::array<T>`, `std::builtin::array_*<T>` | Runtime primitive | Public constructor and methods live in `std/src/array.kizu`; direct user calls are rejected |
| `std::builtin::map<K, V>`, `std::builtin::map_*<K, V>` | Runtime primitive | Public constructor and methods live in `std/src/map.kizu`; direct user calls are rejected |
| `std::builtin::thread_scoped<T>` | Runtime primitive | Public `std::thread::scoped<T>(io, worker, arg)` lives in `std/src/thread.kizu`; direct user calls are rejected |

`std::testing` now performs assertion checks and message construction in
`std/src/testing.kizu`. Equality diagnostics are built with `std::fmt` into an
explicit allocator-backed `std::string::String`; Go remains only the test runner
and error-union reporting boundary, not the assertion implementation.

Stateful runtime APIs such as `std::array::Array`, `std::map::Map`,
`std::task::parallel_for`, `std::task::parallel_map`, `std::channel::Channel`,
`std::thread::scoped<T>`, `std::sync::Mutex`, and `std::atomic::Atomic` still
keep Go runtime primitives where they own runtime storage, scheduler,
synchronization, and borrow-safety rules. Treat those primitives as explicit
runtime boundaries, not ordinary stdlib logic. The first wrapper split was
tracked by #360 and must not leave dual public paths behind. Remaining stateful
method wrappers are tracked by #382, and broader `std::thread::scoped`
argument forwarding is tracked by #383. The
task constructors `Group`, `Queue`, `partition_mut`, and `LocalBuffer` are now
Kizu wrappers over reserved `std::builtin::task_*` primitives. `parallel_for`
uses a `comptime Function` parameter to forward the worker name through
`std/src/task.kizu`. `parallel_map` uses an explicit `&mut Partition` parameter
to mutate partition output without moving the owner. `std::channel::Channel<T>()`,
`std::atomic::Atomic<T>(value)`, `std::sync::Mutex<T>(value)`,
`std::mem::Box<T>(allocator, value)`, `std::array::Array<T>(allocator)`,
`std::map::Map<K, V>(allocator)`, and
`std::thread::scoped<T>(io, worker, arg)` now use source-level type-argument
forwarding through Kizu std source.

## Builtin Registry

| Module | Current APIs | Current Go responsibility | Kizu migration target |
| --- | --- | --- | --- |
| `std::mem` | `page_allocator`, `Box<T>`, `borrow`, `borrow_mut`, `deinit`, `len`, `byte_at`, `equal_bytes`, `starts_with`, `slice`, `trim_ascii` | Kizu module in `std/src/mem.kizu`; allocator, Box storage, Box local borrow, Box deinit, and len use trusted primitives | keep only allocator capability, Box storage/local-borrow boundary, and slice metadata primitives trusted |
| `std::array` | `Array<T>`, `append`, `len`, `capacity`, `get`, `at`, `at_mut`, `set`, `deinit` | Kizu constructor and method wrappers over reserved `std::builtin::array_*`; Go owned storage, bounds checks, element borrow tracking, deinit state | keep allocation/storage and local element borrow primitives trusted |
| `std::string` | `String`, `append_bytes`, `append_byte`, `reserve`, `truncate`, `clear`, `len`, `capacity`, `as_bytes`, `deinit` | Kizu implementation in `std/src/string.kizu` backed by private `std::array::Array<u8>` storage | use as the explicit owned byte buffer for path construction and diagnostics; keep raw storage and mutable slices unexposed |
| `std::fmt` | `append_i64`, `append_bool`, `append_bytes_literal` | Kizu source over `String` | no hidden allocation or Go scalar formatting |
| `std::map` | `Map<[]const u8, V>`, `insert`, `get`, `contains`, `len`, `deinit` | Kizu constructor and method wrappers over reserved `std::builtin::map_*`; Go owned key/value storage, key copy, copy-only value rule, boundary checks | keep hash table primitive until Kizu has arrays/slices robust enough |
| `std::testing` | `expect`, equality helpers, `fail` | Kizu source over `std::fmt` and `String` | keep Go limited to the runner and error-union reporting boundary |
| `std::kizu::{ast,lexer,parser}` | `SourceFile`, `Ast`, `AstNode`, `AstData`, `NodeId`, `ChildRange`, `Span`, `TokenKind`, `Token`, minimal fn/block/return/call/binary parser APIs | Kizu source under `std/src/kizu/`; Go only loads nested std modules and runs normal type/ownership/interpreter checks. `NodeId` is an AST-scoped opaque wrapper over `handle<AstNode>`, and child storage uses `std::array::Array<NodeId>`. | grow into the self-host compiler frontend without adding parser builtins |
| `std::fs` | `read_file`, `write_file`, `exists`, `metadata`, `create_dir`, `remove_dir`, `remove_file`, `Metadata` | Kizu wrappers in `std/src/fs.kizu` over `std::builtin::fs_*` host filesystem primitives | migrated wrapper module; keep host filesystem calls primitive |
| `std::path` | `join`, `clean`, `basename`, `dirname`, `extension` | Kizu module in `std/src/path.kizu`; `join` and `clean` return allocator-backed `std::string::String` | keep only allocator and Array storage primitives trusted |
| `std::io` | `blocking`, `threaded`, `failing`, `write_stdout`, `write_stderr`, `read_stdin` | Kizu wrappers in `std/src/io.kizu` over `std::builtin::io_*` primitives | migrated wrapper module; keep host I/O and explicit capability construction trusted |
| `std::process` | `arg_count`, `arg`, `env`, `exit_code` | Kizu wrappers in `std/src/process.kizu`; only arg count, arg, and env use host primitives | keep host process access primitives trusted |
| `std::task` | `Group`, `Queue`, `partition_mut`, `LocalBuffer`, `parallel_for`, `parallel_map` | Kizu wrappers for task constructors, `parallel_for`, and `parallel_map`; Go scheduler, task state, data-parallel execution, and safety boundaries | keep scheduling primitives trusted; method wrappers tracked by #382 |
| `std::channel` | `Channel<T>`, `send`, `recv` | Kizu constructor and method wrappers over reserved `std::builtin::channel_*`; Go owned message queue and boundary checks | keep queue primitive trusted |
| `std::thread` | `scoped<T>` | Kizu one-argument wrapper; Go host thread boundary and join semantics | keep thread boundary primitive trusted; broader argument forwarding tracked by #383 |
| `std::sync` | `Mutex<T>` | Kizu constructor and `get` wrapper over reserved `std::builtin::mutex_get`; Go shared mutable state primitive and copy-value restrictions | keep mutex storage primitive trusted |
| `std::atomic` | `Atomic<T>` | Kizu constructor plus `load`/`store` wrappers over reserved `std::builtin::atomic_*`; Go atomic storage, seq_cst operations, supported type set | keep atomic storage primitive trusted; ordering API remains future work |

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
    kizu/
      ast.kizu
      lexer.kizu
      parser.kizu
```

The compiler still reserves the root namespace `std`. User packages cannot be
named `std`.

Do not create a separate Kizu compiler tree until the Go implementation is ready
to be ported. `std::kizu::*` is the narrow exception for reusable frontend
library components that examples and conformance can execute today.

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

## Byte Storage Boundary

`std::string::String` is the current Kizu-facing owned byte buffer. ADR 0057
moves its behavior into Kizu source over private `std::array::Array<u8>`
storage. The remaining trusted boundary is Array storage, not string-specific
Go logic. ADR 0056 still rejects a public `std::mem::OwnedBytes` type until
mutable slice, raw storage provenance, and generic container rules are
specified.

The Kizu `String` implementation uses std-only Array storage helpers for
reserve, truncate, clear, and byte-slice exposure. Those helpers are not public
`std::array` API in v0.2.

The migration path is:

- keep `String` public behavior in Kizu source over private Array storage
- keep `std::builtin::string_*` removed so user code cannot bypass wrapper
  borrow and move checks
- add regression coverage for each storage safety rule before expanding the
  primitive set
- use `String` for diagnostic and byte-building needs in the self-host frontend
- revisit a public byte storage type only when it can preserve allocator,
  cleanup, and view-borrow safety without raw pointer exposure
