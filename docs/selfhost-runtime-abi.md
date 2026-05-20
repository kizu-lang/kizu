# Minimum Selfhost Runtime ABI

This document defines `selfhost-abi-v0`, the runtime ABI contract required by
#455 for the first selfhost compiler artifact. It covers only the shapes listed
by the #453 IR manifest:

```text
kizu-ir-shape-v0
node-kind function
node-kind block
node-kind diagnostic
value-type i64
value-type bool
value-type []const u8
storage local
storage owned-container
storage borrowed-view
call direct
call std-primitive
cleanup deinit
external std::fs::read_file
external std::fs::write_file
external std::io::blocking
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
| module function `selfhost::m::f` | `@kizu_selfhost_m__f` |
| std primitive `std::fs::read_file` | `@kizu_rt_fs_read_file` |
| std primitive `std::fs::write_file` | `@kizu_rt_fs_write_file` |
| std primitive `std::io::blocking` | `@kizu_rt_io_blocking` |
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

The slice pointer is read-only for safe Kizu. Length is signed `i64` because
the current language surface uses `i64` for lengths and indexes. A slice with
length `0` may use a null pointer only when no dereference occurs.

## Storage

| IR storage shape | Layout | Ownership rule | Cleanup |
| --- | --- | --- | --- |
| `local` | SSA value or stack slot chosen by backend | valid inside the current function | none unless it owns a container |
| `owned-container` | opaque runtime handle `%kizu.owned = type { ptr }` for #453 | owner must call its deinit hook exactly once | `deinit` hook |
| `borrowed-view` | same value layout as the viewed type, with no ownership bit | cannot outlive the owner | none |

The first artifact may keep owned containers opaque. #456 owns reachable runtime
storage behind those handles. #454 must not guess array, string, or map internals.

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

## External Primitives

| Kizu primitive | Runtime symbol | Signature |
| --- | --- | --- |
| `std::io::blocking` | `@kizu_rt_io_blocking` | `() -> %kizu.owned` |
| `std::fs::read_file` | `@kizu_rt_fs_read_file` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.slice.u8` |
| `std::fs::write_file` | `@kizu_rt_fs_write_file` | `(%kizu.owned, %kizu.slice.u8, %kizu.slice.u8) -> %kizu.error.void` |

The first argument is the explicit `Io` capability. The runtime must not use a
hidden global default capability.

## Textual LLVM Validation

Until CI requires an LLVM verifier binary, #454 uses this repository command as
the documented textual-IR validation gate:

```sh
go test ./cmd/kizu -run TestSelfhostBackendArtifactGate
```

The gate checks that `target/selfhost/selfhost.ll`:

- starts with the `; kizu selfhost bootstrap ll v0` marker
- records `target/selfhost/selfhost.ir` as `source_filename`
- defines `%kizu.slice.u8`, `%kizu.owned`, `%kizu.error.slice.u8`, and
  `%kizu.error.void`
- declares all unresolved runtime symbols used by the bootstrap artifact:
  `@kizu_rt_io_blocking`, `@kizu_rt_fs_read_file`,
  `@kizu_rt_fs_write_file`, `@kizu_rt_owned_deinit`, and `@kizu_rt_trap`
- defines the stable bootstrap entry symbol `@kizu_selfhost__smoke`

The same gate checks that `target/selfhost/selfhost.ll.meta` records
`selfhost-abi-v0`, the source IR path, the shape manifest path, the output path,
the validation command, each unresolved external, and the blocker policy for
unsupported shapes. This metadata is a stable input for the #459 stage
comparison.

## Unsupported Shapes Tracked By #495

The following are intentionally outside `selfhost-abi-v0`:

- raw pointers and nullable pointers
- floats and integer widths other than `i64` and `u8`
- mutable slices
- full array, string, and map storage layout
- tagged-union payload ABI beyond blocker-specific additions
- task, thread, channel, mutex, and atomic runtime ABI
- C ABI interop and native object/linker metadata beyond textual LLVM emission
- async/await or hidden runtime scheduling

If #454, #456, or #457 reaches one of these shapes in the selfhost artifact,
the dependent issue must either extend this document or open a concrete blocker
linked to #495.
