# ADR-0052: Zig-style native build policy

## Status

Accepted.

## Context

Kizu needs native executable generation, but the build model must stay explicit.
Zig is the main reference for the direction: target selection, libc linkage,
runtime choice, and generated artifacts should be visible build inputs rather
than hidden global behavior.

The current implementation can link a limited LLVM-lowered subset through
`clang` and a small `kizu_print_*` runtime shim. This is useful as a first
pipeline, but it is not yet a Zig-equivalent build system.

## Decision

Kizu native builds follow these rules:

- native builds are explicit: `kizu build --target native ...`
- the current native mode links with libc through the host `clang`
- libc usage is allowed for the initial native backend
- future no-libc / freestanding builds are a first-class build mode
- libc vs no-libc must be visible in command-line flags and build metadata
- target triple, CPU, OS, ABI, linker, and runtime mode must become explicit
  build inputs before cross compilation is considered complete
- build cache keys must include target, ABI, libc mode, linker mode, runtime
  hash, optimization mode, and stdlib hash
- native build must reject unsupported lowered features before invoking clang
- no build mode may weaken safe Kizu ownership, borrow, or memory-safety checks

Initial command shape:

```text
kizu build --target native \
  [--opt] \
  [--triple <arch-os-abi>] \
  [--libc on|off] \
  [--runtime hosted|freestanding] \
  [--emit exe|obj|llvm] \
  [-o <out>] \
  <file>
```

Planned command shape:

```text
kizu build \
  --target native \
  --triple <arch-os-abi> \
  --cpu <cpu> \
  --abi <abi> \
  --libc on|off \
  --runtime hosted|freestanding \
  --emit exe|obj|llvm \
  [-o <out>] \
  <file>
```

Only `--libc on --runtime hosted --emit exe` is implemented today.
`--libc off`, `--runtime freestanding`, and object/native LLVM artifact modes
are accepted as explicit command-line vocabulary but rejected until they have
real backend support.

`--libc on` means the build may use C runtime and libc symbols. `--libc off`
means generated code and the selected Kizu runtime must not require libc.

`--runtime hosted` means the runtime can assume OS facilities selected by the
target and capability APIs. `--runtime freestanding` means no hidden OS or libc
facilities are available; the program must provide or avoid required runtime
symbols explicitly.

## Current Native Backend Scope

The current backend supports only the LLVM-lowered subset:

- scalar integer and boolean operations
- string literals as `[]const u8`
- `print` through `kizu_print_*`
- simple functions
- if / if expression
- while / for / break / continue

Unsupported features must fail with a backend error before clang:

- struct layout and field access
- enum, union, and match lowering
- error union ABI
- borrow / dereference lowering
- arena / handle runtime
- stdlib containers and host APIs
- task, thread, channel, mutex, and atomic runtime
- extern C library selection
- raw pointer runtime operations

## Consequences

The first native backend may depend on libc, but the design must not bake libc
into the language semantics. Kizu can improve incrementally from hosted libc
builds toward freestanding builds without changing source-level memory-safety
rules.

Build performance work must measure native builds separately from interpreter,
IR, and WASM paths. Native artifacts must stay under `target/native/`, and
cache growth must remain bounded.
