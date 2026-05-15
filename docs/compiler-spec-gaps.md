# Compiler Specification Gaps

This document tracks language and toolchain decisions that must be specified
before the self-host compiler can replace the Go implementation.

## Required Before Practical Self-Host

### Module Graph

- Parse `kizu.toml`.
- Resolve package root namespace from `[package].name`.
- Resolve source modules from `[modules].root` and `[modules].paths`.
- Reject cyclic imports.
- Define duplicate module path errors.
- Define missing module errors.

### Name Resolution

- Resolve local bindings before imported module names.
- Resolve imported modules by their last path segment.
- Reject ambiguous imported last segments.
- Keep `std::...` available without user import.
- Define whether imported modules can shadow local names. Recommended: no.

### Visibility

- Enforce default-private top-level declarations.
- Enforce default-private struct fields across module boundaries.
- Reject private types in public function signatures.
- Reject private struct fields in external construction.
- Define public enum tag and union variant access.

### Diagnostics

- Define source span model: file, byte offset, line, and column.
- Define multi-file diagnostic rendering.
- Define import-cycle diagnostic format.
- Define private-item diagnostic format.

### Compiler Outputs

- Define check-only output.
- Define interpreter run output.
- Define IR artifact output.
- Define native, WASM, and C backend artifact naming.
- Define generated artifact locations under `target/`.

### Build Cache

- Define cache key fields for manifest, module graph, source hashes, compiler
  version, target, backend, and optimization mode.
- Define no-op rebuild behavior.
- Define cache size accounting and pruning behavior.

### Bootstrap

- Define the stage where the Kizu compiler can compile/check its own sources.
- Define the Go compiler oracle comparison boundary.
- Define which conformance tests must pass before switching production paths.

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
