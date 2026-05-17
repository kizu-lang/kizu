# Kizu Bootstrap Contract

This document defines how the Go compiler and the Kizu self-host compiler stay
1:1 until the Kizu implementation can replace a production path.

The current completion audit is tracked in
[`docs/bootstrap-1to1-audit.md`](bootstrap-1to1-audit.md). That audit is the
source of truth for whether "1:1対応完了" is actually achieved.

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

## v0.3 Target

v0.3 is not the existing oracle bridge state. v0.3 means a Kizu-only standalone
self-host compiler artifact that can check/build user programs and rebuild the
selfhost compiler package without using the Go CLI/interpreter as the compiler
execution path.

Required v0.3 smoke shape:

```sh
kizu build --target aarch64-apple-darwin selfhost
./target/kizu-selfhost check examples/hello.kizu
./target/kizu-selfhost build --target aarch64-apple-darwin examples/hello.kizu
./target/kizu-selfhost check selfhost
./target/kizu-selfhost build --target aarch64-apple-darwin selfhost
./target/kizu-selfhost build --emit-llvm examples/hello.kizu
```

The v0.3 standalone artifact is a native executable. The required native backend
path is Kizu-owned LLVM IR text, `llc` object emission, and `lld` native linking.
The initial supported native target is host macOS arm64
(`aarch64-apple-darwin`). The target model must still preserve explicit arch,
OS, ABI, and object-format fields so later targets do not require a redesign.
libc / libSystem is allowed only as an explicit target stdlib backend boundary;
the Kizu language core must not depend on libc. See
[`docs/adr/0055-v0-3-native-libc-boundary.md`](adr/0055-v0-3-native-libc-boundary.md).

The previous bridge work already implemented:

- lexer token snapshots
- parser AST snapshots
- semantic symbol/diagnostic snapshots
- selected memory-safety negative fixtures through the Go checker oracle
- minimal IR/backend snapshots
- module-aware cache measurement through `internal/buildcache`

Those bridges are required inputs for v0.3, but they are not the v0.3 release.
The v0.3 umbrella is #256.

## Self-Host Migration Strategy

Self-host migration is module-first. The Go compiler remains the oracle for
multi-file package loading, module graph resolution, visibility, diagnostics,
and package-level check/build behavior while Kizu compiler modules are ported
one component at a time.

`selfhost/frontend.kizu` is a legacy oracle harness. It can keep protecting
existing bootstrap behavior, but it is not the production self-host compiler
source. New self-host work should target a normal multi-file Kizu package under
`selfhost/src`.

The target source layout is:

```text
selfhost/
  kizu.toml
  src/
    main.kizu
    token.kizu
    lexer.kizu
    ast.kizu
    parser.kizu
    diagnostics.kizu
    resolver.kizu
    types.kizu
    ownership.kizu
    ir.kizu
    backend.kizu
    cache_contract.kizu
```

The initial migration issues are:

- module-first migration reset: #190
- multi-file self-host scaffold: #191
- component readiness gate: #196
- token and lexer port: #192
- AST and parser port: #193

Before starting a port, use [`docs/selfhost-readiness.md`](selfhost-readiness.md)
to confirm the component's Go oracle, fixture coverage, stdlib dependencies,
memory-safety boundary, and switch criteria.

Backend smoke scope before standalone v0.3:

- LLVM text emission remains Go-owned until the Kizu-owned self-host backend
  reaches parity through `kizu build --emit-llvm`
- WASM WAT emission remains experimental and is not the v0.3 standalone artifact
  path
- package build smoke includes `kizu build --emit-llvm selfhost` and
  `kizu build --target aarch64-apple-darwin selfhost`
- native executable generation is required for the v0.3 self-host switch target

Cache ownership before standalone v0.3:

- build cache remains Go-owned until Kizu owns filesystem, hashing, module graph,
  and artifact layout APIs
- self-host compiler must keep emitting the cache contract snapshot so a future
  switch cannot silently drop compiler version, target, backend, optimization
  mode, input kind, manifest hash, module graph hash, source hash, public
  interface hash, stdlib hash, artifact layout, default limit, status, prune,
  or why-rebuild behavior

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
go test ./tests/bootstrap
go test ./internal/buildcache -run 'Package(Cache|Why)'
go run ./cmd/kizu build --emit-llvm examples/hello.kizu
go run ./cmd/kizu build --target wasm32-wasi examples/hello.kizu
go run ./cmd/kizu build --target aarch64-apple-darwin examples/hello.kizu
just perf-cache-isolated
```

Use the strict completion gate only when claiming Go/Kizu 1:1 completion:

```sh
KIZU_REQUIRE_1TO1=1 go test ./tests/bootstrap
```
