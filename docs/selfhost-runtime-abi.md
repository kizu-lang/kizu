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
value-type []u8
value-type record
value-type error-union
value-type handle
storage local
storage owned-container
storage borrowed-view
storage arena
storage handle
call direct
call direct-record-roundtrip
call std-primitive
cleanup deinit
host-capabilities selfhost-host-v0
external std::mem::page_allocator
external std::fs::exists
external std::fs::metadata
external std::fs::read_dir
external std::fs::read_file
external std::fs::write_file
external std::fs::rename
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
| std primitive `std::fs::rename` | `@kizu_rt_fs_rename` |
| std primitive `std::fs::create_dir` | `@kizu_rt_fs_create_dir` |
| std primitive `std::fs::remove_dir` | `@kizu_rt_fs_remove_dir` |
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
| `[]u8` | `%kizu.slice.u8 = type { ptr, i64 }` | passed and returned by value | borrowed; no cleanup |
| selfhost record | `%kizu.record.<name> = type { <fields> }` | passed and returned by value | field-dependent |
| `!T` | `%kizu.error.T = type { i1, T, %kizu.slice.u8 }` | passed and returned by value | field-dependent |
| `std::arena::Handle<T>` | `%kizu.handle = type { ptr, i64 }` | passed and returned by value | copyable opaque ID |

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
Tagged-union payload storage uses the minimal selfhost ABI in
[Tagged-Union Payload Layout](#tagged-union-payload-layout). Shapes outside that
section are deferred to #495; a reachable unsupported shape is a blocker issue,
not a silent fallback.

Diagnostics use the same record rule. Diagnostic message text is `[]u8`.
The renderer is outside this ABI; only the record layout and recoverable error
boundary are specified here.

For #578, the hosted artifact exercises the first concrete record ABI shape:
`%kizu.record.abi.summary = type { i64, %kizu.slice.u8 }`. Direct calls return
that record by value, and `%kizu.error.record.abi.summary` covers both success
and failure return paths. Tagged-union payload ABI follows
[Tagged-Union Payload Layout](#tagged-union-payload-layout) and must not be
silently lowered through a Go fallback.

## Tagged-Union Payload Layout

This section specifies the minimal selfhost/v0.2 tagged-union payload ABI
(`selfhost-abi-v0`) needed by selfhost compiler data structures such as the MIR
sum types in `selfhost/src/backend/compiled_mir.kizu`. It is intentionally not a
general-purpose union ABI for every Kizu type (#495 tracks broader work). It
composes with the owner aggregate cleanup contract in ADR-0075.

### Representation

A `union` value lowers to `tag + inline payload storage`:

```text
%kizu.union.<module>.<Type> = type { i64, [<N> x i8] }
```

- The first field is the active tag, an `i64`, in variant declaration order
  starting at `0`. It mirrors how enum tags lower to `i64`.
- The second field is inline payload storage: a fixed byte array sized and
  aligned for the largest variant payload. Payload size and alignment are
  compile-time known for every supported shape.
- A variant payload is stored in that inline storage using the value layout
  table recursively (records, slices, scalars). The backend reads it back at the
  declared payload type for the active variant.
- There is no hidden heap allocation in the ABI. A union value is self-contained
  by value, like records, and is passed and returned by value.
- A payload-free variant (for example a `union` arm written as a bare tag) uses
  tag `i64` only; the inline storage is unused for that variant.

The tag-plus-all-fields mega record with sentinel values is not the target
representation. Backends must not encode a union as a struct that materializes
every variant's fields simultaneously.

### Active Variant And Inactive Storage

- The active tag alone determines which payload is initialized.
- Only the active variant's payload bytes are initialized and readable. Inactive
  payload bytes have no semantic value; safe code must not read them, and a
  `match` only exposes the active variant's binding.
- Reading a payload at a variant that is not the active tag is undefined for safe
  Kizu and must be impossible by representation (`match` dispatch on the tag),
  not by sentinel assumptions about field contents.

### Ownership And Cleanup

The cleanup rule composes with ADR-0075 owner aggregates:

- A union whose every variant payload is copy/scalar (or payload-free) is a copy
  value and needs no `deinit`.
- A union with at least one owner payload (a payload that contains an owned
  container such as `std::array::Array<T>`, `std::string::String`, a nested
  owner aggregate, etc.) is itself an **owner aggregate**: it is move-only,
  read-only APIs take `&T`, mutating APIs take `&var T`, and consuming APIs take
  it by value.
- A named owner union used as a standalone, returned, or by-value value must
  expose an explicit `deinit(self: T) -> void`.
- A union `deinit` cleans **only the active variant**, normally through an
  exhaustive `match`, and delegates to that payload's own cleanup:

```kizu
union MirStmt {
    LetCall(MirLetCall),
    ReturnExpr(MirReturnExpr),
    If(MirIf),
}

impl MirStmt {
    fn deinit(self: MirStmt) -> void {
        match self {
            LetCall(stmt) => stmt.deinit(),
            ReturnExpr(stmt) => stmt.deinit(),
            If(stmt) => stmt.deinit(),
        };
    }
}
```

- Inactive payload storage is never cleaned. Cleanup touches only the bytes the
  active tag says are initialized.
- `Array<OwnerUnion>.deinit()` cleans each initialized element through that
  element's explicit `deinit`. It must not rely on hidden destructor synthesis,
  and the union's `deinit` stays the single source-visible cleanup contract.
- No backend cleanup is generated that is not visible from a source-level
  `deinit`. There is no Drop/RAII/implicit destructor behavior.

### Unsupported Shapes

A payload shape that this section does not cover must fail visibly with a
backend/IR diagnostic at lowering time. It must not be lowered through a Go
fallback, a fixture-specific branch, or a static artifact path. Currently
unsupported shapes include:

- payloads whose size or alignment is not compile-time known for the union
- payloads that require a representation outside the value layout table (for
  example raw/nullable pointers or non-`i64`/`u8` integer widths, per
  [Unsupported Shapes Tracked By #495](#unsupported-shapes-tracked-by-495))
- recursive unions whose inline payload size cannot be bounded without
  indirection (indirection/boxing for recursive payloads remains #495 work)

When a selfhost path reaches an unsupported union shape, the dependent issue
must extend this section or open a concrete blocker linked to #495, never silence
it with a fallback.

### First Implementation Target

`MirStmt` in `selfhost/src/backend/compiled_mir.kizu` is the first representative
selfhost MIR sum type for #1001 to migrate off the tag-plus-all-fields encoding
onto this ABI. It currently carries a `kind: MirStmtKind` tag plus many inactive
owned `Array` and record fields; under this ABI its variants
(`LetCall`, `LetStruct`, `ReturnCall`, `ReturnStruct`, `ReturnExpr`,
`ReturnError`, `If`, ...) become payload variants with an explicit active-variant
`deinit`. #1001 implements that migration after this decision lands.

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
| `direct-record-roundtrip` | direct LLVM `call` passing and returning the #578 record shape |
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
| Array borrowed view | `@kizu_rt_array_at` | returns `![]u8` with a local read-only element view |
| Array cleanup | `@kizu_rt_array_deinit` | releases owned array storage |
| String construction | `@kizu_rt_string_new` | diagnostic and path buffers |
| String append bytes | `@kizu_rt_string_append_bytes` | copies borrowed bytes |
| String append byte | `@kizu_rt_string_append_byte` | appends one byte |
| String length | `@kizu_rt_string_len` | returns byte length |
| String borrowed view | `@kizu_rt_string_as_bytes` | returns a local read-only byte view |
| String cleanup | `@kizu_rt_string_deinit` | releases owned string storage |
| Map construction | `@kizu_rt_map_new` | resolver, type, and ownership tables |
| Map insert | `@kizu_rt_map_insert` | copies `[]u8` key and copy value |
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

For #575, Array storage is no longer a count-only smoke for the hosted runtime
template. The runtime object stores allocator pointer, element byte buffer
pointer, length, capacity, and element byte size. `append` copies exactly one
copy-element payload whose byte length matches the Array element size, `at`
returns a borrowed `[]u8` view of the stored element bytes for in-bounds
indexes, and returns the diagnostic `array index out of bounds` for invalid
indexes. `deinit` releases both the element buffer and the Array object.
Invalid element shape, null payload for positive element size, and
length/capacity overflow are rejected with the diagnostic `invalid array
element`. The storage artifact metadata records
`array-storage copy-element-byte-buffer`, `array-at returns-stored-element`,
`array-deinit releases-element-buffer`, and
`array-invalid-element-diagnostic invalid array element`,
`array-oob-diagnostic array index out of bounds`.

For #574, owned String storage is no longer a length-only smoke. The runtime
object stores allocator pointer, byte buffer pointer, byte length, and capacity.
`append_bytes` and `append_byte` copy caller bytes into owned storage,
`as_bytes` returns a borrowed `[]u8` view of those stored bytes, and
`deinit` releases both the byte buffer and the String object. `append_bytes`
rejects negative lengths, positive lengths with null pointers, and length
overflow with the diagnostic `invalid slice` before mutating the old buffer. The
storage artifact metadata records `string-storage byte-buffer`,
`string-as-bytes returns-stored-bytes`, and
`string-deinit releases-byte-buffer`,
`string-invalid-slice-diagnostic invalid slice`.

For #576, Map storage is no longer a single found/value slot. The hosted
runtime template implements the first bounded `Map<[]u8, i64>` shape with
two owned key slots. `insert` copies key bytes into map-owned storage and stores
the copy value, `contains` and `get_i64` compare key bytes instead of only
checking whether any insert happened, and `deinit` releases copied keys plus the
Map object. Missing keys return `Map.get key not found`; inserting beyond the
bounded two-entry slice returns `map capacity exceeded`. The storage artifact
metadata records `map-storage string-key-i64-two-entry`,
`map-key-ownership copies-key-bytes`,
`map-missing-key-diagnostic Map.get key not found`, and
`map-capacity-diagnostic map capacity exceeded`.

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

For #577, Arena storage is no longer a provenance/count-only smoke. The hosted
runtime template stores allocator pointer, length, element byte size, and inline
storage for two `AstNode` payload slots of at most 24 bytes each. `arena.add`
copies one payload whose byte length matches the configured element size, and
`arena.get` returns a borrowed view of the stored payload after provenance and
index checks. `arena.deinit` releases the Arena object containing the inline
payload storage. The storage artifact metadata records
`arena-storage ast-node-inline-two-slot`, `arena-get returns-stored-payload`,
`arena-deinit releases-inline-payload-storage`, and
`arena-payload-constraints ast-node-copy-scalar-view`.

ADR-0062 fixes the selfhost AST storage constraints for this ABI slice. This is
not the final general-purpose `std::arena::Arena<T>` payload policy for all Kizu programs;
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

`Box<i64>` uses an owned runtime object whose constructor copies an 8-byte
payload slice into heap storage. `Box<i64>.borrow()` returns a checked borrowed
`[]u8` view over that stored payload, and `Box<i64>.deinit()` releases both the
payload buffer and object header through the allocator captured at construction.
`borrow_mut`, writes, recursive payloads, and non-`i64` Box payloads remain
future issue-linked work; this ABI must not be widened by hidden fallback paths.

## External Primitives

| Kizu primitive | Runtime symbol | Signature |
| --- | --- | --- |
| `std::mem::page_allocator` | `@kizu_rt_mem_page_allocator` | `() -> %kizu.owned` |
| `std::mem::Box<i64>` constructor | `@kizu_rt_box_new` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.owned` |
| `Box<i64>.borrow` | `@kizu_rt_box_borrow` | `(%kizu.owned) -> %kizu.error.slice.u8` |
| `Box<i64>.deinit` | `@kizu_rt_box_deinit` | `(%kizu.owned) -> void` |
| `std::io::blocking` | `@kizu_rt_io_blocking` | `() -> %kizu.owned` |
| `std::fs::exists` | `@kizu_rt_fs_exists` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.bool` |
| `std::fs::metadata` | `@kizu_rt_fs_metadata` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.metadata` |
| `std::fs::read_dir` | `@kizu_rt_fs_read_dir` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.owned` |
| `std::fs::read_file` | `@kizu_rt_fs_read_file` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.slice.u8` |
| `std::fs::write_file` | `@kizu_rt_fs_write_file` | `(%kizu.owned, %kizu.slice.u8, %kizu.slice.u8) -> %kizu.error.void` |
| `std::fs::rename` | `@kizu_rt_fs_rename` | `(%kizu.owned, %kizu.slice.u8, %kizu.slice.u8) -> %kizu.error.void` |
| `std::fs::create_dir` | `@kizu_rt_fs_create_dir` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.void` |
| `std::fs::remove_dir` | `@kizu_rt_fs_remove_dir` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.void` |
| `std::io::write_stdout` | `@kizu_rt_io_write_stdout` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.void` |
| `std::io::write_stderr` | `@kizu_rt_io_write_stderr` | `(%kizu.owned, %kizu.slice.u8) -> %kizu.error.void` |
| `std::process::arg_count` | `@kizu_rt_process_arg_count` | `() -> i64` |
| `std::process::arg` | `@kizu_rt_process_arg` | `(i64) -> %kizu.error.slice.u8` |
| `std::process::env` | `@kizu_rt_process_env` | `(%kizu.slice.u8) -> %kizu.error.slice.u8` |
| `std::process::exit_code` | `@kizu_rt_process_exit_code` | `(i64) -> i64` |
| process termination | `@kizu_rt_process_exit` | `(i64) -> noreturn` |

The first argument is the explicit `Io` capability. The runtime must not use a
hidden global default capability.

`std::fs::read_dir` returns the `%kizu.error.owned` ABI because the success
payload is an owned `Array<std::fs::DirEntry>` handle. Each array element uses
the `%kizu.fs.dir_entry = { %kizu.slice.u8, %kizu.slice.u8, i1 }` layout for
`name`, `path`, and `is_dir`; consumers read it through the generic
`@kizu_rt_array_at` element-view ABI.

## Stdlib And Runtime Capability Inventory

This inventory records which capabilities are available before shrinking
Go-owned compiler helpers further:

| Capability | Status | Boundary |
| --- | --- | --- |
| file loading | available | `std::fs::{exists, metadata, read_dir, read_file}` through explicit `Io` |
| artifact writing | available | `std::fs::write_file`, `std::fs::rename`, `std::fs::create_dir`, and publish/rename runtime boundary |
| path handling | available | Kizu `std::path` and `std::path_bits`; no host path fallback |
| arrays | available | `std::array::Array<T>` over opaque runtime storage |
| strings | available | `std::string::String` over opaque runtime storage |
| maps | available for current compiler tables | `std::map::Map<[]u8, i64>` style copy payloads; broader payloads need issue-linked ABI work |
| boxes | available for `Box<i64>` constructor/read/deinit | copied 8-byte payload storage through `@kizu_rt_box_*`; borrow_mut/write/non-i64 payloads need issue-linked ABI work |
| allocator capability | available | explicit `std::mem::page_allocator()`; no implicit allocator fallback |
| diagnostics rendering | available for current CLI gates | structured source diagnostics remain a replacement blocker for broader frontend switches |
| process args/env/exit | available | `std::process::{arg_count, arg, env, exit_code}` and explicit process exit boundary |
| stdout/stderr | available | `std::io::blocking`, `write_stdout`, and `write_stderr` |
| future embed/builtin needs | blocked | `@embed` and broader `@` builtin syntax remain tracked by #610 |
| fixed-buffer/user allocators | blocked | allocator API design remains tracked by #549 |

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
publish boundary and is exposed through `std::fs::rename` for #1073 atomic
formatter writes. Additional filesystem calls require a concrete selfhost call
site and a linked roadmap issue.

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
selfhost check selfhost/tests/cli/parse_ok_minimal.kizu
selfhost check selfhost/tests/cli/parse_ok_minimal_alias.kizu
selfhost check selfhost/tests/cli/test_expect_ok.kizu
selfhost check selfhost/tests/cli/test_expect_ok_alias.kizu
selfhost check selfhost/tests/cli/test_expect_failure.kizu
selfhost check selfhost/tests/cli/test_expect_failure_alias.kizu
selfhost check selfhost/tests/cli/parse_invalid_missing_expr.kizu
selfhost check selfhost/tests/cli/parse_invalid_missing_expr_alias.kizu
selfhost check selfhost/tests/cli/parse_invalid_missing_assign.kizu
selfhost check selfhost/tests/cli/parse_invalid_missing_assign_alias.kizu
selfhost check examples/negative/moved_value.kizu
```

For #592, the check parity manifest adds aliases with identical source bytes.
For #602, the manifest also covers the positive minimal-main-return source and
its alias. For #604, it covers the `std::testing::expect(true|false)` sources
and aliases. For #646, it covers the missing-assignment parse diagnostic source
and alias. The hosted stage2 artifact reads the selected target source and
recognizes the print-hello, minimal-main-return, testing-expect,
missing-expression, missing-assignment, and moved-value-use source shapes
instead of branching on those fixed fixture paths.
The check parity gate runs through
`target/selfhost/stage2/selfhost`, records `go.cmd-kizu-fallback none`, and does
not bootstrap from scratch by default. This does not claim general
parse/type/move/borrow checker parity and does not extend `selfhost-abi-v0`.

For #525, the hosted CLI also accepts the bounded `parse <file>` parity cases in
`selfhost/tests/cli/parse-parity.tsv`:

```sh
selfhost parse selfhost/tests/cli/parse_ok_minimal.kizu
selfhost parse selfhost/tests/cli/parse_ok_minimal_alias.kizu
selfhost parse selfhost/tests/cli/parse_print_hello.kizu
selfhost parse selfhost/tests/cli/parse_print_hello_alias.kizu
selfhost parse selfhost/tests/cli/test_expect_ok.kizu
selfhost parse selfhost/tests/cli/test_expect_ok_alias.kizu
selfhost parse selfhost/tests/cli/test_expect_failure.kizu
selfhost parse selfhost/tests/cli/test_expect_failure_alias.kizu
selfhost parse examples/negative/moved_value.kizu
selfhost parse selfhost/tests/cli/check_moved_value_alias.kizu
selfhost parse selfhost/tests/cli/parse_invalid_missing_expr.kizu
selfhost parse selfhost/tests/cli/parse_invalid_missing_expr_alias.kizu
selfhost parse selfhost/tests/cli/parse_invalid_missing_assign.kizu
selfhost parse selfhost/tests/cli/parse_invalid_missing_assign_alias.kizu
```

The parse parity gate compares byte-for-byte stdout, stderr, and exit codes
against checked-in goldens and runs through `target/selfhost/stage2/selfhost`.
It does not invoke Go `cmd/kizu` as a fallback. For #579, the positive minimal
parse shape is source-driven: any file containing exactly the newline-terminated
`fn main() { return; }` source uses the same hosted parse path. For #594, the
print-call shape reads the target once and recognizes the newline-terminated
multi-line `print("hello, kizu")` source shape, emitting the same canonical
parse stdout as the Go CLI. For #598, the `std::testing::expect(true|false)`
shapes add the first parse slice with an error-union return type and a
qualified call expression that matches the hosted test artifact sources. For
#600, the moved-value source shape adds a struct declaration, record literal,
field access, direct calls, and repeated value use to the positive parse
surface. For #586, the negative missing-expression shape is also source-driven:
any file containing exactly the newline-terminated
`fn main() { let value = ; }` source uses the same hosted diagnostic path. The
same source-driven guarantee applies to #646 for the multi-line
`let value;` missing-assignment diagnostic path. The alias fixtures prove these
parse branches are no longer bound to a single path. Broader parse and
diagnostic recovery remain deferred. The current CLI parity support and
deferrals are recorded in `docs/selfhost-cli-parity.md`.

For #1073, hosted `fmt <file>` is routed through the selfhost formatter writer
and has its own dispatch instead of the parse command's `fn main` source-shape
guard. Hosted `fmt --write <file>` reuses the same formatted byte buffer and
publishes it with `fs_write_file` to a sibling temporary path followed by
`fs_rename`. The dedicated
`selfhost/tests/cli/fmt-parity.tsv` gate records source-preserving comment,
doc-comment, inline-comment, import sorting, and deterministic no-write rows.
Syntax surfaces outside that manifest remain deferred.

For #531, hosted `run <file>` and `kizu test <file>` use backend artifact
emit/link/execute instead of a selfhost interpreter. The first runnable fixture
pair only requires the existing stdout, stderr, process exit, and trap
boundaries:

```text
std::io::blocking
std::io::write_stdout
std::io::write_stderr
std::process::exit_code
@kizu_rt_process_exit
@kizu_rt_trap
```

The target program/test fixtures do not require new container storage layout or
box storage, so #496 is not a blocker for the first slice. The hosted compiler
may still use the existing selfhost storage ABI while parsing, checking, and
emitting those artifacts.

For #588, the first `run <file>` success and frontend-failure dispatch paths are
source-driven. Files containing a supported `print("<simple ascii>")` call in
`main` emit `target/selfhost/run/<basename-without-extension>.ll` with stdout
derived from that string payload. The hosted emitter escapes backslash bytes as
LLVM C string hex escapes while keeping the runtime stdout length derived from
the original payload. Files containing exactly the newline-terminated
`fn main() { let value = ; }` source return the existing parse diagnostic
without emitting an artifact. The original, alias, helper-before-main, custom
string, and backslash rows in `run-parity.tsv` prove those branches are not
bound to one fixture path, one fixed artifact stem, the first declaration being
`main`, or the `hello, kizu` source literal. Broader expression lowering remains
outside this slice.

For #590, the first `test <file>` expect-ok and expect-failure dispatch paths are
also source-driven. Files containing exactly the newline-terminated expect-ok or
expect-failure source emit
`target/selfhost/test/<basename-without-extension>.ll`. The original, alias, and
helper-before-main rows in `test-parity.tsv` prove those branches are not bound
to one fixture path
or one fixed artifact stem. Test discovery remains outside this slice.

The first #531 parity gate may execute the emitted program/test artifact outside
the hosted compiler process. Therefore `selfhost-abi-v0` does not add a process
spawn/wait primitive for the first slice. If later public `run` parity requires
the hosted compiler process itself to spawn and wait for child artifacts, that
new process ABI belongs in #495 and must be documented here before use.

## Textual LLVM Validation

Until CI requires an LLVM verifier binary, #454 uses this repository command as
the documented textual-IR validation gate:

```sh
just selfhost-backend-artifact-gate
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

The backend consumes only IR artifacts that declare the checked selfhost package
contract. The required facts include the `selfhost-checked-package-v1` contract,
the checked `selfhost::cli_main` package entry, the hosted CLI and smoke entries,
the executable contract source facts for `selfhost::backend::data` and
`selfhost::backend::executable`, the selected-body executable lowering fact,
the hosted executable parser body contract, the hosted executable lowering body
contract, the hosted executable result layout and enum tag ABI, and
`checked-diagnostics 0`.
This keeps `target/selfhost/selfhost.ll` tied to a successful selfhost frontend
check instead of accepting any file with a `kizu-ir-v0` header and `package
selfhost`.

For #456 the Kizu backend performs cheap header validation before copying the
runtime storage template. The same Go gate checks
`target/selfhost/selfhost.storage.ll` and `target/selfhost/selfhost.storage.ll.meta`.
The storage validation requires the reachable Array, String, Map, diagnostic,
Arena, and Handle runtime symbols, the `@kizu_selfhost__runtime_storage_smoke`
entry, explicit allocator-boundary metadata, Array copy-element metadata, String
byte-buffer metadata, Map string-key/i64 metadata, Arena payload metadata,
handle provenance metadata, and the absence of Go interpreter/stdprim fallback
markers in the storage LLVM artifact.
The gate also links `selfhost.storage.ll` with the host capability runtime and a
tiny C harness, then runs the storage smoke so Array payload storage, String
byte storage, Map key/value storage, and Arena payload/Handle behavior cannot be
only dead textual declarations. For #578, the same smoke also runs a direct
record and error-union ABI round-trip, including `!record` success and failure
paths.

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
- tagged-union payload shapes beyond the minimal `tag + inline payload storage`
  ABI in [Tagged-Union Payload Layout](#tagged-union-payload-layout) (for
  example heap-indirected or recursive payloads)
- task, thread, channel, mutex, and atomic runtime ABI
- C ABI interop and native object/linker metadata beyond textual LLVM emission
- async/await or hidden runtime scheduling

If #454, #456, or #457 reaches one of these shapes in the selfhost artifact,
the dependent issue must either extend this document or open a concrete blocker
linked to #495.
