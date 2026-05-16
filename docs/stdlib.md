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
- `tests/conformance`: reusable behavior corpus for the future self-host compiler.
- `selfhost/STDLIB.md`: std APIs currently exercised by the self-host skeleton.

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

## Builtin Registry

| Module | Current APIs | Current Go responsibility | Kizu migration target |
| --- | --- | --- | --- |
| `std::mem` | `page_allocator`, `len`, `byte_at`, `equal_bytes`, `starts_with`, `slice`, `trim_ascii` | type/ownership/runtime builtin checks and byte operations | keep `page_allocator` as primitive; move pure byte helpers to `std/mem.kizu` once slice/view semantics are stable |
| `std::array` | `Array<T>`, `append`, `len`, `capacity`, `get`, `at`, `at_mut`, `set`, `deinit` | owned storage, bounds checks, element borrow tracking, deinit state | keep allocation/storage primitives trusted; move ergonomic wrappers and tests to `std/array.kizu` after module resolver supports std sources |
| `std::string` | `String`, `append_bytes`, `append_byte`, `clear`, `len`, `as_bytes`, `deinit` | owned byte storage, view borrow tracking, deinit state | build on Array/slice primitives; keep raw allocation hidden |
| `std::map` | `Map<[]const u8, V>`, `insert`, `get`, `contains`, `len`, `deinit` | owned key/value storage, key copy, copy-only value rule, boundary checks | keep hash table primitive until Kizu has arrays/slices robust enough; move wrapper and symbol-table shape to Kizu first |
| `std::testing` | `expect`, `expect_equal_i64`, `expect_equal_bool`, `expect_equal_bytes`, `fail` | assertion failure formatting and `!void` error behavior | move most helpers to Kizu after `std::string` and diagnostics helpers are available |
| `std::fs` | `read_file`, `write_file`, `exists`, `metadata`, `create_dir`, `remove_dir`, `remove_file`, `Metadata` | host filesystem calls through explicit `Io` | keep host calls primitive; move path/type validation and error shaping to Kizu wrappers |
| `std::path` | `join`, `clean`, `basename`, `dirname`, `extension` | pure path string operations | early Kizu migration candidate after `std::string`/byte helpers are usable |
| `std::io` | `blocking`, `threaded`, `failing`, `write_stdout`, `write_stderr`, `read_stdin` | host I/O and explicit Io capability construction | keep host I/O primitive; Kizu wrappers select capabilities and shape errors |
| `std::process` | `arg_count`, `arg`, `env`, `exit_code` | host process access and bounds checks | keep host reads primitive; move validation wrappers to Kizu |
| `std::task` | `Group`, `Queue`, `partition_mut`, `LocalBuffer`, `parallel_for`, `parallel_map` | structured task state, runtime scheduling, safety boundaries | keep scheduling primitives trusted; move high-level structured wrappers once module and borrow diagnostics are mature |
| `std::channel` | `Channel<T>`, `send`, `recv` | owned message queue and boundary checks | keep queue primitive; move typed wrapper after owned generic std sources are supported |
| `std::thread` | `scoped` | host thread boundary and join semantics | trusted primitive; expose only through Kizu safe wrapper |
| `std::sync` | `Mutex<T>` | shared mutable state primitive and copy-value restrictions | trusted primitive; safe wrapper remains Kizu-facing |
| `std::atomic` | `Atomic<T>` | atomic storage, seq_cst operations, supported type set | trusted primitive; ordering API should be designed before expanding |

## Lexer Dependency Gate

The self-host lexer may depend on this v0.2 stdlib surface before it is ported
from Go to `selfhost/src/lexer.kizu`:

- source buffers are `[]const u8`
- string literals remain `[]const u8`, not primitive `string`
- byte scanning uses `std::mem::len`, `std::mem::byte_at`,
  `std::mem::equal_bytes`, `std::mem::starts_with`, `std::mem::slice`, and
  `std::mem::trim_ascii`
- token streams use `std::array::Array<token::Token>` with an explicit
  `std::mem::page_allocator()` allocator
- recoverable lexer failures use `!T`
- not-found lookups use `?T`
- token stream cleanup uses explicit `deinit`
- `Array<Token>` must keep the existing borrow and cleanup rules: append, set,
  and deinit are rejected while an element borrow is live; deinit moves the
  array; `get` remains copy-only; non-copy token values are read through
  `at` / `at_mut`

The conformance suite already includes negative cases for Array use after
`deinit`, append/set/deinit while borrowed, read while mutably borrowed,
copy-only `get`, non-copy moves, and rejected raw pointer / handle / nested
array / map / concurrency capability element types. New lexer work must reuse
those cases instead of weakening Array rules for compiler convenience.

## Source Layout

The Kizu-written stdlib skeleton lives under `std/`:

```text
std/
  kizu.toml
  README.md
  src/
    mod.kizu
    mem.kizu
    path.kizu
    io.kizu
    process.kizu
    testing.kizu
```

The compiler still reserves the root namespace `std`. User packages cannot be
named `std`. The compiler may load `std/kizu.toml` through the explicit
compiler-owned std path; normal user manifest parsing still rejects the package
name.

The v0.3 std sources are declaration skeletons. They define the public wrapper
surface and primitive ABI boundary, while runtime behavior remains in the
current Go builtins. `std::mem` and `std::path` are the first pure-helper
migration candidates because they do not need hidden runtime state.

Current skeleton modules:

- `std::mem`: allocator marker plus byte/slice helper primitive boundary
- `std::path`: pure path helper primitive boundary
- `std::io`: explicit `Io` capability constructors and host I/O boundary
- `std::process`: explicit argv/env/exit boundary
- `std::testing`: assertion helper boundary

The skeleton is intentionally checkable:

```sh
go run ./cmd/kizu check std
```

## Acceptance Rules For New Std APIs

New std APIs require all of the following in the same change:

- SPEC or ADR entry for signature, ownership, error, and capability rules.
- This registry updated with the current implementation boundary.
- Positive example under `examples/`.
- Negative example when the API has safety or error boundaries.
- `tests/conformance/v0_1.json` entry while that manifest remains the active
  reusable corpus.
- Self-host usage note in `selfhost/STDLIB.md` if the API exists to support the
  compiler.
- No hidden global allocator, hidden global runtime, implicit blocking I/O, or
  silent fallback behavior.

## Migration Gates

Before replacing a Go builtin with Kizu source, the following must be true:

- module resolver can load `std` sources deterministically
- diagnostics can report spans in both user source and std source
- conformance tests pass through both Go and self-host paths
- cache keys include std source hashes and public interface hashes
- host primitives remain explicit and small
- safe wrappers preserve the memory-safety contract in `docs/memory-safety.md`
