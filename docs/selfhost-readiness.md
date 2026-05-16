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
| token / lexer | strong legacy oracle through `tests/selfhost` | token API, lexer scanner body, and package component test are ported under `selfhost/src` | production switch still blocked until package runtime execution is specified |
| AST / parser | strong legacy oracle through `tests/selfhost` | AST node shapes plus parser summary/declaration snapshot APIs with token spans are ported under `selfhost/src` | parser detail snapshots and package runtime execution |
| diagnostics / resolver | strong legacy oracle through `tests/selfhost` and module fixtures | scaffold only | token/parser modules first |
| type checker | strong legacy oracle for selected conformance and diagnostics | scaffold only | parser/resolver modules first |
| ownership / borrow checker | strong legacy memory-safety oracle | scaffold only | type checker module first |
| IR | strong normalized dump oracle | scaffold only | type and ownership modules first |
| backend | Go-owned smoke fingerprint oracle | contract only | not a v0.3 production switch target |
| cache | Go-owned switch contract oracle | contract only | Kizu filesystem, hashing, module graph, artifact layout APIs |

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
- Until #197 lands, #192 must keep using Go tests as the component oracle and
  must not claim that Kizu package component tests fully replace it.
- Cross-module value expression support is available for imported enum variants,
  public struct literals, and public function calls.
- The scanner body is now ported into `selfhost/src/lexer.kizu`; #197 remains
  the blocker for replacing the legacy frontend oracle with package component
  tests.

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
