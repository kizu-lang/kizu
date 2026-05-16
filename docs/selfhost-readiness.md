# Self-Host Readiness Gate

This document defines when a Go compiler component is ready to be ported into
the Kizu self-host compiler package under `selfhost/src`.

The goal is not to finish every Go compiler feature before writing Kizu code.
The goal is to avoid porting unclear behavior. Each component must have a clear
Go oracle, reusable fixtures, and explicit stdlib / memory-safety boundaries
before the Kizu version can replace any production path.

## Global Rules

- Go remains the production compiler until a component passes its switch gate.
- Kizu modules under `selfhost/src` are the future production source.
- `selfhost/frontend.kizu` remains a legacy oracle harness only.
- New compiler-shaped TODOs must become GitHub Issues, not local comments.
- New language or safety decisions require `SPEC.md` or ADR updates.
- Build/cache changes must include no-op rebuild and cache-size considerations.

## Component Gate

Before starting a component port:

- Issue exists and names the Go package and target Kizu module.
- Input shape is fixed: single file, package directory, manifest, or source buffer.
- Required stdlib APIs are listed.
- Required ownership, borrow, allocator, and deinit behavior is listed.
- Positive and negative fixtures exist or are listed as required work.
- Go oracle test exists or is listed as required work.
- Expected diagnostics include span expectations when spans are part of behavior.

Before merging a component port:

- Kizu module checks as part of `kizu check selfhost` or equivalent package test.
- Go and Kizu oracle outputs match for the component's covered input shape.
- Conformance fixtures are reused instead of duplicated when possible.
- Memory-safety regressions remain covered by negative tests.
- `pre-commit run --all-files` passes.
- The component Issue is updated with the exact verification commands.

Before switching production behavior from Go to Kizu:

- The component row in `docs/bootstrap-1to1-audit.md` is `strong | none`.
- A strict opt-in gate fails if the component is incomplete.
- Any remaining Go-owned behavior is explicit and tested as Go-owned.
- The PR states what is switched and what remains Go-owned.

## Current Component Status

| Component | Go oracle status | Kizu module status | Next blocker |
| --- | --- | --- | --- |
| token / lexer | strong legacy oracle through `tests/selfhost` | token API, lexer scanner body, and executable package component test are ported under `selfhost/src` | production switch decision |
| AST / parser | strong legacy oracle through `tests/selfhost` | AST node shapes plus executable parser summary/declaration/detail component tests are ported under `selfhost/src` | production switch decision |
| diagnostics / resolver | strong legacy oracle through `tests/selfhost` and module fixtures | diagnostic span shape, module alias helpers, and executable package component test are ported under `selfhost/src` | expand to full module graph and diagnostic object oracle |
| type checker | strong legacy oracle for selected conformance and diagnostics | core type-name classification and executable package component test are ported under `selfhost/src` | expand to function/local type environment snapshots |
| ownership / borrow checker | strong legacy memory-safety oracle | move/copy/borrow transition facts and executable package component test are ported under `selfhost/src` | expand to scoped environment snapshots |
| IR | strong normalized dump oracle | IR module/function/block/instruction summary facts and executable package component test are ported under `selfhost/src` | expand to full normalized IR dump |
| backend | Go-owned smoke fingerprint oracle | target/artifact summary facts and executable package component test are ported under `selfhost/src` | not a native production switch target |
| cache | Go-owned switch contract oracle | cache input/rebuild reason summary facts and executable package component test are ported under `selfhost/src` | Go-owned filesystem and hashing primitives |

## Production Ownership Decision

As of #230, no production compiler phase is switched from Go-owned execution to
Kizu-owned execution.

The Kizu modules under `selfhost/src` are executable component boundaries and
oracle-facing summaries. They prove the package shape, stdlib dependencies,
and phase facts needed for self-host migration. They do not yet replace the Go
compiler packages in the CLI path.

| Component | Production owner | Decision |
| --- | --- | --- |
| token / lexer | Go | Keep Go-owned until the package runtime can execute the lexer as a production scanner, not only a component oracle. |
| AST / parser | Go | Keep Go-owned until full AST construction, errors, and spans are produced by Kizu source for package and single-file inputs. |
| diagnostics / resolver | Go | Keep Go-owned until Kizu owns full module graph loading, cycle detection, visibility checks, and multi-file diagnostic rendering. |
| type checker | Go | Keep Go-owned until Kizu owns function signatures, local environments, call checking, stdlib boundaries, and diagnostics. |
| ownership / borrow checker | Go | Keep Go-owned until Kizu owns scoped state, last-use borrow endings, field borrow tracking, and memory-safety diagnostics. |
| IR | Go | Keep Go-owned until Kizu emits the normalized IR dump, not only summary facts. |
| backend | Go | Keep Go-owned; native production switching is not a v0.3 target. LLVM/WASM smoke output remains Go-emitted. |
| cache | Go | Keep Go-owned until Kizu owns filesystem walking, hashing, artifact layout, status, prune, and why-rebuild execution. |

This is an explicit non-switch decision, not a hidden fallback. The current CLI
continues to use the Go compiler path while `kizu check selfhost`,
`kizu test selfhost`, and `KIZU_REQUIRE_1TO1=1 go test ./tests/bootstrap`
guard the self-host package boundary.

## #192 Token / Lexer Readiness

Target mapping:

```text
internal/token -> selfhost/src/token.kizu
internal/lexer -> selfhost/src/lexer.kizu
```

Ready to implement after:

- #198 proves imported types such as `token::Token` and `token::TokenKind` work
  across self-host modules.
- #199 records the lexer stdlib dependency gate for `[]const u8`,
  `std::array::Array<Token>`, `!T`, `?T`, allocator, and `deinit`.
- The Kizu lexer output schema is fixed to token kind, literal, byte start,
  byte end, line, and column.
- Package component tests now execute through `kizu test selfhost`. Go tests
  remain the production oracle until the switch gate is satisfied.
- Cross-module value expression support is available for imported enum variants,
  public struct literals, and public function calls.
- The scanner body is now ported into `selfhost/src/lexer.kizu`, and package
  component tests exercise the module boundary.

Completion evidence:

- `selfhost/src/token.kizu` exposes the token API needed by lexer.
- `selfhost/src/lexer.kizu` scans source buffers without hidden allocation.
- Reused conformance fixtures compare Kizu lexer output against Go lexer output.
- Invalid token fixtures compare diagnostics and spans.

## #193 AST / Parser Readiness

Target mapping:

```text
internal/ast -> selfhost/src/ast.kizu
internal/parser -> selfhost/src/parser.kizu
```

Ready to implement after:

- #192 lands with a stable token stream API.
- Parser node storage chooses `std::array::Array<T>` or `Arena<T> / Handle<T>`
  explicitly for AST ownership.
- Parse errors are modeled as `!T` diagnostics, not hidden panics.
- Snapshot granularity is fixed for declarations, statements, expressions, and spans.

Completion evidence:

- AST and parser modules no longer depend on selected helpers from
  `selfhost/frontend.kizu`.
- Parseable conformance sources and module fixtures compare AST snapshots
  against the Go parser.
- Negative parser fixtures compare diagnostic message substrings and spans.
- Semicolon, explicit return, `::` namespace, import, `pub`, enum, union, and
  typed-error syntax are covered.

## #218 Diagnostics / Resolver Readiness

Target mapping:

```text
internal/project diagnostics -> selfhost/src/diagnostics.kizu
internal/project resolver    -> selfhost/src/resolver.kizu
```

Ready to expand after:

- Token and parser package component tests execute through `kizu test selfhost`.
- Diagnostic primary span data is represented in Kizu source.
- Module alias resolution is represented without allocation-heavy helpers.

Completion evidence:

- `selfhost/src/diagnostics.kizu` exposes severity, message, and primary span
  data.
- `selfhost/src/resolver.kizu` exposes final-segment alias and reserved `std`
  path classification.
- `selfhost/src/resolver_component_test.kizu` executes those APIs through the
  package component runtime.
- Further resolver work must compare full module graph snapshots against the Go
  package resolver before production switching.

## #220 Type Checker Readiness

Target mapping:

```text
internal/types -> selfhost/src/types.kizu
```

Ready to expand after:

- Parser declaration snapshots expose enough type spellings for checker input.
- Resolver module path helpers can classify imported namespaces.
- Core type-name facts are executable through package component tests.

Completion evidence:

- `selfhost/src/types.kizu` classifies primitive, byte-slice, user, unknown, and
  `!T` error-union type spellings.
- Type summaries expose copy-ness and error-union success type without hidden
  runtime behavior.
- `selfhost/src/types_component_test.kizu` executes those APIs through
  `kizu test selfhost`.
- Full type checker porting still requires Go/Kizu snapshots for function
  signatures, local bindings, call checking, stdlib boundaries, and diagnostics.

## #224 Ownership / Borrow Readiness

Target mapping:

```text
internal/ownership -> selfhost/src/ownership.kizu
```

Ready to expand after:

- Type summaries expose copy-ness and error-union facts.
- Parser snapshots expose enough local binding and call information.
- Move and borrow transitions are executable through package component tests.

Completion evidence:

- `selfhost/src/ownership.kizu` exposes explicit value states and transitions
  for owned, moved, shared-borrowed, and mutably-borrowed values.
- The transition helpers cover copy-preserving moves, non-copy move
  invalidation, double move, shared borrow, mutable borrow conflict, and
  move-while-borrowed facts.
- `selfhost/src/ownership_component_test.kizu` executes those APIs through
  `kizu test selfhost`.
- Full ownership checker porting still requires scoped environment snapshots
  and parity against the memory-safety oracle in `tests/selfhost`.

## #223 IR Readiness

Target mapping:

```text
internal/ir -> selfhost/src/ir.kizu
```

Ready to expand after:

- Type and ownership summaries can feed lowerability decisions.
- Parser snapshots expose enough function/block structure for lowering.
- IR summary facts are executable through package component tests.

Completion evidence:

- `selfhost/src/ir.kizu` exposes instruction, block, function, and module
  summary shapes.
- The summary helpers compare deterministic function-level facts without hidden
  runtime behavior.
- `selfhost/src/ir_component_test.kizu` executes those APIs through
  `kizu test selfhost`.
- Full IR porting still requires normalized Go/Kizu IR dump equality for the
  conformance and lowerability fixture matrix.

## #227 Backend Readiness

Target mapping:

```text
internal/llvm and internal/wasm -> selfhost/src/backend.kizu
```

Ready to expand after:

- IR summaries can provide deterministic function and instruction counts.
- Backend target and artifact smoke facts are executable through package
  component tests.

Completion evidence:

- `selfhost/src/backend.kizu` exposes target and artifact summary shapes.
- The target helpers classify `llvm`, `wasm`, `c`, and unsupported spellings.
- `selfhost/src/backend_component_test.kizu` executes those APIs through
  `kizu test selfhost`.
- Native executable production switching remains out of scope until a backend
  emission path is explicitly selected.

## #226 Cache Contract Readiness

Target mapping:

```text
internal/buildcache -> selfhost/src/cache_contract.kizu
```

Ready to expand after:

- Backend and module graph summaries define deterministic artifact identities.
- Cache input and rebuild reason facts are executable through package component
  tests.

Completion evidence:

- `selfhost/src/cache_contract.kizu` exposes compiler version, target, backend,
  optimization, source hash, and std hash cache input facts.
- The rebuild helper reports no-op, source, std, target/backend/optimization,
  and compiler-version changes.
- `selfhost/src/cache_component_test.kizu` executes those APIs through
  `kizu test selfhost`.
- Go remains responsible for filesystem walking, hashing, status, and prune
  execution until Kizu owns those host primitives.
