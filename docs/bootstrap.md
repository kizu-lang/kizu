# Kizu Bootstrap Contract

This document defines how the Go compiler and the Kizu self-host compiler stay
1:1 until the Kizu implementation can replace a production path.

## Phase Oracle Contract

Each compiler phase must expose the same normalized output from Go and Kizu.

| Phase | Go oracle | Kizu oracle | Required equality |
| --- | --- | --- | --- |
| lexer | `token.Token` stream | `Token` stream | kind, literal, byte start/end, line, column |
| parser | Go AST snapshot | self-host AST snapshot | functions, imports, structs, enums, unions, returns |
| resolver | `project.Package` graph | self-host module graph snapshot | module paths, imports, diagnostics |
| diagnostics | `project.Diagnostic` / checker errors | self-host diagnostic snapshot | primary span, related spans, message substring |
| type | `internal/types` result | self-host semantic snapshot | pass/fail and symbol/type counts |
| ownership | `internal/ownership` result | self-host semantic snapshot | pass/fail for memory-safety fixtures |
| IR | `internal/ir` summary | self-host IR snapshot | function items and block items |
| backend | LLVM/WASM smoke result | self-host backend snapshot | selected artifact count and smoke status |
| cache | `internal/buildcache` result | self-host cache contract | no-op, edit reason, std hash behavior |

The snapshots are intentionally normalized. They are not a compatibility layer:
when Kizu gains a richer phase output, Go must expose the same shape or the
switch is blocked.

## Current v0.3 Scope

Implemented oracle bridges:

- lexer token snapshots
- parser AST snapshots
- semantic symbol/diagnostic snapshots
- selected memory-safety negative fixtures through the Go checker oracle
- minimal IR/backend snapshots
- module-aware cache measurement through `internal/buildcache`

Backend smoke scope in v0.3:

- LLVM text emission remains Go-owned and smoke-tested through `kizu build --emit-llvm`
- WASM WAT emission remains Go-owned and smoke-tested through `kizu build --target wasm32-wasi`
- native executable generation is not a v0.3 self-host switch target

## Switch Checklist

Before any phase switches from Go to Kizu, all of these must be true:

- the phase has a checked-in Go/Kizu oracle snapshot test
- positive examples pass in both implementations
- selected negative examples fail with matching diagnostic substrings
- memory-safety negative fixtures keep matching pass/fail behavior
- module fixtures are covered when the phase reads package input
- build cache keys include every new phase input
- no-op rebuild, single-file edit, and std source hash behavior are measured
- any allowed difference is documented in an ADR before merge

## Performance Commands

Use these commands before changing bootstrap, backend, or cache behavior:

```sh
go test ./tests/selfhost
go test ./internal/buildcache -run 'Package(Cache|Why)'
go run ./cmd/kizu build --emit-llvm examples/hello.kizu
go run ./cmd/kizu build --target wasm32-wasi examples/hello.kizu
just perf-cache-isolated
```
