# Minimum Selfhost Runtime ABI

This document defines `selfhost-abi-v0`, the runtime ABI contract required by
#455 for the first selfhost compiler artifact. It covers only the shapes listed
by the #453 IR manifest:

```text
kizu-ir-shape-v0
manifest-version selfhost-abi-v0
node-kind function
node-kind block
node-kind diagnostic
value-type i64
value-type bool
value-type []const u8
value-type handle
storage local
storage owned-container
storage borrowed-view
storage arena
storage handle
call direct
call std-primitive
cleanup deinit
host-capabilities selfhost-host-v0
external std::mem::page_allocator
external std::fs::exists
external std::fs::metadata
external std::fs::read_dir
external std::fs::read_file
external std::fs::write_file
external std::fs::create_dir
external std::io::blocking
external std::io::write_stdout
external std::io::write_stderr
external std::process::arg_count
external std::process::arg
external std::process::env
external std::process::exit_code
manifest-complete selfhost-abi-v0
```

Broader ABI work is tracked by #495. #454, #456, and #457 must treat any shape
outside this table as unsupported unless a later issue extends this contract.

## Version

ABI version: `selfhost-abi-v0`.

Artifact metadata emitted for this bootstrap line must include this version. A
different ABI version is a cache and stage-comparison input.

## Symbols

Kizu module and function symbols lower to deterministic LLVM symbol names:

| Kizu shape | LLVM symbol |
| --- | --- |
| package entry `selfhost::smoke` | `@kizu_selfhost__smoke` |
| hosted compiler CLI entry `selfhost::cli_main` | `@kizu_selfhost__cli_main` |
| module function `selfhost::m::f` | `@kizu_selfhost_m__f` |
| allocator capability | `@kizu_rt_mem_page_allocator` |
| std primitive `std::fs::exists` | `@kizu_rt_fs_exists` |
| std primitive `std::fs::metadata` | `@kizu_rt_fs_metadata` |
| std primitive `std::fs::read_dir` | `@kizu_rt_fs_read_dir` |
| std primitive `std::fs::read_file` | `@kizu_rt_fs_read_file` |
| std primitive `std::fs::write_file` | `@kizu_rt_fs_write_file` |
| std primitive `std::fs::create_dir` | `@kizu_rt_fs_create_dir` |
| artifact publish rename boundary | `@kizu_rt_fs_rename` |
| std primitive `std::io::blocking` | `@kizu_rt_io_blocking` |
| std primitive `std::io::write_stdout` | `@kizu_rt_io_write_stdout` |
| std primitive `std::io::write_stderr` | `@kizu_rt_io_write_stderr` |
| std primitive `std::process::arg_count` | `@kizu_rt_process_arg_count` |
| std primitive `std::process::arg` | `@kizu_rt_process_arg` |
| std primitive `std::process::env` | `@kizu_rt_process_env` |
| std primitive `std::process::exit_code` | `@kizu_rt_process_exit_code` |
| process exit boundary | `@kizu_rt_process_exit` |
| trap boundary | `@kizu_rt_trap` |

Name lowering replaces `::` module separators with `_` inside the module path
and uses `__` before the function name. User and std symbols must never be
resolved by host linker defaults.

## Value Layout

| IR value type | LLVM layout | Call/return convention | Cleanup |
| --- | --- | --- | --- |
| `void` | `void` | returned as `void` | none |
| `bool` | `i1` in SSA, zero-extended to `i8` only at runtime ABI boundaries | direct | none |
| `i64` | `i64` | direct | none |
| `u8` | `i8` | direct | none |
| `[]const u8` | `%kizu.slice.u8 = type { ptr, i64 }` | passed and returned by value | borrowed; no cleanup |
| `handle<T>` | `%kizu.handle = type { ptr, i64 }` | passed and returned by value | copyable opaque ID |

The slice pointer is read-only for safe Kizu. Length is signed `i64` because
the current language surface uses `i64` for lengths and indexes. A slice with
length `0` may use a null pointer only when no dereference occurs.

## Storage

| IR storage shape | Layout | Ownership rule | Cleanup |
| --- | --- | --- | --- |
| `local` | SSA value or stack slot chosen by backend | valid inside the current function | none unless it owns a container |
| `owned-container` | opaque runtime handle `%kizu.owned = type { ptr }` for #453 | owner must call its deinit hook exactly once | `deinit` hook |
| `borrowed-view` | same value layout as the viewed type, with no ownership bit | cannot outlive the owner | none |
| `arena` | opaque runtime handle `%kizu.owned = type { ptr }` | owns values inserted through `arena.add` | `arena.deinit` |
| `handle` | `%kizu.handle = type { ptr, i64 }` | tied to the arena pointer that produced it | none |

The first artifact may keep owned containers opaque. #456 and #519 own reachable
runtime storage behind those handles. #454 must not guess array, string, map, or
arena internals.

## Records And Diagnostics

Selfhost records reachable from the #453 corpus lower to named LLVM struct
types:

```text
%kizu.struct.<module>.<Type> = type { <fields in source order> }
```

Fields use the value layout table recursively. Enum tags lower to `i64`.
Tagged-union payload storage is deferred to #495 unless #454 encounters a
reachable union payload; in that case the payload shape is a blocker issue, not
a silent fallback.

Diagnostics use the same record rule. Diagnostic message text is `[]const u8`.
The renderer is outside this ABI; only the record layout and recoverable error
boundary are specified here.

## Error And Trap Representation

Recoverable `!T` values use:

```text
%kizu.error.T = type { i1, T, %kizu.slice.u8 }
```

Field order is:

1. `ok`: `true` for success, `false` for failure.
2. `value`: valid only when `ok` is true.
3. `message`: valid only when `ok` is false.

For `!void`, the value field is omitted:

```text
%kizu.error.void = type { i1, %kizu.slice.u8 }
```

Unrecoverable compiler/runtime invariant failures call:

```text
declare void @kizu_rt_trap(%kizu.slice.u8) noreturn
```

#454 may emit trap calls for unsupported shapes, but only after the missing
shape is linked to a follow-up issue.

## Calls

| IR call form | Lowering |
| --- | --- |
| `direct` | direct LLVM `call` to the lowered Kizu symbol |
| `std-primitive` | direct LLVM `call` to the runtime symbol in the external table |

Borrowed arguments are passed by value using their ABI layout. Owned-container
arguments transfer or borrow according to the checked ownership handoff; #454
must reject any call whose ownership mode is absent from the handoff.

## Cleanup

The #453 manifest contains only `deinit` cleanup. Cleanup calls are explicit:

| Owned shape | Runtime symbol |
| --- | --- |
| opaque owned container | `@kizu_rt_owned_deinit` |
| owned string handle | `@kizu_rt_string_deinit` |
| array handle | `@kizu_rt_array_deinit` |
| map handle | `@kizu_rt_map_deinit` |

#454 may declare these symbols without defining them. #456 owns the storage
implementation. A value that needs cleanup but lacks a listed hook is unsupported.

## Reachable Runtime Storage

#456 keeps public `std::array::Array`, `std::string::String`, and
`std::map::Map` APIs in Kizu source. The bootstrap storage implementation is an
opaque runtime template at `selfhost/runtime/selfhost.storage.ll`, with static
metadata contract lines in `selfhost/runtime/selfhost.storage.ll.meta.tail`.
The Kizu backend copies them to artifacts linked next to
`target/selfhost/selfhost.ll`:

```text
target/selfhost/selfhost.storage.ll
target/selfhost/selfhost.storage.ll.meta
```

The public ABI still exposes owned containers only as `%kizu.owned`. Runtime
storage internals are not available to safe Kizu code. The minimum reachable
selfhost storage symbols are:

| Shape | Runtime symbol | Purpose |
| --- | --- | --- |
| Array construction | `@kizu_rt_array_new` | token lists and AST child lists |
| Array append | `@kizu_rt_array_append` | copies one lowered element into storage |
| Array length | `@kizu_rt_array_len` | returns the checked element count |
| Array borrowed view | `@kizu_rt_array_at` | returns a local read-only element view |
| Array cleanup | `@kizu_rt_array_deinit` | releases owned array storage |
| String construction | `@kizu_rt_string_new` | diagnostic and path buffers |
| String append bytes | `@kizu_rt_string_append_bytes` | copies borrowed bytes |
| String append byte | `@kizu_rt_string_append_byte` | appends one byte |
| String length | `@kizu_rt_string_len` | returns byte length |
| String borrowed view | `@kizu_rt_string_as_bytes` | returns a local read-only byte view |
| String cleanup | `@kizu_rt_string_deinit` | releases owned string storage |
| Map construction | `@kizu_rt_map_new` | resolver, type, and ownership tables |
| Map insert | `@kizu_rt_map_insert` | copies `[]const u8` key and copy value |
| Map contains | `@kizu_rt_map_contains` | checks key presence |
| Map `i64` get | `@kizu_rt_map_get_i64` | returns copy payloads used by symbol tables |
| Map cleanup | `@kizu_rt_map_deinit` | releases owned map storage |
| Diagnostic buffer construction | `@kizu_rt_diagnostic_buffer_new` | compiler failure storage |
| Diagnostic push | `@kizu_rt_diagnostic_push` | copies diagnostic message text |
| Diagnostic cleanup | `@kizu_rt_diagnostic_buffer_deinit` | releases diagnostic storage |
| Arena construction | `@kizu_rt_arena_new` | `std::kizu::ast::Ast` node arena |
| Arena add | `@kizu_rt_arena_add` | appends one lowered AST node and returns a handle |
| Arena get | `@kizu_rt_arena_get` | checks handle provenance and returns a borrowed node view |
| Arena cleanup | `@kizu_rt_arena_deinit` | releases arena storage at `Ast.deinit()` |

Construction takes an explicit allocator capability represented as `%kizu.owned`
and allocates through runtime-internal `@kizu_rt_alloc(ptr, i64)`. Cleanup calls
`@kizu_rt_free(ptr, ptr)` using the allocator pointer stored inside the opaque
runtime object. #457 owns binding allocator capability creation to host
facilities; #456 keeps the storage calls capability-shaped and non-Go.

Append operations return `!void` using `%kizu.error.void`. The runtime artifact
metadata must include `allocator-boundary explicit`, `go-stdprim-storage none`,
and `interpreter-storage none`; Go-backed interpreter storage is allowed only
for stage0/oracle execution, not for this artifact path.

For #519, the first concrete arena/handle call site is the
`std::kizu::ast::Ast` node arena. Handles use `%kizu.handle = type { ptr, i64 }`:
the first field is the producing arena runtime pointer and the second field is
the zero-based slot index. `@kizu_rt_arena_get` rejects mismatched or out-of-range
handles with the diagnostic `invalid arena handle`; valid gets return a local
borrowed view. The storage artifact metadata records `reachable arena
ast-node-storage`, `reachable handle ast-node-id`, `arena-allocator-boundary
explicit`, `arena-handle-provenance checked`, and
`arena-invalid-handle-diagnostic invalid arena handle`.

ADR-0062 fixes the selfhost AST storage constraints for this ABI slice. This is
not the final general-purpose `arena<T>` payload policy for all Kizu programs;
future arena payload expansion remains allowed when explicit cleanup,
allocator, borrow, and checker rules are specified.
`AstNode` arena payloads may contain scalar copy values, spans, token/symbol
ids, child ranges, `NodeId`, borrowed source views tied to the owning `Ast`, and
payload records that recursively obey the same rule. They must not contain
owned containers, allocator or I/O capabilities, arbitrary arenas or handles,
concurrency capabilities, or raw pointers. Variable-length AST relationships
must use `ChildRange` into the AST-owned child array.

Static checking owns known safe-side lifetime failures: cross-`Ast` `NodeId`
use, use after `Ast.deinit()`, `NodeId` outliving the owning `Ast`, raw-pointer
escape, and storing borrowed views returned by `Array.at`, `String.as_bytes`, or
`arena.get`. Runtime `@kizu_rt_arena_get` diagnostics are a backstop for unknown
provenance or corrupted handles, not a replacement for static checking.

ADR-0063 stabilizes `std::mem::page_allocator() -> Allocator` as the hosted
selfhost allocator factory. `Allocator` is a visible copyable capability.
Passing it to an owned container constructor reads the capability and the
created runtime object stores the allocator pointer needed for its own `deinit`;
the allocator itself is not moved. Allocating operations that can fail return
`!T` or `!void`. No hosted selfhost path may add a hidden default allocator or
implicit global allocator.

Box storage remains deferred to #496 unless a later selfhost IR artifact lists a
concrete reachable call site.

## External Primitives

| Kizu primitive | Runtime symbol | Signature |
| --- | --- | --- |
| `std::mem::page_allocator` | `@kizu_rt_mem_page_allocator` | `() -> %kizu.owned` |
| `std::io::blocking` | `@kizu_rt_io_blocking` | `() -> %kizu.owned` |
| `std::fs::exists` | `@kizu_rt_fs_exists` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.bool` |
| `std::fs::metadata` | `@kizu_rt_fs_metadata` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.metadata` |
| `std::fs::read_dir` | `@kizu_rt_fs_read_dir` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.owned` |
| `std::fs::read_file` | `@kizu_rt_fs_read_file` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.slice.u8` |
| `std::fs::write_file` | `@kizu_rt_fs_write_file` | `(%kizu.owned, %kizu.slice.u8, %kizu.slice.u8) -> %kizu.error.void` |
| `std::fs::create_dir` | `@kizu_rt_fs_create_dir` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.void` |
| `std::io::write_stdout` | `@kizu_rt_io_write_stdout` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.void` |
| `std::io::write_stderr` | `@kizu_rt_io_write_stderr` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.void` |
| `std::process::arg_count` | `@kizu_rt_process_arg_count` | `() -> i64` |
| `std::process::arg` | `@kizu_rt_process_arg` | `(i64) -> %kizu.error.slice.u8` |
| `std::process::env` | `@kizu_rt_process_env` | `(%kizu.slice.u8) -> %kizu.error.slice.u8` |
| `std::process::exit_code` | `@kizu_rt_process_exit_code` | `(i64) -> i64` |
| process termination | `@kizu_rt_process_exit` | `(i64) -> noreturn` |

The first argument is the explicit `Io` capability. The runtime must not use a
hidden global default capability.

## Host Capability Binding

#457 binds the host-facing side of the ABI through
`selfhost/runtime/selfhost.host.ll`, copied by the Kizu backend to:

```text
target/selfhost/selfhost.host.ll
target/selfhost/selfhost.host.ll.meta
```

The host artifact defines the `@kizu_rt_*` symbols consumed by the selfhost
compiler artifact and delegates to lower-level `@kizu_host_*` imports. The
hosted implementation for those imports lives in
`selfhost/runtime/selfhost.hosted.c`. Those imports are the only allowed OS
boundary for the first bootstrap path. Metadata must include `go-stdprim-host
none`, `interpreter-host none`, and explicit allocator, filesystem, process,
stdout, stderr, and exit boundaries.

The current bootstrap comparison is artifact-only before external linking, so
process spawn/wait is deferred to #459 unless that issue chooses a hosted linker
execution path. `@kizu_rt_fs_rename` is included as the deterministic artifact
publish boundary without adding a public `std::fs::rename` wrapper yet;
additional filesystem calls require a concrete selfhost call site and a linked
roadmap issue.

For #458, `selfhost.ll` also exposes `@kizu_selfhost__cli_main` as the minimum
hosted compiler CLI entry. A host launcher initializes process arguments with
`kizu_host_init(argc, argv)`; the hosted runtime presents Kizu process args
without the executable name, matching `std::process::arg(0)` in the interpreter.
The hosted smoke links `selfhost.ll`, `selfhost.host.ll`, and
`selfhost/runtime/selfhost.hosted.c`, then runs:

```sh
selfhost check selfhost
selfhost stage selfhost
```

The check path reads `selfhost/kizu.toml`, reads `selfhost/src`, writes
`check: ok` to stdout, and returns exit code `0` through the runtime process
boundary. Unsupported commands write a deterministic stderr diagnostic and
return exit code `64`. This is a runnable no-Go artifact smoke; broader stage
comparison remains #459.

For #459, the hosted `stage selfhost` path also materializes the supported
stage2 artifact set by reading the current `target/selfhost/selfhost.*` LLVM
artifacts and writing the corresponding files under `target/selfhost/stage2/`
through the explicit filesystem runtime boundary. This is the supported
bootstrap subset until later issues replace the artifact materialization path
with a broader selfhost backend.

For #460, the hosted CLI also accepts the manifest-selected corpus checks:

```sh
selfhost check examples/hello.kizu
selfhost check examples/negative/moved_value.kizu
```

These targets are bounded to checked manifests. The supported corpus manifest in
`selfhost/tests/supported-corpus.tsv` verifies user-visible stdout, stderr, and
exit-code behavior through the hosted artifact. Broad CLI parity, broad
example/conformance coverage, and unsupported ABI shapes remain blocked by #497
and #495.

For #530, those same bounded `check <file>` targets are also recorded in
`selfhost/tests/cli/check-parity.tsv` with checked-in stdout/stderr goldens:

```sh
selfhost check examples/hello.kizu
selfhost check examples/negative/moved_value.kizu
```

The check parity gate runs through `target/selfhost/stage2/selfhost`, records
`go.cmd-kizu-fallback none`, and does not bootstrap from scratch by default.

For #525, the hosted CLI also accepts the bounded `parse <file>` parity cases in
`selfhost/tests/cli/parse-parity.tsv`:

```sh
selfhost parse selfhost/tests/cli/parse_ok_minimal.kizu
selfhost parse selfhost/tests/cli/parse_invalid_missing_expr.kizu
```

The parse parity gate compares byte-for-byte stdout, stderr, and exit codes
against checked-in goldens and runs through `target/selfhost/stage2/selfhost`.
It does not invoke Go `cmd/kizu` as a fallback. General `parse <file>` parity
outside those fixture paths remains under #497. The current CLI parity support
and deferrals are recorded in `docs/selfhost-cli-parity.md`.

## Textual LLVM Validation

Until CI requires an LLVM verifier binary, #454 uses this repository command as
the documented textual-IR validation gate:

```sh
go test ./cmd/kizu -run TestSelfhostBackendArtifactGate
```

The gate checks that `target/selfhost/selfhost.ll`:

- starts with the `; kizu selfhost bootstrap ll v0` marker
- records `target/selfhost/selfhost.ir` as `source_filename`
- defines `%kizu.slice.u8`, `%kizu.owned`, `%kizu.handle`,
  `%kizu.error.slice.u8`, and
  `%kizu.error.void`
- declares all unresolved runtime symbols used by the bootstrap artifact:
  `@kizu_rt_mem_page_allocator`, `@kizu_rt_io_blocking`,
  `@kizu_rt_fs_read_file`, `@kizu_rt_fs_write_file`,
  `@kizu_rt_fs_read_dir`, stdout/stderr, process, exit,
  `@kizu_rt_owned_deinit`, and `@kizu_rt_trap`
- defines the hosted compiler CLI entry `@kizu_selfhost__cli_main`
- defines the stable bootstrap entry symbol `@kizu_selfhost__smoke`

The same gate checks that `target/selfhost/selfhost.ll.meta` records
`selfhost-abi-v0`, the source IR path, the shape manifest path, the output path,
the validation command, each unresolved external, and the blocker policy for
unsupported shapes. This metadata is a stable input for the #459 stage
comparison.

For #456 the Kizu backend performs cheap header validation before copying the
runtime storage template. The same Go gate checks
`target/selfhost/selfhost.storage.ll` and `target/selfhost/selfhost.storage.ll.meta`.
The storage validation requires the reachable Array, String, Map, diagnostic,
Arena, and Handle runtime symbols, the `@kizu_selfhost__runtime_storage_smoke`
entry, explicit allocator-boundary metadata, handle provenance metadata, and the
absence of Go interpreter/stdprim fallback markers in the storage LLVM artifact.
The gate also links `selfhost.storage.ll` with the host capability runtime and a
tiny C harness, then runs the storage smoke so Arena/Handle calls cannot be only
dead textual declarations.

For #457 the same Go gate checks `target/selfhost/selfhost.host.ll` and
`target/selfhost/selfhost.host.ll.meta`. It validates host capability wrapper
symbols, the `@kizu_selfhost__host_capability_smoke` entry, explicit host
boundary metadata, and the absence of Go interpreter/stdprim fallback markers
for host access. The gate also links `selfhost.host.ll` with
`selfhost/runtime/selfhost.hosted.c` and a tiny C harness, then runs the smoke
from the repository root. The smoke reads `selfhost/kizu.toml`, reads
`selfhost/src`, writes `target/selfhost/host-smoke.status`, writes stdout
and stderr, reads process args/env, and returns an exit code through the hosted
runtime boundary.

For #458 the gate also links the generated compiler artifact itself with the
host capability runtime and runs `@kizu_selfhost__cli_main` for the supported CLI
contract. That validation proves the `check selfhost` user-visible path is
reachable from the Kizu-built LLVM artifact without Go `cmd/kizu` dispatch.

## Unsupported Shapes Tracked By #495

The following are intentionally outside `selfhost-abi-v0`:

- raw pointers and nullable pointers
- floats and integer widths other than `i64` and `u8`
- mutable slices
- public array, string, map, arena, and handle storage layout beyond the listed
  opaque runtime representations
- tagged-union payload ABI beyond blocker-specific additions
- task, thread, channel, mutex, and atomic runtime ABI
- C ABI interop and native object/linker metadata beyond textual LLVM emission
- async/await or hidden runtime scheduling

If #454, #456, or #457 reaches one of these shapes in the selfhost artifact,
the dependent issue must either extend this document or open a concrete blocker
linked to #495.
