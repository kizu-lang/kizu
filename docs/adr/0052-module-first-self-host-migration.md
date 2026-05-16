# ADR-0052: module-first self-host migration

## Status

採用

## Context

The current `selfhost/frontend.kizu` file is a useful bootstrap oracle harness,
but it is not the desired long-term shape of the Kizu compiler. It centralizes
many compiler-shaped probes in one file and makes package-by-package migration
from the Go implementation difficult.

Kizu already specifies explicit modules, `kizu.toml`, `import`, and `pub`
visibility. A real self-host compiler should exercise those features early,
because compiler code naturally needs separate token, lexer, AST, parser,
diagnostic, resolver, type, ownership, IR, backend, and cache modules.

## Decision

Self-host migration will be module-first.

We will first keep the Go compiler responsible for multi-file package loading,
module graph resolution, visibility, diagnostics, and package-level check/build
behavior. Then we will port Go compiler packages to Kizu modules one component
at a time.

The future self-host source layout is:

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

The intended package mapping is:

```text
internal/token      -> selfhost/src/token.kizu
internal/lexer      -> selfhost/src/lexer.kizu
internal/ast        -> selfhost/src/ast.kizu
internal/parser     -> selfhost/src/parser.kizu
internal/project    -> selfhost/src/diagnostics.kizu and resolver.kizu
internal/types      -> selfhost/src/types.kizu
internal/ownership  -> selfhost/src/ownership.kizu
internal/ir         -> selfhost/src/ir.kizu
internal/llvm/wasm  -> selfhost/src/backend.kizu
internal/buildcache -> selfhost/src/cache_contract.kizu
```

`selfhost/frontend.kizu` remains a legacy oracle harness while the new modules
are built. It should not be treated as the production self-host compiler source.
Once the new module tree covers the same oracle surface, the legacy harness can
be deleted.

## Migration Order

1. Finish the Go-owned multi-file package path and keep it as the oracle.
2. Add the multi-file `selfhost/` package scaffold.
3. Port `token` and `lexer`.
4. Port `ast` and `parser`.
5. Port diagnostics and resolver.
6. Port type checking and ownership checking.
7. Port IR.
8. Treat backend and cache as explicit switch decisions, not hidden fallbacks.

Each ported component must have oracle tests against the Go implementation
before it can replace any production path.

## Consequences

- The self-host compiler becomes a normal Kizu package instead of a giant
  single-file harness.
- Go package boundaries guide Kizu module boundaries, which makes migration
  reviewable and testable.
- The legacy harness may be rewritten or deleted when its replacement modules
  have equivalent tests.
- Bootstrap work remains issue-driven. The tracking issue for this reset is
  #190.

