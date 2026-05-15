# Compiler Specification Decisions

This document tracks compiler-facing language and toolchain decisions that are
specified but still need implementation work before the self-host compiler can
replace the Go implementation.

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

Source: [ADR-0050](adr/0050-visibility-diagnostics.md).

- top-level declarations are private by default
- struct fields are private by default
- `pub` exposes top-level declarations and fields
- public signatures may not expose private types
- external modules may not construct or access private fields
- public enum tags and union variants are visible when their type is public

### Diagnostics

Source: [ADR-0050](adr/0050-visibility-diagnostics.md).

Spans carry:

- file
- byte start
- byte end
- line
- column

Multi-file diagnostics use one primary span plus related spans. Import cycles
and private access errors must show the relevant module graph or definition
site.

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
- `kizu why-rebuild`

### Bootstrap

Source: [ADR-0051](adr/0051-compiler-outputs-cache-bootstrap.md).

The Go implementation remains the oracle until the Kizu compiler matches it for
lexer, parser, diagnostics, type checking, ownership checking, IR, backend smoke
tests, and self-check/build.

## Implementation Work Still Needed

- Connect explicit build outputs to a package artifact layout under `target/`: #100.
- Add bootstrap oracle tests for parser, diagnostics, type checking, ownership,
  IR, backend outputs, and module fixtures: #91.

## Implemented Groundwork

- `pub` and `import` tokens.
- Top-level import parsing.
- `pub` parsing on top-level declarations and struct fields.
- Minimal `kizu.toml` parsing for `[package]` and `[modules]`.
- File path to module path graph resolution.
- Single-program public API checks for private type leaks.
- Module-boundary visibility checks reject private namespace access, imported
  private type leaks in public signatures, and private field construction.
- AST declarations and visibility-sensitive expressions carry byte spans with
  line/column origins for multi-file diagnostics.
- Visibility diagnostics render a primary location and related declaration or
  field location.
- Module-aware build cache keys include manifest, module graph, source, public
  interface, target/backend/optimization, and stdlib hashes.
- `why-rebuild` explains package input changes for manifest, module graph,
  public interface, source-only, stdlib, and cache version changes.
- Multi-file module conformance fixture at `tests/conformance/modules/basic`.
- Go project tests resolve the module fixture graph and parse every fixture
  source file.
- Resolver phase parses package modules, validates explicit imports, rejects
  missing imports, same-name import collisions, import shadowing, and cycles.
- `kizu check <package-dir>` and `kizu check <package-dir>/kizu.toml` run
  multi-file package smoke checks.
- The self-host frontend can read the module fixture source path through the
  same explicit `std::fs` / `std::path` / `std::process` APIs.

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
