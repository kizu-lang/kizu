# Compiler Specification Decisions

This document tracks compiler-facing language and toolchain decisions that are
specified but still need implementation work before another compiler
implementation can replace the Go implementation.

## Accepted Decisions

### Module Graph

Source: [ADR-0049](adr/0049-module-graph-name-resolution.md).

- `kizu.toml` is the package root marker.
- `[package].name` is the user package root namespace.
- `[modules].root` defines the entry module.
- `[modules].paths` defines source roots.
- File paths map to module paths predictably.
- Duplicate module paths, missing imports, and cyclic imports are errors.

### Name Resolution

Source: [ADR-0049](adr/0049-module-graph-name-resolution.md).

Resolution order:

1. local bindings
2. current module top-level declarations
3. imported module names by last segment
4. built-in root namespace `std`
5. error

Rules:

- imported modules are referenced by their last path segment
- same-last-segment imports are rejected
- local declarations may not shadow imported module names
- user packages may not be named `std`

### Visibility

Source: [ADR-0050](adr/0050-visibility-diagnostics.md) and
[ADR-0070](adr/0070-package-export-surface.md).

- top-level declarations are private by default
- struct fields are private by default
- `pub` exposes top-level declarations and fields
- `[modules].exports` lists package modules visible outside the package
- non-exported package modules may share `pub` helpers inside the package
- public signatures may not expose private types
- external modules may not construct or access private fields
- public enum tags and union variants are visible when their type is public

### Struct Literals

Source: [ADR-0079](adr/0079-struct-literal-field-initializers.md).

- a struct literal names each declared field exactly once
- an undeclared field name is an error
- an omitted declared field is an error
- a repeated field name is an error; last-wins is not the rule
- written order is free; fields match the declaration by name
- `examples/negative/duplicate_struct_field.kizu` carries the repeated-name case

### Diagnostics

Source: [ADR-0050](adr/0050-visibility-diagnostics.md) and
[ADR-0072](adr/0072-diagnostic-message-style.md).

Spans carry:

- file
- byte start
- byte end
- line
- column

Multi-file diagnostics use one primary span plus related spans. Import cycles
and private access errors must show the relevant module graph or definition
site.

Go/Kizu diagnostic oracle tests compare a stable summary, not terminal
rendering. The summary is ordered as primary file path, byte start/end, line,
column, message, then related spans in emission order with the same fields and
their message. Color, caret art, and help text are renderer concerns.

### Compiler Outputs

Source: [ADR-0051](adr/0051-compiler-outputs-cache-bootstrap.md).

Artifact families live under:

```text
target/
  check/
  interp/
  ir/
  native/
  wasm/
  c/
  cache/
```

`kizu check` does not create durable artifacts by default. IR, WASM, native, and
C outputs are explicit build artifacts.

### Build Cache

Source: [ADR-0051](adr/0051-compiler-outputs-cache-bootstrap.md).

Cache keys include compiler version, manifest hash, module graph hash, source
hashes, public interface hash, target, backend, optimization mode, and stdlib
version or hash.

Required user-facing commands:

- `kizu cache status`
- `kizu cache prune`

### Native Build Policy

Source: [ADR-0052](adr/0052-zig-style-native-build-policy.md).

- native builds are explicit through `kizu build --target native`
- the current backend may use host `clang` and libc
- no-libc / freestanding builds are a planned first-class mode
- `--cpu`, `--abi`, `--libc off`, `--runtime freestanding`,
  `--emit obj|llvm`, and non-clang linkers are accepted CLI vocabulary but
  rejected until implemented
- native builds write `<output>.kizu-build.json` with explicit build inputs
- libc mode, runtime mode, target triple, ABI, linker, and optimization mode
  are build inputs and cache-key inputs
- unsupported lowered features must fail before invoking clang

### Tagged-Union Payload Layout

Inline tagged-union payloads must have a compile-time known size and alignment.
Heap-indirected and recursive payload shapes are tracked by #495.

### Bootstrap

Source: [ADR-0051](adr/0051-compiler-outputs-cache-bootstrap.md).

The Go implementation remains the oracle until the Kizu compiler matches it for
lexer, parser, diagnostics, type checking, ownership checking, IR, backend smoke
tests, and self-check/build.

Self-host component migration readiness is tracked by
[ADR-0054](adr/0054-self-host-readiness-gate.md). A component should not
replace a Go production path until its language features, stdlib dependencies,
diagnostics, memory-safety cases, and oracle tests are explicit.

## Implementation Work Still Needed

- Connect parsed imports to multi-file checking: #88.
- Add resolver phase between parser and type checker: #88.
- Enforce visibility across module boundaries: #89.
- Preserve byte spans and file IDs through compiler phases: #89.
- Render multi-file diagnostics: #89.
- Add artifact layout under `target/`: #90.
- Add explicit native target metadata flags for triple, ABI, libc, runtime, and
  linker mode.
- Add no-libc / freestanding runtime support after hosted libc native builds are
  stable.
- Extend build cache keys with module graph and public interface hashes: #90.
- Add bootstrap oracle tests for parser, diagnostics, type checking, ownership,
  IR, backend outputs, and module fixtures: #91.

## Implemented Groundwork

- `pub` and `import` tokens.
- Top-level import parsing.
- `pub` parsing on top-level declarations and struct fields.
- Minimal `kizu.toml` parsing for `[package]` and `[modules]`.
- File path to module path graph resolution.
- Single-program public API checks for private type leaks.
- Multi-file module fixture at `tests/fixtures/modules/basic`.
- Go project tests resolve the module fixture graph and parse every fixture
  source file.

## Postponed

- Package manager.
- Alias imports.
- Re-exports.
- Wildcard imports.
- Relative imports.
- `pub(crate)` / `pub(super)`.
- Conditional imports.
- Build scripts.
- Plugin-based compiler extensions.
